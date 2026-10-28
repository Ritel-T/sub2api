import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import type { AdminUsageQueryParams } from '@/api/admin/usage'
import { objectQueryId } from './objectContext'

export type UsagePage = 'analysis' | 'records'
export type UsageDetailTab = 'analysis' | 'usage' | 'errors' | 'connections' | 'ranking' | 'traffic'
const recordTabs = ['usage', 'errors', 'connections', 'traffic']
const boolKeys = ['stream', 'native_compaction_v2', 'upstream_model_mismatch'] as const
const idKeys = ['user_id', 'api_key_id', 'account_id', 'group_id'] as const
const stringKeys = ['model', 'request_id', 'billing_mode', 'error_phase', 'error_category', 'timezone'] as const
const requestTypes = ['unknown', 'sync', 'stream', 'ws_v2', 'cyber', 'live']
const presets = ['today', 'yesterday', 'last24Hours', '7days', '14days', '30days', 'thisMonth', 'lastMonth']
export const queryValue = (value: LocationQuery[string]): string | undefined => Array.isArray(value)
  ? value.find((item): item is string => typeof item === 'string' && item.length > 0)
  : typeof value === 'string' && value.length ? value : undefined
const optionalBool = (value: LocationQuery[string]) => {
  const v = queryValue(value)
  return v === 'true' || v === '1' ? true : v === 'false' || v === '0' ? false : undefined
}
export function parseAdminUsageLocation(query: LocationQuery, defaults: { start: string; end: string }, observer = false) {
  const legacy = queryValue(query.view)
  const tabQuery = queryValue(query.tab) ?? (legacy && [...recordTabs, 'ranking'].includes(legacy) ? legacy : undefined)
  const page: UsagePage = legacy === 'analysis' || tabQuery === 'ranking' ? 'analysis' : 'records'
  let tab: UsageDetailTab = page === 'analysis' ? tabQuery === 'ranking' ? 'ranking' : 'analysis'
    : recordTabs.includes(tabQuery ?? '') ? tabQuery as UsageDetailTab : 'usage'
  if (observer && ['connections', 'ranking', 'traffic'].includes(tab)) tab = page === 'analysis' ? 'analysis' : 'usage'
  const queryStart = queryValue(query.start_date)
  const queryEnd = queryValue(query.end_date)
  const calendarBoundary = (queryStart && !queryStart.includes('T')) || (queryEnd && !queryEnd.includes('T'))
  const start = queryStart ?? (calendarBoundary ? defaults.start.slice(0, 10) : defaults.start)
  const end = queryEnd ?? (calendarBoundary ? defaults.end.slice(0, 10) : defaults.end)
  const preset = queryValue(query.preset)
  const filters: AdminUsageQueryParams = { start_date: start, end_date: end }
  for (const key of idKeys) filters[key] = observer && ['user_id', 'account_id'].includes(key) ? undefined : objectQueryId(query[key])
  for (const key of boolKeys) filters[key] = optionalBool(query[key])
  for (const key of stringKeys) filters[key] = queryValue(query[key])
  const type = queryValue(query.request_type)
  if (type && requestTypes.includes(type)) filters.request_type = type as AdminUsageQueryParams['request_type']
  const billing = queryValue(query.billing_type)
  if (billing === '0' || billing === '1') filters.billing_type = Number(billing)
  const status = objectQueryId(query.status_code)
  if (status && status >= 100 && status <= 599) filters.status_code = status
  const sortOrder = queryValue(query.sort_order) === 'asc' ? 'asc' as const : 'desc' as const
  return { page, tab, filters, start, end,
    preset: preset && presets.includes(preset) ? preset : queryStart || queryEnd ? null : 'last24Hours',
    granularity: queryValue(query.granularity) === 'day' ? 'day' as const : queryValue(query.granularity) === 'hour' ? 'hour' as const : undefined,
    sortBy: queryValue(query.sort_by) ?? 'created_at', sortOrder,
  }
}
export function adminUsageLocationQuery(state: {
  page: UsagePage; tab: UsageDetailTab; filters: AdminUsageQueryParams; start: string; end: string;
  preset: string | null; granularity: 'day' | 'hour'; sortBy: string; sortOrder: 'asc' | 'desc'
}, current: LocationQuery = {}): LocationQueryRaw {
  const query: LocationQueryRaw = { ...current, view: state.page, tab: state.tab,
    start_date: state.start, end_date: state.end, preset: state.preset ?? undefined,
    granularity: state.granularity, sort_by: state.sortBy, sort_order: state.sortOrder }
  for (const key of [...idKeys, ...boolKeys, ...stringKeys, 'request_type', 'billing_type', 'status_code'] as const) {
    const value = state.filters[key]
    query[key] = value == null || value === '' ? undefined : String(value)
  }
  return query
}
