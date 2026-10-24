<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-4">
      <div class="min-w-0">
        <AppHeading as="h3" variant="subsection" class="text-gray-900 dark:text-white">{{ t('admin.settings.gatewayForwarding.requestTimezoneEnabled') }}</AppHeading>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.requestTimezoneEnabledDesc') }}</p>
      </div>
      <Toggle :aria-label="t('admin.settings.gatewayForwarding.requestTimezoneEnabled')" id="request-timezone-enabled" :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" />
    </div>
    <div v-if="modelValue" class="space-y-2">
      <label for="request-timezone-mode" class="block text-sm font-medium">{{ t('admin.settings.gatewayForwarding.requestTimezoneMode') }}</label>
      <select id="request-timezone-mode" class="input" :value="auto ? 'auto' : 'manual'" @change="$emit('update:auto', ($event.target as HTMLSelectElement).value === 'auto')">
        <option value="manual">{{ t('admin.settings.gatewayForwarding.requestTimezoneManual') }}</option>
        <option value="auto">{{ t('admin.settings.gatewayForwarding.requestTimezoneAuto') }}</option>
      </select>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.requestTimezoneModeDesc') }}</p>
    </div>
  </div>
</template>
<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
defineProps<{ modelValue: boolean; auto?: boolean }>()
defineEmits<{ (e: 'update:modelValue', value: boolean): void; (e: 'update:auto', value: boolean): void }>()
const { t } = useI18n()
</script>
