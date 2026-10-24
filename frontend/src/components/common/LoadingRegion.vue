<template>
  <component :is="as" :role="announce ? 'status' : undefined" aria-busy="true" :aria-label="message">
    <slot />
    <span class="sr-only">{{ message }}</span>
  </component>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  as?: 'div' | 'span'
  label?: string
  /** Repeated cell placeholders stay readable without creating a live region per row. */
  announce?: boolean
}>(), { as: 'div', announce: true })
const { t } = useI18n()
const message = computed(() => props.label || t('common.loading'))
</script>
