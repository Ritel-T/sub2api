import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../UserOrdersView.vue'
const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn() }))
const push = vi.hoisted(() => vi.fn())
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  push.mockReset()
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: ['provider-1'] } })
})
async function page(status: string, type = 'squarespace') {
  api.getMyOrders.mockResolvedValue({ data: { items: [{ id: 42, status, payment_type: type, payment_claim_mode: 'receipt_otp', provider_instance_id: 'provider-1' }], total: 1 } })
  const wrapper = mount(UserOrdersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' },
    OrderTable: { props: ['orders'], template: '<div><div v-for="row in orders" :key="row.id"><slot name="actions" :row="row" /></div></div>' },
    BaseDialog: true, Icon: true, Pagination: true, Select: true,
  } } })
  await flushPromises()
  return wrapper
}
describe('receipt claim order entry', () => {
  it.each(['EXPIRED'])('lets the owner return to a %s order for official paid-time verification', async status => {
    const wrapper = await page(status)
    expect(wrapper.text()).toContain('paymentRetail.claim.openOrder')
    await wrapper.get('[data-test="open-square-order"]').trigger('click')
    expect(push).toHaveBeenCalledWith({ path: '/payment/result', query: { order_id: '42' } })
  })
  it('offers only an order result for cancelled orders without promising receipt claim', async () => {
    const wrapper = await page('CANCELLED')
    expect(wrapper.text()).toContain('paymentRetail.claim.viewResult')
    expect(wrapper.text()).not.toContain('paymentRetail.claim.openOrder')
    await wrapper.get('[data-test="open-square-order"]').trigger('click')
    expect(push).toHaveBeenCalledWith({ path: '/payment/result', query: { order_id: '42' } })
  })

  it('offers only the completed result and hides unsupported Squarespace self-service refund', async () => {
    const wrapper = await page('COMPLETED')
    expect(wrapper.text()).toContain('paymentRetail.claim.viewResult')
    expect(wrapper.text()).not.toContain('payment.orders.requestRefund')
  })
  it('keeps existing refund eligibility for other gateways', async () => {
    const wrapper = await page('COMPLETED', 'alipay')
    expect(wrapper.find('[data-test="open-square-order"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.orders.requestRefund')
  })
})
