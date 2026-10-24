import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import BatchImageGuideView from '../BatchImageGuideView.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'

const mocks = vi.hoisted(() => ({
  keys: vi.fn(), jobs: vi.fn(), job: vi.fn(), items: vi.fn(), content: vi.fn(), copy: vi.fn(), error: vi.fn(),
}))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ apiBaseUrl: '', fetchPublicSettings: vi.fn().mockResolvedValue(undefined), showError: mocks.error, showSuccess: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: mocks.copy }) }))
vi.mock('@/api', () => ({ keysAPI: { list: mocks.keys } }))
vi.mock('@/api/batchImage', () => ({
  listBatchImageJobs: mocks.jobs, getBatchImageJob: mocks.job, listBatchImageItems: mocks.items,
  getBatchImageItemContent: mocks.content, listBatchImageModels: vi.fn().mockResolvedValue({ data: [] }),
  cancelBatchImageJob: vi.fn(), deleteBatchImageJobRecord: vi.fn(), downloadBatchImageZip: vi.fn(), saveBlob: vi.fn(), submitBatchImageJob: vi.fn(),
}))
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
enableAutoUnmount(afterEach)
function viewport(desktop: boolean) {
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({ matches: desktop, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
}
function job(id: string, parent: string | null = null) {
  return { id, object: 'batch_image_job', task_name: id, parent_batch_id: parent, status: 'completed', model: 'gemini', provider: 'gemini_api', item_count: 200, success_count: 200, fail_count: 0, estimated_cost: 0, hold_amount: 0, actual_cost: 0, created_at: 1, submitted_at: 1, settled_at: 1, downloaded_at: null }
}
function item(index = 0) {
  return { custom_id: `img_${index}`, status: 'succeeded', prompt_preview: `Complete original prompt ${index}`, mime_type: 'image/png', file_extension: 'png', image_count: 1, error: null }
}
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear(); viewport(true)
  mocks.keys.mockResolvedValue({ items: [{ id: 42, key: 'sk-synthetic', name: 'Synthetic key', status: 'active', group: { platform: 'gemini', allow_batch_image_generation: true } }] })
  mocks.jobs.mockResolvedValue({ data: [job('parent'), job('child', 'parent')], has_more: false })
  mocks.job.mockResolvedValue(job('parent'))
  mocks.items.mockResolvedValue({ data: [item()] })
  mocks.content.mockResolvedValue(new Blob(['image'], { type: 'image/png' }))
  vi.stubGlobal('createImageBitmap', vi.fn().mockRejectedValue(new Error('Use original blob')))
  vi.stubGlobal('URL', class extends URL { static createObjectURL = vi.fn().mockReturnValue('blob:synthetic'); static revokeObjectURL = vi.fn() })
})
async function openBatch() {
  const wrapper = mount(BatchImageGuideView, { attachTo: document.body, global: { stubs: {
    AppLayout: { template: '<main><slot/></main>' },
    TablePageLayout: { template: '<div><slot name="header"/><slot name="filters"/><slot name="table"/><slot/></div>' },
    BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" :data-dialog="title"><div class="test-dialog-body" style="max-height:260px;overflow-y:auto"><slot/></div><slot name="footer"/></section>' },
    Icon: true, 'i18n-t': true,
  } } })
  await flushPromises()
  ;(wrapper.vm as any).selectJob('parent')
  await flushPromises()
  return wrapper
}
function detailTable(wrapper: Awaited<ReturnType<typeof openBatch>>) {
  return wrapper.findAllComponents(DataTable).find(table => table.props('columnOrderKey') === 'user-batch-image-items')!
}

describe('Batch image detail shared records', () => {
  it('windows a 200-output parent and child results in the dialog while retaining every complete record and composite key', async () => {
    mocks.items.mockImplementation((_key: string, batchId: string) => Promise.resolve({ data: Array.from({ length: batchId === 'parent' ? 200 : 25 }, (_, index) => item(index)) }))
    const wrapper = await openBatch()
    const table = detailTable(wrapper)
    expect(mocks.items.mock.calls.map(call => call.slice(0, 2))).toEqual([['sk-synthetic', 'parent'], ['sk-synthetic', 'child']])
    expect(table.props('data')).toHaveLength(225)
    expect(table.props('columns').every((column: any) => column.primary)).toBe(true)
    expect(table.props('columns').map((column: any) => column.key)).toEqual(['custom_id', 'prompt', 'status', 'preview', 'result'])
    expect(table.props('columns').some((column: any) => column.sortable)).toBe(false)
    const keyOf = table.props('rowKey') as (row: any) => string
    expect(new Set(table.props('data').map(keyOf)).size).toBe(225)
    const body = table.get('tbody').element as HTMLElement
    const host = body.closest('.test-dialog-body') as HTMLElement
    Object.defineProperty(host, 'clientHeight', { configurable: true, value: 260 })
    host.getBoundingClientRect = () => ({ top: 0, bottom: 260, left: 0, right: 900, width: 900, height: 260, x: 0, y: 0, toJSON() {} })
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
    body.getBoundingClientRect = () => ({ top: -host.scrollTop, bottom: 225 * 80 - host.scrollTop, left: 0, right: 900, width: 900, height: 225 * 80, x: 0, y: -host.scrollTop, toJSON() {} })
    host.dispatchEvent(new Event('scroll'))
    await vi.waitFor(() => { expect(table.findAll('tr[data-index]').length).toBeGreaterThan(0); expect(table.findAll('tr[data-index]').length).toBeLessThan(40) })
    host.scrollTop = 200 * 80; host.dispatchEvent(new Event('scroll'))
    await vi.waitFor(() => expect(table.find('tr[data-index="200"]').exists()).toBe(true))
    const childRow = table.get('tr[data-index="200"]')
    expect(childRow.text()).toContain('img_0')
    expect(childRow.text()).toContain('Complete original prompt 0')
    expect(childRow.find('button[aria-label="batchImage.detail.previewLoad"]').exists()).toBe(true)
    expect(table.props('data')).toHaveLength(225)
    expect(mocks.error).not.toHaveBeenCalled()
  })

  it('persists detail preferences and preserves child Prompt focus, copy, Escape and preview targets in expanded fields', async () => {
    const wrapper = await openBatch()
    const table = detailTable(wrapper)
    const settings = table.getComponent(ColumnSettings)
    settings.vm.$emit('toggle', 'prompt'); settings.vm.$emit('toggle', 'preview')
    await flushPromises()
    await table.get('th[data-column-key="result"] button').trigger('keydown', { key: 'Home' })
    wrapper.unmount()
    const reopened = await openBatch()
    const detail = detailTable(reopened)
    expect(detail.find('th[data-column-key="prompt"]').exists()).toBe(false)
    expect(detail.findAll('th[data-column-key]')[0].attributes('data-column-key')).toBe('result')
    await detail.findAll('[data-test="toggle-details"]')[1].trigger('click')
    const expanded = detail.get('.table-detail-row')
    const trigger = expanded.get('.batch-prompt-trigger').element as HTMLElement
    trigger.focus(); await flushPromises()
    const popover = document.body.querySelector('.batch-prompt-popover')!
    expect(popover.textContent).toContain('Complete original prompt 0')
    ;(popover.querySelector('button') as HTMLButtonElement).click(); await flushPromises()
    expect(mocks.copy).toHaveBeenCalledWith('Complete original prompt 0', 'batchImage.promptPopover.copied')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await flushPromises()
    expect(document.body.querySelector('.batch-prompt-popover')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    await expanded.get('button[aria-label="batchImage.detail.previewLoad"]').trigger('click'); await flushPromises()
    expect(mocks.content).toHaveBeenCalledWith('sk-synthetic', 'child', 'img_0', 0)
    expect(Object.keys((reopened.vm as any).itemPreviewUrls)).toHaveLength(1)
    await expanded.get('button[aria-label="batchImage.detail.previewZoom"]').trigger('click'); await flushPromises()
    expect((reopened.vm as any).previewImageItem.batch_id).toBe('child')
    expect(reopened.find('img[src="blob:synthetic"][alt="img_0"]').exists()).toBe(true)
    expect(mocks.error).not.toHaveBeenCalled()
  })

  it('retains row DOM and focus across background refresh, failure and retry', async () => {
    const wrapper = await openBatch()
    const table = detailTable(wrapper)
    const trigger = table.findAll('.batch-prompt-trigger')[1].element as HTMLElement
    trigger.focus(); await flushPromises()
    let completeParent!: (result: { data: ReturnType<typeof item>[] }) => void
    mocks.items.mockImplementationOnce(() => new Promise(resolve => { completeParent = resolve }))
    const pending = (wrapper.vm as any).loadItems()
    await flushPromises()
    expect(wrapper.get('[data-testid="batch-image-items"]').attributes('aria-busy')).toBe('true')
    expect(table.findAll('.batch-prompt-trigger')[1].element).toBe(trigger)
    expect(document.activeElement).toBe(trigger)
    completeParent({ data: [item()] }); await pending; await flushPromises()
    expect(table.findAll('.batch-prompt-trigger')[1].element).toBe(trigger)
    mocks.items.mockRejectedValueOnce(new Error('Temporary item error'))
    await (wrapper.vm as any).loadItems(); await flushPromises()
    expect(table.props('data')).toHaveLength(2)
    expect(table.findAll('.batch-prompt-trigger')[1].element).toBe(trigger)
    expect(mocks.error).toHaveBeenCalledOnce()
    await (wrapper.vm as any).loadItems(); await flushPromises()
    expect(table.props('data')).toHaveLength(2)
    expect(wrapper.get('[data-testid="batch-image-items"]').attributes('aria-busy')).toBe('false')
    expect(document.activeElement).toBe(trigger)
  })

  it('keeps the five detail fields and original preview action in mobile record flow', async () => {
    viewport(false)
    const wrapper = await openBatch()
    const table = detailTable(wrapper)
    expect(table.find('table').exists()).toBe(false)
    const card = table.findAll('.table-card')[1]
    expect(card.text()).toContain('img_0')
    expect(card.text()).toContain('Complete original prompt 0')
    expect(card.text()).toContain('common.status')
    expect(card.text()).toContain('batchImage.detail.result')
    await card.get('button[aria-label="batchImage.detail.previewLoad"]').trigger('click'); await flushPromises()
    expect(mocks.content).toHaveBeenCalledWith('sk-synthetic', 'child', 'img_0', 0)
    await card.get('button[aria-label="batchImage.detail.previewZoom"]').trigger('click'); await flushPromises()
    expect((wrapper.vm as any).previewImageItem.batch_id).toBe('child')
  })
})
