<template>
  <BaseDialog
    :show="show"
    :title="t('admin.announcements.readStatus')"
    width="extra-wide"
    @close="handleClose"
  >
    <div class="space-y-4">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex-1">
          <input
            v-model="search"
            type="text"
            class="input"
            :placeholder="t('admin.announcements.searchUsers')"
            @input="handleSearch"
          />
        </div>
        <button @click="load" :disabled="loading" class="btn btn-secondary" :title="t('common.refresh')">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>

      <DataTable :key="announcementId ?? 0" column-order-key="table-columns:announcement-read-status:v1"
        :columns="columns"
        :data="items"
        row-key="user_id"
        :loading="loading"
        :server-side-sort="true"
        default-sort-key="email"
        default-sort-order="asc"
        @sort="handleSort"
      >
        <template #cell-email="{ value }">
          <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
        </template>

        <template #cell-balance="{ value }">
          <span class="font-medium text-gray-900 dark:text-white">${{ Number(value ?? 0).toFixed(2) }}</span>
        </template>

        <template #cell-eligible="{ value }">
          <span :class="['badge', value ? 'badge-success' : 'badge-gray']">
            {{ value ? t('admin.announcements.eligible') : t('common.no') }}
          </span>
        </template>

        <template #cell-read_at="{ value }">
          <span class="text-sm text-gray-500 dark:text-dark-400">
            {{ value ? formatDateTime(value) : t('admin.announcements.unread') }}
          </span>
        </template>
      </DataTable>

      <InfiniteListSentinel :loading="loading || list.loadingMore.value || list.refreshing.value" :error="list.error.value" :has-more="list.hasMore.value" @load-more="list.loadMore" @retry="list.retry" />
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="handleClose">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AnnouncementUserReadStatus } from '@/types'
import type { Column } from '@/components/common/types'
import { useInfiniteList } from '@/composables/useInfiniteList'

import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const props = defineProps<{
  show: boolean
  announcementId: number | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const search = ref('')


const sortState = reactive({
  sort_by: 'email',
  sort_order: 'asc' as 'asc' | 'desc'
})

let appliedQuery = { announcementId: 0, search: '', sort_by: 'email', sort_order: 'asc' as 'asc' | 'desc' }
const list = useInfiniteList<AnnouncementUserReadStatus>({
  keyOf: row => row.user_id, pageSize: 50,
  fetchPage: (page, signal) => adminAPI.announcements.getReadStatus(appliedQuery.announcementId, page, 50, {
    search: appliedQuery.search, sort_by: appliedQuery.sort_by, sort_order: appliedQuery.sort_order,
  }, { signal }),
})
const { items, loading } = list
watch(list.error, error => {
  if (error) appStore.showError((error as { response?: { data?: { detail?: string } } }).response?.data?.detail || t('admin.announcements.failedToLoadReadStatus'))
})

const columns = computed<Column[]>(() => [
  { key: 'email', label: t('common.email'), sortable: true },
  { key: 'username', label: t('admin.users.columns.username'), sortable: true },
  { key: 'balance', label: t('common.balance'), sortable: true },
  { key: 'eligible', label: t('admin.announcements.eligible') },
  { key: 'read_at', label: t('admin.announcements.readAt') }
])

let searchDebounceTimer: number | null = null

function resetDialogState() {
  search.value = ''
  list.clear()
  sortState.sort_by = 'email'
  sortState.sort_order = 'asc'
}

function cancelPendingLoad(resetState = false) {
  if (searchDebounceTimer) {
    window.clearTimeout(searchDebounceTimer)
    searchDebounceTimer = null
  }
  list.cancel()
  if (resetState) {
    resetDialogState()
  }
}

function applyQuery() {
  if (!props.show || !props.announcementId) return
  appliedQuery = { announcementId: props.announcementId, search: search.value, ...sortState }
  return list.reset()
}

async function load() {
  if (!props.show || !props.announcementId) return
  await list.refresh()
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  void applyQuery()
}

function handleSearch() {
  if (searchDebounceTimer) window.clearTimeout(searchDebounceTimer)
  searchDebounceTimer = window.setTimeout(() => {
    void applyQuery()
  }, 300)
}

function handleClose() {
  cancelPendingLoad(true)
  emit('close')
}

watch(
  () => [props.show, props.announcementId] as const,
  ([show, announcementId]) => {
    cancelPendingLoad(true)
    if (show && announcementId) void applyQuery()
  },
  { immediate: true }
)

onUnmounted(() => {
  cancelPendingLoad()
})
</script>
