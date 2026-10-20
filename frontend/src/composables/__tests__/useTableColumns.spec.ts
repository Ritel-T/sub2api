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
  it('shows every column in the supplied default order', () => {
    const table = useTableColumns('table', defaults, options)
    expect(keys(table.columns.value)).toEqual(keys(defaults))
  })
  it('moves required columns freely while anchoring the selection column', () => {
    const table = useTableColumns('table', defaults, options)
    table.moveColumn('actions', 'name')
    table.moveColumn('select', 'id')
    table.toggleColumn('name')
    expect(keys(table.columns.value)).toEqual(['select', 'actions', 'name', 'status', 'id'])
    expect(useTableColumns('table', defaults, options).columns.value).toEqual(table.columns.value)
  })
  it('reorders visible columns without losing hidden column positions', () => {
    const table = useTableColumns('table', defaults, options)
    table.toggleColumn('status')
    table.setColumnOrder(['actions', 'name', 'id'])
    expect(keys(table.columns.value)).toEqual(['select', 'actions', 'name', 'id'])
    table.toggleColumn('status')
    expect(keys(table.columns.value)).toEqual(['select', 'actions', 'status', 'name', 'id'])
  })
  it('sanitizes duplicates and stale keys and appends new columns visibly', () => {
    localStorage.setItem('table', JSON.stringify({ order: ['actions', 'gone', 'actions', 'name'], hidden: ['name', 'gone', 12] }))
    const source = ref(defaults.slice())
    const table = useTableColumns('table', source, options)
    expect(keys(table.columns.value)).toEqual(['select', 'actions', 'name', 'status', 'id'])
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
    expect(keys(table.columns.value)).toEqual(['select', 'attr_7', 'name', 'actions', 'status', 'id'])
  })

  it('restores all columns and default order and persists the reset', () => {
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
    expect(keys(table.columns.value)).not.toContain('status')
  })
})
