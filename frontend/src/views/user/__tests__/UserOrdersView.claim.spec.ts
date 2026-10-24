import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../UserOrdersView.vue'
const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn(), cancelOrder: vi.fn(), requestRefund: vi.fn() }))
const push = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { role: 'user' }, isSimpleMode: false }) }))
vi.mock('@/composables/useMerchantTestPaymentAccess', () => ({ useMerchantTestPaymentAccess: () => ({ merchantTestPaymentAccess: { value: false } }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  push.mockReset()
  showSuccess.mockReset()
  api.cancelOrder.mockReset()
  api.requestRefund.mockReset()
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: ['provider-1'] } })
})
async function page(status: string, type = 'squarespace') {
  api.getMyOrders.mockResolvedValue({ data: { items: [{ id: 42, amount: 10, status, payment_type: type, payment_claim_mode: 'receipt_otp', provider_instance_id: 'provider-1' }], total: 1 } })
  const wrapper = mount(UserOrdersView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' },
    OrderTable: { props: ['orders'], template: '<div><div v-for="row in orders" :key="row.id"><slot name="actions" :row="row" /></div></div>' },
    BillingNavigation: true, RouterLink: { props: ['to'], template: '<a><slot /></a>' }, BaseDialog: { props: ['show'], template: '<div v-if="show" data-test="refund-form"><slot /><slot name="footer" /></div>' },
    ConfirmDialog: { name: 'ConfirmDialog', props: ['show', 'busy'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-test="cancel-confirm" />' }, Icon: true, Pagination: true, Select: true,
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
  it('submits cancellation once and refuses cancel while its request is pending', async () => {
    let finish!: () => void
    api.cancelOrder.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const wrapper = await page('PENDING', 'alipay')
    await wrapper.findAll('button').find(button => button.text() === 'payment.orders.cancel')!.trigger('click')
    const dialog = wrapper.findComponent({ name: 'ConfirmDialog' })
    dialog.vm.$emit('confirm')
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(api.cancelOrder.mock.calls).toEqual([[42]])
    dialog.vm.$emit('cancel')
    await flushPromises()
    expect(dialog.props('show')).toBe(true)
    finish()
    await flushPromises()
    expect(dialog.props('show')).toBe(false)
  })

  it('captures the refund reason, prevents duplicates, and ignores the result after unmount', async () => {
    let finish!: () => void
    api.requestRefund.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const wrapper = await page('COMPLETED', 'alipay')
    await wrapper.findAll('button').find(button => button.text() === 'payment.orders.requestRefund')!.trigger('click')
    await wrapper.get('textarea').setValue('  original reason  ')
    const confirm = wrapper.get('[data-test="refund-form"] .btn-primary')
    const first = confirm.trigger('click')
    const second = confirm.trigger('click')
    await Promise.all([first, second])
    expect(api.requestRefund.mock.calls).toEqual([[42, { reason: 'original reason' }]])
    expect(wrapper.get('textarea').attributes('disabled')).toBeDefined()
    wrapper.unmount()
    finish()
    await flushPromises()
    expect(showSuccess).not.toHaveBeenCalled()
  })

})
