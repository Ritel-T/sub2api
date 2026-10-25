package admin

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsageStatsCacheKeyEveryResultFilter(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 123, time.UTC)
	end := start.AddDate(0, 0, 1)
	base := usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}
	key := usageStatsCacheKey(base)
	// Walk the filter struct so additions require an explicit classification.
	for i := 0; i < reflect.TypeOf(base).NumField(); i++ {
		field := reflect.TypeOf(base).Field(i)
		t.Run(field.Name, func(t *testing.T) {
			other := base
			v := reflect.ValueOf(&other).Elem().Field(i)
			switch v.Kind() {
			case reflect.Bool:
				v.SetBool(true)
			case reflect.Int64:
				v.SetInt(7)
			case reflect.String:
				v.SetString("different")
			case reflect.Pointer:
				p := reflect.New(v.Type().Elem())
				if v.Type().Elem() == reflect.TypeOf(time.Time{}) {
					p.Elem().Set(reflect.ValueOf(v.Elem().Interface().(time.Time).Add(time.Nanosecond)))
				}
				v.Set(p) // nil versus explicit false/0 is distinct
			default:
				t.Fatalf("unclassified result filter %s", field.Name)
			}
			require.NotEqual(t, key, usageStatsCacheKey(other))
			if v.Kind() == reflect.Pointer && v.Type().Elem() != reflect.TypeOf(time.Time{}) {
				zeroKey := usageStatsCacheKey(other)
				if v.Elem().Kind() == reflect.Bool {
					v.Elem().SetBool(true)
				} else {
					v.Elem().SetInt(1)
				}
				require.NotEqual(t, zeroKey, usageStatsCacheKey(other))
			}
		})
	}
	localStart := start.In(time.FixedZone("offset", -4*3600))
	base.StartTime = &localStart
	require.Equal(t, key, usageStatsCacheKey(base))
	noStart := base
	noStart.StartTime = nil
	require.NotEqual(t, key, usageStatsCacheKey(noStart))
}

type statsIsolationRepo struct {
	service.UsageLogRepository
	calls   atomic.Int32
	value   *usagestats.UsageStats
	release <-chan struct{}
}

func (r *statsIsolationRepo) GetStatsWithFilters(context.Context, usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.calls.Add(1)
	if r.release != nil {
		<-r.release
	}
	return r.value, nil
}
func newStatsIsolationHandler(t *testing.T, repo *statsIsolationRepo) *UsageHandler {
	previous := usageStatsCache
	usageStatsCache = newSnapshotCache(30 * time.Second)
	t.Cleanup(func() { usageStatsCache = previous })
	return NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
}
func statsIsolationValue() *usagestats.UsageStats {
	cost := math.Nextafter(0.1, 1)
	return &usagestats.UsageStats{TotalRequests: 1<<53 + 1, TotalCost: cost, TotalAccountCost: &cost,
		Endpoints:         []usagestats.EndpointStat{{Endpoint: "in", Requests: 1<<53 + 1, Cost: cost}},
		UpstreamEndpoints: []usagestats.EndpointStat{{Endpoint: "out"}},
		EndpointPaths:     []usagestats.EndpointStat{{Endpoint: "/v1/responses"}}}
}
func mutateStats(s *usagestats.UsageStats) {
	s.TotalRequests = 0
	*s.TotalAccountCost = 0
	s.Endpoints[0].Endpoint = "changed"
	s.UpstreamEndpoints[0].Endpoint = "changed"
	s.EndpointPaths[0].Endpoint = "changed"
}

func TestUsageStatsCacheConcurrentIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		repo := &statsIsolationRepo{value: statsIsolationValue(), release: release}
		h := newStatsIsolationHandler(t, repo)
		results := make([]*usagestats.UsageStats, 8)
		var wg sync.WaitGroup
		for i := range results {
			wg.Go(func() {
				s, _, err := h.getStatsCached(context.Background(), usagestats.UsageLogFilters{UserID: 7})
				if err != nil {
					t.Error(err)
				}
				results[i] = s
			})
		}
		synctest.Wait()
		require.Equal(t, int32(1), repo.calls.Load())
		close(release)
		wg.Wait()
		want := statsIsolationValue()
		for _, result := range results {
			require.Equal(t, want, result)
		}
		mutateStats(repo.value)
		for _, result := range results {
			wg.Go(func() { mutateStats(result) })
		}
		wg.Wait()
		got, hit, err := h.getStatsCached(context.Background(), usagestats.UsageLogFilters{UserID: 7})
		require.NoError(t, err)
		require.True(t, hit)
		require.Equal(t, want, got)
		require.Equal(t, int32(1), repo.calls.Load())
	})
}

func TestCloneUsageStatsShapeAndPrecision(t *testing.T) {
	require.Nil(t, cloneUsageStats(nil))
	for _, source := range []*usagestats.UsageStats{{}, {Endpoints: []usagestats.EndpointStat{}, UpstreamEndpoints: []usagestats.EndpointStat{}, EndpointPaths: []usagestats.EndpointStat{}}, statsIsolationValue()} {
		clone := cloneUsageStats(source)
		require.Equal(t, source, clone)
		before, err := json.Marshal(source)
		require.NoError(t, err)
		after, err := json.Marshal(clone)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after))
	}
}

func TestUsageStatsCacheHandlerContractAndBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &statsIsolationRepo{value: statsIsolationValue()}
	h := newStatsIsolationHandler(t, repo)
	router := gin.New()
	router.GET("/stats", h.Stats)
	url := "/stats?start_date=2026-07-01&end_date=2026-09-30&user_id=7&api_key_id=8&account_id=9&group_id=10&model=x"
	request := func(suffix, cache string) string {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url+suffix, nil))
		require.Equal(t, 200, rec.Code)
		require.Equal(t, cache, rec.Header().Get("X-Usage-Stats-Cache"))
		return rec.Body.String()
	}
	original := request("", "miss")
	require.Equal(t, original, request("", "hit"))
	require.Equal(t, int32(1), repo.calls.Load())
	repo.value.TotalRequests = 42
	require.NotEqual(t, original, request("&nocache=true", "bypass"))
	require.Equal(t, original, request("", "hit"))
	require.Equal(t, int32(2), repo.calls.Load())
	request("&stream=false", "miss")
	require.Equal(t, int32(3), repo.calls.Load())
	// Real admin middleware rejects unauthenticated callers even with a warm cache.
	guarded := gin.New()
	guarded.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(nil, nil, nil, nil)))
	guarded.GET("/stats", h.Stats)
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, 401, rec.Code)
	require.Equal(t, int32(3), repo.calls.Load())
}
