import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount, mount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)


import OpsAlertRulesCard from '../OpsAlertRulesCard.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { adminAPI } from '@/api'
import { ref } from 'vue'
const { createAlertRule, listAlertRules, deleteAlertRule } = vi.hoisted(() => ({ createAlertRule: vi.fn(), listAlertRules: vi.fn(), deleteAlertRule: vi.fn() }))
vi.mock('@vueuse/core', async importOriginal => ({ ...await importOriginal<typeof import('@vueuse/core')>(), useMediaQuery: () => ref(true) }))
vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { listAlertRules, createAlertRule, deleteAlertRule } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))

async function openRule() {
  const wrapper = shallowMount(OpsAlertRulesCard, {
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } },
  })
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text().includes('admin.ops.alertRules.create'))!.trigger('click')
  await wrapper.find('input[type="text"]').setValue('Error rate')
  return wrapper
}
describe('alert rule integer durations', () => {
  beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); vi.spyOn(window, 'scrollTo').mockImplementation(() => {}); vi.mocked(adminAPI.groups.getAll).mockResolvedValue([]); listAlertRules.mockResolvedValue([]); deleteAlertRule.mockResolvedValue(undefined) })
  it.each(['1.5', '0.5'])('rejects fractional duration %s before submitting', async value => {
    const wrapper = await openRule()
    const input = wrapper.find(value === '1.5' ? 'input[min="1"][max="1440"]' : 'input[min="0"][max="1440"]')
    await input.setValue(value)
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    expect(wrapper.text()).toContain(value === '1.5' ? 'admin.ops.alertRules.validation.sustainedRange' : 'admin.ops.alertRules.validation.cooldownRange')
    await save.trigger('click')
    expect(createAlertRule).not.toHaveBeenCalled()
  })
  it('allows whole-minute boundaries and a fractional metric threshold', async () => {
    const wrapper = await openRule()
    await wrapper.get('input[min="1"][max="1440"]').setValue('1440')
    await wrapper.get('input[min="0"][max="1440"]').setValue('0')
    await wrapper.get('input[type="number"]:not([min])').setValue('1.25')
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click')
    await flushPromises()
    expect(createAlertRule).toHaveBeenCalledWith(expect.objectContaining({ sustained_minutes: 1440, cooldown_minutes: 0, threshold: 1.25 }))
  })
})


const rule = (id: number) => ({ id, name: `Rule ${id}`, description: `Description ${id}`, enabled: true, metric_type: 'error_rate', operator: '>', threshold: 1.25, window_minutes: 1, sustained_minutes: 2, severity: 'P1', cooldown_minutes: 10, notify_email: true, updated_at: '2026-10-24T00:00:00Z' })
const mountRecords = () => mount(OpsAlertRulesCard, { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } } })

describe('alert rule shared record contracts', () => {
  beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); vi.spyOn(window, 'scrollTo').mockImplementation(() => {}); vi.mocked(adminAPI.groups.getAll).mockResolvedValue([]); listAlertRules.mockResolvedValue([]); deleteAlertRule.mockResolvedValue(undefined) })
  afterEach(() => { vi.restoreAllMocks() })
  it('restores demoted fields and dispatches original edit/delete identities from the final main column', async () => {
    listAlertRules.mockResolvedValue([rule(77)])
    const wrapper = mountRecords()
    await flushPromises()
    const table = wrapper.findComponent(DataTable)
    table.findComponent(ColumnSettings).vm.$emit('toggle', 'metric_type')
    table.findComponent(ColumnSettings).vm.$emit('toggle', 'actions')
    await flushPromises()
    expect(table.find('th[data-column-key="metric_type"]').exists()).toBe(false)
    wrapper.unmount()
    const restored = mountRecords()
    await flushPromises()
    const records = restored.findComponent(DataTable)
    expect(records.findAll('th[data-column-key]').at(-1)?.attributes('data-column-key')).toBe('actions')
    await records.get('[data-test="toggle-details"]').trigger('click')
    const detail = records.get('.table-detail-row')
    expect(detail.text()).toContain('error_rate')
    expect(detail.text()).toContain('1.25')
    expect(detail.findAll('button').some(button => button.text() === 'common.edit')).toBe(false)
    await records.get('tr[data-index="0"] td[data-column-key="actions"]').findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    expect((restored.find('input[type="text"]').element as HTMLInputElement).value).toBe('Rule 77')
    await records.get('tr[data-index="0"] td[data-column-key="actions"]').findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
    restored.findComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(deleteAlertRule).toHaveBeenCalledWith(77)
    restored.unmount()
  })

  it('uses the same windowed mobile records for large rule snapshots', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: false, media: query, onchange: null, addEventListener: vi.fn(), removeEventListener: vi.fn(), addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn() }))
    listAlertRules.mockResolvedValue(Array.from({ length: 140 }, (_, index) => rule(index + 1)))
    const wrapper = mountRecords()
    await flushPromises()
    const table = wrapper.findComponent(DataTable)
    expect(table.props('data').map(item => item.id)).toEqual(Array.from({ length: 140 }, (_, index) => 140 - index))
    expect(table.findAll('.table-record').length).toBeGreaterThan(0)
    expect(table.findAll('.table-record').length).toBeLessThan(40)
    expect(table.findAll('.table-record')[0].findAll('[data-field]').map(field => field.attributes('data-field'))).toEqual(['name', 'metric_type', 'severity', 'enabled', 'actions'])
    expect(table.findAll('.table-record')[0].text()).toContain('Description 140')
    expect(table.props('columns').every(column => column.primary && !column.sortable)).toBe(true)
    wrapper.unmount()
  })
})
