package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type dashboardSharedFiltersRepo struct {
	service.UsageLogRepository
	calls     map[string][]usagestats.UsageLogFilters
	breakdown usagestats.UserBreakdownDimension
}

func (r *dashboardSharedFiltersRepo) take(name string, f usagestats.UsageLogFilters) {
	r.calls[name] = append(r.calls[name], f)
}
func (r *dashboardSharedFiltersRepo) GetUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, f usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.take("trend", f)
	return []usagestats.TrendDataPoint{}, nil
}
func (r *dashboardSharedFiltersRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, _, _ time.Time, f usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	r.take("models", f)
	return []usagestats.ModelStat{}, nil
}
func (r *dashboardSharedFiltersRepo) GetGroupStatsWithUsageFilters(_ context.Context, _, _ time.Time, f usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	r.take("groups", f)
	return []usagestats.GroupStat{}, nil
}
func (r *dashboardSharedFiltersRepo) GetAPIKeyUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, _ int, f usagestats.UsageLogFilters) ([]usagestats.APIKeyUsageTrendPoint, error) {
	r.take("api-keys-trend", f)
	return []usagestats.APIKeyUsageTrendPoint{}, nil
}
func (r *dashboardSharedFiltersRepo) GetUserUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, _ int, _ string, f usagestats.UsageLogFilters) ([]usagestats.UserUsageTrendPoint, error) {
	r.take("users-trend", f)
	return []usagestats.UserUsageTrendPoint{}, nil
}
func (r *dashboardSharedFiltersRepo) GetUserSpendingRankingWithUsageFilters(_ context.Context, _, _ time.Time, _ int, f usagestats.UsageLogFilters) (*usagestats.UserSpendingRankingResponse, error) {
	r.take("users-ranking", f)
	return &usagestats.UserSpendingRankingResponse{}, nil
}
func (r *dashboardSharedFiltersRepo) GetStatsWithFilters(_ context.Context, f usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.take("stats", f)
	return &usagestats.UsageStats{}, nil
}
func (r *dashboardSharedFiltersRepo) GetUserBreakdownStats(_ context.Context, _, _ time.Time, d usagestats.UserBreakdownDimension, _ int) ([]usagestats.UserBreakdownItem, error) {
	r.breakdown = d
	return []usagestats.UserBreakdownItem{}, nil
}
func TestDashboardSharedFiltersPropagateAndIsolateCaches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"trend", "models", "groups", "api-keys-trend", "users-trend", "users-ranking", "stats", "snapshot"} {
		t.Run(endpoint, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			dashboardUsersRankingCache = newSnapshotCache(time.Minute)
			usageStatsCache = newSnapshotCache(time.Minute)
			t.Cleanup(resetDashboardReadCachesForTest)
			repo := &dashboardSharedFiltersRepo{calls: map[string][]usagestats.UsageLogFilters{}}
			h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
			u := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
			router := gin.New()
			routes := map[string]gin.HandlerFunc{"trend": h.GetUsageTrend, "models": h.GetModelStats, "groups": h.GetGroupStats, "api-keys-trend": h.GetAPIKeyUsageTrend, "users-trend": h.GetUserUsageTrend, "users-ranking": h.GetUserSpendingRanking, "stats": u.Stats, "snapshot": h.GetSnapshotV2}
			router.GET("/", routes[endpoint])
			q := url.Values{"start_date": {"2026-10-01T00:00:00Z"}, "end_date": {"2026-10-02T00:00:00Z"}, "user_id": {"11"}, "api_key_id": {"12"}, "account_id": {"13"}, "group_id": {"14"}, "model": {"client-alias"}, "request_id": {"req-first"}, "billing_mode": {"image"}, "request_type": {"ws_v2"}, "stream": {"false"}, "native_compaction_v2": {"true"}, "billing_type": {"1"}, "upstream_model_mismatch": {"false"}, "include_stats": {"false"}, "include_trend": {"true"}, "include_model_stats": {"true"}, "include_group_stats": {"true"}, "include_users_trend": {"true"}}
			send := func() {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest("GET", "/?"+q.Encode(), nil))
				require.Equal(t, 200, rec.Code, rec.Body.String())
			}
			send()
			send()
			expected := []string{endpoint}
			if endpoint == "snapshot" {
				expected = []string{"trend", "models", "groups", "users-trend"}
			}
			for _, name := range expected {
				require.Len(t, repo.calls[name], 1)
				f := repo.calls[name][0]
				require.Equal(t, int64(11), f.UserID)
				require.Equal(t, int64(12), f.APIKeyID)
				require.Equal(t, int64(13), f.AccountID)
				require.Equal(t, int64(14), f.GroupID)
				require.Equal(t, "client-alias", f.Model)
				require.Equal(t, usagestats.ModelSourceRequested, f.ModelFilterSource)
				require.Equal(t, "req-first", f.RequestID)
				require.Equal(t, "image", f.BillingMode)
				require.Equal(t, int16(service.RequestTypeWSV2), *f.RequestType)
				require.Nil(t, f.Stream)
				require.True(t, *f.NativeCompactionV2)
				require.Equal(t, int8(1), *f.BillingType)
				require.False(t, *f.UpstreamModelMismatch)
			}
			for _, change := range []struct{ key, value string }{{"billing_mode", "video"}, {"request_id", "req-second"}, {"model", "other-alias"}, {"upstream_model_mismatch", "true"}} {
				q.Set(change.key, change.value)
				send()
			}
			for _, name := range expected {
				require.Len(t, repo.calls[name], 5, "cache must isolate every active dimension")
			}
		})
	}
}
func TestDashboardUserBreakdownSharesExtendedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &dashboardSharedFiltersRepo{calls: map[string][]usagestats.UsageLogFilters{}}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	r := gin.New()
	r.GET("/", h.GetUserBreakdown)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/?start_date=2026-10-01&end_date=2026-10-02&billing_mode=token&request_id=exact&upstream_model_mismatch=false&request_type=ws_v2&stream=false", nil))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, "token", repo.breakdown.BillingMode)
	require.Equal(t, "exact", repo.breakdown.RequestID)
	require.False(t, *repo.breakdown.UpstreamModelMismatch)
	require.Nil(t, repo.breakdown.Stream)
}

func TestSharedUsageFiltersRejectInvalidValuesBeforeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &DashboardHandler{}
	u := &UsageHandler{}
	endpoints := map[string]gin.HandlerFunc{"trend": h.GetUsageTrend, "models": h.GetModelStats, "groups": h.GetGroupStats, "snapshot": h.GetSnapshotV2, "users-trend": h.GetUserUsageTrend, "api-keys-trend": h.GetAPIKeyUsageTrend, "users-ranking": h.GetUserSpendingRanking, "user-breakdown": h.GetUserBreakdown, "records": u.List, "stats": u.Stats}
	bad := []string{"user_id=-1", "user_id=0", "user_id=nope", "api_key_id=-1", "api_key_id=0", "api_key_id=nope", "account_id=-1", "account_id=0", "account_id=nope", "group_id=-1", "group_id=0", "group_id=nope", "billing_type=-1", "billing_type=2", "billing_type=nope", "billing_mode=unknown"}
	for name, handler := range endpoints {
		t.Run(name, func(t *testing.T) {
			r := gin.New()
			r.GET("/", handler)
			for _, filter := range bad {
				t.Run(filter, func(t *testing.T) {
					rec := httptest.NewRecorder()
					r.ServeHTTP(rec, httptest.NewRequest("GET", "/?start_date=2026-10-01&end_date=2026-10-02&"+filter, nil))
					require.Equal(t, 400, rec.Code, rec.Body.String())
				})
			}
		})
	}
}
func TestSharedUsageFilterEnumsAndEmptyValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"", "token", "per_request", "image", "video"} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/?user_id=&api_key_id=&account_id=&group_id=&billing_type=0&billing_mode="+mode, nil)
		f, err := parseDashboardSnapshotV2Filters(c)
		require.NoError(t, err)
		require.Equal(t, mode, f.BillingMode)
		require.Equal(t, int8(0), *f.BillingType)
	}
}
