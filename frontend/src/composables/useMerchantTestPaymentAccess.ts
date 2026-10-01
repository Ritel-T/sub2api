import { computed, onScopeDispose, ref, watch } from 'vue'
import { paymentAPI } from '@/api/payment'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

/** Current protected authorization for a globally closed merchant payment pilot. */
export function useMerchantTestPaymentAccess() {
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const authorized = ref(false)
  let generation = 0

  watch(
    [
      () => authStore.isAuthenticated,
      () => authStore.user?.id,
      () => authStore.token,
      () => appStore.cachedPublicSettings?.payment_enabled,
    ] as const,
    async ([authenticated, userId, token, publicPaymentEnabled]) => {
      const requestGeneration = ++generation
      // Clear synchronously, including when logout and a new login happen in
      // the same tick. A prior account's request may never authorize this one.
      authorized.value = false
      if (!authenticated || userId == null || !token || publicPaymentEnabled !== false) return

      try {
        const response = await paymentAPI.getConfig()
        if (requestGeneration !== generation) return
        authorized.value = response.data?.enabled === true && response.data?.merchant_test_access === true
      } catch {
        // Authentication failures, unavailable APIs, and malformed data stay closed.
      }
    },
    { immediate: true, flush: 'sync' },
  )

  onScopeDispose(() => {
    generation++
    authorized.value = false
  })

  return { merchantTestPaymentAccess: computed(() => authorized.value) }
}
