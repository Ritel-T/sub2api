import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { PaymentOrder } from '@/types/payment'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import hk from '@/i18n/locales/zh-Hant-HK'
import AdminOrderDetail from '../AdminOrderDetail.vue'
import { getVerifiedSettlement, hasOrderPayment, hasOrderCredit, summarizeOrderAuditLog, formatAuditDetail } from '../adminOrderDetail'
import { gelvOrder, order21Logs } from './orderDetailFixtures'

function render(order: PaymentOrder, locale = 'en') {
  return mount(AdminOrderDetail, {
    props: { show: true, order },
    global: { plugins: [createI18n({ legacy: false, locale, messages: { en, zh, 'zh-Hant-HK': hk } })], stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
    } },
  })
}

describe('settled admin retail detail', () => {
  it.each([
    { channel: 'wechat_virtual', rate: 100, credit: 9.9, label: 'Android', fee: '1% fee' },
    { channel: 'apple_iap', rate: 1200, credit: 8.8, label: 'iOS (Apple IAP)', fee: '12% fee' },
  ] as const)('shows actual $credit credit and verified $label for a paid order', ({ channel, rate, credit, label, fee }) => {
    const wrapper = render(gelvOrder({ amount: credit, pay_amount: 11, settlement: { channel, fee_rate_bps: rate, verified: true } }))
    const amounts = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(amounts).toContain('Actual payment¥11.00 CNY')
    expect(amounts).toContain('Actual credit$' + credit.toFixed(2))
    expect(amounts).toContain(label)
    expect(amounts).toContain(fee)
    expect(amounts).not.toContain('principal')
    expect(amounts).not.toContain('Android credit')
    expect(amounts).not.toContain('iOS credit')
    expect(amounts).not.toContain('¥8.80')
    expect(wrapper.text()).toContain('WeChat Mini Program')
    expect(wrapper.text()).not.toContain('Expires At')
    expect(wrapper.text()).toContain('Completed At')
    wrapper.unmount()
  })

  it.each(['PENDING', 'EXPIRED', 'CANCELLED'] as const)('keeps unpaid %s estimates and expiry without a platform claim', status => {
    const wrapper = render(gelvOrder({ status, amount: 8.8, paid_at: undefined, completed_at: undefined }))
    const amounts = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(amounts).toContain('Estimated credit at checkout')
    expect(amounts).toContain('Android credit$9.90')
    expect(amounts).toContain('iOS credit$8.80')
    expect(amounts).toContain('Payment due¥10.00 CNY')
    expect(wrapper.text()).toContain('Expires At')
    expect(wrapper.find('[data-testid="verified-payment-platform"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('uses completed status for legacy paid rows without timestamps, without guessing the platform from $9.90', () => {
    const wrapper = render(gelvOrder({ paid_at: undefined, completed_at: undefined }))
    const text = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(text).toContain('Actual credit$9.90')
    expect(text).not.toContain('$8.80')
    expect(wrapper.find('[data-testid="verified-payment-platform"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([['zh', '实际支付', '实际到账'], ['zh-Hant-HK', '實際支付', '實際到帳']])('provides clear settled labels in %s', (locale, paid, credit) => {
    const wrapper = render(gelvOrder(), locale)
    expect(wrapper.text()).toContain(paid)
    expect(wrapper.text()).toContain(credit)
    expect(wrapper.text()).not.toContain('payment.admin.detail.')
    wrapper.unmount()
  })


  it.each(['PAID', 'RECHARGING', 'FAILED'] as const)('shows actual payment but awaiting credit for unfulfilled %s', status => {
    const order = gelvOrder({ status, completed_at: undefined })
    expect(hasOrderPayment(order)).toBe(true)
    expect(hasOrderCredit(order)).toBe(false)
    const wrapper = render(order)
    const text = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(text).toContain('Actual payment¥10.00 CNY')
    expect(text).toContain('Credit awaiting fulfillment$9.90')
    expect(text).not.toContain('Actual credit')
    expect(text).not.toContain('$8.80')
    wrapper.unmount()
  })

  it('retains actual credit for FAILED after recorded completion', () => {
    const order = gelvOrder({ status: 'FAILED' })
    expect(hasOrderCredit(order)).toBe(true)
    const wrapper = render(order)
    expect(wrapper.get('[data-testid="admin-retail-order-amounts"]').text()).toContain('Actual credit$9.90')
    wrapper.unmount()
  })
  it('ignores unverified, inconsistent, pending and non-Gelv settlement claims', () => {
    const settlement = { channel: 'wechat_virtual', fee_rate_bps: 100, verified: true } as const
    for (const overrides of [
      { settlement: { ...settlement, verified: false } },
      { settlement: { ...settlement, channel: 'unknown' } },
      { settlement: { ...settlement, fee_rate_bps: 1200 } },
      { settlement, payment_type: 'stripe' },
      { settlement, status: 'PENDING', paid_at: undefined, completed_at: undefined },
    ]) expect(getVerifiedSettlement(gelvOrder(overrides as Partial<PaymentOrder>))).toBeNull()
    expect(hasOrderPayment(gelvOrder({ status: 'FAILED', completed_at: undefined }))).toBe(true)
    expect(hasOrderPayment(gelvOrder({ status: 'FAILED', paid_at: undefined, completed_at: undefined }))).toBe(false)
  })
})

describe('readable order audit summaries', () => {
  it('keeps the frozen $8.80 out of paid Gelv creation summaries while preserving it in technical data', () => {
    const summary = summarizeOrderAuditLog(order21Logs[0], gelvOrder())
    expect(summary.summaryKey).toBe('payment.admin.detail.createdPayment')
    expect(summary.params).toEqual({ paid: '¥10.00 CNY' })
    expect(formatAuditDetail(order21Logs[0].detail)).toContain('8.8')
  })

  it('labels unpaid creation credit as an estimate, never as actual credit', () => {
    const summary = summarizeOrderAuditLog(order21Logs[0], gelvOrder({ status: 'PENDING', paid_at: undefined, completed_at: undefined }))
    expect(summary.summaryKey).toBe('payment.admin.detail.createdEstimate')
    expect(summary.params).toEqual({ paid: '¥10.00 CNY', credit: '$8.80' })
  })

  it('uses final event credit before order fallback and summarizes the real payment and rebate', () => {
    const summaries = order21Logs.map(log => summarizeOrderAuditLog(log, gelvOrder({ amount: 7 })))
    expect(summaries[1].params).toEqual({ paid: '¥10.00 CNY' })
    expect(summaries[2].params).toEqual({ rebate: '$0.99', base: '$9.90' })
    expect(summaries[3].params).toEqual({ credit: '$9.90' })
    expect(summarizeOrderAuditLog({ ...order21Logs[3], detail: null }, gelvOrder()).params).toEqual({ credit: '$9.90' })
  })

  it.each([null, 'broken JSON', '[]', 'null', '{"creditedAmount":"8.80"}', '{"creditedAmount":-5}'])('handles missing/legacy/malformed detail %s without treating quote values as settlement', detail => {
    const log = { ...order21Logs[3], detail }
    expect(summarizeOrderAuditLog(log, gelvOrder()).params.credit).toBe('$9.90')
    expect(summarizeOrderAuditLog(log, gelvOrder({ status: 'PENDING', paid_at: undefined, completed_at: undefined })).summaryKey).toBe('')
    expect(() => formatAuditDetail(detail)).not.toThrow()
  })


  it('does not use a paid-but-unfulfilled order as fallback evidence for a malformed success log', () => {
    const order = gelvOrder({ status: 'PAID', completed_at: undefined })
    expect(summarizeOrderAuditLog({ ...order21Logs[3], detail: null }, order).summaryKey).toBe('')
  })
  it('never surfaces arbitrary payload text, redeem codes or unknown event names as the readable summary', () => {
    const log = { ...order21Logs[0], action: 'NEW_PROVIDER_SECRET_EVENT', detail: '{"secret":"<script>unsafe</script>"}' }
    expect(summarizeOrderAuditLog(log, gelvOrder())).toEqual({ titleKey: 'payment.admin.detail.events.other', summaryKey: '', params: {} })
    const summary = JSON.stringify(summarizeOrderAuditLog(order21Logs[3], gelvOrder()))
    expect(summary).not.toContain('preview-redacted')
    expect(summary).not.toContain('rechargeCode')
  })
})
