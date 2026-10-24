<template>
  <label class="flex flex-col items-center gap-0.5 cursor-pointer">
    <span class="text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">{{ label }}</span>
    <Toggle
      :model-value="checked"
      :disabled="disabled"
      :aria-label="label"
      size="small"
      @update:model-value="emit('toggle')"
    />
  </label>
</template>

<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
defineProps<{ label: string; checked: boolean; disabled?: boolean }>()
const emit = defineEmits<{ toggle: [] }>()
</script>
