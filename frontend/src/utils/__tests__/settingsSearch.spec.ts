// @vitest-environment node
import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import { buildFeatureSearchEntries, searchFeatures } from '../featureSearch'
import { settingsLocation, withSettingsSearch } from '../settingsSearch'

function translate(messages: unknown, key: string): string {
  const value = key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown>)?.[part], messages)
  expect(typeof value, key).toBe('string')
  return value as string
}

describe('settings feature search', () => {
  it('finds risk switches in both languages even when the business page is absent', () => {
    for (const [locale, messages, queries] of [
      ['zh', zh, ['风控', 'cyber', '会话自动屏蔽', '屏蔽时长', '严格要求明确会话身份']],
      ['en', en, ['risk', 'cyber']]
    ] as const) {
      const entries = buildFeatureSearchEntries(withSettingsSearch([
        { path: '/admin/settings', label: '系统设置' }
      ], key => translate(messages, key), locale))
      for (const query of queries) {
        expect(searchFeatures(entries, query).some(item => item.path === '/admin/settings?tab=features#settings-section-features-risk-control'), query).toBe(true)
      }
      expect(entries.some(item => item.path === '/admin/risk-control')).toBe(false)
      expect(withSettingsSearch([{ path: '/admin/users', label: '用户' }], key => translate(messages, key), locale)[0]?.children).toBeUndefined()
    }
  })

  it('finds the usage cache rate switches from the usage records card', () => {
    for (const [locale, messages, query] of [
      ['zh', zh, '缓存率'],
      ['en', en, 'cache rate'],
    ] as const) {
      const entries = buildFeatureSearchEntries(withSettingsSearch([
        { path: '/admin/settings', label: 'Settings' }
      ], key => translate(messages, key), locale))
      expect(searchFeatures(entries, query).some(item => item.path === '/admin/settings?tab=gateway#settings-section-usage-records'), query).toBe(true)
    }
  })

  it('validates locations and prefers the target card tab over a conflicting query', () => {
    expect(settingsLocation('security', '#settings-section-features-risk-control').tab).toBe('features')
    expect(settingsLocation(['features'], '#unknown')).toEqual({ tab: 'general', anchor: undefined, linked: false })
    expect(settingsLocation('email', '')).toEqual({ tab: 'email', anchor: undefined, linked: true })
  })
})
