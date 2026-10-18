import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '../DashboardView.vue'
import { selectOverviewKey } from '@/components/user/dashboard/overviewState'
import type { ApiKey } from '@/types'

const api = vi.hoisted(() => ({ stats: vi.fn(), listKeys: vi.fn(), recent: vi.fn(), errors: vi.fn(), subscriptions: vi.fn(), user: vi.fn(), quotas: vi.fn(), trend: vi.fn(), models: vi.fn() }))
const auth = reactive({ user: { id: 1 }, token: 'first', isSimpleMode: false, refreshUserIfCurrent: api.user })
const app = reactive({ cachedPublicSettings: { subscription_enabled: true, payment_enabled: true } })
const locale = ref('en')
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ locale, t: (key: string) => key }) }))
vi.mock('@/api/usage', () => ({ usageAPI: { getDashboardStats: api.stats, query: api.recent, listMyErrorRequests: api.errors, getDashboardTrend: api.trend, getDashboardModels: api.models } }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: api.listKeys } }))
vi.mock('@/api/subscriptions', () => ({ getSubscriptionSummary: api.subscriptions }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: api.quotas }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/user/dashboard/UserDashboardStats.vue', () => ({ default: { props: ['balance'], template: '<div data-test="full-stats">{{ balance }}</div>' } }))
vi.mock('@/components/user/dashboard/UserDashboardCharts.vue', () => ({ default: { template: '<div data-test="charts" />' } }))
enableAutoUnmount(afterEach)

function key(overrides: Partial<ApiKey> = {}): ApiKey {
  return { id: 4, name: 'Production', status: 'active', expires_at: null, quota: 0, quota_used: 0, last_used_at: '2026-10-17T12:00:00Z', ...overrides } as ApiKey
}
const statistics = { total_api_keys: 1, active_api_keys: 1, today_requests: 4, today_tokens: 1800, today_actual_cost: 0.25 }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: ['/dashboard', '/usage', '/keys', '/purchase', '/subscriptions'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push('/dashboard'); await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  auth.user = { id: 1 }; auth.token = 'first'; auth.isSimpleMode = false
  app.cachedPublicSettings = { subscription_enabled: true, payment_enabled: true }; locale.value = 'en'
  api.stats.mockResolvedValue(statistics)
  api.user.mockResolvedValue({ id: 1, balance: 25 })
  api.listKeys.mockImplementation((_page: number, _size: number, filters?: unknown) => Promise.resolve({ total: 1, items: filters ? [key()] : [key()] }))
  api.recent.mockResolvedValue({ total: 1, items: [{ id: 9, request_id: 'r9', model: 'gpt-test', created_at: '2026-10-17T10:00:00Z', input_tokens: 10, output_tokens: 2, total_cost: 0.5, actual_cost: 0.25 }] })
  api.errors.mockResolvedValue({ total: 0, items: [] })
  api.subscriptions.mockResolvedValue({ active_count: 0, subscriptions: [] })
  api.quotas.mockResolvedValue({ platform_quotas: [] })
  api.trend.mockResolvedValue({ trend: [] }); api.models.mockResolvedValue({ models: [] })
})

describe('real overview workflow', () => {
  it('uses an existing active key, paginates five recent requests, and preserves real request details links', async () => {
    const wrapper = await render()
    expect(wrapper.find('a[href="/keys?connect=1&key_id=4"]').text()).toBe('Connect an app')
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Needs attention')
    expect(api.recent).toHaveBeenCalledWith({ page: 1, page_size: 5, sort_by: 'created_at', sort_order: 'desc' })
    expect(wrapper.find('a[href="/usage?view=records&usage_id=9"]').text()).toBe('gpt-test')
    expect(wrapper.find('a[href="/usage?view=analysis"]').exists()).toBe(true)
    expect(api.trend).not.toHaveBeenCalled()
  })
  it('finds a previously used key beyond a page of unused keys', async () => {
    api.listKeys.mockImplementation((page: number, _size: number, filters?: unknown) => Promise.resolve(!filters ? { total: 51, items: [] }
      : page === 1 ? { total: 51, items: Array.from({ length: 50 }, (_, index) => key({ id: index + 10, last_used_at: null })) }
        : { total: 51, items: [key()] }))
    const wrapper = await render()
    expect(wrapper.find('a[href="/keys?connect=1&key_id=4"]').exists()).toBe(true)
    expect(api.listKeys).toHaveBeenCalledTimes(3)
  })
  it('offers management instead of creation when existing keys are unusable', async () => {
    api.listKeys.mockImplementation((_page: number, _size: number, filters?: unknown) => Promise.resolve({ total: filters ? 0 : 1, items: [] }))
    const wrapper = await render()
    expect(wrapper.find('a[href="/keys"]').text()).toBe('Manage API keys')
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(false)
  })
  it('offers creation only after the server confirms no keys exist', async () => {
    api.listKeys.mockResolvedValue({ total: 0, items: [] })
    const wrapper = await render()
    expect(wrapper.find('a[href="/keys?create=1"]').text()).toBe('Create API key')
    expect(api.listKeys).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).not.toContain('No balance or active subscription')
  })
  it('keeps unresolved data distinct from zero and does not show onboarding while keys load', async () => {
    const pending = deferred<{ total: number; items: ApiKey[] }>()
    api.listKeys.mockReturnValue(pending.promise); api.user.mockReturnValue(new Promise(() => {})); api.stats.mockReturnValue(new Promise(() => {}))
    const wrapper = await render()
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('$0.00')
    expect(wrapper.text()).not.toContain('No balance or active subscription')
    pending.resolve({ total: 0, items: [] }); await flushPromises()
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(true)
  })
  it('offers recoverable errors per section instead of replacing failures with empty or zero data', async () => {
    api.stats.mockRejectedValue(new Error('offline')); api.recent.mockRejectedValue(new Error('offline')); api.listKeys.mockRejectedValue(new Error('offline')); api.user.mockRejectedValue(new Error('offline')); api.errors.mockRejectedValue(new Error('offline'))
    const wrapper = await render()
    expect(wrapper.findAll('[role="alert"]').length).toBe(5)
    expect(wrapper.text()).not.toContain('No requests yet')
    expect(wrapper.text()).not.toContain('No balance or active subscription')
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(false)
    api.listKeys.mockResolvedValue({ total: 0, items: [] })
    await wrapper.find('section[aria-labelledby="continue-title"] button').trigger('click'); await flushPromises()
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(true)
  })
  it('shows actual failed count and subscription expiry only when returned by the API', async () => {
    api.errors.mockResolvedValue({ total: 7, items: [] })
    api.subscriptions.mockResolvedValue({ active_count: 1, subscriptions: [{ id: 1, group_name: 'Plan A', status: 'active', days_remaining: 2, expires_at: '2026-10-20T00:00:00Z' }] })
    api.user.mockResolvedValue({ id: 1, balance: 0 })
    const wrapper = await render()
    const attention = wrapper.find('section[aria-labelledby="attention-title"]')
    expect(attention.text()).toContain('Failed requests today 7')
    expect(attention.text()).toContain('Plan A')
    expect(attention.text()).not.toContain('No balance or active subscription')
    expect(attention.find('a').attributes('href')).toMatch(/^\/usage\?view=errors&start_date=/)
  })
  it('preserves simple mode and disabled subscription/payment contracts', async () => {
    auth.isSimpleMode = true
    const wrapper = await render()
    expect(api.subscriptions).not.toHaveBeenCalled(); expect(api.user).not.toHaveBeenCalled()
    expect(wrapper.find('a[href="/subscriptions"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/purchase"]').exists()).toBe(false)
    wrapper.unmount()
    auth.isSimpleMode = false; app.cachedPublicSettings = { subscription_enabled: false, payment_enabled: false }
    const disabled = await render()
    expect(api.subscriptions).not.toHaveBeenCalled()
    expect(disabled.find('a[href="/purchase"]').exists()).toBe(false)
    expect(disabled.find('a[href="/subscriptions"]').exists()).toBe(false)
  })
  it('ignores requests resolving after account changes', async () => {
    const old = deferred<{ total: number; items: ApiKey[] }>()
    api.listKeys.mockReturnValueOnce(old.promise)
    const wrapper = await render()
    api.listKeys.mockResolvedValue({ total: 0, items: [] })
    auth.user = { id: 2 }; auth.token = 'second'
    await flushPromises()
    old.resolve({ total: 1, items: [key({ name: 'Other account secret' })] }); await flushPromises()
    expect(wrapper.text()).not.toContain('Other account secret')
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(true)
  })
  it('uses Hong Kong traditional and simplified Chinese without changing data or destinations', async () => {
    locale.value = 'zh-HK'
    const wrapper = await render()
    expect(wrapper.find('h1').text()).toBe('總覽')
    expect(wrapper.find('a[href="/keys?connect=1&key_id=4"]').text()).toBe('連接應用程式')
    locale.value = 'zh'; await flushPromises()
    expect(wrapper.find('h1').text()).toBe('总览')
    expect(wrapper.find('a[href="/keys?connect=1&key_id=4"]').text()).toBe('连接应用')
  })
  it('loads original charts and platform quotas only after expanding statistics', async () => {
    const wrapper = await render()
    const details = wrapper.find('details')
    ;(details.element as HTMLDetailsElement).open = true
    await details.trigger('toggle'); await flushPromises()
    expect(api.trend).toHaveBeenCalledTimes(1); expect(api.models).toHaveBeenCalledTimes(1); expect(api.quotas).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-test="full-stats"]').text()).toBe('25')
  })
})

describe('overview key selection', () => {
  it('rejects disabled, expired and exhausted keys and prefers last use over new unused keys', () => {
    expect(selectOverviewKey([
      key({ id: 1, status: 'inactive' }), key({ id: 2, expires_at: '2020-01-01' }),
      key({ id: 3, quota: 10, quota_used: 10 }), key({ id: 5, last_used_at: null }),
      key({ id: 6, last_used_at: '2026-10-18T00:00:00Z' }),
    ])?.id).toBe(6)
    expect(selectOverviewKey([key({ status: 'expired' })])).toBeNull()
  })
})
