import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref, nextTick } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import KeyWorkspaceTable from '../KeyWorkspaceTable.vue'
import type { ApiKey } from '@/types'
import type { Column } from '@/components/common/types'

enableAutoUnmount(afterEach)
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))

const columns: Column[] = [
  { key: 'id', label: 'ID', sortable: true },
  { key: 'name', label: 'Name', sortable: true },
  { key: 'key', label: 'API key' },
  { key: 'usage', label: 'Usage' },
  { key: 'status', label: 'Status', sortable: true },
  { key: 'actions', label: 'Actions' },
  { key: 'last_used_ip', label: 'Last IP' },
]
function key(id: number, name: string): ApiKey {
  return {
    id, user_id: 1, name, key: `sk-fixture-${id}`, group_id: null, status: 'active',
    ip_whitelist: [], ip_blacklist: [], quota: 0, quota_used: 0, expires_at: null,
    created_at: '2026-10-20T00:00:00Z', updated_at: '2026-10-20T00:00:00Z',
    last_used_at: null, last_used_ip: '192.0.2.1', current_concurrency: 0, concurrency_limit: 0,
    rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0, usage_5h: 0, usage_1d: 0, usage_7d: 0,
    window_5h_start: null, window_1d_start: null, window_7d_start: null,
    reset_5h_at: null, reset_1d_at: null, reset_7d_at: null,
  }
}
function matchViewport(desktop: boolean) {
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn().mockImplementation((query: string) => ({
    matches: desktop, media: query, onchange: null, addEventListener: vi.fn(), removeEventListener: vi.fn(),
    addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn(),
  })) })
}
function mountTable(overrides: Record<string, unknown> = {}) {
  return mount(KeyWorkspaceTable, {
    props: { columns, data: [key(11, 'Zulu'), key(29, 'Alpha')], loading: false,
      selectedKeys: [], busyKeys: new Set<number>(), rename: vi.fn().mockResolvedValue(true),
      sortKey: 'name', sortOrder: 'asc', ...overrides },
    slots: { 'cell-actions': '<button data-test="request-action">Requests</button>',
      'cell-last_used_ip': '<span>192.0.2.1</span>' },
  })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
beforeEach(() => { matchViewport(true); localStorage.clear() })

describe('KeyWorkspaceTable task interactions', () => {
  it('selects by stable key IDs while keeping selection separate from expansion', async () => {
    const wrapper = mountTable()
    await wrapper.get('[data-test="select-row"][data-key-id="11"]').trigger('click')
    expect(wrapper.emitted('update:selectedKeys')?.at(-1)).toEqual([[11]])
    await wrapper.setProps({ selectedKeys: [11] })
    await wrapper.get('[data-test="select-row"][data-key-id="29"]').trigger('click')
    expect(wrapper.emitted('update:selectedKeys')?.at(-1)).toEqual([[11, 29]])
    const selectionCalls = wrapper.emitted('update:selectedKeys')?.length
    await wrapper.get('[data-test="toggle-details"][data-key-id="11"]').trigger('click')
    expect(wrapper.emitted('update:selectedKeys')).toHaveLength(selectionCalls!)
    expect(wrapper.get('[data-test="toggle-details"][data-key-id="11"]').attributes('aria-expanded')).toBe('true')
  })

  it('does not expand a record when its action, selection, or rename controls are activated', async () => {
    const wrapper = mountTable()
    const toggle = wrapper.get('[data-test="toggle-details"][data-key-id="11"]')
    await wrapper.get('tr.key-main-row[data-key-id="11"] [data-test="request-action"]').trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await wrapper.get('[data-test="select-row"][data-key-id="11"]').trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await wrapper.get('[data-test="rename-key"][data-key-id="11"]').trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await wrapper.get('[data-test="cancel-name"]').trigger('click')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find(`[id="${toggle.attributes('aria-controls')}"]`).exists()).toBe(true)
  })

  it('keeps the name editor during the real save and retains its draft on failure', async () => {
    const pending = deferred<boolean>()
    const rename = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValueOnce(true)
    const wrapper = mountTable({ rename })
    await wrapper.get('[data-test="rename-key"][data-key-id="11"]').trigger('click')
    await wrapper.get('[data-test="rename-input"]').setValue('Production')
    await wrapper.get('[data-test="save-name"]').trigger('click')
    expect(rename).toHaveBeenCalledOnce()
    expect(rename.mock.calls[0][0]).toMatchObject({ id: 11, name: 'Zulu' })
    expect(rename.mock.calls[0][1]).toBe('Production')
    expect(wrapper.get('[data-test="rename-input"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="rename-input"]').trigger('blur')
    expect(rename).toHaveBeenCalledOnce()
    pending.resolve(false)
    await flushPromises()
    expect((wrapper.get('[data-test="rename-input"]').element as HTMLInputElement).value).toBe('Production')
    expect(wrapper.props('data')[0].name).toBe('Zulu')
    await wrapper.get('[data-test="save-name"]').trigger('click')
    await flushPromises()
    expect(rename).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-test="rename-input"]').exists()).toBe(false)
    await wrapper.setProps({ data: [key(11, 'Production'), key(29, 'Alpha')] })
    expect(wrapper.get('[data-test="rename-key"][data-key-id="11"]').text()).toContain('Production')
  })

  it('cancels an unsaved name with Escape without submitting a request', async () => {
    const rename = vi.fn()
    const wrapper = mountTable({ rename })
    await wrapper.get('[data-test="rename-key"][data-key-id="11"]').trigger('click')
    await wrapper.get('[data-test="rename-input"]').setValue('Discard me')
    await wrapper.get('[data-test="rename-input"]').trigger('keydown', { key: 'Escape' })
    expect(rename).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="rename-input"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="rename-key"][data-key-id="11"]').text()).toContain('Zulu')
  })

  it('requests server sorting without changing the provided page or selected IDs', async () => {
    const wrapper = mountTable({ selectedKeys: [29] })
    await wrapper.get('[data-test="sort-column"][data-column-key="name"]').trigger('click')
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['name', 'desc'])
    expect(wrapper.findAll('tr.key-main-row').map(row => row.attributes('data-key-id'))).toEqual(['11', '29'])
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
  })

  it('moves columns with the keyboard including ID while leaving row sorting untouched', async () => {
    const wrapper = mountTable()
    expect(wrapper.find('th[data-column-key="id"] .table-column-drag-handle').exists()).toBe(true)
    await wrapper.get('th[data-column-key="actions"] .table-column-drag-handle').trigger('keydown', { key: 'Home' })
    expect(wrapper.emitted('columnReorder')?.at(-1)).toEqual([['actions', 'id', 'name', 'key', 'usage', 'status', 'last_used_ip']])
    expect(wrapper.emitted('sort')).toBeUndefined()
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
  })

  it('emits a pointer column reorder without changing the selected records or server sort', async () => {
    const wrapper = mountTable({ selectedKeys: [29] })
    const order = ['usage', 'name', 'key', 'status', 'actions', 'id', 'last_used_ip']
    const byKey = new Map(columns.map(column => [column.key, column]))
    wrapper.findComponent(VueDraggable).vm.$emit('update:modelValue', order.map(key => byKey.get(key)!))
    await flushPromises()
    expect(wrapper.emitted('columnReorder')?.at(-1)).toEqual([order])
    expect(wrapper.emitted('sort')).toBeUndefined()
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    expect(wrapper.findAll('tr.key-main-row').map(row => row.attributes('data-key-id'))).toEqual(['11', '29'])
  })

  it('sorts by ID separately from selecting all visible records', async () => {
    const wrapper = mountTable({ selectedKeys: [29] })
    await wrapper.get('[data-test="sort-column"][data-column-key="id"]').trigger('click')
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['id', 'asc'])
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    await wrapper.get('[data-test="select-all"]').trigger('click')
    expect(wrapper.emitted('update:selectedKeys')?.at(-1)).toEqual([[29, 11]])
    expect(wrapper.emitted('sort')).toHaveLength(1)
    expect(wrapper.findAll('tr.key-main-row').map(row => row.attributes('data-key-id'))).toEqual(['11', '29'])
    const moved = [...columns.filter(column => column.key !== 'id'), columns[0]]
    await wrapper.setProps({ columns: moved })
    expect(wrapper.findAll('th[data-column-key]').at(-2)?.attributes('data-column-key')).toBe('id')
    expect(wrapper.findAll('th[data-column-key]').at(-1)?.attributes('data-column-key')).toBe('actions')
  })

  it('keeps selection, sorting, and record actions reachable in the mobile layout', async () => {
    matchViewport(false)
    const wrapper = mountTable()
    expect(wrapper.find('[data-test="select-all-mobile"]').exists()).toBe(true)
    const mobileSort = wrapper.get('button[data-test="mobile-sort"]')
    expect(mobileSort.attributes('aria-label')).toBe('common.sort')
    await mobileSort.trigger('click')
    const idOption = Array.from(document.querySelectorAll('[role="listbox"] [role="option"]')).find(option => option.textContent?.trim() === 'ID')
    expect(idOption).toBeTruthy()
    idOption!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['id', 'asc'])
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    expect(wrapper.find('[data-test="toggle-details"][data-key-id="11"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="rename-key"][data-key-id="11"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="request-action"]').exists()).toBe(true)
    await wrapper.get('[data-test="select-all-mobile"]').trigger('click')
    expect(wrapper.emitted('update:selectedKeys')?.at(-1)).toEqual([[11, 29]])
  })

  it('promotes formerly secondary columns and retains demoted main controls in details', async () => {
    const supplied = columns.map(column => ({ ...column, primary: column.key !== 'status' }))
    const wrapper = mountTable({ columns: supplied })
    expect(wrapper.find('th[data-column-key="last_used_ip"]').exists()).toBe(true)
    expect(wrapper.find('th[data-column-key="status"]').exists()).toBe(false)
    await wrapper.get('[data-test="toggle-details"][data-key-id="11"]').trigger('click')
    expect(wrapper.get('.key-detail-row [data-column-key="status"]').text()).toContain('active')
    expect(wrapper.get('colgroup col:nth-last-child(2)').attributes('style')).not.toContain('Infinity')
  })

  it('waits for the surrounding filter target to mount before teleporting and survives row updates', async () => {
    const rows = ref([key(11, 'Zulu')])
    const Parent = defineComponent({
      components: { KeyWorkspaceTable },
      setup: () => ({ columns, rows, rename: vi.fn().mockResolvedValue(true) }),
      template: '<div><div id="key-filter-target"></div><KeyWorkspaceTable toolbar-target="#key-filter-target" :columns="columns" :data="rows" :selected-keys="[]" :rename="rename" sort-key="name" sort-order="asc" /></div>',
    })
    const host = document.createElement('div')
    document.body.append(host)
    const wrapper = mount(Parent, { attachTo: host })
    await flushPromises()
    expect(host.querySelector('#key-filter-target .workspace-sort-control')).not.toBeNull()
    expect(wrapper.findAll('.key-main-row')).toHaveLength(1)
    rows.value = [key(11, 'Updated'), key(29, 'Second')]
    await nextTick()
    expect(wrapper.findAll('.key-main-row')).toHaveLength(2)
    expect(wrapper.get('.key-main-row').text()).toContain('Updated')
    wrapper.unmount(); host.remove()
  })
})
