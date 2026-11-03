//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyModelDiscoveryAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		google bool
		status int
		want   []int64
	}{
		{name: "union_without_probes_or_billing", status: 200, want: []int64{1, 2}},
		{name: "missing_primary_subscription", google: true, status: 200, want: []int64{2}},
		{name: "expired_key", status: 403},
		{name: "key_quota", google: true, status: 429},
		{name: "custom_codex_policy", status: 200, want: []int64{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			first := &service.Group{ID: 1, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformGemini, SubscriptionType: service.SubscriptionTypeSubscription, StreamOnly: true, BlockSubagents: true, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"first-only"}}}
			second := &service.Group{ID: 2, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformGemini}
			user := &service.User{ID: 7, Status: service.StatusActive, Balance: -10}
			key := &service.APIKey{ID: 8, Key: "discovery-key", Status: service.StatusActive, UserID: 7, User: user, GroupID: &first.ID, Group: first, GroupIDs: []int64{1, 2}, ConcurrencyLimit: 1}
			switch tc.name {
			case "expired_key":
				expired := time.Now().Add(-time.Minute)
				key.ExpiresAt = &expired
				cfg.RunMode = config.RunModeSimple
			case "key_quota":
				key.Quota = 1
				key.QuotaUsed = 1
			case "custom_codex_policy":
				first.CodexCLIOnly = true
				second.CodexCLIOnly = true
			}
			writes := 0
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }, updateLastUsed: func(context.Context, int64, time.Time) error { writes++; return nil }}
			subRepo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
				if tc.name == "missing_primary_subscription" {
					return nil, service.ErrSubscriptionNotFound
				}
				return &service.UserSubscription{Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour), DailyUsageUSD: 100}, nil
			}}
			keys := service.NewAPIKeyService(repo, &overflowAuthUserRepo{user: user}, &overflowMultiAuthGroupRepo{groups: map[int64]*service.Group{1: first, 2: second}}, subRepo, &overflowAuthRateRepo{}, nil, cfg)
			probe := &deepSeekToolsGroupRouterStub{}
			keys.SetGroupRouter(probe)
			subs := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
			var settings *service.SettingService
			if tc.name == "custom_codex_policy" {
				settings = service.NewSettingService(fakeSettingRepo{values: map[string]string{service.SettingKeyCodexCLIOnlyWhitelist: `[{"originator":"custom-client","ua_contains":["custom-client/"]}]`}}, cfg)
			}
			auth := gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, subs, cfg))
			path, url := "/v1/models/:model", "/v1/models/requested"
			if tc.google {
				auth = APIKeyAuthWithSubscriptionGoogle(keys, subs, cfg)
				path, url = "/v1beta/models/:model", "/v1beta/models/requested"
			}
			r := gin.New()
			r.Use(auth, GroupModelAllowlist(), GroupCodexCLIOnly(settings), GroupBlockSubagents(), GroupStreamOnly())
			called := false
			r.GET(path, func(c *gin.Context) {
				called = true
				candidates, multi := GetAPIKeyModelDiscoveryGroups(c)
				require.True(t, multi)
				ids := []int64{}
				for _, candidate := range candidates {
					ids = append(ids, *candidate.GroupID)
				}
				require.Equal(t, tc.want, ids)
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodGet, url, nil)
			request.Header.Set("Authorization", "Bearer discovery-key")
			request.Header.Set("X-OpenAI-Subagent", "worker")
			if tc.name == "custom_codex_policy" {
				request.Header.Set("User-Agent", "custom-client/1.0")
				request.Header.Set("Originator", "custom-client")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.status == 200, called)
			require.Empty(t, probe.probed)
			require.Zero(t, writes)
		})
	}
}
