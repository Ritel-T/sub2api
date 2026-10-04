import { describe, expect, it } from 'vitest'
import { borrowingRouteState, gatewayBorrowReasonKey } from '../gatewayBorrowStatus'

describe('Gateway borrowing presentation', () => {
  it('maps structured codes and older embedded reasons, including HTTP failures', () => {
    expect(gatewayBorrowReasonKey('target_probe_degraded')).toContain('ticketChanged')
    expect(gatewayBorrowReasonKey('target_account_rate_limited')).toContain('rateLimited')
    expect(gatewayBorrowReasonKey('target_account_unavailable')).toContain('accountUnavailable')
    expect(gatewayBorrowReasonKey('gateway_borrow_response_model_mismatch')).toContain('modelMismatch')
    expect(gatewayBorrowReasonKey('', 'astra_route_not_ready: target_quality_failed')).toContain('answerFailed')
    expect(gatewayBorrowReasonKey('', 'HTTP error! status: 429')).toContain('rateLimited')
    expect(gatewayBorrowReasonKey('', 'HTTP error! status: 401')).toContain('authFailed')
    expect(gatewayBorrowReasonKey('latest_state_unavailable')).toContain('stateUnknown')
    expect(gatewayBorrowReasonKey('account_binding_changed')).toContain('changed')
    expect(gatewayBorrowReasonKey('unknown', 'provider failure')).toBeUndefined()
  })
  it('never accepts a ready marker without a valid future expiry', () => {
    const row = { account_id: 34, state: 'ready', reason: 'target_probe_passed', remaining_seconds: 100, active: true }
    expect(borrowingRouteState(row, Date.now())).toBe('expired')
    expect(borrowingRouteState({ ...row, expires_at: 'invalid' }, Date.now())).toBe('expired')
  })
})
