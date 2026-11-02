package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/store"
)

type Worker struct {
	store               store.Store
	processor           *Processor
	concurrency         int
	pollInterval        time.Duration
	shutdownGracePeriod time.Duration
}

func NewWorker(st store.Store, proc *Processor, concurrency int, pollInterval time.Duration) *Worker {
	if concurrency <= 0 {
		concurrency = 1
	}
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	return &Worker{
		store:               st,
		processor:           proc,
		concurrency:         concurrency,
		pollInterval:        pollInterval,
		shutdownGracePeriod: 15 * time.Second,
	}
}

// StepOnce claims and processes at most one job synchronously.
func (w *Worker) StepOnce(ctx context.Context) (bool, error) {
	run, claimed, err := ClaimNext(ctx, w.store, "", w.processor.leaseDuration)
	if err != nil {
		return false, err
	}
	if !claimed || run == nil {
		return false, nil
	}
	if err := w.processor.ProcessRun(ctx, *run); err != nil {
		return true, err
	}
	return true, nil
}

func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			w.loop(ctx, workerIdx)
		}(i)
	}
	wg.Wait()
	return nil
}

func (w *Worker) loop(ctx context.Context, workerIdx int) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Claim using parent ctx so we never claim new jobs after shutdown signal
		run, claimed, err := ClaimNext(ctx, w.store, "", w.processor.leaseDuration)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("worker claim error", "worker_id", workerIdx, "error", err.Error())
		} else if claimed && run != nil {
			// Process claimed job with a drain context so graceful shutdown lets in-flight work finish cleanly
			execCtx, cancel := context.WithCancel(context.Background())
			stopWatch := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					timer := time.NewTimer(w.shutdownGracePeriod)
					defer timer.Stop()
					select {
					case <-timer.C:
						cancel()
					case <-stopWatch:
					}
				case <-stopWatch:
				}
			}()

			procErr := w.processor.ProcessRun(execCtx, *run)
			close(stopWatch)
			cancel()

			if procErr != nil {
				slog.Warn("worker process run failed", "worker_id", workerIdx, "analysis_id", run.ID, "error", procErr.Error())
			} else {
				slog.Info("worker completed analysis", "worker_id", workerIdx, "analysis_id", run.ID)
			}
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
