import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { gelvAlipayAmountRange, isValidAlipayFeeRateBps, isValidGelvAlipayPayURL, openGelvAlipayCheckout } from '../gelvAlipay'
import { buildCreateOrderPayload, decidePaymentLaunch, getVisibleMethods, readPaymentRecoverySnapshot } from '../paymentFlow'
import { isGelvAlipayQuote, isRetailQuote } from '../retailQuote'
import { PROVIDER_CONFIG_FIELDS, PROVIDER_SUPPORTED_TYPES } from '../providerConfig'
import AmountInput from '../AmountInput.vue'
import RetailQuoteSummary from '../RetailQuoteSummary.vue'
import PaymentMethodSelector from '../PaymentMethodSelector.vue'
import AdminRetailOrderAmounts from '@/components/admin/payment/AdminRetailOrderAmounts.vue'
import { paymentMethodI18nKey } from '@/views/user/paymentUx'
import type { MethodLimit, RetailQuote } from '@/types/payment'
import zh from '@/i18n/locales/zh/gelv'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'en' }, t: (key: string, params?: Record<string, unknown>) => key + (params ? ' ' + JSON.stringify(params) : '') }) }))
const url = 'https://alar1c.cn/partner-pay/#id=' + 'a'.repeat(24) + '&token=' + 'b'.repeat(64)
const reversed = 'https://alar1c.cn/partner-pay/#token=' + 'b'.repeat(64) + '&id=' + 'a'.repeat(24)
const quote: RetailQuote = { pricing_mode: 'gelv_channel_cny', pricing_basis_currency: 'CNY', base_amount_cny: 9.37,
  credited_amount_usd: 9.37, credit_by_channel: { alipay_web: 9.37 }, pay_amount: 10, currency: 'CNY',
  base_amount_gbp: 0, included_cost_gbp: 0, total_amount_gbp: 0, fx: {} as RetailQuote['fx'], fx_source: 'fixed_cny',
  fx_asof: '', issued_at: '', expires_at: '2099-01-01T00:00:00Z', checkout_reference: 'test_alipay' }
const limit: MethodLimit = { amount_currency: 'CNY', amount_step: 0.01, fee_rate_bps: 60, currency: 'CNY', available: true, fee_rate: 0, single_min: 0, single_max: 0, daily_limit: 0, daily_used: 0, daily_remaining: 0 }
const result = { order_id: 42, amount: 9.37, pay_amount: 10, currency: 'CNY', fee_rate: 0, expires_at: quote.expires_at, pay_url: url, retail_quote: quote, payment_mode: 'redirect' }
const context = { visibleMethod: 'gelv_alipay', orderType: 'balance' as const, isMobile: true }
afterEach(() => vi.restoreAllMocks())

describe('Alipay hosted checkout boundary', () => {
  it('accepts exactly the two key orders without normalization', () => {
    expect(isValidGelvAlipayPayURL(url)).toBe(true)
    expect(isValidGelvAlipayPayURL(reversed)).toBe(true)
  })
  it.each([
    url + '&x=1', url + '&token=' + 'b'.repeat(64), url + '&id=' + 'a'.repeat(24), url + '\n', ' ' + url,
    url.replace('https:', 'http:'), url.replace('alar1c.cn', 'evil.alar1c.cn'), url.replace('alar1c.cn', 'alar1c.cn.evil.test'),
    url.replace('alar1c.cn', 'user@alar1c.cn'), url.replace('alar1c.cn', 'alar1c.cn@evil.test'),
    url.replace('alar1c.cn', 'alar1c.cn:443'), url.replace('alar1c.cn', 'ALAR1C.CN'),
    url.replace('/#', '/?x=1#'), url.replace('/#', '/?#'), url.replace('id=', 'id=%61'),
    url.replace('token=', 'token=B'), url.replace('&token=', '&amp;token='), url.replace('/partner-pay/', '/x/../partner-pay/'),
    url.replace('/partner-pay/', '/partner-pay//'), url.replace('#id=', '#id=' + 'a'), url.replace('&token=', '&token=' + 'b'),
    url.replace('&token=', ';token='), url.replace('#id=', '#%69d='), url + '#', null, {},
  ])('rejects noncanonical or extra URL material %#', value => expect(isValidGelvAlipayPayURL(value)).toBe(false))
  it('uses a user-click anchor with no opener or referrer and never falls back to redirect', () => {
    const anchor = document.createElement('a')
    vi.spyOn(document, 'createElement').mockReturnValue(anchor)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    expect(openGelvAlipayCheckout(url)).toBe(true)
    expect(anchor?.target).toBe('_blank')
    expect(anchor?.rel).toBe('noopener noreferrer')
    expect(anchor?.referrerPolicy).toBe('no-referrer')
    expect(open).not.toHaveBeenCalled()
    expect(openGelvAlipayCheckout(url + '&extra=1')).toBe(false)
    expect(click).toHaveBeenCalledTimes(1)
  })
  it('accepts arbitrary cash without tier metadata and preserves explicit availability', () => {
    expect(getVisibleMethods({ gelv_alipay: limit }).gelv_alipay.available).toBe(true)
    expect(getVisibleMethods({ gelv_alipay: { ...limit, available: false } }).gelv_alipay.available).toBe(false)
    expect(gelvAlipayAmountRange(limit)).toEqual({ min: 0.01, max: 50 })
    expect(gelvAlipayAmountRange({ ...limit, single_min: 2, single_max: 40 }, 3, 30)).toEqual({ min: 3, max: 30 })
    expect(gelvAlipayAmountRange({ ...limit, single_min: 5, single_max: 20 }, 1, 100)).toEqual({ min: 5, max: 20 })
    expect(gelvAlipayAmountRange({ ...limit, single_max: 500 }, 0, 500)).toEqual({ min: 0.01, max: 50 })
  })
  it('uses explicit rate strings, preserving zero and leaving missing values disabled', () => {
    for (const value of ['0', '100', '9999']) expect(isValidAlipayFeeRateBps(value)).toBe(true)
    for (const value of ['', undefined, 0, '-1', '10000', '1.5', '1e2', ' 100', '100\n', '01']) expect(isValidAlipayFeeRateBps(value)).toBe(false)
    expect(PROVIDER_SUPPORTED_TYPES.gelv).toEqual(['gelv', 'gelv_alipay'])
    expect(PROVIDER_CONFIG_FIELDS.gelv.find(f => f.key === 'alipayFeeRateBps')).toMatchObject({ optional: true, clearable: true })
    expect(PROVIDER_CONFIG_FIELDS.gelv.find(f => f.key === 'alipayFeeRateBps')?.defaultValue).toBeUndefined()
    expect(zh.alipayName).toBe('支付宝网页支付')
  })
  it('validates frozen credits without assuming a partner rate or accepting unknown rules', () => {
    expect(isRetailQuote(quote)).toBe(true)
    expect(isRetailQuote({ ...quote, base_amount_cny: 10, credited_amount_usd: 10, credit_by_channel: { alipay_web: 10 } })).toBe(true)
    for (const value of [
      { ...quote, pricing_mode: 'unknown' }, { ...quote, credit_by_channel: {} },
      { ...quote, credit_by_channel: { alipay_web: 9.37, apple_iap: 8.8 } },
      { ...quote, credit_by_channel: { alipay_web: 9.37, other: 9.37 } },
      { ...quote, credit_by_channel: { alipay_web: NaN } }, { ...quote, base_amount_cny: 9 },
      { ...quote, pay_amount: 11.001 }, { ...quote, pay_amount: NaN }, { ...quote, pay_amount: -1 }, { ...quote, currency: 'USD' },
    ]) expect(isRetailQuote(value)).toBe(false)
  })
  it('accepts arbitrary and historical cash snapshots without imposing new-order limits on history', () => {
    expect(isRetailQuote({ ...quote, pay_amount: 12.34, credited_amount_usd: 12.27, base_amount_cny: 12.27, credit_by_channel: { alipay_web: 12.27 } })).toBe(true)
    expect(isRetailQuote({ ...quote, pay_amount: 500 })).toBe(true)
  })
  it('launches as waiting on either device and rejects subscription or mismatched channel quotes', () => {
    for (const isMobile of [true, false]) expect(decidePaymentLaunch(result, { ...context, isMobile }).kind).toBe('redirect_waiting')
    expect(decidePaymentLaunch({ ...result, pay_url: reversed }, context).kind).toBe('redirect_waiting')
    expect(decidePaymentLaunch(result, { ...context, orderType: 'subscription' }).kind).toBe('unhandled')
    expect(() => buildCreateOrderPayload({ amount: 9.37, paymentType: 'gelv_alipay', orderType: 'subscription', isMobile: true })).toThrow()
    const rejected = decidePaymentLaunch({ ...result, pay_url: url + '&x=1' }, context)
    expect(rejected.kind).toBe('unhandled')
    expect(rejected.recovery.payUrl).toBe('')
    expect(decidePaymentLaunch({ ...result, amount: 9.9 }, context).kind).toBe('unhandled')
  })
  it('recovers only valid single-channel balance quotes and retains a token-free hold', () => {
    const state = decidePaymentLaunch(result, context).recovery
    expect(readPaymentRecoverySnapshot(JSON.stringify(state))?.retailQuote).toEqual(quote)
    for (const change of [{ payUrl: url + '&x=1' }, { orderType: 'subscription' }, { retailQuote: { ...quote, pricing_mode: 'unknown' } }, { amount: 9.9 }]) {
      expect(readPaymentRecoverySnapshot(JSON.stringify({ ...state, ...change }))).toBeNull()
    }
    const held = readPaymentRecoverySnapshot(JSON.stringify({ ...state, payUrl: '', refundReconciliationPending: true }))
    expect(held?.payUrl).toBe('')
    expect(held?.refundReconciliationPending).toBe(true)
    expect(readPaymentRecoverySnapshot(JSON.stringify({ ...state, payUrl: '', refundReconciliationPending: true, expiresAt: '2000-01-01T00:00:00Z' }))?.orderId).toBe(42)
    expect(readPaymentRecoverySnapshot(JSON.stringify({ ...state, refundReconciliationPending: true }))).toBeNull()
    expect(isGelvAlipayQuote(held?.retailQuote)).toBe(true)
  })
  it('renders only Alipay credits and the frozen agreed deduction in summaries and receipts', () => {
    for (const component of [RetailQuoteSummary, AdminRetailOrderAmounts]) {
      const wrapper = mount(component, { props: { quote } })
      expect(wrapper.text()).toContain('gelv.agreedDeduction')
      expect(wrapper.text()).toContain('¥0.63 CNY')
      expect(wrapper.text()).toContain('gelv.orderPayable')
      expect(wrapper.text()).not.toMatch(/gelv\.(android|ios|feeRates)|GBP|1%|12%/)
      wrapper.unmount()
    }
    expect(paymentMethodI18nKey('gelv_alipay')).toBe('gelv.alipayName')
    expect(paymentMethodI18nKey('gelv')).toBe('gelv.name')
  })
  it('allows a CNY custom amount without preset tiers and keeps the Alipay icon', async () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, min: 0.01, max: 50, paymentType: 'gelv_alipay' } })
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.text()).toContain('¥')
    const input = wrapper.get('input')
    await input.setValue('12.34')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([12.34])
    await input.setValue('12.345')
    expect((input.element as HTMLInputElement).value).toBe('12.34')
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    wrapper.unmount()
    const selector = mount(PaymentMethodSelector, { props: { selected: 'gelv_alipay', methods: [{ type: 'gelv_alipay', fee_rate: 12, available: true }, { type: 'gelv', fee_rate: 0, available: true }] } })
    expect(selector.findAll('button')).toHaveLength(2)
    expect(selector.findAll('img')[0].attributes('src')).toContain('alipay.svg')
    expect(selector.findAll('[data-testid=payment-method-label]')[0].classes()).not.toContain('truncate')
    expect(selector.findAll('[data-testid=payment-method-label]')[1].classes()).toContain('truncate')
    expect(selector.text()).not.toContain('12%')
    expect(selector.text()).toContain('gelv.name')
    selector.unmount()
  })
})
