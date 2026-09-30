import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { PaymentOrder, RetailQuote } from '@/types/payment'
import AdminOrdersView from '../AdminOrdersView.vue'

const { getOrders, getOrder, showError, showSuccess } = vi.hoisted(() => ({
  getOrders: vi.fn(), getOrder: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getOrders, getOrder }, default: { getOrders, getOrder } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const quote: RetailQuote = {
  credited_amount_usd: 10, base_amount_gbp: 7.47, included_cost_gbp: 0.41,
  total_amount_gbp: 7.88, pay_amount: 7.88, currency: 'GBP',
  fx: { GBP: 1, USD: 1.34, CNY: 9.6 }, fx_source: 'manual',
  fx_asof: '2026-09-30T00:00:00Z', issued_at: '2026-09-30T00:00:00Z',
  expires_at: '2026-09-30T00:15:00Z', checkout_reference: 'RXT-TEST',
}
const order: PaymentOrder = {
  id: 1, user_id: 10, amount: 10, pay_amount: 7.88, currency: 'GBP',
  fee_rate: 88, payment_type: 'squarespace', out_trade_no: 'sub2_test',
  status: 'COMPLETED', order_type: 'balance', created_at: '2026-09-30T00:00:00Z',
  expires_at: '2026-09-30T00:30:00Z', refund_amount: 0, retail_quote: quote,
}

function mountView() {
  return mount(AdminOrdersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        OrderTable: { props: ['orders'], template: '<div><div v-for="row in orders" :key="row.id" :data-order="row.id"><slot name="actions" :row="row" /></div></div>' },
        BaseDialog: { props: ['show'], template: '<div v-if="show" data-testid="order-detail"><slot /></div>' },
        Pagination: true, Select: true, Icon: true, OrderStatusBadge: true, AdminRefundDialog: true,
      },
    },
  })
}

describe('real admin orders route retail snapshots', () => {
  beforeEach(() => {
    getOrders.mockResolvedValue({ data: { items: [order], total: 1 } })
    getOrder.mockResolvedValue({ data: { order, audit_logs: [] } })
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('shows all frozen monetary values and hides the legacy fee in the actual route detail', async () => {
    const wrapper = mountView()
    await flushPromises()
    const view = wrapper.findAll('button').find(button => button.text() === 'common.view')
    if (!view) throw new Error('View action missing')
    await view.trigger('click')
    await flushPromises()
    const detail = wrapper.get('[data-testid="order-detail"]')
    expect(detail.text()).toContain('$10.00 USD')
    expect(detail.text()).toContain('£7.88 GBP')
    expect(detail.text()).toContain('£0.41 GBP')
    expect(detail.text()).not.toContain('payment.admin.feeRate')
    expect(detail.text()).not.toContain('88%')
    expect(getOrder).toHaveBeenCalledWith(1)
    wrapper.unmount()
  })

  it('offers no unsupported Squarespace automatic refund action while retaining it for existing methods', async () => {
    getOrders.mockResolvedValueOnce({ data: { items: [
      order, { ...order, id: 2, payment_type: 'stripe' },
      { ...order, id: 3, status: 'REFUND_REQUESTED' },
      { ...order, id: 4, status: 'REFUND_PENDING' },
      { ...order, id: 5, status: 'REFUND_FAILED' },
    ], total: 5 } })
    const wrapper = mountView()
    await flushPromises()
    for (const id of [1, 3, 4, 5]) {
      const actions = wrapper.get('[data-order="' + id + '"]').text()
      expect(actions).not.toContain('payment.admin.refund')
      expect(actions).not.toContain('payment.admin.approveRefund')
      expect(actions).not.toContain('payment.admin.retryRefund')
      expect(actions).not.toContain('payment.admin.queryRefundStatus')
    }
    expect(wrapper.get('[data-order="2"]').text()).toContain('payment.admin.refund')
    wrapper.unmount()
  })
})
