import { describe, expect, it } from 'vitest'
import { arrangeConsoleNavigation, consoleNavigationText, isConsoleLocationActive, workspaceLocation, legalWorkspaceDestination, type ConsoleNavItem } from '../consoleNavigation'
import { buildFeatureSearchEntries, searchFeatures } from '@/utils/featureSearch'
const icons = { billing: null, applications: null, operations: null, system: null, extensions: null }
const item = (path: string, label = path): ConsoleNavItem => ({ path, label, icon: null })
describe('console navigation organization', () => {
  it('preserves every granted leaf and custom SVG once, including children of structural groups', () => {
    const custom = { ...item('/custom/custom-1'), iconSvg: '<svg />' }
    const source = [item('/admin/dashboard'), item('/admin/accounts'), { ...item('/admin/smart-ops'), expandOnly: true, children: [item('/admin/token-guard'), item('/admin/token-guard-v2')] }, { ...item('/admin/orders'), expandOnly: true, children: [item('/admin/orders'), item('/admin/orders/plans')] }, custom]
    const nav = arrangeConsoleNavigation(source, 'management', consoleNavigationText('en'), icons)
    const entries = buildFeatureSearchEntries(nav)
    expect(entries.map(entry => entry.path).sort()).toEqual(['/admin/dashboard', '/admin/accounts', '/admin/token-guard', '/admin/token-guard-v2', '/admin/orders', '/admin/orders/plans', '/custom/custom-1'].sort())
    expect(nav.find(node => node.path.endsWith('extensions'))?.children?.[0]?.iconSvg).toBe('<svg />')
    expect(nav.slice(0, 2).map(node => node.path)).toEqual(['/admin/dashboard', '/admin/accounts'])
  })
  it('keeps five personal essentials direct, moves billing into one group and searches multilingual aliases', () => {
    const source = ['/profile', '/usage?view=records', '/keys', '/purchase', '/dashboard', '/usage?view=analysis', '/model-plaza?embedded=1', '/redeem'].map(path => item(path))
    const nav = arrangeConsoleNavigation(source, 'personal', consoleNavigationText('en'), icons)
    expect(nav.slice(0, 5).map(node => node.path)).toEqual(['/dashboard', '/keys', '/model-plaza?embedded=1', '/usage?view=analysis', '/usage?view=records'])
    const entries = buildFeatureSearchEntries(nav)
    expect(searchFeatures(entries, '用量')[0]?.path).toBe('/usage?view=analysis')
    expect(searchFeatures(entries, '請求')[0]?.path).toBe('/usage?view=records')
    expect(searchFeatures(entries, '充值')[0]?.path).toBe('/purchase')
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
