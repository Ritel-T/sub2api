<template>
  <div class="workspace-row-actions">
    <button :title="t('admin.channelMonitor.runNow')" :aria-label="t('admin.channelMonitor.runNow')"
      @click="$emit('run', row)"
      :disabled="running"
      class="text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400 workspace-icon-action"
    >
      <Icon name="refresh" size="sm" :class="running ? 'animate-spin' : ''" />
      <span class="sr-only">{{ t('admin.channelMonitor.runNow') }}</span>
    </button>
    <button :aria-label="duplicateTitle"
      data-testid="monitor-duplicate"
      :title="duplicateTitle"
      :disabled="duplicating || Boolean(row.api_key_decrypt_failed)"
      @click="$emit('duplicate', row)"
      class="text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-primary-400 workspace-icon-action"
    >
      <Icon name="copy" size="sm" />
      <span class="sr-only">
        {{ duplicating ? t('admin.channelMonitor.duplicating') : t('admin.channelMonitor.duplicate') }}
      </span>
    </button>
    <button :title="t('common.edit')" :aria-label="t('common.edit')"
      @click="$emit('edit', row)"
      class="text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400 workspace-icon-action"
    >
      <Icon name="edit" size="sm" />
      <span class="sr-only">{{ t('common.edit') }}</span>
    </button>
    <button :title="t('common.delete')" :aria-label="t('common.delete')"
      @click="$emit('delete', row)"
      class="text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400 workspace-icon-action"
    >
      <Icon name="trash" size="sm" />
      <span class="sr-only">{{ t('common.delete') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  row: ChannelMonitor
  running: boolean
  duplicating: boolean
}>()

defineEmits<{
  (e: 'run', row: ChannelMonitor): void
  (e: 'duplicate', row: ChannelMonitor): void
  (e: 'edit', row: ChannelMonitor): void
  (e: 'delete', row: ChannelMonitor): void
}>()

const { t } = useI18n()
const duplicateTitle = computed(() => {
  if (props.row.api_key_decrypt_failed) return t('admin.channelMonitor.duplicateKeyUnavailable')
  if (props.duplicating) return t('admin.channelMonitor.duplicating')
  return t('admin.channelMonitor.duplicate')
})
</script>
