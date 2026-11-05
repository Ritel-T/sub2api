//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildPlatformCatalogJSONShape(t *testing.T) {
	raw, err := json.Marshal(BuildPlatformCatalog())
	require.NoError(t, err)

	type modeJSON struct {
		Mode          string            `json:"mode"`
		BaseURLs      map[string]string `json:"base_urls"`
		ProtocolRules []ProtocolRule    `json:"protocol_rules"`
	}
	type platformJSON struct {
		ID            string `json:"id"`
		DisplayName   string `json:"display_name"`
		Gateway       string `json:"gateway"`
		CNProvider    *bool  `json:"cn_provider"`
		MultiProtocol *struct {
			DefaultMode   string     `json:"default_mode"`
			Routing       string     `json:"routing"`
			ResponsesPath string     `json:"responses_path"`
			Modes         []modeJSON `json:"modes"`
		} `json:"multi_protocol"`
	}
	var decoded struct {
		Platforms           []platformJSON `json:"platforms"`
		CompositePrecedence []string       `json:"composite_precedence"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, domain.CompositePrecedencePlatformIDs(), decoded.CompositePrecedence)
	byID := map[string]platformJSON{}
	for _, p := range decoded.Platforms {
		byID[p.ID] = p
	}

	anthropic := byID[PlatformAnthropic]
	require.Equal(t, "Anthropic", anthropic.DisplayName)
	require.Equal(t, "anthropic", anthropic.Gateway)
	require.NotNil(t, anthropic.CNProvider)
	require.False(t, *anthropic.CNProvider)
	require.Nil(t, anthropic.MultiProtocol)
	require.NotContains(t, string(raw), `"multi_protocol":null`)

	deepseek := byID[PlatformDeepseek].MultiProtocol
	require.NotNil(t, deepseek)
	require.Equal(t, "by_inbound", deepseek.Routing)
	require.Equal(t, "/responses", deepseek.ResponsesPath)

	openCode := byID[PlatformOpenCodeGo].MultiProtocol
	require.NotNil(t, openCode)
	require.Equal(t, "by_model", openCode.Routing)
	require.Equal(t, AccountModeGo, openCode.DefaultMode)
	require.Len(t, openCode.Modes, 2)
	require.Equal(t, AccountModeGo, openCode.Modes[0].Mode)
	require.Equal(t, DefaultOpenCodeGoBaseURL, openCode.Modes[0].BaseURLs[APIProtocolChatCompletions])
	require.Equal(t, DefaultOpenCodeGoProtocolRules(), openCode.Modes[0].ProtocolRules)
	require.Equal(t, AccountModeZen, openCode.Modes[1].Mode)
}

// 下发的是副本：调用方修改结果不影响内置 profile。
func TestBuildPlatformCatalogReturnsCopies(t *testing.T) {
	catalog := BuildPlatformCatalog()
	for _, entry := range catalog.Platforms {
		if entry.MultiProtocol == nil {
			continue
		}
		for _, mode := range entry.MultiProtocol.Modes {
			for protocol := range mode.BaseURLs {
				mode.BaseURLs[protocol] = "mutated"
			}
			for i := range mode.ProtocolRules {
				mode.ProtocolRules[i].Protocol = "mutated"
			}
		}
	}
	require.Equal(t, DefaultOpenCodeGoBaseURL, LookupProviderProfile(PlatformOpenCodeGo).DefaultBaseURL(AccountModeGo, APIProtocolChatCompletions))
	require.NotEqual(t, "mutated", LookupProviderProfile(PlatformOpenCodeGo).Modes[AccountModeGo].ProtocolRules[0].Protocol)
}
