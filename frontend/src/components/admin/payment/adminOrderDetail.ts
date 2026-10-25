import type { PaymentOrder } from '@/types/payment'
import { currencySymbol } from '@/components/payment/currency'

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

export function getVerifiedSettlement(order: PaymentOrder) {
  const settlement = order.settlement
  if (order.payment_type !== 'gelv' || !hasOrderPayment(order) || settlement?.verified !== true) return null
  const expectedRate = settlement.channel === 'wechat_virtual' ? 100 : settlement.channel === 'apple_iap' ? 1200 : null
  return expectedRate !== null && settlement.fee_rate_bps === expectedRate ? settlement : null
}

export interface OrderAuditLog {
  id: number
  action: string
  detail: string | null
  operator: string | null
  created_at: string
}

function parseDetail(detail: string | null): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(detail || '')
    return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
  } catch { return {} }
}

function amount(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : undefined
}

function cash(value: number, order: PaymentOrder): string {
  return currencySymbol(order.currency || order.retail_quote?.currency) + value.toFixed(2) + (order.currency || order.retail_quote?.currency ? ' ' + (order.currency || order.retail_quote?.currency) : '')
}

const eventLabels: Record<string, string> = {
  ORDER_CREATED: 'payment.admin.detail.events.created', ORDER_PAID: 'payment.admin.detail.events.paid', RECHARGE_SUCCESS: 'payment.admin.detail.events.credited',
  AFFILIATE_REBATE_APPLIED: 'payment.admin.detail.events.rebate', AFFILIATE_REBATE_SKIPPED: 'payment.admin.detail.events.rebateSkipped',
  AFFILIATE_REBATE_FAILED: 'payment.admin.detail.events.rebateFailed', ORDER_RECOVERED: 'payment.admin.detail.events.recovered',
  ORDER_CANCELLED: 'payment.admin.detail.events.cancelled', ORDER_EXPIRED: 'payment.admin.detail.events.expired',
  RECHARGE_RETRY: 'payment.admin.detail.events.retry', FULFILLMENT_FAILED: 'payment.admin.detail.events.failed',
  SUBSCRIPTION_ASSIGNED: 'payment.admin.detail.events.subscriptionAssigned',
  REFUND_REQUESTED: 'payment.admin.detail.events.refundRequested', REFUND_PENDING: 'payment.admin.detail.events.refundPending',
  REFUND_QUERY_PENDING: 'payment.admin.detail.events.refundPending', REFUND_SUCCESS: 'payment.admin.detail.events.refunded', REFUND_FAILED: 'payment.admin.detail.events.refundFailed',
  GELV_CHANNEL_REVIEW_REQUIRED: 'payment.admin.detail.events.channelReview', GELV_REFUND_REVIEW_REQUIRED: 'payment.admin.detail.events.refundReview',
  PAYMENT_UPSTREAM_UNCONFIRMED: 'payment.admin.detail.events.unconfirmed',
}

export function summarizeOrderAuditLog(log: OrderAuditLog, order: PaymentOrder | null) {
  const titleKey = eventLabels[log.action] || 'payment.admin.detail.events.other'
  const data = parseDetail(log.detail)
  let summaryKey = ''
  const params: Record<string, string> = {}
  if (!order) return { titleKey, summaryKey, params }

  if (log.action === 'ORDER_CREATED') {
    const paid = amount(data.payAmount)
    const credit = amount(data.creditedAmount)
    // Creation records the frozen quote. Never present it as settled credit.
    if (paid !== undefined) {
      summaryKey = 'payment.admin.detail.createdPayment'
      params.paid = cash(paid, order)
    }
    if (credit !== undefined && paid !== undefined && order.order_type === 'balance' && !(order.payment_type === 'gelv' && hasOrderPayment(order))) {
      summaryKey = 'payment.admin.detail.createdEstimate'
      params.credit = '$' + credit.toFixed(2)
    }
  } else if (log.action === 'ORDER_PAID') {
    const paid = amount(data.paidAmount) ?? (hasOrderPayment(order) ? amount(order.pay_amount) : undefined)
    if (paid !== undefined) {
      summaryKey = 'payment.admin.detail.paidSummary'
      params.paid = cash(paid, order)
    }
  } else if (log.action === 'RECHARGE_SUCCESS' && order.order_type === 'balance') {
    // Only a final fulfillment event (or its paid order) supplies actual credit.
    const credit = amount(data.creditedAmount) ?? (hasOrderCredit(order) ? amount(order.amount) : undefined)
    if (credit !== undefined) {
      summaryKey = 'payment.admin.detail.creditSummary'
      params.credit = '$' + credit.toFixed(2)
    }
  } else if (log.action === 'AFFILIATE_REBATE_APPLIED') {
    const rebate = amount(data.rebateAmount)
    const base = amount(data.baseAmount)
    if (rebate !== undefined && base !== undefined) {
      summaryKey = 'payment.admin.detail.rebateSummary'
      params.rebate = '$' + rebate.toFixed(2)
      params.base = '$' + base.toFixed(2)
    }
  }
  return { titleKey, summaryKey, params }
}

export function formatAuditDetail(detail: string | null): string {
  try { return JSON.stringify(JSON.parse(detail || ''), null, 2) }
  catch { return detail || '' }
}
