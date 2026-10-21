import { useInfiniteList } from '@/composables/useInfiniteList'
import { listBatchImageJobs, type BatchImageJob, type BatchImageJobsListOptions } from '@/api/batchImage'
import type { ApiKey } from '@/types'

/** Each API key owns its offset cursor; never truncate the merged results across keys. */
export function useBatchImageJobList<T extends { id: string }>(options: {
  getKeys: () => ApiKey[]
  getFilters: () => BatchImageJobsListOptions
  mapJob: (job: BatchImageJob, key: ApiKey) => T
}) {
  let offsets = new Map<number, number>()
  let finished = new Set<number>()
  return useInfiniteList<T>({
    pageSize: 50,
    keyOf: row => row.id,
    fetchPage: async (page, signal) => {
      const currentOffsets = page === 1 ? new Map<number, number>() : new Map(offsets)
      const currentFinished = page === 1 ? new Set<number>() : new Set(finished)
      const keys = options.getKeys().filter(key => !currentFinished.has(key.id))
      const filters = { ...options.getFilters() }
      const results: Array<{ key: ApiKey; offset: number; result: Awaited<ReturnType<typeof listBatchImageJobs>> }> = []
      for (let start = 0; start < keys.length; start += 4) {
        if (signal.aborted) return { items: [], pages: page }
        const batch = await Promise.allSettled(keys.slice(start, start + 4).map(async key => {
          const offset = currentOffsets.get(key.id) ?? 0
          const result = await listBatchImageJobs(key.key, { ...filters, limit: 50, cursor: String(offset) }, signal)
          return { key, offset, result }
        }))
        if (signal.aborted) return { items: [], pages: page }
        for (const result of batch) {
          if (result.status === 'rejected') throw result.reason
          results.push(result.value)
        }
      }
      const items = results.flatMap(({ key, offset, result }) => {
        currentOffsets.set(key.id, offset + result.data.length)
        if (!result.has_more || result.data.length === 0) currentFinished.add(key.id)
        return result.data.map(job => options.mapJob(job, key))
      })
      if (!signal.aborted) {
        offsets = currentOffsets
        finished = currentFinished
      }
      return { items, pages: results.some(({ key }) => !currentFinished.has(key.id)) ? page + 1 : page }
    },
  })
}
