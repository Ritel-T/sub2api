<template>
  <AppLayout>
    <ConsoleObjectLinks v-if="!props.observerMode && filters.user_id === objectQueryId(route.query.user_id) && objectQueryId(route.query.user_id)" kind="user" :id="objectQueryId(route.query.user_id)!" context class="open-page mb-4" />
    <ConsoleObjectLinks v-if="!props.observerMode && filters.account_id === objectQueryId(route.query.account_id) && objectQueryId(route.query.account_id)" kind="account" :id="objectQueryId(route.query.account_id)!" context class="open-page mb-4" />
    <div class="open-page space-y-6">
      <div class="flex flex-wrap items-center border-b border-gray-200 px-2 dark:border-dark-700 sm:px-4">
        <button
          v-for="tab in detailTabs"
          :key="tab.key"
          type="button"
          data-testid="usage-detail-tab"
          class="-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-3 text-sm font-medium transition-colors sm:px-4"
          :class="activeTab === tab.key
            ? 'border-primary-500 text-primary-600 dark:text-primary-400'
            : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
          @click="switchTab(tab.key)"
        >
          <Icon :name="tab.icon" size="sm" />
          {{ tab.label }}
        </button>
      </div>
      <UsageFilters v-model="filters" ref="usageFiltersRef" flat :mode="activeTab" class="admin-usage-filters" data-testid="admin-usage-filters" :start-date="startDate" :end-date="endDate" :exporting="exporting" :model-options="modelNameOptions" @change="applyFilters" @refresh="refreshData" @reset="resetFilters" @cleanup="openCleanupDialog" @export="exportToExcel">
        <template #before-filters>
          <div class="usage-time-filter">
            <label class="input-label">{{ t('admin.dashboard.timeRange') }}</label>
            <DateRangePicker data-field-role="filter" :aria-label="t('admin.dashboard.timeRange')" enable-time :preset="selectedPreset"
              v-model:start-date="startDate"
              v-model:end-date="endDate"
              @change="onDateRangeChange"
            />
          </div>
          <div class="usage-granularity-filter">
            <label class="input-label">{{ t('admin.dashboard.granularity') }}</label>
            <Select v-model="granularity" :options="granularityOptions" :aria-label="t('admin.dashboard.granularity')" @change="loadChartData" />
          </div>
        </template>
        <template #after-reset>
          <div ref="tableToolbarTarget" />
          <ColumnSettings
            v-if="activeTab === 'usage' || activeTab === 'errors'"
            :columns="activeTab === 'errors' ? errOrderedColumns : orderedColumns"
            :hidden-columns="activeTab === 'errors' ? errHiddenColumns : hiddenColumns"
            :required-columns="activeTab === 'errors' ? ERR_ALWAYS_VISIBLE : ALWAYS_VISIBLE"
            @toggle="toggleCurrentColumn"
            @reorder="setCurrentColumnOrder"
            @reset="resetCurrentColumns"
          />
        </template>
      </UsageFilters>
      <UsageStatsCards :stats="usageStats" />
      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:source="modelDistributionSource"
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :upstream-model-stats="upstreamModelStats"
            :mapping-model-stats="mappingModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :enable-breakdown="!props.observerMode"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :enable-breakdown="!props.observerMode"
          />
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :enable-breakdown="!props.observerMode"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>
      <div class="open-section">
        <div v-show="activeTab === 'usage'" class="min-w-0">
          <UsageTable
            enable-timing-details
            :toolbar-target="activeTab === 'usage' ? tableToolbarTarget : null"
            flat
            :data="usageLogs"
            :loading="loading"
            :columns="visibleColumns"
            @column-reorder="setColumnOrder"
            :server-side-sort="true"
            :default-sort-key="'created_at'"
            :default-sort-order="'desc'"
            :cache-rate-audience="props.observerMode ? 'user' : 'admin'"
            @sort="handleSort"
            @userClick="handleUserClick"
            @ipGeoBatchFailed="handleIpGeoBatchFailed"
          />
          <InfiniteListSentinel :loading="logList.loading.value || logList.loadingMore.value" :error="logList.error.value" :has-more="logList.hasMore.value" @load-more="logList.loadMore" @retry="logList.retry" />
        </div>
        <div v-show="activeTab === 'errors'" class="min-w-0">
          <OpsErrorLogTable
            :toolbar-target="activeTab === 'errors' ? tableToolbarTarget : null"
            flat
            :rows="errRows" :total="errTotal" :loading="errLoading"
            :loading-more="errorList.loadingMore.value" :error="errorList.error.value" :has-more="errorList.hasMore.value"
            :visible-column-keys="errVisibleColumnKeys"
            :primary-column-keys="errPrimaryColumnKeys"
            @column-reorder="setErrColumnOrder"
            :user-clickable="!props.observerMode"
            @userClick="handleUserClick"
            @openErrorDetail="openError"
            @sort="onErrSort"
            @load-more="errorList.loadMore"
            @retry="errorList.retry"
            @ipGeoBatchFailed="handleIpGeoBatchFailed" />
        </div>
        <UsageConnections
          v-if="!props.observerMode && activeTab === 'connections'"
          :filters="connectionFilters"
          :refresh-key="connectionRefreshKey"
        />
        <!-- 懒挂载：首次切到该 tab 才请求排行数据，之后随筛选自动刷新 -->
        <div v-if="!props.observerMode && rankingMounted" v-show="activeTab === 'ranking'" class="min-w-0">
          <UserTokenRanking
            ref="rankingRef"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :model="filters.model"
            @select-user="handleRankingSelectUser"
          />
        </div>
      </div>
      <OpsErrorDetailModal v-model:show="showErrorModal" :error-id="selectedErrorId" :error-type="'request'" />
    </div>
  </AppLayout>
  <UsageExportProgress :show="exportProgress.show" :progress="exportProgress.progress" :current="exportProgress.current" :total="exportProgress.total" :estimated-time="exportProgress.estimatedTime" @cancel="cancelExport" />
  <UsageCleanupDialog
    v-if="!props.observerMode"
    :show="cleanupDialogVisible"
    :filters="filters"
    :start-date="startDate"
    :end-date="endDate"
    @close="cleanupDialogVisible = false"
  />
  <!-- Balance history modal triggered from usage table user click -->
  <UserBalanceHistoryModal
    v-if="!props.observerMode"
    :show="showBalanceHistoryModal"
    :user="balanceHistoryUser"
    :hide-actions="true"
    @close="showBalanceHistoryModal = false; balanceHistoryUser = null"
  />
</template>

<script setup lang="ts">
import { getLast24HourRange as getLast24HoursRangeDates, getDatePresetRange, getGranularityForRange, parseDateBoundary } from '@/utils/dateRange'
import { ref, reactive, computed, onMounted, onUnmounted, watch, provide } from 'vue'
import { observerUsageAPI } from '@/api/observerUsage'
import { observerUsageContext } from '@/components/admin/usage/observerUsageContext'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { useRoute } from 'vue-router'
import { objectQueryId } from './objectContext'
import ConsoleObjectLinks from '@/components/admin/ConsoleObjectLinks.vue'
import { useAppStore } from '@/stores/app'; import { adminAPI } from '@/api/admin'; import { adminUsageAPI } from '@/api/admin/usage'
import { useTableColumns } from '@/composables/useTableColumns'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import { formatReasoningEffort } from '@/utils/format'
import { formatUsageCacheRate, showUsageCacheRate } from '@/utils/usageCacheRate'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import AppLayout from '@/components/layout/AppLayout.vue'; import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'; import { useInfiniteList } from '@/composables/useInfiniteList'; import Select from '@/components/common/Select.vue'; import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'; import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'; import UsageExportProgress from '@/components/admin/usage/UsageExportProgress.vue'
import UsageConnections from '@/components/admin/usage/UsageConnections.vue'
import UserTokenRanking from '@/components/admin/usage/UserTokenRanking.vue'
import UsageCleanupDialog from '@/components/admin/usage/UsageCleanupDialog.vue'
import UserBalanceHistoryModal from '@/components/admin/user/UserBalanceHistoryModal.vue'
import OpsErrorLogTable from '@/views/admin/ops/components/OpsErrorLogTable.vue'
import OpsErrorDetailModal from '@/views/admin/ops/components/OpsErrorDetailModal.vue'
import { listErrorLogs } from '@/api/admin/ops'
import type { OpsErrorLog } from '@/api/admin/ops'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'; import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'; import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import Icon from '@/components/icons/Icon.vue'
import type { AdminUsageLog, TrendDataPoint, ModelStat, GroupStat, EndpointStat, AdminUser } from '@/types'; import type { AdminUsageStatsResponse, AdminUsageQueryParams } from '@/api/admin/usage'

const props = withDefaults(defineProps<{ observerMode?: boolean }>(), { observerMode: false })
provide(observerUsageContext, props.observerMode)
const { t } = useI18n()
const appStore = useAppStore()
type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'
const route = useRoute()
const tableToolbarTarget = ref<HTMLElement | null>(null)
const usageStats = ref<AdminUsageStatsResponse | null>(null); const exporting = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const requestedModelStats = ref<ModelStat[]>([]); const upstreamModelStats = ref<ModelStat[]>([]); const mappingModelStats = ref<ModelStat[]>([]); const groupStats = ref<GroupStat[]>([]); const chartsLoading = ref(false); const modelStatsLoading = ref(false); const granularity = ref<'day' | 'hour'>('hour')
const modelDistributionMetric = ref<DistributionMetric>('tokens')
const modelDistributionSource = ref<ModelDistributionSource>('requested')
const loadedModelSources = reactive<Record<ModelDistributionSource, boolean>>({
  requested: false,
  upstream: false,
  mapping: false,
})
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])
const endpointStatsLoading = ref(false)
let exportAbortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0
const exportProgress = reactive({ show: false, progress: 0, current: 0, total: 0, estimatedTime: '' })
const cleanupDialogVisible = ref(false)
// Balance history modal state
const showBalanceHistoryModal = ref(false)
const balanceHistoryUser = ref<AdminUser | null>(null)

const breakdownFilters = computed(() => {
  const f: Record<string, any> = {}
  if (filters.value.user_id) f.user_id = filters.value.user_id
  if (filters.value.api_key_id) f.api_key_id = filters.value.api_key_id
  if (filters.value.account_id) f.account_id = filters.value.account_id
  if (filters.value.group_id) f.group_id = filters.value.group_id
  if (filters.value.request_type != null) f.request_type = filters.value.request_type
  if (filters.value.native_compaction_v2 != null) f.native_compaction_v2 = filters.value.native_compaction_v2
  if (filters.value.billing_type != null) f.billing_type = filters.value.billing_type
  return f
})

const modelNameOptions = computed(() =>
  Array.from(new Set(requestedModelStats.value.map((m) => m.model).filter(Boolean))).sort()
)

const handleUserClick = async (userId: number) => {
  if (props.observerMode) return
  try {
    const user = await adminAPI.users.getById(userId, true)
    balanceHistoryUser.value = user
    showBalanceHistoryModal.value = true
  } catch {
    appStore.showError(t('admin.usage.failedToLoadUser'))
  }
}

// Drill down from the per-user token ranking: scope the whole usage view to
// that user and jump to the usage-detail tab so the drill-down is visible.
const handleRankingSelectUser = (userId: number, email: string) => {
  if (props.observerMode) return
  filters.value = { ...filters.value, user_id: userId }
  usageFiltersRef.value?.setUserKeyword?.(email || '')
  activeTab.value = 'usage'
  applyFilters()
}

const granularityOptions = computed(() => [{ value: 'day', label: t('admin.dashboard.day') }, { value: 'hour', label: t('admin.dashboard.hour') }])
// Use local timezone to avoid UTC timezone issues
const selectedPreset = ref<string | null>('last24Hours')
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start); const endDate = ref(defaultRange.end)
const filters = ref<AdminUsageQueryParams>({ user_id: undefined, model: undefined, group_id: undefined, request_type: undefined, native_compaction_v2: null, billing_type: null, start_date: startDate.value, end_date: endDate.value })
const logList = useInfiniteList<AdminUsageLog>({
  keyOf: row => row.id, pageSize: 50,
  fetchPage: (page, signal) => (props.observerMode ? observerUsageAPI.list : adminAPI.usage.list)(
    buildUsageListParams(page, 50, false), { signal }),
})
const { items: usageLogs, loading } = logList
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const getSingleQueryValue = (value: string | null | Array<string | null> | undefined): string | undefined => {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === 'string' && item.length > 0)
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

const applyRouteQueryFilters = () => {
  const queryStartDate = getSingleQueryValue(route.query.start_date)
  const queryEndDate = getSingleQueryValue(route.query.end_date)
  const queryUserId = props.observerMode ? undefined : objectQueryId(route.query.user_id)
  const queryAccountId = props.observerMode ? undefined : objectQueryId(route.query.account_id)
  if (queryStartDate || queryEndDate) selectedPreset.value = null

  if (queryStartDate) {
    startDate.value = queryStartDate
  }
  if (queryEndDate) {
    endDate.value = queryEndDate
  }

  filters.value = {
    ...filters.value,
    user_id: queryUserId,
    account_id: queryAccountId,
    start_date: startDate.value,
    end_date: endDate.value
  }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
}

const loadRouteUserFilterLabel = async () => {
  if (props.observerMode) return
  const requestedUserId = filters.value.user_id
  if (!requestedUserId) return
  const userSearchRevision = usageFiltersRef.value?.getUserSearchRevision?.()

  const routeUserFilterIsCurrent = () => (
    filters.value.user_id === requestedUserId
    && usageFiltersRef.value?.getUserSearchRevision?.() === userSearchRevision
  )

  try {
    const user = await adminAPI.users.getById(requestedUserId, true)
    if (!routeUserFilterIsCurrent()) return
    usageFiltersRef.value?.setUserKeyword?.(user.email || String(requestedUserId))
  } catch {
    if (!routeUserFilterIsCurrent()) return
    usageFiltersRef.value?.setUserKeyword?.(String(requestedUserId))
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  selectedPreset.value = range.preset
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value = {
    ...filters.value,
    start_date: range.startDate,
    end_date: range.endDate
  }
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const buildUsageListParams = (
  page: number,
  pageSize: number,
  exactTotal: boolean
): AdminUsageQueryParams => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    page,
    page_size: pageSize,
    exact_total: exactTotal,
    ...filters.value,
    stream: legacyStream === null ? undefined : legacyStream,
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
}

const loadLogs = () => logList.reset()
const loadStats = async (force = false) => {
  const seq = ++statsReqSeq
  endpointStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const s = await (props.observerMode ? observerUsageAPI.getStats : adminAPI.usage.getStats)({
      ...filters.value,
      stream: legacyStream === null ? undefined : legacyStream,
      ...(force ? { nocache: 1 } : {}),
    })
    if (seq !== statsReqSeq) return
    usageStats.value = s
    inboundEndpointStats.value = s.endpoints || []
    upstreamEndpointStats.value = s.upstream_endpoints || []
    endpointPathStats.value = s.endpoint_paths || []
  } catch (error) {
    if (seq !== statsReqSeq) return
    console.error('Failed to load usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) endpointStatsLoading.value = false
  }
}

// 失效模型统计缓存:仅标记需要重取,保留旧数据直到新数据到达(避免刷新时图表闪空)。
const invalidateModelStatsCache = () => {
  loadedModelSources.requested = false
  loadedModelSources.upstream = false
  loadedModelSources.mapping = false
}

const loadModelStats = async (source: ModelDistributionSource, force = false) => {
  if (!force && loadedModelSources[source]) {
    return
  }

  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const baseParams = {
      start_date: filters.value.start_date || startDate.value,
      end_date: filters.value.end_date || endDate.value,
      user_id: filters.value.user_id,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      native_compaction_v2: filters.value.native_compaction_v2,
      billing_type: filters.value.billing_type,
	  upstream_model_mismatch: filters.value.upstream_model_mismatch,
    }

    const response = await (props.observerMode ? observerUsageAPI.getModelStats : adminAPI.dashboard.getModelStats)({ ...baseParams, model_source: source })

    if (seq !== modelStatsReqSeq) return

    const models = response.models || []
    if (source === 'requested') {
      requestedModelStats.value = models
    } else if (source === 'upstream') {
      upstreamModelStats.value = models
    } else {
      mappingModelStats.value = models
    }
    loadedModelSources[source] = true
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    console.error('Failed to load model stats:', error)
    if (source === 'requested') {
      requestedModelStats.value = []
    } else if (source === 'upstream') {
      upstreamModelStats.value = []
    } else {
      mappingModelStats.value = []
    }
    loadedModelSources[source] = false
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  const seq = ++chartReqSeq
  chartsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const snapshot = await (props.observerMode ? observerUsageAPI.getSnapshotV2 : adminAPI.dashboard.getSnapshotV2)({
      start_date: filters.value.start_date || startDate.value,
      end_date: filters.value.end_date || endDate.value,
      granularity: granularity.value,
      user_id: filters.value.user_id,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      native_compaction_v2: filters.value.native_compaction_v2,
      billing_type: filters.value.billing_type,
	  upstream_model_mismatch: filters.value.upstream_model_mismatch,
      include_stats: false,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
      include_users_trend: false
    })
    if (seq !== chartReqSeq) return
    trendData.value = snapshot.trend || []
    groupStats.value = snapshot.groups || []
  } catch (error) { console.error('Failed to load chart data:', error) } finally { if (seq === chartReqSeq) chartsLoading.value = false }
}
const applyFilters = () => {
  invalidateModelStatsCache()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  if (activeTab.value === 'errors') {
    loadAdminErrors()
  } else {
    errRows.value = []
  }
}
const refreshData = () => {
  const range = selectedPreset.value ? getDatePresetRange(selectedPreset.value) : null
  if (range) {
    startDate.value = range.start
    endDate.value = range.end
    filters.value.start_date = range.start
    filters.value.end_date = range.end
  }
  connectionRefreshKey.value++
  invalidateModelStatsCache()
  loadLogs()
  loadStats(true)
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  if (activeTab.value === 'errors') loadAdminErrors()
  if (rankingMounted.value) rankingRef.value?.reload()
}
const resetFilters = () => {
  selectedPreset.value = 'last24Hours'
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = { start_date: startDate.value, end_date: endDate.value, request_type: undefined, native_compaction_v2: null, billing_type: null, billing_mode: undefined }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
  applyFilters()
}
const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  loadLogs()
}

const handleIpGeoBatchFailed = () => {
  appStore.showError(t('usage.ipGeo.batchFailed'))
}
const cancelExport = () => exportAbortController?.abort()
const openCleanupDialog = () => { if (!props.observerMode) cleanupDialogVisible.value = true }
const getRequestTypeLabel = (log: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'cyber') return t('usage.cyber')
  if (requestType === 'live') return t('usage.live')
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const exportToExcel = async () => {
  if (exporting.value) return; exporting.value = true; exportProgress.show = true
  const c = new AbortController(); exportAbortController = c
  try {
    let p = 1; let total = logList.total.value ?? 0; let exportedCount = 0
    const XLSX = await import('xlsx')
    const headers = [
      t('usage.time'), t('admin.usage.user'), t('usage.apiKeyFilter'),
      t('admin.usage.account'), t('usage.requestedModel'), t('usage.sentUpstreamModel'), t('usage.upstreamResponseModel'), t('usage.upstreamModelMismatch'), t('usage.requestedReasoningEffort'), t('usage.reasoningEffort'), t('admin.usage.group'),
      t('usage.inboundEndpoint'), t('usage.upstreamEndpoint'),
      t('usage.type'),
      t('admin.usage.inputTokens'), t('admin.usage.outputTokens'),
      t('admin.usage.cacheReadTokens'), t('admin.usage.cacheCreationTokens'),
      ...(showUsageCacheRate(appStore.cachedPublicSettings, props.observerMode ? 'user' : 'admin') ? [t('usage.latencyCacheRate')] : []),
      t('admin.usage.inputCost'), t('admin.usage.outputCost'),
      t('admin.usage.cacheReadCost'), t('admin.usage.cacheCreationCost'),
      t('usage.rate'), t('usage.accountMultiplier'), t('usage.original'), t('usage.userBilled'), t('usage.accountBilled'),
      t('usage.firstToken'), t('usage.duration'),
      t('admin.usage.requestId'), t('admin.usage.upstreamRequestId'), t('usage.userAgent'), t('admin.usage.ipAddress')
    ]
    const ws = XLSX.utils.aoa_to_sheet([headers])
    while (true) {
      const res = await (props.observerMode ? observerUsageAPI.list : adminUsageAPI.list)(
        buildUsageListParams(p, 100, true),
        { signal: c.signal }
      )
      if (c.signal.aborted) break; if (p === 1) { total = res.total; exportProgress.total = total }
      const rows = (res.items || []).map((log: AdminUsageLog) => [
        log.created_at, log.user?.email || '', log.api_key?.name || '', log.account?.name || '', log.model,
        log.upstream_model || log.model, log.upstream_response_model || '', log.upstream_model_mismatch == null ? '' : t(log.upstream_model_mismatch ? 'common.yes' : 'common.no'), formatReasoningEffort(log.reasoning_effort), formatReasoningEffort(log.upstream_reasoning_effort || log.reasoning_effort), log.group?.name || '',
        log.inbound_endpoint || '', log.upstream_endpoint || '', getRequestTypeLabel(log),
        log.input_tokens, log.output_tokens, log.cache_read_tokens, log.cache_creation_tokens,
        ...(showUsageCacheRate(appStore.cachedPublicSettings, props.observerMode ? 'user' : 'admin') ? [formatUsageCacheRate(log) ?? ''] : []),
        log.input_cost?.toFixed(6) || '0.000000', log.output_cost?.toFixed(6) || '0.000000',
        log.cache_read_cost?.toFixed(6) || '0.000000', log.cache_creation_cost?.toFixed(6) || '0.000000',
        log.rate_multiplier?.toPrecision(4) || '1.00', (log.account_rate_multiplier ?? 1).toPrecision(4),
        log.total_cost?.toFixed(6) || '0.000000', log.actual_cost?.toFixed(6) || '0.000000',
        ((log.account_stats_cost ?? log.total_cost) * (log.account_rate_multiplier ?? 1)).toFixed(6), log.first_token_ms ?? '', log.duration_ms,
        log.request_id || '', log.upstream_request_id || '', log.user_agent || '', log.ip_address || ''
      ])
      if (rows.length) {
        XLSX.utils.sheet_add_aoa(ws, rows, { origin: -1 })
      }
      exportedCount += rows.length
      exportProgress.current = exportedCount
      exportProgress.progress = total > 0 ? Math.min(100, Math.round(exportedCount / total * 100)) : 0
      if (exportedCount >= total || res.items.length < 100) break; p++
    }
    if(!c.signal.aborted) {
      const wb = XLSX.utils.book_new()
      XLSX.utils.book_append_sheet(wb, ws, 'Usage')
      saveAs(new Blob([XLSX.write(wb, { bookType: 'xlsx', type: 'array' })], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }), `usage_${filters.value.start_date}_to_${filters.value.end_date}.xlsx`)
      appStore.showSuccess(t('usage.exportSuccess'))
    }
  } catch (error) { console.error('Failed to export:', error); appStore.showError('Export Failed') }
  finally { if(exportAbortController === c) { exportAbortController = null; exporting.value = false; exportProgress.show = false } }
}

const ALWAYS_VISIBLE = ['user', 'created_at']
const ERR_ALWAYS_VISIBLE = ['user', 'status', 'created_at', 'actions']

const allColumns = computed(() => [
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'user', label: t('admin.usage.user'), sortable: false },
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'account', label: t('admin.usage.account'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'ip_address', label: t('admin.usage.ipAddress'), sortable: false },
  { key: 'request_id', label: t('admin.usage.requestId'), sortable: false },
  { key: 'upstream_request_id', label: t('admin.usage.upstreamRequestId'), sortable: false },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
])

const {
  columns: visibleColumns,
  orderedColumns,
  hiddenColumns,
  toggleColumn,
  setColumnOrder,
  resetColumns,
} = useTableColumns('table-columns:admin-usage:v1', allColumns, { required: ALWAYS_VISIBLE })

const errAllColumns = computed(() => [
  { key: 'created_at', label: t('admin.ops.errorLog.time') },
  { key: 'user', label: t('admin.ops.errorLog.user') },
  { key: 'api_key', label: t('admin.ops.errorLog.apiKey') },
  { key: 'account', label: t('admin.ops.errorLog.account') },
  { key: 'model', label: t('admin.ops.errorLog.model') },
  { key: 'status', label: t('admin.ops.errorLog.status') },
  { key: 'message', label: t('admin.ops.errorLog.message') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'group', label: t('admin.ops.errorLog.group') },
  { key: 'type', label: t('admin.ops.errorLog.type') },
  { key: 'platform', label: t('admin.ops.errorLog.platform') },
  { key: 'endpoint', label: t('admin.ops.errorLog.endpoint') },
  { key: 'actions', label: t('admin.ops.errorLog.action') },
  { key: 'client_ip', label: t('admin.ops.errorLog.ip') },
  { key: 'user_agent', label: t('usage.userAgent') },
])

const {
  columns: errColumns,
  orderedColumns: errOrderedColumns,
  hiddenColumns: errHiddenColumns,
  toggleColumn: toggleErrColumn,
  setColumnOrder: setErrColumnOrder,
  resetColumns: resetErrColumns,
} = useTableColumns('table-columns:admin-errors:v1', errAllColumns, { required: ERR_ALWAYS_VISIBLE })

const errVisibleColumnKeys = computed(() => errColumns.value.map(column => column.key))
const errPrimaryColumnKeys = computed(() => errColumns.value.filter(column => column.primary !== false).map(column => column.key))
const toggleCurrentColumn = (key: string) =>
  activeTab.value === 'errors' ? toggleErrColumn(key) : toggleColumn(key)
const setCurrentColumnOrder = (keys: string[]) =>
  activeTab.value === 'errors' ? setErrColumnOrder(keys) : setColumnOrder(keys)
const resetCurrentColumns = () =>
  activeTab.value === 'errors' ? resetErrColumns() : resetColumns()

// Detail tabs
type DetailTab = 'usage' | 'errors' | 'connections' | 'ranking'
const activeTab = ref<DetailTab>('usage')
const canViewErrors = computed(() => !props.observerMode || !!appStore.cachedPublicSettings?.allow_user_view_error_requests)
const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
  ...(canViewErrors.value ? [{ key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const }] : []),
  ...(!props.observerMode ? [{ key: 'connections' as const, label: t('connections.title'), icon: 'clock' as const }] : []),
  ...(!props.observerMode ? [{ key: 'ranking' as const, label: t('usage.tabs.ranking'), icon: 'chart' as const }] : []),
])
const usageFiltersRef = ref<InstanceType<typeof UsageFilters> | null>(null)
const connectionRefreshKey = ref(0)
const connectionFilters = computed(() => ({
  start_time: toRFC3339(filters.value.start_date),
  end_time: toRFC3339(filters.value.end_date, true),
  user_id: filters.value.user_id ?? undefined,
  api_key_id: filters.value.api_key_id ?? undefined,
  account_id: filters.value.account_id ?? undefined,
  group_id: filters.value.group_id ?? undefined,
  model: filters.value.model || undefined,
}))
const rankingMounted = ref(false)
const rankingRef = ref<InstanceType<typeof UserTokenRanking> | null>(null)

const switchTab = (tab: DetailTab) => {
  if (!detailTabs.value.some(item => item.key === tab)) return
  activeTab.value = tab
  if (tab === 'errors' && errRows.value.length === 0) loadAdminErrors()
  if (tab === 'ranking') rankingMounted.value = true
}

// Error tab state
const errSortBy = ref('created_at')
const errSortOrder = ref<'asc' | 'desc'>('desc')
const showErrorModal = ref(false)
const selectedErrorId = ref<number | null>(null)

// Error queries share the same exclusive end as usage records and statistics.
const toRFC3339 = (d: string | undefined, endOfDay = false): string | undefined =>
  d ? parseDateBoundary(d, endOfDay).toISOString() : undefined

const errorList = useInfiniteList<OpsErrorLog>({
  keyOf: row => row.id, pageSize: 50,
  fetchPage: async page => {
    if (!canViewErrors.value) return { items: [], total: 0 }
    return (props.observerMode ? observerUsageAPI.listErrors : listErrorLogs)({
      ...(props.observerMode ? { start_date: filters.value.start_date, end_date: filters.value.end_date, status_code: filters.value.status_code } : {}),
      page,
      page_size: 50,
      view: 'all',
      start_time: toRFC3339(filters.value.start_date),
      end_time: toRFC3339(filters.value.end_date, true),
      user_id: filters.value.user_id ?? undefined,
      api_key_id: filters.value.api_key_id ?? undefined,
      account_id: filters.value.account_id ?? undefined,
      group_id: filters.value.group_id ?? undefined,
      model: filters.value.model || undefined,
      phase: filters.value.error_phase || undefined,
      category: filters.value.error_category || undefined,
      status_codes: filters.value.status_code != null ? String(filters.value.status_code) : undefined,
      sort_by: errSortBy.value,
      sort_order: errSortOrder.value,
    })
  },
})
const { items: errRows, loading: errLoading } = errorList
const errTotal = computed(() => errorList.total.value ?? 0)
const loadAdminErrors = () => errorList.reset()

const onErrSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errSortBy.value = sortBy
  errSortOrder.value = sortOrder
  loadAdminErrors()
}
const openError = (id: number) => { selectedErrorId.value = id; showErrorModal.value = true }

watch(() => [route.query.user_id, route.query.account_id, route.query.start_date, route.query.end_date], () => {
  applyRouteQueryFilters()
  void loadRouteUserFilterLabel()
  applyFilters()
})
onMounted(() => {
  applyRouteQueryFilters()
  void loadRouteUserFilterLabel()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  window.setTimeout(() => {
    void loadChartData()
  }, 120)
})
onUnmounted(() => { exportAbortController?.abort() })

watch(modelDistributionSource, (source) => {
  void loadModelStats(source)
})

defineExpose({ requestedModelStats, refreshData })
</script>

<style scoped>
.usage-time-filter { flex: 0 1 auto; min-width: 0; max-width: 100%; }
.usage-granularity-filter { flex: 0 0 7rem; min-width: 0; }
.admin-usage-filters :deep(.usage-filter-controls) { align-items: flex-end; }
</style>
