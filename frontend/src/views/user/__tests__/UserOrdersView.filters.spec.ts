import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../UserOrdersView.vue'
import Select from '@/components/common/Select.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'

const api = vi.hoisted(() => ({ getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn() }))
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { role: 'user' }, isSimpleMode: false }) }))
vi.mock('@/composables/useMerchantTestPaymentAccess', () => ({ useMerchantTestPaymentAccess: () => ({ merchantTestPaymentAccess: { value: false } }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  api.getMyOrders.mockImplementation(({ page }) => Promise.resolve({ data: { items: [{ id: page, status: 'COMPLETED' }], total: 100 } }))
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
})

async function openOrders() {
  const wrapper = mount(UserOrdersView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      OrderTable: true, BillingNavigation: true, RouterLink: { props: ['to'], template: '<a><slot /></a>' }, BaseDialog: true, Icon: true, InfiniteListSentinel: true, teleport: true
    } }
  })
  await flushPromises()
  wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
  await flushPromises()
  return wrapper
}

describe('order status filtering', () => {
  it('loads the first page with the selected status', async () => {
    const wrapper = await openOrders()
    const select = wrapper.getComponent(Select)
    await select.get('button').trigger('click')
    await select.findAll('[role="option"]').find(option => option.text() === 'payment.status.pending')!.trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 50, status: 'PENDING' })
    expect(wrapper.getComponent({ name: 'OrderTable' }).props('orders').map((order: { id: number }) => order.id)).toEqual([1])
    expect(api.getMyOrders).toHaveBeenCalledTimes(3)
  })

  it('refreshes the full accumulated extent without replacing it with only the last page', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 2, page_size: 50, status: undefined })
    expect(wrapper.getComponent({ name: 'OrderTable' }).props('orders').map((order: { id: number }) => order.id)).toEqual([1, 2])
  })
})
