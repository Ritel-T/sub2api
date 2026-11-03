//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderProfile_ProfilesMatchMultiProtocolPlatforms(t *testing.T) {
	t.Parallel()

	for _, platform := range []string{
		PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformComposite, "",
	} {
		want := IsCNProvider(platform) || platform == PlatformOpenCodeGo
		require.Equal(t, want, IsMultiProtocolAPIKeyProvider(platform), platform)
		require.Equal(t, want, LookupProviderProfile(platform) != nil, platform)
	}

	for platform, profile := range providerProfiles {
		require.Equal(t, platform, profile.Platform)
		_, ok := profile.Modes[profile.DefaultMode]
		require.True(t, ok, "%s default mode %q must be registered", platform, profile.DefaultMode)
	}
}

func TestProviderProfile_BaseURLKeysAreNativeProtocols(t *testing.T) {
	t.Parallel()

	for platform, profile := range providerProfiles {
		for mode, endpoints := range profile.Modes {
			require.NotEmpty(t, endpoints.BaseURLs[APIProtocolChatCompletions], "%s/%s must have a chat_completions base", platform, mode)
			for protocol, baseURL := range endpoints.BaseURLs {
				require.True(t, isNativeUpstreamProtocol(protocol), "%s/%s has unknown protocol key %q", platform, mode, protocol)
				require.NotEmpty(t, baseURL, "%s/%s/%s", platform, mode, protocol)
			}
		}
	}
}
