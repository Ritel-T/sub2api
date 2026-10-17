package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	SettingKeyCCSImportConfig = "ccs_import_config"
	CCSImportMaxProfiles      = 64
	CCSImportMaxConfigBytes   = 512 * 1024
	CCSImportMaxTextBytes     = 16 * 1024
	CCSImportMaxScriptBytes   = 128 * 1024
)

var ccsImportProfileIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// CCSImportConfig is public provider-import metadata. Do not store private
// administrator credentials here: this object is returned to every visitor.
type CCSImportConfig struct {
	Profiles []CCSImportProfile `json:"profiles"`
}

type CCSImportProfile struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Platform string `json:"platform"`
	App      string `json:"app"`
	// Config and usageScript are raw plaintext, encoded by the frontend once.
	// API keys are supplied dynamically from the selected key row.
	Parameters map[string]any `json:"parameters"`
}

// Strict decoding prevents misspelled fields from silently becoming defaults
// and rejects resource/configUrl options that cannot be used by this UI.
func (c *CCSImportConfig) UnmarshalJSON(data []byte) error {
	type plain CCSImportConfig
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("CCS import config must contain one JSON object")
	}
	*c = CCSImportConfig(decoded)
	return nil
}

func DefaultCCSImportConfig() CCSImportConfig {
	return CCSImportConfig{Profiles: []CCSImportProfile{
		{ID: "anthropic-claude", Label: "Claude Code", Platform: "anthropic", App: "claude", Parameters: map[string]any{}},
		{ID: "openai-codex", Label: "Codex", Platform: "openai", App: "codex", Parameters: map[string]any{"model": "gpt-6-astra"}},
		{ID: "gemini-cli", Label: "Gemini CLI", Platform: "gemini", App: "gemini", Parameters: map[string]any{}},
		{ID: "antigravity-claude", Label: "Claude Code", Platform: "antigravity", App: "claude", Parameters: map[string]any{}},
		{ID: "antigravity-gemini", Label: "Gemini CLI", Platform: "antigravity", App: "gemini", Parameters: map[string]any{}},
		{ID: "grok-build", Label: "Grok Build", Platform: "grok", App: "grokbuild", Parameters: map[string]any{"model": "grok-4.5"}},
	}}
}

var ccsImportPlatforms = map[string]bool{
	"anthropic": true, "openai": true, "gemini": true, "antigravity": true, "grok": true,
}
var ccsImportApps = map[string]bool{
	"claude": true, "codex": true, "gemini": true, "grokbuild": true,
	"opencode": true, "openclaw": true, "hermes": true, "pi": true,
}
var ccsImportStringParameters = map[string]bool{
	"name": true, "endpoint": true, "homepage": true, "model": true,
	"apiFormat": true, "haikuModel": true, "sonnetModel": true, "opusModel": true,
	"notes": true, "icon": true, "iconUrl": true, "config": true, "configFormat": true,
	"usageScript": true, "usageApiKey": true, "usageBaseUrl": true,
	"usageAccessToken": true, "usageUserId": true,
}

// NormalizeCCSImportConfig validates public templates without evaluating code
// or resolving any placeholders. Explicit empty strings survive unchanged.
func NormalizeCCSImportConfig(cfg CCSImportConfig) (CCSImportConfig, error) {
	if cfg.Profiles == nil {
		return cfg, fmt.Errorf("profiles must be an array; use [] to disable imports")
	}
	if len(cfg.Profiles) > CCSImportMaxProfiles {
		return cfg, fmt.Errorf("at most %d import profiles are allowed", CCSImportMaxProfiles)
	}
	out := CCSImportConfig{Profiles: make([]CCSImportProfile, 0, len(cfg.Profiles))}
	seen := make(map[string]bool, len(cfg.Profiles))
	for index, profile := range cfg.Profiles {
		prefix := fmt.Sprintf("profile %d", index+1)
		if !ccsImportProfileIDPattern.MatchString(profile.ID) || seen[profile.ID] {
			return cfg, fmt.Errorf("%s has an invalid or duplicate id", prefix)
		}
		seen[profile.ID] = true
		if strings.TrimSpace(profile.Label) == "" || len(profile.Label) > 128 {
			return cfg, fmt.Errorf("%s label must contain 1-128 bytes", prefix)
		}
		if !ccsImportPlatforms[profile.Platform] {
			return cfg, fmt.Errorf("%s has an unsupported platform", prefix)
		}
		if !ccsImportApps[profile.App] {
			return cfg, fmt.Errorf("%s has an unsupported app", prefix)
		}
		if profile.Parameters == nil {
			profile.Parameters = map[string]any{}
		}
		params := make(map[string]any, len(profile.Parameters))
		for key, value := range profile.Parameters {
			switch {
			case ccsImportStringParameters[key]:
				text, ok := value.(string)
				if !ok {
					return cfg, fmt.Errorf("%s parameter %s must be a string", prefix, key)
				}
				limit := CCSImportMaxTextBytes
				if key == "config" || key == "usageScript" {
					limit = CCSImportMaxScriptBytes
				}
				if len(text) > limit {
					return cfg, fmt.Errorf("%s parameter %s exceeds %d bytes", prefix, key, limit)
				}
				params[key] = text
			case key == "enabled" || key == "usageEnabled":
				if _, ok := value.(bool); !ok {
					return cfg, fmt.Errorf("%s parameter %s must be a boolean", prefix, key)
				}
				params[key] = value
			case key == "usageAutoInterval":
				number, ok := ccsImportInterval(value)
				if !ok {
					return cfg, fmt.Errorf("%s usageAutoInterval must be a nonnegative safe integer", prefix)
				}
				params[key] = number
			default:
				return cfg, fmt.Errorf("%s contains unsupported parameter %s", prefix, key)
			}
		}
		profile.Parameters = params
		if err := validateCCSImportFormats(profile); err != nil {
			return cfg, fmt.Errorf("%s: %w", prefix, err)
		}
		out.Profiles = append(out.Profiles, profile)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return cfg, fmt.Errorf("invalid import configuration")
	}
	if len(encoded) > CCSImportMaxConfigBytes {
		return cfg, fmt.Errorf("import configuration exceeds %d bytes", CCSImportMaxConfigBytes)
	}
	return out, nil
}

func ccsImportInterval(value any) (int64, bool) {
	var number float64
	switch v := value.(type) {
	case json.Number:
		rational, ok := new(big.Rat).SetString(v.String())
		if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
			return 0, false
		}
		integer := rational.Num().Int64()
		return integer, integer >= 0 && integer <= 9007199254740991
	case float64:
		number = v
	case float32:
		number = float64(v)
	case int:
		number = float64(v)
	case int64:
		number = float64(v)
	case uint64:
		number = float64(v)
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 9007199254740991 || number != math.Trunc(number) {
		return 0, false
	}
	return int64(number), true
}

func validateCCSImportFormats(profile CCSImportProfile) error {
	if err := validateCCSImportAPIFormat(profile); err != nil {
		return err
	}
	configFormat, _ := profile.Parameters["configFormat"].(string)
	if configFormat != "" && configFormat != "json" && configFormat != "toml" {
		return fmt.Errorf("configFormat must be json or toml")
	}
	config, _ := profile.Parameters["config"].(string)
	if profile.App == "pi" {
		model, _ := profile.Parameters["model"].(string)
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("pi requires a nonempty model parameter")
		}
	}
	if config != "" {
		if configFormat == "toml" {
			var parsed map[string]any
			if err := toml.Unmarshal([]byte(config), &parsed); err != nil {
				return fmt.Errorf("config must contain valid TOML when configFormat is toml")
			}
		} else {
			var parsed map[string]json.RawMessage
			if err := json.Unmarshal([]byte(config), &parsed); err != nil || parsed == nil {
				return fmt.Errorf("config must contain a valid JSON object; configFormat defaults to json")
			}
		}
	}
	return nil
}

func parseCCSImportConfigOrDefault(raw string) CCSImportConfig {
	if strings.TrimSpace(raw) != "" && len(raw) <= CCSImportMaxConfigBytes {
		var cfg CCSImportConfig
		if json.Unmarshal([]byte(raw), &cfg) == nil {
			if normalized, err := NormalizeCCSImportConfig(cfg); err == nil {
				return normalized
			}
		}
	}
	return DefaultCCSImportConfig()
}

var ccsImportAPIFormats = map[string]bool{
	"anthropic": true, "openai_chat": true, "openai_responses": true, "gemini_native": true,
}

func validateCCSImportAPIFormat(profile CCSImportProfile) error {
	format, _ := profile.Parameters["apiFormat"].(string)
	if format == "" {
		// Additive Hermes imports infer the group platform. Gemini Native is
		// not a Hermes protocol, so an explicit supported format is required.
		if profile.App == "hermes" && profile.Platform == "gemini" {
			return fmt.Errorf("hermes with the Gemini platform requires an explicit supported apiFormat")
		}
		return nil
	}
	if !ccsImportAPIFormats[format] {
		return fmt.Errorf("unsupported apiFormat")
	}
	if profile.App == "gemini" && format != "gemini_native" {
		return fmt.Errorf("gemini CLI only supports gemini_native apiFormat")
	}
	if format == "gemini_native" && (profile.App == "codex" || profile.App == "grokbuild" || profile.App == "hermes") {
		return fmt.Errorf("%s does not support gemini_native apiFormat", profile.App)
	}
	return nil
}
