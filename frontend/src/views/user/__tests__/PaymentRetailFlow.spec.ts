import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import PaymentView from '../PaymentView.vue'
import AmountInput from '@/components/payment/AmountInput.vue'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import type { PaymentQuoteResponse, RetailQuote } from '@/types/payment'

const api = vi.hoisted(() => ({ quote: vi.fn(), checkout: vi.fn(), createOrder: vi.fn(), showError: vi.fn(), showWarning: vi.fn() }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
const routeState = vi.hoisted(() => ({ path: '/purchase', query: {} as Record<string, string> }))
vi.mock('vue-router', () => ({ useRoute: () => routeState, useRouter: () => ({ resolve: () => ({ href: '/payment/result' }), replace: vi.fn(), push: vi.fn() }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { quote: api.quote, getCheckoutInfo: api.checkout } }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ createOrder: api.createOrder }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { username: 'tester', balance: 0 }, refreshUser: vi.fn() }) }))
vi.mock('@/stores/subscriptions', () => ({ useSubscriptionStore: () => ({ activeSubscriptions: [], fetchActiveSubscriptions: vi.fn().mockResolvedValue(undefined) }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ cachedPublicSettings: { subscription_enabled: false }, showError: api.showError, showWarning: api.showWarning, showInfo: vi.fn() }) }))
vi.mock('@/utils/device', () => ({ isMobileDevice: () => true }))

function quote(amount = 10, currency = 'GBP', ttl = 60000): RetailQuote {
  return { credited_amount_usd: amount, base_amount_gbp: 8, included_cost_gbp: 0.42, total_amount_gbp: 8.42,
    pay_amount: currency === 'GBP' ? 8.42 : 10.61, currency, fx: { GBP: 1, USD: 1.26, CNY: 9.2 },
    fx_source: 'manual', fx_asof: new Date().toISOString(), issued_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + ttl).toISOString(), checkout_reference: 'RYNEX-topup-test', product_id: '0123456789abcdef01234567', payment_claim_mode: 'receipt_otp' as const }
}
function response(value = quote(), token = 'quote-1'): { data: PaymentQuoteResponse } { return { data: { quote_token: token, retail_quote: value } } }
function order(value = quote()) {
  return { order_id: 42, amount: value.credited_amount_usd, pay_amount: value.pay_amount, currency: value.currency,
    fee_rate: 0, expires_at: new Date(Date.now() + 600000).toISOString(), payment_type: 'squarespace',
    payment_mode: 'redirect', pay_url: 'https://example.squarespace.com/pay-link/test', out_trade_no: 'order-42', retail_quote: value }
}
async function mountPage() {
  const wrapper = shallowMount(PaymentView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, AmountInput: false, PaymentMethodSelector: false,
    RetailQuoteSummary: false, Teleport: true, Transition: false,
  } } })
  await flushPromises()
  return wrapper
}
async function setAmount(wrapper: ReturnType<typeof shallowMount>, value: number) {
  wrapper.findComponent(AmountInput).vm.$emit('update:modelValue', value)
  await vi.advanceTimersByTimeAsync(300)
  await flushPromises()
}

describe('server-priced retail checkout', () => {
  beforeEach(() => {
    routeState.query = {}
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-29T12:00:00Z'))
    api.quote.mockReset().mockResolvedValue(response())
    api.createOrder.mockReset().mockResolvedValue(order())
    api.showError.mockReset(); api.showWarning.mockReset()
    const limit = { currency: 'GBP', fee_rate: 99, single_min: 0, single_max: 0, available: true, daily_limit: 0, daily_used: 0, daily_remaining: 0 }
    api.checkout.mockResolvedValue({ data: { methods: { squarespace: { ...limit, display_name: 'Card / Squarespace' }, stripe: { ...limit, currency: 'USD' } },
      global_min: 1, global_max: 100, plans: [], balance_disabled: false, balance_retail_pricing_enabled: true,
      balance_recharge_multiplier: 999, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 99,
      help_text: '', help_image_url: '', stripe_publishable_key: '' } })
    window.localStorage.clear()
    vi.spyOn(window, 'open').mockReturnValue(null)
  })
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

  it('shows the merchant test notice only for the effective test account', async () => {
    const checkout = await api.checkout()
    api.checkout.mockResolvedValueOnce({ data: { ...checkout.data, merchant_test_access: true } })
    const wrapper = await mountPage()
    expect(wrapper.find('[data-test="merchant-test-access"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('paymentRetail.merchantTest')
    wrapper.unmount()
  })

  it('uses server GBP totals and included cost, confirms before creation, and never auto-opens PayLink', async () => {
    const wrapper = await mountPage()
    wrapper.findComponent(PaymentMethodSelector).vm.$emit('select', 'squarespace')
    await setAmount(wrapper, 10)
    expect(api.quote).toHaveBeenLastCalledWith({ amount: 10, payment_type: 'squarespace', order_type: 'balance' })
    expect(wrapper.text()).toContain('£8.42 GBP')
    expect(wrapper.text()).toContain('£0.42 GBP')
    expect(wrapper.text()).not.toContain('99%')
    expect(wrapper.text()).not.toContain('9990')
    expect(wrapper.get('[data-test="create-recharge-order"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(api.createOrder).toHaveBeenCalledWith(expect.objectContaining({ amount: 10, payment_type: 'squarespace', quote_token: 'quote-1', order_type: 'balance' }))
    expect(window.open).not.toHaveBeenCalled()
    expect(wrapper.findComponent(PaymentStatusPanel).props('retailQuote')).toEqual(expect.objectContaining({ credited_amount_usd: 10, pay_amount: 8.42, currency: 'GBP', checkout_reference: 'RYNEX-topup-test' }))
    wrapper.unmount()
  })

  it('ignores an old quote response after the amount changes', async () => {
    let resolveOld!: (value: ReturnType<typeof response>) => void
    api.quote.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve })).mockResolvedValue(response(quote(20), 'quote-20'))
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    await setAmount(wrapper, 20)
    resolveOld(response(quote(10), 'quote-old'))
    await flushPromises()
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(api.createOrder).toHaveBeenCalledWith(expect.objectContaining({ amount: 20, quote_token: 'quote-20' }))
    wrapper.unmount()
  })

  it('refreshes an expired quote and requires another confirmation', async () => {
    api.quote.mockResolvedValueOnce(response(quote(10, 'GBP', 1500))).mockResolvedValueOnce(response(quote(), 'quote-new'))
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('paymentRetail.expired')
    expect(wrapper.get('[data-test="create-recharge-order"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="refresh-retail-quote"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="confirm-retail-quote"]').element).toHaveProperty('checked', false)
    expect(api.createOrder).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps the GBP product total when a method settles in USD and resets confirmation', async () => {
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    api.quote.mockResolvedValue(response(quote(10, 'USD'), 'quote-usd'))
    wrapper.findComponent(PaymentMethodSelector).vm.$emit('select', 'stripe')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.text()).toContain('£8.42 GBP')
    expect(wrapper.text()).toContain('$10.61 USD')
    expect(wrapper.get('[data-test="confirm-retail-quote"]').element).toHaveProperty('checked', false)
    wrapper.unmount()
  })

  it('gets a fresh quote after WeChat authorization and keeps openid for the confirmed continuation', async () => {
    routeState.query = { wechat_resume: '1', payment_type: 'wxpay', openid: 'wx-authorized-user', amount: '10', order_type: 'balance' }
    const checkout = await api.checkout()
    api.checkout.mockResolvedValue({ data: { ...checkout.data, methods: { wxpay: { ...checkout.data.methods.squarespace, currency: 'CNY' } } } })
    api.quote.mockResolvedValue(response(quote(10, 'CNY'), 'wechat-quote'))
    api.createOrder.mockResolvedValue({ ...order(quote(10, 'CNY')), payment_type: 'wxpay', qr_code: 'https://pay.example.com/qr/42', pay_url: undefined, payment_mode: 'qrcode' })
    const wrapper = await mountPage()
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(api.createOrder).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(api.createOrder).toHaveBeenCalledWith(expect.objectContaining({ amount: 10, payment_type: 'wxpay', quote_token: 'wechat-quote', openid: 'wx-authorized-user' }))
    wrapper.unmount()
  })

  it.each(['COMPLETED', 'EXPIRED', 'CANCELLED', 'PAID', 'RECHARGING'] as const)('does not launch checkout again for a replayed %s order', async status => {
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    api.createOrder.mockResolvedValueOnce({ ...order(), status })
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(window.open).not.toHaveBeenCalled()
    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(false)
    wrapper.unmount()
  })

  it('accepts a dedicated balance-service quote without a fixed product ID and does not show internal scope to the payer', async () => {
    const dedicated = { ...quote(), product_id: undefined, order_scope_mode: 'dedicated_site_service' as const, expected_service_name: 'Pay', purpose: 'balance_topup_only' }
    api.quote.mockResolvedValue(response(dedicated))
    api.createOrder.mockResolvedValue(order(dedicated))
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(api.createOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('dedicated_site_service')
    expect(wrapper.text()).not.toContain('balance_topup_only')
    expect(window.open).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('rejects a changed product snapshot without exposing the product ID to the payer', async () => {
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    api.createOrder.mockResolvedValueOnce({ ...order(), retail_quote: { ...quote(), product_id: 'abcdef0123456789abcdef01' } })
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(window.open).not.toHaveBeenCalled()
    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(false)
    expect(api.showError).toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('abcdef0123456789abcdef01')
    wrapper.unmount()
  })

  it('does not launch checkout when creation returns a different price from the confirmed quote', async () => {
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    api.createOrder.mockResolvedValueOnce({ ...order(), pay_amount: 12.99 })
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(window.open).not.toHaveBeenCalled()
    expect(wrapper.findComponent(PaymentStatusPanel).exists()).toBe(false)
    expect(api.showError).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('refreshes after the server rejects a stale token without silently creating another order', async () => {
    const wrapper = await mountPage()
    await setAmount(wrapper, 10)
    api.createOrder.mockRejectedValueOnce({ reason: 'PAYMENT_QUOTE_EXPIRED' })
    await wrapper.get('[data-test="confirm-retail-quote"]').setValue(true)
    await wrapper.get('[data-test="create-recharge-order"]').trigger('click')
    await flushPromises()
    expect(api.quote).toHaveBeenCalledTimes(2)
    expect(api.createOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test="confirm-retail-quote"]').element).toHaveProperty('checked', false)
    wrapper.unmount()
  })
})
