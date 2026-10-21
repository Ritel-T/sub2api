import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminUser } from '@/types'
import UserBalanceHistoryModal from '../UserBalanceHistoryModal.vue'

const mocks = vi.hoisted(() => ({ getUserBalanceHistory: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: mocks } }))
vi.mock('@/utils/format', () => ({ formatDateTime: () => 'date' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks(); vi.spyOn(console, 'error').mockImplementation(() => {}) })
afterEach(() => vi.restoreAllMocks())
function result(id: number) {
  return { items: [{ id, type: 'admin_balance', value: id, notes: `History ${id}` }], total: 1, total_recharged: id }
}
function deferred() {
  let resolve!: (value: ReturnType<typeof result>) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<ReturnType<typeof result>>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
async function openDialog() {
  const wrapper = mount(UserBalanceHistoryModal, {
    props: { show: false, user: { id: 1, email: 'one@example.com', balance: 1 } as AdminUser },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' }, Icon: true, Select: true } }
  })
  await wrapper.setProps({ show: true })
  return wrapper
}

describe('UserBalanceHistoryModal request ordering', () => {
  it('shows signup gifts as balance and supports the dedicated filter', async () => {
    mocks.getUserBalanceHistory.mockResolvedValue({
      items: [{ id: -1, code: 'AFF-SIGNUP-1', type: 'affiliate_signup_bonus', value: 5, notes: '' }],
      total: 1,
      total_recharged: 0
    })
    const wrapper = await openDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.users.typeAffiliateSignupBonus')
    expect(wrapper.text()).toContain('+$5.00')
    expect(wrapper.find('.text-emerald-600').exists()).toBe(true)
    const select = wrapper.findComponent({ name: 'Select' })
    expect(select.props('options')).toEqual(expect.arrayContaining([
      { value: 'affiliate_signup_bonus', label: 'admin.users.typeAffiliateSignupBonus' }
    ]))
    select.vm.$emit('update:modelValue', 'affiliate_signup_bonus')
    await flushPromises()
    select.vm.$emit('change', 'affiliate_signup_bonus')
    await flushPromises()
    expect(mocks.getUserBalanceHistory).toHaveBeenLastCalledWith(1, 1, 15, 'affiliate_signup_bonus', { signal: expect.any(AbortSignal) })
  })

  it('shows inviter first-recharge gifts separately from recharge totals and supports their filter', async () => {
    mocks.getUserBalanceHistory.mockResolvedValue({
      items: [{ id: -2, code: 'AFF-FIRST-2', type: 'affiliate_first_recharge_bonus', value: 5, notes: '' }],
      total: 1,
      total_recharged: 0
    });
    const wrapper = await openDialog();
    await flushPromises();
    expect(wrapper.text()).toContain('admin.users.typeAffiliateFirstRechargeBonus');
    expect(wrapper.text()).toContain('+$5.00');
    const select = wrapper.findComponent({ name: 'Select' });
    expect(select.props('options')).toEqual(expect.arrayContaining([
      { value: 'affiliate_first_recharge_bonus', label: 'admin.users.typeAffiliateFirstRechargeBonus' }
    ]));
    select.vm.$emit('update:modelValue', 'affiliate_first_recharge_bonus');
    await flushPromises();
    select.vm.$emit('change', 'affiliate_first_recharge_bonus');
    await flushPromises();
    expect(mocks.getUserBalanceHistory).toHaveBeenLastCalledWith(1, 1, 15, 'affiliate_first_recharge_bonus', { signal: expect.any(AbortSignal) });
  });

  it('keeps the new user history when an old response finishes later', async () => {
    const old = deferred()
    mocks.getUserBalanceHistory.mockReturnValueOnce(old.promise).mockResolvedValueOnce(result(20))
    const wrapper = await openDialog()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, user: { id: 2, email: 'two@example.com', balance: 2 } as AdminUser })
    await flushPromises()
    old.resolve(result(10))
    await flushPromises()
    expect(wrapper.text()).toContain('History 20')
    expect(wrapper.text()).not.toContain('History 10')
    expect(wrapper.text()).toContain('$20.00')
  })

  it('does not end the current filter loading state when an old request finishes', async () => {
    const old = deferred()
    const current = deferred()
    mocks.getUserBalanceHistory.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openDialog()
    wrapper.findComponent({ name: 'Select' }).vm.$emit('change', 'balance')
    await flushPromises()
    old.resolve(result(10))
    await flushPromises()
    expect(wrapper.find('svg.animate-spin').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('History 10')
    current.resolve(result(20))
    await flushPromises()
    expect(wrapper.find('svg.animate-spin').exists()).toBe(false)
    expect(wrapper.text()).toContain('History 20')
  })

  it.each(['close', 'unmount'])('ignores failures after %s', async (action) => {
    const pending = deferred()
    mocks.getUserBalanceHistory.mockReturnValueOnce(pending.promise)
    const wrapper = await openDialog()
    if (action === 'close') await wrapper.setProps({ show: false })
    else wrapper.unmount()
    pending.reject(new Error('Stale error'))
    await flushPromises()
    expect(console.error).not.toHaveBeenCalled()
  })

  it('still reports current failures and ends loading', async () => {
    const error = new Error('Current error')
    mocks.getUserBalanceHistory.mockRejectedValueOnce(error)
    const wrapper = await openDialog()
    await flushPromises()
    expect(console.error).toHaveBeenCalledWith('Failed to load balance history:', error)
    expect(wrapper.find('svg.animate-spin').exists()).toBe(false)
  })
  it('appends money history without replacing previous records or changing the recharge summary', async () => {
    mocks.getUserBalanceHistory.mockResolvedValueOnce({ ...result(1), total: 2, total_recharged: 500 })
    const wrapper = await openDialog()
    await flushPromises()
    mocks.getUserBalanceHistory.mockResolvedValueOnce({ ...result(2), total: 2, total_recharged: 500 })
    wrapper.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more')
    await flushPromises()
    expect(mocks.getUserBalanceHistory).toHaveBeenLastCalledWith(1, 2, 15, undefined, { signal: expect.any(AbortSignal) })
    expect(wrapper.text()).toContain('History 1')
    expect(wrapper.text()).toContain('History 2')
    expect(wrapper.text()).toContain('$500.00')
    expect(wrapper.getComponent({ name: 'InfiniteListSentinel' }).props('hasMore')).toBe(false)
  })

})
