package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type weeklyQuotaAdminStub struct {
	service.AdminService // Unused methods panic: the handler may only list accounts.
	calls                int
	list                 func(context.Context, int, int, int) ([]service.Account, int64, error)
	t                    *testing.T
}

func (s *weeklyQuotaAdminStub) ListAccounts(ctx context.Context, page, size int, platform, typ, status, search string, group int64, privacy, sort, order string) ([]service.Account, int64, error) {
	s.calls++
	require.Equal(s.t, service.PlatformOpenAI, platform)
	require.Equal(s.t, service.AccountTypeOAuth, typ)
	require.Empty(s.t, status)
	require.Empty(s.t, search)
	require.Zero(s.t, group)
	require.Empty(s.t, privacy)
	require.Equal(s.t, "id", sort)
	require.Equal(s.t, "asc", order)
	require.Equal(s.t, 200, size)
	deadline, ok := ctx.Deadline()
	require.True(s.t, ok)
	require.WithinDuration(s.t, time.Now().Add(15*time.Second), deadline, time.Second)
	return s.list(ctx, page, size, s.calls)
}

func weeklyQuotaHandlerFixture(id int64, now time.Time) service.Account {
	return service.Account{ID: id, Name: "secret-account-name", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"plan_type": "plus", "email": "secret@example.test", "access_token": "secret-token"},
		Extra:       map[string]any{"codex_7d_used_percent": 50, "codex_7d_window_minutes": 10080, "codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339), "codex_7d_reset_at": now.Add(time.Hour).Format(time.RFC3339)},
	}
}

func weeklyQuotaRouter(svc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &AccountHandler{adminService: svc}
	group := r.Group("/api/v1/admin/accounts")
	group.GET("/openai-weekly-quota", h.GetOpenAIWeeklyQuota)
	group.GET("/:id", func(c *gin.Context) { c.String(http.StatusTeapot, "id route") })
	return r
}

func TestAccountHandlerOpenAIWeeklyQuotaIgnoresUIFiltersAndHidesIdentity(t *testing.T) {
	now := time.Now().UTC()
	accounts := make([]service.Account, 201)
	for i := range accounts {
		accounts[i] = weeklyQuotaHandlerFixture(int64(i+1), now)
	}
	// A non-active account must be excluded even with a valid quota snapshot.
	accounts[0].Status = "error"
	stub := &weeklyQuotaAdminStub{t: t}
	stub.list = func(_ context.Context, page, size, call int) ([]service.Account, int64, error) {
		start := (page - 1) * size
		return accounts[start:min(start+size, len(accounts))], int64(len(accounts)), nil
	}
	r := weeklyQuotaRouter(stub)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/openai-weekly-quota?platform=anthropic&type=apikey&status=disabled&search=secret&group=invalid&privacy_mode=blocked&page=999&page_size=1&sort_by=name&sort_order=desc", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, 2, stub.calls)
	var payload struct {
		Code    int                              `json:"code"`
		Message string                           `json:"message"`
		Data    service.OpenAIWeeklyQuotaSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Zero(t, payload.Code)
	require.Equal(t, "success", payload.Message)
	require.Equal(t, 200, payload.Data.IncludedAccounts)
	require.Equal(t, float64(14000), payload.Data.TotalRemaining)
	require.Equal(t, float64(28000), payload.Data.TotalWeeklyLimit)
	require.Equal(t, 1, payload.Data.Excluded.Inactive)
	require.Len(t, payload.Data.Tiers, 4)
	require.Equal(t, []string{"Plus", "Pro100", "Pro200", "Pro500"}, []string{payload.Data.Tiers[0].Label, payload.Data.Tiers[1].Label, payload.Data.Tiers[2].Label, payload.Data.Tiers[3].Label})
	require.NotNil(t, payload.Data.OldestSnapshotAt)
	require.NotNil(t, payload.Data.LatestSnapshotAt)
	for _, secret := range []string{"secret", "credentials", "access_token", "email", "account_id", "items"} {
		require.NotContains(t, rec.Body.String(), secret)
	}
}

func TestAccountHandlerOpenAIWeeklyQuotaFailsClosed(t *testing.T) {
	for _, mode := range []string{"nil service", "page failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			stub := &weeklyQuotaAdminStub{t: t}
			now := time.Now().UTC()
			stub.list = func(_ context.Context, page, size, call int) ([]service.Account, int64, error) {
				if mode == "page failure" {
					return nil, 0, errors.New("secret@example.test secret-token upstream failed")
				}
				a := weeklyQuotaHandlerFixture(1, now)
				return []service.Account{a}, 1, nil
			}
			var svc service.AdminService = stub
			if mode == "nil service" {
				svc = nil
			}
			r := weeklyQuotaRouter(svc)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/openai-weekly-quota", nil)
			if mode == "canceled" {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, fmt.Sprint(mode, rec.Body.String()))
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.NotContains(t, body, "data")
			require.NotContains(t, rec.Body.String(), "secret")
			require.NotContains(t, rec.Body.String(), "total_remaining")
			if mode == "nil service" || mode == "canceled" {
				require.Zero(t, stub.calls)
			}
		})
	}
}
