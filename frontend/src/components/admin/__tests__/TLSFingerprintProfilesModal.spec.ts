import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import TLS from '../TLSFingerprintProfilesModal.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), delete: vi.fn(), showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { tlsFingerprintProfiles: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
function viewport(desktop: boolean) { vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({ matches: desktop, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn() }))) }
beforeEach(() => { vi.resetAllMocks(); localStorage.clear(); viewport(true) })
function profile(id = 7) { return { id, name: `Profile ${id}`, description: `Description ${id}`, enable_grease: true, alpn_protocols: ['h2', 'http/1.1'], cipher_suites: [4865], curves: [29], point_formats: [0], signature_algorithms: [2052], supported_versions: [772], key_share_groups: [29], psk_modes: [1], extensions: [0] } }
async function openProfiles(rows = [profile()], host: HTMLElement = document.body) {
  mocks.list.mockResolvedValue(rows)
  const wrapper = mount(TLS, { attachTo: host, props: { show: false }, global: { stubs: {
    BaseDialog: { props: ['show'], template: '<section v-if="show"><slot/><slot name="footer"/></section>' }, ConfirmDialog: true, Icon: true
  } } })
  await wrapper.setProps({ show: true }); await flushPromises(); return wrapper
}

describe('TLS profile shared records', () => {
  it('restores detail preferences and retains edit and delete targets in the final main column', async () => {
    const wrapper = await openProfiles()
    expect(wrapper.getComponent(DataTable).props('columns')).toHaveLength(5)
    wrapper.getComponent(ColumnSettings).vm.$emit('toggle', 'description')
    wrapper.getComponent(ColumnSettings).vm.$emit('toggle', 'actions')
    await flushPromises()
    await wrapper.get('th[data-column-key="enable_grease"] button').trigger('keydown', { key: 'Home' })
    wrapper.unmount()
    const reopened = await openProfiles()
    expect(reopened.find('th[data-column-key="description"]').exists()).toBe(false)
    expect(reopened.findAll('th[data-column-key]')[0].attributes('data-column-key')).toBe('enable_grease')
    expect(reopened.findAll('th[data-column-key]').at(-1)?.attributes('data-column-key')).toBe('actions')
    await reopened.get('[data-test="toggle-details"]').trigger('click')
    expect(reopened.get('.table-detail-row [data-field="description"]').text()).toContain('Description 7')
    expect(reopened.find('.table-detail-row button[aria-label="common.edit"]').exists()).toBe(false)
    expect(mocks.update).not.toHaveBeenCalled()
    await reopened.get('tr[data-row-id="7"] td[data-column-key="actions"] button[aria-label="common.edit"]').trigger('click')
    await reopened.get('form input[required]').setValue('Updated profile')
    mocks.update.mockResolvedValue(profile())
    await reopened.get('form').trigger('submit'); await flushPromises()
    expect(mocks.update.mock.calls[0][0]).toBe(7)
    expect(mocks.update.mock.calls[0][1]).toMatchObject({ name: 'Updated profile', cipher_suites: [4865], enable_grease: true })
    await reopened.get('tr[data-row-id="7"] td[data-column-key="actions"] button[aria-label="common.delete"]').trigger('click')
    expect(reopened.getComponent(ConfirmDialog).props('show')).toBe(true)
    expect(mocks.delete).not.toHaveBeenCalled()
    mocks.delete.mockResolvedValue(undefined)
    reopened.getComponent(ConfirmDialog).vm.$emit('confirm'); await flushPromises()
    expect(mocks.delete).toHaveBeenCalledWith(7)
  })

  it('keeps all fields and action wiring in the mobile record layout', async () => {
    viewport(false)
    const wrapper = await openProfiles()
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.get('.table-card').text()).toContain('Description 7')
    expect(wrapper.get('.table-card').text()).toContain('http/1.1')
    await wrapper.get('.table-card button[aria-label="common.edit"]').trigger('click')
    expect((wrapper.get('form input[required]').element as HTMLInputElement).value).toBe('Profile 7')
  })

  it('windows a long profile snapshot without dropping its loaded data', async () => {
    const host = document.createElement('div'); host.style.overflowY = 'auto'; host.style.maxHeight = '200px'; document.body.append(host)
    Object.defineProperty(host, 'clientHeight', { configurable: true, value: 200 })
    host.getBoundingClientRect = () => ({ top: 0, bottom: 200, left: 0, right: 800, width: 800, height: 200, x: 0, y: 0, toJSON() {} })
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
    const rows = Array.from({ length: 140 }, (_, index) => profile(index + 1))
    const wrapper = await openProfiles(rows, host)
    expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(140)
    const body = wrapper.get('tbody').element as HTMLElement
    body.getBoundingClientRect = () => ({ top: -host.scrollTop, bottom: 140 * 80 - host.scrollTop, left: 0, right: 800, width: 800, height: 140 * 80, x: 0, y: -host.scrollTop, toJSON() {} })
    await vi.waitFor(() => { expect(wrapper.findAll('tr[data-index]').length).toBeGreaterThan(0); expect(wrapper.findAll('tr[data-index]').length).toBeLessThan(40) })
    host.scrollTop = 80 * 80; host.dispatchEvent(new Event('scroll'))
    await vi.waitFor(() => expect(wrapper.find('tr[data-index="80"]').exists()).toBe(true))
    expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(140)
    wrapper.unmount(); host.remove()
  })
})
