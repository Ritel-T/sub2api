import type { PaymentOrder, RetailQuote } from '@/types/payment'
import type { OrderAuditLog } from '../adminOrderDetail'

export const gelvQuote: RetailQuote = {
  pricing_mode: 'gelv_channel_cny', pricing_basis_currency: 'CNY', base_amount_cny: 8.8,
  credited_amount_usd: 8.8, credit_by_channel: { wechat_virtual: 9.9, apple_iap: 8.8 },
  base_amount_gbp: 0, included_cost_gbp: 0, total_amount_gbp: 0, pay_amount: 10, currency: 'CNY',
  fx: { GBP: 0, USD: 0, CNY: 1 }, fx_source: 'fixed_cny', fx_asof: '2026-10-25T12:00:00Z',
  issued_at: '2026-10-25T12:00:00Z', expires_at: '2026-10-25T12:15:00Z', checkout_reference: 'preview-order-21',
}
export function gelvOrder(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 21, user_id: 1, amount: 9.9, pay_amount: 10, currency: 'CNY', fee_rate: 0,
    payment_type: 'gelv', out_trade_no: 'preview_order21_0123456789abcdefghijklmnopqrstuvwxyz',
    status: 'COMPLETED', order_type: 'balance', refund_amount: 0, retail_quote: gelvQuote,
    created_at: '2026-10-25T12:00:00Z', expires_at: '2026-10-25T12:15:00Z',
    paid_at: '2026-10-25T12:02:00Z', completed_at: '2026-10-25T12:02:01Z',
    ...overrides,
  }
}
export const order21Logs: OrderAuditLog[] = [
  { id: 1, action: 'ORDER_CREATED', detail: JSON.stringify({ bonusAmount: 0, creditedAmount: 8.8, orderType: 'balance', payAmount: 10, paymentAmount: 8.8, paymentSource: 'hosted_redirect', paymentType: 'gelv' }), operator: 'user:1', created_at: '2026-10-25T12:00:00Z' },
  { id: 2, action: 'ORDER_PAID', detail: JSON.stringify({ paidAmount: 10, tradeNo: 'preview-trade' }), operator: 'gelv', created_at: '2026-10-25T12:02:00Z' },
  { id: 3, action: 'AFFILIATE_REBATE_APPLIED', detail: JSON.stringify({ baseAmount: 9.9, rebateAmount: 0.99 }), operator: 'system', created_at: '2026-10-25T12:02:00Z' },
  { id: 4, action: 'RECHARGE_SUCCESS', detail: JSON.stringify({ creditedAmount: 9.9, payAmount: 10, rechargeCode: 'preview-redacted' }), operator: 'system', created_at: '2026-10-25T12:02:01Z' },
]
