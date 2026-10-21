/**
 * Common component types
 */

export interface Column {
  key: string
  label: string
  /** Checked fields occupy the main row; unchecked fields remain in expanded details. */
  primary?: boolean
  /** Optional table layout overrides in pixels and relative flexible weight. */
  width?: number
  minWidth?: number
  weight?: number
  sortable?: boolean
  class?: string
  formatter?: (value: any, row: any) => string
}


/** Compact defaults for unconfigured tables; callers can supply domain-specific primary keys. */
export function defaultPrimaryColumnKeys(columns: Column[]): Set<string> {
  if (columns.some(column => typeof column.primary === 'boolean')) {
    return new Set(columns.filter(column => column.primary !== false).map(column => column.key))
  }
  if (columns.length <= 6) return new Set(columns.map(column => column.key))
  const leading = columns.find(column => !['select', 'id', 'actions'].includes(column.key))?.key
  const common = new Set(['name', 'email', 'key', 'status', 'enabled', 'group', 'groups', 'usage', 'balance', 'model', 'platform', 'current_concurrency', 'concurrency', 'capacity', 'cost', 'requests'])
  const business = columns.filter(column => column.key === leading || common.has(column.key)).slice(0, 5)
  const selected = new Set(['select', 'id', 'actions', ...business.map(column => column.key)])
  if (business.length < 3) columns.filter(column => !selected.has(column.key)).slice(0, 3 - business.length).forEach(column => selected.add(column.key))
  return selected
}
