<template>
  <div class="min-w-0">
    <div class="mb-5 flex flex-wrap items-center justify-between gap-4">
      <span v-if="!loading && items.length > 0" class="text-sm text-gray-500">{{ t('admin.usage.tokenRanking.userCount', { count: items.length }) }}</span>
      <div class="ml-auto flex flex-wrap items-center gap-3">
        <Select :model-value="sortBy" :options="sortableColumns.map(col => ({ value: col.key, label: t(col.label) }))" :aria-label="t('common.sort')" class="w-48" @update:model-value="setSort($event as SortKey)" />
        <Select :aria-label="t('common.resultLimit')" v-model="limit" :options="limitOptions" class="w-28" @change="load" />
      </div>
    </div>
    <div class="relative overflow-x-auto overflow-y-hidden">
      <table class="w-full min-w-[650px] table-fixed text-sm tabular-nums">
        <colgroup><col class="w-14" /><col class="w-[36%]" /><col /><col /><col /><col class="w-10" /></colgroup>
        <thead><tr class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-700">
          <th scope="col" class="py-3 pr-3 text-left">#</th>
          <th scope="col" class="px-3 py-3 text-left">{{ t('admin.usage.tokenRanking.columns.user') }}</th>
          <th v-for="col in primaryColumns" :key="col.key" scope="col" class="px-3 py-3 text-right" :aria-sort="sortBy === col.key ? 'descending' : 'none'">
            <button type="button" class="inline-flex min-h-8 items-center justify-end gap-1" @click="setSort(col.key)">{{ t(col.label) }}<span v-if="sortBy === col.key" aria-hidden="true">↓</span></button>
          </th>
          <th scope="col"><span class="sr-only">{{ t('common.expand') }}</span></th>
        </tr></thead>
        <tbody v-if="loading">
          <tr><td colspan="6" class="py-12 text-center"><LoadingSpinner /></td></tr>
        </tbody>
        <tbody v-else-if="items.length === 0">
          <tr><td colspan="6" class="py-12 text-center text-gray-500">{{ t('admin.dashboard.noDataAvailable') }}</td></tr>
        </tbody>
        <DocumentRows v-else as="tbody" :items="rankingRows" item-key="key" :source-count="items.length" :colspan="6" :estimate-height="72" :item-attrs="rankingRowAttrs">
          <template #default="{ item: entry }">
            <template v-if="entry.kind === 'main'">
              <td class="py-4 pr-3 text-gray-500">{{ entry.rank }}</td>
              <td class="px-3 py-4"><button type="button" class="max-w-full truncate text-left font-medium underline decoration-transparent underline-offset-4 hover:decoration-current" :title="entry.item.email" @click.stop="$emit('select-user', entry.item.user_id, entry.item.email)">{{ entry.item.email || `User #${entry.item.user_id}` }}</button><span class="ml-2 text-xs text-gray-500">#{{ entry.item.user_id }}</span></td>
              <td class="px-3 py-4 text-right">{{ entry.item.requests.toLocaleString() }}</td>
              <td class="px-3 py-4 text-right">{{ fmtTokens(entry.item.total_tokens) }}</td>
              <td class="px-3 py-4 text-right font-medium">${{ fmtCost(entry.item.actual_cost) }}</td>
              <td class="py-4 text-right"><button type="button" class="inline-grid h-8 w-8 place-items-center" :aria-label="`${t('common.expand')}: ${entry.item.email || entry.item.user_id}`" :aria-expanded="expanded.has(entry.item.user_id)" @click.stop="toggleDetails(entry.item.user_id)"><svg aria-hidden="true" class="h-4 w-4" :class="{ 'rotate-90': expanded.has(entry.item.user_id) }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="m9 5 7 7-7 7" /></svg></button></td>
            </template>
            <template v-else><td colspan="6" class="border-b border-gray-200 py-5 dark:border-dark-700"><dl class="flex flex-wrap gap-8">
              <div><dt class="mb-2 text-xs text-gray-500">{{ t('admin.usage.tokenRanking.columns.inputTokens') }}</dt><dd>{{ fmtTokens(entry.item.input_tokens) }}</dd></div>
              <div><dt class="mb-2 text-xs text-gray-500">{{ t('admin.usage.tokenRanking.columns.outputTokens') }}</dt><dd>{{ fmtTokens(entry.item.output_tokens) }}</dd></div>
              <div><dt class="mb-2 text-xs text-gray-500">{{ t('admin.usage.tokenRanking.columns.cacheTokens') }}</dt><dd>{{ fmtTokens(entry.item.cache_tokens) }}</dd></div>
            </dl></td></template>
          </template>
        </DocumentRows>
      </table>
    </div>
    <button v-if="items.length > 10" type="button" class="mt-4 inline-flex items-center gap-2 text-sm text-gray-500"
      data-testid="user-ranking-toggle" :aria-expanded="showAll" @click="showAll = !showAll">
      {{ t(showAll ? 'common.collapse' : 'common.expand') }} <span v-if="!showAll">(+{{ items.length - 10 }})</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUserBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import { formatCompactNumber, formatCostFixed } from '@/utils/format'
import type { UserBreakdownItem } from '@/types'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'

const props = defineProps<{
  startDate: string
  endDate: string
  filters: Record<string, unknown>
  model?: string
}>()

defineEmits<{ (e: 'select-user', userId: number, email: string): void }>()

const { t } = useI18n()

type SortKey = NonNullable<UserBreakdownParams['sort_by']>
const sortableColumns: { key: SortKey; label: string }[] = [
  { key: 'requests', label: 'admin.usage.tokenRanking.columns.requests' },
  { key: 'input_tokens', label: 'admin.usage.tokenRanking.columns.inputTokens' },
  { key: 'output_tokens', label: 'admin.usage.tokenRanking.columns.outputTokens' },
  { key: 'cache_tokens', label: 'admin.usage.tokenRanking.columns.cacheTokens' },
  { key: 'total_tokens', label: 'admin.usage.tokenRanking.columns.totalTokens' },
  { key: 'actual_cost', label: 'admin.usage.tokenRanking.columns.cost' },
]

const limitOptions = [
  { value: 20, label: 'Top 20' },
  { value: 50, label: 'Top 50' },
  { value: 100, label: 'Top 100' },
  { value: 200, label: 'Top 200' },
]

const primaryColumns = sortableColumns.filter(col => ['requests', 'total_tokens', 'actual_cost'].includes(col.key))
const expanded = ref(new Set<number>())
function toggleDetails(id: number) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}

const items = ref<UserBreakdownItem[]>([])
// Expanded details are distinct presentation rows; ranks always follow the server's record order.
const showAll = ref(false)
const visibleItems = computed(() => showAll.value ? items.value : items.value.slice(0, 10))
const rankingRows = computed(() => visibleItems.value.flatMap((item, index) => {
  const main = { key: `user-${item.user_id}`, kind: 'main' as 'main' | 'detail', item, rank: index + 1 }
  return expanded.value.has(item.user_id) ? [main, { ...main, key: `detail-${item.user_id}`, kind: 'detail' as const }] : [main]
}))
const rankingRowAttrs = (entry: typeof rankingRows.value[number]) => ({
  class: entry.kind === 'main' ? 'ranking-main-row cursor-pointer border-b border-gray-200 dark:border-dark-700 hover:bg-gray-50/50 dark:hover:bg-dark-800/30' : 'ranking-detail-row',
  'data-user-id': entry.kind === 'main' ? entry.item.user_id : undefined,
  onClick: entry.kind === 'main' ? () => toggleDetails(entry.item.user_id) : undefined
})
const loading = ref(false)
const sortBy = ref<SortKey>('total_tokens')
const limit = ref(50)
let reqSeq = 0

const fmtTokens = (v: number) => formatCompactNumber(v)
const fmtCost = (v: number) => formatCostFixed(v, 4)

const setSort = (key: SortKey) => {
  if (sortBy.value === key) return
  sortBy.value = key
  load()
}

const load = async () => {
  const seq = ++reqSeq
  loading.value = true
  try {
    const params: UserBreakdownParams = {
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      sort_by: sortBy.value,
      limit: limit.value,
    }
    if (props.model) params.model = props.model
    const res = await getUserBreakdown(params)
    if (seq !== reqSeq) return
    items.value = res.users || []
  } catch {
    if (seq !== reqSeq) return
    items.value = []
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

// Reload when the shared filters / date range / model change.
watch(
  () => [props.startDate, props.endDate, props.model, JSON.stringify(props.filters)],
  () => load(),
  { immediate: true }
)

defineExpose({ reload: load })
</script>
