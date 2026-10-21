<template>
  <header class="console-top-bar" :class="{ 'console-top-bar-compact': compactNav }" :inert="mobileOpen ? true : undefined" role="banner">
    <div ref="barRef" class="console-top-bar-inner">
      <div ref="leftRef" class="console-top-bar-left">
        <router-link :to="homePath" class="console-wordmark" :aria-label="siteName" @click="handleNavigate(homePath)">
          <img :src="siteLogo" alt="" class="ritel-logo console-wordmark-logo" />
          <span class="console-wordmark-name">{{ siteName }}</span>
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
      </nav>
      <div ref="rightRef" class="console-top-bar-right">
        <FeatureSearch
          :items="searchItems"
          :collapsed="true"
          @navigate="handleNavigate"
        />
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
          v-if="user && !authStore.isObserver && !appStore.backendModeEnabled && subscriptionFeatureEnabled"
        />
        <div
          v-if="showBalance && workspace === 'personal'"
          class="console-balance"
        >
          <span class="console-balance-amount" :aria-label="`${shellCopy.available}: ${formatMoney(availableBalance)}`">{{ formatMoney(availableBalance) }}</span>
          <span v-if="frozenBalance > 0" class="console-balance-frozen">{{ shellCopy.frozen }} {{ formatMoney(frozenBalance) }}</span>
        </div>
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
              <dl v-if="showBalance" class="console-menu-balances border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div><dt>{{ shellCopy.available }}</dt><dd>{{ formatMoney(availableBalance) }}</dd></div>
                <div v-if="frozenBalance > 0"><dt>{{ shellCopy.frozen }}</dt><dd>{{ formatMoney(frozenBalance) }}</dd></div>
                <div v-if="frozenBalance > 0"><dt>{{ shellCopy.total }}</dt><dd>{{ formatMoney(availableBalance + frozenBalance) }}</dd></div>
              </dl>

              <div class="py-1">
                <router-link
                  v-if="!appStore.backendModeEnabled || authStore.isAdmin"
                  to="/profile"
                  class="dropdown-item"
                  @click="closeDropdown"
                >
                  <Icon name="user" size="sm" />
                  {{ t('nav.profile') }}
                </router-link>
                <router-link
                  v-if="!authStore.isObserver && (!appStore.backendModeEnabled || authStore.isAdmin)"
                  to="/keys"
                  class="dropdown-item"
                  @click="closeDropdown"
                >
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>
                <a
                  v-if="authStore.isAdmin"
                  href="https://github.com/Wei-Shaw/sub2api"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="dropdown-item"
                  @click="closeDropdown"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path fill-rule="evenodd" clip-rule="evenodd" d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"/>
                  </svg>
                  {{ t('nav.github') }}
                </a>
              </div>

              <div v-if="contactInfo" class="border-t border-gray-100 px-4 py-2.5 dark:border-dark-700">
                <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t('common.contactSupport') }}:</span>
                  <span class="font-medium text-gray-700 dark:text-gray-300">{{ contactInfo }}</span>
                </div>
              </div>

              <div v-if="showOnboardingButton" class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button type="button" class="dropdown-item w-full" @click="handleReplayGuide">
                  <Icon name="infoCircle" size="sm" />
                  {{ t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button
                  type="button"
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
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
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import VersionBadge from '@/components/common/VersionBadge.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import FeatureSearch from './FeatureSearch.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { BRAND_LOGO, BRAND_NAME } from '@/utils/branding'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'
import { consoleShellCopy, formatNavBadge } from '@/utils/consoleShellUi'
import { sectionBadge } from '@/navigation/consoleNavigation'

const { t, locale } = useI18n()
const shellCopy = computed(() => consoleShellCopy(locale.value))
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()

const {
  navText, sections, activeLocation, workspaceOptions, workspace,
  searchItems, homePath, switchWorkspace, handleNavigate,
} = useConsoleNav()

const siteName = BRAND_NAME
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
const showOnboardingButton = computed(() => !authStore.isSimpleMode && user.value?.role === 'admin')

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
  // The compact-only trigger and caption must not change the fit decision.
  const utilityGap = parseFloat(getComputedStyle(right).columnGap) || 0
  const compactWidth = [menuRef.value, captionRef.value].reduce((sum, el) =>
    sum + (el && el.offsetWidth ? el.offsetWidth + utilityGap : 0), 0)
  const available = bar.clientWidth - padding - left.offsetWidth - (right.offsetWidth - compactWidth) - gap * 2
  const links = [...nav.querySelectorAll<HTMLElement>('.console-section-link')]
  const linkGap = parseFloat(getComputedStyle(nav).columnGap) || 0
  const required = links.reduce((sum, link) => sum + link.getBoundingClientRect().width, 0) + Math.max(0, links.length - 1) * linkGap
  compactNav.value = window.innerWidth < 1024 || required + 8 > available
}
function scheduleMeasure() {
  cancelAnimationFrame(measureFrame)
  measureFrame = requestAnimationFrame(measureNavigation)
}
watch([locale, sections, workspace], async () => { await nextTick(); scheduleMeasure() })

function formatMoney(value: number) {
  if (!Number.isFinite(value)) return '$0.00'
  return `$${value.toFixed(2)}`
}

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

async function handleLogout() {
  closeDropdown()
  try { await authStore.logout() } catch { /* ignore */ }
  await router.push('/login')
}

function handleReplayGuide() {
  closeDropdown()
  onboardingStore.replay()
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

.console-menu-balances { font-size: 12px; }
.console-menu-balances > div { display: flex; justify-content: space-between; gap: 16px; margin: 4px 0; }
.console-menu-balances dt { color: var(--ritel-muted); }
.console-menu-balances dd { font-variant-numeric: lining-nums tabular-nums; color: var(--ritel-focus); }
.console-docs-link { display: none; }
@media (min-width: 1280px) { .console-docs-link { display: inline-flex; } }
@media (max-width: 1279px) { .console-balance { display: none; } }
@media (max-width: 1023px) { .console-workspace-switch { display: none; } }
@media (max-width: 639px) { .console-theme, .console-locale, .console-mobile-section { display: none; } }
@media (max-width: 359px) { .console-wordmark-name { display: none; } }
</style>
