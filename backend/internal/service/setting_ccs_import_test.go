package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCCSImportDefaultsAndStorageFallback(t *testing.T) {
	defaults := DefaultCCSImportConfig()
	require.Len(t, defaults.Profiles, 6)
	require.Equal(t, "gpt-6-astra", defaults.Profiles[1].Parameters["model"])
	for _, raw := range []string{"", "null", "{broken", `{"profiles":null}`, `{"profiles":[{"id":"x","label":"bad","platform":"unsupported","app":"codex"}]}`} {
		require.Equal(t, defaults, parseCCSImportConfigOrDefault(raw), raw)
	}
	disabled := parseCCSImportConfigOrDefault(`{"profiles":[]}`)
	require.NotNil(t, disabled.Profiles)
	require.Empty(t, disabled.Profiles)
	// Callers must not share mutable default parameter maps.
	defaults.Profiles[1].Parameters["model"] = "changed"
	require.Equal(t, "gpt-6-astra", DefaultCCSImportConfig().Profiles[1].Parameters["model"])
}

func TestCCSImportRoundTripPreservesRawTemplatesAndEmptyOverrides(t *testing.T) {
	raw := `{"profiles":[{"id":"test","label":"自定义","platform":"openai","app":"codex","parameters":{"name":"","endpoint":"","homepage":"","model":"","enabled":false,"usageEnabled":false,"usageAutoInterval":0,"usageScript":"\n{{apiKey}} 中文\n","config":"{\n  \"literal\": \"{{apiKey}}\"\n}","configFormat":"json","apiFormat":"openai_responses","notes":"{{siteName}} {{keyName}} {{userId}}"}}]}`
	var decoded CCSImportConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	normalized, err := NormalizeCCSImportConfig(decoded)
	require.NoError(t, err)
	encoded, err := json.Marshal(normalized)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(encoded))
	require.Equal(t, normalized, parseCCSImportConfigOrDefault(string(encoded)))
	require.NotContains(t, DefaultCCSImportConfig().Profiles[1].Parameters, "enabled")
}

func TestCCSImportRejectsInvalidParameters(t *testing.T) {
	valid := DefaultCCSImportConfig().Profiles[1]
	for name, params := range map[string]map[string]any{
		"credential override":         {"apiKey": "forbidden"},
		"external config":             {"configUrl": ""},
		"resource":                    {"resource": "provider"},
		"app":                         {"app": "claude"},
		"unknown":                     {"futureParam": true},
		"boolean string":              {"enabled": "true"},
		"numeric boolean":             {"usageEnabled": 1},
		"nested object":               {"notes": map[string]any{}},
		"null":                        {"model": nil},
		"interval string":             {"usageAutoInterval": "30"},
		"interval negative":           {"usageAutoInterval": -1},
		"interval fractional":         {"usageAutoInterval": 1.5},
		"interval too large":          {"usageAutoInterval": float64(9007199254740992)},
		"invalid format":              {"apiFormat": "openai"},
		"unsupported format":          {"apiFormat": "gemini_native"},
		"invalid config format":       {"configFormat": "yaml"},
		"invalid JSON config":         {"configFormat": "json", "config": "{broken"},
		"invalid default JSON":        {"config": "{broken"},
		"array JSON config":           {"config": "[]"},
		"null JSON config":            {"config": "null"},
		"primitive JSON config":       {"config": "true"},
		"invalid TOML config":         {"configFormat": "toml", "config": "[broken"},
		"precise fractional interval": {"usageAutoInterval": json.Number("9007199254740991.01")},
		"oversized config":            {"config": strings.Repeat("x", CCSImportMaxScriptBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			profile := valid
			profile.Parameters = params
			_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
			require.Error(t, err)
		})
	}
	_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{valid, valid}})
	require.Error(t, err)
	_, err = NormalizeCCSImportConfig(CCSImportConfig{})
	require.Error(t, err)
	var cfg CCSImportConfig
	require.Error(t, json.Unmarshal([]byte(`{"profiles":[],"configUrl":"https://example.com"}`), &cfg))
	require.Error(t, json.Unmarshal([]byte(`{"profiles":[{"id":"x","label":"x","platform":"openai","app":"codex","resource":"provider"}]}`), &cfg))
}

func TestCCSImportAPIFormatCompatibility(t *testing.T) {
	for app := range ccsImportApps {
		for format := range ccsImportAPIFormats {
			profile := CCSImportProfile{ID: "test", Label: "Test", Platform: "openai", App: app, Parameters: map[string]any{"apiFormat": format, "model": "required-model"}}
			_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
			allowed := (app != "gemini" || format == "gemini_native") &&
				(format != "gemini_native" || (app != "codex" && app != "grokbuild" && app != "hermes"))
			require.Equal(t, allowed, err == nil, "%s/%s: %v", app, format, err)
		}
	}
	profile := DefaultCCSImportConfig().Profiles[1]
	profile.Parameters = map[string]any{"usageAutoInterval": json.Number("9007199254740991"), "configFormat": "toml", "config": "model = \"{{apiKey}}\""}
	_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
	require.NoError(t, err)
}

func TestCCSImportPiRequiresDirectModel(t *testing.T) {
	profile := CCSImportProfile{ID: "pi", Label: "Pi", Platform: "openai", App: "pi", Parameters: map[string]any{"config": `{"model":"embedded-only"}`}}
	_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
	require.Error(t, err)
	profile.Parameters["model"] = "gpt-6-astra"
	_, err = NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
	require.NoError(t, err)
}

func TestCCSImportHermesGeminiRequiresExplicitSupportedFormat(t *testing.T) {
	for _, parameters := range []map[string]any{{}, {"apiFormat": ""}, {"apiFormat": "gemini_native"}} {
		profile := CCSImportProfile{ID: "hermes", Label: "Hermes", Platform: "gemini", App: "hermes", Parameters: parameters}
		_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
		require.Error(t, err)
	}
	profile := CCSImportProfile{ID: "hermes", Label: "Hermes", Platform: "gemini", App: "hermes", Parameters: map[string]any{"apiFormat": "openai_responses"}}
	_, err := NormalizeCCSImportConfig(CCSImportConfig{Profiles: []CCSImportProfile{profile}})
	require.NoError(t, err)
}
