import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SquarespaceCheckoutInstructions from '../SquarespaceCheckoutInstructions.vue'
import type { RetailQuote } from '@/types/payment'
const showError = vi.hoisted(() => vi.fn())
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError }) }))
const quote: RetailQuote = { credited_amount_usd: 10, base_amount_gbp: 8, included_cost_gbp: 0.42, total_amount_gbp: 8.42,
  pay_amount: 8.42, currency: 'GBP', fx: { GBP: 1, USD: 1.25, CNY: 9 }, fx_source: 'manual', fx_asof: '2026-09-29T12:00:00Z',
  issued_at: '2026-09-29T12:00:00Z', expires_at: '2099-01-01T00:00:00Z', checkout_reference: 'RYNEX-topup-order-42', product_id: '0123456789abcdef01234567', payment_claim_mode: 'reference' }
const payUrl = 'https://ritelt.squarespace.com/pay-link/'
const writeText = vi.fn()
describe('Squarespace checkout instructions', () => {
  beforeEach(() => {
    writeText.mockReset().mockResolvedValue(undefined)
    showError.mockReset()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    vi.spyOn(window, 'open').mockReturnValue(null)
  })
  afterEach(() => { vi.restoreAllMocks() })
  it('displays and copies exact GBP amount and full reference before an explicit checkout action', async () => {
    const wrapper = mount(SquarespaceCheckoutInstructions, { props: { quote, payUrl, orderNumber: 'order-42' } })
    expect(wrapper.get('[data-test="checkout-exact-amount"]').element).toHaveProperty('value', '8.42')
    expect(wrapper.get('[data-test="checkout-reference"]').element).toHaveProperty('value', 'RYNEX-topup-order-42')
    expect(wrapper.text()).toContain('order-42')
    expect(wrapper.get('[data-test="open-squarespace-checkout"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="copy-checkout-amount"]').trigger('click')
    await wrapper.get('[data-test="copy-checkout-reference"]').trigger('click')
    await flushPromises()
    expect(writeText.mock.calls).toEqual([['8.42'], ['RYNEX-topup-order-42']])
    expect(window.open).not.toHaveBeenCalled()
    await wrapper.get('[data-test="checkout-confirm"]').setValue(true)
    await wrapper.get('[data-test="open-squarespace-checkout"]').trigger('click')
    expect(window.open).toHaveBeenCalledWith(payUrl, '_blank', 'noopener,noreferrer')
    wrapper.unmount()
  })
  it('uses the account-email receipt workflow without claiming that a required reference field exists', async () => {
    const wrapper = mount(SquarespaceCheckoutInstructions, { props: { quote: { ...quote, payment_claim_mode: 'receipt_otp' }, payUrl, orderNumber: 'order-42', orderId: 42, accountEmail: 'mine@example.com' } })
    expect(wrapper.text()).toContain('paymentRetail.receiptCheckoutHint')
    expect(wrapper.text()).toContain('mine@example.com')
    expect(wrapper.find('[data-test="checkout-reference"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('paymentRetail.checkoutHint')
    expect(wrapper.find('[data-test="start-receipt-claim"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('reports clipboard failure and never launches a non-GBP quote', async () => {
    writeText.mockRejectedValueOnce(new Error('denied'))
    const wrapper = mount(SquarespaceCheckoutInstructions, { props: { quote: { ...quote, currency: 'USD' }, payUrl, orderNumber: 'order-42' } })
    await wrapper.get('[data-test="copy-checkout-amount"]').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('paymentRetail.copyFailed')
    await wrapper.get('[data-test="checkout-confirm"]').setValue(true)
    expect(wrapper.get('[data-test="open-squarespace-checkout"]').attributes('disabled')).toBeDefined()
    expect(window.open).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
