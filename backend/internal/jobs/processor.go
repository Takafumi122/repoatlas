package jobs

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

type Processor struct {
	store             store.Store
	ghClient          githubclient.Client
	analyzer          analyzer.Analyzer
	archiveLimits     githubclient.ArchiveLimits
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	analysisTimeout   time.Duration
}

type ProcessorOptions struct {
	ArchiveLimits     githubclient.ArchiveLimits
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	AnalysisTimeout   time.Duration
}

func NewProcessor(st store.Store, gh githubclient.Client, az analyzer.Analyzer, opts ProcessorOptions) *Processor {
	limits := opts.ArchiveLimits
	if limits.MaxCompressedBytes <= 0 {
		limits = githubclient.DefaultArchiveLimits()
	}
	lease := opts.LeaseDuration
	if lease <= 0 {
		lease = DefaultLeaseDuration
	}
	heartbeat := opts.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = lease / 3
		if heartbeat <= 0 {
			heartbeat = DefaultHeartbeatInterval
		}
	}
	timeout := opts.AnalysisTimeout
	if timeout <= 0 {
		timeout = DefaultAnalysisTimeout
	}
	return &Processor{
		store:             st,
		ghClient:          gh,
		analyzer:          az,
		archiveLimits:     limits,
		leaseDuration:     lease,
		heartbeatInterval: heartbeat,
		analysisTimeout:   timeout,
	}
}

func (p *Processor) ProcessRun(ctx context.Context, run domain.AnalysisRun) error {
	jobCtx, cancel := context.WithTimeout(ctx, p.analysisTimeout)
	defer cancel()

	leaseOwner := ""
	if run.LeaseOwner != nil {
		leaseOwner = *run.LeaseOwner
	}

	// Start periodic heartbeat goroutine to extend lease while job is active
	hbStop := make(chan struct{})
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		if leaseOwner == "" || p.heartbeatInterval <= 0 {
			return
		}
		ticker := time.NewTicker(p.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				hbErr := p.store.ExtendAnalysisLease(jobCtx, run.ID, leaseOwner, p.leaseDuration)
				if errors.Is(hbErr, domain.ErrLeaseLost) {
					// Lease was stolen or expired; abort in-flight work immediately
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		close(hbStop)
		<-hbDone
	}()

	snapshot := githubclient.RepositorySnapshot{
		GitHubID:      run.Repository.GitHubRepositoryID,
		Owner:         run.Repository.Owner,
		Name:          run.Repository.Name,
		DefaultBranch: run.Repository.DefaultBranch,
		CommitSHA:     run.CommitSHA,
		HTMLURL:       run.Repository.HTMLURL,
	}

	rc, err := p.ghClient.DownloadArchive(jobCtx, snapshot)
	if err != nil {
		p.recordFailure(ctx, run.ID, leaseOwner, err)
		return err
	}

	extracted, _, err := githubclient.ExtractRepositoryFilesFromZip(rc, p.archiveLimits)
	_ = rc.Close()
	if err != nil {
		p.recordFailure(ctx, run.ID, leaseOwner, err)
		return err
	}

	sourceFiles := make([]analyzer.SourceFile, len(extracted))
	for i, ef := range extracted {
		sourceFiles[i] = analyzer.SourceFile{
			Path:    ef.Path,
			Content: ef.Content,
		}
	}

	var stageLeaseErr error
	output, err := p.analyzer.Analyze(jobCtx, sourceFiles, func(stage domain.AnalysisStage) {
		if uErr := p.store.UpdateAnalysisStage(jobCtx, run.ID, leaseOwner, stage, p.leaseDuration); errors.Is(uErr, domain.ErrLeaseLost) {
			stageLeaseErr = uErr
			cancel()
		}
	})
	if stageLeaseErr != nil {
		return stageLeaseErr
	}
	if err != nil {
		p.recordFailure(ctx, run.ID, leaseOwner, err)
		return err
	}

	if err := p.store.UpdateAnalysisStage(jobCtx, run.ID, leaseOwner, domain.StagePersisting, p.leaseDuration); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
	}

	if err := p.store.CompleteAnalysis(jobCtx, run.ID, leaseOwner, output); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
		p.recordFailure(ctx, run.ID, leaseOwner, err)
		return err
	}

	return nil
}

func (p *Processor) recordFailure(_ context.Context, analysisID string, leaseOwner string, err error) {
	if errors.Is(err, domain.ErrLeaseLost) {
		return
	}
	code := string(domain.ErrCodeInternalError)
	msg := "リポジトリの解析中にエラーが発生しました"

	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		code = string(appErr.Code)
		msg = appErr.Message
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "ANALYSIS_TIMEOUT"
		msg = "解析処理がタイムアウト（90秒上限）しました"
	}

	failCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.store.FailAnalysis(failCtx, analysisID, leaseOwner, code, msg)
}

func (p *Processor) ProcessPRRun(ctx context.Context, pr domain.PullRequestAnalysis) error {
	jobCtx, cancel := context.WithTimeout(ctx, p.analysisTimeout)
	defer cancel()

	leaseOwner := ""
	if pr.LeaseOwner != nil {
		leaseOwner = *pr.LeaseOwner
	}

	hbStop := make(chan struct{})
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		if leaseOwner == "" || p.heartbeatInterval <= 0 {
			return
		}
		ticker := time.NewTicker(p.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				hbErr := p.store.ExtendPRAnalysisLease(jobCtx, pr.ID, leaseOwner, p.leaseDuration)
				if errors.Is(hbErr, domain.ErrLeaseLost) {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		close(hbStop)
		<-hbDone
	}()

	// Stage 1: resolving_metadata
	if err := p.store.UpdatePRAnalysisStage(jobCtx, pr.ID, leaseOwner, domain.StageResolvingMetadata, p.leaseDuration); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
	}

	snap, err := p.ghClient.GetPullRequest(jobCtx, pr.Repository.Owner, pr.Repository.Name, pr.PullNumber)
	if err != nil {
		p.recordPRFailure(ctx, pr.ID, leaseOwner, err)
		return err
	}

	// Verify commit SHA consistency
	if snap.HeadSHA != pr.HeadSHA || snap.BaseSHA != pr.BaseSHA {
		err := domain.NewAppError(
			domain.ErrCodePRHeadSHAChanged,
			http.StatusConflict,
			"解析中にPRのコミットSHAが更新されました。再取得してください",
			nil,
		)
		p.recordPRFailure(ctx, pr.ID, leaseOwner, err)
		return err
	}

	files, err := p.ghClient.GetPullRequestFiles(jobCtx, pr.Repository.Owner, pr.Repository.Name, pr.PullNumber, 300)
	if err != nil {
		p.recordPRFailure(ctx, pr.ID, leaseOwner, err)
		return err
	}

	// Stage 2: analyzing_head
	if err := p.store.UpdatePRAnalysisStage(jobCtx, pr.ID, leaseOwner, domain.StageAnalyzingHead, p.leaseDuration); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
	}

	headSnap := githubclient.RepositorySnapshot{
		GitHubID:      pr.Repository.GitHubRepositoryID,
		Owner:         pr.Repository.Owner,
		Name:          pr.Repository.Name,
		DefaultBranch: pr.Repository.DefaultBranch,
		CommitSHA:     pr.HeadSHA,
		HTMLURL:       pr.Repository.HTMLURL,
	}

	headRun, _, err := p.store.CreateOrGetAnalysis(jobCtx, headSnap, pr.AnalyzerVersion)
	if err != nil {
		p.recordPRFailure(ctx, pr.ID, leaseOwner, err)
		return err
	}

	if headRun.Status != domain.StatusCompleted {
		headRunToProcess := headRun
		if headRunToProcess.Status == domain.StatusQueued {
			claimedRun, claimed, cErr := p.store.ClaimNextAnalysis(jobCtx, leaseOwner, p.leaseDuration)
			if cErr == nil && claimed && claimedRun != nil && claimedRun.ID == headRun.ID {
				headRunToProcess = *claimedRun
			}
		}
		if pErr := p.ProcessRun(jobCtx, headRunToProcess); pErr != nil {
			p.recordPRFailure(ctx, pr.ID, leaseOwner, pErr)
			return pErr
		}
	}

	// Stage 3: analyzing_impact
	if err := p.store.UpdatePRAnalysisStage(jobCtx, pr.ID, leaseOwner, domain.StageAnalyzingImpact, p.leaseDuration); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
	}

	_, headFiles, headEdges, err := p.store.GetAnalysisGraph(jobCtx, headRun.ID)
	if err != nil {
		p.recordPRFailure(ctx, pr.ID, leaseOwner, err)
		return err
	}

	impactResult := graph.AnalyzeMultiSourceImpact(headFiles, headEdges, files, 100)

	// Stage 4: persisting
	if err := p.store.UpdatePRAnalysisStage(jobCtx, pr.ID, leaseOwner, domain.StagePersisting, p.leaseDuration); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
	}

	if err := p.store.CompletePRAnalysis(jobCtx, pr.ID, leaseOwner, headRun.ID, files, impactResult.Summary); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			return err
		}
		p.recordPRFailure(ctx, pr.ID, leaseOwner, err)
		return err
	}

	return nil
}

func (p *Processor) recordPRFailure(_ context.Context, prID string, leaseOwner string, err error) {
	if errors.Is(err, domain.ErrLeaseLost) {
		return
	}
	code := string(domain.ErrCodeInternalError)
	msg := "Pull Requestの解析中にエラーが発生しました"

	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		code = string(appErr.Code)
		msg = appErr.Message
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "ANALYSIS_TIMEOUT"
		msg = "PR解析処理がタイムアウト（90秒上限）しました"
	}

	failCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.store.FailPRAnalysis(failCtx, prID, leaseOwner, code, msg)
}
