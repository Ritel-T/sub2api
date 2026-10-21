<template>
  <nav :aria-label="t('accountOps.smartTitle')" class="mb-6 flex flex-wrap gap-x-6 border-b border-gray-200 dark:border-dark-700">
    <RouterLink v-for="item in items" :key="item.path" :to="item.path" v-slot="{ isActive }" class="border-b-2 py-3 text-sm transition-colors" :class="route.path === item.path ? 'border-current font-medium text-gray-900 dark:text-gray-100' : 'border-transparent text-gray-500 hover:text-gray-900 dark:hover:text-gray-100'"><span :aria-current="isActive ? 'page' : undefined">{{ t(item.label) }}</span></RouterLink>
  </nav>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore } from '@/stores/adminSettings'
const { t } = useI18n(), route = useRoute(), adminSettingsStore = useAdminSettingsStore()
// Keep the order in sync with the 智能运维 group in navigation/useConsoleNavSource.ts.
const items = computed(() => [{ path: '/admin/auto-config', label: 'autoConfig.title' }, { path: '/admin/priority-scheduling', label: 'priorityScheduling.title' }, { path: '/admin/account-quality', label: 'qualityOps.title' }, { path: '/admin/controlled-experiments', label: 'controlledExperiments.title' }, { path: '/admin/account-ops', label: 'accountOps.title' }, { path: '/admin/token-guard', label: 'tokenGuard.title' }, { path: '/admin/token-guard-v2', label: 'tokenGuardV2.title' }, { path: '/admin/pelican-tests', label: 'pelicanTests.title' }, { path: '/admin/group-cache-tests', label: 'admin.groupCacheTest.title' }, ...(adminSettingsStore.requestCaptureEnabled ? [{ path: '/admin/request-captures', label: 'admin.requestCapture.title' }] : []), { path: '/admin/harvest-flow', label: 'nav.harvestFlow' }])
</script>
