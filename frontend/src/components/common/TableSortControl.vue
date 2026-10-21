<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from './Select.vue'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  columns: Array<{ key: string; label: string }>
  sortKey?: string
  sortOrder?: 'asc' | 'desc'
  disabled?: boolean
}>(), { sortKey: '', sortOrder: 'asc', disabled: false })
const emit = defineEmits<{ select: [key: string]; toggle: [] }>()
const { t } = useI18n()
const options = computed(() => props.columns.map(column => ({
  value: column.key,
  label: column.label
})))
const directionLabel = computed(() => props.sortOrder === 'asc'
  ? t('common.sortDescending')
  : t('common.sortAscending'))
</script>

<template>
  <div class="workspace-sort-control" data-test="mobile-sort-controls">
    <Select
      class="workspace-sort-field"
      :model-value="sortKey"
      :options="options"
      :placeholder="t('common.sort')"
      :aria-label="t('common.sort')"
      :searchable="false"
      :disabled="disabled"
      data-test="mobile-sort"
      @update:model-value="emit('select', String($event ?? ''))"
    />
    <button
      type="button"
      class="workspace-sort-direction"
      :title="directionLabel"
      :aria-label="directionLabel"
      data-test="mobile-sort-direction"
      :disabled="disabled"
      @click="emit('toggle')"
    >
      <Icon :name="sortOrder === 'asc' ? 'arrowUp' : 'arrowDown'" size="sm" />
    </button>
  </div>
</template>

<style scoped>
.workspace-sort-control {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  width: 100%;
}
.workspace-sort-field { flex: 1; min-width: 7.5rem; }
.workspace-sort-direction {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 36px;
  width: 36px;
  height: 44px;
  color: var(--ritel-muted);
  background: transparent;
  border: 0;
  border-radius: 0;
}
.workspace-sort-direction:hover { color: var(--ritel-fg); }
.workspace-sort-direction:disabled { opacity: .5; cursor: not-allowed; }
.workspace-sort-direction:focus-visible {
  outline: 2px solid var(--ritel-focus);
  outline-offset: 2px;
}
</style>
