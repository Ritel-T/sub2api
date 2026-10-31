import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'

const ports = vi.hoisted(() => ({ bootstrap: vi.fn() }))
vi.mock('../accountConfig', () => ({centralAccountEnabled: true, accountOrigin: 'https://account.example.test'}))
vi.mock('/auth/browser.mjs', () => ({bootstrapSub2apiConsumer: ports.bootstrap}))

beforeEach(() => { vi.resetModules(); vi.useFakeTimers(); ports.bootstrap.mockReset() })
afterEach(() => { vi.useRealTimers() })
const session = {user: {uid: 'sub2api:42'}, csrf: 'c'.repeat(43)}

describe('central session recovery', () => {
  it('recovers a transient first check without publishing a startup error', async () => {
    let attempts = 0
    ports.bootstrap.mockImplementation(async (options) => {
      if (++attempts === 1) throw new TypeError('temporary network failure')
      await options.onSession(session)
      return {session, refresh: vi.fn(), stop: vi.fn()}
    })
    const account = await import('../centralAccount')
    const hydrate = vi.fn(async () => {})
    const work = account.initializeCentralAccount(hydrate)
    await vi.waitFor(() => expect(ports.bootstrap).toHaveBeenCalledTimes(1))
    await vi.advanceTimersByTimeAsync(801)
    await work
    expect(attempts).toBe(2); expect(hydrate).toHaveBeenCalledTimes(1)
    expect(account.centralSessionError.value).toBe('')
  })

  it('same-user background checks merge, await hydration and do not replay any business request', async () => {
    let resolve!: () => void
    let onSession!: (s: typeof session) => Promise<void>
    const refresh = vi.fn(async () => { await new Promise<void>(r => {resolve = r}); await onSession(session); return session })
    ports.bootstrap.mockImplementation(async (options) => { onSession = options.onSession; await onSession(session); return {session, refresh, stop: vi.fn()} })
    const account = await import('../centralAccount'), hydrate = vi.fn(async () => {})
    await account.initializeCentralAccount(hydrate)
    const a = account.refreshCentralAccount(), b = account.refreshCentralAccount()
    expect(a).toBe(b); expect(account.centralSessionError.value).toBe('')
    resolve(); await a
    expect(refresh).toHaveBeenCalledTimes(1); expect(hydrate).toHaveBeenCalledTimes(2)
  })

  it('can initialize again after a sustained outage instead of treating a failed bootstrap as ready', async () => {
    ports.bootstrap.mockRejectedValue(new TypeError('offline'))
    const account = await import('../centralAccount')
    const first = account.initializeCentralAccount(async () => {}).catch(error => error)
    await vi.waitFor(() => expect(ports.bootstrap).toHaveBeenCalledTimes(1))
    await vi.advanceTimersByTimeAsync(801)
    await vi.waitFor(() => expect(ports.bootstrap).toHaveBeenCalledTimes(2))
    await vi.advanceTimersByTimeAsync(2001); await first
    expect(ports.bootstrap).toHaveBeenCalledTimes(3)
    ports.bootstrap.mockImplementation(async (options) => {await options.onSession(session); return {session, refresh: vi.fn(), stop: vi.fn()}})
    await account.initializeCentralAccount(async () => {})
    expect(account.centralSessionError.value).toBe('')
  })
})
