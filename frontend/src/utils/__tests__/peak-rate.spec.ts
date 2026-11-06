import { describe, expect, it } from 'vitest'
import { formatLocalTimeWindow, formatPeakRateWindow, sourcePeakRateWindow } from '../peak-rate'

describe('viewer-local recurring windows', () => {
  it('anchors today in the source zone and discloses a previous viewer date', () => {
    expect(formatLocalTimeWindow('00:30', '08:30:00', 'Asia/Shanghai', new Date('2026-11-06T02:00:00Z'), 'America/Indianapolis'))
      .toBe('2026-11-05 11:30–2026-11-05 19:30 (America/Indianapolis)')
  })
  it('uses the source IANA DST offset for each endpoint of a crossing window', () => {
    expect(formatLocalTimeWindow('01:00', '04:00', 'America/New_York', new Date('2026-03-08T12:00:00Z'), 'UTC'))
      .toBe('2026-03-08 06:00–2026-03-08 08:00 (UTC)')
  })
  it('discloses an overnight window with both dates including midnight 24:00', () => {
    expect(formatLocalTimeWindow('23:00', '01:00', 'UTC', new Date('2026-11-06T12:00:00Z'), 'UTC'))
      .toBe('2026-11-06 23:00–2026-11-07 01:00 (UTC)')
    expect(formatLocalTimeWindow('18:00', '24:00', 'UTC', new Date('2026-11-06T12:00:00Z'), 'UTC'))
      .toBe('2026-11-06 18:00–2026-11-07 00:00 (UTC)')
  })
  it('accepts an explicitly known fixed server offset, without inventing a zone', () => {
    expect(formatLocalTimeWindow('14:00', '18:00', 'UTC+08:00', new Date('2026-11-06T12:00:00Z'), 'UTC'))
      .toBe('2026-11-06 06:00–2026-11-06 10:00 (UTC)')
    expect(formatLocalTimeWindow('14:00', '18:00')).toBe('14:00–18:00')
    expect(formatLocalTimeWindow('14:00', '18:00', 'Invalid/Zone')).toBe('14:00–18:00 (Invalid/Zone)')
  })
  it('retains the original policy instead of inventing a DST gap instant', () => {
    expect(formatLocalTimeWindow('02:30', '04:00', 'America/New_York', new Date('2026-03-08T12:00:00Z'), 'UTC'))
      .toBe('02:30–04:00 (America/New_York)')
  })
  it('retains the source policy for ambiguous repeated DST clocks', () => {
    expect(formatLocalTimeWindow('01:00', '01:30', 'America/New_York', new Date('2026-11-01T12:00:00Z'), 'UTC'))
      .toBe('01:00–01:30 (America/New_York)')
  })
  it('keeps the original multiplier and supports the legacy missing-settings case', () => {
    const fields = { peak_rate_enabled: true, peak_start: '14:00', peak_end: '18:00', peak_rate_multiplier: 1.5 }
    expect(formatPeakRateWindow(fields)).toBe('14:00-18:00 ×1.5')
    expect(sourcePeakRateWindow(fields, 'Asia/Shanghai')).toBe('14:00-18:00 ×1.5 (Asia/Shanghai)')
    expect(formatPeakRateWindow({ ...fields, peak_rate_enabled: false })).toBe('')
  })
})
