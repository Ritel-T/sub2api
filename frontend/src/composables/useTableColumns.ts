import { computed, reactive, ref, toValue, watchEffect, type MaybeRefOrGetter } from 'vue'
import { defaultPrimaryColumnKeys, type Column } from '@/components/common/types'

interface ColumnOptions {
  required?: MaybeRefOrGetter<string[]>
  fixed?: string[]
  primary?: MaybeRefOrGetter<string[] | undefined>
}

/** Per-table main-row preferences. Every other field remains available in expanded details. */
export function useTableColumns(
  storageKey: string,
  source: MaybeRefOrGetter<Column[]>,
  options: ColumnOptions = {}
) {
  const order = ref<string[]>([])
  const primaryOverrides = reactive(new Map<string, boolean>())
  const required = computed(() => new Set([...(options.required ? toValue(options.required) : []), ...(options.fixed ?? [])]))
  const fixed = new Set(options.fixed ?? [])
  try {
    const saved = storageKey ? localStorage.getItem(storageKey) : null
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      if (parsed && typeof parsed === 'object') {
        const value = parsed as { version?: unknown; order?: unknown; primary?: unknown }
        if (Array.isArray(value.order)) {
          order.value = [...new Set(value.order.filter((key): key is string => typeof key === 'string'))]
        }
        // Old hidden preferences meant data removal. Preserve order, adopt the new main-row defaults once.
        if (value.version === 2 && value.primary && typeof value.primary === 'object' && !Array.isArray(value.primary)) {
          Object.entries(value.primary).forEach(([key, primary]) => {
            if (typeof primary === 'boolean' && !required.value.has(key)) primaryOverrides.set(key, primary)
          })
        }
      }
    }
  } catch {
    // Browser storage can be unavailable; preferences still work for this visit.
  }

  const orderedColumns = computed(() => {
    const defaults = toValue(source)
    const byKey = new Map(defaults.map(column => [column.key, column]))
    const keys = [...new Set([...order.value, ...defaults.map(column => column.key)])]
    const movable = keys.filter(key => byKey.has(key) && !fixed.has(key))
    let index = 0
    return defaults.map(column => fixed.has(column.key) ? column : byKey.get(movable[index++])!)
  })
  const defaultPrimary = computed(() => {
    if (toValue(source).some(column => typeof column.primary === 'boolean')) return defaultPrimaryColumnKeys(toValue(source))
    const explicit = options.primary ? toValue(options.primary) : undefined
    return explicit ? new Set(explicit) : defaultPrimaryColumnKeys(toValue(source))
  })
  const isColumnPrimary = (key: string) => required.value.has(key) || (primaryOverrides.get(key) ?? defaultPrimary.value.has(key))
  // Existing data loaders use visibility to decide which API fields to fetch. Details are visible too.
  const isColumnVisible = (key: string) => toValue(source).some(column => column.key === key)
  const hiddenColumns = reactive(new Set<string>())
  watchEffect(() => {
    const next = orderedColumns.value.filter(column => !isColumnPrimary(column.key)).map(column => column.key)
    hiddenColumns.clear()
    next.forEach(key => hiddenColumns.add(key))
  }, { flush: 'sync' })
  const columns = computed(() => orderedColumns.value.map(column => ({ ...column, primary: isColumnPrimary(column.key) })))

  function save() {
    if (!storageKey) return
    try {
      localStorage.setItem(storageKey, JSON.stringify({ version: 2, order: order.value, primary: Object.fromEntries(primaryOverrides) }))
    } catch {
      // Keep in-memory preferences when storage is full or blocked.
    }
  }

  function toggleColumn(key: string) {
    if (required.value.has(key) || !toValue(source).some(column => column.key === key)) return
    primaryOverrides.set(key, !isColumnPrimary(key))
    save()
  }

  function setColumnOrder(keys: string[]) {
    const current = orderedColumns.value.map(column => column.key)
    const valid = new Set(current.filter(key => !fixed.has(key)))
    const requested = [...new Set(keys.filter(key => valid.has(key)))]
    const included = new Set(requested)
    let index = 0
    // A table header only supplies visible columns. Detail fields retain their slots.
    const previous = [...new Set([...order.value, ...current])]
    order.value = previous.map(key => included.has(key) ? requested[index++]! : key)
    save()
  }

  function moveColumn(from: string, to: string) {
    if (fixed.has(from) || fixed.has(to)) return
    const keys = orderedColumns.value.map(column => column.key)
    const fromIndex = keys.indexOf(from)
    const toIndex = keys.indexOf(to)
    if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return
    keys.splice(toIndex, 0, keys.splice(fromIndex, 1)[0]!)
    setColumnOrder(keys)
  }

  function resetColumns() {
    order.value = []
    primaryOverrides.clear()
    save()
  }

  return { columns, orderedColumns, hiddenColumns, isColumnVisible, isColumnPrimary, toggleColumn, setColumnOrder, moveColumn, resetColumns }
}
