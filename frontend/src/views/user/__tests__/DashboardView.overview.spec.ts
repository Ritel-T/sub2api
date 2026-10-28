import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '../DashboardView.vue'
import type { ApiKey, Group, PublicSettings } from '@/types'

const api = vi.hoisted(() => ({ stats: vi.fn(), listKeys: vi.fn(), createKey: vi.fn(), groups: vi.fn(), recent: vi.fn(), errors: vi.fn(), subscriptions: vi.fn(), user: vi.fn() }))
const auth = reactive({ user: { id: 1 }, token: 'first', isSimpleMode: false, refreshUserIfCurrent: api.user })
const app = reactive({ cachedPublicSettings: { subscription_enabled: true, payment_enabled: true, allow_user_view_error_requests: true } as Partial<PublicSettings> })
const locale = ref('en')
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ locale, t: (key: string) => key }) }))
vi.mock('@/api/usage', () => ({ usageAPI: { getDashboardStats: api.stats, query: api.recent, listMyErrorRequests: api.errors } }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: api.listKeys, create: api.createKey } }))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getAvailable: api.groups } }))
vi.mock('@/api/subscriptions', () => ({ getSubscriptionSummary: api.subscriptions }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/keys/UseKeyModal.vue', () => ({ default: { props: ['apiKey', 'keyRecord', 'platform', 'show'], template: '<div data-test="use-dialog">{{ keyRecord.name }} {{ platform }}</div>' } }))
vi.mock('@/components/user/UserErrorDetailModal.vue', () => ({ default: { template: '<div />' } }))
enableAutoUnmount(afterEach)
const group = { id: 12, name: 'OpenAI default', platform: 'openai', status: 'active' } as Group
function key(overrides: Partial<ApiKey> = {}): ApiKey {
  return { id: 4, key: 'test-key', name: 'Production', status: 'active', expires_at: null, quota: 0, quota_used: 0, group_id: 12, group, created_at: '2026-10-25T12:00:00Z', ...overrides } as ApiKey
}
const statistics = { total_api_keys: 1, active_api_keys: 1, today_requests: 4, today_tokens: 1800, today_actual_cost: 0.25 }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: ['/dashboard', '/usage', '/keys', '/purchase', '/subscriptions', '/affiliate'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push('/dashboard'); await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks()
  auth.user = { id: 1 }; auth.token = 'first'; auth.isSimpleMode = false
  app.cachedPublicSettings = { subscription_enabled: true, payment_enabled: true, allow_user_view_error_requests: true }; locale.value = 'en'
  api.stats.mockResolvedValue(statistics)
  api.user.mockResolvedValue({ id: 1, balance: 25 })
  api.listKeys.mockResolvedValue({ total: 1, items: [key()] })
  api.createKey.mockResolvedValue(key({ id: 8, name: 'OpenAI Key' }))
  api.groups.mockResolvedValue([group])
  api.recent.mockResolvedValue({ total: 1, items: [{ id: 9, request_id: 'r9', model: 'gpt-test', created_at: '2026-10-25T10:00:00Z', input_tokens: 10, output_tokens: 2, cache_creation_tokens: 3, cache_read_tokens: 5, duration_ms: 1400, total_cost: 0.5, actual_cost: 0.25 }] })
  api.errors.mockResolvedValue({ total: 0, items: [] })
  api.subscriptions.mockResolvedValue({ active_count: 0, subscriptions: [] })
})

describe('overview quick workflow', () => {
  it('shows the referral invitation only while referrals are enabled', async () => {
    const wrapper = await render()
    expect(wrapper.find('[data-test="referral-entry"]').exists()).toBe(false)
    app.cachedPublicSettings.affiliate_enabled = true
    await flushPromises()
    expect(wrapper.find('[data-test="referral-entry"]').attributes('href')).toBe('/affiliate')
    expect(wrapper.find('[data-test="referral-entry"]').text()).toContain('Invite friends')
  })
  it('states both $5 rewards with their actual signup and first-recharge triggers', async () => {
    app.cachedPublicSettings = { affiliate_enabled: true, affiliate_signup_bonus: 5, affiliate_first_recharge_bonus: 5, affiliate_first_recharge_min_amount: 8 }
    const wrapper = await render()
    const entry = wrapper.get('[data-test="referral-entry"]')
    expect(entry.get('h2').text()).toBe('You and your friend each get $5')
    expect(entry.text()).toContain('when they sign up through your link')
    expect(entry.text()).toContain('first top-up of $8 or more')
    expect(api.createKey).not.toHaveBeenCalled()
    locale.value = 'zh'
    await flushPromises()
    expect(entry.get('h2').text()).toBe('邀请好友，双方各得 $5')
    expect(entry.text()).toContain('首次充值满 $8')
  })
  it('uses live reward amounts and does not promise a reward when its configuration is absent or disabled', async () => {
    app.cachedPublicSettings = { affiliate_enabled: true, affiliate_signup_bonus: 2, affiliate_first_recharge_bonus: 7, affiliate_first_recharge_min_amount: 10 }
    const wrapper = await render()
    expect(wrapper.get('[data-test="referral-entry"]').text()).toContain('friend gets $2')
    expect(wrapper.get('[data-test="referral-entry"]').text()).toContain('You get $7')
    app.cachedPublicSettings = { affiliate_enabled: true }
    await flushPromises()
    expect(wrapper.get('[data-test="referral-entry"]').text()).not.toContain('$5')
    app.cachedPublicSettings.affiliate_enabled = false
    await flushPromises()
    expect(wrapper.find('[data-test="referral-entry"]').exists()).toBe(false)
  })
  it('shows balance with top-up, three newest keys and five latest requests without charts', async () => {
    const wrapper = await render()
    expect(wrapper.find('[data-test="balance"]').text()).toContain('$25.00')
    expect(wrapper.find('[data-test="spending"]').text()).toContain('$0.2500')
    expect(wrapper.find('[data-test="requests"]').text()).toContain('4')
    expect(wrapper.find('[data-test="tokens"]').text()).toContain('1,800')
    expect(wrapper.find('.dashboard-page-actions a').attributes('href')).toBe('/purchase')
    expect(wrapper.find('a[href="/usage?view=records"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('gpt-test')
    expect(api.listKeys).toHaveBeenCalledTimes(1)
    expect(api.listKeys).toHaveBeenCalledWith(1, 3, { sort_by: 'created_at', sort_order: 'desc' })
    expect(api.recent).toHaveBeenCalledWith({ page: 1, page_size: 5, sort_by: 'created_at', sort_order: 'desc' })
  })
  it('creates using first eligible ordered group per platform with no optional limits, then opens use dialog', async () => {
    api.groups.mockResolvedValue([group, { ...group, id: 16 }, { ...group, id: 3, platform: 'anthropic', status: 'inactive' }])
    const wrapper = await render()
    expect(wrapper.findAll('[data-platform]')).toHaveLength(1)
    await wrapper.find('[data-platform="openai"]').trigger('click'); await flushPromises()
    expect(api.createKey).toHaveBeenCalledTimes(1)
    expect(api.createKey).toHaveBeenCalledWith('OpenAI Key', 12)
    expect(wrapper.find('[data-test="use-dialog"]').text()).toContain('OpenAI Key openai')
  })
  it('offers Grok and one domestic entry using the first eligible domestic group with real platform preserved', async () => {
    api.groups.mockResolvedValue([group, { ...group, id: 21, platform: 'grok' }, { ...group, id: 17, platform: 'deepseek' }, { ...group, id: 18, platform: 'kimi' }])
    api.createKey.mockResolvedValue(key({ id: 90, name: 'Chinese models Key', group_id: 17, group: undefined }))
    const wrapper = await render()
    expect(wrapper.find('[data-platform="grok"] svg').exists()).toBe(true)
    expect(wrapper.findAll('[data-platform="domestic"]')).toHaveLength(1)
    expect(wrapper.find('[data-platform="domestic"]').findAll('svg')).toHaveLength(1)
    expect(wrapper.find('[data-platform="domestic"]').text()).toContain('Chinese models')
    await wrapper.find('[data-platform="domestic"]').trigger('click'); await flushPromises()
    expect(api.createKey).toHaveBeenCalledWith('Chinese models Key', 17)
    expect(wrapper.find('[data-test="use-dialog"]').text()).toContain('deepseek')
  })
  it('prefers the available active Grok 060 group by its current name without a fixed ID', async () => {
    api.groups.mockResolvedValue([
      { ...group, id: 61, name: 'Grok Premium', platform: 'grok' },
      { ...group, id: 806, name: 'Grok 060', platform: 'grok' },
      { ...group, id: 32, name: 'Grok 060', platform: 'grok', status: 'inactive' }
    ])
    const wrapper = await render()
    expect(wrapper.findAll('[data-platform="grok"]')).toHaveLength(1)
    await wrapper.get('[data-platform="grok"]').trigger('click'); await flushPromises()
    expect(api.createKey).toHaveBeenCalledTimes(1)
    expect(api.createKey).toHaveBeenCalledWith('Grok Key', 806)
  })
  it('falls back to the first eligible Grok group if Grok 060 is unavailable', async () => {
    api.groups.mockResolvedValue([
      { ...group, id: 32, name: 'Grok 060', platform: 'grok', status: 'inactive' },
      { ...group, id: 75, name: 'Grok Available', platform: 'grok' },
      { ...group, id: 81, name: 'Grok Other', platform: 'grok' }
    ])
    const wrapper = await render()
    await wrapper.get('[data-platform="grok"]').trigger('click'); await flushPromises()
    expect(api.createKey).toHaveBeenCalledTimes(1)
    expect(api.createKey).toHaveBeenCalledWith('Grok Key', 75)
  })
  it('prevents duplicate creation while pending and ignores a late result after owner changes', async () => {
    const pending = deferred<ApiKey>(); api.createKey.mockReturnValue(pending.promise)
    const wrapper = await render()
    await wrapper.find('[data-platform="openai"]').trigger('click')
    await wrapper.find('[data-platform="openai"]').trigger('click')
    expect(api.createKey).toHaveBeenCalledTimes(1)
    auth.user = { id: 2 }; auth.token = 'second'; await flushPromises()
    pending.resolve(key({ name: 'Old account secret' })); await flushPromises()
    expect(wrapper.find('[data-test="use-dialog"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Old account secret')
  })
  it('keeps creation pending across normal same-user token rotation', async () => {
    const pending = deferred<ApiKey>(); api.createKey.mockReturnValue(pending.promise)
    const wrapper = await render()
    await wrapper.find('[data-platform="openai"]').trigger('click')
    auth.token = 'refreshed-same-user'; await flushPromises()
    expect(wrapper.find('[data-platform="openai"]').attributes('disabled')).toBeDefined()
    expect(api.listKeys).toHaveBeenCalledTimes(1)
    pending.resolve(key({ name: 'Created after refresh' })); await flushPromises()
    expect(wrapper.find('[data-test="use-dialog"]').text()).toContain('Created after refresh')
    expect(api.createKey).toHaveBeenCalledTimes(1)
  })
  it('does not blindly retry an uncertain creation and links to key management', async () => {
    api.createKey.mockRejectedValue(new Error('timeout'))
    const wrapper = await render()
    await wrapper.find('[data-platform="openai"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-platform="openai"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[role="alert"]').text()).toContain('Check your API keys')
    expect(api.createKey).toHaveBeenCalledTimes(1)
  })
  it('allows retry after a definite rejected creation', async () => {
    api.createKey.mockRejectedValue({ status: 403, message: 'Forbidden' })
    const wrapper = await render()
    await wrapper.find('[data-platform="openai"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-platform="openai"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').text()).toContain('Could not create')
  })
  it('opens the same use dialog for each recent key, without rendering raw keys in the list', async () => {
    api.listKeys.mockResolvedValue({ total: 4, items: [key({ id: 8, name: 'Newest' }), key({ id: 6, name: 'Second' }), key()] })
    const wrapper = await render()
    expect(wrapper.findAll('[data-key-id]')).toHaveLength(3)
    expect(wrapper.text()).not.toContain('test-key')
    await wrapper.find('[data-key-id="6"] button').trigger('click')
    expect(wrapper.find('[data-test="use-dialog"]').text()).toContain('Second')
  })
  it('keeps loading distinct from empty data and leaves platform creation independent of key list', async () => {
    api.listKeys.mockReturnValue(new Promise(() => {})); api.user.mockReturnValue(new Promise(() => {})); api.stats.mockReturnValue(new Promise(() => {}))
    const wrapper = await render()
    expect(wrapper.text()).not.toContain('No API keys yet')
    expect(wrapper.text()).not.toContain('$0.00')
    expect(wrapper.find('[data-platform="openai"]').attributes('disabled')).toBeUndefined()
  })
  it('shows recoverable section errors without treating failures as zero or empty', async () => {
    api.stats.mockRejectedValue(new Error('offline')); api.recent.mockRejectedValue(new Error('offline')); api.listKeys.mockRejectedValue(new Error('offline')); api.user.mockRejectedValue(new Error('offline')); api.groups.mockRejectedValue(new Error('offline'))
    const wrapper = await render()
    expect(wrapper.findAll('[role="alert"]').length).toBeGreaterThanOrEqual(5)
    expect(wrapper.text()).not.toContain('No requests yet')
    expect(wrapper.text()).not.toContain('No balance or active subscription')
    expect(wrapper.find('[data-platform]').exists()).toBe(false)
    api.listKeys.mockResolvedValue({ total: 0, items: [] })
    await wrapper.find('section[aria-labelledby="recent-keys-title"] button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('No API keys yet')
  })
  it('keeps recent failures without requesting the removed attention summary', async () => {
    api.errors.mockResolvedValue({ total: 7, items: [] })
    const wrapper = await render()
    expect(wrapper.find('[aria-labelledby="attention-title"]').exists()).toBe(false)
    expect(api.subscriptions).not.toHaveBeenCalled()
    expect(api.errors).toHaveBeenCalledTimes(1)
    expect(api.errors).toHaveBeenCalledWith({ page: 1, page_size: 5, sort_by: 'created_at', sort_order: 'desc' })
  })
  it('honors simple mode, disabled subscriptions/payments and error visibility', async () => {
    auth.isSimpleMode = true
    app.cachedPublicSettings.allow_user_view_error_requests = false
    const wrapper = await render()
    expect(api.subscriptions).not.toHaveBeenCalled(); expect(api.user).not.toHaveBeenCalled(); expect(api.errors).not.toHaveBeenCalled()
    expect(wrapper.find('a[href="/purchase"]').exists()).toBe(false)
    wrapper.unmount()
    auth.isSimpleMode = false; app.cachedPublicSettings = { subscription_enabled: false, payment_enabled: false }
    const disabled = await render()
    expect(api.subscriptions).not.toHaveBeenCalled()
    expect(disabled.find('a[href="/purchase"]').exists()).toBe(false)
  })
  it('ignores list and balance responses from a previous owner', async () => {
    const old = deferred<{ total: number; items: ApiKey[] }>(); const oldBalance = deferred<{ id: number; balance: number }>()
    api.listKeys.mockReturnValueOnce(old.promise); api.user.mockReturnValueOnce(oldBalance.promise)
    const wrapper = await render()
    api.listKeys.mockResolvedValue({ total: 0, items: [] }); api.user.mockResolvedValue({ id: 2, balance: 19 })
    auth.user = { id: 2 }; auth.token = 'second'; await flushPromises()
    old.resolve({ total: 1, items: [key({ name: 'Other account secret' })] }); oldBalance.resolve({ id: 1, balance: 987654 }); await flushPromises()
    expect(wrapper.text()).not.toContain('Other account secret')
    expect(wrapper.text()).not.toContain('987,654')
    expect(wrapper.find('[data-test="balance"]').text()).toContain('$19.00')
  })
  it('refreshes on explicit request with no timer polling', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = await render()
      await vi.advanceTimersByTimeAsync(120000)
      expect(api.recent).toHaveBeenCalledTimes(1)
      await wrapper.find('header button').trigger('click'); await flushPromises()
      expect(api.recent).toHaveBeenCalledTimes(2)
      expect(api.listKeys).toHaveBeenCalledTimes(2)
    } finally { vi.useRealTimers() }
  })
  it('uses both Chinese variants without changing destinations', async () => {
    locale.value = 'zh-Hant-HK'
    const wrapper = await render()
    expect(wrapper.find('h1').text()).toBe('總覽')
    expect(wrapper.text()).toContain('按平台建立 Key')
    locale.value = 'zh'; await flushPromises()
    expect(wrapper.find('h1').text()).toBe('总览')
    expect(wrapper.text()).toContain('按平台创建 Key')
    expect(wrapper.find('.dashboard-page-actions a').attributes('href')).toBe('/purchase')
  })

})
