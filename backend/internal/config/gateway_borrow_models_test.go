package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGatewayBorrowAutomaticScopesAndLegacyDefaults(t *testing.T) {
	legacy := CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{1}, TargetAccountIDs: []int64{2}}
	require.NoError(t, legacy.Validate())
	require.True(t, legacy.TargetRequiresModel(2, "gpt-6-astra"))
	require.False(t, legacy.TargetRequiresModel(2, "gpt-6.1-sol"))
	auto := AstraRoutingSettings{AutoQuality: true, CookiePool: CodexGatewayPinConfig{Enabled: true, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}, TargetAccountIDs: []int64{2}, TargetModels: map[string][]string{"2": {"gpt-6.1-sol"}}}}
	resolved, err := ResolveAstraDependencies(auto)
	require.NoError(t, err, "empty healthy-source pool is valid configuration but failclosed at dispatch")
	require.True(t, resolved.CookiePool.TargetRequiresModel(2, "gpt-6-sol"))
	require.False(t, resolved.CookiePool.TargetRequiresModel(2, "gpt-6-astra"))
	auto.CookiePool.TargetModels["2"][0] = "gpt-6-astra"
	require.Equal(t, []string{"gpt-6.1-sol"}, resolved.CookiePool.TargetModels["2"], "detached configuration cannot inherit caller slice mutations")
	resolved.CookiePool.Models = []string{"gpt-6-luna"}
	require.Error(t, resolved.Validate())
	resolved.CookiePool.Models = []string{"gpt-6-astra", "gpt-6.1-sol"}
	resolved.CookiePool.SourceAccountIDs = []int64{2}
	require.Error(t, resolved.Validate(), "empty-source compatibility must not allow overlapping donor/target IDs")
}
