<template>
  <div class="w-full" v-bind="wrapperAttrs()">
    <label v-if="label" :for="fieldId" class="input-label mb-1.5 block">
      {{ label }}
      <span v-if="required" class="text-red-500">*</span>
    </label>
    <div class="relative">
      <textarea
        v-bind="fieldAttrs()"
        :id="fieldId"
        :aria-invalid="ariaInvalid()"
        :aria-describedby="describedBy()"
        ref="textAreaRef"
        :value="modelValue"
        :disabled="disabled"
        :required="required"
        :placeholder="placeholderText"
        :readonly="readonly"
        :rows="rows"
        :class="[
          'input w-full min-h-[80px] transition-all duration-200 resize-y',
          error ? 'input-error ring-2 ring-red-500/20' : '',
          disabled ? 'cursor-not-allowed bg-gray-100 opacity-60 dark:bg-dark-900' : ''
        ]"
        @input="onInput"
        @change="$emit('change', ($event.target as HTMLTextAreaElement).value)"
        @blur="$emit('blur', $event)"
        @focus="$emit('focus', $event)"
      ></textarea>
    </div>
    <!-- Hint / Error Text -->
    <p v-if="error" :id="descriptionId" class="input-error-text mt-1.5">
      {{ error }}
    </p>
    <p v-else-if="hint" :id="descriptionId" class="input-hint mt-1.5">
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useAttrs, useId, type HTMLAttributes } from 'vue'

defineOptions({ inheritAttrs: false })

interface Props {
  modelValue: string | null | undefined
  label?: string
  placeholder?: string
  disabled?: boolean
  required?: boolean
  readonly?: boolean
  error?: string
  hint?: string
  id?: string
  rows?: number | string
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  required: false,
  readonly: false,
  rows: 3
})

const attrs = useAttrs()
const ariaInvalid = () => props.error ? true : attrs['aria-invalid'] as HTMLAttributes['aria-invalid']
const generatedId = useId()
const fieldId = computed(() => props.id || `field-${generatedId}`)
const descriptionId = computed(() => `${fieldId.value}-description`)
const wrapperAttrs = () => ({ class: attrs.class, style: attrs.style })
const fieldAttrs = () => Object.fromEntries(Object.entries(attrs).filter(([key]) => key !== 'class' && key !== 'style'))
const describedBy = () => [attrs['aria-describedby'], props.error || props.hint ? descriptionId.value : undefined].filter(Boolean).join(' ') || undefined

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'change', value: string): void
  (e: 'blur', event: FocusEvent): void
  (e: 'focus', event: FocusEvent): void
}>()

const textAreaRef = ref<HTMLTextAreaElement | null>(null)
const placeholderText = computed(() => props.placeholder || '')

const onInput = (event: Event) => {
  const value = (event.target as HTMLTextAreaElement).value
  emit('update:modelValue', value)
}

// Expose focus method
defineExpose({
  focus: () => textAreaRef.value?.focus(),
  select: () => textAreaRef.value?.select()
})
</script>
