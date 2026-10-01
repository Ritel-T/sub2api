import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSubscriptionStore } from '../subscriptions'
import type { UserSubscription } from '@/types'

const getActive = vi.hoisted(() => vi.fn())
const state = vi.hoisted(() => ({ auth: null as unknown as { user: { id: number }, token: string } }))
vi.mock('@/api/subscriptions', () => ({ default: { getActiveSubscriptions: getActive } }))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  state.auth = reactive({ user: { id: 7 }, token: 'token-7' })
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
})
