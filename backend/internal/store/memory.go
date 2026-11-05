package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
)

// MemoryStore is a thread-safe in-memory implementation of Store for unit and integration testing.
type MemoryStore struct {
	mu            sync.Mutex
	nextID        int64
	maxQueuedJobs int
	repos         map[int64]domain.Repository
	runs          map[string]domain.AnalysisRun
	snapIdx       map[string]string
	files         map[string][]domain.SourceFile
	edges         map[string][]graph.Edge
	importRefs    map[string][]domain.ImportReference
	evidences     map[string][]domain.ImportEvidence
	prRuns        map[string]domain.PullRequestAnalysis
	prSnapIdx     map[string]string
	prFiles       map[string][]domain.PullRequestFile
	diffs         map[string]domain.ArchitectureDiffAnalysis
	diffSnapIdx   map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nextID:      1,
		repos:       make(map[int64]domain.Repository),
		runs:        make(map[string]domain.AnalysisRun),
		snapIdx:     make(map[string]string),
		files:       make(map[string][]domain.SourceFile),
		edges:       make(map[string][]graph.Edge),
		importRefs:  make(map[string][]domain.ImportReference),
		evidences:   make(map[string][]domain.ImportEvidence),
		prRuns:      make(map[string]domain.PullRequestAnalysis),
		prSnapIdx:   make(map[string]string),
		prFiles:     make(map[string][]domain.PullRequestFile),
		diffs:       make(map[string]domain.ArchitectureDiffAnalysis),
		diffSnapIdx: make(map[string]string),
	}
}

func (m *MemoryStore) SetMaxQueuedJobs(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxQueuedJobs = max
}

func (m *MemoryStore) CountActiveAnalyses(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, r := range m.runs {
		if r.Status == domain.StatusQueued || r.Status == domain.StatusRunning {
			count++
		}
	}
	for _, r := range m.prRuns {
		if r.Status == domain.StatusQueued || r.Status == domain.StatusRunning {
			count++
		}
	}
	return count, nil
}

func (m *MemoryStore) CreateOrGetAnalysis(_ context.Context, snap githubclient.RepositorySnapshot, analyzerVersion string) (domain.AnalysisRun, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	repo, ok := m.repos[snap.GitHubID]
	if !ok {
		repo = domain.Repository{
			ID:                 m.nextID,
			GitHubRepositoryID: snap.GitHubID,
			CreatedAt:          now,
		}
		m.nextID++
	}
	repo.Owner = snap.Owner
	repo.Name = snap.Name
	repo.DefaultBranch = snap.DefaultBranch
	repo.HTMLURL = snap.HTMLURL
	repo.UpdatedAt = now
	m.repos[snap.GitHubID] = repo

	snapKey := fmt.Sprintf("%d:%s:%s", repo.ID, snap.CommitSHA, analyzerVersion)
	if existingID, exists := m.snapIdx[snapKey]; exists {
		existing := m.runs[existingID]
		existing.Repository = repo
		cached := existing.Status == domain.StatusCompleted
		return existing, cached, nil
	}

	if m.maxQueuedJobs > 0 {
		active := 0
		for _, r := range m.runs {
			if r.Status == domain.StatusQueued || r.Status == domain.StatusRunning {
				active++
			}
		}
		if active >= m.maxQueuedJobs {
			return domain.AnalysisRun{}, false, domain.NewAppError(
				domain.ErrCodeQueueLimitExceeded,
				http.StatusTooManyRequests,
				"現在解析待ちのジョブが上限に達しています。しばらく待ってから再試行してください",
				nil,
			)
		}
	}

	id := newUUIDv4()
	run := domain.AnalysisRun{
		ID:              id,
		RepositoryID:    repo.ID,
		Repository:      repo,
		CommitSHA:       snap.CommitSHA,
		AnalyzerVersion: analyzerVersion,
		Status:          domain.StatusQueued,
		Stage:           domain.StageQueued,
		CreatedAt:       now,
	}
	m.runs[id] = run
	m.snapIdx[snapKey] = id
	return run, false, nil
}

func (m *MemoryStore) GetAnalysis(_ context.Context, id string) (domain.AnalysisRun, error) {
	if !uuidRegex.MatchString(id) {
		return domain.AnalysisRun{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された解析IDが見つかりません", nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	run, ok := m.runs[id]
	if !ok {
		return domain.AnalysisRun{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された解析IDが見つかりません", nil)
	}
	return run, nil
}

func (m *MemoryStore) ClaimNextAnalysis(_ context.Context, leaseOwner string, leaseDuration time.Duration) (*domain.AnalysisRun, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if leaseOwner == "" {
		leaseOwner = newUUIDv4()
	}

	now := time.Now().UTC()

	// Expire exhausted runs
	for id, r := range m.runs {
		if r.Status == domain.StatusRunning && r.LeaseExpiresAt != nil && r.LeaseExpiresAt.Before(now) && r.AttemptCount >= 3 {
			code := "MAX_ATTEMPTS_EXCEEDED"
			msg := "解析ジョブの試行回数が上限を超えました"
			r.Status = domain.StatusFailed
			r.Stage = domain.StageFailed
			r.ErrorCode = &code
			r.ErrorMessage = &msg
			r.LeaseOwner = nil
			r.LeaseExpiresAt = nil
			r.CompletedAt = &now
			m.runs[id] = r
		}
	}

	var candidates []domain.AnalysisRun
	for _, r := range m.runs {
		if r.AttemptCount >= 3 {
			continue
		}
		if r.Status == domain.StatusQueued || (r.Status == domain.StatusRunning && r.LeaseExpiresAt != nil && r.LeaseExpiresAt.Before(now)) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	chosen := candidates[0]
	leaseExp := now.Add(leaseDuration)
	ownerCopy := leaseOwner
	chosen.Status = domain.StatusRunning
	chosen.Stage = domain.StageFetching
	chosen.AttemptCount++
	if chosen.StartedAt == nil {
		chosen.StartedAt = &now
	}
	chosen.LeaseOwner = &ownerCopy
	chosen.LeaseExpiresAt = &leaseExp
	m.runs[chosen.ID] = chosen
	return &chosen, true, nil
}

func (m *MemoryStore) ExtendAnalysisLease(_ context.Context, id string, leaseOwner string, leaseDuration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runs[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner {
		return domain.ErrLeaseLost
	}
	now := time.Now().UTC()
	leaseExp := now.Add(leaseDuration)
	r.LeaseExpiresAt = &leaseExp
	m.runs[id] = r
	return nil
}

func (m *MemoryStore) UpdateAnalysisStage(_ context.Context, id string, leaseOwner string, stage domain.AnalysisStage, leaseDuration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runs[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}
	now := time.Now().UTC()
	leaseExp := now.Add(leaseDuration)
	r.Stage = stage
	r.LeaseExpiresAt = &leaseExp
	m.runs[id] = r
	return nil
}

func (m *MemoryStore) CompleteAnalysis(_ context.Context, id string, leaseOwner string, output graph.BuildOutput) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runs[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}

	filePathSet := make(map[string]bool, len(output.Files))
	for _, f := range output.Files {
		filePathSet[f.Path] = true
	}
	for _, imp := range output.Imports {
		if !filePathSet[imp.SourcePath] {
			return fmt.Errorf("unknown source file path %q", imp.SourcePath)
		}
		if imp.Classification == domain.ImportInternal && !filePathSet[imp.TargetPath] {
			return fmt.Errorf("unknown target file path %q", imp.TargetPath)
		}
	}

	now := time.Now().UTC()
	r.Status = domain.StatusCompleted
	r.Stage = domain.StageCompleted
	r.Language = output.Language
	if r.Language == "" {
		r.Language = "python"
	}
	r.GraphGranularity = output.GraphGranularity
	if r.GraphGranularity == "" {
		if r.Language == "go" {
			r.GraphGranularity = "package"
		} else {
			r.GraphGranularity = "file"
		}
	}
	r.FilesCount = output.FilesCount
	r.InternalEdgesCount = output.InternalEdgesCount
	r.ExternalImportsCount = output.ExternalImportsCount
	r.UnresolvedImportsCount = output.UnresolvedImportsCount
	r.ErrorCode = nil
	r.ErrorMessage = nil
	r.LeaseOwner = nil
	r.LeaseExpiresAt = nil
	r.CompletedAt = &now
	m.runs[id] = r

	filesCopy := make([]domain.SourceFile, len(output.Files))
	copy(filesCopy, output.Files)
	m.files[id] = filesCopy

	edgesCopy := make([]graph.Edge, len(output.Edges))
	copy(edgesCopy, output.Edges)
	m.edges[id] = edgesCopy

	importsCopy := make([]domain.ImportReference, len(output.Imports))
	copy(importsCopy, output.Imports)
	m.importRefs[id] = importsCopy

	evList := make([]domain.ImportEvidence, 0, len(output.Imports))
	for i, imp := range output.Imports {
		startLine := imp.StartLine
		if startLine <= 0 {
			startLine = imp.LineNumber
		}
		startCol := imp.StartColumn
		if startCol <= 0 {
			startCol = 1
		}
		endLine := imp.EndLine
		if endLine < startLine {
			endLine = startLine
		}
		endCol := imp.EndColumn
		if endCol <= 0 {
			endCol = startCol + len(imp.ImportText)
		}
		importKind := imp.ImportKind
		if importKind == "" {
			importKind = "absolute"
		}
		resReason := imp.ResolutionReason
		if resReason == "" && imp.ResolutionNote != nil {
			resReason = *imp.ResolutionNote
		}
		ghURL := ""
		if r.Repository.Owner != "" && r.Repository.Name != "" && r.CommitSHA != "" && imp.SourcePath != "" && startLine > 0 {
			ghURL = fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%d", r.Repository.Owner, r.Repository.Name, r.CommitSHA, imp.SourcePath, startLine)
		}
		evList = append(evList, domain.ImportEvidence{
			ID:               int64(i + 1),
			AnalysisID:       id,
			SourcePath:       imp.SourcePath,
			TargetPath:       imp.TargetPath,
			ImportStatement:  imp.ImportText,
			ModuleName:       imp.ModuleName,
			StartLine:        startLine,
			StartColumn:      startCol,
			EndLine:          endLine,
			EndColumn:        endCol,
			ImportKind:       importKind,
			ResolutionStatus: string(imp.Classification),
			ResolutionReason: resReason,
			GitHubURL:        ghURL,
		})
	}
	m.evidences[id] = evList
	return nil
}

func (m *MemoryStore) GetImportReferences(_ context.Context, id string) ([]domain.ImportReference, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	refs, ok := m.importRefs[id]
	if !ok {
		return []domain.ImportReference{}, nil
	}
	res := make([]domain.ImportReference, len(refs))
	copy(res, refs)
	return res, nil
}

func (m *MemoryStore) CreateOrGetArchitectureDiff(
	_ context.Context,
	snap githubclient.RepositorySnapshot,
	baseSHA, headSHA string,
	analyzerVersion, diffAlgVersion string,
) (domain.ArchitectureDiffAnalysis, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	repo, ok := m.repos[snap.GitHubID]
	if !ok {
		repo = domain.Repository{
			ID:                 m.nextID,
			GitHubRepositoryID: snap.GitHubID,
			CreatedAt:          now,
		}
		m.nextID++
	}
	repo.Owner = snap.Owner
	repo.Name = snap.Name
	repo.DefaultBranch = snap.DefaultBranch
	repo.HTMLURL = snap.HTMLURL
	repo.UpdatedAt = now
	m.repos[snap.GitHubID] = repo

	diffKey := fmt.Sprintf("%d:%s:%s:%s:%s", repo.ID, baseSHA, headSHA, analyzerVersion, diffAlgVersion)
	if existingID, exists := m.diffSnapIdx[diffKey]; exists {
		existing := m.diffs[existingID]
		existing.Repository = repo
		cached := existing.Status == domain.DiffStatusCompleted
		return existing, cached, nil
	}

	newID := newUUIDv4()
	diffAnalysis := domain.ArchitectureDiffAnalysis{
		ID:                   newID,
		RepositoryID:         repo.ID,
		Repository:           repo,
		BaseSHA:              baseSHA,
		HeadSHA:              headSHA,
		AnalyzerVersion:      analyzerVersion,
		DiffAlgorithmVersion: diffAlgVersion,
		Status:               domain.DiffStatusQueued,
		Stage:                "queued",
		CreatedAt:            now,
	}

	m.diffs[newID] = diffAnalysis
	m.diffSnapIdx[diffKey] = newID
	return diffAnalysis, false, nil
}

func (m *MemoryStore) GetArchitectureDiff(_ context.Context, id string) (domain.ArchitectureDiffAnalysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	diffAnalysis, ok := m.diffs[id]
	if !ok {
		return domain.ArchitectureDiffAnalysis{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された差分解析IDが見つかりません", nil)
	}
	return diffAnalysis, nil
}

func (m *MemoryStore) CompleteArchitectureDiff(_ context.Context, id string, leaseOwner string, summary domain.DiffSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	diffAnalysis, ok := m.diffs[id]
	if !ok {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && diffAnalysis.LeaseOwner != nil && *diffAnalysis.LeaseOwner != leaseOwner {
		return domain.ErrLeaseLost
	}

	now := time.Now().UTC()
	diffAnalysis.Status = domain.DiffStatusCompleted
	diffAnalysis.Stage = "completed"
	diffAnalysis.DiffSummary = &summary
	diffAnalysis.ErrorCode = nil
	diffAnalysis.ErrorMessage = nil
	diffAnalysis.LeaseOwner = nil
	diffAnalysis.LeaseExpiresAt = nil
	diffAnalysis.CompletedAt = &now
	m.diffs[id] = diffAnalysis
	return nil
}

func (m *MemoryStore) FailArchitectureDiff(_ context.Context, id string, leaseOwner string, code string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	diffAnalysis, ok := m.diffs[id]
	if !ok {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && diffAnalysis.LeaseOwner != nil && *diffAnalysis.LeaseOwner != leaseOwner {
		return domain.ErrLeaseLost
	}

	now := time.Now().UTC()
	diffAnalysis.Status = domain.DiffStatusFailed
	diffAnalysis.Stage = "failed"
	diffAnalysis.ErrorCode = &code
	diffAnalysis.ErrorMessage = &message
	diffAnalysis.LeaseOwner = nil
	diffAnalysis.LeaseExpiresAt = nil
	diffAnalysis.CompletedAt = &now
	m.diffs[id] = diffAnalysis
	return nil
}

func (m *MemoryStore) FailAnalysis(_ context.Context, id string, leaseOwner string, code string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runs[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}
	now := time.Now().UTC()
	r.Status = domain.StatusFailed
	r.Stage = domain.StageFailed
	r.ErrorCode = &code
	r.ErrorMessage = &message
	r.LeaseOwner = nil
	r.LeaseExpiresAt = nil
	r.CompletedAt = &now
	m.runs[id] = r
	return nil
}

func (m *MemoryStore) GetAnalysisGraph(_ context.Context, id string) (domain.AnalysisRun, []domain.SourceFile, []graph.Edge, error) {
	if !uuidRegex.MatchString(id) {
		return domain.AnalysisRun{}, nil, nil, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された解析IDが見つかりません", nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.runs[id]
	if !ok {
		return domain.AnalysisRun{}, nil, nil, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された解析IDが見つかりません", nil)
	}
	if r.Status != domain.StatusCompleted {
		return domain.AnalysisRun{}, nil, nil, domain.NewAppError(
			domain.ErrCodeAnalysisNotReady,
			http.StatusConflict,
			fmt.Sprintf("解析はまだ完了していません（現在の状態: %s）", r.Status),
			nil,
		)
	}

	return r, m.files[id], m.edges[id], nil
}

func (m *MemoryStore) GetImportEvidences(_ context.Context, analysisID string, sourcePath, targetPath string, unresolvedOnly bool) ([]domain.ImportEvidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all := m.evidences[analysisID]
	var res []domain.ImportEvidence
	for _, ev := range all {
		if unresolvedOnly {
			if ev.ResolutionStatus == "unresolved" {
				res = append(res, ev)
			}
			continue
		}
		if sourcePath != "" && targetPath != "" {
			if ev.SourcePath == sourcePath && ev.TargetPath == targetPath {
				res = append(res, ev)
			}
			continue
		}
		if sourcePath != "" {
			if ev.SourcePath == sourcePath {
				res = append(res, ev)
			}
			continue
		}
		res = append(res, ev)
	}
	if res == nil {
		res = []domain.ImportEvidence{}
	}
	return res, nil
}

func (m *MemoryStore) CreateOrGetPRAnalysis(_ context.Context, snap domain.PullRequestSnapshot, analyzerVersion string) (domain.PullRequestAnalysis, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	repo, ok := m.repos[snap.Repository.GitHubRepositoryID]
	if !ok {
		repo = snap.Repository
		if repo.ID == 0 {
			repo.ID = m.nextID
			m.nextID++
		}
		repo.CreatedAt = now
	}
	repo.Owner = snap.Repository.Owner
	repo.Name = snap.Repository.Name
	repo.DefaultBranch = snap.Repository.DefaultBranch
	repo.HTMLURL = snap.Repository.HTMLURL
	repo.UpdatedAt = now
	m.repos[snap.Repository.GitHubRepositoryID] = repo

	snapKey := fmt.Sprintf("%d:%d:%s:%s:%s", repo.ID, snap.PullNumber, snap.BaseSHA, snap.HeadSHA, analyzerVersion)
	if existingID, exists := m.prSnapIdx[snapKey]; exists {
		existing := m.prRuns[existingID]
		if existing.Status == domain.StatusCompleted {
			return existing, true, nil
		}
		return existing, false, nil
	}

	if m.maxQueuedJobs > 0 {
		active := 0
		for _, r := range m.runs {
			if r.Status == domain.StatusQueued || r.Status == domain.StatusRunning {
				active++
			}
		}
		for _, r := range m.prRuns {
			if r.Status == domain.StatusQueued || r.Status == domain.StatusRunning {
				active++
			}
		}
		if active >= m.maxQueuedJobs {
			return domain.PullRequestAnalysis{}, false, domain.NewAppError(
				domain.ErrCodeQueueLimitExceeded,
				http.StatusTooManyRequests,
				"現在解析待ちのジョブが上限に達しています。しばらく待ってから再試行してください",
				nil,
			)
		}
	}

	id := newUUIDv4()
	run := domain.PullRequestAnalysis{
		ID:              id,
		RepositoryID:    repo.ID,
		Repository:      repo,
		PullNumber:      snap.PullNumber,
		Title:           snap.Title,
		State:           snap.State,
		BaseSHA:         snap.BaseSHA,
		HeadSHA:         snap.HeadSHA,
		BaseRef:         snap.BaseRef,
		HeadRef:         snap.HeadRef,
		AnalyzerVersion: analyzerVersion,
		Status:          domain.StatusQueued,
		Stage:           domain.StageQueued,
		CreatedAt:       now,
	}
	m.prRuns[id] = run
	m.prSnapIdx[snapKey] = id
	return run, false, nil
}

func (m *MemoryStore) GetPRAnalysis(_ context.Context, id string) (domain.PullRequestAnalysis, error) {
	if !uuidRegex.MatchString(id) {
		return domain.PullRequestAnalysis{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定されたPR解析IDが見つかりません", nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	run, ok := m.prRuns[id]
	if !ok {
		return domain.PullRequestAnalysis{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定されたPR解析IDが見つかりません", nil)
	}
	return run, nil
}

func (m *MemoryStore) ClaimNextPRAnalysis(_ context.Context, leaseOwner string, leaseDuration time.Duration) (*domain.PullRequestAnalysis, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if leaseOwner == "" {
		leaseOwner = newUUIDv4()
	}
	now := time.Now().UTC()

	var candidates []domain.PullRequestAnalysis
	for _, r := range m.prRuns {
		if r.AttemptCount >= 3 {
			continue
		}
		if r.Status == domain.StatusQueued || (r.Status == domain.StatusRunning && r.LeaseExpiresAt != nil && r.LeaseExpiresAt.Before(now)) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	chosen := candidates[0]
	leaseExp := now.Add(leaseDuration)
	ownerCopy := leaseOwner
	chosen.Status = domain.StatusRunning
	chosen.Stage = domain.StageFetching
	chosen.AttemptCount++
	if chosen.StartedAt == nil {
		chosen.StartedAt = &now
	}
	chosen.LeaseOwner = &ownerCopy
	chosen.LeaseExpiresAt = &leaseExp
	m.prRuns[chosen.ID] = chosen
	return &chosen, true, nil
}

func (m *MemoryStore) ExtendPRAnalysisLease(_ context.Context, id string, leaseOwner string, leaseDuration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.prRuns[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner {
		return domain.ErrLeaseLost
	}
	now := time.Now().UTC()
	leaseExp := now.Add(leaseDuration)
	r.LeaseExpiresAt = &leaseExp
	m.prRuns[id] = r
	return nil
}

func (m *MemoryStore) UpdatePRAnalysisStage(_ context.Context, id string, leaseOwner string, stage domain.AnalysisStage, leaseDuration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.prRuns[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}
	now := time.Now().UTC()
	leaseExp := now.Add(leaseDuration)
	r.Stage = stage
	r.LeaseExpiresAt = &leaseExp
	m.prRuns[id] = r
	return nil
}

func (m *MemoryStore) CompletePRAnalysis(_ context.Context, id string, leaseOwner string, headAnalysisID string, files []domain.PullRequestFile, summary domain.PRImpactSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.prRuns[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}

	now := time.Now().UTC()
	r.Status = domain.StatusCompleted
	r.Stage = domain.StageCompleted
	if headAnalysisID != "" {
		r.HeadAnalysisID = &headAnalysisID
	}
	r.ChangedFilesCount = summary.ChangedFilesCount
	r.DirectImpactCount = summary.DirectImpactCount
	r.IndirectImpactCount = summary.IndirectImpactCount
	r.TotalAffectedCount = summary.TotalAffectedCount
	r.UnanalyzedFilesCount = summary.UnanalyzedFilesCount
	r.AnalysisCompleteness = summary.AnalysisCompleteness
	r.LeaseOwner = nil
	r.LeaseExpiresAt = nil
	r.CompletedAt = &now
	m.prRuns[id] = r

	filesCopy := make([]domain.PullRequestFile, len(files))
	copy(filesCopy, files)
	m.prFiles[id] = filesCopy
	return nil
}

func (m *MemoryStore) FailPRAnalysis(_ context.Context, id string, leaseOwner string, code string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.prRuns[id]
	if !ok || r.Status != domain.StatusRunning {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (r.LeaseOwner == nil || *r.LeaseOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}
	now := time.Now().UTC()
	r.Status = domain.StatusFailed
	r.Stage = domain.StageFailed
	r.ErrorCode = &code
	r.ErrorMessage = &message
	r.LeaseOwner = nil
	r.LeaseExpiresAt = nil
	r.CompletedAt = &now
	m.prRuns[id] = r
	return nil
}

func (m *MemoryStore) GetPRAnalysisImpact(_ context.Context, id string) (domain.PullRequestAnalysis, []domain.PullRequestFile, error) {
	if !uuidRegex.MatchString(id) {
		return domain.PullRequestAnalysis{}, nil, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定されたPR解析IDが見つかりません", nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.prRuns[id]
	if !ok {
		return domain.PullRequestAnalysis{}, nil, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定されたPR解析IDが見つかりません", nil)
	}
	return r, m.prFiles[id], nil
}

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
