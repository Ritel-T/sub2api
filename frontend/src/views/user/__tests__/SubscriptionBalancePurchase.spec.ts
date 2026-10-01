import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import PaymentView from '../PaymentView.vue'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'
import { newBalancePurchase, readBalancePurchase, saveBalancePurchase } from '@/components/payment/balanceSubscriptionPurchase'
import type { SubscriptionPlan } from '@/types/payment'

const api = vi.hoisted(() => ({ checkout: vi.fn(), buy: vi.fn(), create: vi.fn(), refresh: vi.fn(), subscriptions: vi.fn(), quote: vi.fn(), refreshCurrent: vi.fn() }))
const state = vi.hoisted(() => ({ auth: null as unknown as { user: { id: number, username: string, balance: number }, token: string }, subscriptions: [] as Array<Record<string, unknown>> }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: { tab: 'subscription' } }), useRouter: () => ({ replace: vi.fn(), push: vi.fn(), resolve: () => ({ href: '/payment/result' }) }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { getCheckoutInfo: api.checkout, buySubscriptionWithBalance: api.buy, quote: api.quote } }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ createOrder: api.create }) }))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  state.auth = reactive({ user: { id: 7, username: 'fixture', balance: 500 }, token: 'token-7' })
  return { useAuthStore: () => ({ get user() { return state.auth.user }, get token() { return state.auth.token },
    refreshUser: api.refresh, refreshUserIfCurrent: api.refreshCurrent }) }
})
vi.mock('@/stores/subscriptions', () => ({ useSubscriptionStore: () => ({ get activeSubscriptions() { return state.subscriptions }, ownerUserId: 7, clear: vi.fn(), fetchActiveSubscriptionsIfCurrent: api.subscriptions, fetchActiveSubscriptions: api.subscriptions }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ cachedPublicSettings: { subscription_enabled: true }, showError: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn() }) }))

const plan: SubscriptionPlan = { id: 1, group_id: 3, group_platform: 'openai', group_name: 'fixture', name: 'Plus',
  description: '', price: 138, currency: 'CNY', validity_days: 30, validity_unit: 'day', features: [], for_sale: true, sort_order: 0 }

async function mountPage() {
  const wrapper = shallowMount(PaymentView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Teleport: true } } })
  await flushPromises()
  return wrapper
}
async function select(wrapper: ReturnType<typeof shallowMount>) {
  wrapper.findComponent(SubscriptionPlanCard).vm.$emit('select', plan)
  await flushPromises()
}

describe('subscription purchase using site credit', () => {
  beforeEach(() => {
    vi.restoreAllMocks(); vi.clearAllMocks(); sessionStorage.clear(); localStorage.clear()
    state.auth.user.id = 7; state.auth.user.balance = 500; state.auth.token = 'token-7'; state.subscriptions = []
    api.refresh.mockResolvedValue(undefined); api.refreshCurrent.mockResolvedValue(undefined); api.subscriptions.mockResolvedValue(undefined)
    api.buy.mockResolvedValue({ data: { order_id: 50, status: 'COMPLETED', site_credit_amount: 138, renewed: false } })
    api.checkout.mockResolvedValue({ data: { payment_enabled: true, methods: { squarespace: { currency: 'GBP', available: true, single_min: .5 } },
      plans: [plan], global_min: 1, global_max: 0, balance_disabled: false, balance_retail_pricing_enabled: true,
      balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 99, recharge_fee_rate: 99 } })
  })
  it('uses the CNY plan price as site credit without a second fee or gateway order', async () => {
    const wrapper = await mountPage(); await select(wrapper)
    expect(wrapper.get('[data-test="balance-subscription-summary"]').text()).toContain('$138.00')
    expect(wrapper.text()).toContain('¥138.00')
    expect(wrapper.findComponent(PaymentMethodSelector).exists()).toBe(false)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy).toHaveBeenCalledWith(expect.objectContaining({ expected_user_id: 7, plan_id: 1, expected_price: 138, expected_currency: 'CNY', expected_group_id: 3, expected_validity_days: 30, expected_validity_unit: 'day' }))
    expect(api.create).not.toHaveBeenCalled(); expect(api.quote).not.toHaveBeenCalled()
    expect(api.refreshCurrent).toHaveBeenCalledWith(7); expect(api.subscriptions).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('paymentRetail.balanceSubscription.success')
    expect(readBalancePurchase(7)).toBeNull(); wrapper.unmount()
  })
  it('does not submit when balance is insufficient and offers top-up', async () => {
    state.auth.user.balance = 10
    const wrapper = await mountPage(); await select(wrapper)
    expect(wrapper.get('[data-test="confirm-subscription"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="subscription-top-up"]').exists()).toBe(true)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click')
    expect(api.buy).not.toHaveBeenCalled(); wrapper.unmount()
  })
  it('suppresses double clicks while the purchase is pending', async () => {
    let resolve!: (value: unknown) => void
    api.buy.mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click')
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click')
    expect(api.buy).toHaveBeenCalledTimes(1)
    resolve({ data: { order_id: 50, status: 'COMPLETED', site_credit_amount: 138, renewed: false } })
    await flushPromises(); wrapper.unmount()
  })
  it('restores the same frozen nonce after an unknown network outcome and page reload', async () => {
    api.buy.mockRejectedValueOnce(new Error('timeout'))
    const first = await mountPage(); await select(first)
    await first.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    const original = api.buy.mock.calls[0][0]
    expect(readBalancePurchase(7)?.request).toEqual(original)
    first.unmount()
    const second = await mountPage()
    expect(second.text()).toContain('paymentRetail.balanceSubscription.pending')
    await second.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy.mock.calls[1][0]).toEqual(original)
    expect(readBalancePurchase(7)).toBeNull(); second.unmount()
  })
  it('requires a new selection when server terms have changed', async () => {
    api.buy.mockRejectedValueOnce({ reason: 'PLAN_PRICE_CHANGED' })
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(readBalancePurchase(7)).toBeNull()
    expect(wrapper.find('[data-test="confirm-subscription"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('paymentRetail.balanceSubscription.changed'); wrapper.unmount()
  })
  it('does not restore a pending purchase for a different signed-in account', async () => {
    api.buy.mockRejectedValueOnce(new Error('timeout'))
    const first = await mountPage(); await select(first)
    await first.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises(); first.unmount()
    state.auth.user.id = 8
    const second = await mountPage()
    expect(second.find('[data-test="balance-subscription-summary"]').exists()).toBe(false)
    expect(readBalancePurchase(8)).toBeNull(); second.unmount()
  })

  it('does not charge if browser storage is unavailable', async () => {
    const wrapper = await mountPage(); await select(wrapper)
    const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('QuotaExceeded') })
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('paymentRetail.balanceSubscription.storageUnavailable')
    expect(wrapper.get('[data-test="confirm-subscription"]').attributes('disabled')).toBeUndefined()
    storage.mockRestore(); wrapper.unmount()
  })
  it('requires an exact storage readback before charging', async () => {
    const wrapper = await mountPage(); await select(wrapper)
    const get = vi.spyOn(Storage.prototype, 'getItem').mockReturnValue(null)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('paymentRetail.balanceSubscription.storageUnavailable')
    get.mockRestore(); wrapper.unmount()
  })
  it('recovers from nonce creation failure without leaving the button busy', async () => {
    const wrapper = await mountPage(); await select(wrapper)
    const random = vi.spyOn(crypto, 'randomUUID').mockImplementation(() => { throw new Error('Crypto unavailable') })
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-subscription"]').attributes('disabled')).toBeUndefined()
    random.mockRestore(); wrapper.unmount()
  })
  it('rejects a saved request asserting a different owner', () => {
    const pending = newBalancePurchase(7, plan)
    pending.request.expected_user_id = 8
    expect(saveBalancePurchase(pending)).toBe(true)
    expect(readBalancePurchase(7)).toBeNull()
  })
  it('clears the mounted confirmation synchronously when identity changes', async () => {
    const wrapper = await mountPage(); await select(wrapper)
    state.auth.user.id = 8; state.auth.token = 'token-8'
    await flushPromises()
    expect(wrapper.find('[data-test="confirm-subscription"]').exists()).toBe(false)
    expect(api.buy).not.toHaveBeenCalled(); wrapper.unmount()
  })
  it('does not apply a late successful purchase to another account', async () => {
    let resolve!: (value: unknown) => void
    api.buy.mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click')
    state.auth.user.id = 8; state.auth.token = 'token-8'
    resolve({ data: { order_id: 50, status: 'COMPLETED', site_credit_amount: 138, renewed: false } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('paymentRetail.balanceSubscription.success')
    expect(api.refreshCurrent).not.toHaveBeenCalled()
    expect(api.subscriptions).toHaveBeenLastCalledWith(8)
    expect(api.subscriptions.mock.calls.filter(args => args[0] === 7)).toHaveLength(1)
    expect(readBalancePurchase(7)).toBeNull(); wrapper.unmount()
  })
  it('preserves the original account nonce after a late unknown outcome', async () => {
    let reject!: (error: unknown) => void
    api.buy.mockReturnValue(new Promise((_, r) => { reject = r }))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click')
    const request = api.buy.mock.calls[0][0]
    state.auth.user.id = 8; state.auth.token = 'token-8'
    reject(new Error('timeout')); await flushPromises()
    expect(readBalancePurchase(7)?.request).toEqual(request)
    expect(readBalancePurchase(8)).toBeNull()
    expect(wrapper.text()).not.toContain('paymentRetail.balanceSubscription.pending'); wrapper.unmount()
  })
  it('ignores checkout responses after a mounted account change', async () => {
    let resolve!: (value: unknown) => void
    api.checkout.mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = shallowMount(PaymentView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Teleport: true } } })
    state.auth.user.id = 8; state.auth.token = 'token-8'
    resolve({ data: { plans: [plan], methods: {}, balance_retail_pricing_enabled: true } }); await flushPromises()
    expect(wrapper.findComponent(SubscriptionPlanCard).exists()).toBe(false)
    expect(api.subscriptions).not.toHaveBeenCalled(); wrapper.unmount()
  })
  it('ignores a late plan-change checkout after switching accounts', async () => {
    api.buy.mockRejectedValueOnce({ reason: 'PLAN_PRICE_CHANGED' })
    const wrapper = await mountPage(); await select(wrapper)
    let resolve!: (value: unknown) => void
    api.checkout.mockReturnValueOnce(new Promise(r => { resolve = r }))
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    state.auth.user.id = 8; state.auth.token = 'token-8'
    resolve({ data: { plans: [{ ...plan, name: 'OLD CHECKOUT', price: 999 }], methods: {} } }); await flushPromises()
    expect(wrapper.text()).not.toContain('paymentRetail.balanceSubscription.changed')
    expect(wrapper.findAllComponents(SubscriptionPlanCard)[0].props('plan').price).toBe(138)
    expect(api.refreshCurrent).not.toHaveBeenCalled(); wrapper.unmount()
  })
  it('does not continue subscription refresh when identity changes during user refresh', async () => {
    let resolve!: (value: unknown) => void
    api.refreshCurrent.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    state.auth.user.id = 8; state.auth.token = 'token-8'
    resolve(undefined); await flushPromises()
    expect(api.subscriptions.mock.calls.filter(args => args[0] === 7)).toHaveLength(1)
    expect(api.subscriptions).toHaveBeenLastCalledWith(8)
    expect(wrapper.text()).not.toContain('paymentRetail.balanceSubscription.success'); wrapper.unmount()
  })
  it('does not apply purchase results after the view is disposed', async () => {
    let resolve!: (value: unknown) => void
    api.buy.mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); wrapper.unmount()
    resolve({ data: { order_id: 50, status: 'COMPLETED', site_credit_amount: 138, renewed: false } }); await flushPromises()
    expect(api.refreshCurrent).not.toHaveBeenCalled()
    expect(readBalancePurchase(7)).toBeNull()
  })

  it('restores an unresolved request after token changes before a new selection', async () => {
    api.buy.mockRejectedValueOnce(new Error('timeout'))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    const original = api.buy.mock.calls[0][0]
    state.auth.token = 'rotated-token'; await flushPromises()
    await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy.mock.calls[1][0]).toEqual(original)
    wrapper.unmount()
  })

  it('keeps the frozen plan when a different selection follows an unknown result', async () => {
    api.buy.mockRejectedValueOnce(new Error('timeout'))
    const other = { ...plan, id: 2, price: 678, name: 'Pro 100' }
    api.checkout.mockResolvedValueOnce({ data: { payment_enabled: true, methods: {}, plans: [plan, other], global_min: 1,
      balance_disabled: false, balance_retail_pricing_enabled: true } })
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    const original = api.buy.mock.calls[0][0]
    state.auth.token = 'rotated-token'; await flushPromises()
    wrapper.findAllComponents(SubscriptionPlanCard)[1].vm.$emit('select', other); await flushPromises()
    expect(wrapper.get('[data-test="balance-subscription-summary"]').text()).toContain('$138.00')
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy.mock.calls[1][0]).toEqual(original)
    wrapper.unmount()
  })
  it('shows a storage error without sending a retry of an unresolved purchase', async () => {
    api.buy.mockRejectedValueOnce(new Error('timeout'))
    const wrapper = await mountPage(); await select(wrapper)
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    const storage = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('QuotaExceeded') })
    await wrapper.get('[data-test="confirm-subscription"]').trigger('click'); await flushPromises()
    expect(api.buy).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('paymentRetail.balanceSubscription.storageUnavailable')
    expect(readBalancePurchase(7)).not.toBeNull()
    storage.mockRestore(); wrapper.unmount()
  })

  it('filters old store responses belonging to another account from the current view', async () => {
    state.subscriptions = [
      { id: 1, user_id: 7, group_id: 3, status: 'active', group: { name: 'OWN PLAN', platform: 'openai' } },
      { id: 2, user_id: 8, group_id: 4, status: 'active', group: { name: 'OTHER ACCOUNT PLAN', platform: 'openai' } },
    ]
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('OWN PLAN')
    expect(wrapper.text()).not.toContain('OTHER ACCOUNT PLAN')
    wrapper.unmount()
  })
})
