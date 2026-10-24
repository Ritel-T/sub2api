<template>
  <AppLayout :show-page-title="false">
    <div class="open-page w-full min-w-0 space-y-6 pb-8">
      <header
        class="open-section page-header mb-0 p-5"
      >
        <AppHeading variant="page" as="h1" class="page-title flex items-center gap-2 text-gray-900 dark:text-white">
          <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
            <Icon name="chart" size="sm" />
          </span>
          {{ t('admin.channelMonitor.title') }}
        </AppHeading>
        <p class="page-description mt-1.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t(`channelMonitorV3.admin.description.${siteMode}`) }}
        </p>
        <div class="open-section mt-4 flex flex-wrap items-center gap-x-3 gap-y-2" data-testid="monitor-mode-switch">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('channelMonitorV3.admin.siteMode') }}</span>
          <div class=" inline-flex p-0.5" role="radiogroup" :aria-label="t('channelMonitorV3.admin.siteMode')">
            <button
              v-for="mode in modes"
              :key="mode"
              type="button"
              role="radio"
              :aria-checked="siteMode === mode"
              class="rounded-md px-3 py-1 text-xs font-medium transition disabled:cursor-not-allowed"
              :class="siteMode === mode ? 'bg-primary-600 text-white shadow-sm' : 'text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
              :disabled="switchingMode || siteMode === 'off'"
              :data-testid="`monitor-mode-${mode}`"
              @click="pendingMode = mode"
            >
              {{ t(`channelMonitorV3.modes.${mode}`) }}
            </button>
          </div>
          <span class="text-xs text-gray-500 dark:text-gray-400">
            {{ siteMode === 'off' ? t('channelMonitorV3.admin.siteModeOff') : t('channelMonitorV3.admin.siteModeHint') }}
            <router-link v-if="siteMode === 'off'" to="/admin/settings" class="ml-1 text-primary-600 hover:underline dark:text-primary-400">{{ t('admin.settings.tabs.features') }}</router-link>
          </span>
        </div>
        <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700">
          <div
            class="tabs inline-flex w-full max-w-2xl flex-wrap sm:w-auto"
            role="tablist"
            :aria-label="t('channelMonitorV2.admin.tabAria')"
          >
            <button
              type="button"
              role="tab"
              class="tab flex-1 sm:flex-none"
              :class="adminMonitorTab === 'v3' ? 'tab-active' : ''"
              :aria-selected="adminMonitorTab === 'v3'"
              data-testid="monitor-tab-v3"
              @click="adminMonitorTab = 'v3'"
            >
              {{ t('channelMonitorV3.admin.tab') }}
            </button>
            <button
              type="button"
              role="tab"
              class="tab flex-1 sm:flex-none"
              :class="adminMonitorTab === 'v2' ? 'tab-active' : ''"
              :aria-selected="adminMonitorTab === 'v2'"
              @click="adminMonitorTab = 'v2'"
            >
              {{ t('channelMonitorV2.admin.tabV2') }}
            </button>
            <button
              type="button"
              role="tab"
              class="tab flex-1 sm:flex-none"
              :class="adminMonitorTab === 'legacy' ? 'tab-active' : ''"
              :aria-selected="adminMonitorTab === 'legacy'"
              @click="adminMonitorTab = 'legacy'"
            >
              {{ isV1Mode ? t('channelMonitorV2.admin.tabV1Active') : t('channelMonitorV2.admin.tabV1History') }}
            </button>
          </div>
        </div>
      </header>

      <V3SettingsPanel v-if="adminMonitorTab === 'v3'" />
      <MonitorSettingsPanel v-else-if="adminMonitorTab === 'v2'" />

      <TablePageLayout open v-else>
      <template #filters>
        <MonitorFiltersBar
          v-model:search="searchQuery"
          v-model:provider="providerFilter"
          v-model:enabled="enabledFilter"
          :loading="loading"
          @reload="reload"
          @create="openCreateDialog"
          @manage-templates="showTemplateManager = true"
          @search-input="handleSearch"
        />
      </template>

      <template #table>
        <DataTable column-order-key="table-columns:channel-monitors:v1" :columns="columns" :data="monitors" :loading="loading">
          <template #cell-name="{ row, value }">
            <div class="flex items-center gap-1.5">
              <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
              <HelpTooltip v-if="row.api_key_decrypt_failed" :content="t('admin.channelMonitor.apiKeyDecryptFailed')">
                <Icon name="exclamationTriangle" size="sm" class="text-red-500" />
              </HelpTooltip>
            </div>
          </template>

          <template #cell-provider="{ row }">
            <span class="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium" :class="providerBadgeClass(row.provider)">
              {{ providerLabel(row.provider) }}
            </span>
            <!-- 三种检测模式并列展示，quota 系配额数据源与纯探活一眼可分 -->
            <span class="ml-1 inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium" :class="checkModeBadgeClass(row.check_mode)">
              {{ checkModeLabel(row.check_mode) }}
            </span>
          </template>

          <template #cell-primary_model="{ row }">
            <MonitorPrimaryModelCell :row="row" />
          </template>

          <template #cell-availability_7d="{ row }">
            <span class="text-sm text-gray-900 dark:text-gray-100">{{ formatAvailability(row) }}</span>
          </template>

          <template #cell-latency="{ row }">
            <span class="text-sm text-gray-900 dark:text-gray-100">{{ formatLatency(row.primary_latency_ms) }}</span>
          </template>

          <template #cell-enabled="{ row }">
            <Toggle :aria-label="t('admin.channelMonitor.form.enabled') + ': ' + row.name" :modelValue="row.enabled" @update:modelValue="toggleEnabled(row)" />
          </template>

          <template #cell-actions="{ row }">
            <MonitorActionsCell
              :row="row"
              :running="runningId === row.id"
              :duplicating="duplicatingIds.has(row.id)"
              @run="handleRunNow"
              @duplicate="handleDuplicate"
              @edit="openEditDialog"
              @delete="handleDelete"
            />
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.channelMonitor.noMonitorsYet')"
              :description="t('admin.channelMonitor.createFirstMonitor')"
              :action-text="t('admin.channelMonitor.createButton')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <InfiniteListSentinel :loading="list.loading.value || list.loadingMore.value" :error="list.error.value" :has-more="list.hasMore.value" @load-more="list.loadMore" @retry="list.retry" />
      </template>
      </TablePageLayout>
    </div>

    <MonitorFormDialog
      :show="showDialog"
      :monitor="editing"
      @close="closeDialog"
      @saved="reload"
    />

    <MonitorTemplateManagerDialog
      :show="showTemplateManager"
      @close="showTemplateManager = false"
      @updated="reload"
    />

    <MonitorRunResultDialog
      :show="showRunResult"
      :results="runResults"
      @close="showRunResult = false"
    />

    <ConfirmDialog :busy="applyModeAction.busy.value"
      :show="pendingMode !== null"
      :title="t('channelMonitorV3.admin.switchTitle')"
      :message="pendingMode ? t('channelMonitorV3.admin.switchConfirm', { mode: t(`channelMonitorV3.modes.${pendingMode}`), effect: t(`channelMonitorV3.admin.switchEffect.${pendingMode}`) }) : ''"
      :confirm-text="t('channelMonitorV3.admin.switchAction')"
      :cancel-text="t('common.cancel')"
      @confirm="applyMode"
      @cancel="pendingMode = null"
    />

    <ConfirmDialog :busy="confirmDeleteAction.busy.value"
      :show="showDeleteDialog"
      :title="t('common.delete')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import type {
  ChannelMonitor,
  CheckResult,
  ListParams,
  Provider,
} from '@/api/admin/channelMonitor'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useConfirmedAction } from '@/composables/useConfirmation'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import MonitorFiltersBar from '@/components/admin/monitor/MonitorFiltersBar.vue'
import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import MonitorTemplateManagerDialog from '@/components/admin/monitor/MonitorTemplateManagerDialog.vue'
import MonitorRunResultDialog from '@/components/admin/monitor/MonitorRunResultDialog.vue'
import MonitorPrimaryModelCell from '@/components/admin/monitor/MonitorPrimaryModelCell.vue'
import MonitorActionsCell from '@/components/admin/monitor/MonitorActionsCell.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import MonitorSettingsPanel from '@/features/channel-monitor-v2/MonitorSettingsPanel.vue'
import V3SettingsPanel from '@/features/channel-monitor-v3/V3SettingsPanel.vue'
import { setMonitorMode } from '@/api/channelMonitorV3'
import { getChannelMonitorMode, isChannelMonitorV1Mode, type ChannelMonitorMode } from '@/utils/featureFlags'

const { t } = useI18n()
const appStore = useAppStore()
const isV1Mode = computed(() => isChannelMonitorV1Mode())
const modes: ChannelMonitorMode[] = ['v1', 'v2', 'v3']
const siteMode = computed(() => (appStore.cachedPublicSettings?.channel_monitor_enabled === false ? 'off' : getChannelMonitorMode()))
const tabForMode = (mode: ChannelMonitorMode) => (mode === 'v1' ? 'legacy' : mode)
const adminMonitorTab = ref<'v3' | 'v2' | 'legacy'>(tabForMode(getChannelMonitorMode()))
const pendingMode = ref<ChannelMonitorMode | null>(null)
const switchingMode = ref(false)

function applyMode() {
  return applyModeAction.run(async (isCurrent) => {
    const mode = pendingMode.value
    if (!mode || mode === siteMode.value) return
    switchingMode.value = true
    try {
      await setMonitorMode(mode)
      if (!isCurrent()) return
      // Sidebar, the user page and the runners all follow the public settings.
      await appStore.fetchPublicSettings(true)
      if (!isCurrent()) return
      adminMonitorTab.value = tabForMode(mode)
      pendingMode.value = null
      appStore.showSuccess(t('channelMonitorV3.admin.switched', { mode: t(`channelMonitorV3.modes.${mode}`) }))
    } catch (err: unknown) {
      if (!isCurrent()) return
      appStore.showError(extractApiErrorMessage(err, t('channelMonitorV3.admin.switchFailed')))
    } finally {
      switchingMode.value = false
    }
  })
}
const {
  providerLabel,
  providerBadgeClass,
  checkModeLabel,
  checkModeBadgeClass,
  formatLatency,
  formatAvailability,
} = useChannelMonitorFormat()

let appliedSearch = ''
const list = useInfiniteList<ChannelMonitor>({
  keyOf: row => row.id, pageSize: 50,
  fetchPage: async (page, signal) => {
    const params: ListParams = { page, page_size: 50 }
    if (providerFilter.value) params.provider = providerFilter.value
    if (enabledFilter.value === 'true') params.enabled = true
    if (enabledFilter.value === 'false') params.enabled = false
    if (appliedSearch) params.search = appliedSearch
    return adminAPI.channelMonitor.list(params, { signal })
  },
})
const { items: monitors, loading } = list
const runningId = ref<number | null>(null)
const searchQuery = ref('')
const providerFilter = ref<Provider | ''>('')
const enabledFilter = ref<'' | 'true' | 'false'>('')

const showDialog = ref(false)
const showTemplateManager = ref(false)
const editing = ref<ChannelMonitor | null>(null)
const showDeleteDialog = ref(false)
const deleting = ref<ChannelMonitor | null>(null)
const showRunResult = ref(false)
const runResults = ref<CheckResult[]>([])
const duplicatingIds = reactive(new Set<number>())

let searchTimeout: ReturnType<typeof setTimeout> | null = null

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.channelMonitor.columns.name'), sortable: false },
  { key: 'enabled', label: t('admin.channelMonitor.columns.enabled'), sortable: false },
  { key: 'availability_7d', label: t('admin.channelMonitor.columns.availability7d'), sortable: false },
  { key: 'latency', label: t('admin.channelMonitor.columns.latency'), sortable: false },
  { key: 'provider', label: t('admin.channelMonitor.columns.provider'), sortable: false },
  { key: 'primary_model', label: t('admin.channelMonitor.columns.primaryModel'), sortable: false },
  { key: 'actions', label: t('admin.channelMonitor.columns.actions'), sortable: false }
])

const deleteConfirmMessage = computed(() => {
  const name = deleting.value?.name || ''
  return t('admin.channelMonitor.deleteConfirm', { name })
})

async function reload() {
  appliedSearch = searchQuery.value.trim()
  await list.reset()
  if (list.error.value) appStore.showError(extractApiErrorMessage(list.error.value, t('admin.channelMonitor.loadError')))
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    reload()
  }, 300)
}

function openCreateDialog() {
  editing.value = null
  showDialog.value = true
}

function openEditDialog(row: ChannelMonitor) {
  editing.value = row
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editing.value = null
}

async function toggleEnabled(row: ChannelMonitor) {
  const next = !row.enabled
  try {
    await adminAPI.channelMonitor.update(row.id, { enabled: next })
    row.enabled = next
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}

async function handleRunNow(row: ChannelMonitor) {
  if (!isV1Mode.value) {
    appStore.showError(t('admin.channelMonitor.runFailed'))
    return
  }
  if (runningId.value != null) return
  runningId.value = row.id
  try {
    const res = await adminAPI.channelMonitor.runNow(row.id)
    runResults.value = res.results || []
    showRunResult.value = true
    appStore.showSuccess(t('admin.channelMonitor.runSuccess'))
    // Refresh row to get latest status from backend
    void reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.runFailed')))
  } finally {
    runningId.value = null
  }
}

async function handleDuplicate(row: ChannelMonitor) {
  if (row.api_key_decrypt_failed) {
    appStore.showError(t('admin.channelMonitor.duplicateKeyUnavailable'))
    return
  }
  if (duplicatingIds.has(row.id)) return

  duplicatingIds.add(row.id)
  try {
    const duplicate = await adminAPI.channelMonitor.duplicate(row.id)
    appStore.showSuccess(t('admin.channelMonitor.duplicateSuccess', { name: duplicate.name }))
    await reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.duplicateFailed')))
  } finally {
    duplicatingIds.delete(row.id)
  }
}

function handleDelete(row: ChannelMonitor) {
  deleting.value = row
  showDeleteDialog.value = true
}

function confirmDelete() {
  return confirmDeleteAction.run(async (isCurrent) => {
    const targetId = deleting.value?.id
    if (targetId == null) return

    if (!deleting.value) return
    try {
      await adminAPI.channelMonitor.del(targetId)
      if (!isCurrent()) return
      deleting.value = null
      appStore.showSuccess(t('admin.channelMonitor.deleteSuccess'))
      showDeleteDialog.value = false
        reload()
    } catch (err: unknown) {
      if (!isCurrent()) return
      appStore.showError(extractApiErrorMessage(err, t('common.error')))
    }
  })
}

watch(adminMonitorTab, (tab) => {
  if (tab === 'legacy' && monitors.value.length === 0) void reload()
})
onMounted(() => {
  if (adminMonitorTab.value === 'legacy') void reload()
})
onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
})

// Direct confirmations share one execution and stale-context contract.
const applyModeAction = useConfirmedAction(() => ({ active: pendingMode.value !== null, identity: pendingMode.value }))
watch(() => ({ active: pendingMode.value !== null, identity: JSON.stringify(pendingMode.value) }), (next, previous) => { if (previous.active && next.active && next.identity !== previous.identity) { pendingMode.value = null } }, { flush: 'sync' })
const confirmDeleteAction = useConfirmedAction(() => ({ active: showDeleteDialog.value, identity: deleting.value?.id }))
watch(() => ({ active: showDeleteDialog.value, identity: JSON.stringify(deleting.value?.id) }), (next, previous) => { if (previous.active && next.active && next.identity !== previous.identity) { showDeleteDialog.value = false } }, { flush: 'sync' })
</script>
