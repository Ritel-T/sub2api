import { readonly, ref } from 'vue'
import type { Router } from 'vue-router'

interface RouteLoadFailure {
  path: string
  updateAvailable: boolean
}

const failure = ref<RouteLoadFailure | null>(null)
const retrying = ref(false)
export const routeLoadFailure = readonly(failure)
export const routeLoadRetrying = readonly(retrying)

export function clearRouteLoadFailure(): void { failure.value = null }

function entryModule(document: Document): string | null {
  const source = document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src')
  if (!source) return null
  const url = new URL(source, location.origin)
  return url.origin === location.origin && url.pathname.startsWith('/assets/') ? url.href : null
}

export async function reportRouteLoadFailure(error: Error, path: string): Promise<void> {
  const isChunkError = error.message?.includes('Failed to fetch dynamically imported module') ||
    error.message?.includes('Loading chunk') || error.message?.includes('Loading CSS chunk') ||
    error.name === 'ChunkLoadError'
  if (!isChunkError || !document.getElementById('app')?.hasAttribute('data-v-app')) return

  failure.value = { path, updateAvailable: false }
  const pending = failure.value
  // A network error is not evidence of a deployment. Keep the current page and
  // drafts; only offer a full navigation when fresh HTML names another build.
  try {
    const current = entryModule(document)
    if (!current) return
    const response = await fetch(location.pathname, {
      credentials: 'same-origin', cache: 'no-store', redirect: 'error',
      signal: AbortSignal.timeout(5000)
    })
    if (!response.ok || !response.headers.get('content-type')?.includes('text/html')) return
    const latest = new DOMParser().parseFromString(await response.text(), 'text/html')
    const next = entryModule(latest)
    if (failure.value === pending && latest.querySelector('meta[name="ritel-brand"][content="fixed"]') &&
      next && next !== current) pending.updateAvailable = true
  } catch {
    // An unavailable version check leaves same-page retry available.
  }
}

export async function retryRouteLoad(router: Router): Promise<void> {
  const pending = failure.value
  if (!pending || retrying.value) return
  if (pending.updateAvailable) {
    // This is an explicit user action after the update notice, never an
    // automatic response to a transient network failure.
    location.assign(pending.path)
    return
  }
  retrying.value = true
  try {
    const navigationFailure = await router.push(pending.path)
    if (!navigationFailure && failure.value === pending) clearRouteLoadFailure()
  } catch {
    // router.onError retains the target and updates the recovery notice.
  } finally { retrying.value = false }
}
