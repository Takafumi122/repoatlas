package jobs

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

const (
	DefaultLeaseDuration     = 120 * time.Second
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultAnalysisTimeout   = 90 * time.Second
)

func ClaimNext(ctx context.Context, st store.Store, leaseOwner string, leaseDuration time.Duration) (*domain.AnalysisRun, bool, error) {
	if leaseDuration <= 0 {
		leaseDuration = DefaultLeaseDuration
	}
	if leaseOwner == "" {
		leaseOwner = generateLeaseToken()
	}
	return st.ClaimNextAnalysis(ctx, leaseOwner, leaseDuration)
}

func ClaimNextPR(ctx context.Context, st store.Store, leaseOwner string, leaseDuration time.Duration) (*domain.PullRequestAnalysis, bool, error) {
	if leaseDuration <= 0 {
		leaseDuration = DefaultLeaseDuration
	}
	if leaseOwner == "" {
		leaseOwner = generateLeaseToken()
	}
	return st.ClaimNextPRAnalysis(ctx, leaseOwner, leaseDuration)
}

func generateLeaseToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("worker-%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
