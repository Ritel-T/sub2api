// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSubscriptionStore } from '../subscriptions'
import type { UserSubscription } from '@/types'

const getActive = vi.hoisted(() => vi.fn())
const state = vi.hoisted(() => ({ auth: null as unknown as { user: { id: number }, token: string, readonly sessionIdentity: string } }))
vi.mock('@/api/subscriptions', () => ({ default: { getActiveSubscriptions: getActive } }))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  state.auth = reactive({ user: { id: 7 }, token: 'token-7',
    // Match the auth-store identity contract while retaining the original
    // token fixture's account switches and pending-response transitions.
    get sessionIdentity() { return this.token }
  })
  return { useAuthStore: () => state.auth }
})
function deferred() {
  let resolve!: (value: UserSubscription[]) => void
  const promise = new Promise<UserSubscription[]>(yes => { resolve = yes })
  return { promise, resolve }
}
const own = [{ id: 1, user_id: 7, group_id: 1 } as UserSubscription]
beforeEach(() => {
  setActivePinia(createPinia()); getActive.mockReset()
  state.auth.user.id = 7; state.auth.token = 'token-7'
})
describe('identity-bound subscription reads', () => {
  it('publishes fresh subscriptions with their authenticated owner', async () => {
    getActive.mockResolvedValueOnce(own)
    const store = useSubscriptionStore()
    expect(await store.fetchActiveSubscriptionsIfCurrent(7)).toEqual(own)
    expect(store.ownerUserId).toBe(7)
    expect(store.activeSubscriptions).toEqual(own)
    expect(store.loading).toBe(false)
  })
  it('does not adopt another account cache', async () => {
    getActive.mockResolvedValueOnce(own).mockResolvedValueOnce([{ id: 2, user_id: 8 }])
    const store = useSubscriptionStore()
    await store.fetchActiveSubscriptionsIfCurrent(7)
    state.auth.user.id = 8; state.auth.token = 'token-8'
    await store.fetchActiveSubscriptionsIfCurrent(8)
    expect(getActive).toHaveBeenCalledTimes(2)
    expect(store.ownerUserId).toBe(8)
    expect(store.activeSubscriptions.map(s => s.user_id)).toEqual([8])
  })
  it('clears and rejects an old response after identity changes', async () => {
    const pending = deferred(); getActive.mockReturnValueOnce(pending.promise)
    const store = useSubscriptionStore()
    const read = store.fetchActiveSubscriptionsIfCurrent(7)
    state.auth.user.id = 8; state.auth.token = 'token-8'
    expect(store.activeSubscriptions).toEqual([]); expect(store.loading).toBe(false)
    pending.resolve(own)
    expect(await read).toEqual([])
    expect(store.ownerUserId).toBeNull(); expect(store.activeSubscriptions).toEqual([])
  })
  it('rejects identity bounce and mismatched response subjects', async () => {
    const pending = deferred(); getActive.mockReturnValueOnce(pending.promise)
    const store = useSubscriptionStore()
    const read = store.fetchActiveSubscriptionsIfCurrent(7)
    state.auth.user.id = 8; state.auth.user.id = 7
    pending.resolve(own); expect(await read).toEqual([])
    getActive.mockResolvedValueOnce([{ id: 2, user_id: 8 }])
    expect(await store.fetchActiveSubscriptionsIfCurrent(7)).toEqual([])
    expect(store.activeSubscriptions).toEqual([])
  })
  it('does not let an older cleared response replace the new owner', async () => {
    const old = deferred(), next = deferred()
    getActive.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const store = useSubscriptionStore()
    const first = store.fetchActiveSubscriptionsIfCurrent(7)
    state.auth.user.id = 8; state.auth.token = 'token-8'
    const second = store.fetchActiveSubscriptionsIfCurrent(8)
    old.resolve(own); await first
    expect(store.loading).toBe(true)
    next.resolve([{ id: 2, user_id: 8 } as UserSubscription]); await second
    expect(store.ownerUserId).toBe(8)
    expect(store.activeSubscriptions.map(s => s.user_id)).toEqual([8])
  })
  it('does not call the provider for an unexpected authenticated user', async () => {
    const store = useSubscriptionStore()
    expect(await store.fetchActiveSubscriptionsIfCurrent(8)).toEqual([])
    expect(getActive).not.toHaveBeenCalled()
  })
  it('merges ordinary force reads only within the same session and rejects identity bounce', async () => {
    const old = deferred(), next = deferred()
    getActive.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const store = useSubscriptionStore()
    const first = store.fetchActiveSubscriptions()
    const merged = store.fetchActiveSubscriptions(true)
    expect(getActive).toHaveBeenCalledTimes(1)
    state.auth.token = 'new-session-7'
    expect(store.loading).toBe(false)
    const second = store.fetchActiveSubscriptions(true)
    old.resolve(own)
    expect(await first).toEqual([])
    expect(await merged).toEqual([])
    expect(store.loading).toBe(true)
    next.resolve(own)
    expect(await second).toEqual(own)
    state.auth.user.id = 8
    state.auth.user.id = 7
    expect(store.activeSubscriptions).toEqual([])
    getActive.mockResolvedValueOnce(own)
    await store.fetchActiveSubscriptions()
    expect(getActive).toHaveBeenCalledTimes(3)
  })

  it('keeps IfCurrent independent from an ordinary pre-write read', async () => {
    const pending = deferred()
    const authoritative = deferred()
    getActive.mockReturnValueOnce(pending.promise).mockReturnValueOnce(authoritative.promise)
    const store = useSubscriptionStore()
    const first = store.fetchActiveSubscriptions()
    const guarded = store.fetchActiveSubscriptionsIfCurrent(7)
    expect(getActive).toHaveBeenCalledTimes(2)
    pending.resolve([])
    expect(await first).toEqual([])
    expect(store.loading).toBe(true)
    authoritative.resolve(own)
    expect(await guarded).toEqual(own)
    expect(store.loading).toBe(false)
    expect(store.activeSubscriptions).toEqual(own)
  })

  it('does not merge an ordinary read into another user or accept an A -> B -> A pending response', async () => {
    const old = deferred(), next = deferred()
    getActive.mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise)
    const store = useSubscriptionStore()
    const first = store.fetchActiveSubscriptions()
    state.auth.user.id = 8
    const second = store.fetchActiveSubscriptions(true)
    expect(getActive).toHaveBeenCalledTimes(2)
    old.resolve(own)
    expect(await first).toEqual([])
    expect(store.loading).toBe(true)
    state.auth.user.id = 7
    next.resolve([{ id: 2, user_id: 8 } as UserSubscription])
    expect(await second).toEqual([])
    expect(store.activeSubscriptions).toEqual([])
  })

  it('lets ordinary and force reads adopt the pending authoritative result', async () => {
    const pending = deferred()
    getActive.mockReturnValueOnce(pending.promise)
    const store = useSubscriptionStore()
    const guarded = store.fetchActiveSubscriptionsIfCurrent(7)
    const ordinary = store.fetchActiveSubscriptions()
    const forced = store.fetchActiveSubscriptions(true)
    expect(getActive).toHaveBeenCalledTimes(1)
    pending.resolve(own)
    expect(await Promise.all([guarded, ordinary, forced])).toEqual([own, own, own])
    expect(store.activeSubscriptions).toEqual(own)
    expect(store.loading).toBe(false)
  })

  it.each(['ordinary-first', 'authority-first'] as const)('preserves a newer IfCurrent while afterWrite waits: %s', async order => {
    getActive.mockResolvedValueOnce(own)
    const store = useSubscriptionStore()
    await store.fetchActiveSubscriptions()
    const old = deferred(), authoritative = deferred()
    const updated = [{ ...own[0], id: 2 }]
    getActive.mockReturnValueOnce(old.promise).mockReturnValueOnce(authoritative.promise)
    const ordinary = store.fetchActiveSubscriptions(true)
    const afterWrite = store.fetchActiveSubscriptions(true, { afterWrite: true })
    const guarded = store.fetchActiveSubscriptionsIfCurrent(7)
    if (order === 'ordinary-first') {
      old.resolve(own)
      expect(await afterWrite).toEqual([])
      expect(store.loading).toBe(true)
      authoritative.resolve(updated)
    } else {
      authoritative.resolve(updated)
      expect(await guarded).toEqual(updated)
      old.resolve(own)
    }
    expect(await ordinary).toEqual([])
    expect(await afterWrite).toEqual([])
    expect(await guarded).toEqual(updated)
    expect(getActive).toHaveBeenCalledTimes(3)
    expect(store.activeSubscriptions).toEqual(updated)
    expect(store.loading).toBe(false)
  })

  it('waits for an already pending IfCurrent then starts a fresh post-write GET', async () => {
    const pending = deferred(), fresh = deferred()
    const updated = [{ ...own[0], id: 2 }]
    getActive.mockReturnValueOnce(pending.promise).mockReturnValueOnce(fresh.promise)
    const store = useSubscriptionStore()
    const guarded = store.fetchActiveSubscriptionsIfCurrent(7)
    const afterWrite = store.fetchActiveSubscriptions(true, { afterWrite: true })
    expect(getActive).toHaveBeenCalledTimes(1)
    pending.resolve(own)
    expect(await guarded).toEqual(own)
    await Promise.resolve()
    expect(getActive).toHaveBeenCalledTimes(2)
    fresh.resolve(updated)
    expect(await afterWrite).toEqual(updated)
    expect(store.activeSubscriptions).toEqual(updated)
  })

  it('keeps only the latest independent IfCurrent authoritative', async () => {
    const old = deferred(), latest = deferred()
    getActive.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const store = useSubscriptionStore()
    const first = store.fetchActiveSubscriptionsIfCurrent(7)
    const second = store.fetchActiveSubscriptionsIfCurrent(7)
    old.resolve(own)
    expect(await first).toEqual([])
    expect(store.loading).toBe(true)
    const ordinary = store.fetchActiveSubscriptions(true)
    latest.resolve(own)
    expect(await second).toEqual(own)
    expect(await ordinary).toEqual(own)
    expect(getActive).toHaveBeenCalledTimes(2)
  })

})
