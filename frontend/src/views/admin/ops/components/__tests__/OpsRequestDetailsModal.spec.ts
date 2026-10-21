import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { OpsDashboardOverview } from '@/api/admin/ops'
import { flushPromises, mount, shallowMount } from '@vue/test-utils'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import OpsRequestDetailsModal from '../OpsRequestDetailsModal.vue'
import { resetAccountNameCacheForTests } from '../../utils/accountNameMap'

const { listRequestDetails, listAccounts, viewport } = vi.hoisted(() => ({
  listRequestDetails: vi.fn(),
  listAccounts: vi.fn(),
  viewport: { desktop: true },
}))

vi.mock('@vueuse/core', async (importOriginal) => ({ ...await importOriginal<typeof import('@vueuse/core')>(), useMediaQuery: () => ref(viewport.desktop) }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { listRequestDetails } }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: { list: listAccounts } }))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
  useAdminSettingsStore: () => ({ opsRealtimeMonitoringEnabled: false }),
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

async function openDetails(sort: 'duration_desc' | 'ttft_desc') {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: viewport.desktop ? 1280 : 390 })
  vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: viewport.desktop, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }))
  const wrapper = mount(OpsRequestDetailsModal, {
    props: { modelValue: false, timeRange: '1h', preset: { title: 'Details', sort } },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Pagination: true } },
  })
  await wrapper.setProps({ modelValue: true })
  await flushPromises()
  return wrapper
}

afterEach(() => { vi.restoreAllMocks() })

describe('Ops request latency details', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetAccountNameCacheForTests()
    viewport.desktop = true
    listRequestDetails.mockResolvedValue({
      items: [
        { kind: 'success', created_at: '2026-09-10T00:00:00Z', duration_ms: 12000, first_token_ms: 800, account_id: 101 },
        { kind: 'success', created_at: '2026-09-10T00:00:01Z', duration_ms: 9000, first_token_ms: 0, account_id: 102 },
        { kind: 'success', created_at: '2026-09-10T00:00:02Z', duration_ms: 5000, first_token_ms: null, account_id: 999 },
      ],
      total: 3,
    })
    listAccounts.mockResolvedValue({
      items: [
        { id: 101, name: 'acc-alpha' },
        { id: 102, name: 'acc-beta' },
      ],
      total: 2,
    })
  })

  it('opens the TTFT card with first-token sorting and successful requests', async () => {
    const wrapper = shallowMount(OpsDashboardHeader, {
      props: { overview: {} as OpsDashboardOverview, platform: '', groupId: null, timeRange: '1h', queryMode: 'auto', loading: false, lastUpdated: null },
    })
    await flushPromises()
    const button = wrapper.findAll('button').find((item) =>
      item.text() === 'admin.ops.requestDetails.details' &&
      item.element.parentElement?.textContent?.includes('TTFT'),
    )
    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('openRequestDetails')).toEqual([[
      { title: 'admin.ops.ttftLabel', kind: 'success', sort: 'ttft_desc' },
    ]])
    wrapper.unmount()
  })

  it.each([true, false])('shows TTFT rather than total duration (desktop: %s)', async (desktop) => {
    viewport.desktop = desktop
    const wrapper = await openDetails('ttft_desc')
    expect(listRequestDetails).toHaveBeenCalledWith(expect.objectContaining({ sort: 'ttft_desc' }))
    expect(wrapper.text()).toContain('admin.ops.ttftLabel')
    expect(wrapper.text()).toContain('800 ms')
    expect(wrapper.text()).toContain('0 ms')
    expect(wrapper.text()).not.toContain('12000 ms')
    expect(wrapper.text()).not.toContain('9000 ms')
    expect(wrapper.text()).not.toContain('5000 ms')
    if (desktop) expect(wrapper.findAll('tbody tr')[2].find('[data-column-key="latency"]').text()).toBe('-')
    else expect(wrapper.findAll('[data-field="latency"]')[2].text()).toContain('-')
    wrapper.unmount()
  })

  it('shows account names with id fallback in the account column', async () => {
    const wrapper = await openDetails('ttft_desc')
    for (const toggle of wrapper.findAll('[data-test="toggle-details"]')) await toggle.trigger('click')
    expect(wrapper.text()).toContain('admin.ops.requestDetails.table.account')
    const accountCells = wrapper.findAll('.table-detail-row [data-field="account"] dd').map(cell => cell.text())
    expect(accountCells).toEqual(['acc-alpha', 'acc-beta', '#999'])
    // 999 不在账号映射里，应触发一次强制刷新重建缓存
    expect(listAccounts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('keeps total duration for duration details', async () => {
    const wrapper = await openDetails('duration_desc')
    expect(listRequestDetails).toHaveBeenCalledWith(expect.objectContaining({ sort: 'duration_desc' }))
    expect(wrapper.text()).toContain('admin.ops.requestDetails.table.duration')
    expect(wrapper.text()).toContain('12000 ms')
    expect(wrapper.text()).not.toContain('800 ms')
    wrapper.unmount()
  })
  it('appends details without changing its time window and retries a failed next batch', async () => {
    const first = { kind: 'success', request_id: 'request-1', created_at: '2026-10-20T00:00:00Z', duration_ms: 100 }
    const second = { ...first, request_id: 'request-2', created_at: '2026-10-20T00:00:01Z' }
    listRequestDetails.mockResolvedValueOnce({ items: [first], total: 2 })
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ items: [second], total: 2 })
    const wrapper = await openDetails('duration_desc')
    const initialQuery = listRequestDetails.mock.calls[0][0]
    await wrapper.get('.infinite-list-action').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.get('.infinite-list-action').text()).toBe('Retry')
    await wrapper.get('.infinite-list-action').trigger('click')
    await flushPromises()
    expect(listRequestDetails).toHaveBeenLastCalledWith({ ...initialQuery, page: 2 })
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.find('.infinite-list-action').exists()).toBe(false)
    wrapper.unmount()
  })

  it('offers retry after a failed initial request', async () => {
    listRequestDetails.mockRejectedValueOnce(new Error('offline'))
    const wrapper = await openDetails('duration_desc')
    expect(wrapper.get('.infinite-list-action').text()).toBe('Retry')
    await wrapper.get('.infinite-list-action').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    wrapper.unmount()
  })

})
