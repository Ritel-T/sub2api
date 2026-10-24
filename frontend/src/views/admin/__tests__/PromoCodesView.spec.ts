import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import PromoCodesView from '../PromoCodesView.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'

enableAutoUnmount(afterEach)
const api = vi.hoisted(() => ({ list: vi.fn(), getUsages: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { promo: { list: api.list, getUsages: api.getUsages } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: api.showError, showSuccess: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string, params?: { id?: number }) => params?.id ? `${key} ${params.id}` : key }) }))

const code = { id: 7, code: 'SYNTHETIC', bonus_amount: 2, used_count: 0, max_uses: 0, status: 'active', created_at: '2026-10-24T00:00:00Z' }
const usage = (id: number) => ({ id, user_id: id + 100, bonus_amount: 2.5, used_at: '2026-10-24T00:00:00Z', ...(id === 1 ? { user: { email: 'synthetic@example.test' } } : {}) })
async function mountUsages() {
  const wrapper = mount(PromoCodesView, { attachTo: document.body, global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' },
    TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
    TableToolbarTarget: true, DataTable: true, Select: true, Icon: true, ConfirmDialog: true,
    BaseDialog: { props: ['show'], template: '<section v-if="show" role="dialog"><slot /><slot name="footer" /></section>' }
  } } })
  await flushPromises()
  await (wrapper.vm as any).handleViewUsages(code)
  await flushPromises()
  return wrapper
}
beforeEach(() => { api.list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 1 }); api.getUsages.mockReset(); api.showError.mockReset() })
afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

describe('PromoCodes usage records', () => {
  it('preserves all original usage fields and code identity through the shared record wrapper', async () => {
    api.getUsages.mockResolvedValue({ items: [usage(1), usage(2)], total: 2, pages: 1 })
    const wrapper = await mountUsages()
    expect(api.getUsages).toHaveBeenCalledWith(7, 1, 50, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const records = wrapper.findAll('[data-testid="promo-usage-record"]')
    expect(records).toHaveLength(2)
    expect(records[0].text()).toContain('synthetic@example.test')
    expect(records[1].text()).toContain('admin.promo.userPrefix 102')
    expect(records.every(row => row.text().includes('+$2.50'))).toBe(true)
  })

  it('bounds the rendered usage records after three API pages while retaining all loaded data', async () => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    const all = Array.from({ length: 120 }, (_, index) => usage(index + 1))
    api.getUsages.mockImplementation(async (_id: number, page: number) => ({ items: all.slice((page - 1) * 50, page * 50), total: 120, pages: 3 }))
    const wrapper = await mountUsages()
    const vm = wrapper.vm as any
    await vm.usageList.loadMore(); await flushPromises()
    await vm.usageList.loadMore(); await flushPromises()
    expect(vm.usages).toHaveLength(120)
    expect(wrapper.findComponent(DocumentRows).props('items')).toHaveLength(120)
    const records = wrapper.findAll('[data-testid="promo-usage-record"]')
    expect(records.length).toBeGreaterThan(0)
    expect(records.length).toBeLessThan(100)
    expect(api.getUsages.mock.calls.map(call => call[1])).toEqual([1, 2, 3])
    expect(api.showError).not.toHaveBeenCalled()
  })
})
