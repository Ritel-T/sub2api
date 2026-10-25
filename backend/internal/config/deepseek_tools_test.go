package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLoadDeepSeekToolsDisabledByDefault(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.Gateway.DeepSeekTools.Enabled)
	require.Zero(t, cfg.Gateway.DeepSeekTools.SearchGroupID)
	require.Empty(t, cfg.Gateway.DeepSeekTools.SearchModel)
}

func TestLoadDeepSeekToolsExplicitBackendFromEnv(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_DEEPSEEK_TOOLS_ENABLED", "true")
	t.Setenv("GATEWAY_DEEPSEEK_TOOLS_SEARCH_GROUP_ID", "12")
	t.Setenv("GATEWAY_DEEPSEEK_TOOLS_SEARCH_MODEL", "gpt-6-astra")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, GatewayDeepSeekToolsConfig{Enabled: true, SearchGroupID: 12, SearchModel: "gpt-6-astra"}, cfg.Gateway.DeepSeekTools)
}
