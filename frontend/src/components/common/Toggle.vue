<template>
  <button
    type="button"
    @click="toggle"
    class="relative inline-flex flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-800"
    :class="[size === 'small' ? 'h-5 w-9' : 'h-6 w-11', modelValue ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600']"
    :disabled="disabled"
    role="switch"
    :aria-checked="modelValue"
  >
    <span
      class="pointer-events-none inline-block transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
      :class="[size === 'small' ? 'h-4 w-4' : 'h-5 w-5', modelValue ? (size === 'small' ? 'translate-x-4' : 'translate-x-5') : 'translate-x-0']"
    />
  </button>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  modelValue: boolean
  disabled?: boolean
  size?: 'default' | 'small'
}>(), { disabled: false, size: 'default' })

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

function toggle() {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}
</script>
