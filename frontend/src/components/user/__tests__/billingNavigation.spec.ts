import { describe, expect, it } from 'vitest'
import { billingDestinations } from '../billingNavigation'

describe('billing navigation permissions', () => {
  it('keeps common billing tasks together with opt-out defaults and opt-in invitations', () => {
    expect(billingDestinations({})).toEqual(['purchase', 'subscriptions', 'orders', 'redeem'])
  })
  it('hides billing destinations in simple mode even for a merchant tester', () => {
    expect(billingDestinations({ simpleMode: true, merchantTestAccess: true })).toEqual([])
  })
  it('respects payment and subscription switches independently', () => {
    expect(billingDestinations({ settings: { payment_enabled: false, subscription_enabled: false } })).toEqual(['redeem'])
    expect(billingDestinations({ settings: { payment_enabled: false, subscription_enabled: true } })).toEqual(['subscriptions', 'redeem'])
    expect(billingDestinations({ settings: { subscription_enabled: false, payment_balance_disabled: false } })).toEqual(['purchase', 'orders', 'redeem'])
    expect(billingDestinations({ settings: { payment_balance_disabled: true } })).toEqual(['purchase', 'subscriptions', 'orders', 'redeem'])
  })
  it('only restores payment destinations for protected merchant access', () => {
    expect(billingDestinations({ settings: { payment_enabled: false, subscription_enabled: false, affiliate_enabled: true }, merchantTestAccess: true })).toEqual(['purchase', 'orders', 'redeem', 'affiliate'])
  })
})
