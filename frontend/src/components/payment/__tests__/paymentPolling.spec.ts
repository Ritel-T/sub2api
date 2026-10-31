import { afterEach, describe, expect, it, vi } from 'vitest'
import { isGelvPaymentType, onPaymentPageReturn, paymentPollInterval } from '../paymentPolling'

describe('persisted payment polling policy', () => {
  afterEach(() => vi.restoreAllMocks())

  it('uses two seconds only for the exact Gelv payment types', () => {
    for (const type of ['gelv', 'gelv_alipay']) {
      expect(isGelvPaymentType(type)).toBe(true)
      expect(paymentPollInterval(type, 3000)).toBe(2000)
      expect(paymentPollInterval(type, 5000)).toBe(2000)
    }
    for (const type of ['alipay', 'wxpay', 'squarespace', 'custom_gelv', undefined]) {
      expect(isGelvPaymentType(type)).toBe(false)
      expect(paymentPollInterval(type, 3000)).toBe(3000)
      expect(paymentPollInterval(type, 2000)).toBe(2000)
    }
  })

  it('ignores hidden-page events and removes both return listeners on disposal', () => {
    const refresh = vi.fn()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    const dispose = onPaymentPageReturn(refresh)
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).not.toHaveBeenCalled()
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new Event('focus'))
    expect(refresh).toHaveBeenCalledTimes(2)
    dispose()
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).toHaveBeenCalledTimes(2)
  })
})
