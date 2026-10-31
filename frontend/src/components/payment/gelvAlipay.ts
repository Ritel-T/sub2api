import type { MethodLimit } from '@/types/payment'

export const GELV_ALIPAY_MAX_CNY = 50

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

/** New requests use cash bounds. Historical quote snapshots may exceed today's cap. */
export function gelvAlipayAmountRange(limit?: MethodLimit, globalMin = 0, globalMax = 0) {
  const positive = (values: (number | undefined)[]) => values.filter((value): value is number =>
    typeof value === 'number' && Number.isFinite(value) && value > 0)
  return {
    min: Math.max(0.01, ...positive([limit?.single_min, globalMin])),
    max: Math.min(GELV_ALIPAY_MAX_CNY, ...positive([limit?.single_max, globalMax])),
  }
}
