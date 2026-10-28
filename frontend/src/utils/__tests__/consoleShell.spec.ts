// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { consoleRouteTitle } from '../consoleShell'

describe('console page context', () => {
  it('keeps analysis, successful records and failed requests distinct', () => {
    expect(consoleRouteTitle('/usage', { view: 'analysis' }, 'en')).toBe('Usage analytics')
    expect(consoleRouteTitle('/usage', { view: 'records' }, 'en')).toBe('Request records')
    expect(consoleRouteTitle('/usage', { view: 'records', tab: 'errors' }, 'zh-Hant')).toBe('失敗請求')
    expect(consoleRouteTitle('/usage', { view: 'errors' }, 'en')).toBe('Failed requests')
  })
  it('uses the split page title for administrators and observers', () => {
    expect(consoleRouteTitle('/usage', { view: 'analysis' }, 'en', true)).toBe('Usage analytics')
    expect(consoleRouteTitle('/admin/usage', { view: 'analysis' }, 'en')).toBe('Usage analytics')
    expect(consoleRouteTitle('/admin/usage', { view: 'records', tab: 'errors' }, 'en')).toBe('Request records')
    expect(consoleRouteTitle('/admin/usage', { view: 'ranking' }, 'zh-Hant-HK')).toBe('用量分析')
    expect(consoleRouteTitle('/usage', { view: 'records' }, 'zh-Hant-HK', true)).toBe('請求記錄')
    expect(consoleRouteTitle('/orders', {}, 'en')).toBeUndefined()
  })
})
