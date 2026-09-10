import { describe, expect, it } from 'vitest'
import { formatLocalMinute, getDatePresetRange, parseDateBoundary, parseLocalMinute } from '../dateRange'

describe('minute ranges', () => {
  it('uses identical exact 24-hour windows within a minute', () => {
    const first = getDatePresetRange('last24Hours', new Date('2026-09-10T05:12:01.123Z'))!
    const second = getDatePresetRange('last24Hours', new Date('2026-09-10T05:12:59.999Z'))!
    expect(first).toEqual(second)
    expect(first.end).toBe('2026-09-10T05:12:00.000Z')
    expect(new Date(first.end).getTime() - new Date(first.start).getTime()).toBe(86400000)
  })
  it('keeps calendar-day boundaries and advances presets across midnight', () => {
    expect(getDatePresetRange('today', new Date(2026, 8, 10, 23, 59))).toEqual({ start: '2026-09-10', end: '2026-09-10' })
    expect(getDatePresetRange('today', new Date(2026, 8, 11, 0, 1))).toEqual({ start: '2026-09-11', end: '2026-09-11' })
    expect(formatLocalMinute(parseDateBoundary('2026-09-10', true))).toBe('2026-09-11T00:00')
    expect(getDatePresetRange('custom')).toBeNull()
  })
  it('rejects invalid local minutes without silently normalizing', () => {
    expect(parseLocalMinute('2026-02-30T10:00')).toBeNull()
    expect(parseLocalMinute('2026-09-10T10:00:30')).toBeNull()
    expect(parseLocalMinute('')).toBeNull()
    expect(formatLocalMinute(parseLocalMinute('2026-09-10T14:37')!)).toBe('2026-09-10T14:37')
  })
})
