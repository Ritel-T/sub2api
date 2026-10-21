<script setup lang="ts">
import { inject, onBeforeUnmount, ref, watch, type Ref } from 'vue'

const props = withDefaults(defineProps<{ kind?: 'sort' | 'actions' }>(), { kind: 'sort' })
const target = ref<HTMLElement | null>(null)
const registeredTarget = inject<Ref<HTMLElement | null> | undefined>(
  props.kind === 'actions' ? 'tableActionsTarget' : 'tableInlineToolbarTarget',
  undefined
)
watch(target, element => {
  if (registeredTarget) registeredTarget.value = element
}, { flush: 'post' })
onBeforeUnmount(() => {
  if (registeredTarget?.value === target.value) registeredTarget.value = null
})
</script>

<template>
  <div ref="target" :class="kind === 'actions' ? 'table-header-tools' : 'inline-table-tools'" />
</template>
