import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { PaymentOrder, RetailQuote } from '@/types/payment'
import AdminOrdersView from '../AdminOrdersView.vue'
import { gelvOrder, order21Logs } from '@/components/admin/payment/__tests__/orderDetailFixtures'

const routeState = vi.hoisted(() => ({ query: {} as Record<string, string> }))
vi.mock('vue-router', () => ({ useRoute: () => routeState }))
vi.mock('@/components/admin/ConsoleObjectLinks.vue', () => ({ default: { template: '<nav />' } }))
const { getOrders, getOrder, refundOrder, showError, showSuccess } = vi.hoisted(() => ({
  getOrders: vi.fn(), getOrder: vi.fn(), refundOrder: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getOrders, getOrder, refundOrder }, default: { getOrders, getOrder } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => key + (params ? ' ' + Object.values(params).join(' ') : '') }),
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
        InfiniteListSentinel: true, Select: true, Icon: true, OrderStatusBadge: true, AdminRefundDialog: true,
      },
    },
  })
}

describe('real admin orders route retail snapshots', () => {
  beforeEach(() => {
    Object.keys(routeState.query).forEach(key => delete routeState.query[key])
    getOrders.mockResolvedValue({ data: { items: [order], total: 1 } })
    getOrder.mockResolvedValue({ data: { order, audit_logs: [] } })
    refundOrder.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('passes an exact routed user_id to the real paginated order API', async () => {
    routeState.query.user_id = '42'
    const wrapper = mountView()
    await flushPromises()
    expect(getOrders).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 42, page: 1 }))
    wrapper.unmount()
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
  it('appends server pages and retries a failed append without dropping loaded orders', async () => {
    getOrders.mockReset()
    getOrders.mockResolvedValueOnce({ data: { items: [order], total: 2 } })
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce({ data: { items: [{ ...order, id: 2 }], total: 2 } })
    const wrapper = mountView()
    await flushPromises()
    const sentinel = wrapper.findComponent({ name: 'InfiniteListSentinel' })
    sentinel.vm.$emit('load-more')
    await flushPromises()
    expect(wrapper.findAll('[data-order]')).toHaveLength(1)
    expect(sentinel.props('error')).toBeTruthy()
    sentinel.vm.$emit('retry')
    await flushPromises()
    expect(getOrders.mock.calls.map(call => call[0].page)).toEqual([1, 2, 2])
    expect(wrapper.findAll('[data-order]')).toHaveLength(2)
    expect(sentinel.props('hasMore')).toBe(false)
    wrapper.unmount()
  })

  it('discards an old append after a filter starts a new result set', async () => {
    let resolveOld!: (value: unknown) => void
    getOrders.mockReset()
    getOrders.mockResolvedValueOnce({ data: { items: [order], total: 2 } })
      .mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce({ data: { items: [{ ...order, id: 3 }], total: 1 } })
    const wrapper = mountView()
    await flushPromises()
    wrapper.findComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more')
    await flushPromises()
    wrapper.findAllComponents({ name: 'Select' })[0].vm.$emit('update:modelValue', 'COMPLETED')
    wrapper.findAllComponents({ name: 'Select' })[0].vm.$emit('change')
    await flushPromises()
    resolveOld({ data: { items: [{ ...order, id: 2 }], total: 2 } })
    await flushPromises()
    expect(getOrders).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, status: 'COMPLETED' }))
    expect(wrapper.find('[data-order="3"]').exists()).toBe(true)
    expect(wrapper.find('[data-order="2"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-order]')).toHaveLength(1)
    wrapper.unmount()
  })

  it.each([{ success: true }, { success: false, require_force: true, warning: 'force A' }])('ignores stale refund results after order replacement: %j', async result => {
    const items = [1, 2].map(id => ({ ...order, id, payment_type: 'stripe' }))
    getOrders.mockResolvedValueOnce({ data: { items, total: 2 } })
    let finish!: (value: unknown) => void
    refundOrder.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const refundButton = (id: number) => wrapper.get('[data-order="' + id + '"]').findAll('button').find(button => button.text() === 'payment.admin.refund')!
    await refundButton(1).trigger('click')
    const dialog = wrapper.findComponent({ name: 'AdminRefundDialog' })
    const payload = { amount: 5, reason: 'requested', deduct_balance: true, force: false }
    dialog.vm.$emit('confirm', payload, 1)
    dialog.vm.$emit('confirm', payload, 1)
    await flushPromises()
    expect(refundOrder).toHaveBeenCalledTimes(1)
    expect(refundOrder).toHaveBeenCalledWith(1, payload)
    dialog.vm.$emit('cancel')
    await flushPromises()
    expect(dialog.props('show')).toBe(true)
    await refundButton(2).trigger('click')
    expect(dialog.props('order').id).toBe(2)
    finish({ data: result })
    await flushPromises()
    expect(dialog.props('show')).toBe(true)
    expect(dialog.props('requireForce')).toBe(false)
    expect(dialog.props('warning')).toBe('')
    expect(showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires an explicit second force submission for the same refund order', async () => {
    getOrders.mockResolvedValueOnce({ data: { items: [{ ...order, payment_type: 'stripe' }], total: 1 } })
    refundOrder.mockResolvedValueOnce({ data: { success: false, require_force: true, warning: 'insufficient' } })
      .mockResolvedValueOnce({ data: { success: true } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'payment.admin.refund')!.trigger('click')
    const dialog = wrapper.findComponent({ name: 'AdminRefundDialog' })
    const payload = { amount: 5, reason: 'requested', deduct_balance: true, force: false }
    dialog.vm.$emit('confirm', payload, 1)
    await flushPromises()
    expect(dialog.props('requireForce')).toBe(true)
    expect(dialog.props('show')).toBe(true)
    dialog.vm.$emit('confirm', { ...payload, force: true }, 1)
    await flushPromises()
    expect(refundOrder.mock.calls).toEqual([[1, payload], [1, { ...payload, force: true }]])
    expect(dialog.props('show')).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['wechat_virtual', 100, 9.9, 'android'],
    ['apple_iap', 1200, 8.8, 'ios'],
  ] as const)('renders the actual inline paid %s dialog and folds raw logs', async (channel, rate, credit, label) => {
    const settled = gelvOrder({ amount: credit, settlement: { channel, fee_rate_bps: rate, verified: true } })
    const logs = order21Logs.map(log => log.action === 'RECHARGE_SUCCESS' ? { ...log, detail: JSON.stringify({ creditedAmount: credit, rechargeCode: 'preview-redacted' }) } : log)
    getOrders.mockResolvedValueOnce({ data: { items: [settled], total: 1 } })
    getOrder.mockResolvedValueOnce({ data: { order: settled, audit_logs: logs } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    await flushPromises()
    const detail = wrapper.get('[data-testid="order-detail"]')
    const amounts = detail.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(amounts).toContain('payment.admin.detail.actualPaid')
    expect(amounts).toContain('¥10.00 CNY')
    expect(amounts).toContain('$' + credit.toFixed(2))
    expect(amounts).not.toContain('principalCNY')
    expect(amounts).not.toContain('gelv.android')
    expect(amounts).not.toContain('gelv.ios')
    expect(amounts).toContain('payment.admin.detail.' + label)
    expect(detail.text()).toContain('gelv.name')
    expect(detail.text()).not.toContain('payment.admin.expiresAt')
    expect(detail.text()).toContain('payment.admin.completedAt')
    const technical = detail.findAll('[data-testid="audit-technical-detail"]')
    expect(technical).toHaveLength(4)
    expect(technical.every(item => item.attributes('open') === undefined)).toBe(true)
    expect(technical[0].get('pre').text()).toContain('8.8')
    const readable = detail.findAll('p').filter(item => !item.element.closest('details')).map(item => item.text()).join(' ')
    expect(readable).not.toContain('rechargeCode')
    expect(readable).toContain('payment.admin.detail.creditSummary $' + credit.toFixed(2))
    expect(readable).not.toContain('createdEstimate')
    if (credit === 9.9) expect(readable).not.toContain('8.80')
    expect(detail.findAll('p').find(item => item.text() === settled.out_trade_no)!.classes()).toContain('break-all')
    await technical[0].get('summary').trigger('click')
    // jsdom has no native layout; opening remains the native details/summary control's responsibility.
    expect(technical[0].get('summary').text()).toBe('payment.admin.detail.technicalDetails')
    wrapper.unmount()
  })

  it('retains pending platform estimates, creation estimate and expiry in the inline dialog', async () => {
    const pending = gelvOrder({ amount: 8.8, status: 'PENDING', paid_at: undefined, completed_at: undefined })
    getOrders.mockResolvedValueOnce({ data: { items: [pending], total: 1 } })
    getOrder.mockResolvedValueOnce({ data: { order: pending, auditLogs: [order21Logs[0]] } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    await flushPromises()
    const detail = wrapper.get('[data-testid="order-detail"]')
    expect(detail.text()).toContain('gelv.android')
    expect(detail.text()).toContain('gelv.ios')
    expect(detail.text()).toContain('payment.admin.expiresAt')
    expect(detail.text()).toContain('payment.admin.detail.createdEstimate ¥10.00 CNY $8.80')
    expect(detail.find('[data-testid="verified-payment-platform"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores a late detail response after another order is opened', async () => {
    const first = gelvOrder()
    const second = gelvOrder({ id: 22, amount: 8.8 })
    getOrders.mockResolvedValueOnce({ data: { items: [first, second], total: 2 } })
    let finish!: (value: unknown) => void
    getOrder.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
      .mockResolvedValueOnce({ data: { order: second, audit_logs: [] } })
    const wrapper = mountView()
    await flushPromises()
    const view = (id: number) => wrapper.get('[data-order="' + id + '"]').findAll('button').find(button => button.text() === 'common.view')!
    await view(21).trigger('click')
    await view(22).trigger('click')
    await flushPromises()
    finish({ data: { order: first, audit_logs: order21Logs } })
    await flushPromises()
    const detail = wrapper.get('[data-testid="order-detail"]')
    expect(detail.text()).toContain('#22')
    expect(detail.text()).not.toContain('#21')
    expect(detail.findAll('details')).toHaveLength(0)
    wrapper.unmount()
  })

  it.each([
    ['PAID', undefined, 'awaitingCredit'],
    ['RECHARGING', undefined, 'awaitingCredit'],
    ['FAILED', undefined, 'awaitingCredit'],
    ['FAILED', '2026-10-25T12:02:01Z', 'actualCredited'],
  ] as const)('separates paid cash from balance fulfillment in inline %s with completion %s', async (status, completed_at, label) => {
    const paid = gelvOrder({ status, completed_at })
    getOrders.mockResolvedValueOnce({ data: { items: [paid], total: 1 } })
    getOrder.mockResolvedValueOnce({ data: { order: paid, audit_logs: [] } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    await flushPromises()
    const text = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(text).toContain('payment.admin.detail.actualPaid')
    expect(text).toContain('payment.admin.detail.' + label)
    expect(text).toContain('¥10.00 CNY')
    expect(text).toContain('$9.90')
    if (label === 'awaitingCredit') expect(text).not.toContain('payment.admin.detail.actualCredited')
    wrapper.unmount()
  })

})
