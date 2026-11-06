<template>
  <header class="console-top-bar" :class="{ 'console-top-bar-compact': compactNav }" :inert="mobileOpen ? true : undefined" role="banner">
    <div ref="barRef" class="console-top-bar-inner">
      <div ref="leftRef" class="console-top-bar-left">
        <button
          type="button"
          v-show="compactNav"
          ref="menuRef"
          class="console-icon-btn"
          :aria-label="t('common.toggleMenu')"
          :aria-expanded="mobileOpen"
          aria-controls="console-sidebar"
          @click="appStore.toggleMobileSidebar()"
        >
          <Icon name="menu" size="md" aria-hidden="true" />
        </button>
        <router-link :to="homePath" class="console-wordmark" :aria-label="siteName" @click="handleNavigate(homePath)">
          <img :src="siteLogo" alt="" class="ritel-logo console-wordmark-logo" />
          <span class="console-wordmark-name"><span v-if="brandName.brand" class="ritel-brand-mark">{{ brandName.brand }}</span><span v-if="brandName.suffix" class="ritel-brand-suffix">{{ brandName.suffix }}</span></span>
        </router-link>

        <div
          v-if="workspaceOptions.length > 1"
          id="console-workspace"
          role="group"
          :aria-label="navText.workspace"
          class="console-workspace-switch"
        >
          <button
            v-for="option in workspaceOptions"
            :key="option.value"
            type="button"
            class="console-segment"
            :aria-pressed="workspace === option.value ? 'true' : 'false'"
            @click="switchWorkspace(option.value)"
          >{{ option.label }}</button>
        </div>
      </div>
      <nav
        v-if="sections.length"
        ref="sectionRef"
        class="console-section-nav"
        :inert="compactNav ? true : undefined"
        :aria-hidden="compactNav ? 'true' : undefined"
        :aria-label="navText.navigation"
      >
        <router-link
          v-for="section in visibleSections"
          :key="section.key"
          :to="section.path"
          class="console-section-link"
          :class="{ 'console-section-link-active': activeLocation?.section === section }"
          :aria-current="activeLocation?.section === section ? 'page' : undefined"
          @click="handleNavigate(section.path)"
        >
          {{ section.label }}
          <span
            v-if="sectionBadge(section)"
            class="console-nav-badge"
            :aria-label="shellCopy.unread(sectionBadge(section))"
          >{{ formatNavBadge(sectionBadge(section)) }}</span>
        </router-link>
        <template v-if="workspace === 'personal'">
          <a v-for="link in consoleProductLinks" :key="link.href" :href="link.href" target="_blank" rel="noopener noreferrer" class="console-section-link">{{ link.label }}</a>
        </template>
      </nav>
      <div ref="rightRef" class="console-top-bar-right">
        <FeatureSearch
          :items="searchItems"
          :collapsed="true"
          @navigate="handleNavigate"
        />
        <span v-if="compactNav" ref="captionRef" class="console-mobile-section">{{ activeLocation?.section?.label }}</span>
        <AnnouncementBell v-if="user" />
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="console-icon-btn console-docs-link"
          :aria-label="t('nav.docs')"
        >
          <Icon name="book" size="sm" aria-hidden="true" />
        </a>
        <ThemeSwitcher class="console-theme" />
        <LocaleSwitcher class="console-locale" />
        <SubscriptionProgressMini
          v-if="showBalance && workspace === 'personal' && subscriptionFeatureEnabled"
        />
        <router-link
          v-if="showBalance && workspace === 'personal'"
          to="/purchase"
          class="console-balance"
          :title="t('nav.recharge')"
          @click="handleNavigate('/purchase')"
        >
          <span class="console-balance-amount" :aria-label="`${shellCopy.available}: ${formatMoney(availableBalance)}`">{{ formatMoney(availableBalance) }}</span>
          <span v-if="frozenBalance > 0" class="console-balance-frozen">{{ shellCopy.frozen }} {{ formatMoney(frozenBalance) }}</span>
        </router-link>
        <div v-if="user" class="relative" ref="dropdownRef">
          <button
            type="button"
            class="console-avatar-btn"
            :aria-label="t('common.userMenu')"
            :aria-expanded="dropdownOpen"
            @click="toggleDropdown"
          >
            <div class="console-avatar">
              <img v-if="avatarUrl" :src="avatarUrl" :alt="displayName" class="h-full w-full object-cover" />
              <span v-else>{{ userInitials }}</span>
            </div>
          </button>

          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown console-dropdown" @keydown.esc.stop.prevent="closeDropdownAndFocus">
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="text-sm font-medium text-gray-900 dark:text-white">{{ displayName }}</div>
                <div class="text-xs text-gray-500 dark:text-dark-400">{{ user.email }}</div>
                <VersionBadge v-if="isAdmin" :version="siteVersion" class="mt-2" />
              </div>

              <div class="py-1">
                <a :href="accountPageURL()" class="dropdown-item" @click="closeDropdown">{{ t('consoleExperience.accountLink') }}</a>
              </div>

              <div v-if="contactInfo" class="border-t border-gray-100 px-4 py-2.5 dark:border-dark-700">
                <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t('common.contactSupport') }}:</span>
                  <span class="font-medium text-gray-700 dark:text-gray-300">{{ contactInfo }}</span>
                </div>
              </div>

              <div class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button
                  type="button"
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                  :disabled="logoutPending"
                  @click="handleLogout"
                >
                  <svg class="h-4 w-4 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75"/></svg>
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { SETTINGS_LEAVE_GUARD } from '@/composables/useSettingsDrafts'
import { formatMoney } from '@/utils/consoleDisplay'
import { accountPageURL, centralAccountEnabled } from '@/utils/accountConfig'
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import VersionBadge from '@/components/common/VersionBadge.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import FeatureSearch from './FeatureSearch.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { BRAND_LOGO, BRAND_NAME, splitBrandName } from '@/utils/branding'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'
import { consoleShellCopy, formatNavBadge } from '@/utils/consoleShellUi'
import { consoleProductLinks, sectionBadge } from '@/navigation/consoleNavigation'

const { t, locale } = useI18n()
const shellCopy = computed(() => consoleShellCopy(locale.value))
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

const {
  navText, sections, activeLocation, workspaceOptions, workspace,
  searchItems, homePath, switchWorkspace, handleNavigate,
} = useConsoleNav()

const siteName = computed(() => appStore.siteName || BRAND_NAME)
const brandName = computed(() => splitBrandName(siteName.value))
const siteLogo = BRAND_LOGO
const siteVersion = computed(() => appStore.siteVersion)
const isAdmin = computed(() => authStore.isAdmin)
const user = computed(() => authStore.user)
const mobileOpen = computed(() => appStore.mobileOpen)
const contactInfo = computed(() => appStore.contactInfo)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const showBalance = computed(() => !!user.value && !authStore.isObserver && !appStore.backendModeEnabled)
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

const visibleSections = computed(() => sections.value.filter(s => !s.hidden))

const compactNav = ref(true)
const barRef = ref<HTMLElement | null>(null)
const leftRef = ref<HTMLElement | null>(null)
const sectionRef = ref<HTMLElement | null>(null)
const rightRef = ref<HTMLElement | null>(null)
const menuRef = ref<HTMLElement | null>(null)
const captionRef = ref<HTMLElement | null>(null)
let resizeObserver: ResizeObserver | undefined
let navObserver: MutationObserver | undefined
let measureFrame = 0

function measureNavigation() {
  const bar = barRef.value, left = leftRef.value, nav = sectionRef.value, right = rightRef.value
  if (!bar || !left || !nav || !right) return
  const style = getComputedStyle(bar)
  const gap = parseFloat(style.columnGap) || 0
  const padding = (parseFloat(style.paddingLeft) || 0) + (parseFloat(style.paddingRight) || 0)
  // Exclude compact-only controls from both groups so showing the menu cannot
  // change the fit decision and cause a collapse/expand loop.
  const leftGap = parseFloat(getComputedStyle(left).columnGap) || 0
  const utilityGap = parseFloat(getComputedStyle(right).columnGap) || 0
  const menuWidth = menuRef.value?.offsetWidth || 0
  const captionWidth = captionRef.value?.offsetWidth || 0
  const leftWidth = left.offsetWidth - (menuWidth ? menuWidth + leftGap : 0)
  const rightWidth = right.offsetWidth - (captionWidth ? captionWidth + utilityGap : 0)
  const available = bar.clientWidth - padding - leftWidth - rightWidth - gap * 2
  const links = [...nav.querySelectorAll<HTMLElement>('.console-section-link')]
  const linkGap = parseFloat(getComputedStyle(nav).columnGap) || 0
  const required = links.reduce((sum, link) => sum + link.getBoundingClientRect().width, 0) + Math.max(0, links.length - 1) * linkGap
  compactNav.value = window.innerWidth < 1024 || required + 8 > available
}
function scheduleMeasure() {
  cancelAnimationFrame(measureFrame)
  measureFrame = requestAnimationFrame(measureNavigation)
}
watch([locale, sections, workspace, siteName], async () => { await nextTick(); scheduleMeasure() })


const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

const userInitials = computed(() => displayName.value.slice(0, 2).toUpperCase())

const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

function toggleDropdown() { dropdownOpen.value = !dropdownOpen.value }
function closeDropdown() { dropdownOpen.value = false }
function closeDropdownAndFocus() { closeDropdown(); dropdownRef.value?.querySelector<HTMLButtonElement>('button')?.focus() }

const settingsLeaveGuard = inject(SETTINGS_LEAVE_GUARD, null)
const logoutPending = ref(false)
async function handleLogout() {
  if (logoutPending.value) return
  if (settingsLeaveGuard && !(await settingsLeaveGuard())) return
  closeDropdown()
  logoutPending.value = true
  try {
    await authStore.logout()
    if (!centralAccountEnabled) await router.push('/login')
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '退出结果未确认，请重新检查会话。')
  } finally { logoutPending.value = false }
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) closeDropdown()
}

watch(() => router.currentRoute.value.fullPath, closeDropdown)

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  window.addEventListener('resize', scheduleMeasure)
  resizeObserver = new ResizeObserver(scheduleMeasure)
  for (const element of [barRef.value, leftRef.value, rightRef.value, sectionRef.value]) {
    if (element) resizeObserver.observe(element)
  }
  navObserver = new MutationObserver(scheduleMeasure)
  if (sectionRef.value) navObserver.observe(sectionRef.value, { childList: true, subtree: true, characterData: true })
  void document.fonts?.ready.then(scheduleMeasure)
  scheduleMeasure()
})
onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  window.removeEventListener('resize', scheduleMeasure)
  resizeObserver?.disconnect()
  navObserver?.disconnect()
  cancelAnimationFrame(measureFrame)
})
</script>

<style scoped>
.console-top-bar {
  position: sticky;
  top: 0;
  z-index: 50;
  height: 60px;
  background: color-mix(in srgb, var(--ritel-bg) 95%, transparent);
  border-bottom: 1px solid var(--ritel-border);
  backdrop-filter: blur(12px);
}
.console-top-bar-inner {
  display: flex;
  align-items: center;
  gap: 16px;
  height: 100%;
  max-width: var(--console-content-width, 1296px);
  margin: 0 auto;
  padding: 0 clamp(24px, 3vw, 64px);
}
@media (max-width: 767px)  { .console-top-bar-inner { padding: 0 20px; } }

.console-top-bar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}
.console-wordmark {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  text-decoration: none;
  color: var(--ritel-fg);
}
.console-wordmark-logo {
  width: 24px;
  height: 24px;
  object-fit: contain;
  flex-shrink: 0;
}
.console-wordmark-name {
  font-family: var(--ritel-heading);
  font-size: 18px;
  font-weight: 400;
  letter-spacing: -.025em;
  white-space: nowrap;
}
.console-top-bar-compact .console-top-bar-left {
  flex: 0 1 auto;
  min-width: 0;
}
.console-top-bar-compact .console-wordmark {
  min-width: 0;
}
.console-top-bar-compact .console-wordmark-name {
  overflow: hidden;
  text-overflow: ellipsis;
}
.console-top-bar-compact .console-top-bar-left > .console-icon-btn {
  flex-shrink: 0;
}

.console-workspace-switch {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border: 1px solid var(--ritel-border);
  border-radius: var(--ritel-radius-pill);
}

.console-section-nav {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 2px;
  min-width: 0;
  overflow: visible;
}
.console-top-bar-compact .console-section-nav {
  position: absolute;
  width: 0;
  overflow: hidden;
  visibility: hidden;
  pointer-events: none;
}
.console-section-link {
  flex: 0 0 auto;
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 6px 10px;
  border-radius: var(--ritel-radius-control);
  font-size: 14px;
  font-weight: 400;
  color: var(--ritel-muted);
  text-decoration: none;
  white-space: nowrap;
  transition: color 0.15s, background 0.15s;
}
.console-section-link:hover { color: var(--ritel-fg); background: var(--theme-hover); }
.console-section-link-active { color: var(--ritel-fg) !important; }
.console-section-link-active::after {
  content: '';
  position: absolute;
  bottom: -1px;
  left: 10px;
  right: 10px;
  height: 2px;
  background: var(--ritel-fg);
  border-radius: 1px;
}

.console-top-bar-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  margin-left: auto;
}
.console-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: var(--ritel-radius-control);
  color: var(--ritel-muted);
  transition: color 0.15s, background 0.15s;
}
.console-icon-btn:hover { color: var(--ritel-fg); background: var(--theme-hover); }
.console-mobile-section {
  font-size: 14px;
  font-weight: 500;
  color: var(--ritel-fg);
  margin: 0 4px;
}
.console-balance {
  display: flex;
  align-items: baseline;
  gap: 4px;
  padding: 4px 10px;
  border-radius: var(--ritel-radius-pill);
  background: var(--theme-surface);
  border: 1px solid var(--ritel-border);
}
.console-balance:hover { background: var(--theme-hover); }
.console-balance-amount {
  font-size: 13px;
  font-weight: 600;
  color: var(--ritel-focus);
  font-variant-numeric: lining-nums tabular-nums;
}
.console-balance-frozen {
  font-size: 12px;
  color: var(--ritel-muted);
}
.console-avatar-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--ritel-radius-pill);
  padding: 2px;
  transition: background 0.15s;
}
.console-avatar-btn:hover { background: var(--theme-hover); }
.console-avatar {
  width: 30px;
  height: 30px;
  border-radius: var(--ritel-radius-pill);
  background: var(--theme-control-fill);
  color: var(--theme-on-control);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 600;
  overflow: hidden;
  flex-shrink: 0;
}
.console-dropdown {
  position: absolute;
  right: 0;
  top: calc(100% + 8px);
  min-width: 200px;
  max-width: calc(100vw - 32px);
  max-height: calc(100dvh - 80px);
  overflow-y: auto;
}

.console-docs-link { display: none; }
@media (min-width: 1280px) { .console-docs-link { display: inline-flex; } }
@media (max-width: 1279px) { .console-balance { display: none; } }
@media (max-width: 1023px) { .console-workspace-switch { display: none; } }
@media (max-width: 639px) { .console-theme, .console-locale, .console-mobile-section { display: none; } }
@media (max-width: 359px) { .console-wordmark-name { display: none; } }
</style>
