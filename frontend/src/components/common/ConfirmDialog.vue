<template>
  <BaseDialog :show="effectiveShow" :title="title" width="narrow" :z-index="zIndex"
    :close-on-escape="!interactionLocked" :show-close-button="!interactionLocked" @close="handleCancel">
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ message }}</p>
      <slot></slot>
    </div>

    <template #footer>
      <div class="flex flex-wrap justify-end gap-3">
        <button
          @click="handleCancel"
          type="button"
          :disabled="interactionLocked"
          class="btn btn-secondary"
        >
          {{ cancelText }}
        </button>
        <button
          @click="handleConfirm"
          type="button"
          :disabled="busy || accepting" :aria-busy="busy"
          class="btn" :class="danger ? 'btn-danger' : 'btn-primary'"
        >
          {{ confirmText }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'
import { useConfirmationContext } from '@/composables/useConfirmation'

const { t } = useI18n()

interface Props {
  show: boolean
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
  busy?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'confirm'): void
  (e: 'cancel'): void
}

const props = withDefaults(defineProps<Props>(), {
  danger: false,
  busy: false,
  zIndex: 50
})

const confirmText = computed(() => props.confirmText || t('common.confirm'))
const cancelText = computed(() => props.cancelText || t('common.cancel'))

const emit = defineEmits<Emits>()
const accepting = ref(false)
const acceptancePending = ref(false)
const interactionLocked = computed(() => props.busy || acceptancePending.value)
const invalidated = ref(false)
const currentContext = useConfirmationContext()
const effectiveShow = computed(() => props.show && !invalidated.value)
watch(() => props.show, (show) => {
  accepting.value = false
  acceptancePending.value = false
  invalidated.value = show && !currentContext().active
}, { flush: 'sync', immediate: true })
watch(currentContext, () => {
  // Invalidate on every context transition, including A → B → A before render.
  if (!props.show || invalidated.value) return
  invalidated.value = true
  emit('cancel')
}, { deep: true, flush: 'sync' })
watch(() => props.busy, (busy, wasBusy) => {
  if (wasBusy && !busy) accepting.value = false
}, { flush: 'sync' })

const handleConfirm = () => {
  if (!effectiveShow.value || props.busy || accepting.value) return
  accepting.value = true
  acceptancePending.value = true
  emit('confirm')
  // Bridge the synchronous acceptance to the parent's next-render busy prop.
  // An early-returning handler can still be cancelled after that render.
  void nextTick(() => { acceptancePending.value = false })
}

const handleCancel = () => {
  if (!effectiveShow.value || interactionLocked.value) return
  emit('cancel')
}
</script>
