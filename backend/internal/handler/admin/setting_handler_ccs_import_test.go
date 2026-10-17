package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func getCCSImportSettings(t *testing.T, h *SettingHandler) service.CCSImportConfig {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var envelope struct {
		Data struct {
			Config service.CCSImportConfig `json:"ccs_import_config"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	normalized, err := service.NormalizeCCSImportConfig(envelope.Data.Config)
	require.NoError(t, err)
	return normalized
}

func requireCCSImportPublicParity(t *testing.T, h *SettingHandler, expected service.CCSImportConfig) {
	t.Helper()
	public, err := h.settingService.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, expected, public.CCSImportConfig)
	raw, err := h.settingService.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	injection, ok := raw.(*service.PublicSettingsInjectionPayload)
	require.True(t, ok)
	require.Equal(t, expected, injection.CCSImportConfig)
}

func TestSettingsCCSImportDefaultsRoundTripAndOmission(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	require.Equal(t, service.DefaultCCSImportConfig(), getCCSImportSettings(t, h))
	requireCCSImportPublicParity(t, h, service.DefaultCCSImportConfig())

	rec := doUpdateSettings(t, h, map[string]any{"site_name": "unrelated"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, repo.values, service.SettingKeyCCSImportConfig)

	cfg := service.CCSImportConfig{Profiles: []service.CCSImportProfile{{
		ID: "custom", Label: "自定义 Codex", Platform: "openai", App: "codex",
		Parameters: map[string]any{
			"model": "gpt-6-astra", "name": "", "enabled": false,
			"usageEnabled": false, "usageAutoInterval": 0, "usageScript": "\n{{apiKey}} 中文\n",
			"configFormat": "json", "config": "{\n\"model\":\"{{apiKey}}\"\n}",
		},
	}}}
	rec = doUpdateSettings(t, h, map[string]any{"ccs_import_config": cfg}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	normalized, err := service.NormalizeCCSImportConfig(cfg)
	require.NoError(t, err)
	require.Equal(t, normalized, getCCSImportSettings(t, h))
	requireCCSImportPublicParity(t, h, normalized)
	saved := repo.values[service.SettingKeyCCSImportConfig]
	var envelope struct {
		Data struct {
			Config service.CCSImportConfig `json:"ccs_import_config"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	actual, err := service.NormalizeCCSImportConfig(envelope.Data.Config)
	require.NoError(t, err)
	require.Equal(t, normalized, actual)

	rec = doUpdateSettings(t, h, map[string]any{"site_name": "other"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, saved, repo.values[service.SettingKeyCCSImportConfig])
	require.NotContains(t, repo.lastUpdates, service.SettingKeyCCSImportConfig)

	rec = doUpdateSettings(t, h, map[string]any{"ccs_import_config": map[string]any{"profiles": []any{}}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"profiles":[]}`, repo.values[service.SettingKeyCCSImportConfig])
	require.Empty(t, getCCSImportSettings(t, h).Profiles)
	requireCCSImportPublicParity(t, h, service.CCSImportConfig{Profiles: []service.CCSImportProfile{}})
}

func TestSettingsCCSImportInvalidRejectedBeforeWrite(t *testing.T) {
	valid := service.DefaultCCSImportConfig()
	initial, err := json.Marshal(valid)
	require.NoError(t, err)
	for name, invalid := range map[string]any{
		"null":                nil,
		"missing profiles":    map[string]any{},
		"null profiles":       map[string]any{"profiles": nil},
		"unknown root":        map[string]any{"profiles": []any{}, "configUrl": ""},
		"wrong profiles type": map[string]any{"profiles": "wrong"},
		"unknown parameter":   map[string]any{"profiles": []any{map[string]any{"id": "x", "label": "x", "platform": "openai", "app": "codex", "parameters": map[string]any{"configUrl": ""}}}},
		"API key override":    map[string]any{"profiles": []any{map[string]any{"id": "x", "label": "x", "platform": "openai", "app": "codex", "parameters": map[string]any{"apiKey": "forbidden"}}}},
		"wrong boolean":       map[string]any{"profiles": []any{map[string]any{"id": "x", "label": "x", "platform": "openai", "app": "codex", "parameters": map[string]any{"enabled": "true"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyCCSImportConfig: string(initial), service.SettingKeySiteName: "preserved"})
			rec := doUpdateSettings(t, h, map[string]any{"ccs_import_config": invalid, "site_name": "should not write"}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Empty(t, repo.lastUpdates)
			require.Equal(t, string(initial), repo.values[service.SettingKeyCCSImportConfig])
			require.Equal(t, "preserved", repo.values[service.SettingKeySiteName])
		})
	}
}

func TestSettingsCCSImportMalformedStorageAndAudit(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyCCSImportConfig: "{broken"})
	require.Equal(t, service.DefaultCCSImportConfig(), getCCSImportSettings(t, h))
	requireCCSImportPublicParity(t, h, service.DefaultCCSImportConfig())
	rec := doUpdateSettings(t, h, map[string]any{"site_name": "unrelated"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "{broken", repo.values[service.SettingKeyCCSImportConfig])

	before := &service.SystemSettings{CCSImportConfig: service.DefaultCCSImportConfig()}
	after := &service.SystemSettings{CCSImportConfig: service.DefaultCCSImportConfig()}
	require.NotContains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), service.SettingKeyCCSImportConfig)
	after.CCSImportConfig.Profiles[1].Parameters["model"] = "changed"
	require.Contains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), service.SettingKeyCCSImportConfig)
}
