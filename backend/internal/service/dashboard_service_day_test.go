package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type dayAwareDashboardRepo struct {
	UsageLogRepository
	fetch func(context.Context) (*usagestats.DashboardStats, error)
}

func (r *dayAwareDashboardRepo) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	return r.fetch(ctx)
}

type dashboardDayObservation struct {
	start, end  time.Time
	err         error
	hasDeadline bool
}

func observeDashboardDay(ctx context.Context) dashboardDayObservation {
	start, end := timezone.DayRange(ctx)
	_, deadline := ctx.Deadline()
	return dashboardDayObservation{start, end, ctx.Err(), deadline}
}

func TestDashboardService_AsyncRefreshRetainsCanceledRequestDay(t *testing.T) {
	parent, cancel := context.WithCancel(timezone.WithUserDay(context.Background(), "Pacific/Honolulu"))
	defer cancel()
	start, end := timezone.DayRange(parent)
	payload, err := json.Marshal(dashboardStatsCacheEntry{Stats: &usagestats.DashboardStats{TodayActualCost: 42}, UpdatedAt: time.Now().Add(-time.Minute).Unix()})
	require.NoError(t, err)
	fetched := make(chan dashboardDayObservation, 1)
	saved := make(chan dashboardDayObservation, 1)
	cache := &dashboardCacheStub{
		get: func(context.Context) (string, error) { cancel(); return string(payload), nil },
		set: func(ctx context.Context, _ string, _ time.Duration) error {
			saved <- observeDashboardDay(ctx)
			return nil
		},
	}
	repo := &dayAwareDashboardRepo{fetch: func(ctx context.Context) (*usagestats.DashboardStats, error) {
		fetched <- observeDashboardDay(ctx)
		return &usagestats.DashboardStats{TodayActualCost: 1234567.8912}, nil
	}}
	cfg := &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: true}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true}}
	svc := NewDashboardService(repo, &dashboardAggregationRepoStub{}, cache, cfg)
	stats, err := svc.GetDashboardStats(parent)
	require.NoError(t, err)
	require.Equal(t, 42.0, stats.TodayActualCost)
	for _, ch := range []chan dashboardDayObservation{fetched, saved} {
		select {
		case got := <-ch:
			require.True(t, got.start.Equal(start))
			require.True(t, got.end.Equal(end))
			require.NoError(t, got.err)
			require.True(t, got.hasDeadline)
		case <-time.After(2 * time.Second):
			t.Fatal("request-scoped async refresh timed out")
		}
	}
	require.Equal(t, 1234567.8912, cache.readLastEntry(t).Stats.TodayActualCost)
}

func TestDashboardService_CompletedRefreshSavesInOriginalRequestDay(t *testing.T) {
	parent, cancel := context.WithCancel(timezone.WithUserDay(context.Background(), "Pacific/Honolulu"))
	defer cancel()
	start, end := timezone.DayRange(parent)
	var saved dashboardDayObservation
	cache := &dashboardCacheStub{set: func(ctx context.Context, _ string, _ time.Duration) error {
		saved = observeDashboardDay(ctx)
		return nil
	}}
	repo := &dayAwareDashboardRepo{fetch: func(ctx context.Context) (*usagestats.DashboardStats, error) {
		cancel()
		return &usagestats.DashboardStats{TodayActualCost: 0}, nil
	}}
	cfg := &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: true}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true}}
	svc := NewDashboardService(repo, &dashboardAggregationRepoStub{}, cache, cfg)
	_, err := svc.refreshDashboardStats(parent)
	require.NoError(t, err)
	require.True(t, saved.start.Equal(start))
	require.True(t, saved.end.Equal(end))
	require.NoError(t, saved.err)
	require.True(t, saved.hasDeadline)
	require.Equal(t, int32(1), cache.setCalls)
}

func TestDashboardService_CorruptCacheEvictsOnlyOriginalRequestDay(t *testing.T) {
	parent, cancel := context.WithCancel(timezone.WithUserDay(context.Background(), "Pacific/Honolulu"))
	defer cancel()
	start, end := timezone.DayRange(parent)
	var deleted dashboardDayObservation
	cache := &dashboardCacheStub{
		get: func(context.Context) (string, error) { cancel(); return "invalid-json", nil },
		del: func(ctx context.Context) error { deleted = observeDashboardDay(ctx); return nil },
	}
	cfg := &config.Config{Dashboard: config.DashboardCacheConfig{Enabled: true}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true}}
	svc := NewDashboardService(nil, &dashboardAggregationRepoStub{}, cache, cfg)
	_, _, err := svc.getCachedDashboardStats(parent)
	require.ErrorIs(t, err, ErrDashboardStatsCacheMiss)
	require.True(t, deleted.start.Equal(start))
	require.True(t, deleted.end.Equal(end))
	require.NoError(t, deleted.err)
	require.True(t, deleted.hasDeadline)
	require.Equal(t, int32(1), cache.delCalls)
}
