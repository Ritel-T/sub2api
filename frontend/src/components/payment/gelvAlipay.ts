import type { MethodLimit, TopupOption } from '@/types/payment'

export const GELV_CASH_TIERS = [10, 30, 50, 100, 200, 500] as const

/** Match the raw wire format; URL parsing alone silently normalizes unsafe input. */
export function isValidGelvAlipayPayURL(value: unknown): value is string {
  return typeof value === 'string'
    && /^https:\/\/alar1c\.cn\/partner-pay\/#(?:id=[a-f0-9]{24}&token=[a-f0-9]{64}|token=[a-f0-9]{64}&id=[a-f0-9]{24})(?![\s\S])/.test(value)
}

/** Only a direct user click calls this. Never redirect the tracking tab on failure. */
export function openGelvAlipayCheckout(value: unknown): boolean {
  if (!isValidGelvAlipayPayURL(value)) return false
  const link = document.createElement('a')
  link.href = value
  link.target = '_blank'
  link.rel = 'noopener noreferrer'
  link.referrerPolicy = 'no-referrer'
  link.click()
  return true
}

export function isValidAlipayFeeRateBps(value: unknown): value is string {
  return typeof value === 'string' && /^(0|[1-9][0-9]{0,3})(?![\s\S])/.test(value)
}

/** The server supplies the agreed credits; the client never invents a fee or rate. */
export function hasGelvAlipayTiers(limit?: MethodLimit): boolean {
  const options = limit?.topup_options
  return options?.length === GELV_CASH_TIERS.length
    && GELV_CASH_TIERS.every(cash => options.some((option: TopupOption) =>
      option.cash_cny === cash && Number.isFinite(option.credits) && option.credits > 0 && option.credits <= cash))
}
