<template>
  <nav class="console-bottom-nav" :aria-label="navText.navigation">
    <router-link
      v-for="section in bottomSections"
      :key="section.key"
      :to="section.path"
      class="console-bottom-item"
      :class="{ 'console-bottom-item-active': activeLocation?.section === section }"
      :aria-current="activeLocation?.section === section ? 'page' : undefined"
      @click="handleNavigate(section.path)"
    >
      <span class="console-bottom-icon" aria-hidden="true">
        <component :is="sectionIcon(section.key)" class="h-5 w-5" />
      </span>
      <span class="console-bottom-label">{{ section.label }}</span>
      <span
        v-if="sectionBadge(section)"
        class="console-bottom-badge"
        :aria-label="shellCopy.unread(sectionBadge(section))"
      ></span>
    </router-link>
    <button
      type="button"
      class="console-bottom-item"
      :aria-label="navText.sections.more"
      :aria-expanded="appStore.mobileOpen"
      aria-controls="console-sidebar"
      @click="appStore.setMobileOpen(true)"
    >
      <span class="console-bottom-icon" aria-hidden="true">
        <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6.75h16.5M3.75 12h16.5m-16.5 5.25h16.5"/>
        </svg>
      </span>
      <span class="console-bottom-label">{{ navText.sections.more }}</span>
    </button>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { consoleShellCopy } from '@/utils/consoleShellUi'
import { useAppStore } from '@/stores'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'
import { sectionBadge } from '@/navigation/consoleNavigation'
import {
  DashboardIcon, KeyIcon, ChartIcon, CreditCardIcon, SupportTicketIcon,
  GlobeIcon, UsersIcon, OpsMonitoringIcon, OrderIcon, CogIcon,
} from '@/navigation/consoleNavIcons'

const appStore = useAppStore()
const { locale } = useI18n()
const shellCopy = computed(() => consoleShellCopy(locale.value))
const { navText, sections, activeLocation, handleNavigate } = useConsoleNav()

const bottomSections = computed(() => sections.value.filter(s => !s.hidden).slice(0, 4))

const iconMap: Record<string, unknown> = {
  overview: DashboardIcon,
  access: KeyIcon,
  usage: ChartIcon,
  billing: CreditCardIcon,
  support: SupportTicketIcon,
  resources: GlobeIcon,
  users: UsersIcon,
  operations: OpsMonitoringIcon,
  revenue: OrderIcon,
  system: CogIcon,
}

function sectionIcon(key: string): unknown {
  return iconMap[key] ?? DashboardIcon
}
</script>

<style scoped>
.console-bottom-nav {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 50;
  height: calc(56px + env(safe-area-inset-bottom));
  padding-bottom: env(safe-area-inset-bottom);
  display: flex;
  align-items: stretch;
  background: #0a0a0a;
  border-top: 1px solid #30302d;
}
.console-bottom-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  position: relative;
  color: var(--ritel-muted);
  text-decoration: none;
  transition: color 0.15s;
  min-width: 0;
  padding: 6px 4px;
}
.console-bottom-item:hover { color: var(--ritel-fg); }
.console-bottom-item-active { color: var(--ritel-fg); }
.console-bottom-icon { display: flex; align-items: center; justify-content: center; }
.console-bottom-label {
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.console-bottom-badge {
  position: absolute;
  top: 6px;
  right: calc(50% - 18px);
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #e08b78;
}
@media (min-width: 761px) {
  .console-bottom-nav { display: none; }
}
</style>
