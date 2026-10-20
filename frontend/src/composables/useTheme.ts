import { readonly, ref } from 'vue'

export type ThemePreference = 'system' | 'light' | 'dark'
export const THEME_STORAGE_KEY = 'ritel-theme'
const preference = ref<ThemePreference>('system')
const resolvedTheme = ref<'light' | 'dark'>('light')
let media: MediaQueryList | undefined
let initialized = false

export function normalizeTheme(value: unknown): ThemePreference {
  return value === 'light' || value === 'dark' ? value : 'system'
}

function applyTheme() {
  resolvedTheme.value = preference.value === 'system' ? (media?.matches ? 'dark' : 'light') : preference.value
  const root = document.documentElement
  root.classList.toggle('dark', resolvedTheme.value === 'dark')
  root.style.colorScheme = resolvedTheme.value
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', resolvedTheme.value === 'dark' ? '#1b1b19' : '#f5f3ee')
}

export function initializeTheme() {
  if (initialized || typeof window === 'undefined') return
  initialized = true
  try { preference.value = normalizeTheme(localStorage.getItem(THEME_STORAGE_KEY)) } catch { preference.value = 'system' }
  media = window.matchMedia?.('(prefers-color-scheme: dark)')
  const onSystemChange = () => { if (preference.value === 'system') applyTheme() }
  if (typeof media?.addEventListener === 'function') media.addEventListener('change', onSystemChange)
  else media?.addListener?.(onSystemChange)
  window.addEventListener('storage', event => {
    if (event.key !== THEME_STORAGE_KEY && event.key !== null) return
    preference.value = normalizeTheme(event.newValue)
    applyTheme()
  })
  applyTheme()
}

export function setTheme(value: ThemePreference) {
  initializeTheme()
  preference.value = normalizeTheme(value)
  try { localStorage.setItem(THEME_STORAGE_KEY, preference.value) } catch { /* Keep this session usable when storage is blocked. */ }
  applyTheme()
}

export function useTheme() {
  initializeTheme()
  return { preference: readonly(preference), resolvedTheme: readonly(resolvedTheme), setTheme }
}
