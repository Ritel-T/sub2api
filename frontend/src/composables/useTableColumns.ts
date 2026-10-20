import { computed, reactive, ref, toValue, type MaybeRefOrGetter } from 'vue'
import type { Column } from '@/components/common/types'

interface ColumnOptions {
  required?: string[]
  fixed?: string[]
}

/** Per-table display preferences. New tables and new columns start fully visible. */
export function useTableColumns(
  storageKey: string,
  source: MaybeRefOrGetter<Column[]>,
  options: ColumnOptions = {}
) {
  const order = ref<string[]>([])
  const hiddenColumns = reactive(new Set<string>())
  const required = new Set([...(options.required ?? []), ...(options.fixed ?? [])])
  const fixed = new Set(options.fixed ?? [])
  try {
    const saved = storageKey ? localStorage.getItem(storageKey) : null
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      if (parsed && typeof parsed === 'object') {
        const value = parsed as { order?: unknown; hidden?: unknown }
        if (Array.isArray(value.order)) {
          order.value = [...new Set(value.order.filter((key): key is string => typeof key === 'string'))]
        }
        if (Array.isArray(value.hidden)) {
          value.hidden.forEach(key => {
            if (typeof key === 'string' && !required.has(key)) hiddenColumns.add(key)
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
  const isColumnVisible = (key: string) => required.has(key) || !hiddenColumns.has(key)
  const columns = computed(() => orderedColumns.value.filter(column => isColumnVisible(column.key)))

  function save() {
    if (!storageKey) return
    try {
      localStorage.setItem(storageKey, JSON.stringify({ order: order.value, hidden: [...hiddenColumns] }))
    } catch {
      // Keep in-memory preferences when storage is full or blocked.
    }
  }

  function toggleColumn(key: string) {
    if (required.has(key) || !toValue(source).some(column => column.key === key)) return
    if (hiddenColumns.has(key)) hiddenColumns.delete(key)
    else hiddenColumns.add(key)
    save()
  }

  function setColumnOrder(keys: string[]) {
    const current = orderedColumns.value.map(column => column.key)
    const valid = new Set(current.filter(key => !fixed.has(key)))
    const requested = [...new Set(keys.filter(key => valid.has(key)))]
    const included = new Set(requested)
    let index = 0
    // A table header only supplies visible columns. Hidden columns retain their slots.
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
    hiddenColumns.clear()
    save()
  }

  return { columns, orderedColumns, hiddenColumns, isColumnVisible, toggleColumn, setColumnOrder, moveColumn, resetColumns }
}
