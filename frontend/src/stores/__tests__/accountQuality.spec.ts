// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { useAccountQualityStore } from '../accountQuality'
import { listQualityPlans, listQualityOperations, listQualityTemplates, listQualityPresets } from '@/api/admin/accountQuality'
import { getAllIncludingInactive } from '@/api/admin/groups'
const state = vi.hoisted(() => ({ auth: null as any }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/api/admin/accountQuality', () => ({ listQualityPlans: vi.fn(), listQualityOperations: vi.fn(), listQualityTemplates: vi.fn(), listQualityPresets: vi.fn() }))
vi.mock('@/api/admin/groups', () => ({ getAllIncludingInactive: vi.fn() }))
const deferred = <T>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes }); return { promise, resolve } }
beforeEach(() => { setActivePinia(createPinia()); state.auth = reactive({ user: { id: 1, role: 'admin' } }); vi.resetAllMocks(); vi.mocked(listQualityPresets).mockResolvedValue([]); vi.mocked(listQualityPlans).mockResolvedValue([]); vi.mocked(listQualityTemplates).mockResolvedValue([]); vi.mocked(listQualityOperations).mockResolvedValue({ items: [], next_cursor: 0 }); vi.mocked(getAllIncludingInactive).mockResolvedValue([]) })
describe('quality workspace session state', () => {
  it('reloads persisted reusable templates independently of dynamic group rules and clears them on logout', async () => {
    vi.mocked(listQualityPresets).mockResolvedValueOnce([{ id: 17, name: 'Saved check' }] as any)
    const store = useAccountQualityStore(); await store.refresh()
    expect(store.presets.map(item => item.name)).toEqual(['Saved check'])
    expect(store.templates).toEqual([])
    const pending = deferred<any[]>(); vi.mocked(listQualityPresets).mockReturnValueOnce(pending.promise)
    const refresh = store.refreshPresets(); state.auth.user = null
    expect(store.presets).toEqual([])
    pending.resolve([{ id: 18, name: 'Previous session' }]); await refresh
    expect(store.presets).toEqual([])
  })
  it('keeps rule panels usable when template loading fails and can reload on retry', async () => {
    vi.mocked(listQualityPresets).mockRejectedValueOnce(new Error('preset storage unavailable'))
    const store = useAccountQualityStore(); await store.refresh()
    expect(store.rulesLoaded).toBe(true)
    expect(store.presetsError).toBe('preset storage unavailable')
    vi.mocked(listQualityPresets).mockResolvedValueOnce([{ id: 17 }] as any)
    await store.refreshPresets()
    expect(store.presetsError).toBe('')
    expect(store.presets).toHaveLength(1)
  })
  it('loads independent panels concurrently and retains visible rules while refreshing', async () => {
    const first = deferred<any[]>(); vi.mocked(listQualityPlans).mockReturnValueOnce(first.promise)
    const store = useAccountQualityStore(); const loading = store.refresh()
    await Promise.resolve()
    expect(listQualityOperations).toHaveBeenCalled(); expect(getAllIncludingInactive).toHaveBeenCalled()
    first.resolve([{ id: 1, account_id: 12, account_name: 'named account' }]); await loading
    store.search = 'named'; const next = deferred<any[]>(); vi.mocked(listQualityPlans).mockReturnValueOnce(next.promise)
    const refresh = store.refreshRules(); expect(store.plans[0].account_name).toBe('named account')
    expect(useAccountQualityStore().search).toBe('named')
    next.resolve([]); await refresh
  })
  it('discards a stale pagination response after a newer refresh', async () => {
    const store = useAccountQualityStore()
    vi.mocked(listQualityOperations).mockResolvedValueOnce({ items: [{ id: 10 }] as any, next_cursor: 10 })
    await store.refreshOperations()
    const append = deferred<any>(); vi.mocked(listQualityOperations).mockReturnValueOnce(append.promise).mockResolvedValueOnce({ items: [{ id: 20 }] as any, next_cursor: 0 })
    const pending = store.refreshOperations(true); await Promise.resolve(); await store.refreshOperations()
    append.resolve({ items: [{ id: 9 }], next_cursor: 9 }); await pending
    expect(store.operations.map(o => o.id)).toEqual([20]); expect(store.cursor).toBe(0)
  })
  it('refreshes the loaded cursor window without dropping earlier pages', async () => {
    vi.mocked(listQualityOperations).mockResolvedValueOnce({ items: [{ id: 20 }, { id: 19 }] as any, next_cursor: 19 }).mockResolvedValueOnce({ items: [{ id: 18 }, { id: 17 }] as any, next_cursor: 17 }).mockResolvedValueOnce({ items: [{ id: 21 }, { id: 20 }] as any, next_cursor: 20 }).mockResolvedValueOnce({ items: [{ id: 19 }, { id: 18 }] as any, next_cursor: 18 })
    const store = useAccountQualityStore()
    await store.refreshOperations(); await store.refreshOperations(true); await store.refreshOperations()
    expect(vi.mocked(listQualityOperations).mock.calls.map(call => call[0])).toEqual([0, 19, 0, 20])
    expect(store.operations.map(operation => operation.id)).toEqual([21, 20, 19, 18])
    expect(store.cursor).toBe(18)
  })
  it('retains the loaded window on refresh failure and retries from its first cursor', async () => {
    vi.mocked(listQualityOperations).mockResolvedValueOnce({ items: [{ id: 20 }] as any, next_cursor: 20 }).mockResolvedValueOnce({ items: [{ id: 19 }] as any, next_cursor: 19 }).mockResolvedValueOnce({ items: [{ id: 21 }] as any, next_cursor: 21 }).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ items: [{ id: 22 }] as any, next_cursor: 22 }).mockResolvedValueOnce({ items: [{ id: 21 }] as any, next_cursor: 0 })
    const store = useAccountQualityStore()
    await store.refreshOperations(); await store.refreshOperations(true); await store.refreshOperations()
    expect(store.operations.map(operation => operation.id)).toEqual([20, 19])
    expect(store.cursor).toBe(19); expect(store.operationsError).toBe('offline')
    await store.retryOperations()
    expect(vi.mocked(listQualityOperations).mock.calls.map(call => call[0])).toEqual([0, 20, 0, 21, 0, 22])
    expect(store.operations.map(operation => operation.id)).toEqual([22, 21]); expect(store.cursor).toBe(0)
  })
  it('keeps append retry on the failed cursor and discards a response across an identity ABA change', async () => {
    vi.mocked(listQualityOperations).mockResolvedValueOnce({ items: [{ id: 20 }] as any, next_cursor: 20 }).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ items: [{ id: 19 }] as any, next_cursor: 0 })
    const store = useAccountQualityStore(); await store.refreshOperations(); await store.refreshOperations(true)
    expect(store.operations.map(operation => operation.id)).toEqual([20]); await store.retryOperations()
    expect(vi.mocked(listQualityOperations).mock.calls.map(call => call[0])).toEqual([0, 20, 20])
    expect(store.operations.map(operation => operation.id)).toEqual([20, 19])
    const late = deferred<any>(); vi.mocked(listQualityOperations).mockReturnValueOnce(late.promise)
    const pending = store.refreshOperations(); await Promise.resolve()
    state.auth.user = { id: 2, role: 'admin' }; state.auth.user = { id: 1, role: 'admin' }
    late.resolve({ items: [{ id: 99 }], next_cursor: 99 }); await pending
    expect(store.operations).toEqual([]); expect(store.cursor).toBe(0); expect(store.operationsLoaded).toBe(false)
  })
  it('clears cached data on logout and rejects an old session response', async () => {
    const store = useAccountQualityStore(); store.plans = [{ id: 1 }] as any
    const request = deferred<any[]>(); vi.mocked(listQualityPlans).mockReturnValueOnce(request.promise)
    const pending = store.refreshRules(); state.auth.user = null
    expect(store.plans).toEqual([]); request.resolve([{ id: 99 }]); await pending
    expect(store.plans).toEqual([]); expect(store.rulesLoaded).toBe(false)
  })
  it('loads group rules with the account rules and clears them on logout', async () => {
    vi.mocked(listQualityTemplates).mockResolvedValueOnce([{ id: 3, plan_ids: [1] }] as any)
    const store = useAccountQualityStore(); await store.refreshRules()
    expect(listQualityTemplates).toHaveBeenCalledTimes(1)
    expect(store.templates.map(t => t.id)).toEqual([3])
    state.auth.user = null
    expect(store.templates).toEqual([])
  })
  it('does not clear snapshots when the same user profile is refreshed', () => {
    const store = useAccountQualityStore(); store.plans = [{ id: 1 }] as any
    state.auth.user = { id: 1, role: 'admin', balance: 100 }
    expect(store.plans).toHaveLength(1)
  })
})
