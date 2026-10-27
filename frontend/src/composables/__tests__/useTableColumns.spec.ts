import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { useTableColumns } from '../useTableColumns'

const defaults = [
  { key: 'select', label: '' }, { key: 'name', label: 'Name' },
  { key: 'status', label: 'Status' }, { key: 'id', label: 'ID' }, { key: 'actions', label: 'Actions' }
]
const options = { fixed: ['select'], required: ['name', 'actions'] }
const keys = (columns: { key: string }[]) => columns.map(column => column.key)
describe('useTableColumns', () => {
  beforeEach(() => { localStorage.clear(); vi.restoreAllMocks() })
  it('retains every column in the supplied default order', () => {
    const table = useTableColumns('table', defaults, options)
    expect(keys(table.columns.value)).toEqual(keys(defaults))
  })
  it('anchors selection and action columns while allowing data columns to move', () => {
    const table = useTableColumns('table', defaults, options)
    table.moveColumn('status', 'name')
    table.moveColumn('actions', 'id')
    table.moveColumn('select', 'id')
    table.toggleColumn('name')
    expect(keys(table.columns.value)).toEqual(['select', 'status', 'name', 'id', 'actions'])
    expect(useTableColumns('table', defaults, options).columns.value).toEqual(table.columns.value)
  })
  it('reorders visible columns without losing hidden column positions', () => {
    const table = useTableColumns('table', defaults, options)
    table.toggleColumn('status')
    table.setColumnOrder(['actions', 'name', 'id'])
    expect(keys(table.columns.value)).toEqual(['select', 'name', 'status', 'id', 'actions'])
    expect(table.columns.value.find(column => column.key === 'status')?.primary).toBe(false)
    table.toggleColumn('status')
    expect(keys(table.columns.value)).toEqual(['select', 'name', 'status', 'id', 'actions'])
  })
  it('sanitizes duplicates and stale keys and appends new columns visibly', () => {
    localStorage.setItem('table', JSON.stringify({ order: ['actions', 'gone', 'actions', 'name'], hidden: ['name', 'gone', 12] }))
    const source = ref(defaults.slice())
    const table = useTableColumns('table', source, options)
    expect(keys(table.columns.value)).toEqual(['select', 'name', 'status', 'id', 'actions'])
    source.value.push({ key: 'new', label: 'New' })
    expect(keys(table.columns.value)).toContain('new')
    source.value = source.value.filter(column => column.key !== 'status')
    expect(keys(table.columns.value)).not.toContain('status')
  })
  it('preserves a temporarily unavailable dynamic column when other columns are reordered', () => {
    const source = ref([...defaults, { key: 'attr_7', label: 'Attribute' }])
    const table = useTableColumns('table', source, options)
    table.moveColumn('attr_7', 'name')
    source.value = [...defaults]
    table.moveColumn('actions', 'status')
    source.value = [...defaults, { key: 'attr_7', label: 'Attribute' }]
    expect(keys(table.columns.value)).toEqual(['select', 'attr_7', 'name', 'status', 'id', 'actions'])
  })

  it('restores default main columns and order and persists the reset', () => {
    const table = useTableColumns('table', defaults, options)
    table.moveColumn('actions', 'name')
    table.toggleColumn('status')
    table.resetColumns()
    expect(keys(useTableColumns('table', defaults, options).columns.value)).toEqual(keys(defaults))
  })
  it('handles malformed and inaccessible storage without breaking display or changes', () => {
    localStorage.setItem('table', 'not json')
    expect(keys(useTableColumns('table', defaults, options).columns.value)).toEqual(keys(defaults))
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('full') })
    const table = useTableColumns('table', defaults, options)
    expect(() => table.toggleColumn('status')).not.toThrow()
    expect(table.columns.value.find(column => column.key === 'status')?.primary).toBe(false)
  })

  it('persists main-row promotion and demotion while keeping all detail fields available to data loaders', () => {
    const source = [...defaults, { key: 'notes', label: 'Notes' }]
    const config = { ...options, primary: ['name', 'status', 'actions'] }
    const table = useTableColumns('details', source, config)
    expect(table.hiddenColumns.has('notes')).toBe(true)
    expect(table.isColumnVisible('notes')).toBe(true)
    expect(table.isColumnVisible('missing')).toBe(false)
    table.toggleColumn('notes')
    table.toggleColumn('status')
    const reopened = useTableColumns('details', source, config)
    expect(reopened.isColumnPrimary('notes')).toBe(true)
    expect(reopened.isColumnPrimary('status')).toBe(false)
    expect(keys(reopened.columns.value)).toEqual(['select', 'name', 'status', 'id', 'notes', 'actions'])
    reopened.resetColumns()
    expect(reopened.isColumnPrimary('notes')).toBe(false)
    expect(reopened.isColumnPrimary('status')).toBe(true)
  })

  it('migrates old hide-only preferences to compact defaults while retaining saved ordering', () => {
    localStorage.setItem('legacy', JSON.stringify({ order: ['actions', 'name', 'status'], hidden: ['status'] }))
    const table = useTableColumns('legacy', [...defaults, { key: 'notes', label: 'Notes' }], { ...options, primary: ['name', 'status', 'actions'] })
    expect(table.isColumnPrimary('status')).toBe(true)
    expect(table.isColumnPrimary('notes')).toBe(false)
    expect(keys(table.columns.value)).toEqual(['select', 'name', 'status', 'id', 'notes', 'actions'])
    table.toggleColumn('notes')
    expect(JSON.parse(localStorage.getItem('legacy')!).version).toBe(2)
  })
})
