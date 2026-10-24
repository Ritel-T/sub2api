<template>
  <AppLayout>
    <TablePageLayout open>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex-1 sm:max-w-72">
            <input
              v-model="keyword"
              type="search"
              class="input"
              :placeholder="t('supportTickets.filters.keywordPlaceholder')"
              :aria-label="t('supportTickets.filters.keywordPlaceholder')"
              data-testid="ticket-keyword"
              @input="onKeyword"
            />
          </div>
          <Select :aria-label="t('common.status')" v-model="status" :options="statusOptions" class="w-36" data-testid="ticket-status-filter" />
          <Select :aria-label="t('supportTickets.form.category')"
            v-if="categories.length"
            v-model="category"
            :options="categoryOptions"
            class="w-44"
            data-testid="ticket-category-filter"
          />
          <div class="flex flex-1 justify-end">
            <button type="button" class="btn btn-secondary" :disabled="loading" :title="t('supportTickets.refresh')" :aria-label="t('supportTickets.refresh')" @click="load">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable column-order-key="table-columns:admin-support-tickets:v1" :columns="columns" :data="tickets" :loading="loading" row-key="id" clickable-rows @row-click="openTicket">
          <template #cell-title="{ row }">
            <div class="min-w-0 max-w-md">
              <div class="flex items-center gap-2">
                <span
                  v-if="row.admin_unread"
                  class="h-2 w-2 shrink-0 rounded-full bg-red-500"
                  :title="t('supportTickets.newFromUser')"
                  data-testid="ticket-unread"
                ></span>
                <router-link
                  :to="`/admin/support-tickets/${row.id}`"
                  class="truncate font-medium text-gray-900 hover:text-primary-600 dark:text-white dark:hover:text-primary-400"
                  @click.stop
                >{{ row.title }}</router-link>
              </div>
              <div class="mt-1 flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400">
                <span>#{{ row.id }}</span>
                <span>{{ row.category || t('supportTickets.noCategory') }}</span>
              </div>
            </div>
          </template>
          <template #cell-user="{ row }">
            <span class="text-sm" :class="row.user?.deleted ? 'text-gray-400 line-through' : 'text-gray-700 dark:text-gray-300'">
              {{ row.user?.email || `#${row.user_id}` }}
            </span>
          </template>
          <template #cell-status="{ row }">
            <TicketStatusBadge :status="row.status" />
          </template>
          <template #cell-last_message_at="{ row }">
            <div class="text-sm text-gray-700 dark:text-gray-300">{{ formatRelativeTime(row.last_message_at) }}</div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ row.last_message_role === 'user' ? t('supportTickets.admin.lastFromUser') : t('supportTickets.admin.lastFromAdmin') }}
            </div>
          </template>
          <template #empty>
            <EmptyState :title="t('supportTickets.adminEmpty')" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <InfiniteListSentinel :loading="list.loading.value || list.loadingMore.value" :error="list.error.value" :has-more="list.hasMore.value" @load-more="list.loadMore" @retry="list.retry" />
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import TicketStatusBadge from '@/features/support-tickets/TicketStatusBadge.vue'
import { supportTicketErrorMessage } from '@/features/support-tickets/supportTickets'
import { getAdminSummary, listTickets, type SupportTicket, type SupportTicketStatusFilter } from '@/api/supportTickets'
import { useAppStore } from '@/stores/app'
import { useSupportTicketStore } from '@/stores/supportTickets'
import { formatRelativeTime } from '@/utils/format'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const ticketStore = useSupportTicketStore()

let appliedFilters = { status: 'open' as SupportTicketStatusFilter, category: undefined as string | undefined, keyword: undefined as string | undefined }
const list = useInfiniteList<SupportTicket>({
  keyOf: row => row.id,
  pageSize: 50,
  fetchPage: async (page, signal) => {
    const [result, summary] = await Promise.all([
      listTickets({ ...appliedFilters, page, page_size: 50 }),
      getAdminSummary(),
    ])
    if (!signal.aborted) {
      categories.value = summary.categories
      ticketStore.setAdminPending(summary.pending_count)
    }
    return result
  },
})
const { items: tickets, loading } = list
const status = ref<SupportTicketStatusFilter>('open')
const category = ref('')
const keyword = ref('')
const categories = ref<string[]>([])
let keywordTimer: ReturnType<typeof setTimeout> | undefined

const columns = computed<Column[]>(() => [
  { key: 'title', label: t('supportTickets.columns.ticket') },
  { key: 'user', label: t('supportTickets.columns.user') },
  { key: 'status', label: t('supportTickets.columns.status') },
  { key: 'message_count', label: t('supportTickets.columns.messages') },
  { key: 'last_message_at', label: t('supportTickets.columns.updated') },
])

const statusOptions = computed(() => [
  { value: 'open', label: t('supportTickets.filters.open') },
  { value: 'pending', label: t('supportTickets.status.pending') },
  { value: 'processing', label: t('supportTickets.status.processing') },
  { value: 'replied', label: t('supportTickets.status.replied') },
  { value: 'closed', label: t('supportTickets.status.closed') },
  { value: '', label: t('supportTickets.filters.all') },
])

const categoryOptions = computed(() => [
  { value: '', label: t('supportTickets.filters.allCategories') },
  ...categories.value.map((name) => ({ value: name, label: name })),
])

async function load() {
  appliedFilters = { status: status.value, category: category.value || undefined, keyword: keyword.value.trim() || undefined }
  await list.reset()
  if (list.error.value) appStore.showError(supportTicketErrorMessage(list.error.value, t, 'supportTickets.loadFailed'))
}

function reload() { void load() }

function onKeyword() {
  if (keywordTimer) clearTimeout(keywordTimer)
  keywordTimer = setTimeout(reload, 300)
}

function openTicket(row: SupportTicket) {
  router.push(`/admin/support-tickets/${row.id}`)
}

watch([status, category], reload)
onMounted(load)
onBeforeUnmount(() => {
  if (keywordTimer) clearTimeout(keywordTimer)
})
</script>
