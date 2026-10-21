<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import OpsErrorLogTable from './OpsErrorLogTable.vue'
import { opsAPI, type OpsErrorLog } from '@/api/admin/ops'
import { buildOpsErrorTimeParams } from '../utils/opsErrorParams'

interface Props {
  show: boolean
  timeRange: string
  customStartTime?: string | null
  customEndTime?: string | null
  platform?: string
  groupId?: number | null
  errorType: 'request' | 'upstream'
  resumeState?: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'openErrorDetail', errorId: number): void
}>()

const { t } = useI18n()


let errorQuery: Record<string, any> = {}
const list = useInfiniteList<OpsErrorLog>({
  pageSize: 50,
  keyOf: row => row.id,
  fetchPage: page => props.errorType === 'upstream'
    ? opsAPI.listUpstreamErrors({ ...errorQuery, page, page_size: 50 })
    : opsAPI.listRequestErrors({ ...errorQuery, page, page_size: 50 })
})
const { items: rows, total, loading, loadingMore, error, hasMore } = list

const q = ref('')
const statusCode = ref<number | 'other' | null>(null)
const phase = ref<string>('')
const errorOwner = ref<string>('')
const viewMode = ref<'errors' | 'excluded' | 'all'>('errors')


const modalTitle = computed(() => {
  return props.errorType === 'upstream' ? t('admin.ops.errorDetails.upstreamErrors') : t('admin.ops.errorDetails.requestErrors')
})

const statusCodeSelectOptions = computed(() => {
  const codes = [400, 401, 403, 404, 409, 422, 429, 500, 502, 503, 504, 529]
  return [
    { value: null, label: t('common.all') },
    ...codes.map((c) => ({ value: c, label: String(c) })),
    { value: 'other', label: t('admin.ops.errorDetails.statusCodeOther') || 'Other' }
  ]
})

const ownerSelectOptions = computed(() => {
  return [
    { value: '', label: t('common.all') },
    { value: 'provider', label: t('admin.ops.errorDetails.owner.provider') || 'provider' },
    { value: 'client', label: t('admin.ops.errorDetails.owner.client') || 'client' },
    { value: 'platform', label: t('admin.ops.errorDetails.owner.platform') || 'platform' }
  ]
})


const viewModeSelectOptions = computed(() => {
  return [
    { value: 'errors', label: t('admin.ops.errorDetails.viewErrors') || 'errors' },
    { value: 'excluded', label: t('admin.ops.errorDetails.viewExcluded') || 'excluded' },
    { value: 'all', label: t('common.all') }
  ]
})

const phaseSelectOptions = computed(() => {
  const options = [
    { value: '', label: t('common.all') },
    { value: 'request', label: t('admin.ops.errorDetails.phase.request') || 'request' },
    { value: 'auth', label: t('admin.ops.errorDetails.phase.auth') || 'auth' },
    { value: 'account_auth', label: t('admin.ops.errorDetails.phase.account_auth') || 'account_auth' },
    { value: 'routing', label: t('admin.ops.errorDetails.phase.routing') || 'routing' },
    { value: 'upstream', label: t('admin.ops.errorDetails.phase.upstream') || 'upstream' },
    { value: 'network', label: t('admin.ops.errorDetails.phase.network') || 'network' },
    { value: 'internal', label: t('admin.ops.errorDetails.phase.internal') || 'internal' }
  ]
  return options
})

function close() {
  emit('update:show', false)
}

const sortBy = ref('created_at')
const sortOrder = ref<'asc' | 'desc'>('desc')

function onSort(nextSortBy: string, nextSortOrder: 'asc' | 'desc') {
  sortBy.value = nextSortBy
  sortOrder.value = nextSortOrder
  void fetchErrorLogs()
}

async function fetchErrorLogs() {
  if (!props.show) return
  const params: Record<string, any> = {
    view: viewMode.value,
    sort_by: sortBy.value,
    sort_order: sortOrder.value
  }
  Object.assign(params, buildOpsErrorTimeParams(props.timeRange, props.customStartTime, props.customEndTime))

  if (props.timeRange === 'custom') {
    if (props.customStartTime && props.customEndTime) {
      params.start_time = props.customStartTime
      params.end_time = props.customEndTime
      delete params.time_range
    } else {
      // Safety fallback: avoid sending time_range=custom (backend doesn't support it)
      params.time_range = '1h'
    }
  }

  const platform = String(props.platform || '').trim()
  if (platform) params.platform = platform
  if (typeof props.groupId === 'number' && props.groupId > 0) params.group_id = props.groupId

  if (q.value.trim()) params.q = q.value.trim()
  if (statusCode.value === 'other') params.status_codes_other = '1'
  else if (typeof statusCode.value === 'number') params.status_codes = String(statusCode.value)

  const phaseVal = String(phase.value || '').trim()
  if (phaseVal) params.phase = phaseVal

  const ownerVal = String(errorOwner.value || '').trim()
  if (ownerVal) params.error_owner = ownerVal


  const duration = /^(\d+)(m|h|d)$/.exec(String(params.time_range))
  if (duration && !params.start_time && !params.end_time) {
    const end = Date.now()
    const unit = duration[2] === 'd' ? 86400000 : duration[2] === 'h' ? 3600000 : 60000
    params.start_time = new Date(end - Number(duration[1]) * unit).toISOString()
    params.end_time = new Date(end).toISOString()
    delete params.time_range
  }
  errorQuery = params
  await list.reset()
}

function resetFilters() {
  q.value = ''
  statusCode.value = null
  phase.value = props.errorType === 'upstream' ? 'upstream' : ''
  errorOwner.value = ''
  viewMode.value = 'errors'
  void fetchErrorLogs()
}


watch(
  () => props.show,
  (open) => {
    if (!open) { list.cancel(); return }
    if (props.resumeState) { void list.refresh(); return }
    resetFilters()
  },
  { immediate: true }
)

watch(
  () => [props.timeRange, props.customStartTime, props.customEndTime, props.platform, props.groupId] as const,
  () => {
    if (!props.show) return
    fetchErrorLogs()
  }
)

let searchTimeout: number | null = null
watch(
  () => q.value,
  () => {
    if (!props.show) return
    if (searchTimeout) window.clearTimeout(searchTimeout)
    searchTimeout = window.setTimeout(() => {
      void fetchErrorLogs()
    }, 350)
  }
)

watch(
  () => [statusCode.value, phase.value, errorOwner.value, viewMode.value] as const,
  () => {
    if (!props.show) return
    fetchErrorLogs()
  }
)
onBeforeUnmount(() => { if (searchTimeout) window.clearTimeout(searchTimeout) })
</script>

<template>
  <BaseDialog :show="show" :title="modalTitle" width="full" @close="close">
    <div class="min-w-0">
      <!-- Filters -->
      <div class="mb-4 flex-shrink-0 border-b border-gray-200 pb-4 dark:border-dark-700">
        <div class="grid grid-cols-2 gap-2 md:grid-cols-8">
          <div class="col-span-2 compact-select">
            <div class="relative group">
              <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
                <svg
                  class="h-3.5 w-3.5 text-gray-400 transition-colors group-focus-within:text-primary-400 dark:group-focus-within:text-primary-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
                </svg>
              </div>
              <input
                v-model="q"
                type="text"
                class="w-full rounded-lg border-gray-200 bg-gray-50/50 py-1.5 pl-9 pr-3 text-xs font-medium text-gray-700 transition-all focus:border-primary-400 focus:bg-white focus:ring-2 focus:ring-primary-400/10 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-300 dark:focus:bg-dark-800"
                :placeholder="t('admin.ops.errorDetails.searchPlaceholder')"
              />
            </div>
          </div>

          <div class="compact-select">
            <Select :model-value="statusCode" :options="statusCodeSelectOptions" @update:model-value="statusCode = $event as any" />
          </div>

          <div class="compact-select">
            <Select :model-value="phase" :options="phaseSelectOptions" @update:model-value="phase = String($event ?? '')" />
          </div>

          <div class="compact-select">
            <Select :model-value="errorOwner" :options="ownerSelectOptions" @update:model-value="errorOwner = String($event ?? '')" />
          </div>



          <div class="compact-select">
            <Select :model-value="viewMode" :options="viewModeSelectOptions" @update:model-value="viewMode = $event as any" />
          </div>

          <div class="flex items-center justify-end">
            <button type="button" class="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-semibold text-gray-700 transition-colors hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600" @click="resetFilters">
              {{ t('common.reset') }}
            </button>
          </div>
        </div>
      </div>

      <!-- Body -->
      <div class="min-w-0">
        <div class="mb-2 flex-shrink-0 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.ops.errorDetails.total') }} {{ total }}
        </div>

          <OpsErrorLogTable
            class="min-w-0"
            summary-first
            :rows="rows"
            :total="total ?? 0"
            :loading="loading"
            :loading-more="loadingMore"
            :error="error"
            :has-more="hasMore"
            @openErrorDetail="emit('openErrorDetail', $event)"
            @sort="onSort"

            @load-more="list.loadMore"
            @retry="list.retry"
          />

      </div>
    </div>
  </BaseDialog>
</template>

<style>
.compact-select .select-trigger {
  @apply py-1.5 px-1 text-xs;
}
</style>
