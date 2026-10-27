// @vitest-environment node
import { describe, expect, it } from 'vitest'
import type { CcsImportApp, CcsImportConfig, CcsImportProfile } from '@/types/ccswitch'
import {
  CC_SWITCH_USAGE_SCRIPT,
  buildCcSwitchImportDeeplink,
  createDefaultCcSwitchImportConfig,
  getCcSwitchImportProfiles,
  getCcSwitchApplicationProfiles
} from '../ccswitchImport'

const base = {
  baseUrl: 'https://api.example.com', platform: 'openai' as const,
  providerName: '站点 Ritel SI', apiKey: 'sk-user-key', keyName: '我的 Key', userId: 42
}
const profile = (parameters: CcsImportProfile['parameters'] = {}, app: CcsImportApp = 'codex'): CcsImportProfile => ({
  id: 'openai-custom', label: '自定义客户端', platform: 'openai', app, parameters
})

describe('user CC Switch application choices', () => {
  it('does not offer agent imports for the dedicated SystemOne non-LLM API', () => {
    expect(getCcSwitchApplicationProfiles('typesafe')).toEqual([])
  })
  it.each(['openai', 'anthropic', 'gemini', 'antigravity', 'grok', 'deepseek', 'minimax', 'composite'] as const)(
    'offers the same ordered applications for a %s key and preserves its key in each link', platform => {
      const profiles = getCcSwitchApplicationProfiles(platform)
      expect(profiles.map(item => item.app)).toEqual(['codex', 'claude', 'opencode', 'openclaw', 'gemini', 'grokbuild', 'hermes', 'pi'])
      for (const item of profiles) {
        const params = new URL(buildCcSwitchImportDeeplink({ ...base, platform, profile: item })).searchParams
        expect(params.get('apiKey')).toBe(base.apiKey)
        expect(params.get('app')).toBe(item.app)
        expect(params.get('endpoint')).not.toContain('/v1/v1')
        expect(params.get('model')).toBeTruthy()
      }
    }
  )

  it('routes OpenAI into Claude Code using Responses instead of assuming Anthropic support', () => {
    const profile = getCcSwitchApplicationProfiles('openai').find(item => item.app === 'claude')!
    const params = new URL(buildCcSwitchImportDeeplink({ ...base, profile })).searchParams
    expect(params.get('apiFormat')).toBe('openai_responses')
    expect(params.get('endpoint')).toBe('https://api.example.com/v1')
    expect(params.get('model')).toBe('gpt-6-astra')
    expect(params.get('haikuModel')).toBe('gpt-6-astra')
    expect(params.get('sonnetModel')).toBe('gpt-6-astra')
    expect(params.get('opusModel')).toBe('gpt-6-astra')
  })

  it('uses compatible formats for Gemini keys in clients without native Gemini support', () => {
    for (const profile of getCcSwitchApplicationProfiles('gemini')) {
      const params = new URL(buildCcSwitchImportDeeplink({ ...base, platform: 'gemini', profile })).searchParams
      expect(params.get('apiFormat')).toBe(['codex', 'grokbuild', 'hermes'].includes(profile.app) ? 'openai_responses' : 'gemini_native')
    }
  })

  it.each(['antigravity', 'composite'] as const)('uses a Gemini model for Gemini CLI in %s groups', platform => {
    const profile = getCcSwitchApplicationProfiles(platform).find(item => item.app === 'gemini')!
    const params = new URL(buildCcSwitchImportDeeplink({ ...base, platform, profile })).searchParams
    expect(params.get('apiFormat')).toBe('gemini_native')
    expect(params.get('model')).toBe('gemini-2.5-pro')
  })

  it('retains explicit overrides, app variants, and the global empty setting', () => {
    const config: CcsImportConfig = { profiles: [
      { ...profile({ endpoint: 'https://custom.example.com/v1', model: 'chosen' }), id: 'first' },
      { ...profile({ endpoint: 'https://second.example.com/v1' }), id: 'second' }
    ] }
    const choices = getCcSwitchApplicationProfiles('openai', config)
    expect(choices.slice(0, 2).map(item => item.id)).toEqual(['first', 'second'])
    expect(choices[0]?.parameters.endpoint).toBe('https://custom.example.com/v1')
    expect(choices[0]?.parameters.model).toBe('chosen')
    expect(choices.some(item => item.app === 'claude')).toBe(true)
    expect(getCcSwitchApplicationProfiles('deepseek', { profiles: [] })).toEqual([])
  })
})
const decode = (encoded: string) => new TextDecoder().decode(Uint8Array.from(atob(encoded), byte => byte.charCodeAt(0)))

describe('administrator CC Switch provider profiles', () => {
  it('uses fresh defaults with Astra and preserves explicitly disabled platforms', () => {
    const defaults = createDefaultCcSwitchImportConfig()
    expect(getCcSwitchImportProfiles('openai')[0]?.parameters.model).toBe('gpt-6-astra')
    expect(getCcSwitchImportProfiles('antigravity')).toHaveLength(2)
    expect(getCcSwitchImportProfiles('openai', { profiles: [] })).toEqual([])
    defaults.profiles[1]!.parameters.model = 'edited'
    expect(createDefaultCcSwitchImportConfig().profiles[1]?.parameters.model).toBe('gpt-6-astra')
    expect(() => buildCcSwitchImportDeeplink({ ...base, settings: { profiles: [] } })).toThrow('No CC Switch')
  })

  it('selects only profiles for the key platform and keeps administrator ordering', () => {
    const codex = profile({ model: 'gpt-6-astra' })
    const alternative = { ...profile({ model: 'gpt-6-astra' }, 'opencode'), id: 'openai-opencode' }
    const settings: CcsImportConfig = { profiles: [alternative, ...createDefaultCcSwitchImportConfig().profiles, codex] }
    expect(getCcSwitchImportProfiles('openai', settings).map(item => item.id)).toEqual([
      'openai-opencode', 'openai-codex', 'openai-custom'
    ])
    expect(new URL(buildCcSwitchImportDeeplink({ ...base, settings })).searchParams.get('app')).toBe('opencode')
    expect(() => buildCcSwitchImportDeeplink({ ...base, profile: { ...codex, platform: 'anthropic' } })).toThrow()
  })

  it.each(['claude', 'codex', 'gemini', 'grokbuild', 'opencode', 'openclaw', 'hermes', 'pi'] as const)(
    'can generate the CCS %s application parameter without changing the selected user key', app => {
      const params = new URL(buildCcSwitchImportDeeplink({
        ...base, profile: profile({ model: 'gpt-6-astra' }, app)
      })).searchParams
      expect(params.get('app')).toBe(app)
      expect(params.get('apiKey')).toBe('sk-user-key')
    }
  )

  it('carries every supported provider field, booleans and zero interval without lossy encoding', () => {
    const config = 'model_provider = "custom"\nmodel_reasoning_effort = "high"\n# 中文配置 + 🔧\n[model_providers.custom]\nwire_api = "responses"'
    const script = '({ request: { url: "{{baseUrl}}/v1/usage", headers: { Authorization: "Bearer {{apiKey}}" } }, extractor: () => ({planName: "套餐🚀"}) })'
    const params = new URL(buildCcSwitchImportDeeplink({ ...base, profile: profile({
      name: '{{siteName}} / {{keyName}}', endpoint: 'https://one.example.com/v1,https://two.example.com/v1',
      homepage: 'https://example.com', model: 'gpt-6-astra', apiFormat: 'openai_responses',
      haikuModel: 'small', sonnetModel: 'medium', opusModel: 'large', notes: '备注 + & = 中文',
      icon: 'openai', iconUrl: 'https://example.com/logo.svg', enabled: false,
      configFormat: 'toml', config, usageEnabled: false, usageScript: script,
      usageAutoInterval: 0, usageApiKey: '{{apiKey}}', usageBaseUrl: 'https://usage.example.com',
      usageAccessToken: '{{apiKey}}', usageUserId: '{{userId}}'
    }) })).searchParams
    expect(params.get('name')).toBe('站点 Ritel SI / 我的 Key')
    expect(params.get('endpoint')).toBe('https://one.example.com/v1,https://two.example.com/v1')
    expect(params.get('notes')).toBe('备注 + & = 中文')
    expect(params.get('usageUserId')).toBe('42')
    expect(params.get('usageApiKey')).toBe('sk-user-key')
    expect(params.get('usageAccessToken')).toBe('sk-user-key')
    expect(params.get('usageAutoInterval')).toBe('0')
    expect(params.get('usageEnabled')).toBe('false')
    expect(params.get('enabled')).toBe('false')
    expect(params.get('haikuModel')).toBe('small')
    expect(params.get('sonnetModel')).toBe('medium')
    expect(params.get('opusModel')).toBe('large')
    expect(params.get('iconUrl')).toBe('https://example.com/logo.svg')
    expect(params.get('homepage')).toBe('https://example.com')
    expect(params.get('model')).toBe('gpt-6-astra')
    expect(params.get('apiFormat')).toBe('openai_responses')
    expect(params.get('configFormat')).toBe('toml')
    expect(decode(params.get('config')!)).toBe(config)
    expect(decode(params.get('usageScript')!)).toBe(script)
  })

  it('keeps raw JSON and script placeholders literal, and never accepts a static replacement API key', () => {
    const config = JSON.stringify({ env: { ANTHROPIC_AUTH_TOKEN: '{{apiKey}}', API_TIMEOUT_MS: '600000' } })
    const params = new URL(buildCcSwitchImportDeeplink({ ...base, profile: profile({
      config, configFormat: 'json', apiKey: 'sk-admin-not-allowed', resource: 'mcp', configUrl: 'https://invalid.example/config'
    }) })).searchParams
    expect(decode(params.get('config')!)).toBe(config)
    expect(decode(params.get('usageScript')!)).toBe(CC_SWITCH_USAGE_SCRIPT)
    expect(params.get('resource')).toBe('provider')
    expect(params.get('apiKey')).toBe('sk-user-key')
    expect(params.has('configUrl')).toBe(false)
  })

  it('keeps explicit administrator endpoints and emits exactly one /v1 for default Codex endpoints', () => {
    for (const baseUrl of ['https://api.example.com', 'https://api.example.com/', 'https://api.example.com/v1/']) {
      expect(new URL(buildCcSwitchImportDeeplink({ ...base, baseUrl })).searchParams.get('endpoint')).toBe('https://api.example.com/v1')
    }
    expect(new URL(buildCcSwitchImportDeeplink({ ...base, profile: profile({ endpoint: 'https://proxy.example.com/custom' }) })).searchParams.get('endpoint')).toBe('https://proxy.example.com/custom')
  })

  it('sets the native platform format for additive clients while preserving explicit overrides', () => {
    expect(new URL(buildCcSwitchImportDeeplink({ ...base, profile: profile({}, 'opencode') })).searchParams.get('apiFormat')).toBe('openai_responses')
    expect(new URL(buildCcSwitchImportDeeplink({ ...base, profile: profile({ apiFormat: 'openai_chat' }, 'opencode') })).searchParams.get('apiFormat')).toBe('openai_chat')
    expect(new URL(buildCcSwitchImportDeeplink({ ...base, platform: 'anthropic', profile: { ...profile({}, 'openclaw'), platform: 'anthropic' } })).searchParams.get('apiFormat')).toBe('anthropic')
  })

  it.each([
    ['anthropic', 'opencode', 'anthropic', '/v1'],
    ['anthropic', 'hermes', 'anthropic', ''],
    ['anthropic', 'pi', 'anthropic', ''],
    ['anthropic', 'openclaw', 'anthropic', ''],
    ['gemini', 'opencode', 'gemini_native', '/v1beta'],
    ['gemini', 'pi', 'gemini_native', '/v1beta'],
    ['gemini', 'openclaw', 'gemini_native', '/v1beta'],
    ['gemini', 'gemini', null, ''],
    ['openai', 'codex', null, '/v1'],
    ['antigravity', 'opencode', 'anthropic', '/antigravity/v1']
  ] as const)('uses the %s/%s SDK endpoint convention', (platform, app, apiFormat, suffix) => {
    const params = new URL(buildCcSwitchImportDeeplink({
      ...base, platform, profile: { ...profile({ model: 'custom' }, app), platform }
    })).searchParams
    expect(params.get('endpoint')).toBe(`https://api.example.com${suffix}`)
    expect(params.get('apiFormat')).toBe(apiFormat)
    const usage = new Function(`return ${decode(params.get('usageScript')!).replaceAll('{{baseUrl}}', params.get('endpoint')!)}`)() as { request: { url: string } }
    expect(usage.request.url).toBe(`https://api.example.com${platform === 'antigravity' ? '/antigravity' : ''}/v1/usage`)
  })

  it('requires explicit compatible formats for Hermes on Gemini profiles', () => {
    expect(() => buildCcSwitchImportDeeplink({
      ...base, platform: 'gemini', profile: { ...profile({}, 'hermes'), platform: 'gemini' }
    })).toThrow('Hermes')
    const params = new URL(buildCcSwitchImportDeeplink({
      ...base, platform: 'gemini', profile: { ...profile({ apiFormat: 'openai_chat' }, 'hermes'), platform: 'gemini' }
    })).searchParams
    expect(params.get('apiFormat')).toBe('openai_chat')
    expect(params.get('endpoint')).toBe('https://api.example.com/v1')
  })
})

describe('CCS built-in balance extractor', () => {
  const config = new Function(`return ${CC_SWITCH_USAGE_SCRIPT}`)() as { extractor: (data: unknown) => Record<string, unknown> }
  it('displays subscription names and quotas and hides the unlimited -1 sentinel', () => {
    expect(config.extractor({ isValid: true, planName: '无限套餐', remaining: -1, unit: 'USD' })).toEqual({ isValid: true, planName: '无限套餐', unit: 'USD' })
    expect(config.extractor({ isValid: true, remaining: 20, quota: { used: 5, limit: 25 } })).toEqual({ isValid: true, remaining: 20, used: 5, total: 25, unit: 'USD' })
    expect(config.extractor({ isValid: false, status: 'expired' })).toMatchObject({ isValid: false, invalidMessage: 'expired' })
  })
})
