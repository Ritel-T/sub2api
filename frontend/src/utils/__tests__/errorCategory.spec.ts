import { describe, expect, it } from 'vitest'
import { mapErrorCategory } from '../errorCategory'
import { numericRequestTypeKind } from '../errorBadges'

describe('session safety error categories', () => {
  it.each([
    ['cyber_policy', 'cyber'],
    ['cyber_policy_session_blocked', 'session_blocked'],
    ['cyber_session_identity_rejected', 'session_identity'],
  ])('classifies %s independently of a historical Cyber request type', (type, category) => {
    expect(mapErrorCategory('request', type)).toBe(category)
    // Legacy request_type=4 does not identify transport or turn identity errors into Cyber badges.
    expect(numericRequestTypeKind(4, true)).toBeNull()
  })

  it('preserves new WebSocket identity errors as transport WS', () => {
    expect(mapErrorCategory('request', 'cyber_session_identity_rejected')).toBe('session_identity')
    expect(numericRequestTypeKind(3, true)).toBe('ws_v2')
  })
})
