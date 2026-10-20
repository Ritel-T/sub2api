<template>
  <label class="theme-switcher" :title="`${t('common.theme.label')}: ${t(`common.theme.${preference}`)}`">
    <svg class="theme-switcher-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
      <template v-if="preference === 'system'">
        <rect x="3" y="4" width="18" height="13" rx="2" />
        <path d="M8 21h8m-4-4v4" />
      </template>
      <template v-else-if="preference === 'light'">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" />
      </template>
      <path v-else d="M20.8 13.5A9 9 0 0 1 10.5 3.2 9 9 0 1 0 20.8 13.5Z" />
    </svg>
    <select :value="preference" :aria-label="t('common.theme.label')" @change="changeTheme">
      <option value="system">{{ t('common.theme.system') }}</option>
      <option value="light">{{ t('common.theme.light') }}</option>
      <option value="dark">{{ t('common.theme.dark') }}</option>
    </select>
  </label>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useTheme, type ThemePreference } from '@/composables/useTheme'

const { t } = useI18n()
const { preference, setTheme } = useTheme()
function changeTheme(event: Event) {
  setTheme((event.target as HTMLSelectElement).value as ThemePreference)
}
</script>

<style scoped>
.theme-switcher {
  position: relative;
  display: inline-flex;
  flex: 0 0 36px;
  width: 36px;
  height: 36px;
  align-items: center;
  justify-content: center;
  color: var(--ritel-muted);
  border-radius: var(--ritel-radius-control);
}
.theme-switcher:hover { color: var(--ritel-fg); background: var(--theme-hover); }
.theme-switcher:focus-within { outline: 2px solid var(--ritel-focus); outline-offset: 2px; }
.theme-switcher-icon { width: 18px; height: 18px; pointer-events: none; }
.theme-switcher select {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  opacity: 0;
  cursor: pointer;
}
</style>
