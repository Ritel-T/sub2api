<script setup lang="ts">
import { centralAccountEnabled } from '@/utils/accountConfig'
import { centralChecking, centralSessionError, centralIdentityChanged, refreshCentralAccount } from '@/utils/centralAccount'
import { routeLoadFailure, routeLoadRetrying, retryRouteLoad } from '@/utils/routeLoadRecovery'
function retryAccountCheck() { void refreshCentralAccount().catch(() => { /* Keep the error and drafts visible. */ }) }

import { RouterView, useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onBeforeUnmount, watch, provide } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import { resolveRouteDocumentTitle } from '@/router/title'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { useAppStore, useAuthStore, useSubscriptionStore, useAnnouncementStore, useAdminComplianceStore, useAdminSettingsStore, useSupportTicketStore } from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon, BRAND_NAME, BRAND_FAVICON } from '@/utils/branding'
import { ConfirmationSessionKey } from '@/composables/useConfirmation'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { lotteryAPI } from '@/api/lottery'
import { useLotteryStore } from '@/stores/lottery'

const { locale } = useI18n()
const router = useRouter()
const route = useRoute()
const routeRecoveryCopy = computed(() => {
  if (locale.value === 'zh') return {
    failed: '页面暂时未能加载，当前内容已保留。', retry: '重试打开',
    updated: '页面已有更新，当前内容已保留。请先保存编辑，再重新加载。', reload: '重新加载'
  }
  if (locale.value === 'zh-Hant-HK') return {
    failed: '頁面暫時未能載入，目前內容已保留。', retry: '重試開啟',
    updated: '頁面已有更新，目前內容已保留。請先儲存編輯，再重新載入。', reload: '重新載入'
  }
  return {
    failed: 'This page could not load. Your current content is still here.', retry: 'Try opening again',
    updated: 'An update is available. Your current content is still here. Save your edits before reloading.', reload: 'Reload page'
  }
})
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const supportTicketStore = useSupportTicketStore()
const adminSettingsStore = useAdminSettingsStore()
let claimingLotteryInvitation = false
async function claimLotteryInvitation() {
  if (claimingLotteryInvitation || !authStore.isAuthenticated) return
  try {
    if (!localStorage.getItem('lottery_invitation_pending')) return
    claimingLotteryInvitation = true
    await lotteryAPI.claimInvitation()
    localStorage.removeItem('lottery_invitation_pending')
    await useLotteryStore().refresh(true)
  } catch {
    // Keep the receipt marker for the next login or foreground retry.
  } finally { claimingLotteryInvitation = false }
}

// A confirmation belongs to one authenticated identity and one route context.
provide(ConfirmationSessionKey, () => ({
  active: authStore.isAuthenticated,
  identity: [authStore.user?.id, route.fullPath]
}))

function updateDocumentTitle() {
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, BRAND_NAME, customMenuItems, {
    billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
    observer: authStore.isObserver,
  })
}

// Watch for site settings changes and update favicon/title
watch(
  () => appStore.siteLogo,
  (newLogo) => {
    if (newLogo) {
      updateFavicon(BRAND_FAVICON)
    }
  },
  { immediate: true }
)

watch(
  [
    () => route.fullPath,
    () => locale.value,
    () => route.meta.title,
    () => route.meta.titleKey,
    () => appStore.siteName,
    () => appStore.cachedPublicSettings?.custom_menu_items,
    () => appStore.cachedPublicSettings?.subscription_enabled,
    () => appStore.cachedPublicSettings?.payment_balance_disabled,
    () => authStore.isAdmin,
    () => adminSettingsStore.customMenuItems,
  ],
  updateDocumentTitle,
  { deep: true }
)

// Watch for authentication state and manage subscription data + announcements
function onVisibilityChange() {
  if (document.visibilityState === 'visible' && authStore.isAuthenticated) {
    void claimLotteryInvitation()
    announcementStore.fetchAnnouncements()
    supportTicketStore.refresh()
  }
}

// Public settings may arrive after login; fetch the ticket badge once they say the feature is on.
watch(
  () => appStore.cachedPublicSettings?.support_ticket_enabled,
  (enabled) => {
    if (enabled && authStore.isAuthenticated) supportTicketStore.refresh(true)
  }
)

function onAdminComplianceRequired(event: Event) {
  const detail = (event as CustomEvent<Record<string, string>>).detail || {}
  adminComplianceStore.requireAcknowledgement(detail)
}

// 订阅功能开关（opt-out）。关闭后不再预加载/轮询订阅接口；开关在登录后才到达时补启动，反向则清空。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

function startSubscriptionSync() {
  if (document.visibilityState === 'visible' && navigator.onLine) {
    subscriptionStore.fetchActiveSubscriptions().catch((error) => {
      console.error('Failed to preload subscriptions:', error)
    })
  }
  subscriptionStore.startPolling()
}

// Store identity invalidation runs synchronously; restart only the subscription
// sync on the next Vue flush, even when isAuthenticated stays true.
watch(
  [() => authStore.isAuthenticated, () => authStore.sessionIdentity, () => authStore.user?.id, subscriptionFeatureEnabled],
  ([authenticated, identity, userId, enabled]) => {
    if (authenticated && identity && userId && enabled) startSubscriptionSync()
    else subscriptionStore.clear()
  },
  { immediate: true }
)

watch(
  () => authStore.isAuthenticated,
  (isAuthenticated, oldValue) => {
    if (isAuthenticated) {
      void claimLotteryInvitation()
      if (authStore.isAdmin) {
        adminComplianceStore.fetchStatus().catch((error) => {
          console.error('Failed to fetch admin compliance status:', error)
        })
      }

      // Announcements: new login vs page refresh restore
      if (oldValue === false) {
        // New login: delay 3s then force fetch
        setTimeout(() => announcementStore.fetchAnnouncements(true), 3000)
      } else {
        // Page refresh restore (oldValue was undefined)
        announcementStore.fetchAnnouncements()
      }

      // Support ticket badge (skips itself while the feature is off)
      supportTicketStore.refresh(true)

      // Register visibility change listener
      document.addEventListener('visibilitychange', onVisibilityChange)
    } else {
      // User logged out: clear data and stop polling
      announcementStore.reset()
      adminComplianceStore.reset()
      supportTicketStore.reset()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
router.afterEach(() => {
  if (authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
    supportTicketStore.refresh()
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
})

onMounted(async () => {
  window.addEventListener('admin-compliance-required', onAdminComplianceRequired)

  // Check if setup is needed
  try {
    const status = await getSetupStatus()
    if (status.needs_setup && route.path !== '/setup') {
      router.replace('/setup')
      return
    }
  } catch {
    // If setup endpoint fails, assume normal mode and continue
  }

  // Load public settings into appStore (will be cached for other components)
  await appStore.fetchPublicSettings()

  // Re-resolve document title now that site settings are available
  updateDocumentTitle()
})
</script>

<template>
  <div v-if="!centralIdentityChanged">
    <NavigationProgress />
    <RouterView />
    <Toast />
    <AnnouncementPopup />
    <AdminComplianceDialog />
  </div>
  <section v-if="centralAccountEnabled && (centralSessionError || centralIdentityChanged)" class="fixed bottom-4 right-4 z-[100] flex max-w-[calc(100%-2rem)] items-center gap-4 rounded-lg border border-gray-200 bg-white p-4 text-sm text-gray-900 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-white" role="status" aria-live="polite" :aria-busy="centralChecking">
    <p>{{ centralSessionError || '账号身份已变化，正在重新加载…' }}</p>
    <button v-if="centralSessionError && !centralChecking && !centralIdentityChanged" type="button" class="btn btn-primary" @click="retryAccountCheck">重试连接</button>
  </section>
  <section v-else-if="routeLoadFailure" class="fixed bottom-4 right-4 z-[100] flex max-w-[calc(100%-2rem)] items-center gap-4 rounded-lg border border-gray-200 bg-white p-4 text-sm text-gray-900 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-white" role="status" aria-live="polite" :aria-busy="routeLoadRetrying">
    <p>{{ routeLoadFailure.updateAvailable ? routeRecoveryCopy.updated : routeRecoveryCopy.failed }}</p>
    <button type="button" class="btn btn-primary shrink-0" :disabled="routeLoadRetrying" @click="retryRouteLoad(router)">{{ routeLoadFailure.updateAvailable ? routeRecoveryCopy.reload : routeRecoveryCopy.retry }}</button>
  </section>
</template>
