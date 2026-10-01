import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { RetailQuote } from '@/types/payment'
import paymentRetailEn from '@/i18n/locales/en/paymentRetail'
import paymentRetailZh from '@/i18n/locales/zh/paymentRetail'
import RetailQuoteSummary from '../RetailQuoteSummary.vue'
import { isCurrentRetailQuote, isRetailQuote } from '../retailQuote'

const selectedLocale = vi.hoisted(() => ({ value: 'en' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  locale: selectedLocale,
  t: (key: string, params: Record<string, unknown> = {}) => {
    const messages = selectedLocale.value === 'zh' ? paymentRetailZh : paymentRetailEn
    const value = (messages as Record<string, unknown>)[key.replace(/^paymentRetail\./, '')]
    return typeof value === 'string' ? value.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
  },
}) }))

const currentQuote: RetailQuote = {
  pricing_basis_currency: 'CNY', base_amount_cny: 50, credited_amount_usd: 50,
  base_amount_gbp: 5.64, included_cost_gbp: 0.50, total_amount_gbp: 6.14,
  pay_amount: 6.14, currency: 'GBP', fx: { GBP: 1, USD: 1.32, CNY: 8.88 },
  fx_source: 'ecb', fx_asof: '2026-09-30T12:00:00Z', issued_at: '2026-10-01T10:00:00Z',
  expires_at: '2099-01-01T00:00:00Z', checkout_reference: 'RXT-CNY-QUOTE',
}
function historicalQuote(): RetailQuote {
  const quote = { ...currentQuote, base_amount_gbp: 37.88, included_cost_gbp: 1.84,
    total_amount_gbp: 39.72, pay_amount: 39.72 }
  delete quote.pricing_basis_currency
  delete quote.base_amount_cny
  return quote
}
function render(quote: RetailQuote, locale = 'en') {
  selectedLocale.value = locale
  return mount(RetailQuoteSummary, { props: { quote } })
}

describe('CNY principal pricing display and compatibility', () => {
  it.each(['en', 'zh'])('shows site credit units, CNY principal and frozen GBP/CNY rate in %s', locale => {
    const wrapper = render(currentQuote, locale)
    const text = wrapper.text()
    expect(text).toContain('$50.00')
    expect(text).not.toContain('$50.00 USD')
    expect(text).toContain('¥50.00 CNY')
    expect(text).toContain('£6.14 GBP')
    expect(text).toContain('£0.50 GBP')
    expect(wrapper.get('[data-test="retail-credit-policy"]').text()).toContain('GBP/CNY')
    expect(wrapper.get('[data-test="retail-gbp-cny-rate"]').text()).toContain('£1 = ¥8.88')
    expect(text).not.toContain('GBP/USD')
    const details = wrapper.get('[data-test="retail-price-details"]')
    expect(details.attributes('open')).toBeUndefined()
    expect(details.get('[data-test="retail-cny-principal"]').text()).toContain('¥50.00 CNY')
    expect(wrapper.get('[data-test="retail-included-fee"]').text()).toContain('£0.50 GBP')
    // The GBP total is shown once, as the payable amount, not duplicated in a second row.
    expect(text.match(/£6\.14 GBP/g)).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps site credit and CNY principal distinct from USD gateway settlement', () => {
    const wrapper = render({ ...currentQuote, currency: 'USD', pay_amount: 8.11 })
    const text = wrapper.text()
    expect(text).toContain('Credit received')
    expect(text).toContain('Top-up principal')
    expect(text).toContain('$50.00')
    expect(text).toContain('¥50.00 CNY')
    expect(text).toContain('£6.14 GBP')
    expect(text).toContain('$8.11 USD')
    expect(text).not.toContain('$50.00 USD')
    wrapper.unmount()
  })

  it('preserves historical USD quote amounts without applying the new CNY policy', () => {
    const wrapper = render(historicalQuote())
    expect(wrapper.text()).toContain('$50.00 USD')
    expect(wrapper.text()).toContain('£39.72 GBP')
    expect(wrapper.find('[data-test="retail-cny-principal"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="retail-credit-policy"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="retail-price-details"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('accepts old snapshots for display while requiring CNY principal for fresh quotes', () => {
    expect(isRetailQuote(historicalQuote())).toBe(true)
    expect(isCurrentRetailQuote(historicalQuote())).toBe(false)
    expect(isCurrentRetailQuote(currentQuote)).toBe(true)
  })

  it.each([undefined, 0, -50, 49, Number.NaN, Number.POSITIVE_INFINITY])('rejects an invalid or inconsistent CNY principal: %s', principal => {
    expect(isRetailQuote({ ...currentQuote, base_amount_cny: principal })).toBe(false)
  })

  it('rejects an unsupported pricing basis or unlabelled CNY principal', () => {
    expect(isRetailQuote({ ...currentQuote, pricing_basis_currency: 'USD' })).toBe(false)
    expect(isRetailQuote({ ...currentQuote, pricing_basis_currency: undefined })).toBe(false)
  })
})
