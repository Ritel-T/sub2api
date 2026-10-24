import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import type { PaymentOrder } from '@/types/payment'
import AdminRefundDialog from '../AdminRefundDialog.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

async function openRefund(overrides: Partial<PaymentOrder> = {}) {
  const order: PaymentOrder = {
    id: 1, user_id: 10, amount: 100, pay_amount: 100, fee_rate: 0,
    payment_type: 'stripe', out_trade_no: 'order-1', status: 'COMPLETED',
    order_type: 'balance', created_at: '2026-09-01', expires_at: '2026-09-02',
    refund_amount: 0, ...overrides
  }
  const wrapper = mount(AdminRefundDialog, {
    props: { show: false, order, userBalance: 50 },
    global: { stubs: { BaseDialog: { name: 'BaseDialog', template: '<div><slot /><slot name="footer" /></div>' } } }
  })
  await wrapper.setProps({ show: true })
  return wrapper
}

const warning = 'payment.admin.insufficientBalance'

describe('refund balance warning', () => {
  it('compares the available balance with the edited refund amount', async () => {
    const wrapper = await openRefund()
    expect(wrapper.text()).toContain(warning)
    await wrapper.get('input[type="number"]').setValue('20')
    expect(wrapper.text()).not.toContain(warning)
    await wrapper.get('input[type="number"]').setValue('75')
    expect(wrapper.text()).toContain(warning)
  })

  it('does not warn when the balance exactly covers the refund', async () => {
    const wrapper = await openRefund()
    await wrapper.get('input[type="number"]').setValue('50')
    expect(wrapper.text()).not.toContain(warning)
  })

  it('uses the remaining amount for an already partially refunded order', async () => {
    const wrapper = await openRefund({ status: 'PARTIALLY_REFUNDED', refund_amount: 80 })
    expect((wrapper.get('input[type="number"]').element as HTMLInputElement).value).toBe('20')
    expect(wrapper.text()).not.toContain(warning)
  })

  it('keeps the warning hidden when balance deduction is disabled', async () => {
    const wrapper = await openRefund()
    await wrapper.get('#deduct-balance').setValue(false)
    expect(wrapper.text()).not.toContain(warning)
  })
  it('blocks duplicate same-turn submit and all busy close paths, then allows retry', async () => {
    const wrapper = await openRefund()
    await wrapper.get('textarea').setValue('requested')
    const form = wrapper.get('form')
    const first = form.trigger('submit')
    const second = form.trigger('submit')
    await Promise.all([first, second])
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    expect(wrapper.emitted('confirm')![0][1]).toBe(1)
    await wrapper.setProps({ submitting: true })
    await form.trigger('submit')
    wrapper.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    expect(wrapper.emitted('cancel')).toBeUndefined()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ submitting: false })
    await form.trigger('submit')
    expect(wrapper.emitted('confirm')).toHaveLength(2)
  })

  it('resets reason, amount and force when an open form switches orders', async () => {
    const wrapper = await openRefund()
    await wrapper.get('textarea').setValue('reason A')
    await wrapper.get('input[type="number"]').setValue(25)
    const previous = wrapper.props('order')!
    await wrapper.setProps({ order: { ...previous, id: 2, amount: 40, refund_request_reason: 'reason B' } })
    expect((wrapper.get('input[type="number"]').element as HTMLInputElement).value).toBe('40')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('reason B')
  })

})
