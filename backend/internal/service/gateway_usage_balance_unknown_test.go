//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The Redis script may have committed before the client loses its reply.
// Returning a timeout must not turn that unknown result into a second debit.
type committedTimeoutBalanceCache struct {
	*memInflightCache
	cancel             context.CancelFunc
	invalidationError  error
	invalidations      atomic.Int32
	invalidationCtxErr error
	invalidationBudget time.Duration
}

func (c *committedTimeoutBalanceCache) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	_ = c.memInflightCache.DeductUserBalance(ctx, userID, amount)
	if c.cancel != nil {
		c.cancel()
	}
	return context.DeadlineExceeded
}

func (c *committedTimeoutBalanceCache) InvalidateUserBalance(ctx context.Context, _ int64) error {
	c.invalidations.Add(1)
	c.invalidationCtxErr = ctx.Err()
	if deadline, ok := ctx.Deadline(); ok {
		c.invalidationBudget = time.Until(deadline)
	}
	return c.invalidationError
}

func TestSyncBalanceCacheAfterDeduction_UnknownResultNeverReplaysDebit(t *testing.T) {
	for _, invalidateFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalidate_succeeds", true: "invalidate_fails"}[invalidateFails], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache := &committedTimeoutBalanceCache{memInflightCache: newMemInflightCache(1), cancel: cancel}
			if invalidateFails {
				cache.invalidationError = errors.New("cache unavailable")
			}
			svc := newInflightSvc(t, cache, 60)
			p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 0.25}, User: &User{ID: 3}}
			syncBalanceCacheAfterDeduction(ctx, p, &billingDeps{billingCacheService: svc}, nil)
			svc.Stop() // Drain any queued writes before asserting there was no replay.
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, int32(1), cache.deducts.Load(), "a lost debit reply is not permission to debit again")
			balance, err := cache.GetUserBalance(context.Background(), 3)
			require.NoError(t, err)
			require.InDelta(t, 0.75, balance, 1e-12)
			require.Equal(t, int32(1), cache.invalidations.Load())
			require.NoError(t, cache.invalidationCtxErr, "invalidate independently of the cancelled billing context")
			require.Positive(t, cache.invalidationBudget)
			require.LessOrEqual(t, cache.invalidationBudget, 2*time.Second)
		})
	}
}
