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
let refreshWork: Promise<void> | undefined
let logoutPending = false
let logoutUncertain = false
let identityVersion = 0
const visibleRecheckIntervalMs = 30_000
let lastSuccessfulCheck: number | undefined

/** Keep locally saved admin test outputs separate when this browser changes account. */
export function consoleOwnedStorageKey(key: string): string {
  return centralAccountEnabled ? key + ':owner:' + (session.user?.uid ?? 'anonymous') : key
}
export function centralSessionVersion(): number { return identityVersion }
export function centralSessionUID(): string | null { return session.user?.uid ?? null }
export function markCentralUnavailable(message = '账号会话暂时无法确认，请重新检查。'): void {
  unavailable.value = message
}

// Only a strict same-site path can be handed to the BFF. Never return to an auth
// callback (including old callbacks carrying tokens) or replay its query string.
export function consoleReturnPath(value = location.pathname + location.search + location.hash): string {
  try {
    const url = new URL(value, location.origin)
    if (!value.startsWith('/') || value.startsWith('//') || /[\\\x00- ]/.test(value) || url.origin !== location.origin) return '/'
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


function publish(next: CentralSession): void {
  // Once an identity is revoked, only the fresh document may accept another one.
  if (identityChanged.value) return
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
  void sessionWork.catch(() => markCentralUnavailable())
  if (changed && !logoutPending) location.reload()
}

export async function initializeCentralAccount(onSession: (session: CentralSession) => Promise<void>): Promise<void> {
  if (!centralAccountEnabled || consumer) return
  checking.value = true
  sessionHandler = onSession
  try {
    const browserModulePath = '/auth/browser.mjs'
    const module = await import(/* @vite-ignore */ browserModulePath) as ConsumerModule
    if (typeof module.bootstrapSub2apiConsumer !== 'function') throw new Error('Account browser module unavailable')
    consumer = await module.bootstrapSub2apiConsumer({ authorizeOrigin: accountOrigin!, onSession: publish, onError: () => markCentralUnavailable() })
    await sessionWork
    initialized = true
    unavailable.value = ''
    lastSuccessfulCheck = performance.now()
    window.addEventListener('focus', () => recheckVisible())
    window.addEventListener('pageshow', event => {
      // Persisted restoration must check even before visibilitychange arrives.
      if (event.persisted) recheckVisible(true)
    })
    window.addEventListener('pagehide', event => { if (event.persisted) checking.value = true })
    document.addEventListener('visibilitychange', () => recheckVisible())
  } catch (error) {
    markCentralUnavailable()
    throw error
  } finally { checking.value = false }
}

export function refreshCentralAccount(): Promise<void> {
  if (refreshWork) return refreshWork
  if (!consumer) return Promise.reject(new Error('Account bootstrap not ready'))
  checking.value = true
  refreshWork = (async () => {
    await consumer!.refresh()
    await sessionWork
    if (!identityChanged.value) {
      unavailable.value = ''
      lastSuccessfulCheck = performance.now()
    }
    logoutUncertain = false
  })().catch(error => { markCentralUnavailable(); throw error }).finally(() => {
    checking.value = false
    refreshWork = undefined
  })
  return refreshWork
}
function recheckVisible(restored = false): void {
  if ((!restored && document.visibilityState !== 'visible') || logoutPending || logoutUncertain || identityChanged.value) return
  // Only successful authority checks suppress routine foreground events. Manual
  // refresh and persisted restores bypass this interval; in-flight work is reused.
  if (!restored && !refreshWork && lastSuccessfulCheck !== undefined &&
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
