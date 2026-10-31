<template>
  <div v-if="dirty" class="settings-save-bar" role="region" :aria-label="label" data-testid="settings-save-bar">
    <span role="status">{{ label }} · {{ t('consoleExperience.unsaved') }}</span>
    <div class="flex gap-2">
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="$emit('discard')">{{ t('consoleExperience.discard') }}</button>
      <button type="button" class="btn btn-primary" :disabled="busy || disabled" @click="$emit('save')">{{ t(busy ? 'common.saving' : 'common.save') }}</button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
withDefaults(defineProps<{ dirty: boolean; busy?: boolean; disabled?: boolean; label: string }>(), { busy: false, disabled: false })
defineEmits<{ save: []; discard: [] }>()
const { t } = useI18n()
</script>
<style scoped>
.settings-save-bar { position: sticky; bottom: 16px; z-index: 25; display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 16px; border: 1px solid var(--ritel-border); background: var(--ritel-bg); border-radius: 12px; box-shadow: 0 -4px 20px #0001; font-size: 14px; }
@media (max-width: 767px) { .settings-save-bar { bottom: calc(76px + env(safe-area-inset-bottom)); } }
</style>
