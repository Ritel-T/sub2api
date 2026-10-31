<template>
  <div class="mt-3 space-y-2">
    <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('consoleInline.manageSubscriptionsDynamicProxiesRegionRulesAnd_8fcbba') }}</p>
    <div class="flex flex-wrap gap-2">
      <router-link to="/admin/proxies#subscriptions" class="btn btn-secondary">{{ t('consoleInline.openIPManagement_183af7') }}</router-link>
      <button type="button" class="btn btn-secondary" :disabled="pending" @click="select">{{ t('consoleInline.useForTicketHarvesting_c3e5ec') }}</button>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
const emit = defineEmits<{ ready: [endpoint: string] }>()
const { t } = useI18n()
const pending = ref(false)
const error = ref('')
async function select() {
  pending.value = true
  error.value = ''
  try {
    const { data } = await apiClient.get<{ running: boolean; busy: boolean; nodes: number; endpoint: string }>('/admin/system/mihomo')
    if (!data.running || data.busy || !data.nodes || data.endpoint !== 'http://127.0.0.1:3101') {
      error.value = t('consoleInline.theKernelIsNotReadyConfigureAnd_ebf936')
      return
    }
    emit('ready', data.endpoint)
  } catch {
    error.value = t('consoleInline.cannotReadKernelStatusPleaseRetry_8b5dea')
  } finally { pending.value = false }
}
</script>
