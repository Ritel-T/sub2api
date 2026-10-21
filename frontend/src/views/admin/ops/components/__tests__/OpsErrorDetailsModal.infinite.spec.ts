import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsErrorDetailsModal from '../OpsErrorDetailsModal.vue'

const { request, upstream } = vi.hoisted(() => ({ request: vi.fn(), upstream: vi.fn() }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { listRequestErrors: request, listUpstreamErrors: upstream } }))
vi.mock('vue-i18n', async (original) => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

function open() {
  return mount(OpsErrorDetailsModal, {
    props: { show: true, timeRange: '1h', errorType: 'request' },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Select: true, OpsErrorLogTable: true } }
  })
}

describe('error details infinite requests', () => {
  it('appends within a fixed time window and clears old rows when sort changes', async () => {
    request.mockReset().mockResolvedValueOnce({ items: [{ id: 1 }], total: 2 })
      .mockResolvedValueOnce({ items: [{ id: 2 }], total: 2 })
      .mockResolvedValueOnce({ items: [{ id: 3 }], total: 1 })
    const wrapper = open()
    await flushPromises()
    const table = wrapper.findComponent({ name: 'OpsErrorLogTable' })
    const first = request.mock.calls[0][0]
    expect(first.start_time).toBeTruthy()
    expect(first.end_time).toBeTruthy()
    table.vm.$emit('load-more')
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith({ ...first, page: 2 })
    expect(table.props('rows')).toEqual([{ id: 1 }, { id: 2 }])
    table.vm.$emit('sort', 'status_code', 'asc')
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, sort_by: 'status_code', sort_order: 'asc' }))
    expect(table.props('rows')).toEqual([{ id: 3 }])
    wrapper.unmount()
  })

  it('keeps existing errors on a failed append and retries that page', async () => {
    request.mockReset().mockResolvedValueOnce({ items: [{ id: 1 }], total: 2 })
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ items: [{ id: 2 }], total: 2 })
    const wrapper = open()
    await flushPromises()
    const table = wrapper.findComponent({ name: 'OpsErrorLogTable' })
    table.vm.$emit('load-more')
    await flushPromises()
    expect(table.props('rows')).toEqual([{ id: 1 }])
    expect(table.props('error')).toBeInstanceOf(Error)
    table.vm.$emit('retry')
    await flushPromises()
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))
    expect(table.props('rows')).toEqual([{ id: 1 }, { id: 2 }])
    wrapper.unmount()
  })
})
