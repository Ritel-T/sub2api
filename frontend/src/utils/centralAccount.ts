import { readonly, ref } from 'vue'
import { accountOrigin, centralAccountEnabled } from './accountConfig'

export interface CentralSession {
  user: { uid: string } | null
  csrf: string | null
}
interface Consumer {
  readonly session: CentralSession
  refresh(): Promise<CentralSession>
  logout(): Promise<void>
  stop(): void
}
interface ConsumerModule {
  bootstrapSub2apiConsumer(options: {
    authorizeOrigin: string
    onSession: (session: CentralSession) => void
    onError: (error: unknown) => void
  }): Promise<Consumer>
}

const checking = ref(false)
const unavailable = ref('')
const identityChanged = ref(false)
export const centralChecking = readonly(checking)
export const centralSessionError = readonly(unavailable)
export const centralIdentityChanged = readonly(identityChanged)
let session: CentralSession = { user: null, csrf: null }
let consumer: Consumer | undefined
let sessionHandler: ((session: CentralSession) => Promise<void>) | undefined
let sessionWork: Promise<void> = Promise.resolve()
let initialized = false
let initializeWork: Promise<void> | undefined
let lifecycleInstalled = false
let refreshWork: Promise<void> | undefined
let logoutPending = false
let logoutUncertain = false
let identityVersion = 0
const visibleRecheckIntervalMs = 5 * 60_000
let lastSuccessfulCheck: number | undefined
let recoveryTimer: ReturnType<typeof setTimeout> | undefined

/** Keep locally saved admin test outputs separate when this browser changes account. */
export function consoleOwnedStorageKey(key: string): string {
  return centralAccountEnabled ? key + ':owner:' + (session.user?.uid ?? 'anonymous') : key
}
export function centralSessionVersion(): number { return identityVersion }
export function centralSessionUID(): string | null { return session.user?.uid ?? null }
export function markCentralUnavailable(message?: string): void {
  // A business 401 triggers a read-only recovery check, never request replay.
  if (!message && initialized && !refreshWork && !identityChanged.value) {
    void refreshCentralAccount().catch(() => { /* Persistent failure is shown below. */ })
    return
  }
  unavailable.value = message || '连接暂时不稳定，正在等待恢复。'
  if (!message) scheduleRecovery()
}

function scheduleRecovery(): void {
  if (recoveryTimer || !initialized || identityChanged.value || logoutPending || logoutUncertain) return
  recoveryTimer = setTimeout(() => {
    recoveryTimer = undefined
    if (document.visibilityState === 'visible' && navigator.onLine !== false) {
      void refreshCentralAccount().catch(() => { /* Retry remains bounded and visible-tab only. */ })
    }
  }, 30_000)
}

async function recoverSession(task: () => Promise<void>): Promise<void> {
  const waits = [0, 800, 2000]
  for (let attempt = 0; attempt < waits.length; attempt++) {
    if (attempt) {
      await new Promise(resolve => setTimeout(resolve, waits[attempt]))
      if (document.visibilityState === 'hidden' || navigator.onLine === false) {
        await new Promise<void>(resolve => {
          const resume = () => {
            if (document.visibilityState === 'hidden' || navigator.onLine === false) return
            document.removeEventListener('visibilitychange', resume)
            window.removeEventListener('online', resume)
            resolve()
          }
          document.addEventListener('visibilitychange', resume)
          window.addEventListener('online', resume)
          resume()
        })
      }
    }
    try { await task(); return }
    catch (error) {
      const failure = error as { status?: number; retryable?: boolean }
      if (identityChanged.value || failure.retryable === false ||
        (failure.status !== undefined && failure.status >= 400 && failure.status < 500 && ![408, 429].includes(failure.status)) ||
        attempt === waits.length - 1) throw error
    }
  }
}

// Only a strict same-site path can be handed to the BFF. Never return to an auth
// callback (including old callbacks carrying tokens) or replay its query string.
export function consoleReturnPath(value = location.pathname + location.search + location.hash): string {
  try {
    const url = new URL(value, location.origin)
    if (!value.startsWith('/') || value.startsWith('//') || Array.from(value).some(character => character.charCodeAt(0) <= 32 || character === '\\') || url.origin !== location.origin) return '/'
    if (/^\/(?:auth(?:\/|$)|login$|register$|forgot-password$|reset-password$|email-verify$)/.test(url.pathname)) return '/'
    return url.pathname + url.search + url.hash
  } catch { return '/' }
}
export type RegistrationEntryField = 'promo' | 'aff' | 'invite'
export function centralLogin(returnPath?: string, entry?: {
  mode: 'register' | 'forgot'
  fields?: Partial<Record<RegistrationEntryField, string>>
}): void {
  const query = new URLSearchParams({ returnPath: consoleReturnPath(returnPath) })
  if (entry) {
    query.set('mode', entry.mode)
    for (const field of ['promo', 'aff', 'invite'] as const) {
      const value = entry.fields?.[field]
      if (typeof value === 'string' && value) query.set(field, value)
    }
  }
  location.assign('/auth/login?' + query)
}


function publish(next: CentralSession): Promise<void> {
  // Once an identity is revoked, only the fresh document may accept another one.
  if (identityChanged.value) return Promise.resolve()
  const previous = session
  const changed = initialized && next.user?.uid !== previous.user?.uid
  session = next
  if (changed) {
    identityVersion++
    identityChanged.value = true
  }
  const version = identityVersion
  // Refresh same-user profile too: an avatar may change behind the same URL.
  // Serialize hydration and invalidate queued work after identity revocation.
  sessionWork = sessionWork.catch(() => {}).then(async () => {
    if (version !== identityVersion) return
    await sessionHandler?.(next)
  })
  if (changed && !logoutPending) location.reload()
  return sessionWork
}

export function initializeCentralAccount(onSession: (session: CentralSession) => Promise<void>): Promise<void> {
  if (!centralAccountEnabled || initialized) return Promise.resolve()
  if (initializeWork) return initializeWork
  checking.value = true
  sessionHandler = onSession
  initializeWork = recoverSession(async () => {
    if (!consumer) {
      const browserModulePath = '/auth/browser.mjs'
      const module = await import(/* @vite-ignore */ browserModulePath) as ConsumerModule
      if (typeof module.bootstrapSub2apiConsumer !== 'function') throw Object.assign(new Error('Account browser module unavailable'), {retryable: false})
      consumer = await module.bootstrapSub2apiConsumer({ authorizeOrigin: accountOrigin!, onSession: publish,
        onError: () => {
          // Initial/explicit checks own their retry budget. Independent consumer
          // restore failures join the same recovery rather than prompting refresh.
          if (initialized && !refreshWork) queueMicrotask(() => {
            if (!refreshWork) void refreshCentralAccount().catch(() => {})
          })
        } })
    } else await consumer.refresh()
    await sessionWork
  }).then(() => {
    initialized = true
    unavailable.value = ''
    lastSuccessfulCheck = performance.now()
    if (!lifecycleInstalled) {
      lifecycleInstalled = true
      window.addEventListener('focus', () => recheckVisible())
      window.addEventListener('online', () => recheckVisible(true))
      window.addEventListener('pageshow', event => { if (event.persisted) recheckVisible(true) })
      window.addEventListener('pagehide', event => { if (event.persisted) checking.value = true })
      document.addEventListener('visibilitychange', () => recheckVisible())
    }
  }).catch(error => {
    unavailable.value = '连接暂时不稳定，正在等待恢复。'
    throw error
  }).finally(() => { checking.value = false; initializeWork = undefined })
  return initializeWork
}

export function refreshCentralAccount(): Promise<void> {
  if (refreshWork) return refreshWork
  if (!consumer) return Promise.reject(new Error('Account bootstrap not ready'))
  checking.value = true
  clearTimeout(recoveryTimer); recoveryTimer = undefined
  refreshWork = recoverSession(async () => {
    await consumer!.refresh()
    await sessionWork
    if (!identityChanged.value) {
      unavailable.value = ''
      lastSuccessfulCheck = performance.now()
    }
    logoutUncertain = false
  }).catch(error => { unavailable.value = '连接暂时不稳定，正在等待恢复。'; throw error }).finally(() => {
    checking.value = false
    refreshWork = undefined
    if (unavailable.value) scheduleRecovery()
  })
  return refreshWork
}
function recheckVisible(restored = false): void {
  if (document.visibilityState !== 'visible' || logoutPending || logoutUncertain || identityChanged.value) return
  // Only successful authority checks suppress routine foreground events. Manual
  // refresh and persisted restores bypass this interval; in-flight work is reused.
  if (!restored && !unavailable.value && !refreshWork && lastSuccessfulCheck !== undefined &&
    performance.now() - lastSuccessfulCheck < visibleRecheckIntervalMs) return
  void refreshCentralAccount().catch(() => { /* UI retains drafts and shows the failed check. */ })
}

export async function logoutCentralAccount(): Promise<void> {
  if (!consumer || logoutPending || logoutUncertain) throw new Error('退出尚未确认，请先重新检查会话；不要重复退出请求。')
  logoutPending = true
  checking.value = true
  try {
    await consumer.logout()
    await sessionWork
    identityChanged.value = true
    location.replace('/home')
  } catch {
    logoutUncertain = true
    markCentralUnavailable('退出结果未确认，未自动重试。请先重新检查会话。')
    throw new Error(unavailable.value)
  } finally { logoutPending = false; checking.value = false }
}

export function assertCentralRequest(url: string, method = 'GET'): void {
  if (!centralAccountEnabled) return
  const target = new URL(url, location.origin)
  if (target.origin !== location.origin || !target.pathname.startsWith('/api/v1/')) throw new Error('Console account requests must use the same-origin BFF')
  const isMe = target.pathname === '/api/v1/auth/me' && method.toUpperCase() === 'GET'
  if (identityChanged.value || (!isMe && (checking.value || unavailable.value))) throw new Error('Account session check required')
}
/** Keep the current page stable while its session is rechecked. Requests wait
 * for the same check instead of failing just because the window gained focus. */
export async function waitForCentralRequest(url: string, method = 'GET'): Promise<void> {
  if (!centralAccountEnabled) return
  const target = new URL(url, location.origin)
  const isHydration = target.origin === location.origin && target.pathname === '/api/v1/auth/me' && method.toUpperCase() === 'GET'
  // Profile hydration belongs to refreshWork itself; waiting here would deadlock it.
  if (!isHydration) {
    const version = identityVersion
    while (refreshWork) await refreshWork
    if (version !== identityVersion) throw new Error('Account changed before request')
  }
  assertCentralRequest(url, method)
}

export function consoleAuthHeaders(method = 'GET'): Record<string, string> {
  if (!centralAccountEnabled) {
    const token = localStorage.getItem('auth_token')
    return token ? { Authorization: 'Bearer ' + token } : {}
  }
  if (/^(GET|HEAD|OPTIONS)$/i.test(method)) return {}
  if (!session.csrf) throw new Error('Account CSRF unavailable')
  return { 'X-CSRF-Token': session.csrf }
}

/** SSE and raw console fetches share cookie/CSRF rules; model/Key fetches stay separate. */
export async function consoleFetch(input: string, init: RequestInit = {}): Promise<Response> {
  if (!centralAccountEnabled) return fetch(input, init)
  await waitForCentralRequest(input, init.method)
  const headers = new Headers(init.headers)
  headers.delete('Authorization')
  for (const [name, value] of Object.entries(consoleAuthHeaders(init.method))) headers.set(name, value)
  const version = identityVersion
  const response = await fetch(input, { ...init, headers, credentials: 'same-origin', redirect: 'error' })
  if (version !== identityVersion || identityChanged.value) {
    void response.body?.cancel()
    throw new Error('Account changed during request')
  }
  if (response.status === 401) markCentralUnavailable()
  return response
}
