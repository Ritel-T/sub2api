import { describe, expect, it } from 'vitest'
import { parseAdminUsageLocation, adminUsageLocationQuery } from '../adminUsageLocation'
const defaults = { start: '2026-10-27T12:00:00Z', end: '2026-10-28T12:00:00Z' }
describe('admin usage page locations', () => {
  it('roundtrips the full shared filter scope including false and zero', () => {
    const query = { view: 'records', tab: 'usage', start_date: defaults.start, end_date: defaults.end,
      user_id: '12', api_key_id: '13', account_id: '14', group_id: '15', model: 'gpt-6-astra',
      request_type: 'ws_v2', stream: 'false', native_compaction_v2: 'false', billing_type: '0',
      billing_mode: 'token', upstream_model_mismatch: 'false', request_id: 'request-123',
      error_phase: 'upstream', error_category: 'rate_limit', status_code: '429', timezone: 'America/Los_Angeles',
      sort_by: 'actual_cost', sort_order: 'asc', granularity: 'hour' }
    const parsed = parseAdminUsageLocation(query, defaults)
    expect(parsed.filters).toMatchObject({ user_id: 12, api_key_id: 13, account_id: 14, group_id: 15,
      stream: false, native_compaction_v2: false, billing_type: 0, upstream_model_mismatch: false, status_code: 429 })
    const next = adminUsageLocationQuery({ ...parsed, page: 'analysis', tab: 'analysis', granularity: 'hour' }, query)
    expect(next).toMatchObject({ ...query, view: 'analysis', tab: 'analysis' })
    expect(parseAdminUsageLocation(next as typeof query, defaults).filters).toEqual(parsed.filters)
  })
  it.each(['errors', 'connections', 'traffic', 'ranking'])('accepts the legacy %s view and preserves unowned deep-link parameters', tab => {
    const state = parseAdminUsageLocation({ view: tab }, defaults)
    expect(state.tab).toBe(tab)
    expect(state.page).toBe(tab === 'ranking' ? 'analysis' : 'records')
    expect(adminUsageLocationQuery({ ...state, granularity: 'hour' }, { error_id: '7' }).error_id).toBe('7')
  })
  it('rejects malformed IDs and keeps restricted observer modes within own pages', () => {
    const state = parseAdminUsageLocation({ view: 'ranking', user_id: '44', account_id: '55', api_key_id: '2.5', group_id: '-1' }, defaults, true)
    expect(state).toMatchObject({ page: 'analysis', tab: 'analysis' })
    expect(state.filters).toMatchObject({ user_id: undefined, account_id: undefined, api_key_id: undefined, group_id: undefined })
  })
})
