<template>
  <label class="block w-full" data-field-role="filter">
    <span class="sr-only">{{ labelText }}</span>
    <div class="relative">
      <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
        <Icon name="search" size="md" class="text-gray-400" />
      </div>
      <input v-model="searchValue" type="text" class="input workspace-search-input pl-10"
        :aria-label="labelText" :placeholder="placeholderText" />
    </div>
  </label>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDebounceFn } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  label?: string
  placeholder?: string
  debounceMs?: number
}>(), {
  debounceMs: 300
})

const { t } = useI18n()
const labelText = computed(() => props.label || t('common.search'))
const placeholderText = computed(() => props.placeholder ?? t('common.searchPlaceholder'))

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'search', value: string): void
}>()

const debouncedEmitSearch = useDebounceFn((value: string) => {
  emit('search', value)
}, props.debounceMs)

const searchValue = computed({
  get: () => props.modelValue,
  set: (value: string) => {
    emit('update:modelValue', value)
    debouncedEmitSearch(value)
  }
})
</script>
