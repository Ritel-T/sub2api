import { describe, expect, it } from 'vitest'
import { browserTimezone, displayOrderNumber, formatAmount, formatMoney, formatTimestamp } from '../consoleDisplay'
describe('console display contract', () => {
  it('only removes the historic display prefix', () => {
    const stored = 'sub2_20261031_abc'
    expect(displayOrderNumber(stored)).toBe('20261031_abc')
    expect(stored).toBe('sub2_20261031_abc')
    expect(displayOrderNumber('x_sub2_abc')).toBe('x_sub2_abc')
  })
  it('formats monetary amounts without changing currency', () => {
    expect(formatMoney(12345.678)).toBe('$12,345.68')
    expect(formatMoney(12345.678, 'CNY')).toBe('¥12,345.68')
    expect(formatMoney(12345.678, 'GBP')).toBe('£12,345.68')
    expect(formatAmount(0.123456, 4)).toBe('0.1235')
  })
  it('uses browser-local calendar fields and a discoverable timezone', () => {
    const date = new Date(2026, 9, 31, 23, 4, 59)
    expect(formatTimestamp(date)).toBe('2026-10-31 23:04')
    expect(formatTimestamp('invalid')).toBe('')
    expect(browserTimezone()).toBeTruthy()
  })
})
