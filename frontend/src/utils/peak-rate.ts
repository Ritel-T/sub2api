/** Viewer-local projections of source-zone recurring billing windows. Policy stays unchanged. */
export interface PeakRateFields {
  peak_rate_enabled?: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number
}

export function hasPeakRate(fields?: PeakRateFields | null): boolean {
  return Boolean(fields?.peak_rate_enabled && fields.peak_start && fields.peak_end)
}

export function serverTimezoneLabel(utcOffset?: string | null): string {
  return utcOffset ? `UTC${utcOffset}` : ''
}

type Calendar = { year: number; month: number; day: number; hour: number; minute: number; second: number }
const pad = (n: number) => String(n).padStart(2, '0')

function calendarAt(at: Date, zone: string): Calendar {
  const fixed = /^UTC([+-])(\d{2}):(\d{2})$/.exec(zone)
  if (fixed) {
    const offset = (Number(fixed[2]) * 60 + Number(fixed[3])) * (fixed[1] === '+' ? 1 : -1)
    const shifted = new Date(at.getTime() + offset * 60000)
    return { year: shifted.getUTCFullYear(), month: shifted.getUTCMonth() + 1, day: shifted.getUTCDate(), hour: shifted.getUTCHours(), minute: shifted.getUTCMinutes(), second: shifted.getUTCSeconds() }
  }
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(at)
  const part = (name: Intl.DateTimeFormatPartTypes) => Number(parts.find(p => p.type === name)?.value)
  return { year: part('year'), month: part('month'), day: part('day'), hour: part('hour'), minute: part('minute'), second: part('second') }
}
const calendarEpoch = (p: Calendar) => Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second)
const calendarLabel = (p: Calendar) => `${p.year}-${pad(p.month)}-${pad(p.day)} ${pad(p.hour)}:${pad(p.minute)}${p.second ? ':' + pad(p.second) : ''}`

/** Today's source-calendar occurrence, with explicit viewer dates to disclose day/weekday shifts.
 * A nonexistent DST wall time cannot be projected: retain the source policy label instead.
 */
export function formatLocalTimeWindow(start: string, end: string, sourceZone?: string, now = new Date(), viewerZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'): string {
  const source = `${start}–${end}${sourceZone ? ` (${sourceZone})` : ''}`
  if (!sourceZone) return source
  const clock = (value: string) => /^(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d)?$/.test(value) || /^24:00(?::00)?$/.test(value)
  if (!clock(start) || !clock(end)) return source
  try {
    const today = calendarAt(now, sourceZone)
    const midnight = Date.UTC(today.year, today.month - 1, today.day)
    const seconds = (value: string) => { const [h = 0, m = 0, s = 0] = value.split(':').map(Number); return h * 3600 + m * 60 + s }
    const startSeconds = seconds(start), endSeconds = seconds(end)
    const resolve = (wall: number) => {
      let instant = wall
      for (let i = 0; i < 4; i++) {
        const difference = wall - calendarEpoch(calendarAt(new Date(instant), sourceZone))
        if (difference === 0) {
          // A repeated DST clock can represent two separate billing intervals.
          // Keep the source schedule rather than present one arbitrary occurrence.
          for (const nearby of [instant - 86400000, instant + 86400000]) {
            const offset = calendarEpoch(calendarAt(new Date(nearby), sourceZone)) - nearby
            const alternative = wall - offset
            if (alternative !== instant && calendarEpoch(calendarAt(new Date(alternative), sourceZone)) === wall) throw new Error('Ambiguous source wall time')
          }
          return new Date(instant)
        }
        instant += difference
      }
      throw new Error('Nonexistent source wall time')
    }
    const from = resolve(midnight + startSeconds * 1000)
    const to = resolve(midnight + (endSeconds + (endSeconds <= startSeconds ? 86400 : 0)) * 1000)
    return `${calendarLabel(calendarAt(from, viewerZone))}–${calendarLabel(calendarAt(to, viewerZone))} (${viewerZone})`
  } catch {
    return source
  }
}

/** Original policy remains available alongside its dated viewer-local projection. */
export function sourcePeakRateWindow(fields: PeakRateFields | null | undefined, sourceZone?: string): string {
  if (!hasPeakRate(fields) || !fields) return ''
  return `${fields.peak_start}-${fields.peak_end} ×${fields.peak_rate_multiplier ?? 1}${sourceZone ? ` (${sourceZone})` : ''}`
}

export function formatPeakRateWindow(fields: PeakRateFields | null | undefined, sourceZone?: string): string {
  if (!hasPeakRate(fields) || !fields) return ''
  if (!sourceZone) return sourcePeakRateWindow(fields)
  return `${formatLocalTimeWindow(fields.peak_start!, fields.peak_end!, sourceZone)} ×${fields.peak_rate_multiplier ?? 1}`
}
