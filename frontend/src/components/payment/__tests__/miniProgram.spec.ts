import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { isMiniProgramImage } from '../miniProgramImage'
import { isRetailQuote } from '../retailQuote'
import { decidePaymentLaunch, normalizeVisibleMethod } from '../paymentFlow'
import RetailQuoteSummary from '../RetailQuoteSummary.vue'
import AmountInput from '../AmountInput.vue'
import type { RetailQuote } from '@/types/payment'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'en' }, t: (key: string) => key }) }))
const image = 'data:image/png;base64,iVBORw0KGgo='
const quote: RetailQuote = {
  pricing_mode: 'gelv_channel_cny', pricing_basis_currency: 'CNY', base_amount_cny: 8.8,
  credit_by_channel: { wechat_virtual: 9.9, apple_iap: 8.8 },
  credited_amount_usd: 8.8, base_amount_gbp: 0, included_cost_gbp: 0, total_amount_gbp: 0,
  pay_amount: 10, currency: 'CNY', fx: {}, fx_source: 'fixed_cny',
  fx_asof: '2026-10-15T00:00:00Z', issued_at: '2026-10-15T00:00:00Z',
  expires_at: '2099-01-01T00:00:00Z', checkout_reference: 'sub2_test_miniprogram',
}

describe('shared mini-program products', () => {
  it('accepts only bounded PNG/JPEG images for this payment method', () => {
    expect(isMiniProgramImage('gelv', image)).toBe(true)
    expect(isMiniProgramImage('wxpay', image)).toBe(false)
    expect(isMiniProgramImage('gelv', 'data:image/svg+xml;base64,PHN2Zz4=')).toBe(false)
    expect(isMiniProgramImage('gelv', 'https://example.com/code.png')).toBe(false)
    expect(isMiniProgramImage('gelv', 'data:image/png;base64,' + 'A'.repeat(1048576))).toBe(false)
  })
  it('preserves the official image and frozen net credit through QR launch', () => {
    expect(normalizeVisibleMethod('gelv')).toBe('gelv')
    const result = decidePaymentLaunch({ order_id: 2, amount: 8.8, pay_amount: 10, fee_rate: 0,
      qr_code: image, retail_quote: quote, currency: 'CNY', expires_at: quote.expires_at,
    }, { visibleMethod: 'gelv', isMobile: false, isWechatBrowser: false, orderType: 'balance' })
    expect(result.kind).toBe('qr_waiting')
    expect(result.paymentState.qrCode).toBe(image)
    expect(result.paymentState.amount).toBe(8.8)
  })
  it('validates native CNY quotes without fabricated GBP or FX data', () => {
    expect(isRetailQuote(quote)).toBe(true)
    expect(isRetailQuote({ ...quote, pay_amount: 10.01 })).toBe(false)
    expect(isRetailQuote({ ...quote, credited_amount_usd: 11 })).toBe(false)
    expect(isRetailQuote({ ...quote, currency: 'USD' })).toBe(false)
    const wrapper = mount(RetailQuoteSummary, { props: { quote } })
    expect(wrapper.text()).toContain('¥10.00 CNY')
    expect(wrapper.text()).toContain('gelv.androidCredit')
    expect(wrapper.text()).toContain('gelv.iosCredit')
    expect(wrapper.text()).toContain('gelv.feeRates')
    expect(wrapper.text()).not.toContain('GBP')
    wrapper.unmount()
  })
  it('selects net credit while showing the fixed cash tier and disabling custom amounts', async () => {
    const wrapper = mount(AmountInput, { props: { modelValue: null, amounts: [8.8, 26.4], fixedOptions: [
      { cash_cny: 10, credits: 8.8, android_credits: 9.9, ios_credits: 8.8 }, { cash_cny: 30, credits: 26.4, android_credits: 29.7, ios_credits: 26.4 },
    ] } })
    expect(wrapper.text()).toContain('¥10')
    expect(wrapper.find('input').exists()).toBe(false)
    await wrapper.get('[data-testid="quick-amount-8.8"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([8.8])
    wrapper.unmount()
  })
})
