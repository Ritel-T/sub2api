<script setup lang="ts">
import DataTable from '@/components/common/DataTable.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores'
import { opsAPI, type OpsRequestDetailsParams, type OpsRequestDetail } from '@/api/admin/ops'
import { parseTimeRangeMinutes, formatDateTime } from '../utils/opsFormatters'
import { getAccountNameMap } from '../utils/accountNameMap'

export interface OpsRequestDetailsPreset {
  title: string
  kind?: OpsRequestDetailsParams['kind']
  sort?: OpsRequestDetailsParams['sort']
  min_duration_ms?: number
  max_duration_ms?: number
}

interface Props {
  modelValue: boolean
  timeRange: string
  preset: OpsRequestDetailsPreset
  platform?: string
  groupId?: number | null
  resumeState?: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'openErrorDetail', errorId: number): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()


const list = useInfiniteList<OpsRequestDetail>({
  pageSize: 50,
  keyOf: row => `${row.kind}:${row.error_id ?? row.request_id}:${row.created_at}`,
  fetchPage: async (page) => {
    const res = await opsAPI.listRequestDetails({ ...requestQuery, page, page_size: 50 })
    return res
  }
})
const { items, loading, loadingMore, error, hasMore } = list
const requestRows = computed(() => items.value.map(row => ({ ...row, recordKey: `${row.kind}:${row.error_id ?? row.request_id}:${row.created_at}` })))
let requestQuery: OpsRequestDetailsParams = {}
const accountNameById = ref<Map<number, string>>(new Map())

const close = () => emit('update:modelValue', false)

const showTTFT = computed(() => props.preset.sort === 'ttft_desc')
const latencyLabel = computed(() => t(showTTFT.value ? 'admin.ops.ttftLabel' : 'admin.ops.requestDetails.table.duration'))

function formatLatency(row: OpsRequestDetail): string {
  const value = showTTFT.value ? row.first_token_ms : row.duration_ms
  return typeof value === 'number' ? `${value} ms` : '-'
}

const rangeLabel = computed(() => {
  const minutes = parseTimeRangeMinutes(props.timeRange)
  if (minutes >= 60) return t('admin.ops.requestDetails.rangeHours', { n: Math.round(minutes / 60) })
  return t('admin.ops.requestDetails.rangeMinutes', { n: minutes })
})

function buildTimeParams(): Pick<OpsRequestDetailsParams, 'start_time' | 'end_time'> {
  const minutes = parseTimeRangeMinutes(props.timeRange)
  const endTime = new Date()
  const startTime = new Date(endTime.getTime() - minutes * 60 * 1000)
  return {
    start_time: startTime.toISOString(),
    end_time: endTime.toISOString()
  }
}

const fetchData = async () => {
  if (!props.modelValue) return
  const params: OpsRequestDetailsParams = {
    ...buildTimeParams(),
    kind: props.preset.kind ?? 'all',
    sort: props.preset.sort ?? 'created_at_desc'
  }

  const platform = (props.platform || '').trim()
  if (platform) params.platform = platform
  if (typeof props.groupId === 'number' && props.groupId > 0) params.group_id = props.groupId

  if (typeof props.preset.min_duration_ms === 'number') params.min_duration_ms = props.preset.min_duration_ms
  if (typeof props.preset.max_duration_ms === 'number') params.max_duration_ms = props.preset.max_duration_ms

  requestQuery = params
  await list.reset()
}
watch(items, () => { void refreshAccountNames() })

watch(
  () => props.modelValue,
  (open) => {
    if (!open) { list.cancel(); return }
    if (open) {
      if (props.resumeState) { void list.refresh(); return }
      fetchData()
    }
  },
  { immediate: true }
)

watch(
  () => [
    props.timeRange,
    props.platform,
    props.groupId,
    props.preset.kind,
    props.preset.sort,
    props.preset.min_duration_ms,
    props.preset.max_duration_ms
  ],
  () => {
    if (!props.modelValue) return
    fetchData()
  }
)

async function handleCopyRequestId(requestId: string) {
  const ok = await copyToClipboard(requestId, t('admin.ops.requestDetails.requestIdCopied'))
  if (ok) return
  // `useClipboard` already shows toast on failure; this keeps UX consistent with older ops modal.
  appStore.showWarning(t('admin.ops.requestDetails.copyFailed'))
}

function openErrorDetail(errorId: number | null | undefined) {
  if (!errorId) return
  emit('openErrorDetail', errorId)
}

const kindBadgeClass = (kind: string) => {
  if (kind === 'error') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
}

// 明细行只带 account_id；账号名来自 lite 账号列表映射，缺项时强制刷新一次缓存（新建账号场景）。
async function refreshAccountNames() {
  const ids = items.value
    .map(row => row.account_id)
    .filter((id): id is number => typeof id === 'number' && id > 0)
  if (ids.length === 0) return
  try {
    let map = await getAccountNameMap()
    if (ids.some(id => !map.has(id))) map = await getAccountNameMap(true)
    accountNameById.value = map
  } catch (e) {
    console.warn('[OpsRequestDetailsModal] Failed to load account names', e)
  }
}

function accountLabel(row: OpsRequestDetail): string {
  const id = row.account_id
  if (typeof id !== 'number' || id <= 0) return '-'
  return accountNameById.value.get(id) || `#${id}`
}

const openRecordColumns = computed(() => [{ key: 'time', label: t('admin.ops.requestDetails.table.time'), sortable: false },
  { key: 'kind', label: t('admin.ops.requestDetails.table.kind'), sortable: false },
  { key: 'platform', label: t('admin.ops.requestDetails.table.platform'), sortable: false },
  { key: 'model', label: t('admin.ops.requestDetails.table.model'), sortable: false },
  { key: 'account', label: t('admin.ops.requestDetails.table.account'), sortable: false },
  { key: 'latency', label: latencyLabel.value, sortable: false },
  { key: 'status', label: t('admin.ops.requestDetails.table.status'), sortable: false },
  { key: 'request_id', label: t('admin.ops.requestDetails.table.requestId'), sortable: false },
  { key: 'actions', label: t('admin.ops.requestDetails.table.actions'), sortable: false }])
</script>

<template>
  <BaseDialog :show="modelValue" :title="props.preset.title || t('admin.ops.requestDetails.title')" width="full" @close="close">
    <template #default>
      <div class="min-w-0">
        <div class="mb-4 flex flex-shrink-0 items-center justify-between">
          <div class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.ops.requestDetails.rangeLabel', { range: rangeLabel }) }}
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            @click="fetchData"
          >
            {{ t('common.refresh') }}
          </button>
        </div>

        <!-- Loading -->
        <div v-if="loading" class="flex flex-1 items-center justify-center py-16">
          <div class="flex flex-col items-center gap-3">
            <svg role="status" :aria-label="t('common.loading')" class="h-8 w-8 animate-spin text-primary-500 dark:text-primary-400" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            <span class="text-sm font-medium text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</span>
          </div>
        </div>

        <!-- Table -->
        <div v-else class="min-w-0">
          <div v-if="items.length === 0" class="py-10 text-center">
            <div class="text-sm font-medium text-gray-600 dark:text-gray-300">{{ t('admin.ops.requestDetails.empty') }}</div>
            <div class="mt-1 text-xs text-gray-400">{{ t('admin.ops.requestDetails.emptyHint') }}</div>
          </div>

          <div v-else class="min-w-0">
            <div class="min-w-0">
              <DataTable column-order-key="admin-ops-request-details"
 :columns="openRecordColumns"
 :primary-columns="['time','kind','model','latency','status','actions']"
 :data="requestRows" row-key="recordKey" >
<template #cell-time="{ row: row }"><div class="whitespace-nowrap text-xs text-gray-600 dark:text-gray-300">
                    {{ formatDateTime(row.created_at) }}
                  </div></template>
<template #cell-kind="{ row: row }"><div class="whitespace-nowrap ">
                    <span class="rounded-full text-[10px] font-bold" :class="kindBadgeClass(row.kind)">
                      {{ row.kind === 'error' ? t('admin.ops.requestDetails.kind.error') : t('admin.ops.requestDetails.kind.success') }}
                    </span>
                  </div></template>
<template #cell-platform="{ row: row }"><div class="whitespace-nowrap text-xs font-medium text-gray-700 dark:text-gray-200">
                    {{ (row.platform || 'unknown').toUpperCase() }}
                  </div></template>
<template #cell-model="{ row: row }"><div class="max-w-[240px] truncate text-xs text-gray-600 dark:text-gray-300" :title="row.model || ''">
                    {{ row.model || '-' }}
                  </div></template>
<template #cell-account="{ row: row }"><div class="max-w-[180px] truncate text-xs text-gray-600 dark:text-gray-300" :title="accountLabel(row)">
                    {{ accountLabel(row) }}
                  </div></template>
<template #cell-latency="{ row: row }"><div class="whitespace-nowrap text-xs text-gray-600 dark:text-gray-300">
                    {{ formatLatency(row) }}
                  </div></template>
<template #cell-status="{ row: row }"><div class="whitespace-nowrap text-xs text-gray-600 dark:text-gray-300">
                    {{ row.status_code ?? '-' }}
                  </div></template>
<template #cell-request_id="{ row: row }"><div class="">
                    <div v-if="row.request_id" class="flex items-center gap-2">
                      <span class="max-w-[220px] truncate font-mono text-[11px] text-gray-700 dark:text-gray-200" :title="row.request_id">
                        {{ row.request_id }}
                      </span>
                      <button
                        class="rounded-md bg-gray-100 text-[10px] font-bold text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600"
                        @click="handleCopyRequestId(row.request_id)"
                      >
                        {{ t('admin.ops.requestDetails.copy') }}
                      </button>
                    </div>
                    <span v-else class="text-xs text-gray-400">-</span>
                  </div></template>
<template #cell-actions="{ row: row }"><div class="whitespace-nowrap text-right">
                    <button
                      v-if="row.kind === 'error' && row.error_id"
                      class="rounded-lg bg-red-50 text-xs font-bold text-red-600 hover:bg-red-100 dark:bg-red-900/20 dark:text-red-300 dark:hover:bg-red-900/30"
                      @click="openErrorDetail(row.error_id)"
                    >
                      {{ t('admin.ops.requestDetails.viewError') }}
                    </button>
                    <span v-else class="text-xs text-gray-400">-</span>
                  </div></template>
</DataTable>
            </div>
          </div>

            <InfiniteListSentinel
              :loading="loading || loadingMore"
              :error="error"
              :has-more="hasMore"
              @load-more="list.loadMore"
              @retry="list.retry"
            />
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
