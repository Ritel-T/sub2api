import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DataTable from '../DataTable.vue'
import TableSortControl from '../TableSortControl.vue'
import { VueDraggable } from 'vue-draggable-plus'
import { defineComponent, ref, nextTick, h } from 'vue'
import ColumnSettings from '../ColumnSettings.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const stubDesktopMatchMedia = () => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

const stubMobileMatchMedia = () => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

describe('DataTable', () => {
  beforeEach(() => {
    stubDesktopMatchMedia()
    localStorage.clear()
  })

  it('exposes the active scroll owner for swipe selection even below the virtualization threshold', async () => {
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'id', label: 'ID' }], data: [{ id: 1 }]
    } })
    await nextTick()
    expect((wrapper.vm as any).selectionScrollElement).toBe(window)
    expect((wrapper.vm as any).virtualizer.options.getScrollElement()).toBe(window)
    await wrapper.setProps({ open: false })
    expect((wrapper.vm as any).selectionScrollElement).toBe(wrapper.get('.table-wrapper').element)
    expect((wrapper.vm as any).virtualizer.options.getScrollElement()).toBe(wrapper.get('.table-wrapper').element)
    wrapper.unmount()
  })
  it('reorders header and cells, persists order, and keeps row sorting separate', async () => {
    const props = {
      columnOrderKey: 'test-order',
      columns: [{ key: 'name', label: 'Name', sortable: true }, { key: 'status', label: 'Status' }, { key: 'actions', label: 'Actions' }],
      data: [{ id: 1, name: 'A', status: 'active' }], serverSideSort: true
    }
    const wrapper = mount(DataTable, { props })
    await wrapper.find('th[data-column-key="actions"] button').trigger('keydown', { key: 'Home' })
    expect(wrapper.findAll('th[data-column-key]').map(cell => cell.attributes('data-column-key'))).toEqual(['actions', 'name', 'status'])
    expect(wrapper.find('td[data-column-key="actions"]').classes()).not.toContain('sticky-col-right')
    expect(wrapper.emitted('sort')).toBeUndefined()
    const reopened = mount(DataTable, { props })
    expect(reopened.findAll('th[data-column-key]').map(cell => cell.attributes('data-column-key'))).toEqual(['actions', 'name', 'status'])
    reopened.unmount(); wrapper.unmount()
  })

  it('emits controlled drag reordering without changing selection or row sorting', async () => {
    const onColumnReorder = vi.fn()
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'select', label: '' }, { key: 'name', label: 'Name', sortable: true }, { key: 'status', label: 'Status' }],
      data: [{ id: 1, name: 'A' }], onColumnReorder, serverSideSort: true
    } })
    expect(wrapper.find('th[data-column-key="select"] button').exists()).toBe(false)
    const draggable = wrapper.findComponent(VueDraggable)
    draggable.vm.$emit('update:modelValue', [
      { key: 'select', label: '' }, { key: 'status', label: 'Status' }, { key: 'name', label: 'Name', sortable: true }
    ])
    await wrapper.vm.$nextTick()
    expect(onColumnReorder).toHaveBeenCalledWith(['select', 'status', 'name'])
    expect(wrapper.emitted('sort')).toBeUndefined()
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    wrapper.unmount()
  })

  it('uses the saved order for mobile card fields including the actions column', async () => {
    stubMobileMatchMedia()
    localStorage.setItem('mobile-order', JSON.stringify({ order: ['actions', 'status', 'name'], hidden: [] }))
    const wrapper = mount(DataTable, { props: {
      columnOrderKey: 'mobile-order', columns: [{ key: 'name', label: 'Name' }, { key: 'status', label: 'Status' }, { key: 'actions', label: 'Actions' }],
      data: [{ id: 1, name: 'A', status: 'active' }]
    }, slots: { 'cell-actions': '<button>Edit</button>' } })
    expect(wrapper.findAll('.table-card [data-field]').map(field => field.attributes('data-field'))).toEqual(['actions', 'status', 'name'])
    expect(wrapper.findAll('.table-card button')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not display a false default sorting indicator when the active server-sort column is hidden', async () => {
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'name', label: 'Name', sortable: true }, { key: 'created_at', label: 'Created', sortable: true }],
      data: [], serverSideSort: true, defaultSortKey: 'created_at'
    } })
    await wrapper.find('th[data-column-key="name"]').trigger('click')
    expect(wrapper.emitted('sort')).toEqual([['name', 'asc']])
    await wrapper.setProps({ columns: [{ key: 'created_at', label: 'Created', sortable: true }] })
    expect(wrapper.find('th').attributes('aria-sort')).toBe('none')
    await wrapper.setProps({ columns: [{ key: 'name', label: 'Name', sortable: true }, { key: 'created_at', label: 'Created', sortable: true }] })
    expect(wrapper.find('th[data-column-key="name"]').attributes('aria-sort')).toBe('ascending')
    expect(wrapper.emitted('sort')).toHaveLength(1)
    wrapper.unmount()
  })

  it('retains the active hidden sort field in the mobile sorting control', async () => {
    stubMobileMatchMedia()
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'name', label: 'Name', sortable: true }, { key: 'created_at', label: 'Created', sortable: true }],
      data: [{ id: 1, name: 'A' }], serverSideSort: true, defaultSortKey: 'name'
    } })
    await wrapper.setProps({ columns: [{ key: 'created_at', label: 'Created', sortable: true }] })
    const select = wrapper.findComponent(TableSortControl)
    expect(select.props('sortKey')).toBe('name')
    expect(select.props('columns').map(column => column.label)).toEqual(['Name', 'Created'])
    wrapper.unmount()
  })

  it('renders paired sort arrows and highlights the active direction', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [
          { key: 'name', label: 'Name', sortable: true },
          { key: 'created_at', label: 'Created', sortable: true }
        ],
        data: [
          { id: 1, name: 'Beta', created_at: '2026-01-02T00:00:00Z' },
          { id: 2, name: 'Alpha', created_at: '2026-01-01T00:00:00Z' }
        ],
        defaultSortKey: 'name',
        defaultSortOrder: 'asc'
      },
      slots: {
        'header-name': '<span data-test="custom-name-header">Name</span>'
      }
    })

    await wrapper.vm.$nextTick()

    const nameHeader = wrapper.findAll('th')[0]
    expect(nameHeader.find('[data-test="custom-name-header"]').exists()).toBe(true)
    expect(nameHeader.attributes('aria-sort')).toBe('ascending')
    expect(nameHeader.findAll('svg')).toHaveLength(2)
    expect(nameHeader.findAll('svg')[0].classes()).toContain('text-primary-600')
    expect(nameHeader.findAll('svg')[1].classes()).toContain('text-gray-300')

    await nameHeader.trigger('click')
    await wrapper.vm.$nextTick()

    expect(nameHeader.attributes('aria-sort')).toBe('descending')
    expect(nameHeader.findAll('svg')[0].classes()).toContain('text-gray-300')
    expect(nameHeader.findAll('svg')[1].classes()).toContain('text-primary-600')
  })

  it('renders every row with no virtual padding spacer for small datasets (virtualization off)', async () => {
    const data = Array.from({ length: 8 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data
      }
    })

    await wrapper.vm.$nextTick()

    // Virtualization is OFF for a small list…
    expect((wrapper.vm as any).shouldVirtualize).toBe(false)
    // …every row is in the DOM…
    expect(wrapper.findAll('tbody tr[data-index]')).toHaveLength(data.length)
    // …and there are no aria-hidden virtual padding spacer rows.
    expect(wrapper.findAll('tbody tr[aria-hidden="true"]')).toHaveLength(0)
  })

  it('switches to windowed rendering once row count exceeds virtualizeThreshold', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        open: false, virtualizeThreshold: 3
      }
    })

    await wrapper.vm.$nextTick()

    // Virtualization is ON: the mode-switch decision flipped…
    expect((wrapper.vm as any).shouldVirtualize).toBe(true)
    // …and the virtualizer drives off the full row count.
    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    expect(instance.options.count).toBe(data.length)
  })

  it('keys the virtualizer size cache by row identity, not index (avoids stale heights on sort/filter)', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: 100 + i, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        rowKey: 'id',
        open: false, virtualizeThreshold: 3
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    // getItemKey must resolve to the row's stable key (id), not the positional index.
    expect(instance.options.getItemKey(0)).toBe(100)
    expect(instance.options.getItemKey(5)).toBe(105)
  })

  it('clears stale row and element caches when pagination replaces the row ID set', async () => {
    const firstPage = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 100 }, (_, i) => ({ id: i + 101, name: `Second ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        rowKey: 'id',
        open: false, virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const firstPageIDs = firstPage.map(row => row.id)
    ;(instance as any).itemSizeCache = new Map(firstPageIDs.map(id => [id, 156]))
    instance.elementsCache.clear()
    for (const id of firstPageIDs) {
      instance.elementsCache.set(id, document.createElement('tr'))
    }
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    const sizeCache = (instance as any).itemSizeCache as Map<number, number>
    expect(sizeCache.size).toBeLessThanOrEqual(secondPage.length)
    expect(instance.elementsCache.size).toBeLessThanOrEqual(secondPage.length)
    expect(firstPageIDs.some(id => sizeCache.has(id))).toBe(false)
    expect(firstPageIDs.some(id => instance.elementsCache.has(id))).toBe(false)
    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('clears stale caches when equal-length pages replace rows without stable keys', async () => {
    const firstPage = Array.from({ length: 12 }, (_, i) => ({ name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 12 }, (_, i) => ({ name: `Second ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        open: false, virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('conservatively clears caches when duplicate row-key multiplicity changes', async () => {
    const firstPage = [
      { id: 1, name: 'First A' },
      { id: 1, name: 'First B' },
      { id: 2, name: 'First C' }
    ]
    const secondPage = [
      { id: 1, name: 'Second A' },
      { id: 2, name: 'Second B' },
      { id: 2, name: 'Second C' }
    ]
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        rowKey: 'id',
        open: false, virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('preserves cache when rows without stable keys only reorder the same objects', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        open: false, virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureSpy = vi.spyOn(instance, 'measure')

    await wrapper.setProps({ data: [...data].reverse() })
    await wrapper.vm.$nextTick()

    expect(measureSpy).not.toHaveBeenCalled()
  })

  it('preserves stable row height cache when the same row IDs are only reordered', async () => {
    const data = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        rowKey: 'id',
        open: false, virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    ;(instance as any).itemSizeCache = new Map(data.map(row => [row.id, 156]))
    const measureSpy = vi.spyOn(instance, 'measure')

    await wrapper.setProps({ data: [...data].reverse() })
    await wrapper.vm.$nextTick()

    const sizeCache = (instance as any).itemSizeCache as Map<number, number>
    expect(measureSpy).not.toHaveBeenCalled()
    expect(sizeCache.size).toBe(100)
  })

  it('emits controlled current-page selection while preserving off-page keys', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: [
          { id: 1, name: 'One' },
          { id: 2, name: 'Two' }
        ],
        rowKey: 'id',
        selectable: true,
        selectedKeys: [99]
      }
    })

    await wrapper.get('[data-test="select-all"]').trigger('click')

    const selectedAll = wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]
    expect(selectedAll).toEqual([99, 1, 2])

    await wrapper.setProps({ selectedKeys: selectedAll as number[] })
    const rowCheckboxes = wrapper.findAll('[data-test="select-row"]')
    expect(rowCheckboxes.every((checkbox) => checkbox.attributes('aria-checked') === 'true')).toBe(true)

    await rowCheckboxes[0].trigger('click')

    expect(wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]).toEqual([99, 2])
    expect(wrapper.emitted('selectionChange')?.at(-1)?.[0]).toEqual([99, 2])
  })

  it('keeps the single usage field shrinkable in a 320px mobile card', () => {
    stubMobileMatchMedia()
    const viewport = document.createElement('div')
    viewport.style.width = '320px'
    document.body.appendChild(viewport)
    const wrapper = mount(DataTable, {
      attachTo: viewport,
      props: {
        columns: [{ key: 'usage', label: 'Usage' }],
        data: [{ id: 1, usage: 'snapshot' }],
        rowKey: 'id'
      },
      slots: {
        'cell-usage': '<div data-test="usage-cell">snapshot</div>'
      }
    })

    expect(viewport.style.width).toBe('320px')
    expect(wrapper.findAll('[data-field="usage"]')).toHaveLength(1)
    expect(wrapper.find('[data-field="ollama_cloud_usage"]').exists()).toBe(false)
    const field = wrapper.get('[data-field="usage"]')
    expect(field.classes()).toContain('min-w-0')
    expect(field.get('div').classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))
    expect(wrapper.findAll('[data-test="usage-cell"]')).toHaveLength(1)

    wrapper.unmount()
    viewport.remove()
  })

  it('offers current-page select all in the mobile card layout', async () => {
    stubMobileMatchMedia()
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: [
          { id: 1, name: 'One' },
          { id: 2, name: 'Two' }
        ],
        rowKey: 'id',
        selectable: true,
        selectedKeys: [99]
      }
    })

    await wrapper.get('[data-test="select-all-mobile"]').trigger('click')

    expect(wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]).toEqual([99, 1, 2])
  })
  it('keeps large open lists in document flow and moves secondary fields below each row', async () => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    const columns = ['id', 'name', 'status', 'group', 'usage', 'concurrency', 'actions', 'created_at', 'expires_at', 'notes'].map(key => ({ key, label: key, sortable: true }))
    const data = Array.from({ length: 120 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}`, notes: `Note ${i + 1}` }))
    const wrapper = mount(DataTable, { props: { columns, data }, slots: { 'cell-notes': '<strong data-test="note">Custom detail</strong>' } })
    await flushPromises()
    expect((wrapper.vm as any).shouldVirtualize).toBe(false)
    expect(wrapper.findAll('tr.table-row').length).toBeGreaterThan(0)
    expect(wrapper.findAll('tr.table-row').length).toBeLessThan(120)
    expect(wrapper.get('table').attributes('aria-rowcount')).toBe('121')
    expect(wrapper.get('tr.table-row').attributes('aria-rowindex')).toBe('2')
    expect(wrapper.find('th[data-column-key="notes"]').exists()).toBe(false)
    expect(wrapper.find('.table-detail-row').exists()).toBe(false)
    await wrapper.find('tr.table-row').trigger('click')
    expect(wrapper.findAll('.table-detail-row')).toHaveLength(1)
    expect(wrapper.find('.table-detail-row [data-test="note"]').text()).toBe('Custom detail')
    expect(wrapper.find('[data-test="toggle-details"]').attributes('aria-expanded')).toBe('true')
    wrapper.unmount()
  })

  it.each(['desktop', 'mobile'])('opens a custom row-details slot with no secondary columns on %s', async viewport => {
    if (viewport === 'mobile') stubMobileMatchMedia()
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'name', label: 'Name' }], data: [{ id: 1, name: 'One' }],
    }, slots: { 'row-details': ({ row, columns }: any) => h('button', { 'data-test': 'custom-details' }, `${row.name}:${columns.length}`) } })
    expect(wrapper.find('[data-test="custom-details"]').exists()).toBe(false)
    if (viewport === 'desktop') {
      expect(wrapper.findAll('colgroup col')).toHaveLength(2)
      expect(wrapper.get('colgroup col:last-child').attributes('style')).toContain('44px')
      expect(wrapper.findAll('thead th')).toHaveLength(2)
    }
    await wrapper.get(viewport === 'desktop' ? 'tr.table-row' : '.table-record').trigger('click')
    expect(wrapper.get('[data-test="custom-details"]').text()).toBe('One:0')
    expect(wrapper.get('[data-test="toggle-details"]').attributes('aria-expanded')).toBe('true')
    if (viewport === 'desktop') expect(wrapper.get('.table-detail-row > td').attributes('colspan')).toBe('2')
    await wrapper.get('[data-test="custom-details"]').trigger('click')
    expect(wrapper.get('[data-test="toggle-details"]').attributes('aria-expanded')).toBe('true')
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.find('[data-test="custom-details"]').exists()).toBe(false)
    if (viewport === 'desktop') {
      await wrapper.setProps({ data: [] })
      expect(wrapper.get('tbody td').attributes('colspan')).toBe('2')
      await wrapper.setProps({ loading: true })
      expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(2)
    }
    wrapper.unmount()
  })

  it.each(['desktop', 'mobile'])('lets custom details reuse cell slots, formatters, and plain values on %s', async viewport => {
    if (viewport === 'mobile') stubMobileMatchMedia()
    const wrapper = mount(DataTable, { props: {
      primaryColumns: ['name'],
      columns: [{ key: 'name', label: 'Name' }, { key: 'notes', label: 'Notes' },
        { key: 'amount', label: 'Amount', formatter: (value: number) => `$${value}` }, { key: 'region', label: 'Region' }],
      data: [{ id: 1, name: 'One', notes: 'Custom note', amount: 5, region: 'Tokyo' }],
    }, slots: {
      'cell-notes': ({ row, value, expanded }: any) => h('button', { 'data-test': 'reused-cell' }, `${row.id}:${value}:${expanded}`),
      'row-details': ({ columns, renderCell }: any) => h('section', { 'data-test': 'custom-layout' },
        [...columns].reverse().map(column => h('div', { 'data-detail': column.key }, renderCell(column)))),
    } })
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.findAll('[data-detail]').map(node => node.attributes('data-detail'))).toEqual(['region', 'amount', 'notes'])
    expect(wrapper.get('[data-test="reused-cell"]').text()).toBe('1:Custom note:false')
    expect(wrapper.get('[data-detail="amount"]').text()).toBe('$5')
    expect(wrapper.get('[data-detail="region"]').text()).toBe('Tokyo')
    expect(wrapper.find('.table-details-grid').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not add expansion controls when all fields are primary and no custom details are supplied', () => {
    const wrapper = mount(DataTable, { props: { columns: [{ key: 'name', label: 'Name' }], data: [{ id: 1, name: 'One' }] } })
    expect(wrapper.find('[data-test="toggle-details"]').exists()).toBe(false)
    expect(wrapper.findAll('colgroup col')).toHaveLength(1)
    wrapper.unmount()
  })

  it('sorts secondary fields through the shared control without locally reordering server data', async () => {
    const wrapper = mount(DataTable, { props: {
      primaryColumns: ['name'], columns: [{ key: 'name', label: 'Name' }, { key: 'created_at', label: 'Created', sortable: true }],
      data: [{ id: 2, name: 'Second' }, { id: 1, name: 'First' }], serverSideSort: true
    } })
    wrapper.findComponent(TableSortControl).vm.$emit('select', 'created_at')
    await nextTick()
    expect(wrapper.emitted('sort')).toEqual([['created_at', 'asc']])
    expect(wrapper.findAll('tr.table-row').map(row => row.attributes('data-row-id'))).toEqual(['2', '1'])
    wrapper.unmount()
  })

  it('preserves row navigation while controls and explicit detail expansion stay independent', async () => {
    const action = vi.fn()
    const wrapper = mount(DataTable, { props: { primaryColumns: ['name', 'actions'], columns: [{ key: 'name', label: 'Name' }, { key: 'notes', label: 'Notes' }, { key: 'actions', label: 'Actions' }], data: [{ id: 1, name: 'One', notes: 'Note' }], clickableRows: true }, slots: { 'cell-actions': '<button data-test="action">Edit</button>' } })
    wrapper.get('[data-test="action"]').element.addEventListener('click', action)
    await wrapper.get('[data-test="action"]').trigger('click')
    expect(action).toHaveBeenCalledOnce()
    expect(wrapper.emitted('rowClick')).toBeUndefined()
    expect(wrapper.find('.table-detail-row').exists()).toBe(false)
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.find('.table-detail-row').exists()).toBe(true)
    expect(wrapper.emitted('rowClick')).toBeUndefined()
    await wrapper.get('tr.table-row').trigger('click')
    expect(wrapper.emitted('rowClick')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not relabel a named row key as an ID', () => {
    const wrapper = mount(DataTable, { props: { columns: [{ key: 'name', label: 'Node' }], data: [{ name: 'Tokyo' }], rowKey: 'name', selectable: true } })
    expect(wrapper.find('th[data-column-key="id"]').exists()).toBe(false)
    expect(wrapper.find('input[data-test="select-row"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('preserves expansion by stable identity across sorting and clears removed records', async () => {
    const rows = [{ id: 1, name: 'Beta', notes: 'Beta note' }, { id: 2, name: 'Alpha', notes: 'Alpha note' }]
    const wrapper = mount(DataTable, { props: { primaryColumns: ['name'], columns: [{ key: 'name', label: 'Name', sortable: true }, { key: 'notes', label: 'Notes' }], data: rows } })
    await wrapper.get('tr[data-row-id="1"] [data-test="toggle-details"]').trigger('click')
    await wrapper.get('th[data-column-key="name"]').trigger('click')
    expect(wrapper.find('.table-detail-row').text()).toContain('Beta note')
    await wrapper.setProps({ data: [rows[1]] })
    expect(wrapper.find('.table-detail-row').exists()).toBe(false)
    wrapper.unmount()
  })

  it('expands secondary cell slots in the mobile record flow', async () => {
    stubMobileMatchMedia()
    const wrapper = mount(DataTable, { props: { primaryColumns: ['name'], columns: [{ key: 'name', label: 'Name' }, { key: 'notes', label: 'Notes' }], data: [{ id: 1, name: 'One', notes: 'Note' }] } })
    expect(wrapper.find('[data-field="notes"]').exists()).toBe(false)
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.get('[data-field="notes"]').text()).toContain('Note')
    wrapper.unmount()
  })

  it('moves primary columns past each other without displacing secondary columns', async () => {
    const wrapper = mount(DataTable, { props: { columnOrderKey: 'sparse', selectable: true, primaryColumns: ['id', 'name', 'status', 'actions'], columns: [{ key: 'name', label: 'Name' }, { key: 'notes', label: 'Notes' }, { key: 'status', label: 'Status' }, { key: 'id', label: 'ID' }, { key: 'actions', label: 'Actions' }], data: [{ id: 1, name: 'One' }] } })
    await wrapper.get('th[data-column-key="name"] .table-column-drag-handle').trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.findAll('th[data-column-key]').map(cell => cell.attributes('data-column-key'))).toEqual(['status', 'name', 'id', 'actions'])
    expect(wrapper.get('th[data-column-key="id"]').find('.table-column-drag-handle').exists()).toBe(true)
    expect(wrapper.emitted('columnReorder')?.at(-1)?.[0]).toEqual(['status', 'notes', 'name', 'id', 'actions'])
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.get('.table-detail-row [data-field]').attributes('data-field')).toBe('notes')
    wrapper.unmount()
  })


  it('moves and persists ID while ID sorting and select-all stay independent', async () => {
    const props = { columnOrderKey: 'id-order', selectable: true, serverSideSort: true,
      columns: [{ key: 'id', label: 'ID', sortable: true }, { key: 'name', label: 'Name' }],
      data: [{ id: 2, name: 'Second' }, { id: 1, name: 'First' }], selectedKeys: [2] }
    const wrapper = mount(DataTable, { props })
    await wrapper.get('th[data-column-key="id"] .table-column-drag-handle').trigger('keydown', { key: 'End' })
    expect(wrapper.findAll('th[data-column-key]').map(cell => cell.attributes('data-column-key'))).toEqual(['name', 'id'])
    expect(wrapper.emitted('sort')).toBeUndefined()
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    await wrapper.get('th[data-column-key="id"]').trigger('click')
    expect(wrapper.emitted('sort')).toEqual([['id', 'asc']])
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    await wrapper.get('[data-test="select-all"]').trigger('click')
    expect(wrapper.emitted('update:selectedKeys')?.at(-1)).toEqual([[2, 1]])
    expect(wrapper.emitted('sort')).toHaveLength(1)
    expect(wrapper.findAll('tr.table-row').map(row => row.attributes('data-row-id'))).toEqual(['2', '1'])
    const restored = mount(DataTable, { props })
    expect(restored.findAll('th[data-column-key]').map(cell => cell.attributes('data-column-key'))).toEqual(['name', 'id'])
    restored.unmount(); wrapper.unmount()
  })

  it('uses checkboxes to promote any detail field and demote any optional main field without losing its control', async () => {
    const wrapper = mount(DataTable, { props: {
      columnOrderKey: 'detail-contract', primaryColumns: ['name', 'status'],
      columns: [{ key: 'name', label: 'Name' }, { key: 'status', label: 'Status' }, { key: 'notes', label: 'Notes', sortable: true }],
      data: [{ id: 1, name: 'One', notes: 'Note', status: 'active' }]
    }, slots: { 'cell-status': '<button data-test="status-control">Change status</button>' } })
    const settings = wrapper.findComponent(ColumnSettings)
    settings.vm.$emit('toggle', 'notes')
    settings.vm.$emit('toggle', 'status')
    await nextTick()
    expect(wrapper.find('th[data-column-key="notes"]').exists()).toBe(true)
    expect(wrapper.find('th[data-column-key="status"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="status-control"]').exists()).toBe(false)
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.get('.table-detail-row [data-test="status-control"]').text()).toBe('Change status')
    expect(wrapper.findComponent(TableSortControl).props('columns').some(column => column.key === 'notes')).toBe(true)
    wrapper.unmount()
  })

  it('moves its existing toolbar into the shared filter target after the parent target mounts', async () => {
    const target = document.createElement('div')
    document.body.append(target)
    const targetRef = ref<HTMLElement | null>(null)
    const wrapper = mount(DataTable, { props: { columns: [{ key: 'name', label: 'Name', sortable: true }], data: [] }, global: { provide: { tableToolbarTarget: targetRef } } })
    targetRef.value = target
    await nextTick()
    await nextTick()
    expect(target.querySelector('.workspace-sort-control')).not.toBeNull()
    expect(wrapper.find('.workspace-sort-control').exists()).toBe(false)
    wrapper.unmount(); target.remove()
  })

  it('honors parent main-row metadata even when it demotes the first source field and contradicts default primary keys', async () => {
    const wrapper = mount(DataTable, { props: {
      primaryColumns: ['created_at', 'message'],
      columns: [{ key: 'created_at', label: 'Time', primary: false }, { key: 'message', label: 'Message', primary: false }, { key: 'account', label: 'Account', primary: true }],
      data: [{ id: 1, account: 'One', created_at: 'Today', message: 'Detail' }]
    } })
    expect(wrapper.findAll('th[data-column-key]').map(cell => cell.attributes('data-column-key'))).toEqual(['account'])
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.get('.table-detail-row').text()).toContain('Today')
    expect(wrapper.get('.table-detail-row').text()).toContain('Detail')
    wrapper.unmount()
  })

  it('mounts a sibling string toolbar target before moving controls and retains rows after refresh', async () => {
    const rows = ref([{ id: 1, name: 'One' }])
    const Parent = defineComponent({
      components: { DataTable }, setup: () => ({ rows, columns: [{ key: 'name', label: 'Name', sortable: true }] }),
      template: '<div><div id="table-filter-target"></div><DataTable toolbar-target="#table-filter-target" :columns="columns" :data="rows" /></div>',
    })
    const host = document.createElement('div')
    document.body.append(host)
    const wrapper = mount(Parent, { attachTo: host })
    await nextTick(); await nextTick()
    expect(host.querySelector('#table-filter-target .workspace-sort-control')).not.toBeNull()
    rows.value = [{ id: 2, name: 'Two' }]
    await nextTick()
    expect(wrapper.get('tr.table-row').text()).toContain('Two')
    wrapper.unmount(); host.remove()
  })

  it('reserves the full action group width independently of the detail-expansion column', async () => {
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'name', label: 'Name', primary: true }, { key: 'actions', label: 'Actions', primary: true }, { key: 'notes', label: 'Notes', primary: false }],
      actionsCount: 6, data: [{ id: 1, name: 'Group', notes: 'Detail' }],
    } })
    const widths = () => wrapper.findAll('colgroup col').map(column => column.attributes('style'))
    expect(widths().at(-2)).toContain('240px')
    expect(widths().at(-1)).toContain('44px')
    await wrapper.setProps({ actionsCount: 7 })
    expect(widths().at(-2)).toContain('276px')
    expect(widths().at(-1)).toContain('44px')
    wrapper.unmount()
  })
})


it.each(['desktop', 'mobile'])('exposes one loading status and keeps the %s table structure', async (viewport) => {
  if (viewport === 'desktop') stubDesktopMatchMedia()
  else stubMobileMatchMedia()
  const wrapper = mount(DataTable, { props: { columns: [{ key: 'id', label: 'ID' }], data: [], loading: true } })
  expect(wrapper.findAll('[role="status"]')).toHaveLength(1)
  expect(wrapper.get('[role="status"]').text()).toBe('common.loading')
  const body = wrapper.get(viewport === 'desktop' ? 'table' : '.table-mobile-records')
  expect(body.attributes('aria-busy')).toBe('true')
  if (viewport === 'desktop') expect(wrapper.get('tbody').element.parentElement?.tagName).toBe('TABLE')
  await wrapper.setProps({ loading: false })
  expect(wrapper.find('[role="status"]').exists()).toBe(false)
  expect(body.attributes('aria-busy')).toBe('false')
  wrapper.unmount()
})

describe('disabled controlled selection', () => {
  it.each([true, false])('blocks native selection and keeps column tools usable (desktop %s)', async desktop => {
    if (desktop) stubDesktopMatchMedia()
    else stubMobileMatchMedia()
    const wrapper = mount(DataTable, { props: {
      columns: [{ key: 'id', label: 'ID', primary: true }, { key: 'name', label: 'Name', primary: true }],
      data: [{ id: 1, name: 'One' }, { id: 2, name: 'Two' }], selectable: true,
      selectionDisabled: true, selectedKeys: [99], columnOrderKey: `selection-disabled-${desktop}`
    } })
    const controls = wrapper.findAll('[data-test="select-row"], [data-test="select-all"], [data-test="select-all-mobile"]')
    expect(controls.length).toBeGreaterThan(0)
    expect(controls.every(control => control.attributes('disabled') !== undefined)).toBe(true)
    for (const control of controls) {
      control.element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      control.element.dispatchEvent(new Event('change', { bubbles: true }))
    }
    await nextTick()
    expect(wrapper.emitted('update:selectedKeys')).toBeUndefined()
    expect(wrapper.emitted('selectionChange')).toBeUndefined()
    expect(wrapper.findComponent(ColumnSettings).exists()).toBe(true)
    await wrapper.setProps({ selectionDisabled: false })
    const selectAll = wrapper.get(desktop ? '[data-test="select-all"]' : '[data-test="select-all-mobile"]')
    expect(selectAll.attributes('disabled')).toBeUndefined()
    await selectAll.trigger('click')
    expect(wrapper.emitted('update:selectedKeys')?.at(-1)).toEqual([[99, 1, 2]])
    wrapper.unmount()
  })
})
