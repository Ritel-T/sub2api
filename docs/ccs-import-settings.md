# CC Switch provider import settings

The provider deeplink contract is pinned to [CC Switch v4.0.25](https://github.com/farion1231/cc-switch/releases/tag/v4.0.25), checked on 2026-11-04. This feature configures the API Key page's provider imports. It does not install companion MCP servers, prompts, or skills.

## Administration

Open **Settings → General → Site settings → CC Switch import profiles**. Edit a profile and save the settings page. Each profile has a unique ID, a menu label, an API key platform, a target application, and provider parameters. Profiles appear only for matching key platforms, in the administrator's order. One matching profile opens directly; multiple profiles show a choice. Removing every matching profile hides the import button for that platform. The existing global hide switch remains available.

The initial profiles cover Claude Code, Codex, Gemini CLI, the two Antigravity clients, and Grok Build. Codex's initial model is `gpt-6-astra`. Further profiles can target `claude`, `codex`, `gemini`, `agy`, `grokbuild`, `opencode`, `openclaw`, `hermes`, or `pi`.

All provider parameters supported by this integration are editable:

| Group | Parameters |
| --- | --- |
| Provider and models | `name`, `endpoint`, `homepage`, `model`, `apiFormat`, `haikuModel`, `sonnetModel`, `opusModel`, `enabled` |
| Display | `notes`, `icon`, `iconUrl` |
| Embedded configuration | `config`, `configFormat` |
| Usage | `usageEnabled`, `usageScript`, `usageAutoInterval`, `usageApiKey`, `usageBaseUrl`, `usageAccessToken`, `usageUserId` |

`resource=provider`, `app`, and `apiKey` are constructed from the selected profile and the user's clicked key. A profile cannot override that API key. Multiple endpoints are comma-separated, with the primary endpoint first. `configUrl` is rejected because CCS v4.0.25 does not implement it.

`enabled` can be omitted, true, or false. When omitted, CCS decides its initial switch/enable setting for the target app. Usage queries default to enabled with a 30-minute interval; an explicit `false` disables them, and an interval of `0` disables scheduled refreshes. The built-in script shows balances, subscription names, and finite quotas, while hiding the unlimited `-1` sentinel.

## Metadata templates and raw configuration

Metadata strings support `{{siteName}}`, `{{baseUrl}}`, `{{keyName}}`, `{{userId}}`, and `{{apiKey}}`. Values come from the current site and the clicked API key. Empty provider name, endpoint, or homepage uses the current site default.

**These are public templates distributed in public site settings. Never enter private administrator credentials or system tokens.** Use `{{apiKey}}` for a credential that should belong to the current user.

Enter `config` and `usageScript` as raw UTF-8 text. They are not interpolated by Sub2API. This preserves CCS's own script placeholders and avoids modifying JSON, TOML, or JavaScript string syntax. The builder applies UTF-8 Base64 encoding followed by URL encoding exactly once. Unicode text, plus signs, and newlines survive the round trip.

For Codex reasoning settings, select `configFormat=toml` and use, for example:

```toml
model_provider = "custom"
model_reasoning_effort = "high"

[model_providers.custom]
name = "Custom API"
wire_api = "responses"
```

The link's endpoint, API key, and model take precedence over corresponding embedded configuration values. CCS accepts provider-specific model/reasoning/window fields from its allowlist, rather than replacing the user's entire global configuration. JSON format requires an object; Codex can use `{"config":"<TOML text>"}`. TOML syntax is validated on the server. Custom scripts are stored and transported without being executed by Sub2API.

## Endpoint and format defaults

An explicit profile endpoint is kept unchanged. When omitted, the builder chooses the SDK's path convention from the effective API format and application, preserving the site's path prefix. Antigravity adds its `/antigravity` prefix once.

| Format | Target | Suffix |
| --- | --- | --- |
| `openai_chat` / `openai_responses` | All compatible targets | `/v1` |
| `anthropic` | OpenCode | `/v1` |
| `anthropic` | Other compatible targets | Root |
| `gemini_native` | OpenCode, OpenClaw, Pi | `/v1beta` |
| `gemini_native` | Gemini CLI, Antigravity CLI or Claude local routing | Root |

The builtin usage script removes either version suffix before adding `/v1/usage`. Additive applications infer the API key platform's native protocol unless an administrator chooses a format explicitly. Hermes cannot infer a native Gemini protocol and requires an explicit supported format. Pi requires a nonempty model parameter. The target application's protocol support is validated before settings are written.

## Persistence and compatibility

Settings use the existing key-value store under `ccs_import_config`; no database schema migration is required. Admin GET/PUT, public API, and injected initial settings use the same shape. Omitted fields in older admin clients preserve the saved value; `null` is rejected; `{"profiles":[]}` deliberately disables imports. Missing or malformed stored settings fall back to fresh initial profiles. Saves validate the complete CCS value before any settings are persisted, and the audit records the changed setting key without copying template values.

The application permits up to 64 profiles, 16 KiB per metadata string, 128 KiB per config/script, 512 KiB total, and a nonnegative JavaScript-safe integer for the interval. Large generated links are still subject to the user's browser and operating system URL-launch limits.

CCS v4.0.10+ retains the extended Codex/Grok configuration allowlists and custom icon URLs. Older clients still receive explicit endpoint/key/model fields, but may discard newer options. This feature changes newly generated import links; existing providers in a user's CCS installation are not updated automatically.

## Primary references

- [Pinned provider parser](https://github.com/farion1231/cc-switch/blob/v4.0.25/src-tauri/src/deeplink/parser.rs)
- [Pinned provider configuration builder](https://github.com/farion1231/cc-switch/blob/v4.0.25/src-tauri/src/deeplink/provider.rs)
- [Pinned site integration guide](https://github.com/farion1231/cc-switch/blob/v4.0.25/docs/guides/deeplink-site-integration-guide-en.md)
- [OpenCode Google SDK endpoint convention](https://github.com/vercel/ai/blob/main/packages/google/src/google-provider.ts)
- [Pi Google endpoint convention](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/api/google-generative-ai.ts)
- [OpenClaw Google endpoint convention](https://github.com/openclaw/openclaw/blob/main/src/infra/google-api-base-url.ts)

## Editing inherited defaults

The administrator editor fills inherited provider metadata, endpoint, API format, JSON format, CCS import activation defaults, inherited query key/address, usage-query enablement, 30-minute interval, and the complete built-in usage script into the controls. The endpoint follows the current site API base URL and selected protocol/application. Editing a value creates an override; clearing it restores inheritance. Viewing or saving another field does not persist these inherited values as overrides. Optional fields without defaults remain empty.

The built-in usage script currently shows subscription plan names and the smallest remaining allowance among configured daily, weekly, and monthly limits. It does not show individual subscription period progress or expiry. API keys with their own quota/rate limits use the API-key quota response instead of the subscription response; the current script reads total key quotas but does not yet read `rate_limits` windows.

## Application coverage in v4.0.25

The application picker covers all 14 CCS tools and uses its original application icons. Codex CLI and ChatGPT Desktop share the Codex configuration. Nine apps accept provider deep links: Claude Code, Codex, Gemini CLI, Antigravity CLI (`agy`), Grok Build, OpenCode, OpenClaw, Hermes and Pi. Gemini CLI and Antigravity require a Gemini Native compatible key; CCS cannot route another protocol into them.

Claude Desktop has a separate setup: import an inactive Claude Code source provider, then use CCS Desktop's official import-from-Claude workflow. MiniMax Code (`mcode`), Kimi Code, DeepSeek Harness and Oh My Pi reject website provider links in CCS v4.0.25. Their panels therefore offer copy/download of the exact native editor format and instructions to add it in CCS, rather than generating rejected links. Kimi model keys are namespaced under `ritel/`. These four clients may retain an existing global default; after saving, select the displayed recommended model in the client. Their configuration editors do not accept arbitrary global default fields.

New imports default to the live catalog's flagship text model for the key platform: OpenAI/Composite `gpt-6-astra`, Anthropic `claude-fable-5-1`, Gemini `gemini-4-argon`, Grok `grok-4.7`, DeepSeek `deepseek-v4-pro`, MiniMax `MiniMax-M3`, Kimi `kimi-k3.1-pro`, and Zhipu/OpenCode Go `glm-5.3`. Gemini Native apps select the Gemini model. Explicit administrator model overrides remain intact; blank models inherit the current site default. One app's model override no longer silently becomes the default of every other app. Existing CCS installations are unchanged until the user imports or applies the configuration.

The models were checked using read-only model discovery, without inference requests. Official model references: [OpenAI](https://developers.openai.com/api/docs/models), [Claude Fable](https://www.anthropic.com/claude/fable), [Gemini Argon](https://aistudio.google.com/docs/models/gemini-4-argon), [Grok](https://docs.x.ai/developers/models/grok-4.7). The Antigravity platform and separate national-provider platform types currently have no live groups; their defaults are template choices, not a claim of separately verified live routing.
