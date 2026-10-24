<template>
  <AppLayout :show-page-title="false">
    <div class="open-page support-workspace">
      <AppHeading variant="page" as="h1">{{ t('nav.supportTickets') }}</AppHeading>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="inline-flex flex-wrap gap-5 border-b border-gray-200 dark:border-dark-700" role="group" :aria-label="t('supportTickets.columns.status')">
          <button
            v-for="option in statusOptions"
            :key="option.value"
            type="button"
            class="border-b-2 px-1 py-3 text-sm font-medium transition-colors"
            :class="status === option.value
              ? 'border-current text-gray-900 dark:text-white'
              : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
            :aria-pressed="status === option.value"
            data-testid="ticket-filter"
            @click="setStatus(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading" :title="t('supportTickets.refresh')" :aria-label="t('supportTickets.refresh')" @click="load">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button type="button" class="btn btn-primary" data-testid="ticket-new" @click="openCreate">
            <Icon name="plus" size="md" class="mr-1" />
            {{ t('supportTickets.newTicket') }}
          </button>
        </div>
      </div>
      <p v-if="summary" class="text-xs text-gray-500 dark:text-gray-400" data-testid="ticket-open-count">
        {{ t('supportTickets.form.openCount', { count: summary.open_count, max: summary.max_open }) }}
      </p>

      <LoadingRegion v-if="loading && !tickets.length" class="open-record-list">
        <div v-for="index in 3" :key="index" class="open-section h-20 animate-pulse"></div>
      </LoadingRegion>
      <p v-else-if="loadError && !tickets.length" class="open-section p-6 text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
      <div v-else-if="!tickets.length" class="open-section" data-testid="ticket-empty">
        <EmptyState :title="t('supportTickets.empty')" :description="t('supportTickets.emptyHint')" />
      </div>
      <DocumentRows v-else as="ul" :items="tickets" item-key="id" :estimate-height="110" class="open-record-list">
        <template #default="{ item: ticket }">
          <router-link
            :to="`/support-tickets/${ticket.id}`"
            class="open-section block py-6 transition hover:bg-gray-500/5"
            data-testid="ticket-row"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <span
                    v-if="ticket.user_unread"
                    class="h-2 w-2 shrink-0 rounded-full bg-red-500"
                    :title="t('supportTickets.unread')"
                    data-testid="ticket-unread"
                  ></span>
                  <span class="truncate font-medium text-gray-900 dark:text-white">{{ ticket.title }}</span>
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                  <span>#{{ ticket.id }}</span>
                  <span v-if="ticket.category" class="rounded-md bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700">{{ ticket.category }}</span>
                  <span>{{ t('supportTickets.messageCount', { count: ticket.message_count }) }}</span>
                  <span>{{ formatRelativeTime(ticket.last_message_at) }}</span>
                  <span v-if="ticket.user_unread" class="font-medium text-red-600 dark:text-red-400">{{ t('supportTickets.unread') }}</span>
                </div>
              </div>
              <TicketStatusBadge :status="ticket.status" />
            </div>
          </router-link>
        </template>
      </DocumentRows>

      <InfiniteListSentinel
        :loading="loading || loadingMore || refreshing"
        :error="listError"
        :has-more="hasMore"
        @load-more="loadMore"
        @retry="retry"
      />
    </div>

    <CreateTicketDialog :show="createOpen" :summary="summary" :initial-draft="draft" @close="createOpen = false" @created="onCreated" />
  </AppLayout>
</template>

<script setup lang="ts">
import DocumentRows from '@/components/common/DocumentRows.vue'
import LoadingRegion from '@/components/common/LoadingRegion.vue'
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import Icon from '@/components/icons/Icon.vue'
import CreateTicketDialog from '@/features/support-tickets/CreateTicketDialog.vue'
import TicketStatusBadge from '@/features/support-tickets/TicketStatusBadge.vue'
import { supportTicketErrorMessage } from '@/features/support-tickets/supportTickets'
import {
  getMySummary,
  listMyTickets,
  type SupportTicket,
  type SupportTicketDetail,
  type SupportTicketStatusFilter,
  type SupportTicketUserSummary,
} from '@/api/supportTickets'
import { useSupportTicketStore } from '@/stores/supportTickets'
import { formatRelativeTime } from '@/utils/format'
import { supportTicketDraft } from '@/features/support-tickets/ticketDraft'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const draft = computed(() => supportTicketDraft(route.query || {}))
const ticketStore = useSupportTicketStore()

const status = ref<SupportTicketStatusFilter>('')
const summary = ref<SupportTicketUserSummary | null>(null)
const createOpen = ref(false)
function openCreate() { createOpen.value = true }
watch(() => route.query, () => { if (draft.value) createOpen.value = true }, { immediate: true })

const statusOptions = computed<Array<{ value: SupportTicketStatusFilter; label: string }>>(() => [
  { value: '', label: t('supportTickets.filters.all') },
  { value: 'open', label: t('supportTickets.filters.open') },
  { value: 'closed', label: t('supportTickets.status.closed') },
])

const { items: tickets, loading, loadingMore, refreshing, error: listError, hasMore, loadMore, retry, reset, refresh } = useInfiniteList<SupportTicket>({
  pageSize: 50,
  keyOf: ticket => ticket.id,
  fetchPage: async (page, signal) => {
    const [list, mine] = await Promise.all([
      listMyTickets({ status: status.value, page, page_size: 50 }),
      getMySummary(),
    ])
    if (!signal.aborted) {
      summary.value = mine
      ticketStore.setUserUnread(mine.unread_count)
    }
    return list
  },
})
const loadError = computed(() => listError.value ? supportTicketErrorMessage(listError.value, t, 'supportTickets.loadFailed') : '')
const load = () => refresh()

function setStatus(value: SupportTicketStatusFilter) {
  if (status.value === value) return
  status.value = value
  void reset()
}

function onCreated(detail: SupportTicketDetail) {
  createOpen.value = false
  router.push(`/support-tickets/${detail.ticket.id}`)
}

onMounted(load)
</script>
