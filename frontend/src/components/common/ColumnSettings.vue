<template>
  <button
    ref="trigger" type="button" class="btn btn-secondary px-2 md:px-3"
    :title="t('common.columnSettings')" :aria-label="t('common.columnSettings')"
    :aria-expanded="open" :aria-controls="panelId" aria-haspopup="dialog" @click="open = !open"
  >
    <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="3" y="4" width="18" height="16" rx="2" /><path d="M9 4v16m6-16v16" />
    </svg>
    <span class="hidden md:inline">{{ t('common.columnSettings') }}</span>
  </button>
  <Teleport to="body">
    <div v-if="open" :id="panelId" ref="panel" role="dialog" tabindex="-1"
      :aria-label="t('common.columnSettings')"
      class="fixed z-[9999] flex flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-600 dark:bg-dark-800"
      :style="position"
    >
      <div class="flex shrink-0 items-center justify-between gap-3 border-b border-gray-200 px-3 py-2 dark:border-dark-600">
        <span class="text-sm font-medium">{{ t('common.columnSettings') }}</span>
        <button type="button" class="btn btn-secondary btn-sm" @click="emit('reset')">{{ t('common.resetColumns') }}</button>
      </div>
      <span :id="hintId" class="sr-only">{{ t('common.columnKeyboard') }}</span>
      <VueDraggable v-model="draggableColumns" handle=".column-drag-handle" :animation="150"
        :delay="120" :delay-on-touch-only="true" class="min-h-0 overflow-y-auto overscroll-contain p-1"
      >
        <div v-for="(column, index) in draggableColumns" :key="column.key"
          class="flex items-center gap-2 rounded-lg px-2 py-1 hover:bg-gray-100 dark:hover:bg-dark-700"
        >
          <button type="button" class="column-drag-handle flex h-8 w-8 shrink-0 touch-none cursor-grab items-center justify-center text-gray-500 active:cursor-grabbing"
            :data-column-key="column.key" :aria-label="t('common.moveColumn', { column: column.label })" :aria-describedby="hintId"
            @keydown.up.prevent="move(index, -1)" @keydown.down.prevent="move(index, 1)"
            @keydown.home.prevent="move(index, -index)" @keydown.end.prevent="move(index, draggableColumns.length - 1 - index)"
          >
            <svg width="14" height="18" viewBox="0 0 14 18" fill="currentColor" aria-hidden="true">
              <circle v-for="n in 6" :key="n" :cx="n % 2 ? 4 : 10" :cy="Math.ceil(n / 2) * 6 - 3" r="1.3" />
            </svg>
          </button>
          <label class="flex min-w-0 flex-1 cursor-pointer items-center gap-2 py-1 text-sm">
            <input type="checkbox" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 dark:border-dark-500 dark:bg-dark-900"
              :checked="!hiddenColumns.has(column.key) || requiredColumns.includes(column.key)"
              :disabled="requiredColumns.includes(column.key)" @change="emit('toggle', column.key)"
            />
            <span class="min-w-0 break-words">{{ column.label }}</span>
          </label>
        </div>
      </VueDraggable>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import type { Column } from './types'

const props = withDefaults(defineProps<{
  columns: Column[]
  hiddenColumns: Set<string>
  requiredColumns?: string[]
}>(), { requiredColumns: () => [] })
const emit = defineEmits<{ toggle: [key: string]; reorder: [keys: string[]]; reset: [] }>()
const { t } = useI18n()
const panelId = useId()
const hintId = useId()
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLElement>()
const open = ref(false)
const position = ref<Record<string, string>>({})
const draggableColumns = computed({
  get: () => props.columns.filter(column => column.key !== 'select'),
  set: columns => emit('reorder', columns.map(column => column.key))
})
function place() {
  const rect = trigger.value?.getBoundingClientRect()
  if (!rect) return
  const width = Math.min(360, window.innerWidth - 24)
  const below = window.innerHeight - rect.bottom - 16
  const above = rect.top - 16
  const flip = below < 240 && above > below
  position.value = {
    left: `${Math.max(12, Math.min(rect.right - width, window.innerWidth - width - 12))}px`,
    width: `${width}px`, maxHeight: `${Math.max(100, Math.min(480, flip ? above : below))}px`,
    ...(flip ? { bottom: `${window.innerHeight - rect.top + 8}px` } : { top: `${rect.bottom + 8}px` })
  }
}
async function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) { await nextTick(); trigger.value?.focus() }
}
async function move(index: number, delta: number) {
  const columns = [...draggableColumns.value]
  const target = index + delta
  if (target < 0 || target >= columns.length || target === index) return
  const column = columns.splice(index, 1)[0]!
  columns.splice(target, 0, column)
  emit('reorder', columns.map(item => item.key))
  await nextTick()
  if (!open.value) return
  Array.from(panel.value?.querySelectorAll<HTMLButtonElement>('[data-column-key]') ?? [])
    .find(button => button.dataset.columnKey === column.key)?.focus()
}
watch(open, async value => {
  if (!value) return
  place()
  await nextTick()
  panel.value?.focus()
})
onClickOutside(panel, () => close(), { ignore: [trigger] })
useEventListener(window, 'resize', () => { if (open.value) place() })
useEventListener(window, 'scroll', () => { if (open.value) place() }, true)
useEventListener(document, 'keydown', event => {
  if (open.value && event.key === 'Escape') { event.preventDefault(); close(true) }
})
</script>
