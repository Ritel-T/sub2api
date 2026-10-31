import { computed, shallowRef, type InjectionKey } from 'vue'

export const SETTINGS_LEAVE_GUARD: InjectionKey<() => Promise<boolean>> = Symbol('settings-leave-guard')

export function cloneDraft<T>(value: T): T {
  return value === undefined ? value : JSON.parse(JSON.stringify(value))
}
export function sameDraft(a: unknown, b: unknown): boolean { return JSON.stringify(a) === JSON.stringify(b) }

/** In-memory snapshots only. Secrets are never written to browser storage. */
export function useSettingsDrafts() {
  type Source = { read: () => unknown; write: (value: any) => void }
  const sources = new Map<string, Source>()
  const saved = shallowRef<Record<string, unknown>>({})
  function register(id: string, source: Source) { sources.set(id, source) }
  function has(id: string) { return Object.prototype.hasOwnProperty.call(saved.value, id) }
  function baseline<T>(id: string): T { return cloneDraft(saved.value[id]) as T }
  function capture<T>(id: string): T { return cloneDraft(sources.get(id)!.read()) as T }
  function mark(id: string) { saved.value = { ...saved.value, [id]: capture(id) } }
  function dirty(id: string) { return has(id) && !sameDraft(saved.value[id], sources.get(id)!.read()) }
  function discard(id: string) { if (has(id)) sources.get(id)!.write(baseline(id)) }
  function accept(id: string, submitted: unknown, result: unknown) {
    if (sameDraft(sources.get(id)!.read(), submitted)) sources.get(id)!.write(cloneDraft(result))
    saved.value = { ...saved.value, [id]: cloneDraft(result) }
  }
  function rebase(id: string, patch: Record<string, unknown>) {
    if (has(id)) saved.value = { ...saved.value, [id]: { ...baseline<Record<string, unknown>>(id), ...cloneDraft(patch) } }
  }
  const anyDirty = computed(() => Object.keys(saved.value).some(dirty))
  return { register, has, baseline, capture, mark, dirty, discard, accept, rebase, anyDirty }
}
