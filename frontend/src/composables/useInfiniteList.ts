import { computed, onScopeDispose, ref, shallowRef, type Ref } from 'vue'

export interface InfinitePage<T> {
  items: T[]
  total?: number
  pages?: number
}

export interface InfiniteListOptions<T> {
  fetchPage: (page: number, signal: AbortSignal) => Promise<InfinitePage<T>>
  /** Omit only for APIs whose records have no stable unique identity. */
  keyOf?: (item: T) => string | number
  pageSize?: number
}

/** Server-paged document lists. Only the current request generation may publish. */
export function useInfiniteList<T>(options: InfiniteListOptions<T>) {
  const pageSize = options.pageSize ?? 50
  const items = ref<T[]>([]) as Ref<T[]>
  const total = ref<number | undefined>()
  const page = ref(0)
  const loading = ref(false)
  const loadingMore = ref(false)
  const refreshing = ref(false)
  const error = shallowRef<unknown>(null)
  const exhausted = ref(false)
  const hasMore = computed(() => !exhausted.value)
  let generation = 0
  let controller: AbortController | undefined
  let pending: Promise<void> | undefined
  let failedOperation: 'reset' | 'append' | 'refresh' = 'reset'

  function merge(previous: T[], next: T[]) {
    const keyOf = options.keyOf
    if (!keyOf) return [...previous, ...next]
    const result = [...previous]
    const indices = new Map(result.map((item, index) => [keyOf(item), index]))
    for (const item of next) {
      const key = keyOf(item)
      const index = indices.get(key)
      if (index === undefined) {
        indices.set(key, result.length)
        result.push(item)
      } else result[index] = item
    }
    return result
  }

  function isLast(result: InfinitePage<T>, requestedPage: number, count: number) {
    if (result.items.length === 0) return true
    if (result.pages !== undefined) return requestedPage >= result.pages
    if (result.total !== undefined) return count >= result.total
    return result.items.length < pageSize
  }

  function run(operation: 'reset' | 'append' | 'refresh'): Promise<void> {
    if (operation === 'append' && pending) return pending
    if (operation === 'append' && (exhausted.value || error.value)) return Promise.resolve()
    controller?.abort()
    const requestGeneration = ++generation
    controller = new AbortController()
    const signal = controller.signal
    const previousPage = page.value
    if (operation === 'reset') {
      items.value = []
      page.value = 0
      total.value = undefined
      exhausted.value = false
    }
    error.value = null
    loading.value = items.value.length === 0
    loadingMore.value = operation === 'append' && !loading.value
    refreshing.value = operation === 'refresh'
    failedOperation = operation
    const request = Promise.resolve().then(async () => {
      try {
        if (requestGeneration !== generation || signal.aborted) return
        const firstPage = operation === 'append' ? page.value + 1 : 1
        const finalPage = operation === 'refresh' ? Math.max(1, previousPage) : firstPage
        let nextItems = operation === 'append' ? items.value : []
        let lastResult: InfinitePage<T> | undefined
        let lastPage = firstPage
        let reachedEnd = false
        for (let requestedPage = firstPage; requestedPage <= finalPage; requestedPage++) {
          const result = await options.fetchPage(requestedPage, signal)
          if (requestGeneration !== generation || signal.aborted) return
          nextItems = merge(nextItems, result.items)
          lastResult = result
          lastPage = requestedPage
          reachedEnd = isLast(result, requestedPage, nextItems.length)
          if (reachedEnd) break
        }
        if (requestGeneration !== generation || !lastResult) return
        items.value = nextItems
        page.value = lastPage
        total.value = lastResult.total
        exhausted.value = reachedEnd
      } catch (cause) {
        if (requestGeneration === generation && !signal.aborted) error.value = cause
      } finally {
        if (requestGeneration === generation) {
          loading.value = false
          loadingMore.value = false
          refreshing.value = false
          pending = undefined
        }
      }
    })
    pending = request
    return request
  }

  function retry() {
    if (pending) return pending
    error.value = null
    return run(failedOperation)
  }

  function cancel() {
    generation++
    controller?.abort()
    pending = undefined
    loading.value = false
    loadingMore.value = false
    refreshing.value = false
  }
  function clear() {
    cancel()
    items.value = []
    page.value = 0
    total.value = undefined
    error.value = null
    exhausted.value = false
  }
  /** Publish an independently reconciled loaded window, invalidating older fetches. */
  function replaceLoaded(result: InfinitePage<T>, loadedPage = page.value) {
    cancel()
    items.value = merge([], result.items)
    total.value = result.total
    page.value = loadedPage
    error.value = null
    exhausted.value = result.items.length === 0 ||
      (result.pages !== undefined ? loadedPage >= result.pages :
        result.total !== undefined ? items.value.length >= result.total :
          result.items.length < loadedPage * pageSize)
  }
  onScopeDispose(cancel)

  return {
    items, total, page, loading, loadingMore, refreshing, error, hasMore,
    reset: () => run('reset'),
    loadMore: () => run('append'),
    refresh: () => run('refresh'),
    retry, cancel, clear, replaceLoaded
  }
}
