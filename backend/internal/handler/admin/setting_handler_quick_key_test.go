package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSettingsQuickKeyDefaultsRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	cfg := service.QuickKeyDefaultGroups{"domestic": 817, "grok": 806}
	rec := doUpdateSettings(t, h, map[string]any{"quick_key_default_groups": cfg}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"domestic":817,"grok":806}`, repo.values[service.SettingKeyQuickKeyDefaultGroups])
	all, err := h.settingService.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, cfg, all.QuickKeyDefaultGroups)
	public, err := h.settingService.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, cfg, public.QuickKeyDefaultGroups)
	injected, err := h.settingService.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	require.Equal(t, cfg, injected.(*service.PublicSettingsInjectionPayload).QuickKeyDefaultGroups)
	saved := repo.values[service.SettingKeyQuickKeyDefaultGroups]
	rec = doUpdateSettings(t, h, map[string]any{"site_name": "unrelated"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, saved, repo.values[service.SettingKeyQuickKeyDefaultGroups])
	require.NotContains(t, repo.lastUpdates, service.SettingKeyQuickKeyDefaultGroups)
	for _, invalid := range []any{nil, map[string]any{"domestic": 0}, map[string]any{"domestic": 1.5}} {
		rec = doUpdateSettings(t, h, map[string]any{"quick_key_default_groups": invalid}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, saved, repo.values[service.SettingKeyQuickKeyDefaultGroups])
	}
	rec = doUpdateSettings(t, h, map[string]any{"quick_key_default_groups": map[string]int{}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{}`, repo.values[service.SettingKeyQuickKeyDefaultGroups])
}
