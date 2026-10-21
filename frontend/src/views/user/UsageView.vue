<template>
  <AppLayout>
    <div class="space-y-6">
      <h1 class="text-3xl">{{ consoleCopy[usageView] }}</h1>
      <div v-if="usageView === 'analysis' && analyticsErrors.size" role="alert" class="flex items-center justify-between gap-4 border border-red-500 p-4"><span>{{ t('usage.failedToLoad') }}</span><button type="button" class="btn btn-secondary" @click="refreshData">{{ t('common.refresh') }}</button></div>
      <UsageStatsCards v-if="usageView === 'analysis'" :stats="usageStats" :show-account-cost="false" :strike-standard-cost="true" />

      <div class="space-y-4">
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker enable-time :preset="selectedPreset"
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="onGranularityChange" />
              </div>
            </div>
          </div>
        </div>

        <div v-if="usageView === 'analysis'" class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
        </div>

        <div v-if="usageView === 'analysis'" class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>

      <div class="card p-6">
        <div class="flex flex-wrap items-end justify-between gap-4">
          <div v-if="activeTab === 'errors'" class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.errors.keyName') }}</label>
              <Select v-model="errorFilter.api_key_id" :options="errorKeyOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.errors.model') }}</label>
              <Select
                v-model="errorFilter.model"
                :options="errorModelOptions"
                searchable
                creatable
                clearable
                :placeholder="t('usage.errors.modelPlaceholder')"
                @change="applyErrorFilters"
              />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('usage.errors.category') }}</label>
              <Select v-model="errorFilter.category" :options="errorCategoryOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.errors.status') }}</label>
              <Select v-model="errorFilter.status_code" :options="errorStatusOptions" @change="applyErrorFilters" />
            </div>
          </div>
          <div v-else class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.apiKeyFilter') }}</label>
              <Select v-model="filters.api_key_id" :options="apiKeyOptions" @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.model') }}</label>
              <Select v-model="filters.model" :options="modelOptions" searchable @change="applyFilters" />
            </div>
            <details class="w-full" :open="hasAdvancedFilters || undefined">
              <summary class="cursor-pointer py-2">{{ consoleCopy.advanced }}</summary>
              <div class="flex flex-wrap items-end gap-4 pt-3">
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.group') }}</label>
              <Select v-model="filters.group_id" :options="groupOptions" searchable @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.type') }}</label>
              <Select v-model="filters.request_type" :options="requestTypeOptions" @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.compactionFilter') }}</label>
              <Select v-model="filters.native_compaction_v2" :options="compactionOptions" @change="applyFilters" />
            </div>
            <div v-if="subscriptionFeatureEnabled" class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.billingType') }}</label>
              <Select v-model="filters.billing_type" :options="billingTypeOptions" @change="applyFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.billingMode') }}</label>
              <Select v-model="filters.billing_mode" :options="billingModeOptions" @change="applyFilters" />
            </div>
              </div>
            </details>
          </div>

          <div class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
            <button type="button" @click="refreshData" :disabled="activeTab === 'errors' ? errorLoading : loading" class="btn btn-secondary">
              {{ t('common.refresh') }}
            </button>
            <button type="button" @click="resetFilters" class="btn btn-secondary">
              {{ t('common.reset') }}
            </button>
            <ColumnSettings
              v-if="usageView === 'records'"
              :columns="activeTab === 'errors' ? errOrderedColumns : orderedColumns"
              :hidden-columns="activeTab === 'errors' ? errHiddenColumns : hiddenColumns"
              :required-columns="activeTab === 'errors' ? ERR_ALWAYS_VISIBLE : ALWAYS_VISIBLE"
              @toggle="toggleCurrentColumn"
              @reorder="setCurrentColumnOrder"
              @reset="resetCurrentColumns"
            />
            <button v-if="usageView === 'records' && activeTab !== 'errors'" type="button" @click="exportToCSV" :disabled="exporting" class="btn btn-primary">
              {{ exporting ? t('usage.exporting') : t('usage.exportCsv') }}
            </button>
          </div>
        </div>
      </div>

      <div v-if="usageView === 'records' && errorViewEnabled" class="flex gap-2 border-b border-gray-200 dark:border-dark-700">
        <button class="tab" :class="{ 'tab-active': activeTab === 'usage' }" @click="switchToUsage">
          {{ t('usage.tabs.usage') }}
        </button>
        <button class="tab" :class="{ 'tab-active': activeTab === 'errors' }" @click="switchToErrors">
          {{ t('usage.tabs.errors') }}
        </button>
      </div>

      <template v-if="usageView === 'records' && activeTab === 'usage'">
        <UsageTable
          :data="usageLogs"
          :loading="loading"
          :columns="visibleColumns"
          @column-reorder="setColumnOrder"
          :server-side-sort="true"
          :show-account-billing="false"
          :show-upstream-endpoint="false"
          cache-rate-audience="user"
          :default-sort-key="sortState.sort_by"
          :default-sort-order="sortState.sort_order"
          @row-details="openRequestDetail"
          @sort="handleSort"
          @ipGeoBatchFailed="handleIpGeoBatchFailed"
        />

        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>

      <UserErrorRequestsTable
        v-else-if="usageView === 'records' && errorViewEnabled"
        :rows="errorRows"
        :total="errorTotal"
        :loading="errorLoading"
        :page="errorPage"
        :page-size="errorPageSize"
        :visible-column-keys="errVisibleColumnKeys"
        @column-reorder="setErrColumnOrder"
        :support-enabled="supportEnabled"
        :sort-by="errorSortBy"
        :sort-order="errorSortOrder"
        @sort="onErrorSort"
        @update:page="onErrorPage"
        @update:pageSize="onErrorPageSize"
        @ipGeoBatchFailed="handleIpGeoBatchFailed"
      />
      <UserUsageRequestDetail :record="selectedRequest" :support-enabled="supportEnabled" @close="closeRequestDetail" />
    </div>
  </AppLayout>

</template>

<script setup lang="ts">
import { getLast24HourRange as getLast24HoursRangeDates, getDatePresetRange, getGranularityForRange } from '@/utils/dateRange'
import { computed, onMounted, onUnmounted, reactive, ref, watch, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { routeLocationKey, routerKey } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { parseUsageLocation, usageLocationQuery } from './usageLocation'
import UserUsageRequestDetail from '@/components/user/UserUsageRequestDetail.vue'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { keysAPI, usageAPI, userGroupsAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import UserErrorRequestsTable from '@/components/user/UserErrorRequestsTable.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useTableColumns } from '@/composables/useTableColumns'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import { formatReasoningEffort } from '@/utils/format'
import { formatUsageCacheRate, showUsageCacheRate } from '@/utils/usageCacheRate'
import { getBillingModeLabel, getDisplayBillingMode as resolveDisplayBillingMode } from '@/utils/billingMode'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import type {
  ApiKey,
  EndpointStat,
  Group,
  GroupStat,
  ModelStat,
  TrendDataPoint,
  UsageLog,
  UsageQueryParams,
  UsageStatsResponse,
  UserErrorRequest,
} from '@/types'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'

const { t, locale } = useI18n()
const route = inject(routeLocationKey, undefined)
const router = inject(routerKey, undefined)
const authStore = useAuthStore()
const selectedRequest = ref<UsageLog | null>(null)
const usageView = ref<'analysis' | 'records'>('records')
const consoleCopy = computed(() => locale?.value === 'en' ? {analysis:'Usage analytics', records:'Request records', advanced:'More filters'} : locale?.value === 'zh' ? {analysis:'用量分析', records:'请求记录', advanced:'更多筛选'} : {analysis:'用量分析', records:'請求記錄', advanced:'更多篩選'})
const appStore = useAppStore()
const supportEnabled = computed(() => !authStore.isAdmin && !authStore.isObserver && resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.supportTickets))

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'

const usageStats = ref<UsageStatsResponse | null>(null)
const usageLogs = ref<UsageLog[]>([])
const trendData = ref<TrendDataPoint[]>([])
const requestedModelStats = ref<ModelStat[]>([])
const groupStats = ref<GroupStat[]>([])
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])

const analyticsErrors = reactive(new Set<string>())
const loading = ref(false)
const chartsLoading = ref(false)
const modelStatsLoading = ref(false)
const endpointStatsLoading = ref(false)
const exporting = ref(false)
const errorRows = ref<UserErrorRequest[]>([])
const errorLoading = ref(false)
const errorPage = ref(1)
const errorPageSize = ref(20)
const errorSortBy = ref('created_at')
const errorSortOrder = ref<'asc' | 'desc'>('desc')
const errorTotal = ref(0)
const errorFilter = ref<{ model: string | null; category: string; api_key_id: number | null; status_code: number | null }>({
  model: '',
  category: '',
  api_key_id: null,
  status_code: null,
})

const errorKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allKeys') },
  ...apiKeys.value.map((k) => ({ value: k.id, label: k.name })),
])

// 模型候选取自当前已加载错误中出现过的模型；creatable 允许输入任意片段做后端模糊。
const errorModelOptions = computed<SelectOption[]>(() => {
  const seen = new Set<string>()
  const opts: SelectOption[] = []
  for (const r of errorRows.value) {
    if (r.model && !seen.has(r.model)) {
      seen.add(r.model)
      opts.push({ value: r.model, label: r.model })
    }
  }
  return opts
})

const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber', 'session_blocked', 'session_identity']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

// 状态码候选用固定常用列表(与管理端 UsageFilters 共用常量),不受当前页数据限制:
// 后端 status_code 过滤对全量生效,若只列当前页出现过的码,用户就选不到仅在后续页的码。
const errorStatusOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const applyErrorFilters = () => {
  errorPage.value = 1
  syncLocation()
  void loadErrors()
}

let abortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0



const selectedPreset = ref<string | null>('last24Hours')
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
const granularity = ref<'day' | 'hour'>(getGranularityForRange(startDate.value, endDate.value))

const modelDistributionMetric = ref<DistributionMetric>('tokens')
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const activeTab = ref<'usage' | 'errors'>('usage')
const errorViewEnabled = computed(() => appStore.cachedPublicSettings?.allow_user_view_error_requests ?? false)
const includeCacheRate = computed(() => showUsageCacheRate(appStore.cachedPublicSettings, 'user'))

const filters = ref<UsageQueryParams>({
  start_date: startDate.value,
  end_date: endDate.value,
  request_type: undefined,
  native_compaction_v2: null,
  billing_type: null,
  billing_mode: null,
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc',
})

const granularityOptions = computed<SelectOption[]>(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') },
])
const requestTypeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
])
const compactionOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allCompactionTypes') },
  { value: true, label: t('usage.compactionOnly') },
])
// 订阅功能关闭后只剩余额计费，「计费类型」筛选（余额/订阅）失去意义，整块隐藏。
const subscriptionFeatureEnabled = computed(() => resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription))
const billingTypeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') },
])
const billingModeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') },
])

const apiKeys = ref<ApiKey[]>([])
const groups = ref<Group[]>([])
const modelOptionValues = ref<string[]>([])

const apiKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allApiKeys') },
  ...apiKeys.value.map((key) => ({ value: key.id, label: key.name })),
])
const groupOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allGroups') },
  ...groups.value.map((group) => ({ value: group.id, label: group.name })),
])
const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...modelOptionValues.value.map((model) => ({ value: model, label: model })),
])

const hasAdvancedFilters = computed(() => filters.value.group_id != null || !!filters.value.request_type || filters.value.native_compaction_v2 != null || filters.value.billing_type != null || !!filters.value.billing_mode)

const normalizedFilters = computed<UsageQueryParams>(() => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    ...filters.value,
    start_date: startDate.value,
    end_date: endDate.value,
    stream: legacyStream === null ? undefined : legacyStream,
  }
})

const buildUsageListParams = (page: number, pageSize: number): UsageQueryParams => ({
  page,
  page_size: pageSize,
  ...normalizedFilters.value,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order,
})

const loadLogs = async () => {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  loading.value = true
  try {
    const res = await usageAPI.query(buildUsageListParams(pagination.page, pagination.page_size), {
      signal: controller.signal,
    })
    if (!controller.signal.aborted) {
      usageLogs.value = res.items
      pagination.total = res.total
    }
  } catch (error: any) {
    if (error?.name !== 'AbortError' && error?.code !== 'ERR_CANCELED') {
      usageLogs.value = []
      pagination.total = 0
      appStore.showError(t('usage.failedToLoad'))
    }
  } finally {
    if (abortController === controller) loading.value = false
  }
}

const loadStats = async () => {
  const seq = ++statsReqSeq
  analyticsErrors.delete('stats')
  endpointStatsLoading.value = true
  try {
    const stats = await usageAPI.getStats(normalizedFilters.value)
    if (seq !== statsReqSeq) return
    usageStats.value = stats
    inboundEndpointStats.value = stats.endpoints || []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } catch (error) {
    if (seq !== statsReqSeq) return
    usageStats.value = null
    analyticsErrors.add('stats')
    console.error('Failed to load usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) endpointStatsLoading.value = false
  }
}

const loadModelStats = async () => {
  const seq = ++modelStatsReqSeq
  analyticsErrors.delete('model')
  modelStatsLoading.value = true
  try {
    const response = await usageAPI.getDashboardModels({
      ...normalizedFilters.value,
      model_source: 'requested',
    })
    if (seq !== modelStatsReqSeq) return
    requestedModelStats.value = response.models || []
    refreshModelOptions(response.models || [])
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    analyticsErrors.add('model')
    console.error('Failed to load model stats:', error)
    requestedModelStats.value = []
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  const seq = ++chartReqSeq
  analyticsErrors.delete('chart')
  chartsLoading.value = true
  try {
    const snapshot = await usageAPI.getDashboardSnapshotV2({
      ...normalizedFilters.value,
      granularity: granularity.value,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
    })
    if (seq !== chartReqSeq) return
    trendData.value = snapshot.trend || []
    groupStats.value = snapshot.groups || []
  } catch (error) {
    if (seq !== chartReqSeq) return
    analyticsErrors.add('chart')
    console.error('Failed to load chart data:', error)
    trendData.value = []
    groupStats.value = []
  } finally {
    if (seq === chartReqSeq) chartsLoading.value = false
  }
}

const refreshModelOptions = (models: ModelStat[]) => {
  const current = filters.value.model
  const set = new Set(modelOptionValues.value)
  models.forEach((item) => {
    if (item.model) set.add(item.model)
  })
  if (current) set.add(current)
  modelOptionValues.value = Array.from(set).sort()
}

const applyFilters = () => {
  pagination.page = 1
  errorPage.value = 1
  syncLocation()
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  resetErrorRows()
}

const onGranularityChange = () => { syncLocation(); void loadChartData() }

const refreshData = () => {
  const range = selectedPreset.value ? getDatePresetRange(selectedPreset.value) : null
  if (range) {
    startDate.value = range.start
    endDate.value = range.end
    filters.value.start_date = range.start
    filters.value.end_date = range.end
  }
  syncLocation()
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  if (activeTab.value === 'errors') void loadErrors()
}

const resetFilters = () => {
  selectedPreset.value = 'last24Hours'
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = {
    start_date: range.start,
    end_date: range.end,
    request_type: undefined,
    native_compaction_v2: null,
    billing_type: null,
    billing_mode: null,
  }
  granularity.value = getGranularityForRange(range.start, range.end)
  applyFilters()
  if (activeTab.value === 'errors') {
    errorFilter.value = { model: '', category: '', api_key_id: null, status_code: null }
    applyErrorFilters()
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  selectedPreset.value = range.preset
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value.start_date = range.startDate
  filters.value.end_date = range.endDate
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const handlePageChange = (page: number) => {
  pagination.page = page
  syncLocation()
  void loadLogs()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  syncLocation()
  void loadLogs()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  syncLocation()
  void loadLogs()
}

const handleIpGeoBatchFailed = () => {
  appStore.showError(t('usage.ipGeo.batchFailed'))
}

const getRequestTypeExportText = (log: UsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'cyber') return 'Cyber'
  if (requestType === 'live') return 'Live'
  if (requestType === 'ws_v2') return 'WS'
  if (requestType === 'stream') return 'Stream'
  if (requestType === 'sync') return 'Sync'
  return 'Unknown'
}

const getDisplayBillingMode = (
  row: Pick<UsageLog, 'billing_mode' | 'image_count'> | null | undefined
): string | null | undefined => resolveDisplayBillingMode(row)

const escapeCSVValue = (value: unknown): string => {
  if (value == null) return ''
  const str = String(value)
  const escaped = str.replace(/"/g, '""')
  if (str === '-') return str
  if (/^[=+\-@\t\r]/.test(str)) return `"\'${escaped}"`
  if (/[,"\n\r]/.test(str)) return `"${escaped}"`
  return str
}

const exportToCSV = async () => {
  if (pagination.total === 0) {
    appStore.showWarning(t('usage.noDataToExport'))
    return
  }
  exporting.value = true
  appStore.showInfo(t('usage.preparingExport'))
  try {
    const allLogs: UsageLog[] = []
    const pageSize = 100
    const exportParams = buildUsageListParams(1, pageSize)
    const totalPages = Math.ceil(pagination.total / pageSize)
    for (let page = 1; page <= totalPages; page++) {
      const response = await usageAPI.query({ ...exportParams, page })
      allLogs.push(...response.items)
    }
    if (allLogs.length === 0) {
      appStore.showWarning(t('usage.noDataToExport'))
      return
    }
    const headers = [
      'Time',
      'API Key Name',
      'Model',
      'Reasoning Effort',
      'Inbound Endpoint',
      'IP Address',
      'Type',
      'Billing Mode',
      'Input Tokens',
      'Output Tokens',
      'Cache Read Tokens',
      'Cache Creation Tokens',
      ...(includeCacheRate.value ? ['Cache Rate'] : []),
      'Rate Multiplier',
      'Billed Cost',
      'Original Cost',
      'First Token (ms)',
      'Duration (ms)',
    ]
    const rows = allLogs.map((log) => [
      log.created_at,
      log.api_key?.name || '',
      log.model,
      formatReasoningEffort(log.reasoning_effort),
      log.inbound_endpoint || '',
      log.ip_address || '',
      getRequestTypeExportText(log),
      getBillingModeLabel(getDisplayBillingMode(log), t),
      log.input_tokens,
      log.output_tokens,
      log.cache_read_tokens,
      log.cache_creation_tokens,
      ...(includeCacheRate.value ? [formatUsageCacheRate(log) ?? ''] : []),
      log.rate_multiplier,
      log.actual_cost.toFixed(8),
      log.total_cost.toFixed(8),
      log.first_token_ms ?? '',
      log.duration_ms ?? '',
    ].map(escapeCSVValue))
    const csvContent = [
      headers.map(escapeCSVValue).join(','),
      ...rows.map((row) => row.join(',')),
    ].join('\n')
    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `usage_${exportParams.start_date}_to_${exportParams.end_date}.csv`
    link.click()
    window.URL.revokeObjectURL(url)
    appStore.showSuccess(t('usage.exportSuccess'))
  } catch (error) {
    console.error('CSV Export failed:', error)
    appStore.showError(t('usage.exportFailed'))
  } finally {
    exporting.value = false
  }
}

const ALWAYS_VISIBLE = ['created_at']
const ERR_ALWAYS_VISIBLE = ['status', 'created_at']

const allColumns = computed(() => [
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'request_details', label: t('common.view'), sortable: false },
  { key: 'ip_address', label: 'IP', sortable: false },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
])

const {
  columns: visibleColumns,
  orderedColumns,
  hiddenColumns,
  toggleColumn,
  setColumnOrder,
  resetColumns,
} = useTableColumns('table-columns:user-usage:v1', allColumns, { required: ALWAYS_VISIBLE })

const errAllColumns = computed(() => [
  { key: 'created_at', label: t('usage.errors.time') },
  { key: 'key_name', label: t('usage.errors.keyName') },
  { key: 'model', label: t('usage.errors.model') },
  { key: 'status', label: t('usage.errors.status') },
  { key: 'message', label: t('usage.errors.message') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'group', label: t('admin.usage.group') },
  { key: 'type', label: t('usage.type') },
  { key: 'platform', label: t('usage.errors.platform') },
  { key: 'endpoint', label: t('usage.errors.endpoint') },
  { key: 'client_ip', label: 'IP' },
  { key: 'user_agent', label: t('usage.userAgent') },
])

const {
  columns: errColumns,
  orderedColumns: errOrderedColumns,
  hiddenColumns: errHiddenColumns,
  toggleColumn: toggleErrColumn,
  setColumnOrder: setErrColumnOrder,
  resetColumns: resetErrColumns,
} = useTableColumns('table-columns:user-errors:v1', errAllColumns, { required: ERR_ALWAYS_VISIBLE })

const errVisibleColumnKeys = computed(() => errColumns.value.map(column => column.key))
const toggleCurrentColumn = (key: string) =>
  activeTab.value === 'errors' ? toggleErrColumn(key) : toggleColumn(key)
const setCurrentColumnOrder = (keys: string[]) =>
  activeTab.value === 'errors' ? setErrColumnOrder(keys) : setColumnOrder(keys)
const resetCurrentColumns = () =>
  activeTab.value === 'errors' ? resetErrColumns() : resetColumns()

const loadApiKeys = async () => {
  const firstPage = await keysAPI.list(1, 100)
  const keys = [...firstPage.items]
  for (let page = 2; page <= firstPage.pages && keys.length > 0; page++) {
    const response = await keysAPI.list(page, 100)
    if (response.items.length === 0) break
    keys.push(...response.items)
  }
  return keys
}

const loadFilterOptions = async () => {
  try {
    const [keys, availableGroups] = await Promise.all([
      loadApiKeys(),
      userGroupsAPI.getAvailable(),
    ])
    apiKeys.value = keys
    groups.value = availableGroups
  } catch (error) {
    console.error('Failed to load usage filter options:', error)
  }
}

const resetErrorRows = () => {
  errorPage.value = 1
  if (activeTab.value === 'errors') {
    void loadErrors()
  } else {
    errorRows.value = []
    errorTotal.value = 0
  }
}

let errorReqSeq = 0
const loadErrors = async () => {
  const seq = ++errorReqSeq
  errorLoading.value = true
  try {
    const resp = await usageAPI.listMyErrorRequests({
      page: errorPage.value,
      page_size: errorPageSize.value,
      start_date: startDate.value,
      end_date: endDate.value,
      model: (errorFilter.value.model ?? '').trim() || undefined,
      category: errorFilter.value.category || undefined,
      api_key_id: errorFilter.value.api_key_id ?? undefined,
      status_code: errorFilter.value.status_code ?? undefined,
      sort_by: errorSortBy.value,
      sort_order: errorSortOrder.value,
    })
    if (seq !== errorReqSeq) return
    errorRows.value = resp.items
    errorTotal.value = resp.total
  } catch (error) {
    if (seq !== errorReqSeq) return
    errorRows.value = []
    errorTotal.value = 0
    console.error('[UsageView] loadErrors failed:', error)
    appStore.showError(t('usage.errors.failedToLoad'))
  } finally {
    if (seq === errorReqSeq) errorLoading.value = false
  }
}

const onErrorSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errorSortBy.value = sortBy
  errorSortOrder.value = sortOrder
  errorPage.value = 1
  syncLocation()
  void loadErrors()
}

const onErrorPage = (page: number) => {
  errorPage.value = page
  syncLocation()
  void loadErrors()
}

const onErrorPageSize = (pageSize: number) => {
  errorPageSize.value = pageSize
  errorPage.value = 1
  syncLocation()
  void loadErrors()
}

const switchToErrors = () => {
  errorFilter.value.api_key_id = filters.value.api_key_id ?? null
  errorFilter.value.model = filters.value.model ?? ''
  activeTab.value = 'errors'
  syncLocation()
  if (errorRows.value.length === 0) void loadErrors()
}

let pendingQuery = ''
const syncLocation = () => {
  if (!router) return
  const errors = activeTab.value === 'errors'
  const query = usageLocationQuery({
    usageId: selectedRequest.value?.id, view: usageView.value, tab: activeTab.value, start: startDate.value, end: endDate.value, customRange: true,
    page: errors ? errorPage.value : pagination.page, pageSize: errors ? errorPageSize.value : pagination.page_size,
    granularity: granularity.value, sortBy: errors ? errorSortBy.value : sortState.sort_by,
    sortOrder: errors ? errorSortOrder.value : sortState.sort_order,
    filters: errors ? { api_key_id: errorFilter.value.api_key_id ?? undefined, model: errorFilter.value.model || undefined } : filters.value,
    category: errorFilter.value.category, statusCode: errorFilter.value.status_code,
  })
  pendingQuery = JSON.stringify(router.resolve({ path: '/usage', query }).query)
  void router.push({ path: '/usage', query })
}
let detailReqSeq = 0
const openRequestDetail = (record: UsageLog) => {
  ++detailReqSeq
  selectedRequest.value = record
  syncLocation()
}
const closeRequestDetail = () => {
  ++detailReqSeq
  selectedRequest.value = null
  syncLocation()
}
const restoreRequestDetail = async (id?: number) => {
  const seq = ++detailReqSeq
  selectedRequest.value = null
  if (!id) return
  try {
    const result = await usageAPI.getById(id)
    if (seq === detailReqSeq) selectedRequest.value = result
  } catch {
    if (seq === detailReqSeq) appStore.showError(t('usage.failedToLoad'))
  }
}
const restoreLocation = () => {
  const state = parseUsageLocation(route?.query ?? {}, {start: defaultRange.start, end: defaultRange.end, pageSize: getPersistedPageSize()})
  void restoreRequestDetail(state.usageId)
  usageView.value = state.view as 'analysis' | 'records'
  activeTab.value = state.view !== 'analysis' && state.tab === 'errors' && errorViewEnabled.value ? 'errors' : 'usage'
  startDate.value = state.start
  endDate.value = state.end
  selectedPreset.value = state.customRange ? null : 'last24Hours'
  granularity.value = state.granularity ?? getGranularityForRange(state.start, state.end)
  filters.value = {...state.filters, start_date: state.start, end_date: state.end}
  errorFilter.value = {api_key_id: state.filters.api_key_id ?? null, model: state.filters.model ?? '', category: state.category, status_code: state.statusCode}
  refreshModelOptions([])
  pagination.page = state.page
  pagination.page_size = state.pageSize
  errorPage.value = state.page
  errorPageSize.value = state.pageSize
  sortState.sort_by = state.sortBy
  sortState.sort_order = state.sortOrder
  errorSortBy.value = state.sortBy
  errorSortOrder.value = state.sortOrder
}
const reloadCurrentData = () => {
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  if (activeTab.value === 'errors') void loadErrors()
}
const switchToUsage = () => {
  filters.value.api_key_id = errorFilter.value.api_key_id ?? undefined
  filters.value.model = errorFilter.value.model || undefined
  activeTab.value = 'usage'
  syncLocation()
  void loadLogs()
}
watch(() => route?.query, () => {
  const current = JSON.stringify(route?.query ?? {})
  if (current === pendingQuery) { pendingQuery = ''; return }
  restoreLocation()
  reloadCurrentData()
})
watch(errorViewEnabled, (enabled) => {
  if (enabled && (route?.query.tab === 'errors' || route?.query.view === 'errors' || route?.query.status === 'failed')) {
    restoreLocation()
    if (activeTab.value === 'errors') void loadErrors()
  }
  if (!enabled && activeTab.value === 'errors') {
    ++errorReqSeq
    activeTab.value = 'usage'
    errorRows.value = []
    errorTotal.value = 0
    syncLocation()
  }
})
onMounted(() => {
  restoreLocation()
  void loadFilterOptions()
  reloadCurrentData()
})

onUnmounted(() => {
  abortController?.abort()
  ++errorReqSeq
  ++detailReqSeq
  ++chartReqSeq
  ++statsReqSeq
  ++modelStatsReqSeq
})

watch(endpointDistributionSource, () => {
  // Endpoint source switching is handled by the chart component using already loaded stats.
})
</script>
