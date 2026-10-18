import { describe, it, expect } from 'vitest'
import { consoleRouteTitle, consoleWorkspaceLabel } from '../consoleShell'

describe('console page context', () => {
  it('keeps analysis, successful records and failed requests distinct', () => {
    expect(consoleRouteTitle('/usage', { view: 'analysis' }, 'en')).toBe('Usage analytics')
    expect(consoleRouteTitle('/usage', { view: 'records' }, 'en')).toBe('Request records')
    expect(consoleRouteTitle('/usage', { view: 'records', tab: 'errors' }, 'zh-Hant')).toBe('失敗請求')
    expect(consoleRouteTitle('/usage', { view: 'errors' }, 'en')).toBe('Failed requests')
  })
  it('leaves observer and other route titles to the existing route metadata', () => {
    expect(consoleRouteTitle('/usage', { view: 'analysis' }, 'en', true)).toBeUndefined()
    expect(consoleRouteTitle('/orders', {}, 'en')).toBeUndefined()
  })
  it('makes the management and personal boundaries clear on phones too', () => {
    expect(consoleWorkspaceLabel('/admin/accounts', 'admin', 'en')).toBe('Administration')
    expect(consoleWorkspaceLabel('/keys', 'admin', 'zh-Hant')).toBe('我的帳戶')
    expect(consoleWorkspaceLabel('/admin/accounts', 'observer', 'en')).toBe('Observer')
    expect(consoleWorkspaceLabel('/keys', 'user', 'en')).toBe('')
  })
})
