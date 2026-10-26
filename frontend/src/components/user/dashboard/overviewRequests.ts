import type { UsageLog, UserErrorRequest } from '@/types'

export type OverviewRequest =
  | { kind: 'usage'; key: string; record: UsageLog }
  | { kind: 'error'; key: string; record: UserErrorRequest }

/** Each source supplies its newest five. IDs are local to each source. */
export function recentOverviewRequests(data: UsageLog[], errors: UserErrorRequest[] = []): OverviewRequest[] {
  const rows: OverviewRequest[] = [
    ...data.map(record => ({ kind: 'usage' as const, key: `usage:${record.id}`, record })),
    ...errors.map(record => ({ kind: 'error' as const, key: `error:${record.id}`, record }))
  ]
  return rows.sort((a, b) => {
    const time = (value: string) => Number.isFinite(Date.parse(value)) ? Date.parse(value) : 0
    return time(b.record.created_at) - time(a.record.created_at)
      || b.record.id - a.record.id || a.kind.localeCompare(b.kind)
  }).slice(0, 5)
}

/** Same total as UsageTable: cache writes and cache reads are counted once. */
export function overviewTokenTotal(record: UsageLog): number {
  return (record.input_tokens || 0) + (record.output_tokens || 0)
    + (record.cache_creation_tokens || 0) + (record.cache_read_tokens || 0)
}

export function overviewDuration(ms: number | null | undefined): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const seconds = Math.round(ms / 1000)
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}
