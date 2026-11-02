package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userOverviewDayRepo struct {
	service.UsageLogRepository
	zone string
	uid  int64
}

func (r *userOverviewDayRepo) GetUserDashboardStats(ctx context.Context, uid int64) (*usagestats.UserDashboardStats, error) {
	start, _ := timezone.DayRange(ctx)
	r.zone = start.Location().String()
	r.uid = uid
	return &usagestats.UserDashboardStats{TodayActualCost: 1234.5}, nil
}
func TestUserOverviewHTTPPropagatesUserDay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userOverviewDayRepo{}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/stats", h.DashboardStats)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stats?timezone=Asia%2FShanghai", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "Asia/Shanghai", repo.zone)
	require.Equal(t, int64(42), repo.uid)
}
func TestKeyUsageDateRangeRespectsUserZoneAndDST(t *testing.T) {
	h := &GatewayHandler{}
	for _, tc := range []struct {
		zone, startUTC, endUTC string
		hours                  time.Duration
	}{{"Asia%2FShanghai", "2026-10-31T16:00:00Z", "2026-11-01T16:00:00Z", 24}, {"America%2FLos_Angeles", "2026-11-01T07:00:00Z", "2026-11-02T08:00:00Z", 25}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage?start_date=2026-11-01&end_date=2026-11-01&timezone="+tc.zone, nil)
		start, end := h.parseUsageDateRange(c)
		require.Equal(t, tc.startUTC, start.UTC().Format(time.RFC3339))
		require.Equal(t, tc.endUTC, end.UTC().Format(time.RFC3339))
		require.Equal(t, tc.hours*time.Hour, end.Sub(start))
	}
}
