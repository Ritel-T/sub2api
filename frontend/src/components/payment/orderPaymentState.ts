import type { PaymentOrder } from '@/types/payment'

// Dates preserve paid orders whose fulfillment subsequently failed. Statuses cover old
// admin responses that omitted the timestamps; amounts never establish payment or platform.
export function hasOrderPayment(order: PaymentOrder): boolean {
  return !!(order.paid_at || order.completed_at) || [
    'PAID', 'RECHARGING', 'COMPLETED', 'REFUND_REQUESTED', 'REFUNDING',
    'REFUND_PENDING', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REFUND_FAILED',
  ].includes(order.status)
}

// Payment confirmation binds the final amount before fulfillment credits the balance.
export function hasOrderCredit(order: PaymentOrder): boolean {
  return !!order.completed_at || [
    'COMPLETED', 'REFUND_REQUESTED', 'REFUNDING', 'REFUND_PENDING',
    'PARTIALLY_REFUNDED', 'REFUNDED', 'REFUND_FAILED',
  ].includes(order.status)
}

