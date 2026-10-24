<template>
  <BaseDialog :show="show" :title="t('channelMonitorV3.incidents.title')" width="wide" close-on-click-outside @close="emit('close')">
    <div class="space-y-3" data-testid="monitor-v3-incident-list">
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.incidents.hint', { since: since ? formatMonitorV3Full(since) : '—' }) }}</p>
      <LoadingRegion v-if="loading && !items.length" class="space-y-2">
        <div v-for="index in 3" :key="index" class="h-16 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700" />
      </LoadingRegion>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-300">{{ error }}</p>
      <p v-if="!loading && !error && !items.length" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="monitor-v3-incidents-empty">
        {{ t('channelMonitorV3.incidents.empty') }}
      </p>
      <ul ref="listBody" class="divide-y divide-gray-100 dark:divide-dark-700">
        <li v-if="paddingTop" aria-hidden="true" :style="{ height: `${paddingTop}px` }" />
        <template v-for="{ row: item, index, gapBefore } in rendered" :key="`${item.component_id}-${item.started_at}`">
        <li v-if="gapBefore" aria-hidden="true" :style="{ height: `${gapBefore}px` }" />
        <li :ref="measure" :data-index="index" class="py-3" data-testid="monitor-v3-incident">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex min-w-0 items-center gap-2">
              <StatusDot :status="item.ended_at ? 'operational' : 'down'" />
              <span class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.component_name }}</span>
              <span
                class="rounded-full px-2 py-0.5 text-[10px] font-semibold"
                :class="item.ended_at ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300'"
              >
                {{ t(item.ended_at ? 'channelMonitorV3.incidents.resolved' : 'channelMonitorV3.incidents.ongoing') }}
              </span>
            </div>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ duration(item) }}</span>
          </div>
          <p class="mt-1 text-xs text-gray-600 dark:text-gray-300">
            {{ reason(item.top_error) }} · {{ t('channelMonitorV3.incidents.downSlots', { count: item.down_slots }) }}
            <template v-if="item.requests"> · {{ t('channelMonitorV3.incidents.volume', { requests: item.requests, errors: item.errors || 0 }) }}</template>
            <template v-if="item.probes"> · {{ t('channelMonitorV3.incidents.probes', { count: item.probes }) }}</template>
          </p>
          <p class="mt-0.5 text-[11px] text-gray-400 dark:text-gray-500">
            {{ formatMonitorV3Full(item.started_at) }} → {{ item.ended_at ? formatMonitorV3Full(item.ended_at) : t('channelMonitorV3.incidents.now') }}
          </p>
        </li>
        </template>
        <li v-if="paddingBottom" aria-hidden="true" :style="{ height: `${paddingBottom}px` }" />
      </ul>
      <InfiniteListSentinel :loading="loading || list.loadingMore.value || list.refreshing.value" :error="list.error.value" :has-more="show && list.hasMore.value" @load-more="list.loadMore" @retry="list.retry" />
    </div>
    <template #footer>
      <div class="flex w-full items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
        <span>{{ t('channelMonitorV3.incidents.total', { count: total ?? 0 }) }}</span>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import LoadingRegion from '@/components/common/LoadingRegion.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useDocumentRows } from '@/composables/useDocumentRows'
import { getIncidents, type MonitorV3Incident } from '@/api/channelMonitorV3'
import { extractApiErrorMessage } from '@/utils/apiError'
import StatusDot from './StatusDot.vue'
import { formatMonitorV3Full } from './monitorV3'

const PAGE_SIZE = 20
const props = defineProps<{ show: boolean; admin: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t, te } = useI18n()
const since = ref('')
const keyOf = (item: MonitorV3Incident) => `${item.component_id}-${item.started_at}`
const list = useInfiniteList<MonitorV3Incident>({
  pageSize: PAGE_SIZE, keyOf,
  fetchPage: async (page, signal) => {
    const result = await getIncidents(page, PAGE_SIZE, props.admin, signal)
    if (!signal.aborted) since.value = result.since
    return { items: result.items || [], total: result.total }
  }
})
const { items, total, loading } = list
const error = computed(() => list.error.value ? extractApiErrorMessage(list.error.value, t('channelMonitorV3.incidents.loadFailed')) : '')
const listBody = ref<HTMLElement | null>(null)
const { rendered, paddingTop, paddingBottom, measure } = useDocumentRows(items, listBody, keyOf, computed(() => props.show), () => 108)

function reason(category?: string) {
  const key = `channelMonitorV3.errors.${category || 'other'}`
  return te(key) ? t(key) : t('channelMonitorV3.errors.other')
}

function duration(item: MonitorV3Incident) {
  const end = item.ended_at ? new Date(item.ended_at).getTime() : Date.now()
  const minutes = Math.max(0, Math.round((end - new Date(item.started_at).getTime()) / 60000))
  if (minutes < 60) return t('channelMonitorV3.incidents.minutes', { count: minutes })
  const hours = Math.floor(minutes / 60)
  return t('channelMonitorV3.incidents.hours', { hours, minutes: minutes % 60 })
}

watch(() => [props.show, props.admin], () => {
  since.value = ''
  if (props.show) void list.reset()
  else list.clear()
}, { immediate: true })
</script>
