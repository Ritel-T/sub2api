<template>
  <div class="min-w-0">
    <div class="open-section">
      <IpGeoBatchToolbar :ips="rows.map((r) => r.client_ip)" @failed="emit('ipGeoBatchFailed')" />

      <DataTable :primary-columns="['created_at', 'key_name', 'model', 'status', 'message']"
        :toolbar-target="toolbarTarget"
        :columns="columns"
        @column-reorder="emit('column-reorder', $event)"
        :data="rows"
        :loading="loading"
        clickable-rows
        server-side-sort
        :default-sort-key="sortBy || 'created_at'"
        :default-sort-order="sortOrder || 'desc'"
        @sort="onSort"
        @rowClick="(row) => openDetail(row.id)"
      >
        <template #cell-model="{ row }">
          <span v-if="row.model" class="text-sm font-medium text-gray-900 dark:text-white">{{ row.model }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-key_name="{ row }">
          <div class="text-sm">
            <span class="text-gray-900 dark:text-white">{{ row.key_name || '-' }}</span>
            <span
              v-if="row.key_deleted"
              class="ml-1 inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30"
            >{{ t('usage.errors.keyDeleted') }}</span>
          </div>
        </template>

        <template #cell-endpoint="{ row }">
          <div class="max-w-[320px] text-xs">
            <div class="break-all text-gray-700 dark:text-gray-300">
              <span class="font-medium text-gray-500 dark:text-gray-400">{{ t('usage.inbound') }}:</span>
              <span class="ml-1">{{ row.inbound_endpoint?.trim() || '-' }}</span>
            </div>
          </div>
        </template>

        <template #cell-status="{ row }">
          <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="statusClass(row.status_code)">
            {{ row.status_code || '-' }}
          </span>
        </template>

        <template #cell-category="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ t('usage.errors.categories.' + row.category) }}</span>
        </template>

        <template #cell-message="{ row }">
          <span
            v-if="row.message"
            class="block max-w-[280px] truncate text-sm text-gray-600 dark:text-gray-400"
            :title="row.message"
          >{{ row.message }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-group="{ row }">
          <span
            v-if="row.group_name"
            class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-200"
          >{{ row.group_name }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-type="{ row }">
          <span
            v-if="requestTypeBadge(row)"
            class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium"
            :class="requestTypeBadge(row)!.className"
          >{{ requestTypeBadge(row)!.label }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-platform="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ row.platform || '-' }}</span>
        </template>

        <template #cell-client_ip="{ row }">
          <div @click.stop>
            <div v-if="row.client_ip">
              <span class="text-sm font-mono text-gray-600 dark:text-gray-400">{{ row.client_ip }}</span>
              <IpGeoCell :ip="row.client_ip" />
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
          </div>
        </template>

        <template #cell-created_at="{ row }">
          <span class="text-sm text-gray-600 dark:text-gray-400">{{ formatDateTime(row.created_at) }}</span>
        </template>

        <template #cell-user_agent="{ row }">
          <span
            v-if="row.user_agent"
            class="block max-w-[320px] truncate text-sm text-gray-600 dark:text-gray-400"
            :title="row.user_agent"
          >{{ row.user_agent }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #empty><EmptyState :message="t('usage.errors.empty')" /></template>
      </DataTable>
    </div>

    <InfiniteListSentinel
      :loading="loading || loadingMore"
      :error="error"
      :has-more="hasMore ?? false"
      @load-more="emit('load-more')"
      @retry="emit('retry')"
    />

    <UserErrorDetailModal v-model:show="showDetail" :error-id="selectedId" :support-enabled="supportEnabled" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import UserErrorDetailModal from '@/components/user/UserErrorDetailModal.vue'
import IpGeoCell from '@/components/common/IpGeoCell.vue'
import IpGeoBatchToolbar from '@/components/common/IpGeoBatchToolbar.vue'
import { formatDateTime } from '@/utils/format'
import {
  mapErrorSortKey,
  numericRequestTypeKind,
  requestTypeBadgeClass,
  requestTypeLabelKey,
  statusCodeBadgeClass,
} from '@/utils/errorBadges'
import type { UserErrorRequest } from '@/types'
import type { Column } from '@/components/common/types'

const props = defineProps<{
  toolbarTarget?: string | HTMLElement | null
  rows: UserErrorRequest[]
  loading: boolean
  loadingMore?: boolean
  hasMore?: boolean
  error?: unknown
  /** Ordered fields and their main-row placement from the page's column settings. */
  visibleColumnKeys?: string[]
  primaryColumnKeys?: string[]
  supportEnabled?: boolean
  sortBy?: string
  sortOrder?: 'asc' | 'desc'
}>()

const emit = defineEmits<{
  (e: 'load-more'): void
  (e: 'retry'): void
  (e: 'column-reorder', keys: string[]): void
  (e: 'ipGeoBatchFailed'): void
  (e: 'sort', sortBy: string, sortOrder: 'asc' | 'desc'): void
}>()

function onSort(key: string, order: 'asc' | 'desc') {
  emit('sort', mapErrorSortKey(key), order)
}

const { t } = useI18n()

const allColumns = computed<Column[]>(() => [
  { key: 'created_at', label: t('usage.errors.time'), sortable: true },
  { key: 'key_name', label: t('usage.errors.keyName') },
  { key: 'model', label: t('usage.errors.model'), sortable: true },
  { key: 'status', label: t('usage.errors.status'), sortable: true },
  { key: 'message', label: t('usage.errors.message') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'group', label: t('admin.usage.group') },
  { key: 'type', label: t('usage.type') },
  { key: 'platform', label: t('usage.errors.platform') },
  { key: 'endpoint', label: t('usage.errors.endpoint') },
  { key: 'client_ip', label: 'IP' },
  { key: 'user_agent', label: t('usage.userAgent') },
])

const columns = computed<Column[]>(() => {
  const ordered = props.visibleColumnKeys
    ? props.visibleColumnKeys.flatMap(key => allColumns.value.find(column => column.key === key) ?? [])
    : allColumns.value
  return ordered.map(column => ({
    ...column,
    primary: props.primaryColumnKeys?.includes(column.key)
  }))
})

function requestTypeBadge(row: UserErrorRequest): { label: string; className: string } | null {
  const kind = numericRequestTypeKind(row.request_type, row.stream)
  if (!kind) return null
  return { label: t(requestTypeLabelKey(kind)), className: requestTypeBadgeClass(kind) }
}

const showDetail = ref(false)
const selectedId = ref<number | null>(null)

function openDetail(id: number) {
  selectedId.value = id
  showDetail.value = true
}

const statusClass = statusCodeBadgeClass
</script>
