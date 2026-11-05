import { afterEach, describe, expect, it } from 'vitest'
import { computed } from 'vue'
import { CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import {
  BUILTIN_PLATFORM_CATALOG,
  platformDisplayName,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'
import { platformLabel } from '@/utils/platformColors'
import { normalizePlatformQuotasMap, sanitizePlatformQuotasMap } from '@/api/admin/settings'
import { platformQuotaPlatforms } from '@/api/admin/users'

// 改为读取平台清单之前的平台字面量，加上之后内置登记的 Command Code。
const concretePlatforms = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go',
  'typesafe',
  'command_code',
  'cline'
]

describe('platform option catalogs', () => {

  it('adds composite for group-backed filters', () => {
    expect(GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms,
      'composite'
    ])
  })
})

describe('platform catalog with a newly registered platform', () => {
  const withNewProvider = {
    platforms: [
      ...BUILTIN_PLATFORM_CATALOG.platforms,
      { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai', cn_provider: false }
    ],
    composite_precedence: [...BUILTIN_PLATFORM_CATALOG.composite_precedence, 'acme_router']
  }

  afterEach(() => {
    resetPlatformCatalog()
  })

  it('adds newly registered platforms to options, labels and quota lists reactively', () => {
    const optionValues = computed(() => CONCRETE_PLATFORM_OPTIONS.map((option) => option.value))
    expect(optionValues.value).not.toContain('acme_router')

    setPlatformCatalog(withNewProvider)

    expect(optionValues.value).toEqual([...concretePlatforms, 'acme_router'])
    expect(GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms,
      'acme_router',
      'composite'
    ])
    expect(platformLabel('acme_router')).toBe('Acme Router')
    expect(platformDisplayName('acme_router')).toBe('Acme Router')
    expect(platformLabel('unregistered')).toBe('unregistered')
    expect(platformQuotaPlatforms()).toContain('acme_router')
    expect(Object.keys(normalizePlatformQuotasMap())).toEqual([...concretePlatforms, 'acme_router'])
    expect(sanitizePlatformQuotasMap({ acme_router: { daily: 5, weekly: -1, monthly: null } }).acme_router).toEqual({
      daily: 5,
      weekly: null,
      monthly: null
    })
  })

  it('restores the built-in catalog on reset', () => {
    setPlatformCatalog(withNewProvider)
    resetPlatformCatalog()
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(concretePlatforms)
  })
})
