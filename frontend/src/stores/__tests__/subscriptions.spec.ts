import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useSubscriptionStore } from '@/stores/subscriptions'
import type { UserSubscription, User } from '@/types'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useAnnouncementStore } from '@/stores/announcements'
import { shallowMount, flushPromises } from '@vue/test-utils'
import App from '@/App.vue'
import { i18n } from '@/i18n'

vi.mock('vue-router', async () => ({
  ...(await vi.importActual<typeof import('vue-router')>('vue-router')),
  useRouter: () => ({ afterEach: vi.fn(), replace: vi.fn() }),
  useRoute: () => ({ path: '/keys', fullPath: '/keys', meta: {}, params: {}, query: {} })
}))
vi.mock('@/api/setup', () => ({ getSetupStatus: async () => ({ needs_setup: true }) }))

// Mock subscriptions API
const mockGetActiveSubscriptions = vi.fn()

vi.mock('@/api/subscriptions', () => ({
  default: {
    getActiveSubscriptions: (...args: any[]) => mockGetActiveSubscriptions(...args),
  },
}))

const fakeSubscriptions = [
  {
    id: 1,
    user_id: 1,
    group_id: 1,
    status: 'active' as const,
    daily_usage_usd: 5,
    weekly_usage_usd: 20,
    monthly_usage_usd: 50,
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: null,
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
    expires_at: '2025-01-01',
  },
  {
    id: 2,
    user_id: 1,
    group_id: 2,
    status: 'active' as const,
    daily_usage_usd: 10,
    weekly_usage_usd: 40,
    monthly_usage_usd: 100,
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: null,
    created_at: '2024-02-01',
    updated_at: '2024-02-01',
    expires_at: '2025-02-01',
  },
]

function deferred() {
  let resolve!: (value: UserSubscription[]) => void
  let reject!: (error: Error) => void
  const promise = new Promise<UserSubscription[]>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('useSubscriptionStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
    mockGetActiveSubscriptions.mockReset()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    useSubscriptionStore().clear()
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  // --- fetchActiveSubscriptions ---

  describe('fetchActiveSubscriptions', () => {
    it('成功获取活跃订阅', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      const result = await store.fetchActiveSubscriptions()

      expect(result).toEqual(fakeSubscriptions)
      expect(store.activeSubscriptions).toEqual(fakeSubscriptions)
      expect(store.loading).toBe(false)
    })

    it('缓存有效时返回缓存数据', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      // 第一次请求
      await store.fetchActiveSubscriptions()
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)

      // 第二次请求（60秒内）- 应返回缓存
      const result = await store.fetchActiveSubscriptions()
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1) // 没有新请求
      expect(result).toEqual(fakeSubscriptions)
    })

    it('缓存过期后重新请求', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      await store.fetchActiveSubscriptions()
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)

      // 推进 61 秒让缓存过期
      vi.advanceTimersByTime(61_000)

      const updatedSubs = [fakeSubscriptions[0]]
      mockGetActiveSubscriptions.mockResolvedValue(updatedSubs)

      const result = await store.fetchActiveSubscriptions()
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
      expect(result).toEqual(updatedSubs)
    })

    it('force=true 强制重新请求', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      await store.fetchActiveSubscriptions()

      const updatedSubs = [fakeSubscriptions[0]]
      mockGetActiveSubscriptions.mockResolvedValue(updatedSubs)

      const result = await store.fetchActiveSubscriptions(true)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
      expect(result).toEqual(updatedSubs)
    })

    it('并发请求共享同一个 Promise（去重）', async () => {
      let resolvePromise: (v: any) => void
      mockGetActiveSubscriptions.mockImplementation(
        () => new Promise((resolve) => { resolvePromise = resolve })
      )
      const store = useSubscriptionStore()

      // 并发发起两个请求
      const p1 = store.fetchActiveSubscriptions()
      const p2 = store.fetchActiveSubscriptions()

      // 只调用了一次 API
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)

      // 解决 Promise
      resolvePromise!(fakeSubscriptions)

      const [r1, r2] = await Promise.all([p1, p2])
      expect(r1).toEqual(fakeSubscriptions)
      expect(r2).toEqual(fakeSubscriptions)
    })

    it('API 错误时抛出异常', async () => {
      mockGetActiveSubscriptions.mockRejectedValue(new Error('Network error'))
      const store = useSubscriptionStore()

      await expect(store.fetchActiveSubscriptions()).rejects.toThrow('Network error')
    })
  })

  describe('P16 request scheduling', () => {
    it('merges ordinary and forced reads while a request is pending', async () => {
      const pending = deferred()
      mockGetActiveSubscriptions.mockReturnValue(pending.promise)
      const store = useSubscriptionStore()
      const reads = [store.fetchActiveSubscriptions(), store.fetchActiveSubscriptions(true), store.fetchActiveSubscriptions(true)]
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
      pending.resolve(fakeSubscriptions)
      expect(await Promise.all(reads)).toEqual([fakeSubscriptions, fakeSubscriptions, fakeSubscriptions])
    })

    it.each(['resolve', 'reject'] as const)('reads afresh after a confirmed write when the old GET %s', async outcome => {
      const pending = deferred()
      const updated = [fakeSubscriptions[0]]
      mockGetActiveSubscriptions.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(updated)
      const store = useSubscriptionStore()
      const old = store.fetchActiveSubscriptions().catch(() => [])
      const afterWrite = store.fetchActiveSubscriptions(true, { afterWrite: true })
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
      if (outcome === 'resolve') pending.resolve(fakeSubscriptions)
      else pending.reject(new Error('offline'))
      await old
      expect(await afterWrite).toEqual(updated)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
      expect(store.activeSubscriptions).toEqual(updated)
    })

    it('does not run a queued post-write read after clear', async () => {
      const pending = deferred()
      mockGetActiveSubscriptions.mockReturnValueOnce(pending.promise)
      const store = useSubscriptionStore()
      const old = store.fetchActiveSubscriptions()
      const afterWrite = store.fetchActiveSubscriptions(true, { afterWrite: true })
      store.clear()
      pending.resolve(fakeSubscriptions)
      await old
      expect(await afterWrite).toEqual([])
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
    })

    it.each(['hidden', 'offline'] as const)('does not poll while %s and merges simultaneous resume events', async mode => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()
      await store.fetchActiveSubscriptions()
      store.startPolling()
      if (mode === 'hidden') vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
      else vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
      await vi.advanceTimersByTimeAsync(15 * 60_000)
      document.dispatchEvent(new Event('visibilitychange'))
      window.dispatchEvent(new Event('online'))
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
      const pending = deferred()
      mockGetActiveSubscriptions.mockReturnValueOnce(pending.promise)
      vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
      vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
      document.dispatchEvent(new Event('visibilitychange'))
      window.dispatchEvent(new Event('online'))
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
      pending.resolve(fakeSubscriptions)
      await vi.advanceTimersByTimeAsync(0)
      document.dispatchEvent(new Event('visibilitychange'))
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
    })

    it('resumes only when the 60-second TTL expires, including a timestamp of zero', async () => {
      vi.setSystemTime(0)
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()
      await store.fetchActiveSubscriptions()
      store.startPolling()
      await vi.advanceTimersByTimeAsync(59_999)
      window.dispatchEvent(new Event('online'))
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(1)
      window.dispatchEvent(new Event('online'))
      await vi.advanceTimersByTimeAsync(0)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
      await vi.advanceTimersByTimeAsync(240_000)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(3)
    })

    it('keeps explicit force refresh available while hidden and offline', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()
      await store.fetchActiveSubscriptions()
      vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
      vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
      await store.fetchActiveSubscriptions(true)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
    })

    it('does not cancel IfCurrent when the five-minute poll fires', async () => {
      const auth = useAuthStore()
      auth.user = { id: 1, role: 'user' } as User
      auth.token = 'session-1'
      const pending = deferred()
      mockGetActiveSubscriptions.mockReturnValueOnce(pending.promise)
      const store = useSubscriptionStore()
      store.startPolling()
      const guarded = store.fetchActiveSubscriptionsIfCurrent(1)
      await vi.advanceTimersByTimeAsync(5 * 60_000)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
      pending.resolve(fakeSubscriptions)
      expect(await guarded).toEqual(fakeSubscriptions)
      expect(store.loading).toBe(false)
      await vi.advanceTimersByTimeAsync(5 * 60_000)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
    })

    it('App restarts subscription sync on identity changes while authenticated and stops on feature-off/logout', async () => {
      const auth = useAuthStore()
      auth.user = { id: 1, role: 'user' } as User
      auth.token = 'session-1'
      const app = useAppStore()
      vi.spyOn(useAnnouncementStore(), 'fetchAnnouncements').mockResolvedValue()
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const wrapper = shallowMount(App, { global: { plugins: [i18n] } })
      try {
        await flushPromises()
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)
        auth.token = 'session-2'
        expect(auth.isAuthenticated).toBe(true)
        await flushPromises()
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
        await vi.advanceTimersByTimeAsync(5 * 60_000)
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(3)
        auth.user = { id: 2, role: 'user' } as User
        mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions.map(sub => ({ ...sub, user_id: 2 })))
        await flushPromises()
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(4)
        app.$patch({ cachedPublicSettings: { subscription_enabled: false } })
        await flushPromises()
        await vi.advanceTimersByTimeAsync(5 * 60_000)
        window.dispatchEvent(new Event('online'))
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(4)
        app.$patch({ cachedPublicSettings: { subscription_enabled: true } })
        await flushPromises()
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(5)
        auth.token = null
        expect(auth.isAuthenticated).toBe(false)
        await flushPromises()
        await vi.advanceTimersByTimeAsync(5 * 60_000)
        window.dispatchEvent(new Event('online'))
        expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(5)
      } finally {
        wrapper.unmount()
      }
    })

    it.each(['stopPolling', 'clear'] as const)('%s removes resume listeners and the timer', async action => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()
      store.startPolling()
      store[action]()
      document.dispatchEvent(new Event('visibilitychange'))
      window.dispatchEvent(new Event('online'))
      await vi.advanceTimersByTimeAsync(600_000)
      expect(mockGetActiveSubscriptions).not.toHaveBeenCalled()
    })
  })

  // --- hasActiveSubscriptions ---

  describe('hasActiveSubscriptions', () => {
    it('有订阅时返回 true', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      await store.fetchActiveSubscriptions()

      expect(store.hasActiveSubscriptions).toBe(true)
    })

    it('无订阅时返回 false', () => {
      const store = useSubscriptionStore()
      expect(store.hasActiveSubscriptions).toBe(false)
    })

    it('清除后返回 false', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      await store.fetchActiveSubscriptions()
      expect(store.hasActiveSubscriptions).toBe(true)

      store.clear()
      expect(store.hasActiveSubscriptions).toBe(false)
    })
  })

  // --- invalidateCache ---

  describe('invalidateCache', () => {
    it('失效缓存后下次请求重新获取数据', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      await store.fetchActiveSubscriptions()
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)

      store.invalidateCache()

      await store.fetchActiveSubscriptions()
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(2)
    })
  })

  // --- clear ---

  describe('clear', () => {
    it('清除所有订阅数据', async () => {
      mockGetActiveSubscriptions.mockResolvedValue(fakeSubscriptions)
      const store = useSubscriptionStore()

      await store.fetchActiveSubscriptions()
      expect(store.activeSubscriptions).toHaveLength(2)

      store.clear()

      expect(store.activeSubscriptions).toHaveLength(0)
      expect(store.hasActiveSubscriptions).toBe(false)
    })
  })

  // --- polling ---

  describe('startPolling / stopPolling', () => {
    it('startPolling 不会创建重复 interval', () => {
      const store = useSubscriptionStore()
      mockGetActiveSubscriptions.mockResolvedValue([])

      store.startPolling()
      store.startPolling() // 重复调用

      // 推进5分钟只触发一次
      vi.advanceTimersByTime(5 * 60 * 1000)
      expect(mockGetActiveSubscriptions).toHaveBeenCalledTimes(1)

      store.stopPolling()
    })

    it('stopPolling 停止定期刷新', () => {
      const store = useSubscriptionStore()
      mockGetActiveSubscriptions.mockResolvedValue([])

      store.startPolling()
      store.stopPolling()

      vi.advanceTimersByTime(10 * 60 * 1000)
      expect(mockGetActiveSubscriptions).not.toHaveBeenCalled()
    })
  })

  describe('clearing in-flight subscriptions', () => {
    it.each(['resolve', 'reject'] as const)('resets loading immediately and after the old request %s', async (outcome) => {
      const pending = deferred()
      mockGetActiveSubscriptions.mockReturnValueOnce(pending.promise)
      const store = useSubscriptionStore()
      const request = store.fetchActiveSubscriptions().catch(() => [])
      expect(store.loading).toBe(true)
      store.clear()
      expect(store.loading).toBe(false)
      if (outcome === 'resolve') pending.resolve([{ id: 1 } as UserSubscription])
      else pending.reject(new Error('offline'))
      await request
      expect(store.loading).toBe(false)
      expect(store.activeSubscriptions).toEqual([])
    })

    it('keeps a new request loading when an older cleared request finishes', async () => {
      const older = deferred()
      const newer = deferred()
      mockGetActiveSubscriptions.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
      const store = useSubscriptionStore()
      const first = store.fetchActiveSubscriptions()
      store.clear()
      const second = store.fetchActiveSubscriptions()
      older.resolve([{ id: 1 } as UserSubscription])
      await first
      expect(store.loading).toBe(true)
      expect(store.activeSubscriptions).toEqual([])
      newer.resolve([{ id: 2 } as UserSubscription])
      await second
      expect(store.loading).toBe(false)
      expect(store.activeSubscriptions.map(item => item.id)).toEqual([2])
    })
  })
})
