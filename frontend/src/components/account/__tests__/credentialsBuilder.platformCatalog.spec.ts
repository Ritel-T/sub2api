import { afterEach, describe, expect, it } from 'vitest'

import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog,
  type PlatformCatalog
} from '@/constants/platformCatalog'
import {
  cnBalanceCellVisible,
  cnQuotaCellVisible,
  cnSupportsNativeResponses,
  defaultCNAdaptiveBaseUrls,
  defaultCNBaseUrl,
  defaultProviderProtocolRules,
  isHeaderOverrideCapable,
  isMultiProtocolApiKeyPlatform,
  providerAccountModes,
  providerHasModelCatalog,
  providerNativeProtocols,
  providerRoutesByModel,
  resolveProviderAccountMode
} from '../credentialsBuilder'

afterEach(() => {
  resetPlatformCatalog()
})

describe('credentialsBuilder derives multi-protocol data from the platform catalog', () => {

  it('exposes routing, modes and native protocols of built-in providers', () => {
    expect(providerRoutesByModel('opencode_go')).toBe(true)
    expect(providerRoutesByModel('kimi')).toBe(false)
    expect(providerAccountModes('opencode_go')).toEqual(['go', 'zen'])
    expect(providerAccountModes('deepseek')).toEqual(['payg'])
    expect(providerNativeProtocols('zhipu', 'coding')).toEqual(['chat_completions', 'anthropic'])
    expect(resolveProviderAccountMode('kimi', 'coding')).toBe('coding')
    expect(resolveProviderAccountMode('deepseek', 'coding')).toBe('payg')
    expect(resolveProviderAccountMode('opencode_go', undefined)).toBe('go')
  })
})

describe('credentialsBuilder built-in Command Code provider', () => {
  it('uses the Command Code endpoints and routes Claude / GPT by model', () => {
    expect(isMultiProtocolApiKeyPlatform('command_code')).toBe(true)
    expect(providerRoutesByModel('command_code')).toBe(true)
    expect(providerAccountModes('command_code')).toEqual(['payg'])
    expect(defaultCNAdaptiveBaseUrls('command_code', 'payg')).toEqual({
      chat_completions: 'https://api.commandcode.ai/provider/v1',
      anthropic: 'https://api.commandcode.ai/provider',
      responses: 'https://api.commandcode.ai/provider/v1'
    })
    expect(defaultProviderProtocolRules('command_code', 'payg')).toEqual([
      { pattern: 'claude-*', protocol: 'anthropic' },
      { pattern: 'gpt-*', protocol: 'responses', extraProtocols: ['chat_completions'] }
    ])
    expect(providerHasModelCatalog('command_code')).toBe(true)
    expect(providerHasModelCatalog('opencode_go')).toBe(false)
  })

  it('shows both the quota windows and the credit balance for official-host API keys only', () => {
    const account = (credentials: Record<string, unknown>, type = 'apikey') => ({ platform: 'command_code', type, credentials })
    expect(cnQuotaCellVisible(account({ account_mode: 'payg' }))).toBe(true)
    expect(cnBalanceCellVisible(account({ account_mode: 'payg' }))).toBe(true)
    expect(cnQuotaCellVisible(account({ base_url: 'https://api.commandcode.ai/provider/v1' }))).toBe(true)
    // 自定义中转与非 https 端点：后端不查官方用量接口，单元格不显示。
    const relay = account({ api_protocol: 'chat_completions', base_url: 'https://relay.example.com/v1' })
    expect(cnQuotaCellVisible(relay)).toBe(false)
    expect(cnBalanceCellVisible(relay)).toBe(false)
    expect(cnQuotaCellVisible(account({ base_url: 'http://api.commandcode.ai/provider/v1' }))).toBe(false)
    // 按模型分流的账号默认 adaptive，看 Chat Completions 分协议端点。
    const adaptiveRelay = account({ api_base_urls: { chat_completions: 'https://relay.example.com/v1' } })
    expect(cnQuotaCellVisible(adaptiveRelay)).toBe(false)
    expect(cnBalanceCellVisible(account({ account_mode: 'payg' }, 'oauth'))).toBe(false)
    expect(cnQuotaCellVisible({ platform: 'deepseek', type: 'apikey', credentials: { account_mode: 'payg' } })).toBe(false)
    expect(cnBalanceCellVisible({ platform: 'zhipu', type: 'apikey', credentials: { account_mode: 'payg' } })).toBe(false)
  })
})

describe('credentialsBuilder built-in Cline provider', () => {
  it('only offers Chat Completions and routes by inbound protocol', () => {
    expect(isMultiProtocolApiKeyPlatform('cline')).toBe(true)
    expect(providerRoutesByModel('cline')).toBe(false)
    // 积分与 ClinePass 共用同一个 Key 与端点，只有一个接入模式（不显示账号类型）。
    expect(providerAccountModes('cline')).toEqual(['payg'])
    expect(defaultCNAdaptiveBaseUrls('cline', 'payg')).toEqual({
      chat_completions: 'https://api.cline.bot/api/v1',
      anthropic: '',
      responses: ''
    })
    expect(providerNativeProtocols('cline', 'payg')).toEqual(['chat_completions'])
    expect(defaultProviderProtocolRules('cline', 'payg')).toEqual([])
  })

  it('shows ClinePass windows and the credit balance for official-host API keys only', () => {
    expect(cnQuotaCellVisible({ platform: 'cline', type: 'apikey', credentials: {} })).toBe(true)
    expect(cnBalanceCellVisible({ platform: 'cline', type: 'apikey', credentials: {} })).toBe(true)
    const relay = { platform: 'cline', type: 'apikey', credentials: { base_url: 'https://relay.example.com/v1' } }
    expect(cnQuotaCellVisible(relay)).toBe(false)
    expect(cnBalanceCellVisible(relay)).toBe(false)
  })
})

describe('credentialsBuilder picks up newly registered providers from the catalog', () => {
  const serverCatalog: PlatformCatalog = {
    platforms: [
      ...BUILTIN_PLATFORM_CATALOG.platforms,
      {
        id: 'acme_router',
        display_name: 'Acme Router',
        gateway: 'openai',
        cn_provider: false,
        multi_protocol: {
          default_mode: 'standard',
          routing: 'by_model',
          modes: [
            {
              mode: 'standard',
              base_urls: {
                chat_completions: 'https://api.acme-router.example/provider/v1',
                anthropic: 'https://api.acme-router.example/provider'
              },
              protocol_rules: [
                { pattern: 'claude-*', protocol: 'anthropic' },
                { pattern: 'bad-*', protocol: 'not-a-protocol' }
              ]
            }
          ]
        }
      }
    ],
    composite_precedence: [...BUILTIN_PLATFORM_CATALOG.composite_precedence, 'acme_router']
  }

  it('treats the new provider as a multi-protocol API-key platform', () => {
    expect(isMultiProtocolApiKeyPlatform('acme_router')).toBe(false)
    setPlatformCatalog(serverCatalog)

    expect(isMultiProtocolApiKeyPlatform('acme_router')).toBe(true)
    expect(isHeaderOverrideCapable('acme_router', 'apikey')).toBe(true)
    expect(isHeaderOverrideCapable('acme_router', 'oauth')).toBe(false)
    expect(cnSupportsNativeResponses('acme_router')).toBe(false)
    expect(providerNativeProtocols('acme_router')).toEqual(['chat_completions', 'anthropic'])
    expect(providerRoutesByModel('acme_router')).toBe(true)
    expect(resolveProviderAccountMode('acme_router', 'payg')).toBe('standard')
    expect(defaultCNBaseUrl('acme_router', 'standard', 'anthropic')).toBe('https://api.acme-router.example/provider')
    expect(defaultCNBaseUrl('acme_router', 'standard', 'responses')).toBe('https://api.acme-router.example/provider/v1')
    expect(defaultCNAdaptiveBaseUrls('acme_router', 'standard')).toEqual({
      chat_completions: 'https://api.acme-router.example/provider/v1',
      anthropic: 'https://api.acme-router.example/provider',
      responses: ''
    })
    // 非法协议的规则被丢弃。
    expect(defaultProviderProtocolRules('acme_router', 'standard')).toEqual([
      { pattern: 'claude-*', protocol: 'anthropic' }
    ])
  })
})
