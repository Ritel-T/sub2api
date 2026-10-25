package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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

type historicalStatsRepo struct {
	service.UsageLogRepository
	calls   atomic.Int32
	release <-chan struct{}
	err     error
	last    usagestats.UsageLogFilters
	mu      sync.Mutex
}

func (r *historicalStatsRepo) GetStatsWithFilters(ctx context.Context, f usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.last = f
	r.mu.Unlock()
	if r.release != nil {
		<-r.release
	}
	if r.err != nil {
		return nil, r.err
	}
	cost := 0.123456789012345
	return &usagestats.UsageStats{TotalRequests: int64(r.calls.Load()), TotalCost: cost, TotalAccountCost: &cost, UpstreamEndpoints: []usagestats.EndpointStat{{Endpoint: "private"}}}, nil
}

type historicalKeysRepo struct {
	service.APIKeyRepository
	owner atomic.Int64
	calls atomic.Int32
}

func (r *historicalKeysRepo) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	r.calls.Add(1)
	return &service.APIKey{ID: id, UserID: r.owner.Load()}, nil
}
func historicalRouter(repo *historicalStatsRepo, keys *historicalKeysRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), service.NewAPIKeyService(keys, nil, nil, nil, nil, nil, nil), nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if id, _ := strconv.ParseInt(c.GetHeader("test-user"), 10, 64); id > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		}
		c.Set(string(middleware.ContextKeyUserRole), c.GetHeader("test-role"))
	})
	router.GET("/usage/stats", h.Stats)
	return router
}

const historicalRange = "/usage/stats?start_date=1999-07-01T01:02:03.123456789Z&end_date=1999-09-30T02:03:04.987654321Z"

func historicalRequest(router *gin.Engine, ctx context.Context, url, user, role string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil).WithContext(ctx)
	req.Header.Set("test-user", user)
	req.Header.Set("test-role", role)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestUserHistoricalStatsCacheScopeOwnershipAndBypass(t *testing.T) {
	r := &historicalStatsRepo{}
	keys := &historicalKeysRepo{}
	keys.owner.Store(42)
	router := historicalRouter(r, keys)
	get := func(url, user, role, status string) *httptest.ResponseRecorder {
		w := historicalRequest(router, context.Background(), url, user, role)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, status, w.Header().Get("X-Usage-Stats-Cache"))
		return w
	}
	url := historicalRange + "&api_key_id=7&user_id=999"
	first := get(url, "42", service.RoleUser, "miss")
	require.Equal(t, first.Body.String(), get(url, "42", service.RoleUser, "hit").Body.String())
	require.Equal(t, int32(1), r.calls.Load())
	require.Equal(t, int32(2), keys.calls.Load())
	require.Equal(t, int64(42), r.last.UserID)
	require.Contains(t, first.Body.String(), "1999-07-01T01:02:03.123456789Z")
	require.Contains(t, first.Body.String(), "1999-09-30T02:03:04.987654321Z")
	require.NotContains(t, first.Body.String(), "total_account_cost")
	require.NotContains(t, first.Body.String(), "private")
	get(historicalRange+"&api_key_id=8", "42", service.RoleUser, "miss")
	require.Equal(t, int32(2), r.calls.Load())
	keys.owner.Store(43)
	w := historicalRequest(router, context.Background(), url, "42", service.RoleUser)
	require.Equal(t, 403, w.Code)
	require.Equal(t, int32(2), r.calls.Load())
	get(url, "43", service.RoleUser, "miss")
	require.Equal(t, int32(3), r.calls.Load())
	keys.owner.Store(42)
	observer := get(url, "42", service.RoleObserver, "miss")
	require.Contains(t, observer.Body.String(), "total_account_cost")
	require.Contains(t, observer.Body.String(), "private")
	require.Equal(t, int32(4), r.calls.Load())
	get(url+"&account_id=9", "42", service.RoleObserver, "miss")
	require.Equal(t, int32(5), r.calls.Load())
	get(url+"&nocache=true", "42", service.RoleUser, "bypass")
	require.Equal(t, first.Body.String(), get(url, "42", service.RoleUser, "hit").Body.String())
	require.Equal(t, int32(6), r.calls.Load())
	get(historicalRange+"&group_id=20&nocache=true", "42", service.RoleUser, "bypass")
	get(historicalRange+"&group_id=20", "42", service.RoleUser, "miss")
	require.Equal(t, int32(8), r.calls.Load())
	w = historicalRequest(router, context.Background(), url, "", service.RoleUser)
	require.Equal(t, 401, w.Code)
	require.Equal(t, int32(8), r.calls.Load())
}

func TestUserHistoricalStatsCacheConcurrentHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		r := &historicalStatsRepo{release: release}
		router := historicalRouter(r, &historicalKeysRepo{})
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				w := historicalRequest(router, context.Background(), historicalRange, "42", service.RoleUser)
				if w.Code != 200 {
					t.Errorf("response %d: %s", w.Code, w.Body.String())
				}
			})
		}
		synctest.Wait()
		require.Equal(t, int32(1), r.calls.Load())
		close(release)
		wg.Wait()
		require.Equal(t, int32(1), r.calls.Load())
	})
}

func TestUserHistoricalStatsCacheFailureCancellationAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &historicalStatsRepo{err: errors.New("database unavailable")}
		router := historicalRouter(r, &historicalKeysRepo{})
		for range 2 {
			w := historicalRequest(router, context.Background(), historicalRange, "42", service.RoleUser)
			require.Equal(t, 500, w.Code)
		}
		require.Equal(t, int32(2), r.calls.Load())
		r.err = nil
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		historicalRequest(router, ctx, historicalRange, "42", service.RoleUser)
		require.Equal(t, int32(2), r.calls.Load())
		for _, hit := range []string{"miss", "hit", "miss"} {
			w := historicalRequest(router, context.Background(), historicalRange, "42", service.RoleUser)
			require.Equal(t, 200, w.Code)
			require.Equal(t, hit, w.Header().Get("X-Usage-Stats-Cache"))
			if hit == "hit" {
				time.Sleep(31 * time.Second)
			}
		}
		require.Equal(t, int32(4), r.calls.Load())
	})
}

func TestUserHistoricalStatsCacheKeepsRollingAndFutureRangesUncached(t *testing.T) {
	r := &historicalStatsRepo{}
	router := historicalRouter(r, &historicalKeysRepo{})
	for _, url := range []string{"/usage/stats?period=week", "/usage/stats", historicalRange[:len("/usage/stats")] + "?start_date=1999-07-01&end_date=2099-01-01"} {
		for range 2 {
			w := historicalRequest(router, context.Background(), url, "42", service.RoleUser)
			require.Equal(t, 200, w.Code)
			require.Equal(t, "bypass", w.Header().Get("X-Usage-Stats-Cache"))
		}
	}
	require.Equal(t, int32(6), r.calls.Load())
}
