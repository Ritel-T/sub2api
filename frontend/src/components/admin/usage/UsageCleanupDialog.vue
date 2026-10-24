<template>
  <BaseDialog :show="show" :title="t('admin.usage.cleanup.title')" width="wide" @close="handleClose">
    <div class="space-y-4">
      <UsageFilters
        v-model="localFilters"
        v-model:startDate="localStartDate"
        v-model:endDate="localEndDate"
        :exporting="false"
        :show-actions="false"
        @change="noop"
      />

      <div class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
        {{ t('admin.usage.cleanup.warning') }}
      </div>

      <div class="open-section pt-4">
        <div class="flex items-center justify-between">
          <AppHeading as="h4" variant="minor" class="text-gray-700 dark:text-gray-200">
            {{ t('admin.usage.cleanup.recentTasks') }}
          </AppHeading>
          <button type="button" class="btn btn-ghost btn-sm" @click="loadTasks">
            {{ t('common.refresh') }}
          </button>
        </div>

        <div class="mt-3 space-y-2">
          <div v-if="tasksLoading" class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.usage.cleanup.loadingTasks') }}
          </div>
          <div v-else-if="tasks.length === 0" class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.usage.cleanup.noTasks') }}
          </div>
          <div v-else class="space-y-2">
            <div
              v-for="task in tasks"
              :key="task.id"
              class="flex flex-col gap-2 border-b border-gray-100 py-3 text-sm text-gray-600 dark:border-dark-700 dark:text-gray-300"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div class="flex items-center gap-2">
                  <span :class="statusClass(task.status)" class="rounded-full px-2 py-0.5 text-xs font-semibold">
                    {{ statusLabel(task.status) }}
                  </span>
                  <span class="text-xs text-gray-400">#{{ task.id }}</span>
                  <button
                    v-if="canCancel(task)"
                    type="button"
                    class="btn btn-ghost btn-xs text-rose-600 hover:text-rose-700 dark:text-rose-300"
                    @click="openCancelConfirm(task)"
                  >
                    {{ t('admin.usage.cleanup.cancel') }}
                  </button>
                </div>
                <div class="text-xs text-gray-400">
                  {{ formatDateTime(task.created_at) }}
                </div>
              </div>
              <div class="flex flex-wrap items-center gap-4 text-xs text-gray-500 dark:text-gray-400">
                <span>{{ t('admin.usage.cleanup.range') }}: {{ formatRange(task) }}</span>
                <span>{{ t('admin.usage.cleanup.deletedRows') }}: {{ task.deleted_rows.toLocaleString() }}</span>
              </div>
              <div v-if="task.error_message" class="text-xs text-rose-500">
                {{ task.error_message }}
              </div>
            </div>
          </div>
        </div>

        <InfiniteListSentinel :loading="tasksLoading || taskList.loadingMore.value || taskList.refreshing.value" :error="taskList.error.value" :has-more="taskList.hasMore.value" @load-more="taskList.loadMore" @retry="taskList.retry" />
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-danger" :disabled="submitting" @click="openConfirm">
          {{ submitting ? t('admin.usage.cleanup.submitting') : t('admin.usage.cleanup.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="confirmVisible"
    :busy="submitting"
    :title="t('admin.usage.cleanup.confirmTitle')"
    :message="t('admin.usage.cleanup.confirmMessage')"
    :confirm-text="t('admin.usage.cleanup.confirmSubmit')"
    danger
    @confirm="submitCleanup"
    @cancel="confirmVisible = false"
  />

  <ConfirmDialog
    :show="cancelConfirmVisible"
    :busy="canceling"
    :title="t('admin.usage.cleanup.cancelConfirmTitle')"
    :message="t('admin.usage.cleanup.cancelConfirmMessage')"
    :confirm-text="t('admin.usage.cleanup.cancelConfirm')"
    danger
    @confirm="cancelTask"
    @cancel="cancelConfirmVisible = false"
  />
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { ref, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useConfirmedAction } from '@/composables/useConfirmation'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import { adminUsageAPI } from '@/api/admin/usage'
import type { AdminUsageQueryParams, UsageCleanupTask, CreateUsageCleanupTaskRequest } from '@/api/admin/usage'
import { requestTypeToLegacyStream } from '@/utils/usageRequestType'

interface Props {
  show: boolean
  filters: AdminUsageQueryParams
  startDate: string
  endDate: string
}

const props = defineProps<Props>()
const emit = defineEmits(['close'])

const { t } = useI18n()
const appStore = useAppStore()

const localFilters = ref<AdminUsageQueryParams>({})
const localStartDate = ref('')
const localEndDate = ref('')

const taskList = useInfiniteList<UsageCleanupTask>({
  keyOf: task => task.id, pageSize: 20,
  fetchPage: page => adminUsageAPI.listCleanupTasks({ page, page_size: 20 }),
})
const { items: tasks, loading: tasksLoading } = taskList
watch(taskList.error, error => {
  if (error) appStore.showError(t('admin.usage.cleanup.loadFailed'))
})
const submitAction = useConfirmedAction(() => ({ active: props.show, identity: [localFilters.value, localStartDate.value, localEndDate.value] }))
const cancelAction = useConfirmedAction(() => ({ active: props.show }))
const submitting = submitAction.busy
let submitTarget: CreateUsageCleanupTaskRequest | null = null
const confirmVisible = ref(false)
const cancelConfirmVisible = ref(false)
const canceling = cancelAction.busy
const cancelTarget = ref<UsageCleanupTask | null>(null)
let pollTimer: number | null = null

const noop = () => {}

const resetFilters = () => {
  localFilters.value = { ...props.filters }
  localStartDate.value = props.startDate
  localEndDate.value = props.endDate
  localFilters.value.start_date = localStartDate.value
  localFilters.value.end_date = localEndDate.value
}

const startPolling = () => {
  stopPolling()
  pollTimer = window.setInterval(() => {
    loadTasks()
  }, 10000)
}

const stopPolling = () => {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

const handleClose = () => {
  submitAction.invalidate()
  cancelAction.invalidate()
  taskList.cancel()
  stopPolling()
  confirmVisible.value = false
  cancelConfirmVisible.value = false
  cancelTarget.value = null
  emit('close')
}

const statusLabel = (status: string) => {
  const map: Record<string, string> = {
    pending: t('admin.usage.cleanup.status.pending'),
    running: t('admin.usage.cleanup.status.running'),
    succeeded: t('admin.usage.cleanup.status.succeeded'),
    failed: t('admin.usage.cleanup.status.failed'),
    canceled: t('admin.usage.cleanup.status.canceled')
  }
  return map[status] || status
}

const statusClass = (status: string) => {
  const map: Record<string, string> = {
    pending: 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    running: 'bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-200',
    succeeded: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    failed: 'bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-200',
    canceled: 'bg-gray-200 text-gray-600 dark:bg-dark-600 dark:text-gray-300'
  }
  return map[status] || 'bg-gray-100 text-gray-600'
}

const formatDateTime = (value?: string | null) => {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const formatRange = (task: UsageCleanupTask) => {
  const start = formatDateTime(task.filters.start_time)
  const end = formatDateTime(task.filters.end_time)
  return `${start} ~ ${end}`
}

const getUserTimezone = () => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

const loadTasks = async () => {
  if (!props.show || tasksLoading.value || taskList.loadingMore.value || taskList.refreshing.value) return
  await taskList.refresh()
}

const openConfirm = () => {
  if (!props.show || submitting.value || confirmVisible.value) return
  submitTarget = buildPayload()
  confirmVisible.value = submitTarget !== null
}

const canCancel = (task: UsageCleanupTask) => {
  return task.status === 'pending' || task.status === 'running'
}

const openCancelConfirm = (task: UsageCleanupTask) => {
  if (!props.show || canceling.value || cancelConfirmVisible.value) return
  cancelTarget.value = { ...task }
  cancelConfirmVisible.value = true
}

const buildPayload = (): CreateUsageCleanupTaskRequest | null => {
  if (!localStartDate.value || !localEndDate.value) {
    appStore.showError(t('admin.usage.cleanup.missingRange'))
    return null
  }

  const payload: CreateUsageCleanupTaskRequest = {
    start_date: localStartDate.value,
    end_date: localEndDate.value,
    timezone: getUserTimezone()
  }

  if (localFilters.value.user_id && localFilters.value.user_id > 0) {
    payload.user_id = localFilters.value.user_id
  }
  if (localFilters.value.api_key_id && localFilters.value.api_key_id > 0) {
    payload.api_key_id = localFilters.value.api_key_id
  }
  if (localFilters.value.account_id && localFilters.value.account_id > 0) {
    payload.account_id = localFilters.value.account_id
  }
  if (localFilters.value.group_id && localFilters.value.group_id > 0) {
    payload.group_id = localFilters.value.group_id
  }
  if (localFilters.value.model) {
    payload.model = localFilters.value.model
  }
  if (localFilters.value.request_type) {
    payload.request_type = localFilters.value.request_type
    const legacyStream = requestTypeToLegacyStream(localFilters.value.request_type)
    if (legacyStream !== null && legacyStream !== undefined) {
      payload.stream = legacyStream
    }
  } else if (localFilters.value.stream !== null && localFilters.value.stream !== undefined) {
    payload.stream = localFilters.value.stream
  }
  if (localFilters.value.billing_type !== null && localFilters.value.billing_type !== undefined) {
    payload.billing_type = localFilters.value.billing_type
  }

  return payload
}

const submitCleanup = async () => {
  const payload = submitTarget
  if (!props.show || !confirmVisible.value || !payload) return
  await submitAction.run(async (isCurrent) => {
    try {
      await adminUsageAPI.createCleanupTask(payload)
      if (!isCurrent()) return
      confirmVisible.value = false
      submitTarget = null
      appStore.showSuccess(t('admin.usage.cleanup.submitSuccess'))
      loadTasks()
    } catch (error) {
      if (!isCurrent()) return
      console.error('Failed to create cleanup task:', error)
      appStore.showError(t('admin.usage.cleanup.submitFailed'))
    }
  })
}

const cancelTask = async () => {
  const task = cancelTarget.value
  if (!props.show || !cancelConfirmVisible.value || !task) return
  await cancelAction.run(async (isCurrent) => {
    try {
      await adminUsageAPI.cancelCleanupTask(task.id)
      if (!isCurrent()) return
      cancelConfirmVisible.value = false
      cancelTarget.value = null
      appStore.showSuccess(t('admin.usage.cleanup.cancelSuccess'))
      loadTasks()
    } catch (error) {
      if (!isCurrent()) return
      console.error('Failed to cancel cleanup task:', error)
      appStore.showError(t('admin.usage.cleanup.cancelFailed'))
    }
  })
}

watch(() => [props.show, localFilters.value, localStartDate.value, localEndDate.value], () => {
  confirmVisible.value = false
  submitTarget = null
}, { deep: true, flush: 'sync' })
watch(() => props.show, () => {
  cancelConfirmVisible.value = false
  cancelTarget.value = null
}, { flush: 'sync' })

watch(
  () => props.show,
  (show) => {
    if (show) {
      resetFilters()
      void taskList.reset()
      startPolling()
    } else {
      taskList.cancel()
      stopPolling()
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  taskList.cancel()
  stopPolling()
})
</script>
