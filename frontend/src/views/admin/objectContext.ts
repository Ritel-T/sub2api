import type { LocationQueryValue } from 'vue-router'

/** Object identifiers are exact, positive integers; never treat arbitrary search text as an ID. */
export function objectQueryId(value: LocationQueryValue | LocationQueryValue[] | undefined): number | undefined {
  const raw = Array.isArray(value) ? value[0] : value
  if (typeof raw !== 'string' || !/^[1-9]\d*$/.test(raw)) return undefined
  const id = Number(raw)
  return Number.isSafeInteger(id) ? id : undefined
}
