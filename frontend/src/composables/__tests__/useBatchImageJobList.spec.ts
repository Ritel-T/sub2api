// @vitest-environment node
import { effectScope } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { useBatchImageJobList } from '../useBatchImageJobList'
import type { ApiKey } from '@/types'
import type { BatchImageJob } from '@/api/batchImage'
const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/batchImage', () => ({ listBatchImageJobs: list }))
const keys = [{ id: 1, key: 'test-one' }, { id: 2, key: 'test-two' }] as ApiKey[]
function setup(sourceKeys = keys) {
  const scope = effectScope()
  const filters = { status: '' }
  const state = scope.run(() => useBatchImageJobList({
    getKeys: () => sourceKeys,
    getFilters: () => filters,
    mapJob: (job: BatchImageJob, key: ApiKey) => ({ id: job.id, keyId: key.id }),
  }))!
  return { scope, state, filters }
}
describe('batch image job list', () => {
  it('loads nine sources in batches of at most four without dropping their rows', async () => {
    const sourceKeys = Array.from({ length: 9 }, (_, index) => ({ id: index + 1, key: `source-${index + 1}` })) as ApiKey[]
    let active = 0
    let maximum = 0
    const releases: Array<() => void> = []
    list.mockReset().mockImplementation((key: string) => {
      active++
      maximum = Math.max(maximum, active)
      return new Promise(resolve => releases.push(() => {
        active--
        resolve({ data: Array.from({ length: 50 }, (_, i) => ({ id: `${key}-${i}` })), has_more: false })
      }))
    })
    const { scope, state } = setup(sourceKeys)
    const pending = state.reset()
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(4)
    releases.splice(0).forEach(release => release())
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(8)
    releases.splice(0).forEach(release => release())
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(9)
    releases.splice(0).forEach(release => release())
    await pending
    expect(maximum).toBe(4)
    expect(state.items.value).toHaveLength(450)
    expect(new Set(state.items.value.map(row => row.id)).size).toBe(450)
    expect(list.mock.calls.every(call => call[1].limit === 50)).toBe(true)
    scope.stop()
  })
  it('starts no further source batch after cancellation', async () => {
    const sourceKeys = Array.from({ length: 9 }, (_, index) => ({ id: index + 1, key: `source-${index + 1}` })) as ApiKey[]
    const releases: Array<() => void> = []
    list.mockReset().mockImplementation(() => new Promise(resolve => releases.push(() => resolve({ data: [], has_more: true }))))
    const { scope, state } = setup(sourceKeys)
    const pending = state.reset()
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(4)
    state.cancel()
    releases.forEach(release => release())
    await pending
    expect(list).toHaveBeenCalledTimes(4)
    expect(state.items.value).toEqual([])
    scope.stop()
  })
  it('keeps every source row and advances only the remaining source cursor', async () => {
    list.mockReset().mockImplementation((key: string, options: { cursor: string }) => Promise.resolve({
      data: Array.from({ length: key === 'test-two' ? 2 : options.cursor === '0' ? 50 : 1 }, (_, i) => ({ id: `${key}-${Number(options.cursor) + i}` })),
      has_more: key === 'test-one' && options.cursor === '0',
    }))
    const { scope, state } = setup()
    await state.reset()
    expect(state.items.value).toHaveLength(52)
    await state.loadMore()
    expect(state.items.value).toHaveLength(53)
    expect(list).toHaveBeenCalledTimes(3)
    expect(list.mock.calls[2][1]).toMatchObject({ cursor: '50', limit: 50 })
    expect(state.hasMore.value).toBe(false)
    scope.stop()
  })
  it('does not advance any source cursor when another source fails', async () => {
    list.mockReset().mockResolvedValue({ data: [{ id: 'first' }], has_more: true })
    const { scope, state } = setup()
    await state.reset()
    list.mockResolvedValueOnce({ data: [{ id: 'next-one' }], has_more: false }).mockRejectedValueOnce(new Error('offline'))
    await state.loadMore()
    expect(state.items.value.map(row => row.id)).toEqual(['first'])
    list.mockResolvedValue({ data: [{ id: 'next' }], has_more: false })
    await state.retry()
    expect(list.mock.calls.slice(-2).map(call => call[1].cursor)).toEqual(['1', '1'])
    expect(state.items.value.map(row => row.id)).toEqual(['first', 'next'])
    scope.stop()
  })
  it('cancels an old append before new filters and ignores its late cursor results', async () => {
    list.mockReset().mockResolvedValue({ data: [{ id: 'old' }], has_more: true })
    const { scope, state, filters } = setup()
    await state.reset()
    let resolve!: (value: unknown) => void
    list.mockReturnValueOnce(new Promise(r => { resolve = r })).mockResolvedValueOnce({ data: [], has_more: false })
    const old = state.loadMore()
    await flushPromises()
    const signal = list.mock.calls.at(-1)?.[2] as AbortSignal
    filters.status = 'failed'
    list.mockResolvedValue({ data: [{ id: 'current' }], has_more: false })
    await state.reset()
    resolve({ data: [{ id: 'late' }], has_more: true })
    await old
    expect(signal.aborted).toBe(true)
    expect(state.items.value.map(row => row.id)).toEqual(['current'])
    expect(list.mock.calls.at(-1)?.[1]).toMatchObject({ cursor: '0', status: 'failed' })
    scope.stop()
  })
})
