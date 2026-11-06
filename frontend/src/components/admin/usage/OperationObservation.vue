<template>
  <section class="rounded-lg border border-gray-200 p-4 text-sm dark:border-dark-600" data-testid="operation-observation">
    <p class="font-medium">{{ t('connections.operation.title') }}</p>
    <p class="mt-1 text-xs text-gray-500">{{ t('connections.operation.scope') }}</p>
    <p v-if="operation?.version !== 2" class="mt-3 text-gray-500">{{ t('connections.operation.unknown') }}</p>
    <dl v-else class="mt-3 grid gap-3 sm:grid-cols-2">
      <div v-for="item in rows" :key="item.label"><dt class="text-xs text-gray-500">{{ item.label }}</dt><dd class="mt-1 break-all">{{ item.value }}</dd></div>
    </dl>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OperationObservation } from '@/api/admin/usageTiming'
const props = defineProps<{ operation?: OperationObservation }>()
const { t } = useI18n()
const ms = (v: number | null | undefined) => v == null ? t('requestTiming.missing') : `${(v / 1000).toFixed(3)}s`
const rows = computed(() => {
  const o = props.operation
  if (!o) return []
  return [
    { label: t('connections.state'), value: t(`connections.states.${o.final_outcome}`) },
    { label: t('connections.operation.reason'), value: `${o.reason || '—'} · ${o.finish_class || '—'}` },
    { label: t('connections.operation.elapsed'), value: ms(o.server_elapsed_ms) },
    { label: t('connections.operation.first'), value: ms(o.first_usable_ms) },
    { label: t('connections.operation.finalFirst'), value: ms(o.final_first_usable_ms) },
    { label: t('connections.operation.attempts'), value: `${o.attempt_count} / ${o.failed_attempt_count}` },
    { label: t('connections.operation.delivery'), value: `${o.delivery} · ${o.evidence_status}` },
    { label: t('connections.operation.protocol'), value: `${o.protocol} · ${o.transport} · ${o.scope}` },
    { label: t('connections.operation.recovered'), value: t(o.recovered ? 'requestTiming.yes' : 'requestTiming.no') }
  ]
})
</script>
