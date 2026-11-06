import type { GroupPlatform } from '@/types'
import type { CcsImportApp, CcsImportConfig, CcsImportProfile } from '@/types/ccswitch'

export type CcSwitchApplicationProfile = Omit<CcsImportProfile, 'platform'> & { platform: GroupPlatform }

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-6-astra'
export const GROK_CC_SWITCH_MODEL = 'grok-4.7'
export const CLAUDE_CC_SWITCH_MODEL = 'claude-fable-5-1'
export const GEMINI_CC_SWITCH_MODEL = 'gemini-4-argon'

/** Flagship text models checked against the live provider catalog on 2026-11-04. */
export function getCcSwitchDefaultModel(platform: GroupPlatform, app?: CcsImportApp): string {
  if (app === 'gemini' || app === 'agy') return GEMINI_CC_SWITCH_MODEL
  const models: Partial<Record<GroupPlatform, string>> = {
    openai: OPENAI_CC_SWITCH_CODEX_MODEL, composite: OPENAI_CC_SWITCH_CODEX_MODEL,
    anthropic: CLAUDE_CC_SWITCH_MODEL, antigravity: CLAUDE_CC_SWITCH_MODEL,
    gemini: GEMINI_CC_SWITCH_MODEL, grok: GROK_CC_SWITCH_MODEL,
    deepseek: 'deepseek-v4.1-flash', minimax: 'minimax-m3.1-flash', kimi: 'kimi-k3.1-pro',
    zhipu: 'glm-5.3', opencode_go: 'glm-5.3'
  }
  return models[platform] || ''
}

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

/** Provider import contract: CC Switch v4.0.26 (f621d4216189ef06bcc6acd3588c84937b9794e1). */
export const CC_SWITCH_SUPPORTED_APPS = [
  'claude', 'codex', 'gemini', 'agy', 'grokbuild', 'opencode', 'openclaw', 'hermes', 'pi',
  'claude-desktop', 'mcode', 'kimi', 'dsh', 'omp'
] as const

export function createDefaultCcSwitchImportConfig(): CcsImportConfig {
  return {
    profiles: [
      { id: 'anthropic-claude', label: 'Claude Code', platform: 'anthropic', app: 'claude', parameters: { model: CLAUDE_CC_SWITCH_MODEL } },
      { id: 'openai-codex', label: 'Codex', platform: 'openai', app: 'codex', parameters: { model: OPENAI_CC_SWITCH_CODEX_MODEL } },
      { id: 'gemini-cli', label: 'Gemini CLI', platform: 'gemini', app: 'gemini', parameters: { model: GEMINI_CC_SWITCH_MODEL } },
      { id: 'antigravity-claude', label: 'Claude Code', platform: 'antigravity', app: 'claude', parameters: { model: CLAUDE_CC_SWITCH_MODEL } },
      { id: 'antigravity-gemini', label: 'Gemini CLI', platform: 'antigravity', app: 'gemini', parameters: { model: GEMINI_CC_SWITCH_MODEL } },
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
  const apps: { app: CcsImportApp; label: string }[] = [
    { app: 'codex', label: 'Codex' }, { app: 'claude', label: 'Claude Code' },
    { app: 'opencode', label: 'OpenCode' }, { app: 'openclaw', label: 'OpenClaw' },
    { app: 'gemini', label: 'Gemini CLI' }, { app: 'agy', label: 'Antigravity CLI' }, { app: 'grokbuild', label: 'Grok Build' },
    { app: 'hermes', label: 'Hermes' }, { app: 'pi', label: 'Pi' },
    { app: 'claude-desktop', label: 'Claude Desktop' }, { app: 'mcode', label: 'MiniMax Code' },
    { app: 'kimi', label: 'Kimi Code' }, { app: 'dsh', label: 'DeepSeek Harness' }, { app: 'omp', label: 'Oh My Pi' }
  ]
  return apps.flatMap(({ app, label }) => {
    const model = getCcSwitchDefaultModel(platform, app)
    const profile: CcSwitchApplicationProfile = {
      id: `${platform}-${app}`, label, platform, app, parameters: model ? { model } : {}
    }
    // Gemini CLI cannot consume a converted protocol. Its availability is shown
    // explicitly in the UI. Other clients use CCS routing or a native SDK.
    let apiFormat = platform === 'gemini' ? 'gemini_native'
      : platform === 'anthropic' || platform === 'antigravity' ? 'anthropic'
        : platform === 'openai' || platform === 'composite' ? 'openai_responses' : 'openai_chat'
    if (app === 'gemini' || app === 'agy') apiFormat = 'gemini_native'
    if (apiFormat === 'gemini_native' && ['codex', 'grokbuild', 'hermes', 'mcode', 'dsh'].includes(app)) apiFormat = 'openai_responses'
    profile.parameters.apiFormat = apiFormat
    if (['claude', 'claude-desktop'].includes(app) && model && platform !== 'anthropic' && platform !== 'antigravity') {
      profile.parameters.haikuModel = model
      profile.parameters.sonnetModel = model
      profile.parameters.opusModel = model
    }
    const configured = templates.filter(item => item.app === app)
    if (configured.length) return configured.map(item => ({
      ...item, parameters: {
        ...profile.parameters,
        ...(['claude', 'claude-desktop'].includes(app) && item.parameters.model && platform !== 'anthropic' && platform !== 'antigravity'
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
  if (apiFormat === 'gemini_native' && ['opencode', 'pi', 'openclaw', 'kimi', 'omp'].includes(profile.app)) return `${root}/v1beta`
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
  if (['opencode', 'openclaw', 'hermes', 'pi', 'mcode', 'kimi', 'dsh', 'omp'].includes(profile.app)) {
    return profile.platform === 'gemini' ? (['mcode', 'dsh'].includes(profile.app) ? 'openai_responses' : 'gemini_native')
      : profile.platform === 'anthropic' || profile.platform === 'antigravity' ? 'anthropic'
        : profile.platform === 'grok' ? 'openai_chat' : 'openai_responses'
  }
  return ['claude', 'claude-desktop'].includes(profile.app) ? 'anthropic' : profile.app === 'gemini' || profile.app === 'agy' ? 'gemini_native' : 'openai_responses'
}

/** Current inherited values for the editor; viewing them must not create overrides. */
export function getCcSwitchImportParameterDefaults(profile: CcsImportProfile, baseUrl: string): Record<string, string | boolean | number> {
  const apiFormat = String(profile.parameters.apiFormat || defaultProfileApiFormat(profile))
  const defaults: Record<string, string | boolean | number> = {
    ...DEFAULT_IMPORT_PARAMETERS,
    model: getCcSwitchDefaultModel(profile.platform, profile.app),
    endpoint: defaultProfileEndpoint({ baseUrl, platform: profile.platform }, profile, apiFormat),
    enabled: ['opencode', 'openclaw', 'hermes', 'pi', 'mcode', 'kimi', 'dsh', 'omp'].includes(profile.app),
    usageApiKey: '{{apiKey}}',
  }
  // CCS inherits its provider's current first endpoint when no query URL is set.
  defaults.usageBaseUrl = String(profile.parameters.endpoint || defaults.endpoint).split(',')[0]!.trim().replace(/\/+$/, '')
  // Gemini / Hermes requires an explicit compatible format, not an implicit native default.
  if (!(profile.app === 'hermes' && apiFormat === 'gemini_native')) defaults.apiFormat = apiFormat
  return defaults
}

export function isCcSwitchOmpKeyReference(apiKey: string): boolean {
  return apiKey.trimStart().startsWith('!') || /^[A-Z_][A-Z0-9_]*$/.test(apiKey)
    || apiKey.includes('${') || /\{(?:env|file):/.test(apiKey)
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const profiles = getCcSwitchImportProfiles(input.platform, input.settings)
  const profile = input.profile || profiles.find(item => item.app === input.clientType) || profiles[0]
  if (!profile || !CC_SWITCH_SUPPORTED_APPS.includes(profile.app) || profile.platform !== (input.platform || 'anthropic')) {
    throw new Error('No CC Switch import profile is available for this platform')
  }

  if (profile.app === 'omp' && isCcSwitchOmpKeyReference(input.apiKey)) {
    throw new Error('Oh My Pi requires a literal API key; enter reference-shaped keys manually in CC Switch')
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
    model: getCcSwitchDefaultModel(profile.platform, profile.app),
    name: input.providerName,
    homepage: input.baseUrl,
    usageScript: input.usageScript ?? DEFAULT_IMPORT_PARAMETERS.usageScript,
    ...profile.parameters
  }
  if (!String(parameters.model || '').trim()) parameters.model = getCcSwitchDefaultModel(profile.platform, profile.app)
  // Additive clients default to OpenAI Chat in CCS. Select the platform's
  // native format unless the administrator explicitly chose another one.
  if (!profile.parameters.apiFormat && ['opencode', 'openclaw', 'hermes', 'pi', 'mcode', 'kimi', 'dsh', 'omp'].includes(profile.app)) {
    parameters.apiFormat = defaultProfileApiFormat(profile)
  }
  const apiFormat = String(parameters.apiFormat || defaultProfileApiFormat(profile))
  if (['gemini', 'agy'].includes(profile.app) && apiFormat !== 'gemini_native') {
    throw new Error('This application requires the Gemini Native API format')
  }
  if (apiFormat === 'gemini_native' && ['codex', 'grokbuild', 'hermes', 'mcode', 'dsh'].includes(profile.app)) {
    throw new Error('This application does not support the Gemini Native API format')
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
