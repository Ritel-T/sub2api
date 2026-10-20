<template>
  <nav
    v-if="activeTabs.length > 1"
    class="console-section-tabs"
    :aria-label="activeLocation?.section?.label"
  >
    <div class="console-section-tabs-inner">
      <router-link
        v-for="tab in activeTabs"
        :key="tab.path"
        :to="tab.path"
        :id="tourId(tab.path, 'tabs')"
        :data-tour="tourAttr(tab.path, 'tabs')"
        class="console-tab-link"
        :class="{ 'console-tab-link-active': activeLocation?.tab === tab }"
        :aria-current="activeLocation?.tab === tab ? 'page' : undefined"
        @click="handleNavigate(tab.path)"
      >
        {{ tab.label }}
        <span
          v-if="tabBadge(tab)"
          class="console-nav-badge"
          :aria-label="shellCopy.unread(tabBadge(tab))"
        >{{ formatNavBadge(tabBadge(tab)) }}</span>
      </router-link>
    </div>
  </nav>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { consoleShellCopy, formatNavBadge } from '@/utils/consoleShellUi'
import { tabBadge } from '@/navigation/consoleNavigation'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'

const { activeLocation, handleNavigate, tourId, tourAttr } = useConsoleNav()
const { locale } = useI18n()
const shellCopy = computed(() => consoleShellCopy(locale.value))

const activeTabs = computed(() => activeLocation.value?.section?.tabs ?? [])

let frame = 0
function revealActiveTab() {
  cancelAnimationFrame(frame)
  frame = requestAnimationFrame(() => document.querySelector<HTMLElement>('.console-tab-link-active')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }))
}
watch(activeLocation, async () => { await nextTick(); revealActiveTab() })
onMounted(revealActiveTab)
onBeforeUnmount(() => cancelAnimationFrame(frame))

</script>

<style scoped>
.console-section-tabs {
  position: sticky;
  top: 60px;
  z-index: 40;
  height: 44px;
  background: var(--ritel-bg);
  border-bottom: 1px solid var(--ritel-border);
}
.console-section-tabs-inner {
  display: flex;
  align-items: stretch;
  gap: 0;
  height: 100%;
  max-width: calc(1232px + 64px);
  margin: 0 auto;
  padding: 0 32px;
  overflow-x: auto;
  scrollbar-width: none;
}
.console-section-tabs-inner::-webkit-scrollbar { display: none; }
@media (max-width: 1279px) { .console-section-tabs-inner { padding: 0 24px; } }
@media (max-width: 767px)  { .console-section-tabs-inner { padding: 0 16px; } }

.console-tab-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 2px;
  margin-right: 24px;
  font-size: 14px;
  font-weight: 400;
  color: var(--ritel-muted);
  text-decoration: none;
  white-space: nowrap;
  border-bottom: 2px solid transparent;
  transition: color 0.15s, border-color 0.15s;
}
.console-tab-link:hover { color: var(--ritel-fg); }
.console-tab-link-active {
  color: var(--ritel-fg);
  border-bottom-color: var(--ritel-fg);
}
</style>
