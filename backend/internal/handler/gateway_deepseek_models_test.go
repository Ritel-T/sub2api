package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func deepSeekDiscoveryAccount(id int64, mapping map[string]any, modalities map[string][]string) service.Account {
	a := service.Account{ID: id, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": mapping}}
	if modalities != nil {
		metadata := make(map[string]service.UpstreamModelMetadata, len(modalities))
		for model, inputs := range modalities {
			metadata[model] = service.UpstreamModelMetadata{ID: model, InputModalities: inputs, ContextWindow: 123456, MaxOutputTokens: 1234}
		}
		a.SetUpstreamModelMetadataSnapshot(service.UpstreamModelMetadataSnapshot{Models: metadata})
	}
	return a
}

func requestDeepSeekDiscovery(h *GatewayHandler, group *service.Group, path, model string, denied []string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	if model != "" {
		c.Params = gin.Params{{Key: "model", Value: model}}
	}
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group,
		User: &service.User{ID: 7, UserGroupDeniedModels: denied}})
	h.Models(c)
	return w
}

func deepSeekDiscoveryEntries(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "list", body.Object)
	require.NotNil(t, body.Data)
	return body.Data
}

func TestGatewayDeepSeekModelsCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const flash = "deepseek-v4.1-flash-1017"
	for _, tt := range []struct {
		name       string
		public     string
		targets    []string
		modalities map[string][]string
		vision     bool
	}{
		{name: "1017 without metadata", public: flash, targets: []string{flash}, vision: true},
		{name: "base without metadata", public: "deepseek-v4.1-flash", targets: []string{"deepseek-v4.1-flash"}, vision: true},
		{name: "ID-only snapshot", public: flash, targets: []string{flash}, modalities: map[string][]string{flash: nil}, vision: true},
		{name: "mapped alias", public: "flash-alias", targets: []string{flash}, vision: true},
		{name: "Pro", public: "deepseek-v4-pro", targets: []string{"deepseek-v4-pro"}},
		{name: "V4.1 Pro", public: "deepseek-v4.1-pro", targets: []string{"deepseek-v4.1-pro"}},
		{name: "old V4", public: "deepseek-v4-flash", targets: []string{"deepseek-v4-flash"}},
		{name: "unmapped generic Flash", public: "deepseek-flash", targets: []string{"deepseek-flash"}},
		{name: "unknown", public: "deepseek-unknown", targets: []string{"deepseek-unknown"}},
		{name: "unverified suffix", public: "deepseek-v4.1-flash-future", targets: []string{"deepseek-v4.1-flash-future"}},
		{name: "Flash label maps to Pro", public: flash, targets: []string{"deepseek-v4-pro"}},
		{name: "explicit text only", public: flash, targets: []string{flash}, modalities: map[string][]string{flash: {"text"}}},
		{name: "explicit vision", public: "provider-alias", targets: []string{"provider-private"}, modalities: map[string][]string{"provider-private": {"text", "image"}}, vision: true},
		{name: "mixed mapped targets", public: "flash-alias", targets: []string{flash, "deepseek-v4-pro"}},
		{name: "multiple vision accounts", public: "flash-alias", targets: []string{flash, "deepseek-v4.1-flash"}, vision: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			group := &service.Group{ID: 2717, Platform: service.PlatformDeepseek}
			accounts := make([]service.Account, 0, len(tt.targets))
			for i, target := range tt.targets {
				accounts = append(accounts, deepSeekDiscoveryAccount(int64(i+1), map[string]any{tt.public: target}, tt.modalities))
			}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{group.ID: accounts}})
			for _, path := range []string{"/models", "/v1/models"} {
				entries := deepSeekDiscoveryEntries(t, requestDeepSeekDiscovery(h, group, path, "", nil))
				require.Len(t, entries, 1)
				entry := entries[0]
				require.Len(t, entry, 7, "only add the three discovery fields; do not invent capacities or pricing")
				require.Equal(t, tt.public, entry["id"])
				require.Equal(t, "model", entry["type"])
				require.Equal(t, tt.public, entry["display_name"])
				require.Equal(t, "2024-01-01T00:00:00Z", entry["created_at"])
				want := []any{"text"}
				if tt.vision {
					want = append(want, "image")
				}
				require.Equal(t, want, entry["input_modalities"])
				require.Equal(t, []any{"text"}, entry["output_modalities"])
				require.Equal(t, map[string]any{"anthropic_messages": map[string]any{
					"system_prompt_update": "leading-only", "tool_update": "leading-only",
				}}, entry["api_capabilities"])
			}
		})
	}
}

func TestGatewayDeepSeekModelsSelectionAndRetrieve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const flash = "deepseek-v4.1-flash-1017"
	group := &service.Group{ID: 2718, Platform: service.PlatformDeepseek}
	a := deepSeekDiscoveryAccount(1, map[string]any{
		flash: flash, "flash-alias": flash, "deepseek-v4-pro": "deepseek-v4-pro", "hidden": flash,
	}, nil)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{group.ID: {a}}})
	all := requestDeepSeekDiscovery(h, group, "/models", "", nil)
	require.Equal(t, []string{"deepseek-v4-pro", flash, "flash-alias", "hidden"}, listedModelIDs(t, all.Body.Bytes()))
	group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"deepseek-v4-pro", "deepseek-v4.1-*", "flash-alias"}}
	listed := requestDeepSeekDiscovery(h, group, "/models", "", []string{"deepseek-v4-pro"})
	require.Equal(t, []string{flash, "flash-alias"}, listedModelIDs(t, listed.Body.Bytes()))
	entries := deepSeekDiscoveryEntries(t, listed)
	for _, entry := range entries {
		id, ok := entry["id"].(string)
		require.True(t, ok)
		got := requestDeepSeekDiscovery(h, group, "/v1/models/"+id, id, []string{"deepseek-v4-pro"})
		require.Equal(t, http.StatusOK, got.Code)
		var retrieved map[string]any
		require.NoError(t, json.Unmarshal(got.Body.Bytes(), &retrieved))
		require.Equal(t, entry, retrieved)
	}
	for _, id := range []string{"hidden", "deepseek-v4-pro", "missing"} {
		got := requestDeepSeekDiscovery(h, group, "/models/"+id, id, []string{"deepseek-v4-pro"})
		require.Equal(t, http.StatusNotFound, got.Code)
		require.Contains(t, got.Body.String(), `"code":"model_not_found"`)
	}
	group.ModelAllowlist.Models = []string{"missing"}
	require.Empty(t, deepSeekDiscoveryEntries(t, requestDeepSeekDiscovery(h, group, "/models", "", nil)))
}

func TestGatewayDeepSeekModelsPreservesFallbackAndOtherPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{})
	group := &service.Group{ID: 2719, Platform: service.PlatformDeepseek}
	entries := deepSeekDiscoveryEntries(t, requestDeepSeekDiscovery(h, group, "/models", "", nil))
	require.Len(t, entries, len(claude.DefaultModels))
	for i, entry := range entries {
		delete(entry, "input_modalities")
		delete(entry, "output_modalities")
		delete(entry, "api_capabilities")
		body, err := json.Marshal(claude.DefaultModels[i])
		require.NoError(t, err)
		var original map[string]any
		require.NoError(t, json.Unmarshal(body, &original))
		require.Equal(t, original, entry)
	}
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformGrok, service.PlatformComposite, service.PlatformKimi} {
		group := &service.Group{ID: 2720, Platform: platform}
		a := deepSeekDiscoveryAccount(1, map[string]any{"ordinary-model": "ordinary-model"}, nil)
		a.Platform = platform
		if platform == service.PlatformComposite {
			a.Platform = service.PlatformOpenAI
		}
		h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{group.ID: {a}}})
		actual := requestDeepSeekDiscovery(h, group, "/models", "", nil)
		baseline := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(baseline)
		writeModelsList(c, platform, []string{"ordinary-model"})
		require.JSONEq(t, baseline.Body.String(), actual.Body.String(), platform)
	}
}
