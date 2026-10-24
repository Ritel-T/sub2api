// @vitest-environment node
import { effectScope } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useInfiniteList, type InfiniteListOptions, type InfinitePage } from '../useInfiniteList'

type Row = { id: number; value?: string }
function setup(options: InfiniteListOptions<Row>) {
  const scope = effectScope()
  const list = scope.run(() => useInfiniteList(options))!
  return { list, stop: () => scope.stop() }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('useInfiniteList', () => {
  it('appends pages once, deduplicates stable IDs and stops at server total', async () => {
    const fetchPage = vi.fn(async (page: number) => ({
      items: page === 1 ? [{ id: 1 }, { id: 2 }] : [{ id: 2, value: 'fresh' }, { id: 3 }],
      total: 3
    }))
    const { list, stop } = setup({ fetchPage, keyOf: row => row.id, pageSize: 2 })
    await list.reset()
    await Promise.all([list.loadMore(), list.loadMore()])
    expect(fetchPage).toHaveBeenCalledTimes(2)
    expect(list.items.value).toEqual([{ id: 1 }, { id: 2, value: 'fresh' }, { id: 3 }])
    expect(list.hasMore.value).toBe(false)
    await list.loadMore()
    expect(fetchPage).toHaveBeenCalledTimes(2)
    stop()
  })

  it('rejects old query results even when the transport ignores AbortSignal', async () => {
    const old = deferred<InfinitePage<Row>>()
    const fetchPage = vi.fn().mockReturnValueOnce(old.promise).mockResolvedValueOnce({ items: [{ id: 9 }], total: 1 })
    const { list, stop } = setup({ fetchPage, keyOf: row => row.id })
    const first = list.reset()
    await Promise.resolve()
    await list.reset()
    old.resolve({ items: [{ id: 1 }], total: 1 })
    await first
    expect(list.items.value).toEqual([{ id: 9 }])
    expect(fetchPage.mock.calls[0][1].aborted).toBe(true)
    stop()
  })

  it('keeps rows on append failure and retries the same page explicitly', async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce({ items: [{ id: 1 }], total: 2 })
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ items: [{ id: 2 }], total: 2 })
    const { list, stop } = setup({ fetchPage, keyOf: row => row.id, pageSize: 1 })
    await list.reset()
    await list.loadMore()
    expect(list.items.value).toEqual([{ id: 1 }])
    expect(list.page.value).toBe(1)
    await list.loadMore()
    expect(fetchPage).toHaveBeenCalledTimes(2)
    await list.retry()
    expect(fetchPage.mock.calls.map(call => call[0])).toEqual([1, 2, 2])
    expect(list.error.value).toBeNull()
    expect(list.items.value).toHaveLength(2)
    stop()
  })

  it('refreshes loaded pages atomically and retains them on failure', async () => {
    let version = 'old'
    const second = deferred<InfinitePage<Row>>()
    const fetchPage = vi.fn(async (page: number) => {
      if (version === 'refresh' && page === 2) return second.promise
      return { items: [{ id: page, value: version }], total: 3 }
    })
    const { list, stop } = setup({ fetchPage, keyOf: row => row.id, pageSize: 1 })
    await list.reset()
    await list.loadMore()
    version = 'refresh'
    const refresh = list.refresh()
    await Promise.resolve()
    expect(list.items.value.every(row => row.value === 'old')).toBe(true)
    second.reject(new Error('failed second page'))
    await refresh
    expect(list.items.value.every(row => row.value === 'old')).toBe(true)
    expect(list.page.value).toBe(2)
    stop()
  })

  it('does not publish after clear or scope disposal', async () => {
    const request = deferred<InfinitePage<Row>>()
    const { list, stop } = setup({ fetchPage: () => request.promise })
    const pending = list.reset()
    list.clear()
    request.resolve({ items: [{ id: 1 }] })
    await pending
    expect(list.items.value).toEqual([])
    expect(list.loading.value).toBe(false)
    stop()
  })

  it('uses short-page exhaustion without total and preserves identityless duplicates', async () => {
    const { list, stop } = setup({
      fetchPage: async page => ({ items: page === 1 ? [{ id: 1 }, { id: 1 }] : [] }),
      pageSize: 2
    })
    await list.reset()
    expect(list.items.value).toHaveLength(2)
    await list.loadMore()
    expect(list.hasMore.value).toBe(false)
    stop()
  })

  it('releases its pending guard when a fetch callback throws synchronously', async () => {
    const fetchPage = vi.fn()
      .mockImplementationOnce(() => { throw new Error('sync failure') })
      .mockResolvedValueOnce({ items: [{ id: 1 }], total: 2 })
      .mockResolvedValueOnce({ items: [{ id: 2 }], total: 2 })
    const { list, stop } = setup({ fetchPage, keyOf: row => row.id, pageSize: 1 })
    await list.reset()
    expect(list.error.value).toBeInstanceOf(Error)
    await list.retry()
    await list.loadMore()
    expect(list.items.value).toHaveLength(2)
    expect(fetchPage).toHaveBeenCalledTimes(3)
    stop()
  })

  it('lets a mutation refresh supersede an append without publishing its stale rows', async () => {
    const append = deferred<InfinitePage<Row>>()
    const fetchPage = vi.fn()
      .mockResolvedValueOnce({ items: [{ id: 1, value: 'old' }], total: 3 })
      .mockReturnValueOnce(append.promise)
      .mockResolvedValueOnce({ items: [{ id: 1, value: 'updated' }], total: 3 })
    const { list, stop } = setup({ fetchPage, keyOf: row => row.id, pageSize: 1 })
    await list.reset()
    const loadingNext = list.loadMore()
    await Promise.resolve()
    await list.refresh()
    append.resolve({ items: [{ id: 2 }], total: 3 })
    await loadingNext
    expect(list.items.value).toEqual([{ id: 1, value: 'updated' }])
    expect(list.page.value).toBe(1)
    expect(list.loadingMore.value).toBe(false)
    stop()
  })
})
