<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import DataTable from '@/components/common/DataTable.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { opsAPI, type OpsOpenAITokenStatsItem, type OpsOpenAITokenStatsParams, type OpsOpenAITokenStatsTimeRange } from '@/api/admin/ops'
import { formatNumber } from '@/utils/format'

interface Props {
  platformFilter?: string
  groupIdFilter?: number | null
  refreshToken: number
}

type ViewMode = 'topn' | 'pagination'

const props = withDefaults(defineProps<Props>(), {
  platformFilter: '',
  groupIdFilter: null
})

const { t } = useI18n()
const tableTools0 = ref<HTMLElement | null>(null)

const timeRange = ref<OpsOpenAITokenStatsTimeRange>('30d')
const viewMode = ref<ViewMode>('topn')
const topN = ref<number>(20)
const total = ref(0)
let appliedQuery: OpsOpenAITokenStatsParams = {}
const list = useInfiniteList<OpsOpenAITokenStatsItem>({
  keyOf: row => row.model, pageSize: 20,
  fetchPage: async (page, signal) => {
    const query = { ...appliedQuery }
    const response = await opsAPI.getOpenAITokenStats(
      query.top_n ? query : { ...query, page, page_size: 20 }, { signal })
    if (!signal.aborted) total.value = response.total
    return { items: response.items, total: query.top_n ? response.items.length : response.total }
  },
})
const { items, loading } = list
const errorMessage = computed(() => list.error.value
  ? (list.error.value as Error).message || t('admin.ops.openaiTokenStats.failedToLoad')
  : '')

const timeRangeOptions = computed(() => [
  { value: '30m', label: t('admin.ops.timeRange.30m') },
  { value: '1h', label: t('admin.ops.timeRange.1h') },
  { value: '1d', label: t('admin.ops.timeRange.1d') },
  { value: '15d', label: t('admin.ops.timeRange.15d') },
  { value: '30d', label: t('admin.ops.timeRange.30d') }
])

const viewModeOptions = computed(() => [
  { value: 'topn', label: t('admin.ops.openaiTokenStats.viewModeTopN') },
  { value: 'pagination', label: t('common.all') }
])

const topNOptions = computed(() => [
  { value: 10, label: 'Top 10' },
  { value: 20, label: 'Top 20' },
  { value: 50, label: 'Top 50' },
  { value: 100, label: 'Top 100' }
])

function formatRate(v?: number | null): string {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '-'
  return v.toFixed(2)
}

function formatInt(v?: number | null): string {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '-'
  return formatNumber(Math.round(v))
}

function resetData() {
  appliedQuery = {
    time_range: timeRange.value,
    platform: props.platformFilter || undefined,
    group_id: typeof props.groupIdFilter === 'number' && props.groupIdFilter > 0 ? props.groupIdFilter : undefined,
    ...(viewMode.value === 'topn' ? { top_n: topN.value } : {}),
  }
  total.value = 0
  void list.reset()
}

watch(
  () => [timeRange.value, viewMode.value, topN.value, props.platformFilter, props.groupIdFilter],
  resetData,
  { immediate: true }
)
watch(() => props.refreshToken, () => { void list.refresh() })

const openRecordColumns = computed(() => [{ key: 'model', label: t('admin.ops.openaiTokenStats.table.model'), sortable: false, primary: true }, { key: 'request_count', label: t('admin.ops.openaiTokenStats.table.requestCount'), sortable: false, primary: true }, { key: 'avg_tokens_per_sec', label: t('admin.ops.openaiTokenStats.table.avgTokensPerSec'), sortable: false, primary: true }, { key: 'avg_first_token_ms', label: t('admin.ops.openaiTokenStats.table.avgFirstTokenMs'), sortable: false, primary: true }, { key: 'total_output_tokens', label: t('admin.ops.openaiTokenStats.table.totalOutputTokens'), sortable: false, primary: true }, { key: 'avg_duration_ms', label: t('admin.ops.openaiTokenStats.table.avgDurationMs'), sortable: false, primary: true }, { key: 'requests_with_first_token', label: t('admin.ops.openaiTokenStats.table.requestsWithFirstToken'), sortable: false, primary: true }])
</script>

<template>
  <section class="open-section md:p-5">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <AppHeading variant="subsection" as="h3" class="text-gray-900 dark:text-white">
        {{ t('admin.ops.openaiTokenStats.title') }}
      </AppHeading>
      <div class="open-filter-toolbar flex flex-wrap items-center gap-2">
        <div ref="tableTools0" class="min-w-0" />
        <div class="w-36">
          <Select :aria-label="t('admin.dashboard.timeRange')" v-model="timeRange" :options="timeRangeOptions" />
        </div>
        <div class="w-36">
          <Select :aria-label="t('common.viewMode')" v-model="viewMode" :options="viewModeOptions" />
        </div>
        <div v-if="viewMode === 'topn'" class="w-28">
          <Select :aria-label="t('common.resultLimit')" v-model="topN" :options="topNOptions" />
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="mb-4 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600 dark:bg-red-900/20 dark:text-red-400">
      {{ errorMessage }}
    </div>

    <div v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.ops.loadingText') }}
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('common.noData')"
      :description="t('admin.ops.openaiTokenStats.empty')"
    />

    <div v-else class="space-y-3">
          <DataTable column-order-key="admin-ops-openai-token-stats" :toolbar-target="tableTools0"
 :columns="openRecordColumns"
 :data="items" row-key="model" >
<template #cell-model="{ row: row }"><div class="break-all whitespace-normal font-medium">{{ row.model }}</div></template>
<template #cell-request_count="{ row: row }"><div class="">{{ formatInt(row.request_count) }}</div></template>
<template #cell-avg_tokens_per_sec="{ row: row }"><div class="">{{ formatRate(row.avg_tokens_per_sec) }}</div></template>
<template #cell-avg_first_token_ms="{ row: row }"><div class="">{{ formatRate(row.avg_first_token_ms) }}</div></template>
<template #cell-total_output_tokens="{ row: row }"><div class="">{{ formatInt(row.total_output_tokens) }}</div></template>
<template #cell-avg_duration_ms="{ row: row }"><div class="">{{ formatInt(row.avg_duration_ms) }}</div></template>
<template #cell-requests_with_first_token="{ row: row }"><div class="">{{ formatInt(row.requests_with_first_token) }}</div></template>
</DataTable>
      <div v-if="viewMode === 'topn'" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.ops.openaiTokenStats.totalModels', { total }) }}
      </div>
    </div>
    <InfiniteListSentinel :loading="loading || list.loadingMore.value || list.refreshing.value" :error="list.error.value" :has-more="list.hasMore.value" @load-more="list.loadMore" @retry="list.retry" />
  </section>
</template>
