import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Router } from 'vue-router'
import { clearRouteLoadFailure, reportRouteLoadFailure, retryRouteLoad, routeLoadFailure } from '../routeLoadRecovery'

const chunkError = new TypeError('Failed to fetch dynamically imported module')
const html = (entry: string) => `<meta name="ritel-brand" content="fixed"><script type="module" src="/assets/${entry}.js"></script>`
const fetchMock = vi.fn()
beforeEach(() => {
  clearRouteLoadFailure()
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  document.head.innerHTML = html('current-build')
  document.body.innerHTML = '<div id="app" data-v-app><input value="unsaved draft"></div>'
})
afterEach(() => { clearRouteLoadFailure(); vi.unstubAllGlobals() })

describe('route loading recovery', () => {
  it('keeps the current page on same-build failure and retries the exact intended route in place', async () => {
    fetchMock.mockResolvedValue(new Response(html('current-build'), { headers: { 'content-type': 'text/html' } }))
    await reportRouteLoadFailure(chunkError, '/keys?keep=1#keys')
    expect(routeLoadFailure.value).toEqual({ path: '/keys?keep=1#keys', updateAvailable: false })
    expect(document.querySelector('input')?.value).toBe('unsaved draft')
    const push = vi.fn().mockResolvedValue(undefined)
    await retryRouteLoad({ push } as unknown as Router)
    expect(push).toHaveBeenCalledTimes(1)
    expect(push).toHaveBeenCalledWith('/keys?keep=1#keys')
    expect(routeLoadFailure.value).toBeNull()
  })

  it('offers an update only when fresh branded HTML names a different entry, without discarding drafts', async () => {
    fetchMock.mockResolvedValue(new Response(html('new-build'), { headers: { 'content-type': 'text/html' } }))
    await reportRouteLoadFailure(chunkError, '/keys')
    expect(routeLoadFailure.value?.updateAvailable).toBe(true)
    expect(document.querySelector('input')?.value).toBe('unsaved draft')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ cache: 'no-store', redirect: 'error' })
  })

  it('keeps same-page retry available when the version check fails', async () => {
    fetchMock.mockRejectedValue(new TypeError('network unavailable'))
    await reportRouteLoadFailure(chunkError, '/keys')
    expect(routeLoadFailure.value?.updateAvailable).toBe(false)
  })

  it('ignores a late version result after a successful navigation cleared the failure', async () => {
    let respond!: (response: Response) => void
    fetchMock.mockImplementation(() => new Promise<Response>(resolve => { respond = resolve }))
    const report = reportRouteLoadFailure(chunkError, '/keys')
    clearRouteLoadFailure()
    respond(new Response(html('new-build'), { headers: { 'content-type': 'text/html' } }))
    await report
    expect(routeLoadFailure.value).toBeNull()
  })

  it('leaves first navigation to startup recovery and ignores unrelated errors', async () => {
    document.getElementById('app')!.removeAttribute('data-v-app')
    await reportRouteLoadFailure(chunkError, '/keys')
    document.getElementById('app')!.setAttribute('data-v-app', '')
    await reportRouteLoadFailure(new Error('permission denied'), '/keys')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(routeLoadFailure.value).toBeNull()
  })
})
