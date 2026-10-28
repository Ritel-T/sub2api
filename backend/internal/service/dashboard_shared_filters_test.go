package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type dashboardLegacyFiltersOnlyRepo struct{ UsageLogRepository }

func TestDashboardExtendedFiltersNeverFallBackToUnfilteredData(t *testing.T) {
	svc := NewDashboardService(&dashboardLegacyFiltersOnlyRepo{}, nil, nil, nil)
	f := usagestats.UsageLogFilters{RequestID: "exact-request", BillingMode: "image"}
	_, err := svc.GetUsageTrendWithUsageFilters(context.Background(), time.Time{}, time.Now(), "day", f)
	require.Error(t, err)
	_, err = svc.GetModelStatsWithUsageFiltersBySource(context.Background(), time.Time{}, time.Now(), f, usagestats.ModelSourceRequested)
	require.Error(t, err)
	_, err = svc.GetGroupStatsWithUsageFilters(context.Background(), time.Time{}, time.Now(), f)
	require.Error(t, err)
	_, err = svc.GetAPIKeyUsageTrendWithUsageFilters(context.Background(), time.Time{}, time.Now(), "day", 12, f)
	require.Error(t, err)
	_, err = svc.GetUserUsageTrendWithUsageFilters(context.Background(), time.Time{}, time.Now(), "day", 12, "tokens", f)
	require.Error(t, err)
	_, err = svc.GetUserSpendingRankingWithUsageFilters(context.Background(), time.Time{}, time.Now(), 12, f)
	require.Error(t, err)
}
