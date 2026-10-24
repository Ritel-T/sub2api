<template>
  <component :is="as" ref="body">
    <component :is="itemTag" v-if="paddingTop" aria-hidden="true" role="presentation" data-document-spacer :style="as === 'tbody' ? undefined : spacer(paddingTop)"><td v-if="as === 'tbody'" :colspan="colspan" :style="spacer(paddingTop)" /></component>
    <template v-for="{ row, index, gapBefore } in rendered" :key="keyOf(row, index)">
      <component :is="itemTag" v-if="gapBefore" aria-hidden="true" role="presentation" data-document-spacer :style="as === 'tbody' ? undefined : spacer(gapBefore)"><td v-if="as === 'tbody'" :colspan="colspan" :style="spacer(gapBefore)" /></component>
      <component :is="itemTag" v-bind="itemAttrs?.(row, index)" :ref="measure" :data-index="index" data-document-row :class="[as !== 'tbody' && 'flow-root', itemClass]">
        <slot :item="row" :index="index" />
      </component>
    </template>
    <component :is="itemTag" v-if="paddingBottom" aria-hidden="true" role="presentation" data-document-spacer :style="as === 'tbody' ? undefined : spacer(paddingBottom)"><td v-if="as === 'tbody'" :colspan="colspan" :style="spacer(paddingBottom)" /></component>
  </component>
</template>

<script setup lang="ts" generic="T extends object">
import { computed, ref } from 'vue'
import { useDocumentRows } from '@/composables/useDocumentRows'

const props = withDefaults(defineProps<{
  items: T[]
  itemKey: keyof T | ((item: T, index: number) => string | number)
  as?: 'div' | 'ul' | 'tbody'
  colspan?: number
  sourceCount?: number
  itemClass?: string
  itemAttrs?: (item: T, index: number) => Record<string, unknown>
  estimateHeight?: number
  enabled?: boolean
}>(), { as: 'div', colspan: 1, estimateHeight: 80, enabled: true, itemClass: '' })
defineSlots<{ default: (props: { item: T; index: number }) => unknown }>()
const body = ref<HTMLElement | null>(null)
const itemTag = computed(() => props.as === 'ul' ? 'li' : props.as === 'tbody' ? 'tr' : 'div')
function keyOf(item: T, index: number): string | number {
  const key = typeof props.itemKey === 'function' ? props.itemKey(item, index) : item[props.itemKey]
  return typeof key === 'number' ? key : String(key)
}
const { rendered, paddingTop, paddingBottom, measure } = useDocumentRows(
  computed(() => props.items), body, keyOf, computed(() => props.enabled), () => props.estimateHeight,
  computed(() => props.sourceCount ?? props.items.length)
)
const spacer = (height: number) => ({ height: `${height}px`, border: '0', padding: '0', margin: '0' })
</script>
