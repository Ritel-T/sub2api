<template>
  <aside
    id="console-sidebar"
    ref="sidebarRef"
    class="sidebar console-drawer"
    :inert="!mobileOpen ? true : undefined"
    :aria-hidden="!mobileOpen ? 'true' : undefined"
    :role="mobileOpen ? 'dialog' : undefined"
    :aria-modal="mobileOpen ? 'true' : undefined"
    :aria-owns="mobileOpen && onboardingStore.isDriverActive() ? 'driver-popover-content' : undefined"
    :aria-label="navText.navigation"
    :class="{ 'console-drawer-open': mobileOpen }"
  >
    <div class="sidebar-header">
      <span class="sidebar-logo flex items-center justify-center">
        <img :src="siteLogo" alt="" class="ritel-logo h-full w-full object-contain" />
      </span>
      <div class="sidebar-brand">
        <span class="sidebar-brand-title">{{ siteName }}</span>
        <VersionBadge v-if="isAdmin" :version="siteVersion" />
      </div>
      <button type="button" class="sidebar-mobile-close" :aria-label="t('common.close')" @click="closeMobile">
        <Icon name="x" size="md" aria-hidden="true" />
      </button>
    </div>

    <div v-if="workspaceOptions.length > 1" class="console-drawer-workspace" role="group" :aria-label="navText.workspace">
      <button
        v-for="option in workspaceOptions"
        :key="option.value"
        type="button"
        class="console-segment"
        :aria-pressed="workspace === option.value ? 'true' : 'false'"
        @click="switchWorkspace(option.value)"
      >{{ option.label }}</button>
    </div>

    <div class="console-drawer-utilities">
      <ThemeSwitcher />
      <LocaleSwitcher />
      <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="console-drawer-docs">{{ t('nav.docs') }}</a>
    </div>

    <nav class="sidebar-nav" :aria-label="navText.navigation">
      <section v-for="section in sections" :key="section.key" class="console-drawer-section" :aria-labelledby="`console-drawer-${section.key}`">
        <AppHeading as="h2" variant="subtle" :id="`console-drawer-${section.key}`" class="sidebar-section-heading">{{ section.label }}</AppHeading>
        <router-link
          v-for="tab in section.tabs"
          :key="tab.path"
          :to="tab.path"
          :id="tourId(tab.path, 'drawer')"
          :data-tour="tourAttr(tab.path, 'drawer')"
          class="sidebar-link"
          :class="{ 'sidebar-link-active': activeLocation?.tab === tab }"
          :aria-current="activeLocation?.tab === tab ? 'page' : undefined"
          @click="handleNavigate(tab.path)"
        >
          <span v-if="tab.item.iconSvg" class="h-5 w-5 flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(tab.item.iconSvg)"></span>
          <component v-else-if="tab.item.icon" :is="tab.item.icon" class="h-5 w-5 flex-shrink-0" />
          <span class="sidebar-label">{{ tab.label }}</span>
          <span v-if="tabBadge(tab)" class="sidebar-nav-badge" data-testid="sidebar-nav-badge">{{ formatNavBadge(tabBadge(tab)) }}</span>
        </router-link>
      </section>
    </nav>
  </aside>

  <transition name="fade">
    <div v-if="mobileOpen" class="console-drawer-backdrop" aria-hidden="true" @click="closeMobile"></div>
  </transition>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import VersionBadge from '@/components/common/VersionBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import { sanitizeUrl } from '@/utils/url'
import { sanitizeSvg } from '@/utils/sanitize'
import { BRAND_LOGO, BRAND_NAME } from '@/utils/branding'
import { tabBadge } from '@/navigation/consoleNavigation'
import { formatNavBadge } from '@/utils/consoleShellUi'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const {
  navText, workspace, workspaceOptions, sections, activeLocation,
  switchWorkspace, handleNavigate, tourId, tourAttr,
} = useConsoleNav()

const sidebarRef = ref<HTMLElement | null>(null)
const mobileOpen = computed(() => appStore.mobileOpen)
const isAdmin = computed(() => authStore.isAdmin)
const siteName = BRAND_NAME
const siteLogo = BRAND_LOGO
const siteVersion = computed(() => appStore.siteVersion)
let mobileOpener: HTMLElement | null = null
let originalOverflow: string | undefined
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
function releaseScroll() {
  if (originalOverflow !== undefined) document.body.style.overflow = originalOverflow
  originalOverflow = undefined
}

function closeMobile() { appStore.setMobileOpen(false) }
function mobileFocusTargets(): HTMLElement[] {
  const selector = 'a[href], button:not([disabled]), select:not([disabled]), [tabindex="0"]'
  const popover = onboardingStore.isDriverActive() ? document.getElementById('driver-popover-content') : null
  return [...(sidebarRef.value?.querySelectorAll<HTMLElement>(selector) ?? []), ...(popover?.querySelectorAll<HTMLElement>(selector) ?? [])]
    .filter(element => !element.closest('[aria-hidden="true"]'))
}
function handleViewportResize() {
  if (mobileOpen.value && window.innerWidth >= 1024) closeMobile()
}
function handleMobileKeydown(event: KeyboardEvent) {
  if (!mobileOpen.value || document.querySelector('[role="dialog"][aria-modal="true"]:not(.sidebar)')) return
  if (event.key === 'Escape') {
    if (onboardingStore.isDriverActive()) return
    event.preventDefault(); closeMobile(); return
  }
  if (event.key !== 'Tab') return
  const targets = mobileFocusTargets()
  const first = targets[0]
  const last = targets[targets.length - 1]
  if (!first || !last) return
  const inside = targets.includes(document.activeElement as HTMLElement)
  if (event.shiftKey && (document.activeElement === first || !inside)) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (document.activeElement === last || !inside)) { event.preventDefault(); first.focus() }
}
watch(mobileOpen, async (open, wasOpen) => {
  if (open) {
    if (originalOverflow === undefined) originalOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    mobileOpener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    await nextTick()
    const close = sidebarRef.value?.querySelector<HTMLElement>('.sidebar-mobile-close')
    ;(close ?? mobileFocusTargets()[0])?.focus()
  } else if (wasOpen) {
    releaseScroll()
    await nextTick()
    const fallback = document.querySelector<HTMLElement>('header button[aria-label]')
    if (mobileOpener?.isConnected && mobileOpener !== document.body && !sidebarRef.value?.contains(mobileOpener)) mobileOpener.focus()
    else fallback?.focus()
    mobileOpener = null
  }
}, { immediate: true })

onMounted(() => {
  document.addEventListener('keydown', handleMobileKeydown)
  window.addEventListener('resize', handleViewportResize)
})
onBeforeUnmount(() => {
  releaseScroll()
  document.removeEventListener('keydown', handleMobileKeydown)
  window.removeEventListener('resize', handleViewportResize)
})
</script>

<style scoped>
.console-drawer {
  z-index: 80;
  width: min(320px, 86vw);
  transform: translateX(-100%);
  visibility: hidden;
  transition: transform 0.24s ease, visibility 0s linear 0.24s;
}
.console-drawer-open {
  transform: none;
  visibility: visible;
  transition: transform 0.24s ease;
}
.sidebar-brand {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  align-items: center;
  gap: 10px;
  white-space: nowrap;
}
.sidebar-brand-title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sidebar-mobile-close {
  display: inline-flex;
  flex: 0 0 auto;
  width: 44px;
  height: 44px;
  align-items: center;
  justify-content: center;
  color: var(--ritel-muted);
}
.sidebar-mobile-close:hover { color: var(--ritel-fg); background: var(--theme-hover); }
.console-drawer-workspace {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: 1fr;
  gap: 4px;
  margin: 16px 16px 0;
  padding: 4px;
  border: 1px solid var(--ritel-border);
  border-radius: var(--ritel-radius-pill);
}
.console-drawer-section + .console-drawer-section { margin-top: 24px; }
.sidebar-section-heading { margin-bottom: 8px; padding: 0 12px; color: var(--ritel-muted); }
.sidebar-link { position: relative; margin-bottom: 2px; }
.sidebar-label {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sidebar-nav-badge {
  margin-left: auto;
  flex-shrink: 0;
  min-width: 1.375rem;
  height: 1.375rem;
  padding: 0 0.375rem;
  border-radius: var(--ritel-radius-pill, 999px);
  background: #e08b78;
  color: #0a0a0a;
  font-size: 12px;
  font-weight: 600;
  line-height: 1.375rem;
  text-align: center;
  font-variant-numeric: lining-nums tabular-nums;
}
@media (prefers-reduced-motion: reduce) {
  .console-drawer, .console-drawer-open { transition: none; }
}

/* Custom SVG icon in sidebar: constrain size without overriding uploaded SVG colors */
.sidebar-svg-icon {
  color: currentColor;
}

.sidebar-svg-icon :deep(svg) {
  display: block;
  width: 1.25rem;
  height: 1.25rem;
}
.console-drawer-backdrop { position: fixed; inset: 0; z-index: 70; background: #0009; }
.console-drawer-utilities { display: flex; align-items: center; justify-content: space-between; padding: 12px 24px 0; }
.console-drawer-docs { font-size: 14px; color: var(--ritel-muted); }
</style>
