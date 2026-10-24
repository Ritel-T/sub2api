import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import IncidentsDialog from '../IncidentsDialog.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import type { MonitorV3IncidentPage } from '@/api/channelMonitorV3'

const api = vi.hoisted(() => ({ getIncidents: vi.fn() }))
vi.mock('@/api/channelMonitorV3', () => ({ getIncidents: api.getIncidents }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false, locale: ref('en') }) }))
const dialog = { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }
function data(page: number, ids: number[], total = 40): MonitorV3IncidentPage {
  return { page, page_size: 20, total, since: '2026-10-20T00:00:00Z', items: ids.map(id => ({ component_id: id, component_name: `Component ${id}`, started_at: '2026-10-20T01:00:00Z', ended_at: null, down_slots: 1 })) }
}
let wrapper: ReturnType<typeof mount>
beforeEach(() => vi.clearAllMocks())
afterEach(() => wrapper?.unmount())
function render() { return mount(IncidentsDialog, { props: { show: true, admin: false }, global: { stubs: { BaseDialog: dialog } } }) }

describe('incident continuous loading', () => {
  it('keeps earlier incidents on append failure and retries the same page', async () => {
    api.getIncidents.mockResolvedValueOnce(data(1, [1])).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(data(2, [2], 2))
    wrapper = render(); await flushPromises()
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises()
    expect(wrapper.text()).toContain('Component 1')
    expect(wrapper.getComponent(InfiniteListSentinel).props('error')).toBeTruthy()
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('retry'); await flushPromises()
    expect(wrapper.text()).toContain('Component 1')
    expect(wrapper.text()).toContain('Component 2')
    expect(api.getIncidents.mock.calls.map(x => x[0])).toEqual([1, 2, 2])
  })
  it('cancels and discards an old role response', async () => {
    let resolve!: (value: MonitorV3IncidentPage) => void
    api.getIncidents.mockReturnValueOnce(new Promise<MonitorV3IncidentPage>(r => { resolve = r })).mockResolvedValueOnce(data(1, [9], 1))
    wrapper = render(); await flushPromises()
    const oldSignal = api.getIncidents.mock.calls[0][3] as AbortSignal
    await wrapper.setProps({ admin: true }); await flushPromises()
    resolve(data(1, [1], 1)); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.text()).toContain('Component 9')
    expect(wrapper.text()).not.toContain('Component 1')
  })
  it('cancels on close and starts a clean list on reopen', async () => {
    let resolve!: (value: MonitorV3IncidentPage) => void
    api.getIncidents.mockReturnValueOnce(new Promise<MonitorV3IncidentPage>(r => { resolve = r })).mockResolvedValueOnce(data(1, [2], 1))
    wrapper = render(); await flushPromises()
    await wrapper.setProps({ show: false }); resolve(data(1, [1])); await flushPromises()
    expect((api.getIncidents.mock.calls[0][3] as AbortSignal).aborted).toBe(true)
    await wrapper.setProps({ show: true }); await flushPromises()
    expect(wrapper.text()).toContain('Component 2')
    expect(wrapper.text()).not.toContain('Component 1')
  })
})
