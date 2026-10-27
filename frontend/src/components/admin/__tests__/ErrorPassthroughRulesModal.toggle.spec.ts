import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ErrorPassthroughRulesModal from '../ErrorPassthroughRulesModal.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), toggleEnabled: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { errorPassthrough: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())
beforeEach(() => { vi.resetAllMocks(); localStorage.clear(); Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn().mockImplementation((query: string) => ({ matches: true, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn() })) }) })

function rule(enabled: boolean) {
  return { id: 7, name: 'Rule', enabled, priority: 1, error_codes: [429], keywords: [],
    platforms: [], match_mode: 'any', passthrough_code: true, passthrough_body: true, skip_monitoring: false }
}

async function openRules(enabled = true, rows = [rule(enabled)], host: HTMLElement = document.body) {
  mocks.list.mockResolvedValue(rows)
  const wrapper = mount(ErrorPassthroughRulesModal, {
    attachTo: host, props: { show: false },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      ConfirmDialog: true, Icon: true
    } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('error passthrough enabled state', () => {
  it.each([true, false])('keeps the server state after two pending clicks (enabled=%s)', async (enabled) => {
    const finishes: Array<(value: unknown) => void> = []
    mocks.toggleEnabled.mockImplementation(() => new Promise(resolve => { finishes.push(resolve) }))
    const wrapper = await openRules(enabled)
    const toggle = wrapper.get('button[role="switch"]')
    await toggle.trigger('click')
    await toggle.trigger('click')
    expect(mocks.toggleEnabled.mock.calls).toEqual([[7, !enabled], [7, !enabled]])
    for (const finish of finishes) {
      finish(rule(!enabled))
      await flushPromises()
      expect(toggle.classes().includes('ritel-toggle-on')).toBe(!enabled)
    }
  })

  it('allows sequential toggles in both directions', async () => {
    mocks.toggleEnabled.mockImplementation((_id: number, enabled: boolean) => Promise.resolve(rule(enabled)))
    const wrapper = await openRules()
    const toggle = wrapper.get('button[role="switch"]')
    await toggle.trigger('click')
    await flushPromises()
    expect(toggle.classes()).not.toContain('ritel-toggle-on')
    await toggle.trigger('click')
    await flushPromises()
    expect(toggle.classes()).toContain('ritel-toggle-on')
    expect(mocks.toggleEnabled.mock.calls).toEqual([[7, false], [7, true]])
  })

  it('keeps the original state when saving fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.toggleEnabled.mockRejectedValue(new Error('offline'))
    const wrapper = await openRules()
    const toggle = wrapper.get('button[role="switch"]')
    await toggle.trigger('click')
    await flushPromises()
    expect(toggle.classes()).toContain('ritel-toggle-on')
    expect(mocks.showError).toHaveBeenCalledWith('admin.errorPassthrough.failedToToggle')
  })
})


describe('error rule shared records', () => {
  it('persists hidden conditions and keeps deletion in the final main column', async () => {
    const wrapper = await openRules()
    expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(1)
    expect(wrapper.getComponent(DataTable).props('columns')).toHaveLength(7)
    wrapper.getComponent(ColumnSettings).vm.$emit('toggle', 'conditions')
    wrapper.getComponent(ColumnSettings).vm.$emit('toggle', 'actions')
    await flushPromises()
    await wrapper.get('th[data-column-key="enabled"] button').trigger('keydown', { key: 'Home' })
    wrapper.unmount()
    const reopened = await openRules()
    expect(reopened.find('th[data-column-key="conditions"]').exists()).toBe(false)
    expect(reopened.findAll('th[data-column-key]')[0].attributes('data-column-key')).toBe('enabled')
    expect(reopened.findAll('th[data-column-key]').at(-1)?.attributes('data-column-key')).toBe('actions')
    await reopened.get('[data-test="toggle-details"]').trigger('click')
    expect(reopened.get('.table-detail-row [data-field="conditions"]').text()).toContain('429')
    expect(reopened.find('.table-detail-row button[aria-label="common.delete"]').exists()).toBe(false)
    expect(mocks.delete).not.toHaveBeenCalled()
    await reopened.get('tr[data-row-id="7"] td[data-column-key="actions"] button[aria-label="common.delete"]').trigger('click')
    expect(reopened.getComponent(ConfirmDialog).props('show')).toBe(true)
    expect(mocks.delete).not.toHaveBeenCalled()
    mocks.delete.mockResolvedValue(undefined)
    reopened.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(mocks.delete).toHaveBeenCalledWith(7)
  })

  it('keeps the actual focused row while a background reload is pending', async () => {
    const wrapper = await openRules()
    const toggle = wrapper.get('button[role="switch"]').element as HTMLButtonElement
    toggle.focus()
    let finish!: (value: unknown) => void
    mocks.list.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const reload = (wrapper.vm as any).loadRules()
    await flushPromises()
    expect(wrapper.get('button[role="switch"]').element).toBe(toggle)
    expect(document.activeElement).toBe(toggle)
    finish([rule(true)])
    await reload; await flushPromises()
    expect(wrapper.get('button[role="switch"]').element).toBe(toggle)
    expect(document.activeElement).toBe(toggle)
  })
})


it('keeps the full long rule snapshot while rendering only its actual viewport', async () => {
  const host = document.createElement('div'); host.style.overflowY = 'auto'; host.style.maxHeight = '200px'; document.body.append(host)
  Object.defineProperty(host, 'clientHeight', { configurable: true, value: 200 })
  host.getBoundingClientRect = () => ({ top: 0, bottom: 200, left: 0, right: 800, width: 800, height: 200, x: 0, y: 0, toJSON() {} })
  host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
  const rows = Array.from({ length: 140 }, (_, index) => ({ ...rule(true), id: index + 1, name: `Rule ${index + 1}` }))
  const wrapper = await openRules(true, rows, host)
  const body = wrapper.get('tbody').element as HTMLElement
  body.getBoundingClientRect = () => ({ top: -host.scrollTop, bottom: 140 * 80 - host.scrollTop, left: 0, right: 800, width: 800, height: 140 * 80, x: 0, y: -host.scrollTop, toJSON() {} })
  expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(140)
  await vi.waitFor(() => { expect(wrapper.findAll('tr[data-index]').length).toBeGreaterThan(0); expect(wrapper.findAll('tr[data-index]').length).toBeLessThan(40) })
  host.scrollTop = 80 * 80; host.dispatchEvent(new Event('scroll'))
  await vi.waitFor(() => expect(wrapper.find('tr[data-index="80"]').exists()).toBe(true))
  expect(wrapper.get('tr[data-index="80"]').text()).toContain('Rule 81')
  expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(140)
  wrapper.unmount(); host.remove()
})
