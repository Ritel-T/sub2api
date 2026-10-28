package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type exactWindowCall struct {
	id         int64
	start, end time.Time
}
type exactWindowStatsRepo struct {
	UsageLogRepository
	calls []exactWindowCall
	fail  bool
}

func (r *exactWindowStatsRepo) GetAccountWindowStatsRange(_ context.Context, id int64, start, end time.Time) (*usagestats.AccountStats, error) {
	r.calls = append(r.calls, exactWindowCall{id, start, end})
	if r.fail {
		return nil, context.DeadlineExceeded
	}
	currentPriceEquivalent := 999.0
	return &usagestats.AccountStats{Requests: 2, Tokens: 300, StandardCost: 12.34, Cost: 98.72, UserCost: 1.23, APIEquivalentCost: &currentPriceEquivalent, APIEquivalentUnpricedRequests: 1}, nil
}

func TestAccountUsageWindowStandard_ExactCyclesSeparateFromOtherBillingBases(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	fiveReset := now.Add(2 * time.Hour)
	weekReset := now.Add(3 * 24 * time.Hour)
	repo := &exactWindowStatsRepo{}
	svc := &AccountUsageService{usageLogRepo: repo, cache: NewUsageCache()}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Extra: map[string]any{}}
	five := &UsageProgress{ResetsAt: &fiveReset, Utilization: 95}
	week := &UsageProgress{ResetsAt: &weekReset, Utilization: 12}
	svc.attachCurrentWindowStats(context.Background(), account, five, "5h", 5*time.Hour, now)
	svc.attachCurrentWindowStats(context.Background(), account, week, "7d", 7*24*time.Hour, now)
	require.Equal(t, []exactWindowCall{{42, fiveReset.Add(-5 * time.Hour), now}, {42, weekReset.Add(-7 * 24 * time.Hour), now}}, repo.calls)
	require.Equal(t, 12.34, five.WindowStats.StandardCost)
	require.Equal(t, 98.72, five.WindowStats.Cost)
	require.Equal(t, 1.23, five.WindowStats.UserCost)
	require.Equal(t, 999.0, *five.WindowStats.APIEquivalentCost)
	require.Equal(t, int64(1), five.WindowStats.APIEquivalentUnpricedRequests)
	require.Equal(t, repo.calls[0].start, *five.WindowStartsAt)
	require.Equal(t, now, *week.WindowStatsUntil)
	// Utilization changes do not change dollar consumption or rebuild the query.
	five.Utilization = 20
	svc.attachCurrentWindowStats(context.Background(), account, five, "5h", 5*time.Hour, now.Add(10*time.Second))
	require.Len(t, repo.calls, 2)
	require.Equal(t, 12.34, five.WindowStats.StandardCost)
	require.Equal(t, now, *five.WindowStatsUntil)
	// A newly observed cycle supersedes cache immediately, even within one minute.
	nextReset := fiveReset.Add(10 * time.Minute)
	five.ResetsAt = &nextReset
	svc.attachCurrentWindowStats(context.Background(), account, five, "5h", 5*time.Hour, now.Add(15*time.Second))
	require.Len(t, repo.calls, 3)
	require.Equal(t, nextReset.Add(-5*time.Hour), repo.calls[2].start)
}

func TestAccountUsageWindowStandard_MissingExpiredOrImpossibleCycleIsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	future := now.Add(6 * time.Hour)
	for _, reset := range []*time.Time{nil, &expired, &future} {
		repo := &exactWindowStatsRepo{}
		svc := &AccountUsageService{usageLogRepo: repo, cache: NewUsageCache()}
		progress := &UsageProgress{ResetsAt: reset, WindowStats: &WindowStats{StandardCost: 777}}
		svc.attachCurrentWindowStats(context.Background(), &Account{ID: 1}, progress, "5h", 5*time.Hour, now)
		require.Empty(t, repo.calls)
		require.Nil(t, progress.WindowStats)
		require.Nil(t, progress.WindowStartsAt)
		require.Nil(t, progress.WindowStatsUntil)
	}
}

func TestAccountUsageWindowStandard_ObservedDurationAndFailure(t *testing.T) {
	require.Equal(t, 2*time.Hour, codexWindowDuration(map[string]any{"codex_5h_window_minutes": 120}, "5h", 5*time.Hour))
	require.Zero(t, codexWindowDuration(map[string]any{"codex_5h_window_minutes": "invalid"}, "5h", 5*time.Hour))
	require.Zero(t, codexWindowDuration(map[string]any{"codex_5h_window_minutes": 120.5}, "5h", 5*time.Hour))
	require.Equal(t, 7*24*time.Hour, codexWindowDuration(nil, "7d", 7*24*time.Hour))
	now := time.Now()
	reset := now.Add(time.Hour)
	repo := &exactWindowStatsRepo{fail: true}
	svc := &AccountUsageService{usageLogRepo: repo, cache: NewUsageCache()}
	progress := &UsageProgress{ResetsAt: &reset}
	svc.attachCurrentWindowStats(context.Background(), &Account{ID: 1}, progress, "5h", 2*time.Hour, now)
	require.Equal(t, reset.Add(-2*time.Hour), repo.calls[0].start)
	require.Nil(t, progress.WindowStats)
	require.NotNil(t, progress.WindowStartsAt)
	require.Nil(t, progress.WindowStatsUntil)
}
