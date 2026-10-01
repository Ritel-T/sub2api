import type { BalanceSubscriptionRequest, SubscriptionPlan } from '@/types/payment'

export interface PendingBalancePurchase {
  userId: number
  plan: SubscriptionPlan
  request: BalanceSubscriptionRequest
}

const key = (userId: number) => `rynexai_balance_subscription_v1:${userId}`

export function newBalancePurchase(userId: number, plan: SubscriptionPlan, nonce = crypto.randomUUID()): PendingBalancePurchase {
  return {
    userId,
    plan: { ...plan, features: [...plan.features] },
    request: {
      expected_user_id: userId, plan_id: plan.id, purchase_nonce: nonce, expected_price: plan.price,
      expected_currency: 'CNY', expected_group_id: plan.group_id,
      expected_validity_days: plan.validity_days, expected_validity_unit: plan.validity_unit,
    },
  }
}

// Recovery data is only a frozen request to retry explicitly. It never grants
// access or proves payment; the authenticated server validates every field.
export function saveBalancePurchase(pending: PendingBalancePurchase): boolean {
  try {
    const frozen = JSON.stringify(pending)
    sessionStorage.setItem(key(pending.userId), frozen)
    return sessionStorage.getItem(key(pending.userId)) === frozen
  } catch { return false }
}

export function clearBalancePurchase(userId: number) {
  try { sessionStorage.removeItem(key(userId)) } catch { /* Storage may be unavailable. */ }
}

export function readBalancePurchase(userId: number): PendingBalancePurchase | null {
  try {
    const raw = sessionStorage.getItem(key(userId))
    if (!raw || raw.length > 16384) return null
    const p = JSON.parse(raw) as PendingBalancePurchase
    const r = p.request, plan = p.plan
    if (p.userId !== userId || userId <= 0 || !r || r.expected_user_id !== userId || !plan || !Number.isSafeInteger(r.plan_id) || r.plan_id <= 0 ||
      !Number.isSafeInteger(r.expected_group_id) || r.expected_group_id <= 0 || !Number.isFinite(r.expected_price) || r.expected_price <= 0 ||
      r.expected_price > 1e9 || r.expected_currency !== 'CNY' || !/^[a-zA-Z0-9_-]{24,64}$/.test(r.purchase_nonce) ||
      !Number.isInteger(r.expected_validity_days) || r.expected_validity_days <= 0 ||
      !['day', 'days', 'week', 'weeks', 'month', 'months'].includes(r.expected_validity_unit) ||
      plan.id !== r.plan_id || plan.group_id !== r.expected_group_id || plan.price !== r.expected_price ||
      plan.currency !== 'CNY' || plan.validity_days !== r.expected_validity_days || plan.validity_unit !== r.expected_validity_unit ||
      typeof plan.name !== 'string' || plan.name.length > 100 || !Array.isArray(plan.features) ||
      plan.features.some(f => typeof f !== 'string')) return null
    return p
  } catch { return null }
}
