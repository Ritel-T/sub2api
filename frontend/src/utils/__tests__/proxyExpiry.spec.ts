// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { daysUntil, proxyExpiryBadgeClass, proxyExpiryLabelKey } from '../proxyExpiry'

// 固定「现在」,按天数构造确定输入:isoInDays(n) 距今正好 n 天
const NOW = new Date('2026-06-02T00:00:00Z')
const isoInDays = (n: number): string => new Date(NOW.getTime() + n * 86400000).toISOString()

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
})
afterEach(() => {
  vi.useRealTimers()
})

describe('daysUntil', () => {
  it('返回距今整天数', () => {
    expect(daysUntil(isoInDays(10))).toBe(10)
    expect(daysUntil(isoInDays(-3))).toBe(-3)
  })
})

describe('proxyExpiryBadgeClass', () => {
  it('status=expired → danger', () => {
    expect(proxyExpiryBadgeClass(isoInDays(30), 'expired')).toBe('badge badge-danger')
  })
  it('≤3 天 → danger（含边界 3）', () => {
    expect(proxyExpiryBadgeClass(isoInDays(2), 'active')).toBe('badge badge-danger')
    expect(proxyExpiryBadgeClass(isoInDays(3), 'active')).toBe('badge badge-danger')
  })
  it('4–7 天 → warning（含边界 7）', () => {
    expect(proxyExpiryBadgeClass(isoInDays(5), 'active')).toBe('badge badge-warning')
    expect(proxyExpiryBadgeClass(isoInDays(7), 'active')).toBe('badge badge-warning')
  })
  it('>7 天 → gray', () => {
    expect(proxyExpiryBadgeClass(isoInDays(30), 'active')).toBe('text-gray-500')
  })
})

describe('proxyExpiryLabelKey', () => {
  it('status=expired → expired key', () => {
    expect(proxyExpiryLabelKey(isoInDays(30), 'expired')).toEqual({ key: 'admin.proxies.expired' })
  })
  it('已逾期(d<0) → overdueDays', () => {
    expect(proxyExpiryLabelKey(isoInDays(-3), 'active')).toEqual({
      key: 'admin.proxies.overdueDays',
      params: { days: 3 },
    })
  })
  it('≤7 天 → expiringInDays', () => {
    expect(proxyExpiryLabelKey(isoInDays(5), 'active')).toEqual({
      key: 'admin.proxies.expiringInDays',
      params: { days: 5 },
    })
  })
  it('>7 天 → remainingDays', () => {
    expect(proxyExpiryLabelKey(isoInDays(30), 'active')).toEqual({
      key: 'admin.proxies.remainingDays',
      params: { days: 30 },
    })
  })
})

describe('proxy expiry deadline regressions', () => {
  const now = new Date('2026-09-18T12:00:00Z')
  const atOffset = (ms: number) => new Date(now.getTime() + ms).toISOString()

  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
  })
  afterEach(() => vi.useRealTimers())

  describe('proxy expiry at the deadline', () => {
    it.each([0, -1, -60 * 60 * 1000, -24 * 60 * 60 * 1000 + 1])(
      'shows expired at offset %i before the backend status changes', offset => {
        expect(proxyExpiryLabelKey(atOffset(offset), 'active')).toEqual({ key: 'admin.proxies.expired' })
      }
    )

    it('keeps a future deadline in the expiring state', () => {
      expect(proxyExpiryLabelKey(atOffset(1), 'active')).toEqual({
        key: 'admin.proxies.expiringInDays', params: { days: 1 }
      })
    })

    it('preserves the overdue-day count after a full day', () => {
      expect(proxyExpiryLabelKey(atOffset(-24 * 60 * 60 * 1000), 'active')).toEqual({
        key: 'admin.proxies.overdueDays', params: { days: 1 }
      })
    })
  })
})
