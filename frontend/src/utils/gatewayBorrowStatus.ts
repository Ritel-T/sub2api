import type { Account } from '@/types'
import type { AstraRouteStatus } from '@/api/admin/astraGateway'

export const gatewayBorrowModels = ['gpt-6-astra', 'gpt-6.1-sol'] as const
export function canonicalBorrowModel(model?: string): string {
  return model === 'gpt-6-sol' ? 'gpt-6.1-sol' : model || 'gpt-6-astra'
}

export function requiredBorrowModels(account: Pick<Account, 'platform' | 'type' | 'extra'>): string[] {
  if (account.platform !== 'openai' || account.type !== 'oauth') return []
  const raw = account.extra?.openai_gateway_borrow_models
  if (raw == null) return []
  // The backend fails closed for malformed policy data; do not label it native degradation.
  if (!Array.isArray(raw) || raw.some(model => typeof model !== 'string' || !gatewayBorrowModels.includes(canonicalBorrowModel(model) as typeof gatewayBorrowModels[number]))) return [...gatewayBorrowModels]
  return gatewayBorrowModels.filter(model => raw.some(item => canonicalBorrowModel(item) === model))
}

export function nativeBorrowQuality(account: Pick<Account, 'extra'>, model: string): string | undefined {
  const records = account.extra?.quality_candy_models as Record<string, unknown> | undefined
  const record = records?.[model] ?? (model === 'gpt-6.1-sol' ? records?.['gpt-6-sol'] : undefined)
  if (typeof record !== 'object' || record === null || !('state' in record)) return undefined
  return record.state === 'degraded' || record.state === 'healthy' ? record.state : undefined
}

const reasonKeys: Record<string, string> = {
  source_account_rate_limited: 'rateLimited', source_account_unavailable: 'accountUnavailable',
  warm_budget_exhausted: 'budgetPending', exploration_cooling: 'explorationCooling', target_partially_ready: 'partiallyReady',
  target_account_rate_limited: 'rateLimited', target_account_unavailable: 'accountUnavailable', gateway_borrow_response_model_mismatch: 'modelMismatch',
  target_probe_degraded: 'ticketChanged', target_route_changed: 'ticketChanged',
  target_quality_failed: 'answerFailed', target_quality_degraded: 'answerFailed', answer_mismatch: 'answerFailed',
  target_probe_rate_limited: 'rateLimited', target_quality_rate_limited: 'rateLimited', upstream_rate_limited: 'rateLimited',
  target_probe_auth_failed: 'authFailed', target_quality_auth_failed: 'authFailed', upstream_auth_failed: 'authFailed',
  target_validation_in_progress: 'busy', preparation_in_progress: 'busy', test_already_running: 'busy',
  borrow_route_expired: 'expired', route_expired: 'expired',
  latest_state_unavailable: 'stateUnknown', account_binding_changed: 'changed', group_membership_changed: 'changed',
  borrow_required_route_unavailable: 'notReady', gateway_borrow_request_failed: 'requestFailed', configuration_changed: 'changed', gateway_borrow_testing: 'testing',
  gateway_borrow_route_not_ready: 'notReady', astra_route_not_ready: 'notReady', target_not_verified: 'notReady',
  target_probe_failed: 'verificationFailed', target_quality_probe_failed: 'verificationFailed',
  no_qualified_source_route: 'noSource', source_probe_cooldown: 'noSource',
  gateway_borrow_compact_unavailable: 'compactUnsupported', gateway_borrow_unavailable: 'notReady',
  routing_cookie_missing: 'ticketMissing', routing_cookie_deleted: 'ticketMissing'
}
export function gatewayBorrowReasonKey(code = '', error = ''): string | undefined {
  const key = reasonKeys[code] || Object.entries(reasonKeys).find(([reason]) => error.includes(reason))?.[1]
  if (key) return `admin.astraGateway.testReasons.${key}`
  if (/\b429\b|usage_limit_reached|rate_limit_exceeded/.test(error)) return 'admin.astraGateway.testReasons.rateLimited'
  if (/\b401\b|token_revoked|invalid_token|authentication_error/.test(error)) return 'admin.astraGateway.testReasons.authFailed'
  return undefined
}

export function borrowingRouteState(row: AstraRouteStatus | undefined, now: number): 'ready' | 'expired' | 'waiting' | 'unavailable' {
  if (!row) return 'waiting'
  const expiry = row.expires_at ? Date.parse(row.expires_at) : NaN
  if (row.state === 'ready' && Number.isFinite(expiry) && expiry > now) return 'ready'
  if (row.state === 'ready' || row.state === 'expired' || row.reason === 'route_expired' || row.reason === 'borrow_route_expired') return 'expired'
  return ['waiting', 'preparing', 'validating'].includes(row.state) || row.reason === 'target_validation_in_progress' ? 'waiting' : 'unavailable'
}
