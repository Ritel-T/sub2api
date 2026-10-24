import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UsageCleanupDialog from '../UsageCleanupDialog.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const { listCleanupTasks, createCleanupTask, cancelCleanupTask, showError } = vi.hoisted(() => ({ listCleanupTasks: vi.fn(), createCleanupTask: vi.fn(), cancelCleanupTask: vi.fn(), showError: vi.fn() }))
vi.mock('@/components/admin/usage/UsageFilters.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/api/admin/usage', () => ({ adminUsageAPI: { listCleanupTasks, createCleanupTask, cancelCleanupTask } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks(); listCleanupTasks.mockReset(); vi.spyOn(console, 'error').mockImplementation(() => {}) })
afterEach(() => { vi.restoreAllMocks() })
const page = (number: number) => ({ items: [{ id: number, status: 'succeeded', deleted_rows: number, filters: {} }], total: 15, page: number, page_size: 20 })
async function openDialog() {
  listCleanupTasks.mockResolvedValueOnce(page(1))
  const wrapper = mount(UsageCleanupDialog, {
    props: { show: false, filters: {}, startDate: '2026-09-01', endDate: '2026-09-02' },
    global: { stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' }, UsageFilters: true, ConfirmDialog: true, Pagination: true } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('continuous cleanup tasks', () => {
  it('appends tasks and refreshes the entire loaded range', async () => {
    const wrapper = await openDialog()
    listCleanupTasks.mockResolvedValueOnce(page(2))
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    expect(listCleanupTasks).toHaveBeenLastCalledWith({ page: 2, page_size: 20 })
    expect(wrapper.text()).toContain('#1')
    expect(wrapper.text()).toContain('#2')
    listCleanupTasks.mockResolvedValueOnce(page(1)).mockResolvedValueOnce(page(2))
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    expect(listCleanupTasks.mock.calls.slice(-2).map(call => call[0].page)).toEqual([1, 2])
    expect(wrapper.text()).toContain('#1')
    expect(wrapper.text()).toContain('#2')
  })

  it.each(['success', 'failure'])('ignores a pending append completing with %s after close and reopen', async outcome => {
    const wrapper = await openDialog()
    let resolve!: (value: object) => void
    let reject!: (error: Error) => void
    listCleanupTasks.mockImplementationOnce(() => new Promise((res, rej) => { resolve = res; reject = rej }))
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    await wrapper.setProps({ show: false })
    listCleanupTasks.mockResolvedValueOnce(page(3))
    await wrapper.setProps({ show: true })
    await flushPromises()
    if (outcome === 'success') resolve(page(2))
    else reject(new Error('old failure'))
    await flushPromises()
    expect(wrapper.text()).toContain('#3')
    expect(wrapper.text()).not.toContain('#2')
    expect(showError).not.toHaveBeenCalled()
  })

  it('preserves loaded tasks after failure and retries the same batch', async () => {
    const wrapper = await openDialog()
    listCleanupTasks.mockRejectedValueOnce(new Error('unavailable'))
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    expect(wrapper.text()).toContain('#1')
    expect(showError).toHaveBeenCalledWith('admin.usage.cleanup.loadFailed')
    listCleanupTasks.mockResolvedValueOnce(page(2))
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('retry')
    await flushPromises()
    expect(listCleanupTasks).toHaveBeenLastCalledWith({ page: 2, page_size: 20 })
    expect(wrapper.text()).toContain('#2')
  })

  it('does not create a cleanup task before explicit confirmation', async () => {
    const wrapper = await openDialog()
    await wrapper.findAll('button').find(button => button.text() === 'admin.usage.cleanup.submit')!.trigger('click')
    expect(createCleanupTask).not.toHaveBeenCalled()
    const confirmation = wrapper.findAllComponents(ConfirmDialog)[0]
    expect(confirmation.props('show')).toBe(true)
    createCleanupTask.mockResolvedValue({ id: 5 })
    listCleanupTasks.mockResolvedValueOnce(page(1))
    confirmation.vm.$emit('confirm')
    await flushPromises()
    expect(createCleanupTask).toHaveBeenCalledWith(expect.objectContaining({ start_date: '2026-09-01', end_date: '2026-09-02' }))
    expect(createCleanupTask).toHaveBeenCalledTimes(1)
  })
})


describe('cleanup confirmation lifecycle', () => {
  it('invalidates the cleanup request when filters change before acceptance', async () => {
    const wrapper = await openDialog()
    const vm = wrapper.vm as any
    vm.openConfirm()
    vm.localStartDate = '2026-10-01'
    await flushPromises()
    await vm.submitCleanup()
    expect(createCleanupTask).not.toHaveBeenCalled()
  })
  it('locks the submitted payload and ignores its late result after reopening', async () => {
    const wrapper = await openDialog()
    const vm = wrapper.vm as any
    let finish!: () => void
    createCleanupTask.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    vm.openConfirm()
    const request = vm.submitCleanup()
    void vm.submitCleanup()
    expect(createCleanupTask).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ show: false })
    listCleanupTasks.mockResolvedValueOnce(page(9))
    await wrapper.setProps({ show: true })
    await flushPromises()
    const reads = listCleanupTasks.mock.calls.length
    finish()
    await request
    expect(listCleanupTasks).toHaveBeenCalledTimes(reads)
  })
  it('snapshots cancellation target and blocks stale or duplicate acceptance', async () => {
    const wrapper = await openDialog()
    const vm = wrapper.vm as any
    let finish!: () => void
    cancelCleanupTask.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const task = { id: 7, status: 'pending' }
    vm.openCancelConfirm(task)
    task.id = 8
    const request = vm.cancelTask()
    void vm.cancelTask()
    expect(cancelCleanupTask.mock.calls).toEqual([[7]])
    await wrapper.setProps({ show: false })
    listCleanupTasks.mockResolvedValueOnce(page(9))
    await wrapper.setProps({ show: true })
    await flushPromises()
    const reads = listCleanupTasks.mock.calls.length
    finish()
    await request
    expect(listCleanupTasks).toHaveBeenCalledTimes(reads)
    await vm.cancelTask()
    expect(cancelCleanupTask).toHaveBeenCalledTimes(1)
  })
})
