import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useGatewayBorrowRuntime } from '../useGatewayBorrowRuntime'
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/admin/astraGateway', () => ({ getAstraGatewayRuntime: get }))
afterEach(() => { vi.useRealTimers(); vi.clearAllMocks() })
describe('Shared account borrowing runtime', () => {
  it('fetches one batch for multiple target rows, expires with a shared clock and clears unverified stale data', async () => {
    vi.useFakeTimers()
    get.mockResolvedValue({ targets: [{ account_id: 34, model: 'gpt-6-astra' }, { account_id: 34, model: 'gpt-6-sol' }, { account_id: 109, model: 'gpt-6-astra' }] })
    let runtime!: ReturnType<typeof useGatewayBorrowRuntime>
    const enabled = ref(true)
    const w = mount(defineComponent({ setup() { runtime = useGatewayBorrowRuntime(computed(() => enabled.value)); return {} }, template: '<div />' }))
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(1)
    expect(runtime.byAccount.value['34']).toHaveLength(2)
    expect(runtime.byAccount.value['34'][1].model).toBe('gpt-6.1-sol')
    const now = runtime.now.value
    await vi.advanceTimersByTimeAsync(1000)
    expect(runtime.now.value).toBeGreaterThan(now)
    expect(get).toHaveBeenCalledTimes(1)
    get.mockRejectedValue(new Error('network'))
    await vi.advanceTimersByTimeAsync(9000); await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect(runtime.failed.value).toBe(true)
    expect(runtime.byAccount.value).toEqual({})
    enabled.value = false; await flushPromises(); await vi.advanceTimersByTimeAsync(10000)
    expect(get).toHaveBeenCalledTimes(2)
    w.unmount(); await vi.advanceTimersByTimeAsync(10000)
    expect(get).toHaveBeenCalledTimes(2)
  })
})
