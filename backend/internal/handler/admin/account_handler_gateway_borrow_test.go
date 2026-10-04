package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type borrowTestRecoveryRepo struct {
	service.AccountRepository
	account    *service.Account
	clearCalls int
}

func (r *borrowTestRecoveryRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}
func (r *borrowTestRecoveryRepo) GetOpenAITurnAdmission(context.Context, int64) (*service.Account, *service.Account, error) {
	return r.account, nil, nil
}
func (r *borrowTestRecoveryRepo) ClearRateLimit(context.Context, int64) error {
	r.clearCalls++
	return nil
}
func (r *borrowTestRecoveryRepo) ClearModelRateLimits(context.Context, int64) error {
	r.clearCalls++
	return nil
}
func (r *borrowTestRecoveryRepo) ClearAntigravityQuotaScopes(context.Context, int64) error {
	r.clearCalls++
	return nil
}
func (r *borrowTestRecoveryRepo) ClearTempUnschedulable(context.Context, int64) error {
	r.clearCalls++
	return nil
}

type borrowTestRecoveryUpstream struct{ expiry time.Time }

func (u *borrowTestRecoveryUpstream) Do(req *http.Request, p string, id int64, n int) (*http.Response, error) {
	return u.DoWithTLS(req, p, id, n, nil)
}
func (u *borrowTestRecoveryUpstream) DoWithTLS(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"status\":\"completed\"}}\n\n"))}, nil
}
func (u *borrowTestRecoveryUpstream) CodexGatewayPinWSRequestForModel(context.Context, http.Header, string, int64, int, string) (string, string, func(), error) {
	return "fixture", "", func() {}, nil
}
func (u *borrowTestRecoveryUpstream) CodexGatewayPinWSBindingValidForModel(context.Context, int64, string, string) bool {
	return true
}
func (u *borrowTestRecoveryUpstream) CodexGatewayPinWSBindingExpiryForModel(context.Context, int64, string, string) time.Time {
	return u.expiry
}
func (u *borrowTestRecoveryUpstream) AstraGatewaySnapshot(context.Context) service.AstraGatewayRuntime {
	return service.AstraGatewayRuntime{Targets: []service.AstraRouteStatus{{AccountID: 901, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &u.expiry}}}
}
func (u *borrowTestRecoveryUpstream) SetAstraGatewayPreparer(func(context.Context, int64) error) {}
func (u *borrowTestRecoveryUpstream) PrepareAstraGateway(context.Context) error                  { return nil }

func TestSuccessfulBorrowAccountTestDoesNotClearOtherModelLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, borrowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "borrowed", false: "ordinary"}[borrowed], func(t *testing.T) {
			extra := map[string]any{"model_rate_limits": map[string]any{"gpt-6.1-sol": map[string]any{"rate_limit_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}}
			if borrowed {
				extra[service.GatewayBorrowModelsKey] = []string{"gpt-6-astra"}
			}
			account := &service.Account{ID: 901, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account"}, Extra: extra}
			repo := &borrowTestRecoveryRepo{account: account}
			upstream := &borrowTestRecoveryUpstream{expiry: time.Now().Add(time.Minute)}
			cfg := &config.Config{}
			cfg.Gateway.CodexGatewayPin = config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{902}, TargetAccountIDs: []int64{901}}
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
			testService := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, cfg, nil)
			testService.SetOpenAIGatewayService(gateway)
			h := &AccountHandler{accountTestService: testService, rateLimitService: service.NewRateLimitService(repo, nil, nil, nil, nil)}
			router := gin.New()
			router.POST("/accounts/:id/test", h.Test)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/accounts/901/test", strings.NewReader(`{"model_id":"gpt-6-astra"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Contains(t, rec.Body.String(), `"success":true`)
			if borrowed {
				require.Zero(t, repo.clearCalls, "borrowed Astra success does not prove Sol or native cooldown recovery")
			} else {
				require.Greater(t, repo.clearCalls, 0, "ordinary native tests preserve established recovery behavior")
			}
		})
	}
}
