<template>
  <component :is="as ?? defaultTag" class="ritel-type" :data-type-role="variant"><slot /></component>
</template>

<script setup lang="ts">
import { computed } from 'vue'
export type HeadingVariant = 'page' | 'section' | 'subsection' | 'minor' | 'dialog' | 'auth' | 'hero' | 'empty' | 'metric' | 'subtle'
export type HeadingTag = 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6'
const props = withDefaults(defineProps<{ variant?: HeadingVariant; as?: HeadingTag }>(), { variant: 'section' })
const defaultTag = computed<HeadingTag>(() => ['page', 'auth', 'hero'].includes(props.variant) ? 'h1' : props.variant === 'minor' ? 'h4' : props.variant === 'subsection' ? 'h3' : 'h2')
</script>
