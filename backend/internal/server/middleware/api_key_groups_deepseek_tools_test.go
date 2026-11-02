//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deepSeekToolsGroupRouterStub struct {
	available map[int64]bool
	models    map[int64]string
	probed    []int64
}

func (r *deepSeekToolsGroupRouterStub) ProbeAPIKeyGroup(_ context.Context, group *service.Group, model, kind string) (service.APIKeyGroupAvailability, error) {
	r.probed = append(r.probed, group.ID)
	available := r.available[group.ID]
	if required, ok := r.models[group.ID]; ok && model != required {
		available = false
	}
	return service.APIKeyGroupAvailability{Available: available}, nil
}

func TestSelectAPIKeyRequestGroupDeepSeekTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"search", "fetch"} {
		for _, test := range []struct {
			name          string
			openAIRate    float64
			deepseekRate  float64
			unusableGroup string
		}{
			{name: "openai_cheaper", openAIRate: 0.1, deepseekRate: 1},
			{name: "deepseek_cheaper", openAIRate: 1, deepseekRate: 0.1},
			{name: "missing", openAIRate: 0.1, deepseekRate: 1, unusableGroup: "missing"},
			{name: "disabled", openAIRate: 0.1, deepseekRate: 1, unusableGroup: "disabled"},
			{name: "unentitled", openAIRate: 0.1, deepseekRate: 1, unusableGroup: "unentitled"},
			{name: "missing_subscription", openAIRate: 0.1, deepseekRate: 1, unusableGroup: "missing_subscription"},
			{name: "unavailable", openAIRate: 0.1, deepseekRate: 1, unusableGroup: "unavailable"},
		} {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeStandard}
				primary := &service.Group{ID: 12, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: test.openAIRate}
				deepseek := &service.Group{ID: 17, Platform: service.PlatformDeepseek, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: test.deepseekRate}
				groups := &overflowMultiAuthGroupRepo{groups: map[int64]*service.Group{12: primary, 17: deepseek}}
				user := &service.User{ID: 1, Status: service.StatusActive, Balance: 10}
				key := &service.APIKey{ID: 2, UserID: user.ID, Key: "test-only", Status: service.StatusActive, User: user, Group: primary, GroupID: &primary.ID, GroupIDs: []int64{12, 17}, ConcurrencyLimit: 3, Quota: 50, QuotaUsed: 2}
				router := &deepSeekToolsGroupRouterStub{available: map[int64]bool{12: true, 17: true}}
				subRepo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
					return nil, service.ErrSubscriptionNotFound
				}}
				switch test.unusableGroup {
				case "missing":
					delete(groups.groups, 17)
				case "disabled":
					deepseek.Status = service.StatusDisabled
				case "unentitled":
					deepseek.IsExclusive = true
				case "missing_subscription":
					deepseek.SubscriptionType = service.SubscriptionTypeSubscription
				case "unavailable":
					router.available[17] = false
				}
				keys := service.NewAPIKeyService(nil, &overflowAuthUserRepo{user: user}, groups, subRepo, &overflowAuthRateRepo{}, nil, cfg)
				keys.SetGroupRouter(router)
				body := `{"query":"example", "count":3}`
				if operation == "fetch" {
					body = `{"url":"https://example.com/page"}`
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/"+operation, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				selected, err := selectAPIKeyRequestGroup(c, keys, nil, cfg, key, false)
				if test.unusableGroup == "" {
					require.NoError(t, err)
					require.False(t, c.IsAborted())
					require.Equal(t, int64(17), *selected.GroupID)
					require.Same(t, deepseek, selected.Group)
					require.Equal(t, []int64{17}, router.probed, "tool selection must never probe the OpenAI caller group")
					want := *key
					want.GroupID, want.Group = selected.GroupID, deepseek
					require.Equal(t, want, *selected, "only the request group changes; caller identity, bindings and quota remain intact")
				} else {
					require.Error(t, err)
					require.Nil(t, selected)
					require.True(t, c.IsAborted())
					status := http.StatusForbidden
					if test.unusableGroup == "unavailable" {
						status = http.StatusServiceUnavailable
					}
					require.Equal(t, status, recorder.Code, recorder.Body.String())
					require.NotContains(t, router.probed, int64(12))
				}
				remaining, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(remaining))
				require.Equal(t, int64(12), *key.GroupID)
				require.Same(t, primary, key.Group)
				require.Equal(t, []int64{12, 17}, key.GroupIDs)
			})
		}
	}
}

func TestSelectAPIKeyRequestGroupDeepSeekToolsKeepsOtherRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, path, body string
		groupID          int64
		singleGroup      bool
	}{
		{name: "openai_model", path: "/v1/responses", body: `{"model":"gpt-test"}`, groupID: 12},
		{name: "deepseek_model", path: "/v1/chat/completions", body: `{"model":"deepseek-test"}`, groupID: 17},
		{name: "search_suffix", path: "/v1/tools/search/", body: `{"query":"example"}`, groupID: 12},
		{name: "fetch_suffix", path: "/v1/tools/fetch-extra", body: `{"url":"https://example.com"}`, groupID: 12},
		{name: "single_group_search", path: "/v1/tools/search", body: `{"query":"example"}`, groupID: 12, singleGroup: true},
		{name: "single_group_fetch", path: "/v1/tools/fetch", body: `{"url":"https://example.com"}`, groupID: 12, singleGroup: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			primary := &service.Group{ID: 12, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 0.1}
			deepseek := &service.Group{ID: 17, Platform: service.PlatformDeepseek, Status: service.StatusActive, RateMultiplier: 1}
			user := &service.User{ID: 1, Balance: 10}
			key := &service.APIKey{ID: 2, UserID: user.ID, User: user, Group: primary, GroupID: &primary.ID, GroupIDs: []int64{12, 17}}
			if test.singleGroup {
				key.GroupIDs = []int64{12}
			}
			keys := service.NewAPIKeyService(nil, &overflowAuthUserRepo{user: user}, &overflowMultiAuthGroupRepo{groups: map[int64]*service.Group{12: primary, 17: deepseek}}, nil, &overflowAuthRateRepo{}, nil, cfg)
			router := &deepSeekToolsGroupRouterStub{available: map[int64]bool{12: true, 17: true}}
			if strings.Contains(test.body, `"model"`) {
				router.models = map[int64]string{12: "gpt-test", 17: "deepseek-test"}
			}
			keys.SetGroupRouter(router)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			selected, err := selectAPIKeyRequestGroup(c, keys, nil, cfg, key, false)
			require.NoError(t, err)
			require.False(t, c.IsAborted())
			require.Equal(t, test.groupID, *selected.GroupID)
			if test.singleGroup {
				require.Same(t, key, selected, "single-group platform rejection stays with the existing handler")
				require.Empty(t, router.probed)
			}
			remaining, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, test.body, string(remaining))
		})
	}
}
