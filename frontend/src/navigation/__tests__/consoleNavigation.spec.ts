import { describe, expect, it } from 'vitest'
import { arrangeConsoleSections, consoleNavigationText, consoleSearchTree, isConsoleLocationActive, resolveConsoleLocation, tabBadge, workspaceLocation, legalWorkspaceDestination, type ConsoleNavItem } from '../consoleNavigation'
import { buildFeatureSearchEntries, searchFeatures } from '@/utils/featureSearch'
const item = (path: string, label = path): ConsoleNavItem => ({ path, label, icon: null })
describe('console navigation organization', () => {
  it('places every granted leaf once, merges flagged groups and keeps extensions last', () => {
    const custom = { ...item('/custom/custom-1'), iconSvg: '<svg />' }
    const smartOps = { ...item('/admin/smart-ops', 'Smart ops'), expandOnly: true, mergeTabs: true, children: [item('/admin/token-guard'), { ...item('/admin/token-guard-v2'), badge: () => 2 }] }
    const source = [custom, item('/admin/accounts'), item('/admin/dashboard'), smartOps, { ...item('/admin/orders'), expandOnly: true, children: [item('/admin/orders'), item('/admin/orders/plans')] }, item('/keys'), item('/admin/settings')]
    const sections = arrangeConsoleSections(source, 'management', consoleNavigationText('en'))
    expect(sections.map(section => section.key)).toEqual(['overview', 'resources', 'operations', 'revenue', 'system', 'extensions'])
    expect(sections.find(section => section.key === 'resources')?.tabs.map(tab => tab.path)).toEqual(['/admin/accounts', '/keys'])
    const operations = sections.find(section => section.key === 'operations')!
    expect(operations.tabs).toHaveLength(1)
    expect(operations.tabs[0]).toMatchObject({ label: 'Smart ops', path: '/admin/token-guard', matches: ['/admin/token-guard', '/admin/token-guard-v2'] })
    expect(tabBadge(operations.tabs[0]!)).toBe(2)
    expect(operations.badge?.()).toBe(2)
    expect(sections.at(-1)?.tabs[0]?.item.iconSvg).toBe('<svg />')
    const entries = buildFeatureSearchEntries(consoleSearchTree(sections))
    expect(entries.map(entry => entry.path).sort()).toEqual(['/admin/dashboard', '/admin/accounts', '/admin/token-guard', '/admin/token-guard-v2', '/admin/orders', '/admin/orders/plans', '/keys', '/admin/settings', '/custom/custom-1'].sort())
    expect(entries.find(entry => entry.path === '/admin/token-guard')?.group).toBe('Operations / Smart ops')
  })
  it('groups the personal workspace, resolves the active tab and searches multilingual aliases', () => {
    const source = ['/profile', '/usage?view=records', '/keys', '/purchase', '/dashboard', '/usage?view=analysis', '/model-plaza?embedded=1', '/redeem'].map(path => item(path))
    const sections = arrangeConsoleSections(source, 'personal', consoleNavigationText('zh'))
    expect(sections.map(section => [section.key, section.label, section.tabs.map(tab => tab.path)])).toEqual([
      ['overview', '总览', ['/dashboard']],
      ['access', '接入', ['/keys', '/model-plaza?embedded=1']],
      ['usage', '用量', ['/usage?view=analysis', '/usage?view=records']],
      ['billing', '账务', ['/purchase', '/redeem']],
      ['account', '账户', ['/profile']],
    ])
    expect(sections.find(section => section.key === 'account')?.hidden).toBe(true)
    expect(resolveConsoleLocation(sections, '/usage', {})?.tab.path).toBe('/usage?view=records')
    expect(resolveConsoleLocation(sections, '/usage', { view: 'analysis' })?.tab.path).toBe('/usage?view=analysis')
    expect(resolveConsoleLocation(sections, '/model-plaza', {})?.section.key).toBe('access')
    expect(resolveConsoleLocation(sections, '/admin/users', {})).toBeUndefined()
    const entries = buildFeatureSearchEntries(consoleSearchTree(sections))
    expect(searchFeatures(entries, '用量')[0]?.path).toBe('/usage?view=analysis')
    expect(searchFeatures(entries, '請求')[0]?.path).toBe('/usage?view=records')
    expect(searchFeatures(entries, '充值')[0]?.path).toBe('/purchase')
    expect(searchFeatures(entries, '密碼')[0]?.path).toBe('/profile')
    expect(searchFeatures(entries, 'token guard')).toEqual([])
  })
  it('never marks analytics and records active together and retains legacy records default', () => {
    expect(isConsoleLocationActive('/usage?view=analysis', '/usage', {})).toBe(false)
    expect(isConsoleLocationActive('/usage?view=records', '/usage', {})).toBe(true)
    expect(isConsoleLocationActive('/usage?view=analysis', '/usage', { view: 'analysis' })).toBe(true)
    expect(isConsoleLocationActive('/usage?view=records', '/usage', { view: 'analysis' })).toBe(false)
    expect(isConsoleLocationActive('/keys', '/keys', { connect: '1' })).toBe(true)
  })
})

describe('workspace restoration safety', () => {
  it('permits only granted navigation paths and known structural query values', () => {
    const allowed = [item('/usage?view=records'), item('/keys')]
    expect(workspaceLocation('/usage', { view: 'analysis', page: '3', status: 'sk-secret', type: 'private', search: 'name', api_key: 'secret' }, allowed)).toBe('/usage?view=analysis&page=3')
    expect(legalWorkspaceDestination('/admin/accounts', allowed, '/dashboard')).toBe('/dashboard')
    expect(legalWorkspaceDestination('//attacker.test/keys', allowed, '/dashboard')).toBe('/dashboard')
    expect(legalWorkspaceDestination('/keys?key=secret&connect=1', allowed, '/dashboard')).toBe('/keys')
  })
})
