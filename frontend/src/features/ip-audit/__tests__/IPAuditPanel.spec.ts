import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { auditTab, listIPAudit, releaseIPAudit, type AuditEntry, type AuditPage } from '../api'
import IPAuditPanel from '../IPAuditPanel.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import enCommon from '@/i18n/locales/en/common'
import zhCommon from '@/i18n/locales/zh/common'
import en from '@/i18n/locales/en/ipAudit'
import zh from '@/i18n/locales/zh/ipAudit'
vi.mock('../api', async () => ({ ...(await vi.importActual('../api')), listIPAudit: vi.fn(), releaseIPAudit: vi.fn() }))
const entry: AuditEntry = { id: 'stable', source: 'anonymous_api', network: '8.8.8.8/32', scope: 'invalid_credentials', reason: 'anonymous_api_unknown', state: 'active', effective: 'conditional', manual: false, can_release: true, started_at: 100, expires_at: 200, count: null, threshold: 30, version: 'version' }
const page = (): AuditPage => ({ items: [entry], sources: [
  { id: 'anonymous_api', scope: 'invalid_credentials', storage: 'redis', enumeration: 'indexed', can_release: true },
  { id: 'login_failures', scope: 'authentication', storage: 'redis', enumeration: 'exact_ip', can_release: true },
  { id: 'manual_deny', scope: 'site', storage: 'settings', enumeration: 'bounded_config', can_release: false, manage_url: '/admin/settings?tab=security#ip-risk-settings' },
  { id: 'host_firewall', scope: 'external_host', storage: 'not_connected', enumeration: 'not_connected', can_release: false }
], has_more: true, next_cursor: 20, unrestricted: false })
function render(locale: 'en' | 'zh' = 'en') {
 return mount(IPAuditPanel, { global: { plugins: [createI18n({ legacy: false, locale, messages: { en: { ipAudit: en, common: enCommon.common }, zh: { ipAudit: zh, common: zhCommon.common } } })] } })
}
beforeEach(() => {
 localStorage.clear(); vi.resetAllMocks();vi.mocked(listIPAudit).mockResolvedValue(page())
 vi.mocked(releaseIPAudit).mockResolvedValue({ released: true, readback: 'released', remaining: [{ ...entry, source: 'manual_deny', manual: true, can_release: false }], unknown_sources: ['host_firewall'], unrestricted: false })
})
describe('IP audit', () => {
 it.each(['en', 'zh'] as const)('uses real %s labels and external-source boundaries', async (locale) => {
  const wrapper = render(locale);await flushPromises()
  expect(wrapper.text()).toContain(locale === 'en' ? en.externalHint : zh.externalHint)
  expect(wrapper.text()).not.toContain('ipAudit.')
  expect(wrapper.find('[data-testid="ip-open-settings"]').exists()).toBe(true)
  wrapper.unmount()
 })
 it('sends filters and bounded page cursors to the server, and clears selection on refresh', async () => {
  const wrapper = render();await flushPromises()
  await wrapper.get('[data-testid="audit-ip"]').setValue(' 8.8.8.8 ')
  await wrapper.get('[data-testid="audit-state"]').setValue('active')
  await wrapper.get('form').trigger('submit');await flushPromises()
  expect(listIPAudit).toHaveBeenLastCalledWith(expect.objectContaining({ source: 'anonymous_api', ip: '8.8.8.8', state: 'active', cursor: 0, limit: 20 }), expect.anything())
  await wrapper.get('[data-testid="audit-more"] button').trigger('click');await flushPromises()
  expect(listIPAudit).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 20 }), expect.anything())
  wrapper.unmount()
 })
 it('retains rows and retries the same cursor after append failure', async () => {
  const wrapper = render(); await flushPromises()
  vi.mocked(listIPAudit).mockRejectedValueOnce(new Error('offline'))
  await wrapper.get('[data-testid="audit-more"] button').trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain(entry.network)
  expect(wrapper.get('[role="alert"]').text()).toBe(en.loadError)
  vi.mocked(listIPAudit).mockResolvedValueOnce({ ...page(), items: [{ ...entry, id: 'second', network: '1.1.1.1/32' }], has_more: false, next_cursor: 40 })
  await wrapper.get('[data-testid="audit-more"] button').trigger('click'); await flushPromises()
  expect(vi.mocked(listIPAudit).mock.calls.map(call => call[0].cursor)).toEqual([0, 20, 20])
  expect(wrapper.findAll('tbody tr')).toHaveLength(2)
  expect(wrapper.find('[data-testid="audit-more"] button').exists()).toBe(false)
  wrapper.unmount()
 })
 it('refreshes the loaded cursor window atomically and preserves rows on refresh failure', async () => {
  vi.mocked(listIPAudit).mockImplementation(async query => query.cursor === 0 ? page() : { ...page(), items: [{ ...entry, id: 'second', network: '1.1.1.1/32' }], has_more: false, next_cursor: 40 })
  const wrapper = render(); await flushPromises()
  await wrapper.get('[data-testid="audit-more"] button').trigger('click'); await flushPromises()
  vi.mocked(listIPAudit).mockClear()
  vi.mocked(listIPAudit).mockResolvedValueOnce({ ...page(), items: [{ ...entry, network: '9.9.9.9/32' }] }).mockRejectedValueOnce(new Error('offline'))
  await wrapper.findAll('button').find(button => button.text() === 'Refresh')!.trigger('click'); await flushPromises()
  expect(vi.mocked(listIPAudit).mock.calls.map(call => call[0].cursor)).toEqual([0, 20])
  expect(wrapper.text()).toContain(entry.network); expect(wrapper.text()).toContain('1.1.1.1/32')
  expect(wrapper.text()).not.toContain('9.9.9.9/32')
  await wrapper.get('[data-testid="audit-more"] button').trigger('click'); await flushPromises()
  expect(vi.mocked(listIPAudit).mock.calls.map(call => call[0].cursor)).toEqual([0, 20, 0, 20])
  expect(wrapper.findAll('tbody tr')).toHaveLength(2)
  wrapper.unmount()
 })
 it('aborts an older filter request and prevents it publishing after reset', async () => {
  let resolveOld!: (value: AuditPage) => void
  vi.mocked(listIPAudit).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
  const wrapper = render(); await flushPromises()
  const signal = vi.mocked(listIPAudit).mock.calls[0]![1]!
  await wrapper.get('[data-testid="audit-source"]').setValue('login_failures'); await flushPromises()
  expect(signal.aborted).toBe(true)
  resolveOld({ ...page(), items: [{ ...entry, network: 'stale-network' }] }); await flushPromises()
  expect(wrapper.text()).not.toContain('stale-network')
  const latestSignal = vi.mocked(listIPAudit).mock.calls.at(-1)![1]!
  wrapper.unmount(); expect(latestSignal.aborted).toBe(true)
 })
 it('deduplicates overlapping cursor results and stops a stalled cursor', async () => {
  const wrapper = render(); await flushPromises()
  await wrapper.get('[data-testid="audit-more"] button').trigger('click'); await flushPromises()
  expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  expect(wrapper.find('[data-testid="audit-more"] button').exists()).toBe(false)
  wrapper.unmount()
 })
 it('requires per-source confirmation, then displays readback and remaining restrictions', async () => {
  const wrapper = render();await flushPromises()
  await wrapper.get('[data-testid="release-source"]').trigger('click')
  expect(releaseIPAudit).not.toHaveBeenCalled()
  expect(wrapper.get('[data-testid="release-dialog"]').text()).toContain(entry.network)
  expect(wrapper.get('[data-testid="release-dialog"]').attributes('role')).toBe('region')
  expect(wrapper.get('[data-testid="release-dialog"]').attributes('aria-modal')).toBeUndefined()
  await wrapper.get('[data-testid="release-confirm"]').trigger('click');await flushPromises()
  expect(releaseIPAudit).toHaveBeenCalledWith(entry, 'operator_review')
  expect(listIPAudit).toHaveBeenCalledTimes(2)
  const result = wrapper.get('[data-testid="release-outcome"]').text()
  expect(result).toContain(en.released);expect(result).toContain(en.notUnrestricted)
  expect(result).toContain(en.sources.manual_deny);expect(result).toContain(en.sources.host_firewall)
  wrapper.unmount()
 })
 it('requires a distinct confirmation for the explicitly user-wide penalty action', async () => {
  const userEntry = { ...entry, source: 'security_penalty', network: '', user_id: 9, scope: 'all_user_states', version: 'explicit-user-scope-v1' }
  vi.mocked(listIPAudit).mockResolvedValue({ ...page(), items: [userEntry] })
  const wrapper = render();await flushPromises()
  await wrapper.get('[data-testid="release-source"]').trigger('click')
  expect(wrapper.get('[data-testid="release-dialog"]').text()).toContain(en.confirmUserScope)
  expect(wrapper.get('[data-testid="release-dialog"]').text()).toContain('UID 9')
  expect(releaseIPAudit).not.toHaveBeenCalled()
  await wrapper.get('[data-testid="release-confirm"]').trigger('click');await flushPromises()
  expect(releaseIPAudit).toHaveBeenCalledWith(userEntry, 'operator_review')
  wrapper.unmount()
 })
 it('reports failed release and re-reads state without claiming success', async () => {
  vi.mocked(releaseIPAudit).mockRejectedValue(new Error('conflict'))
  const wrapper = render();await flushPromises()
  await wrapper.get('[data-testid="release-source"]').trigger('click');await wrapper.get('[data-testid="release-confirm"]').trigger('click');await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toBe(en.releaseError)
  expect(listIPAudit).toHaveBeenCalledTimes(2)
  expect(wrapper.find('[data-testid="release-outcome"]').exists()).toBe(false)
  wrapper.unmount()
 })
 it('never repeats an uncertain release while awaiting reconciliation', async () => {
  let rejectRelease!: (cause: Error) => void
  vi.mocked(releaseIPAudit).mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRelease = reject }))
  const wrapper = render(); await flushPromises()
  await wrapper.get('[data-testid="release-source"]').trigger('click')
  await wrapper.get('[data-testid="release-confirm"]').trigger('click')
  await wrapper.get('[data-testid="release-confirm"]').trigger('click')
  expect(releaseIPAudit).toHaveBeenCalledTimes(1)
  rejectRelease(new Error('timeout')); await flushPromises()
  expect(listIPAudit).toHaveBeenCalledTimes(2)
  expect(releaseIPAudit).toHaveBeenCalledTimes(1)
  expect(wrapper.find('[data-testid="release-dialog"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="release-outcome"]').exists()).toBe(false)
  expect(wrapper.get('[role="alert"]').text()).toBe(en.releaseError)
  wrapper.unmount()
 })
 it('keeps an unverified readback distinct from a completed release', async () => {
  vi.mocked(releaseIPAudit).mockResolvedValue({ released: false, readback: 'unavailable', remaining: [], unknown_sources: ['host_firewall'], unrestricted: false })
  const wrapper = render();await flushPromises()
  await wrapper.get('[data-testid="release-source"]').trigger('click');await wrapper.get('[data-testid="release-confirm"]').trigger('click');await flushPromises()
  expect(wrapper.get('[data-testid="release-outcome"]').text()).toContain(en.unverified)
  wrapper.unmount()
 })
 it('clearly reports a source that is still present after recovery', async () => {
  vi.mocked(releaseIPAudit).mockResolvedValue({ released: false, readback: 'still_present', remaining: [entry], unknown_sources: ['host_firewall'], unrestricted: false })
  const wrapper = render();await flushPromises()
  await wrapper.get('[data-testid="release-source"]').trigger('click');await wrapper.get('[data-testid="release-confirm"]').trigger('click');await flushPromises()
  expect(wrapper.get('[data-testid="release-outcome"]').text()).toContain(en.stillRestricted)
  expect(wrapper.get('[data-testid="release-outcome"]').text()).not.toContain(en.released)
  wrapper.unmount()
 })
 it('makes manual and disconnected sources read only, and clears stale rows on read failure', async () => {
  vi.mocked(listIPAudit).mockResolvedValue({ ...page(), items: [{ ...entry, source: 'manual_deny', can_release: false, manual: true }] })
  const wrapper = render();await flushPromises()
  expect(wrapper.find('[data-testid="release-source"]').exists()).toBe(false)
  vi.mocked(listIPAudit).mockRejectedValueOnce(new Error('offline'))
  await wrapper.get('form').trigger('submit');await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toBe(en.loadError)
  expect(wrapper.text()).not.toContain(entry.network)
  wrapper.unmount()
 })
 it('retains admin route guards, security sub-navigation, and legacy settings routes', () => {
  const router = readFileSync('src/router/index.ts', 'utf8')
  const route = router.slice(router.indexOf("path: '/admin/ip-audit'"), router.indexOf("path: '/admin/prompt-audit'"))
  expect(route).toContain('requiresAuth: true');expect(route).toContain('requiresAdmin: true')
  expect(route).not.toContain('requiresRiskControl')
  const sidebar = readFileSync('src/navigation/useConsoleNavSource.ts', 'utf8')
  expect(sidebar).toContain("path: '/admin/ip-audit'")
  expect(router).toContain("path: '/admin/settings'")
  expect(auditTab('ip')).toBe('ip');expect(auditTab('config')).toBe('config');expect(auditTab('unknown')).toBe('events')
 })
})

it('keeps a demoted release action reachable in persisted row details without releasing on expand', async () => {
  let wrapper = render(); await flushPromises()
  wrapper.getComponent(ColumnSettings).vm.$emit('toggle', 'actions'); await flushPromises()
  expect(wrapper.find('th[data-column-key="actions"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="release-source"]').exists()).toBe(false)
  expect(JSON.parse(localStorage.getItem('admin-ip-audit-records')!).primary.actions).toBe(false)
  await wrapper.get('[data-test="toggle-details"]').trigger('click')
  expect(releaseIPAudit).not.toHaveBeenCalled()
  await wrapper.get('[data-testid="release-source"]').trigger('click')
  expect(wrapper.get('[data-testid="release-dialog"]').text()).toContain(entry.network)
  expect(releaseIPAudit).not.toHaveBeenCalled()
  wrapper.unmount()
  wrapper = render(); await flushPromises()
  expect(wrapper.find('th[data-column-key="actions"]').exists()).toBe(false)
  await wrapper.get('[data-test="toggle-details"]').trigger('click')
  expect(wrapper.find('[data-testid="release-source"]').exists()).toBe(true)
  wrapper.unmount()
})
