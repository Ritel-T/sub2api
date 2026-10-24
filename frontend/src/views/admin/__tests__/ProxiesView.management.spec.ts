import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
const { route, replace, list, getById, getProxyAccounts } = vi.hoisted(() => ({ route: { hash: '', query: {} as Record<string, string> }, replace: vi.fn(), list: vi.fn(), getById: vi.fn(), getProxyAccounts: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { list, getById, getProxyAccounts, getAllWithCount: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: VueWrapper
const mountView = () => shallowMount(ProxiesView, { global: { stubs: {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' }
} } })
beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); route.hash = ''; route.query = {}; list.mockResolvedValue({ items: [], total: 0, pages: 1 }) })
afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })
describe('IP management navigation', () => {
  it('opens the exact proxy and real bound-account list without inventing unsupported list filters', async () => {
    route.query.proxy_id = '84'
    getById.mockResolvedValue({ id: 84, name: 'Dedicated exit' })
    getProxyAccounts.mockResolvedValue([{ id: 42, name: 'Upstream' }])
    wrapper = mountView(); await flushPromises()
    expect(getById).toHaveBeenCalledWith(84)
    expect(getProxyAccounts).toHaveBeenCalledWith(84)
    expect((wrapper.vm as any).accountsProxy.id).toBe(84)
    expect((wrapper.vm as any).showAccountsModal).toBe(true)
    expect(list.mock.calls[0][2]).not.toHaveProperty('proxy_id')
    expect(list.mock.calls[0][2]).not.toHaveProperty('account_id')
  })

  it('opens harvest exit settings in IP management', async () => {
    route.hash = '#harvest'; wrapper = mountView(); await flushPromises()
    expect(wrapper.findComponent({ name: 'HarvestProxySettings' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'MihomoSettings' }).exists()).toBe(false)
  })
  it('refreshes static proxies after the node panel creates them', async () => {
    route.hash = '#nodes'; wrapper = mountView(); await flushPromises()
    const before = list.mock.calls.length
    wrapper.findComponent({ name: 'MihomoSettings' }).vm.$emit('static-proxies-added')
    await flushPromises()
    expect(list.mock.calls.length).toBe(before + 1)
  })
  it('preserves the existing proxy list and exposes the four management sections', async () => {
    wrapper = mountView(); await flushPromises()
    expect(wrapper.find('input[type="text"]').exists()).toBe(true)
    for (const [key, label] of [['subscriptions', 'subscriptions'], ['dynamic', 'dynamicProxies'], ['nodes', 'nodes'], ['kernel', 'kernelRules']]) {
      await wrapper.findAll('button').find(b => b.text() === 'admin.proxies.' + label)!.trigger('click')
      expect(wrapper.findComponent({ name: 'MihomoSettings' }).props('section')).toBe(key)
      expect(wrapper.find('select-stub').exists()).toBe(false)
      expect(replace).toHaveBeenLastCalledWith({ hash: '#' + key })
    }
  })
  it.each(['#subscriptions', '#mihomo'])('opens subscription management from %s', async hash => {
    route.hash = hash; wrapper = mountView(); await flushPromises()
    expect(wrapper.findComponent({ name: 'MihomoSettings' }).props('section')).toBe('subscriptions')
  })
})

describe('bound proxy account records', () => {
  it('windows the real account payload and keeps notes reachable after column changes', async () => {
    route.query.proxy_id = '84'
    getById.mockResolvedValue({ id: 84, name: 'Dedicated exit' })
    vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
    const accounts = Array.from({ length: 140 }, (_, i) => ({ id: i + 1, name: `Bound ${i}`, platform: 'openai', type: 'apikey', notes: `Original notes ${i}` }))
    getProxyAccounts.mockResolvedValue(accounts)
    wrapper = shallowMount(ProxiesView, { global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
      DataTable: false, BaseDialog: false, Teleport: true
    } } })
    await flushPromises()
    const table = wrapper.findAllComponents(DataTable).find(view => view.props('columnOrderKey') === 'admin-proxy-bound-accounts')!
    expect(table.props('data')).toHaveLength(140)
    expect(table.props('rowKey')).toBe('id')
    expect(table.findAll('[data-index]').length).toBeGreaterThan(0)
    expect(table.findAll('[data-index]').length).toBeLessThan(40)
    table.findComponent(ColumnSettings).vm.$emit('toggle', 'notes')
    await flushPromises()
    await table.get('[data-test="toggle-details"]').trigger('click')
    expect(table.text()).toContain('Original notes 0')
    expect(table.findComponent(ColumnSettings).props('hiddenColumns')).toContain('notes')
    expect(getProxyAccounts).toHaveBeenCalledWith(84)
    expect(list.mock.calls[0][2]).not.toHaveProperty('account_id')
  })
})
