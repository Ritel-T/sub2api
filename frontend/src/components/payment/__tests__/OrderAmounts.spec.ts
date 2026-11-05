import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OrderAmounts from '../OrderAmounts.vue'
import OrderTable from '../OrderTable.vue'
import AdminOrderTable from '@/components/admin/payment/AdminOrderTable.vue'
import type { PaymentOrder } from '@/types/payment'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
afterEach(() => vi.restoreAllMocks())
const order: PaymentOrder = {
  id: 1, user_id: 1, amount: 12.27, pay_amount: 12.34, currency: 'CNY', fee_rate: 0.6,
  payment_type: 'gelv_alipay', out_trade_no: 'test', status: 'COMPLETED', order_type: 'balance',
  created_at: '2026-10-31T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', refund_amount: 0,
}
const tableStub = { props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-pay_amount" :row="row" :value="row.pay_amount" /></div></div>' }

describe('actual order list amounts', () => {
  it.each(['gelv_alipay', 'gelv', 'alipay', 'wxpay', 'stripe', 'airwallex', 'squarespace', 'balance'])('shows cash and credited totals once for completed %s orders', payment_type => {
    const wrapper = mount(OrderAmounts, { props: { order: { ...order, payment_type, bonus_amount: 1 } } })
    expect(wrapper.get('[data-testid=order-paid]').text()).toContain('¥12.34')
    expect(wrapper.get('[data-testid=order-credited]').text()).toContain('$12.27')
    expect(wrapper.text().match(/12\.34/g)).toHaveLength(1)
    expect(wrapper.text().match(/12\.27/g)).toHaveLength(1)
    expect(wrapper.text()).not.toMatch(/fee|gelv\.|bonus|principal|totalGBP/)
    wrapper.unmount()
  })
  it.each(['PENDING', 'EXPIRED', 'CANCELLED', 'FAILED'] as const)('does not claim any actual amount for unpaid %s orders', status => {
    const wrapper = mount(OrderAmounts, { props: { order: { ...order, status, refund_reconciliation_pending: true } } })
    expect(wrapper.text()).not.toMatch(/12\.34|12\.27/)
    expect(wrapper.get('[data-testid=order-paid]').text()).toContain('—')
    expect(wrapper.get('[data-testid=order-credited]').text()).toContain('—')
    wrapper.unmount()
  })
  it.each(['PAID', 'RECHARGING', 'FAILED'] as const)('shows payment with awaiting credit after confirmed payment in %s', status => {
    const wrapper = mount(OrderAmounts, { props: { order: { ...order, status, paid_at: '2026-10-31T01:00:00Z' } } })
    expect(wrapper.get('[data-testid=order-paid]').text()).toContain('¥12.34')
    expect(wrapper.get('[data-testid=order-credited]').text()).toBe('payment.orders.actualCredited: —')
    wrapper.unmount()
  })
  it('uses persisted completion evidence and never calls a subscription price credited balance', () => {
    const wrapper = mount(OrderAmounts, { props: { order: { ...order, status: 'FAILED', completed_at: '2026-10-31T01:00:00Z' } } })
    expect(wrapper.text()).toContain('$12.27')
    wrapper.unmount()
    const subscription = mount(OrderAmounts, { props: { order: { ...order, order_type: 'subscription' } } })
    expect(subscription.get('[data-testid=order-credited]').text()).toContain('—')
    subscription.unmount()
  })
  it.each(['USD', 'GBP', 'CNY'])('uses persisted %s cash currency rather than a frozen quote total', currency => {
    const wrapper = mount(OrderAmounts, { props: { order: { ...order, currency } } })
    expect(wrapper.get('[data-testid=order-paid]').text()).toContain(({ USD: '$12.34', GBP: '£12.34', CNY: '¥12.34' } as Record<string, string>)[currency])
    wrapper.unmount()
  })
  it('shares the two actual totals across user and admin tables', () => {
    for (const component of [OrderTable, AdminOrderTable]) {
      const wrapper = mount(component, { props: { orders: [order], loading: false, page: 1, pageSize: 50, total: 1 }, global: { stubs: { DataTable: tableStub, Select: true, Icon: true, InfiniteListSentinel: true } } })
      expect(wrapper.findAll('[data-testid=order-amounts]')).toHaveLength(1)
      expect(wrapper.text()).toContain('$12.27')
      expect(wrapper.text()).toContain('¥12.34')
      expect(wrapper.text()).not.toMatch(/gelv\.(agreedDeduction|alipayCredit|androidCredit|iosCredit|feeRates)/)
      wrapper.unmount()
    }
  })
  it('renders mobile order cards through the same slot without duplicate amount rows', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: false, media: query,
      onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }))
    const wrapper = mount(OrderTable, { props: { orders: [order], loading: false } })
    await flushPromises()
    expect(wrapper.find('.table-mobile-records').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid=order-amounts]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid=order-paid]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid=order-credited]')).toHaveLength(1)
    expect(wrapper.text().match(/12\.27/g)).toHaveLength(1)
    expect(wrapper.text().match(/12\.34/g)).toHaveLength(1)
    wrapper.unmount()
  })
})

vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
