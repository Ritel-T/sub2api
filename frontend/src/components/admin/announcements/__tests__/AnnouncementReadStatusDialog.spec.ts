import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import DataTable from '@/components/common/DataTable.vue'

import AnnouncementReadStatusDialog from '../AnnouncementReadStatusDialog.vue'

const { getReadStatus, showError } = vi.hoisted(() => ({
  getReadStatus: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    announcements: {
      getReadStatus,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/composables/usePersistedPageSize', () => ({
  getPersistedPageSize: () => 20,
}))

const BaseDialogStub = {
  props: ['show', 'title', 'width'],
  emits: ['close'],
  template: '<div><slot /><slot name="footer" /></div>',
}

describe('AnnouncementReadStatusDialog', () => {
  afterEach(() => vi.useRealTimers())
  beforeEach(() => {
    getReadStatus.mockReset()
    showError.mockReset()
    vi.useFakeTimers()
  })

  it('appends read statuses, snapshots search, and isolates a different announcement', async () => {
    const item = (id: number) => ({ user_id: id, email: `reader${id}@example.invalid`, balance: 1, eligible: true })
    getReadStatus.mockResolvedValueOnce({ items: [item(1)], total: 3 })
    const wrapper = mount(AnnouncementReadStatusDialog, {
      props: { show: true, announcementId: 1 },
      global: { stubs: { BaseDialog: BaseDialogStub, DataTable: true, Icon: true } },
    })
    await flushPromises()
    await wrapper.get('input').setValue('alice')
    getReadStatus.mockResolvedValueOnce({ items: [item(2)], total: 3 })
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    expect(getReadStatus).toHaveBeenLastCalledWith(1, 2, 50, expect.objectContaining({ search: '' }), expect.any(Object))
    expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(2)
    let resolveOld!: (value: unknown) => void
    getReadStatus.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    const oldSignal = getReadStatus.mock.calls.at(-1)![4].signal as AbortSignal
    getReadStatus.mockResolvedValueOnce({ items: [item(9)], total: 1 })
    await wrapper.setProps({ announcementId: 2 })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(getReadStatus).toHaveBeenLastCalledWith(2, 1, 50, expect.objectContaining({ search: '' }), expect.any(Object))
    resolveOld({ items: [item(3)], total: 3 })
    await flushPromises()
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([item(9)])
    await vi.advanceTimersByTimeAsync(350)
    expect(getReadStatus).toHaveBeenCalledTimes(4)
    wrapper.unmount()
  })

  it('closes by aborting active requests and clearing debounced reloads', async () => {
    let activeSignal: AbortSignal | undefined
    getReadStatus.mockImplementation(async (...args: any[]) => {
      activeSignal = args[4]?.signal
      return new Promise(() => {})
    })

    const wrapper = mount(AnnouncementReadStatusDialog, {
      props: {
        show: false,
        announcementId: 1,
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          DataTable: true,
          Pagination: true,
          Icon: true,
        },
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getReadStatus).toHaveBeenCalledTimes(1)
    expect(activeSignal?.aborted).toBe(false)

    const setupState = (wrapper.vm as any).$?.setupState
    setupState.search = 'alice'
    setupState.handleSearch()

    setupState.handleClose()
    await flushPromises()

    expect(activeSignal?.aborted).toBe(true)
    expect(wrapper.emitted('close')).toHaveLength(1)

    vi.advanceTimersByTime(350)
    await flushPromises()

    expect(getReadStatus).toHaveBeenCalledTimes(1)
  })
})
