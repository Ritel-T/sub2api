import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AstraGatewayHistory from '../AstraGatewayHistory.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/admin/astraGateway', () => ({ getAstraGatewayHistory: mocks.get }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true }) }))
beforeEach(() => { localStorage.clear(); vi.useFakeTimers(); vi.clearAllMocks(); mocks.get.mockResolvedValue({ items: [{ gateway: 'chat.gateway.unified-196.api.openai.com', source_account_id: 299, target_account_id: 300, passes: 1, failures: 2, last_answer: '29', last_reason: 'target_candy_failed', last_pass: '2026-09-23T20:00:00Z' }], total: 21, unique_gateways: 3 }) })
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
describe('persistent gateway history', () => {
  it('defaults to target pass history, searches host and appends batches', async () => {
    const w = mount(AstraGatewayHistory); await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('', true, 1, { signal: expect.any(AbortSignal) })
    expect(w.text()).toContain('unified-196'); expect(w.text()).toContain('#300'); expect(w.text()).toContain('29')
    await w.get('input[type="checkbox"]').setValue(false); await flushPromises()
    await w.get('input[maxlength]').setValue('unified-196'); await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith('unified-196', false, 1, { signal: expect.any(AbortSignal) })
    w.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more'); await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith('unified-196', false, 2, { signal: expect.any(AbortSignal) })
    expect(w.findAll('tbody tr')).toHaveLength(1) // overlapping identity is updated, never duplicated
    w.unmount(); const count = mocks.get.mock.calls.length; await vi.advanceTimersByTimeAsync(15000); expect(mocks.get).toHaveBeenCalledTimes(count)
  })
  it('reports load failures instead of claiming an empty history', async () => {
    mocks.get.mockRejectedValue(new Error('offline'))
    const w = mount(AstraGatewayHistory); await flushPromises(); expect(w.find('[role="alert"]').exists()).toBe(true); w.unmount()
  })
})

describe('gateway history table integration', () => {
  it('retains the composite identity and stores reordered detail fields across remount', async () => {
    let wrapper = mount(AstraGatewayHistory); await flushPromises()
    const table = wrapper.getComponent(DataTable)
    expect(table.props('columns').every(column => column.primary === true)).toBe(true)
    expect((table.props('rowKey') as (row: unknown) => string)(table.props('data')[0])).toBe('chat.gateway.unified-196.api.openai.com/299/300')
    const preferences = table.getComponent(ColumnSettings)
    preferences.vm.$emit('toggle', 'last_result')
    preferences.vm.$emit('reorder', ['last_pass', 'gateway', 'source_account_id', 'target_account_id', 'results', 'last_failure', 'last_result'])
    await flushPromises()
    expect(wrapper.find('th[data-column-key="last_result"]').exists()).toBe(false)
    expect(wrapper.findAll('th[data-column-key]')[0].attributes('data-column-key')).toBe('last_pass')
    expect(JSON.parse(localStorage.getItem('admin-astra-gateway-history')!).primary.last_result).toBe(false)
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.get('.table-detail-row').text()).toContain('29')
    wrapper.unmount()
    wrapper = mount(AstraGatewayHistory); await flushPromises()
    expect(wrapper.find('th[data-column-key="last_result"]').exists()).toBe(false)
    expect(wrapper.findAll('th[data-column-key]')[0].attributes('data-column-key')).toBe('last_pass')
    wrapper.unmount()
  })

  it('keeps all seven loaded pages while rendering a bounded actual row window', async () => {
    vi.stubGlobal('innerHeight', 400)
    vi.stubGlobal('scrollY', 0)
    vi.stubGlobal('scrollTo', vi.fn())
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const index = this.dataset.index === undefined ? 0 : Number(this.dataset.index)
      const height = this.dataset.index === undefined ? 400 : 80
      const top = index * 80 - window.scrollY
      return { top, bottom: top + height, left: 0, right: 800, width: 800, height, x: 0, y: top, toJSON() {} }
    })
    mocks.get.mockImplementation(async (_host: string, _passed: boolean, page: number) => ({
      items: Array.from({ length: 20 }, (_, index) => ({ gateway: `gateway-${(page - 1) * 20 + index}`, source_account_id: 1, target_account_id: 2, passes: 1, failures: 0, last_reason: 'passed' })),
      total: 140, pages: 7, unique_gateways: 140
    }))
    const wrapper = mount(AstraGatewayHistory, { attachTo: document.body })
    try {
      await flushPromises()
      for (let page = 2; page <= 7; page++) {
        wrapper.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more')
        await flushPromises()
      }
      await vi.advanceTimersByTimeAsync(64); await flushPromises()
      expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(140)
      const rendered = wrapper.findAll('[data-testid="astra-history-row"]')
      expect(rendered.length).toBeGreaterThan(0)
      expect(rendered.length).toBeLessThan(40)
      vi.stubGlobal('scrollY', 6400)
      window.dispatchEvent(new Event('scroll'))
      await vi.advanceTimersByTimeAsync(64); await flushPromises()
      expect(wrapper.findAll('[data-testid="astra-history-row"]').some(row => Number(row.attributes('data-index')) >= 72)).toBe(true)
      expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(140)
    } finally { wrapper.unmount() }
  })
})
