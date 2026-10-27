import type { GroupPlatform } from '@/types'
import type { CcsImportApp, CcsImportConfig, CcsImportProfile } from '@/types/ccswitch'

export type CcSwitchApplicationProfile = Omit<CcsImportProfile, 'platform'> & { platform: GroupPlatform }

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-6-astra'
export const GROK_CC_SWITCH_MODEL = 'grok-4.5'

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType?: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript?: string
  keyName?: string
  userId?: string | number
  settings?: CcsImportConfig | null
  profile?: CcSwitchApplicationProfile
}

/** The provider import contract was checked against CC Switch v4.0.14. */
export const CC_SWITCH_SUPPORTED_APPS = [
  'claude', 'codex', 'gemini', 'grokbuild', 'opencode', 'openclaw', 'hermes', 'pi'
] as const

export function createDefaultCcSwitchImportConfig(): CcsImportConfig {
  return {
    profiles: [
      { id: 'anthropic-claude', label: 'Claude Code', platform: 'anthropic', app: 'claude', parameters: {} },
      { id: 'openai-codex', label: 'Codex', platform: 'openai', app: 'codex', parameters: { model: OPENAI_CC_SWITCH_CODEX_MODEL } },
      { id: 'gemini-cli', label: 'Gemini CLI', platform: 'gemini', app: 'gemini', parameters: {} },
      { id: 'antigravity-claude', label: 'Claude Code', platform: 'antigravity', app: 'claude', parameters: {} },
      { id: 'antigravity-gemini', label: 'Gemini CLI', platform: 'antigravity', app: 'gemini', parameters: {} },
      { id: 'grok-build', label: 'Grok Build', platform: 'grok', app: 'grokbuild', parameters: { model: GROK_CC_SWITCH_MODEL } }
    ]
  }
}

export function getCcSwitchImportProfiles(
  platform?: GroupPlatform | null,
  settings?: CcsImportConfig | null
): CcsImportProfile[] {
  const profiles = Array.isArray(settings?.profiles)
    ? settings.profiles
    : createDefaultCcSwitchImportConfig().profiles
  return profiles.filter(profile =>
    profile?.platform === (platform || 'anthropic') && CC_SWITCH_SUPPORTED_APPS.includes(profile.app)
  )
}

/** User-facing app selection is independent of the key's platform templates. */
export function getCcSwitchApplicationProfiles(
  platform: GroupPlatform,
  settings?: CcsImportConfig | null
): CcSwitchApplicationProfile[] {
  // SystemOne/TypeSafe is a dedicated non-LLM API, not an agent provider.
  if (platform === 'typesafe') return []
  if (Array.isArray(settings?.profiles) && settings.profiles.length === 0) return []
  const templates = getCcSwitchImportProfiles(platform, settings)
  const preferredModels: Partial<Record<GroupPlatform, string>> = {
    openai: OPENAI_CC_SWITCH_CODEX_MODEL, anthropic: 'claude-sonnet-4-6',
    antigravity: 'claude-sonnet-4-6', gemini: 'gemini-2.5-pro', grok: GROK_CC_SWITCH_MODEL,
    deepseek: 'deepseek-v4-pro', minimax: 'MiniMax-M3', kimi: 'kimi-k2.5',
    zhipu: 'glm-4.7', opencode_go: 'glm-5.3', composite: OPENAI_CC_SWITCH_CODEX_MODEL
  }
  const platformModel = templates.find(item => item.parameters.model)?.parameters.model || preferredModels[platform]
  const apps: { app: CcsImportApp; label: string }[] = [
    { app: 'codex', label: 'Codex' }, { app: 'claude', label: 'Claude Code' },
    { app: 'opencode', label: 'OpenCode' }, { app: 'openclaw', label: 'OpenClaw' },
    { app: 'gemini', label: 'Gemini CLI' }, { app: 'grokbuild', label: 'Grok Build' },
    { app: 'hermes', label: 'Hermes' }, { app: 'pi', label: 'Pi' }
  ]
  return apps.flatMap(({ app, label }) => {
    const model = app === 'gemini' && (platform === 'antigravity' || platform === 'composite')
      ? 'gemini-2.5-pro' : platformModel
    const profile: CcSwitchApplicationProfile = {
      id: `${platform}-${app}`, label, platform, app, parameters: model ? { model } : {}
    }
    // Gemini CLI cannot consume a converted protocol. Its availability is shown
    // explicitly in the UI. Other clients use CCS routing or a native SDK.
    let apiFormat = platform === 'gemini' ? 'gemini_native'
      : platform === 'anthropic' || platform === 'antigravity' ? 'anthropic'
        : platform === 'openai' || platform === 'composite' ? 'openai_responses' : 'openai_chat'
    if (app === 'gemini') apiFormat = 'gemini_native'
    if (apiFormat === 'gemini_native' && ['codex', 'grokbuild', 'hermes'].includes(app)) apiFormat = 'openai_responses'
    profile.parameters.apiFormat = apiFormat
    if (app === 'claude' && model && platform !== 'anthropic' && platform !== 'antigravity') {
      profile.parameters.haikuModel = model
      profile.parameters.sonnetModel = model
      profile.parameters.opusModel = model
    }
    const configured = templates.filter(item => item.app === app)
    if (configured.length) return configured.map(item => ({
      ...item, parameters: {
        ...profile.parameters,
        ...(app === 'claude' && item.parameters.model && platform !== 'anthropic' && platform !== 'antigravity'
          ? { haikuModel: item.parameters.model, sonnetModel: item.parameters.model, opusModel: item.parameters.model } : {}),
        ...item.parameters
      }
    }))
    return [profile]
  })
}

/**
 * Balance query CC Switch runs against the imported provider. CC Switch fills
 * `{{baseUrl}}` with the provider's base URL as stored. Users
 * may edit it either way afterwards — then evaluates the script, so the URL
 * strips an existing `/v1` instead of blindly appending one (`/v1/v1/usage`
 * is a 404 and CC Switch shows "query failed").
 */
export const CC_SWITCH_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1(?:beta)?$/, "") + "/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const quota = response?.quota;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      const result = {
        isValid: response?.isValid ?? response?.is_active ?? true,
        unit
      };
      if (typeof remaining === "number" && remaining >= 0) result.remaining = remaining;
      if (typeof response?.planName === "string") result.planName = response.planName;
      if (quota?.limit > 0) { result.used = quota.used; result.total = quota.limit; }
      if (!result.isValid) result.invalidMessage = String(response?.status || "Query failed");
      return result;
    }
  })`

function withV1Endpoint(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, '')
  return normalizedBaseUrl.endsWith('/v1') ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`
}

export function resolveCcSwitchImportConfig(
  platform: GroupPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string
): CcSwitchImportConfig {
  switch (platform || 'anthropic') {
    case 'antigravity':
      return {
        app: clientType === 'gemini' ? 'gemini' : 'claude',
        endpoint: `${baseUrl.replace(/\/+$/, '')}/antigravity`
      }
    case 'openai':
      return {
        app: 'codex',
        // CC Switch v4.0.14 writes the endpoint directly to Codex base_url.
        endpoint: withV1Endpoint(baseUrl),
        model: OPENAI_CC_SWITCH_CODEX_MODEL
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl
      }
    case 'grok':
      return {
        app: 'grokbuild',
        endpoint: withV1Endpoint(baseUrl),
        model: GROK_CC_SWITCH_MODEL
      }
    default:
      return {
        app: 'claude',
        endpoint: baseUrl
      }
  }
}

// This is deliberately an allowlist: resource/app/key are controlled by the
// selected profile and key, and configUrl is rejected by CC Switch itself.
const PROVIDER_PARAMETERS = new Set([
  'name', 'endpoint', 'homepage', 'model', 'apiFormat', 'haikuModel', 'sonnetModel',
  'opusModel', 'notes', 'icon', 'iconUrl', 'enabled', 'config', 'configFormat',
  'usageScript', 'usageEnabled', 'usageApiKey', 'usageBaseUrl', 'usageAccessToken',
  'usageUserId', 'usageAutoInterval'
])

function encodeUtf8Base64(value: string): string {
  const bytes = new TextEncoder().encode(value)
  // Avoid spreading large scripts into a function call (argument-count limit).
  return btoa(Array.from(bytes, byte => String.fromCharCode(byte)).join(''))
}

function defaultProfileEndpoint(input: Pick<CcSwitchImportDeeplinkInput, 'baseUrl' | 'platform'>, profile: CcSwitchApplicationProfile, apiFormat: string): string {
  let root = input.baseUrl.replace(/\/+$/, '').replace(/\/v1(?:beta)?$/, '')
  if (input.platform === 'antigravity' && !root.endsWith('/antigravity')) root += '/antigravity'
  if (apiFormat === 'openai_chat' || apiFormat === 'openai_responses') return `${root}/v1`
  if (apiFormat === 'anthropic' && profile.app === 'opencode') return `${root}/v1`
  if (apiFormat === 'gemini_native' && ['opencode', 'pi', 'openclaw'].includes(profile.app)) return `${root}/v1beta`
  return root
}

const DEFAULT_IMPORT_PARAMETERS = {
  name: '{{siteName}}',
  homepage: '{{baseUrl}}',
  configFormat: 'json',
  usageEnabled: true,
  usageScript: CC_SWITCH_USAGE_SCRIPT,
  usageAutoInterval: 30,
}

function defaultProfileApiFormat(profile: CcSwitchApplicationProfile): string {
  if (['opencode', 'openclaw', 'hermes', 'pi'].includes(profile.app)) {
    return profile.platform === 'gemini' ? 'gemini_native'
      : profile.platform === 'anthropic' || profile.platform === 'antigravity' ? 'anthropic'
        : profile.platform === 'grok' ? 'openai_chat' : 'openai_responses'
  }
  return profile.app === 'claude' ? 'anthropic' : profile.app === 'gemini' ? 'gemini_native' : 'openai_responses'
}

/** Current inherited values for the editor; viewing them must not create overrides. */
export function getCcSwitchImportParameterDefaults(profile: CcsImportProfile, baseUrl: string): Record<string, string | boolean | number> {
  const apiFormat = String(profile.parameters.apiFormat || defaultProfileApiFormat(profile))
  const defaults: Record<string, string | boolean | number> = {
    ...DEFAULT_IMPORT_PARAMETERS,
    endpoint: defaultProfileEndpoint({ baseUrl, platform: profile.platform }, profile, apiFormat),
    enabled: ['opencode', 'openclaw', 'hermes', 'pi'].includes(profile.app),
    usageApiKey: '{{apiKey}}',
  }
  // CCS inherits its provider's current first endpoint when no query URL is set.
  defaults.usageBaseUrl = String(profile.parameters.endpoint || defaults.endpoint).split(',')[0]!.trim().replace(/\/+$/, '')
  // Gemini / Hermes requires an explicit compatible format, not an implicit native default.
  if (!(profile.app === 'hermes' && apiFormat === 'gemini_native')) defaults.apiFormat = apiFormat
  return defaults
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const profiles = getCcSwitchImportProfiles(input.platform, input.settings)
  const profile = input.profile || profiles.find(item => item.app === input.clientType) || profiles[0]
  if (!profile || !CC_SWITCH_SUPPORTED_APPS.includes(profile.app) || profile.platform !== (input.platform || 'anthropic')) {
    throw new Error('No CC Switch import profile is available for this platform')
  }

  const variables: Record<string, string> = {
    apiKey: input.apiKey,
    baseUrl: input.baseUrl,
    siteName: input.providerName,
    keyName: input.keyName || '',
    userId: input.userId == null ? '' : String(input.userId)
  }
  const substitute = (value: string) => value.replace(
    /\{\{(apiKey|baseUrl|siteName|keyName|userId)\}\}/g,
    (_, key: string) => variables[key] || ''
  )
  const parameters: Record<string, string | boolean | number> = {
    ...DEFAULT_IMPORT_PARAMETERS,
    name: input.providerName,
    homepage: input.baseUrl,
    usageScript: input.usageScript ?? DEFAULT_IMPORT_PARAMETERS.usageScript,
    ...profile.parameters
  }
  // Additive clients default to OpenAI Chat in CCS. Select the platform's
  // native format unless the administrator explicitly chose another one.
  if (!profile.parameters.apiFormat && ['opencode', 'openclaw', 'hermes', 'pi'].includes(profile.app)) {
    parameters.apiFormat = defaultProfileApiFormat(profile)
  }
  if (profile.app === 'hermes' && parameters.apiFormat === 'gemini_native') {
    throw new Error('Hermes requires an explicit supported API format for Gemini import profiles')
  }
  const defaultEndpoint = defaultProfileEndpoint(input, profile, String(parameters.apiFormat || defaultProfileApiFormat(profile)))
  if (!parameters.endpoint) parameters.endpoint = defaultEndpoint
  const result = new URLSearchParams({ resource: 'provider', app: profile.app })
  for (const [key, value] of Object.entries(parameters)) {
    if (!PROVIDER_PARAMETERS.has(key)) continue
    if (key === 'config' || key === 'usageScript') {
      // Preserve CC Switch's placeholders and all JSON/TOML syntax verbatim.
      if (typeof value === 'string' && value.length > 0) result.set(key, encodeUtf8Base64(value))
    } else if (typeof value === 'string') {
      const resolved = substitute(value)
      if (resolved) result.set(key, resolved)
    } else if (typeof value === 'boolean' || (typeof value === 'number' && Number.isFinite(value))) {
      result.set(key, String(value))
    }
  }
  // Empty metadata fields mean "use the site's current value", not a broken link.
  if (!result.get('name')) result.set('name', input.providerName)
  if (!result.get('endpoint')) result.set('endpoint', defaultEndpoint)
  if (!result.get('homepage')) result.set('homepage', input.baseUrl)
  // No administrator setting can replace the key chosen by this user.
  result.set('apiKey', input.apiKey)
  return `ccswitch://v1/import?${result.toString()}`
}
