/**
 * Subscription Store
 * Global state management for user subscriptions with caching and deduplication
 */

import { defineStore } from 'pinia'
import { ref, computed, watch, onScopeDispose } from 'vue'
import subscriptionsAPI from '@/api/subscriptions'
import { useAuthStore } from '@/stores/auth'
import type { UserSubscription } from '@/types'

// Cache TTL: 60 seconds
const CACHE_TTL_MS = 60_000

export const useSubscriptionStore = defineStore('subscriptions', () => {
  const auth = useAuthStore()
  let requestGeneration = 0
  // Invalidate both cache and pending reads on every identity transition, including A -> B -> A.
  watch([() => auth.sessionIdentity, () => auth.user?.id], clear, { flush: 'sync' })
  onScopeDispose(stopPolling)
  // State
  const activeSubscriptions = ref<UserSubscription[]>([])
  const ownerUserId = ref<number | null>(null)
  const loading = ref(false)
  const loaded = ref(false)
  const lastFetchedAt = ref<number | null>(null)

  // In-flight request deduplication
  let activePromise: Promise<UserSubscription[]> | null = null
  let authoritativePromise: Promise<UserSubscription[]> | null = null

  // Auto-refresh interval
  let pollerInterval: ReturnType<typeof setInterval> | null = null

  // Computed
  const hasActiveSubscriptions = computed(() => activeSubscriptions.value.length > 0)

  /**
   * Fetch active subscriptions with caching and deduplication
   * @param force - Force refresh even if cache is valid
   */
  async function fetchActiveSubscriptions(
    force = false,
    options: { afterWrite?: boolean } = {}
  ): Promise<UserSubscription[]> {
    if (options.afterWrite) {
      // A confirmed write must read after the pre-write GET settles, even if it failed.
      const generation = requestGeneration
      const pending = activePromise ?? authoritativePromise
      if (pending) await pending.catch(() => {})
      // A newer IfCurrent owns publication; a delayed write-read must not supersede it.
      if (generation !== requestGeneration) return []
      force = true
    } else {
      // Ordinary reads may adopt an authoritative read, never cancel it.
      if (authoritativePromise) return authoritativePromise
      if (activePromise) return activePromise
    }
    const expectedIdentity = auth.sessionIdentity
    const expectedUserId = auth.user?.id
    const now = Date.now()

    // Return cached data if valid
    if (
      !force &&
      loaded.value &&
      lastFetchedAt.value !== null &&
      now - lastFetchedAt.value < CACHE_TTL_MS
    ) {
      return activeSubscriptions.value
    }

    const currentGeneration = requestGeneration

    // Start new request
    loading.value = true
    const requestPromise = subscriptionsAPI
      .getActiveSubscriptions()
      .then((data) => {
        if (activePromise !== requestPromise || currentGeneration !== requestGeneration
          || auth.sessionIdentity !== expectedIdentity || auth.user?.id !== expectedUserId) return []
        if (expectedUserId !== undefined && data.some(sub => sub.user_id !== expectedUserId)) return []
        ownerUserId.value = expectedUserId ?? null
        activeSubscriptions.value = data
        loaded.value = true
        lastFetchedAt.value = Date.now()
        return data
      })
      .catch((error) => {
        console.error('Failed to fetch active subscriptions:', error)
        throw error
      })
      .finally(() => {
        if (activePromise === requestPromise) {
          loading.value = false
          activePromise = null
        }
      })

    activePromise = requestPromise

    return activePromise
  }

  // This path always fetches fresh data and never adopts another account's
  // cache. Existing callers retain their original caching behavior.
  async function fetchActiveSubscriptionsIfCurrent(expectedUserId: number): Promise<UserSubscription[]> {
    const auth = useAuthStore()
    const expectedToken = auth.sessionIdentity
    if (!expectedToken || auth.user?.id !== expectedUserId) return []
    if (ownerUserId.value !== null && ownerUserId.value !== expectedUserId) clear()
    const generation = ++requestGeneration
    // This independent authoritative read supersedes the ordinary pending read.
    activePromise = null
    let stale = false
    const isCurrent = () => !stale && generation === requestGeneration
      && auth.sessionIdentity === expectedToken && auth.user?.id === expectedUserId
    const stop = watch([() => auth.sessionIdentity, () => auth.user?.id], () => {
      stale = true
      if (generation === requestGeneration) clear()
    }, { flush: 'sync' })
    loading.value = true
    const requestPromise = subscriptionsAPI.getActiveSubscriptions().then((data) => {
      if (!isCurrent() || data.some(sub => sub.user_id !== expectedUserId)) return []
      activeSubscriptions.value = data
      ownerUserId.value = expectedUserId
      loaded.value = true
      lastFetchedAt.value = Date.now()
      return data
    }).finally(() => {
      if (authoritativePromise === requestPromise) {
        authoritativePromise = null
        loading.value = false
      }
      stop()
    })
    authoritativePromise = requestPromise
    return requestPromise
  }

  /**
   * Start auto-refresh polling 
   */
  function refreshAutomatically(force = false) {
    if (document.visibilityState !== 'visible' || !navigator.onLine) return
    void fetchActiveSubscriptions(force).catch((error) => {
      console.error('Subscription polling failed:', error)
    })
  }

  function onResume() {
    refreshAutomatically()
  }

  function startPolling() {
    if (pollerInterval) return
    document.addEventListener('visibilitychange', onResume)
    window.addEventListener('online', onResume)
    pollerInterval = setInterval(() => refreshAutomatically(true), 5 * 60 * 1000)
  }

  /**
   * Stop auto-refresh polling
   */
  function stopPolling() {
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onResume)
    if (typeof window !== 'undefined') window.removeEventListener('online', onResume)
    if (pollerInterval) {
      clearInterval(pollerInterval)
      pollerInterval = null
    }
  }

  /**
   * Clear all subscription data and stop polling
   */
  function clear() {
    requestGeneration++
    activePromise = null
    authoritativePromise = null
    loading.value = false
    activeSubscriptions.value = []
    ownerUserId.value = null
    loaded.value = false
    lastFetchedAt.value = null
    stopPolling()
  }

  /**
   * Invalidate cache (force next fetch to reload)
   */
  function invalidateCache() {
    lastFetchedAt.value = null
  }

  return {
    // State
    activeSubscriptions,
    ownerUserId,
    loading,
    hasActiveSubscriptions,

    // Actions
    fetchActiveSubscriptions,
    fetchActiveSubscriptionsIfCurrent,
    startPolling,
    stopPolling,
    clear,
    invalidateCache
  }
})
