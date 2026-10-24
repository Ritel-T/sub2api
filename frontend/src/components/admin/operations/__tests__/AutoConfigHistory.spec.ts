import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import AutoConfigHistory from '../AutoConfigHistory.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import { getAutoConfigEvents, type AutoConfigEvent, type AutoConfigEventsPage } from '@/api/admin/autoConfig'

vi.mock('vue-i18n', async () => {
  const { default: autoConfig } = await import('@/i18n/locales/zh/autoConfig')
  return { useI18n: () => ({ t: (key: string, params: Record<string, unknown> = {}) => {
    const message = key.split('.').reduce<any>((value, part) => value?.[part], { autoConfig, common: { loading: '加载中' } })
    return typeof message === 'string' ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
  } }) }
})
const state = vi.hoisted(() => ({ auth: null as any }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/api/admin/autoConfig', () => ({ getAutoConfigEvents: vi.fn() }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
const event = (id: number, kind: AutoConfigEvent['kind'] = 'concurrency_upgraded'): AutoConfigEvent => ({
  id, account_id: 7, account_name: 'OAuth example', platform: 'openai', kind, created_at: '2026-09-29T02:10:00Z',
  details: { priority: 1, load_factor: 10000, concurrency: 6, previous_concurrency: 5, cooldown_seconds: 60, group_ids: [3] }
})
let wrapper: VueWrapper | undefined
function render(attach = false) {
  wrapper = mount(AutoConfigHistory, { props: { refreshKey: 0 }, attachTo: attach ? document.body : undefined })
  return wrapper
}
beforeEach(() => {
  localStorage.clear()
  vi.resetAllMocks()
  state.auth = reactive({ user: { id: 1, role: 'admin' } })
  vi.mocked(getAutoConfigEvents).mockResolvedValue({ items: [], has_more: false })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })
describe('AutoConfigHistory', () => {
  it('renders initial mappings and the saved mapping template', async () => {
    const initial = event(11, 'initial_applied')
    initial.details.model_mapping = { 'gpt-5.4': 'gpt-5.5' }
    const saved = event(10, 'config_saved')
    saved.details.config = { enabled: false, platform: 'openai', priority: 50, load_factor: 1, concurrency: 3, group_ids: [], upgrade_enabled: false, upgrade_group_ids: [], successes_per_step: 20, upgrade_step: 1, max_concurrency: 100, cooldown_seconds: 60, revision: 'saved', model_mappings: [{ from: 'gpt-5.4', to: 'gpt-5.5' }] }
    vi.mocked(getAutoConfigEvents).mockResolvedValue({ items: [initial, saved], has_more: false })
    const w = render(); await flushPromises()
    expect(w.findAll('[data-testid=history-row]')[0].text()).toContain('模型映射：gpt-5.4 → gpt-5.5')
    expect(w.get('details').text()).toContain('模型映射')
    expect(w.get('details').text()).toContain('gpt-5.4 → gpt-5.5')
  })

  it('shows an honest empty state without invented history', async () => {
    const w = render(); await flushPromises()
    expect(w.get('[data-testid=history-empty]').text()).toContain('暂无自动配置日志')
    expect(w.text()).toContain('仅记录此功能上线后的操作')
    expect(getAutoConfigEvents).toHaveBeenCalledWith({ limit: 20, kind: '', before: undefined })
  })
  it('renders account identity, concurrency changes, cooldown and initial values', async () => {
    vi.mocked(getAutoConfigEvents).mockResolvedValue({ items: [event(8), event(7, 'failure_cooldown'), event(6, 'initial_applied')], has_more: false })
    const w = render(); await flushPromises()
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(3)
    expect(w.text()).toContain('5 → 6')
    expect(w.text()).toContain('成功进度已清零')
    expect(w.text()).toContain('加入分组 #3')
    expect(w.text()).toContain('OAuth example')
    expect(w.text()).toContain('#7')
  })
  it('shows the complete saved rule and group snapshot, including zero priority', async () => {
    const saved = event(9, 'config_saved')
    saved.account_id = 0
    saved.details.config = { model_mappings: [{ from: 'gpt-5.4', to: 'gpt-6-luna' }], model_billing: { enabled: true, rules: [{ model: 'gpt-6-luna*', multiplier: 10 }] }, enabled: true, platform: 'openai', priority: 0, load_factor: 10000, concurrency: 5, group_ids: [3], upgrade_enabled: true, upgrade_group_ids: [5], successes_per_step: 20, upgrade_step: 2, max_concurrency: 100, cooldown_seconds: 60, revision: 'saved' }
    vi.mocked(getAutoConfigEvents).mockResolvedValue({ items: [saved], has_more: false })
    const w = render(); await flushPromises()
    expect(w.get('[data-testid=history-row]').text()).toContain('全局配置')
    expect(w.get('[data-testid=history-row]').text()).toContain('优先级 0')
    expect(w.get('details').text()).toContain('连续成功 20 次，提升 2 并发，上限 100，冷却 60 秒')
    expect(w.get('details').text()).toContain('模型计价：开启')
    expect(w.get('details').text()).toContain('gpt-6-luna* · 10×')
    expect(w.get('details').text()).toContain('gpt-5.4 → gpt-6-luna')
    expect(w.get('details').text()).toContain('首次加入分组：#3')
    expect(w.get('details').text()).toContain('允许升级的账号分组：#5')
  })
  it('rebuilds the cursor chain and refreshes all loaded pages atomically', async () => {
    vi.mocked(getAutoConfigEvents).mockResolvedValueOnce({ items: [event(8)], has_more: true }).mockResolvedValueOnce({ items: [event(7)], has_more: true }).mockResolvedValueOnce({ items: [event(9)], has_more: true }).mockResolvedValueOnce({ items: [event(8)], has_more: true })
    const w = render(); await flushPromises()
    w.getComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises()
    expect(getAutoConfigEvents).toHaveBeenLastCalledWith({ limit: 20, kind: '', before: 8 })
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(2)
    await w.get('[data-testid=history-refresh]').trigger('click'); await flushPromises()
    expect(vi.mocked(getAutoConfigEvents).mock.calls.map(call => call[0]?.before)).toEqual([undefined, 8, undefined, 9])
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(2)
    expect(w.getComponent(InfiniteListSentinel).props('hasMore')).toBe(true)
  })
  it('preserves the whole loaded window when the second refresh page fails and retries the refresh', async () => {
    vi.mocked(getAutoConfigEvents).mockResolvedValueOnce({ items: [event(8)], has_more: true }).mockResolvedValueOnce({ items: [event(7)], has_more: true }).mockResolvedValueOnce({ items: [event(9)], has_more: true }).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ items: [event(10)], has_more: true }).mockResolvedValueOnce({ items: [event(9)], has_more: false })
    const w = render(); await flushPromises()
    w.getComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises()
    await w.get('[data-testid=history-refresh]').trigger('click'); await flushPromises()
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(2)
    expect(w.get('[role=alert]').text()).toContain('日志加载失败')
    await w.get('[data-testid=history-retry]').trigger('click'); await flushPromises()
    expect(vi.mocked(getAutoConfigEvents).mock.calls.map(call => call[0]?.before)).toEqual([undefined, 8, undefined, 9, undefined, 10])
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(2)
    expect(w.getComponent(InfiniteListSentinel).props('hasMore')).toBe(false)
  })
  it('ignores an older pending response after changing filter', async () => {
    let resolve!: (page: AutoConfigEventsPage) => void
    vi.mocked(getAutoConfigEvents).mockReturnValueOnce(new Promise(r => { resolve = r })).mockResolvedValueOnce({ items: [event(7, 'failure_cooldown')], has_more: false })
    const w = render(); await flushPromises()
    await w.get('[data-testid=history-kind]').setValue('failure_cooldown'); await flushPromises()
    resolve({ items: [event(8)], has_more: true }); await flushPromises()
    expect(getAutoConfigEvents).toHaveBeenLastCalledWith({ limit: 20, kind: 'failure_cooldown', before: undefined })
    expect(w.get('[data-testid=history-row]').text()).toContain('失败冷却')
    expect(w.getComponent(InfiniteListSentinel).props('hasMore')).toBe(false)
  })
  it('preserves loaded rows after a pagination error and retries that cursor', async () => {
    vi.mocked(getAutoConfigEvents).mockResolvedValueOnce({ items: [event(8)], has_more: true }).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ items: [event(7)], has_more: false })
    const w = render(); await flushPromises()
    w.getComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises()
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(1)
    expect(w.get('[role=alert]').text()).toContain('日志加载失败')
    await w.get('[data-testid=history-retry]').trigger('click'); await flushPromises()
    expect(getAutoConfigEvents).toHaveBeenLastCalledWith({ limit: 20, kind: '', before: 8 })
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(2)
  })
  it('refreshes after a successful save signal without changing the filter', async () => {
    const w = render(); await flushPromises()
    await w.get('[data-testid=history-kind]').setValue('config_saved'); await flushPromises()
    await w.setProps({ refreshKey: 1 }); await flushPromises()
    expect(getAutoConfigEvents).toHaveBeenCalledTimes(3)
    expect(getAutoConfigEvents).toHaveBeenLastCalledWith({ limit: 20, kind: 'config_saved', before: undefined })
  })
  it('clears private history and discards pending responses after logout', async () => {
    let resolve!: (page: AutoConfigEventsPage) => void
    vi.mocked(getAutoConfigEvents).mockReturnValueOnce(new Promise(r => { resolve = r }))
    const w = render(); await flushPromises(); state.auth.user = null
    resolve({ items: [event(8)], has_more: true }); await flushPromises()
    expect(w.findAll('[data-testid=history-row]')).toHaveLength(0)
    expect(getAutoConfigEvents).toHaveBeenCalledTimes(1)
  })
  it('does not fetch logs for a non-admin', async () => {
    state.auth.user.role = 'user'; render(); await flushPromises()
    expect(getAutoConfigEvents).not.toHaveBeenCalled()
  })
})

it('retains saved configuration details after the detail field is demoted and restored from preferences', async () => {
  const saved = event(10, 'config_saved')
  saved.details.config = { enabled: false, platform: 'openai', priority: 0, load_factor: 1, concurrency: 3, group_ids: [3], upgrade_enabled: false, upgrade_group_ids: [5], successes_per_step: 20, upgrade_step: 1, max_concurrency: 100, cooldown_seconds: 60, revision: 'saved', model_mappings: [{ from: 'gpt-5.4', to: 'gpt-6-luna' }] }
  vi.mocked(getAutoConfigEvents).mockResolvedValue({ items: [saved], has_more: false })
  let w = render(); await flushPromises()
  w.getComponent(ColumnSettings).vm.$emit('toggle', 'detail'); await flushPromises()
  expect(w.find('th[data-column-key="detail"]').exists()).toBe(false)
  expect(w.find('details').exists()).toBe(false)
  await w.get('[data-test="toggle-details"]').trigger('click')
  expect(w.get('details').text()).toContain('gpt-5.4 → gpt-6-luna')
  expect(w.get('details').text()).toContain('允许升级的账号分组：#5')
  w.unmount(); wrapper = undefined
  w = render(true); await flushPromises()
  expect(w.find('th[data-column-key="detail"]').exists()).toBe(false)
  await w.get('[data-test="toggle-details"]').trigger('click')
  await w.get('details').trigger('click')
  expect(w.get('[data-test="toggle-details"]').attributes('aria-expanded')).toBe('true')
  expect(w.get('details').text()).toContain('gpt-5.4 → gpt-6-luna')
  const snapshot = w.get('details summary').element as HTMLElement
  snapshot.focus()
  expect(document.activeElement).toBe(snapshot)
  let resolveRefresh!: (page: AutoConfigEventsPage) => void
  vi.mocked(getAutoConfigEvents).mockReturnValueOnce(new Promise(resolve => { resolveRefresh = resolve }))
  await w.setProps({ refreshKey: 1 })
  expect(w.get('[data-testid="auto-config-history"]').attributes('aria-busy')).toBe('true')
  expect(w.get('details summary').element).toBe(snapshot)
  expect(document.activeElement).toBe(snapshot)
  resolveRefresh({ items: [saved], has_more: false }); await flushPromises()
  expect(w.get('details summary').element).toBe(snapshot)
  expect(document.activeElement).toBe(snapshot)
})
