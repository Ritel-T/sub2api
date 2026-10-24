<template>
  <ConfirmDialog :z-index="200" :show="confirmation.show.value" :title="confirmation.title.value || t('common.confirm')" :message="confirmation.message.value"
    :confirm-text="confirmation.confirmText.value" :cancel-text="confirmation.cancelText.value"
    :danger="confirmation.danger.value" :busy="confirmation.busy.value"
    @confirm="confirmation.settle(true)" @cancel="confirmation.settle(false)" />
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import ConfirmDialog from './ConfirmDialog.vue'
import type { Confirmation } from '@/composables/useConfirmation'
defineProps<{ confirmation: Confirmation }>()
const { t } = useI18n()
</script>
