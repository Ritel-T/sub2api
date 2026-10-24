import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import View from '../RequestCaptureView.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
const mocks = vi.hoisted(() => ({ listTasks: vi.fn(), createTask: vi.fn(), listRecords: vi.fn(), getRecord: vi.fn(), getContent: vi.fn(), list: vi.fn(), replace: vi.fn() }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/admin/operations/SmartOpsNav.vue', () => ({ default: { template: '<nav />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ replace: mocks.replace }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ setRequestCaptureEnabledLocal: vi.fn() }) }))
vi.mock('@/api', () => ({ adminAPI: { users: { list: mocks.list }, accounts: { list: mocks.list }, groups: { list: mocks.list } } }))
vi.mock('@/api/admin/requestCaptures', () => ({ ...mocks, canStreamExport: () => false, stopTask: vi.fn(), deleteTask: vi.fn(), exportCapture: vi.fn() }))
const task = { id: 'task', target_type: 'user', target_id: 7, target_name: 'fixture', status: 'running', requests: 1, partial: 0, skipped: 0, bytes: 100, expires_at: '2099-01-01' }
const record = { id: 'record', task_id: 'task', request_id: 'request-id', parts: [{ name: '1-client_request.txt', stage: 'client_request', bytes: 10, attempt: 0, turn: 0 }] }
const options = { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } }
beforeEach(() => { localStorage.removeItem('admin-request-capture-records'); vi.clearAllMocks(); mocks.listTasks.mockResolvedValue({ items: [task], stats: {}, has_more: false }); mocks.list.mockResolvedValue({ items: [{ id: 7, email: 'user@test' }], total: 1 }); mocks.createTask.mockResolvedValue(task); mocks.listRecords.mockResolvedValue({ items: [record], has_more: false }); mocks.getRecord.mockResolvedValue(record); mocks.getContent.mockResolvedValue({ text: '<img src=x onerror=alert(1)>', next_offset: 10, has_more: true }) })
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })
describe('request capture page', () => {
  it('appends targets and tasks with deduplication and preserves the loaded task window on refresh', async () => {
    mocks.list.mockImplementation(async (page: number) => ({ items: page === 1 ? [{ id: 7, email: 'user@test' }] : [{ id: 8, email: 'next@test' }], total: 2 }))
    mocks.listTasks.mockImplementation(async (page: number) => ({ items: page === 1 ? [task] : [task, { ...task, id: 'task2', target_name: 'second-task' }], stats: {}, has_more: page === 1 }))
    const wrapper = mount(View, options); await flushPromises()
    await wrapper.get('[data-testid="capture-targets-more"] button').trigger('click'); await flushPromises()
    expect(wrapper.findAll('select')[1]!.text()).toContain('user@test')
    expect(wrapper.findAll('select')[1]!.text()).toContain('next@test')
    await wrapper.get('[data-testid="capture-tasks-more"] button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('second-task')
    mocks.listTasks.mockClear()
    await wrapper.findAll('button').find(b => b.text() === 'admin.requestCapture.refresh')!.trigger('click'); await flushPromises()
    expect(mocks.listTasks.mock.calls.map(call => call[0])).toEqual([1, 2])
    expect(wrapper.text()).toContain('second-task')
    wrapper.unmount()
  })
  it('retries the failed record page without losing rows, then resets the exact filter', async () => {
    mocks.listRecords.mockResolvedValueOnce({ items: [record], has_more: true })
    const wrapper = mount(View, options); await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('#7'))!.trigger('click'); await flushPromises()
    mocks.listRecords.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('[data-testid="capture-records-more"] button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('request-id')
    expect(mocks.listRecords).toHaveBeenLastCalledWith('task', 2, '', true, expect.any(AbortSignal))
    mocks.listRecords.mockResolvedValueOnce({ items: [{ ...record, id: 'record2', request_id: 'next-request' }], has_more: false })
    await wrapper.get('[data-testid="capture-records-more"] button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('request-id'); expect(wrapper.text()).toContain('next-request')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.get('input[placeholder="Request ID"]').setValue('exact')
    mocks.listRecords.mockResolvedValueOnce({ items: [], has_more: false })
    await wrapper.findAll('form')[1]!.trigger('submit'); await flushPromises()
    expect(mocks.listRecords).toHaveBeenLastCalledWith('task', 1, 'exact', true, expect.any(AbortSignal))
    expect(wrapper.get('[data-testid="capture-records-section"]').findAll('[data-index]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('next-request')
    expect(wrapper.text()).toContain('#7')
    wrapper.unmount()
  })
  it('cancels target searches immediately during debounce and ignores stale results', async () => {
    vi.useFakeTimers()
    let resolveOld!: (value: unknown) => void
    mocks.list.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = mount(View, options); await flushPromises()
    const oldSignal = mocks.list.mock.calls[0]![3].signal as AbortSignal
    await wrapper.get('input[type="search"]').setValue('new')
    expect(oldSignal.aborted).toBe(true)
    resolveOld({ items: [{ id: 99, email: 'old@test' }], total: 1 }); await flushPromises()
    expect(wrapper.text()).not.toContain('old@test')
    await vi.advanceTimersByTimeAsync(250); await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith(1, 20, { search: 'new' }, expect.anything())
    wrapper.unmount()
  })
  it('cancels stale record results after a filter reset and unmount', async () => {
    let resolveOld!: (value: unknown) => void
    mocks.listRecords.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = mount(View, options); await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('#7'))!.trigger('click'); await flushPromises()
    const oldSignal = mocks.listRecords.mock.calls[0]![4] as AbortSignal
    await wrapper.get('input[placeholder="Request ID"]').setValue('new-filter')
    mocks.listRecords.mockResolvedValueOnce({ items: [], has_more: false })
    await wrapper.findAll('form')[1]!.trigger('submit'); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    resolveOld({ items: [{ ...record, request_id: 'stale-request' }], has_more: false }); await flushPromises()
    expect(wrapper.text()).not.toContain('stale-request')
    const currentSignal = mocks.listRecords.mock.calls.at(-1)![4] as AbortSignal
    wrapper.unmount(); expect(currentSignal.aborted).toBe(true)
  })
  it('validates duration and defaults media capture off', async () => {
    const wrapper = mount(View, options); await flushPromises()
    await wrapper.findAll('select')[1]!.setValue('7')
    await wrapper.find('input[type=number]').setValue('0'); await wrapper.find('form').trigger('submit'); expect(mocks.createTask).not.toHaveBeenCalled()
    await wrapper.find('input[type=number]').setValue('1440'); await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(mocks.createTask).toHaveBeenCalledWith({ target_type: 'user', target_id: 7, duration_minutes: 1440, save_media: false }); wrapper.unmount()
  })
  it('renders capture content as text and replaces preview pages', async () => {
    const wrapper = mount(View, options); await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('#7'))!.trigger('click'); await flushPromises()
    expect(mocks.listRecords).toHaveBeenLastCalledWith('task', 1, '', true, expect.any(AbortSignal))
    expect(wrapper.findAll('input[type=checkbox]')).toHaveLength(1) // Only media remains configurable.
    await wrapper.findAll('button').find(b => b.text() === 'request-id')!.trigger('click'); await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false); expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    mocks.getContent.mockResolvedValueOnce({ text: 'next page', next_offset: 19, has_more: false })
    await wrapper.findAll('button').find(b => b.text() === 'admin.requestCapture.nextSegment')!.trigger('click'); await flushPromises()
    expect(mocks.getContent).toHaveBeenLastCalledWith('task', 'record', '1-client_request.txt', 10); expect(wrapper.text()).not.toContain('<img src=x'); expect(wrapper.text()).toContain('next page'); wrapper.unmount()
  })
  it.each([
    ['failed', 'basispoints_stream_incomplete', 'streamFailed'],
    ['completed', 'upstream_read_failed', 'completedWithErrors'],
    [undefined, 'unknown_error', 'containsErrors'],
  ])('shows business outcome for HTTP 200: %s', async (outcome, code, label) => {
    mocks.listRecords.mockResolvedValue({ items: [{ ...record, status: 200, is_error: true, client_outcome: outcome, error_code: code }], has_more: false })
    const wrapper = mount(View, options); await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('#7'))!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('HTTP 200')
    expect(wrapper.get('[data-testid="capture-outcome"]').text()).toBe('admin.requestCapture.' + label)
    expect(wrapper.text()).toContain(code)
    wrapper.unmount()
  })

  it('windows large record histories and preserves detail access through persisted column preferences', async () => {
    const rows = Array.from({ length: 140 }, (_, index) => ({ ...record, id: `record-${index}`, request_id: `request-${index}`, model: 'fixture-model', status: 200, protocol: 'responses', turn: 2 }))
    mocks.listRecords.mockResolvedValue({ items: rows, has_more: false })
    const wrapper = mount(View, { ...options, attachTo: document.body }); await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('#7'))!.trigger('click'); await flushPromises()
    const table = wrapper.findAllComponents(DataTable).find(item => item.props('columnOrderKey') === 'admin-request-capture-records')!
    expect(table.props('data')).toHaveLength(140)
    expect(table.props('columns').every(column => !column.sortable)).toBe(true)
    const rendered = wrapper.get('[data-testid="capture-records-section"]').findAll('[data-index]')
    expect(rendered.length).toBeGreaterThan(0); expect(rendered.length).toBeLessThan(40)
    table.getComponent(ColumnSettings).vm.$emit('toggle', 'model'); await flushPromises()
    expect(JSON.parse(localStorage.getItem('admin-request-capture-records')!).primary.model).toBe(false)
    await wrapper.get('[data-testid="capture-records-section"] [data-test="toggle-details"]').trigger('click')
    expect(wrapper.text()).toContain('fixture-model')
    await wrapper.findAll('button').find(button => button.text() === 'request-0')!.trigger('click'); await flushPromises()
    expect(mocks.getRecord).toHaveBeenCalledWith('task', 'record-0')
    expect(wrapper.text()).toContain('responses')
    wrapper.unmount()
  })

})
