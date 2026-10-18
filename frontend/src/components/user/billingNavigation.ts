import type { PublicSettings } from '@/types'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'

export type BillingDestination = 'purchase' | 'subscriptions' | 'orders' | 'redeem' | 'affiliate'
export interface BillingNavigationContext {
  settings?: Partial<PublicSettings> | null
  simpleMode?: boolean
  merchantTestAccess?: boolean
}

export function billingDestinations(context: BillingNavigationContext): BillingDestination[] {
  if (context.simpleMode) return []
  const settings = context.settings
  const destinations: BillingDestination[] = []
  if (resolveFeatureFlag(settings, FeatureFlags.payment) || context.merchantTestAccess) destinations.push('purchase')
  if (resolveFeatureFlag(settings, FeatureFlags.subscription)) destinations.push('subscriptions')
  if (resolveFeatureFlag(settings, FeatureFlags.payment) || context.merchantTestAccess) destinations.push('orders')
  destinations.push('redeem')
  if (resolveFeatureFlag(settings, FeatureFlags.affiliate)) destinations.push('affiliate')
  return destinations
}

export const billingMessages = {
  en: { billing: { title: 'Billing', purchase: 'Balance & plans', subscriptions: 'Subscriptions', orders: 'Orders', redeem: 'Redeem', affiliate: 'Invitations', resume: 'Return to selected plan', support: 'Contact support', request: 'Request', order: 'Order', issue: 'Please describe the issue.', topUp: 'Top up', plans: 'Plans' } },
  zh: { billing: { title: '账务', purchase: '余额与套餐', subscriptions: '订阅', orders: '订单', redeem: '兑换', affiliate: '邀请', resume: '返回已选套餐', support: '联系支持', request: '请求', order: '订单', issue: '请描述遇到的问题。', topUp: '充值', plans: '套餐' } },
  'zh-Hant-HK': { billing: { title: '帳務', purchase: '餘額與方案', subscriptions: '訂閱', orders: '訂單', redeem: '兌換', affiliate: '邀請', resume: '返回已選方案', support: '聯絡支援', request: '請求', order: '訂單', issue: '請描述遇到的問題。', topUp: '充值', plans: '方案' } },
}
