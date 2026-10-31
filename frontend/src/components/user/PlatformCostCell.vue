<template>
  <div v-if="usage" class="text-sm">
    <div class="flex items-center gap-1.5">
      <span class="text-gray-500 dark:text-gray-400">{{ t('admin.users.today') }}:</span>
      <span class="font-medium text-gray-900 dark:text-white">${{ formatDisplayAmount(usage.today_actual_cost, 2) }}</span>
    </div>
    <div class="mt-0.5 flex items-center gap-1.5">
      <span class="text-gray-500 dark:text-gray-400">{{ t('admin.users.total') }}:</span>
      <span class="font-medium text-gray-900 dark:text-white">${{ formatDisplayAmount(usage.total_actual_cost, 2) }}</span>
    </div>
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-gray-500">—</span>
</template>

<script setup lang="ts">
import { formatAmount as formatDisplayAmount } from '@/utils/consoleDisplay'

import { useI18n } from 'vue-i18n'
import type { PlatformUsage } from '@/api/admin/dashboard'

defineProps<{
  usage?: PlatformUsage
}>()

const { t } = useI18n()
</script>
