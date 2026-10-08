import { describe, expect, it } from 'vitest'
import { borrowingRouteState, gatewayBorrowReasonKey, nativeBorrowQuality } from '../gatewayBorrowStatus'

describe('Gateway borrowing presentation', () => {
  it('maps structured codes and older embedded reasons, including HTTP failures', () => {
    expect(gatewayBorrowReasonKey('target_partially_ready')).toContain('partiallyReady')
    expect(gatewayBorrowReasonKey('warm_budget_exhausted')).toContain('budgetPending')
    expect(gatewayBorrowReasonKey('exploration_cooling')).toContain('explorationCooling')
    expect(gatewayBorrowReasonKey('target_probe_degraded')).toContain('ticketChanged')
    expect(gatewayBorrowReasonKey('source_account_rate_limited')).toContain('rateLimited')
    expect(gatewayBorrowReasonKey('source_account_unavailable')).toContain('accountUnavailable')
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
    expect(borrowingRouteState({ ...row, state: 'expired', expires_at: '2099-01-01T00:00:00Z' }, Date.now())).toBe('expired')
    expect(borrowingRouteState({ ...row, expires_at: 'invalid' }, Date.now())).toBe('expired')
  })
})

const completeAstra = { state: 'degraded', model: 'gpt-6-astra', correct: 1, total: 4 }
describe('Account quality presentation', () => {
  it('uses complete Astra classification for both models and ignores stale independent Sol results', () => {
    const account = { extra: { openai_gateway_borrow_quality_mode: 'astra_controls_sol_v2', quality_candy: completeAstra, quality_candy_models: { 'gpt-6.1-sol': { state: 'healthy' } } } }
    for (const model of ['gpt-6-astra', 'gpt-6.1-sol', 'gpt-6-sol']) expect(nativeBorrowQuality(account, model)).toBe('degraded')
    expect(nativeBorrowQuality(account, 'gpt-6-luna')).toBeUndefined()
    account.extra.quality_candy = { ...completeAstra, state: 'healthy', correct: 3 }
    expect(nativeBorrowQuality(account, 'gpt-6.1-sol')).toBe('healthy')
  })
  it.each([
    { total: 1 }, { correct: 5 }, { correct: 1.5 }, { correct: 3, state: 'degraded' }, { model: 'gpt-6.1-sol' }, { state: 'inconclusive' }
  ])('does not invent classification from invalid or partial evidence %o', changes => {
    const account = { extra: { openai_gateway_borrow_quality_mode: 'astra_controls_sol_v2', quality_candy: { ...completeAstra, ...changes }, quality_candy_models: { 'gpt-6.1-sol': { state: 'degraded' } } } }
    expect(nativeBorrowQuality(account, 'gpt-6.1-sol')).toBeUndefined()
  })
  it('keeps pending unknown and retains legacy display compatibility', () => {
    expect(nativeBorrowQuality({ extra: { openai_gateway_borrow_quality_mode: 'astra_controls_sol_v2', openai_gateway_borrow_quality_pending: true, quality_candy: completeAstra } }, 'gpt-6.1-sol')).toBeUndefined()
    expect(nativeBorrowQuality({ extra: { quality_candy_models: { 'gpt-6-sol': { state: 'degraded' } } } }, 'gpt-6.1-sol')).toBe('degraded')
  })
})
