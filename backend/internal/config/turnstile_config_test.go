//go:build unit

package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestTurnstileExpectedHostnamesFromEnvironment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	t.Setenv("TURNSTILE_EXPECTED_HOSTNAMES", "app.example.com,login.example.com")
	t.Setenv("TURNSTILE_STRICT_CONTEXT", "true")

	var cfg Config
	require.NoError(t, viper.Unmarshal(&cfg))
	require.Equal(t, []string{"app.example.com", "login.example.com"}, cfg.Turnstile.ExpectedHostnames)
	require.True(t, cfg.Turnstile.StrictContext)
}

func TestTurnstileStrictContextDefaultsToLegacyCompatibility(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()

	var cfg Config
	require.NoError(t, viper.Unmarshal(&cfg))
	require.False(t, cfg.Turnstile.StrictContext)
	require.Empty(t, cfg.Turnstile.ExpectedHostnames)
}
