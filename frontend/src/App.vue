<script setup lang="ts">
import { centralAccountEnabled } from '@/utils/accountConfig'
import { centralChecking, centralSessionError, centralIdentityChanged, refreshCentralAccount } from '@/utils/centralAccount'
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

const { locale } = useI18n()
const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const supportTicketStore = useSupportTicketStore()
const adminSettingsStore = useAdminSettingsStore()

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
  subscriptionStore.fetchActiveSubscriptions().catch((error) => {
    console.error('Failed to preload subscriptions:', error)
  })
  subscriptionStore.startPolling()
}

watch(subscriptionFeatureEnabled, (enabled) => {
  if (!authStore.isAuthenticated) return
  if (enabled) {
    startSubscriptionSync()
  } else {
    subscriptionStore.clear()
  }
})

watch(
  () => authStore.isAuthenticated,
  (isAuthenticated, oldValue) => {
    if (isAuthenticated) {
      if (authStore.isAdmin) {
        adminComplianceStore.fetchStatus().catch((error) => {
          console.error('Failed to fetch admin compliance status:', error)
        })
      }

      // User logged in: preload subscriptions and start polling (skipped when the
      // subscription feature is switched off; see the flag watcher below)
      if (subscriptionFeatureEnabled.value) {
        startSubscriptionSync()
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
      subscriptionStore.clear()
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
  <div v-if="!centralIdentityChanged" v-show="!centralChecking && !centralSessionError">
    <NavigationProgress />
    <RouterView />
    <Toast />
    <AnnouncementPopup />
    <AdminComplianceDialog />
  </div>
  <section v-if="centralAccountEnabled && (centralChecking || centralSessionError || centralIdentityChanged)" class="fixed inset-0 z-[100] grid place-content-center gap-4 bg-white p-6 text-gray-900 dark:bg-dark-900 dark:text-white" role="status" aria-live="polite" :aria-busy="centralChecking">
    <p>{{ centralSessionError || (centralIdentityChanged ? '账号身份已变化，正在重新加载…' : '正在检查账号会话…') }}</p>
    <button v-if="centralSessionError && !centralChecking && !centralIdentityChanged" type="button" class="btn btn-primary" @click="retryAccountCheck">重新检查</button>
  </section>
</template>
