import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import type { UsageQueryParams } from '@/types'

export const errorCategories = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']
const first = (v: unknown): string => typeof v === 'string' ? v : ''
const positive = (v: unknown): number | undefined => /^\d+$/.test(first(v)) && Number.isSafeInteger(Number(v)) && Number(v) > 0 ? Number(v) : undefined
const date = (v: unknown): string => {
  const s = first(v)
  if (!/^\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}(?::\d{2}(?:\.\d{1,3})?)?(?:Z|[+-]\d{2}:\d{2})?)?$/.test(s)) return ''
  const parsed = new Date(s)
  return Number.isFinite(parsed.getTime()) && new Date(`${s.slice(0,10)}T00:00:00Z`).toISOString().slice(0,10) === s.slice(0,10) ? s : ''
}
export function parseUsageLocation(q: LocationQuery, defaults: {start: string; end: string; pageSize: number}) {
  const start = date(q.start_date), end = date(q.end_date)
  const validRange = !!start && !!end && new Date(start).getTime() <= new Date(end).getTime()
  const tab = q.view === 'errors' || q.tab === 'errors' || q.status === 'failed' || q.status === 'errors' ? 'errors' : q.view === 'traffic' || q.tab === 'traffic' ? 'traffic' : 'usage'
  const key = positive(q.api_key_id) ?? positive(q.key)
  const model = first(q.model).trim().slice(0, 256)
  const params: UsageQueryParams = {
    api_key_id: key, model: model || undefined, group_id: positive(q.group_id),
    request_type: ['ws_v2','live','stream','sync'].includes(first(q.request_type)) ? first(q.request_type) as UsageQueryParams['request_type'] : undefined,
    native_compaction_v2: q.native_compaction_v2 === 'true' ? true : q.native_compaction_v2 === 'false' ? false : null,
    billing_type: q.billing_type === '0' ? 0 : q.billing_type === '1' ? 1 : null,
    billing_mode: ['token','per_request','image','video'].includes(first(q.billing_mode)) ? first(q.billing_mode) : null,
  }
  return {
    usageId: positive(q.usage_id),
    view: q.view === 'analysis' ? 'analysis' : 'records', tab,
    start: validRange ? start : defaults.start, end: validRange ? end : defaults.end, customRange: validRange,
    page: positive(q.page) ?? 1,
    pageSize: [10,20,50,100].includes(positive(q.page_size) ?? 0) ? Number(q.page_size) : defaults.pageSize,
    granularity: q.granularity === 'day' ? 'day' as const : q.granularity === 'hour' ? 'hour' as const : undefined,
    sortBy: (tab === 'errors' ? ['created_at','model','status_code'] : ['created_at','model','actual_cost','total_tokens','duration_ms']).includes(first(q.sort_by)) ? first(q.sort_by) : 'created_at',
    sortOrder: q.sort_order === 'asc' ? 'asc' as const : 'desc' as const,
    category: errorCategories.includes(first(q.category)) ? first(q.category) : '',
    statusCode: positive(q.status_code) && Number(q.status_code) >= 400 && Number(q.status_code) <= 599 ? Number(q.status_code) : null,
    filters: params,
  }
}
export function usageLocationQuery(state: ReturnType<typeof parseUsageLocation>): LocationQueryRaw {
  const q: LocationQueryRaw = {
    view: state.view, tab: state.tab, usage_id: state.usageId, start_date: state.start, end_date: state.end,
    page: state.page, page_size: state.pageSize, granularity: state.granularity,
    sort_by: state.sortBy, sort_order: state.sortOrder,
    api_key_id: state.filters.api_key_id, model: state.filters.model,
  }
  if (state.tab === 'errors') {
    if (state.category) q.category = state.category
    if (state.statusCode) q.status_code = state.statusCode
  } else {
    for (const k of ['group_id','request_type','native_compaction_v2','billing_type','billing_mode'] as const) {
      const v = state.filters[k]
      if (v != null) q[k] = String(v)
    }
  }
  return q
}
