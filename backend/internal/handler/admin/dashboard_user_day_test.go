package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type overviewDayRepo struct {
	service.UsageLogRepository
	calls []string
}

func (r *overviewDayRepo) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	start, _ := timezone.DayRange(ctx)
	name := start.Location().String()
	r.calls = append(r.calls, name)
	cost := 12.34
	if name == "Asia/Shanghai" {
		cost = 1234.5
	}
	return &usagestats.DashboardStats{TodayActualCost: cost, TotalActualCost: 9999.5}, nil
}
func TestOverviewStatsHTTPUsesRequestedDayAndSeparatesSnapshotCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dashboardSnapshotV2Cache = newSnapshotCache(30 * time.Second)
	repo := &overviewDayRepo{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/stats", h.GetStats)
	router.GET("/snapshot", h.GetSnapshotV2)
	for _, path := range []string{"/stats", "/snapshot?start_date=2026-11-01T00:00:00Z&end_date=2026-11-02T00:00:00Z&include_trend=false&include_model_stats=false"} {
		for _, tc := range []struct {
			zone string
			want float64
		}{{"Asia%2FShanghai", 1234.5}, {"America%2FLos_Angeles", 12.34}, {"Asia%2FShanghai", 1234.5}} {
			separator := "?"
			if path != "/stats" {
				separator = "&"
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+separator+"timezone="+tc.zone, nil))
			require.Equal(t, 200, w.Code, w.Body.String())
			var response struct {
				Data json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			var stats map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(response.Data, &stats))
			data := response.Data
			if path != "/stats" {
				data = stats["stats"]
			}
			var costs struct {
				Today float64 `json:"today_actual_cost"`
				Total float64 `json:"total_actual_cost"`
			}
			require.NoError(t, json.Unmarshal(data, &costs))
			require.Equal(t, tc.want, costs.Today)
			require.Equal(t, 9999.5, costs.Total)
		}
	}
	require.Equal(t, []string{"Asia/Shanghai", "America/Los_Angeles", "Asia/Shanghai", "Asia/Shanghai", "America/Los_Angeles"}, repo.calls)
}
