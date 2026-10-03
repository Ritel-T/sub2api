<template>
  <span data-test="api-equivalent-cost" :title="tooltip">{{ prefix }}{{ displayCost }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    cost?: number | null
    unpricedRequests?: number
    prefix?: string
  }>(),
  { prefix: 'A ' }
)
const { t } = useI18n()

const hasKnownCost = computed(() =>
  typeof props.cost === 'number' && Number.isFinite(props.cost) && props.cost >= 0
)
const unpricedRequests = computed(() =>
  Math.max(0, Math.trunc(props.unpricedRequests ?? 0))
)
const displayCost = computed(() => {
  if (!hasKnownCost.value) return '—'
  const partial = unpricedRequests.value > 0 ? '≥' : ''
  return `${partial}$${props.cost!.toFixed(2)}`
})
const tooltip = computed(() => {
  const details = [t('usage.apiEquivalentCostTooltip')]
  if (unpricedRequests.value > 0) {
    details.push(t('usage.apiEquivalentUnpriced', { count: unpricedRequests.value }))
  }
  if (!hasKnownCost.value) details.push(t('usage.apiEquivalentUnavailable'))
  else if (unpricedRequests.value > 0) details.push(t('usage.apiEquivalentPartial'))
  return details.join(' ')
})
</script>
