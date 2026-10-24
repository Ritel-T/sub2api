<template>
  <AppLayout>
    <div class="open-page mx-auto max-w-6xl space-y-4">
      <router-link
        to="/admin/support-tickets"
        class="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
      >
        <Icon name="arrowLeft" size="sm" />
        {{ t('supportTickets.detail.back') }}
      </router-link>

      <LoadingRegion v-if="loading && !detail" class="bg-gray-200 dark:bg-dark-700 h-64 animate-pulse"></LoadingRegion>
      <p v-else-if="loadError" class="card p-6 text-sm text-red-600 dark:text-red-400" data-testid="ticket-load-error">{{ loadError }}</p>
      <div v-else-if="detail" class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div class="min-w-0 space-y-4">
          <div class="open-section p-5">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0">
                <AppHeading variant="section" as="h2" class="break-words text-gray-900 dark:text-white" data-testid="ticket-title">{{ detail.ticket.title }}</AppHeading>
                <p class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t('supportTickets.detail.number', { id: detail.ticket.id }) }}</span>
                  <span class="rounded-md bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700">{{ detail.ticket.category || t('supportTickets.noCategory') }}</span>
                  <span>{{ t('supportTickets.detail.createdAt', { time: formatDateTimeToMinute(detail.ticket.created_at) }) }}</span>
                </p>
              </div>
              <TicketStatusBadge :status="detail.ticket.status" />
            </div>
          </div>

          <div class="open-section p-5">
            <TicketConversation :messages="detail.messages" viewer="admin" />
          </div>

          <div v-if="closed" class="open-section flex flex-wrap items-center justify-between gap-3 p-5 text-sm text-gray-600 dark:text-gray-300" data-testid="ticket-closed">
            <span>{{ detail.ticket.closed_by_role === 'user' ? t('supportTickets.admin.closedByUser') : t('supportTickets.admin.closedByAdmin') }}</span>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" data-testid="ticket-reopen" @click="setStatus('pending')">
              {{ t('supportTickets.admin.reopen') }}
            </button>
          </div>
          <div v-else class="open-section p-5">
            <TicketReplyBox v-model="reply" :placeholder="t('supportTickets.detail.adminReplyPlaceholder')" :sending="busy" @submit="send">
              <template #extra>
                <label class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <span class="whitespace-nowrap">{{ t('supportTickets.admin.replyStatus') }}</span>
                  <Select :aria-label="t('supportTickets.admin.replyStatus')" v-model="replyStatus" :options="replyStatusOptions" class="w-52" data-testid="ticket-reply-status" />
                </label>
              </template>
            </TicketReplyBox>
          </div>
          <p class="text-center text-xs text-gray-400">{{ t('supportTickets.detail.autoRefresh') }}</p>
        </div>

        <aside class="space-y-4">
          <div class="open-section p-5" data-testid="ticket-user">
            <AppHeading variant="subsection" as="h3" class="text-gray-900 dark:text-white">{{ t('supportTickets.admin.userInfo') }}</AppHeading>
            <p v-if="user?.deleted" class="mt-2 rounded-lg bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
              {{ t('supportTickets.admin.deletedUser') }}
            </p>
            <dl class="mt-3 space-y-2 text-sm">
              <div class="flex justify-between gap-3">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('supportTickets.admin.userId') }}</dt>
                <dd class="text-gray-900 dark:text-gray-100">{{ detail.ticket.user_id }}</dd>
              </div>
              <div class="flex justify-between gap-3">
                <dt class="shrink-0 text-gray-500 dark:text-gray-400">{{ t('supportTickets.admin.email') }}</dt>
                <dd class="flex min-w-0 items-center gap-1">
                  <span class="truncate text-gray-900 dark:text-gray-100" :title="user?.email">{{ user?.email || '—' }}</span>
                  <button
                    v-if="user?.email"
                    type="button"
                    class="shrink-0 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                    :title="t('supportTickets.admin.copyEmail')"
                    :aria-label="t('supportTickets.admin.copyEmail')"
                    @click="copyEmail"
                  >
                    <Icon name="copy" size="sm" />
                  </button>
                </dd>
              </div>
              <div class="flex justify-between gap-3">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('supportTickets.admin.username') }}</dt>
                <dd class="truncate text-gray-900 dark:text-gray-100">{{ user?.username || '—' }}</dd>
              </div>
              <div class="flex justify-between gap-3">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('supportTickets.admin.balance') }}</dt>
                <dd class="text-gray-900 dark:text-gray-100">${{ (user?.balance ?? 0).toFixed(2) }}</dd>
              </div>
              <div class="flex justify-between gap-3">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('supportTickets.admin.accountStatus') }}</dt>
                <dd class="text-gray-900 dark:text-gray-100">{{ accountStatus }}</dd>
              </div>
              <div class="flex justify-between gap-3">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('supportTickets.admin.registeredAt') }}</dt>
                <dd class="text-gray-900 dark:text-gray-100">{{ user?.created_at ? formatDateTimeToMinute(user.created_at) : '—' }}</dd>
              </div>
            </dl>
          </div>

          <div class="open-section space-y-2 p-5">
            <button
              v-if="!closed && detail.ticket.status !== 'processing'"
              type="button"
              class="btn btn-secondary w-full"
              :disabled="busy"
              data-testid="ticket-mark-processing"
              @click="setStatus('processing')"
            >
              {{ t('supportTickets.admin.markProcessing') }}
            </button>
            <button
              v-if="!closed"
              type="button"
              class="btn btn-secondary w-full"
              :disabled="busy"
              data-testid="ticket-close"
              @click="setStatus('closed')"
            >
              {{ t('supportTickets.admin.close') }}
            </button>
            <button type="button" class="btn btn-danger w-full" :disabled="busy" data-testid="ticket-delete" @click="confirmDelete = true">
              {{ t('supportTickets.admin.delete') }}
            </button>
          </div>
        </aside>
      </div>
    </div>

    <ConfirmDialog :busy="removeAction.busy.value || busy"
      :show="confirmDelete"
      :title="t('supportTickets.admin.deleteTitle')"
      :message="t('supportTickets.admin.deleteConfirm', { id: detail?.ticket.id ?? '', title: detail?.ticket.title ?? '' })"
      :confirm-text="t('supportTickets.admin.delete')"
      danger
      @confirm="remove"
      @cancel="confirmDelete = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import LoadingRegion from '@/components/common/LoadingRegion.vue'
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useConfirmedAction } from '@/composables/useConfirmation'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import TicketConversation from '@/features/support-tickets/TicketConversation.vue'
import TicketReplyBox from '@/features/support-tickets/TicketReplyBox.vue'
import TicketStatusBadge from '@/features/support-tickets/TicketStatusBadge.vue'
import { supportTicketErrorMessage } from '@/features/support-tickets/supportTickets'
import {
  deleteTicket,
  getTicket,
  replyTicket,
  setTicketStatus,
  type SupportTicketDetail,
  type SupportTicketReplyStatus,
} from '@/api/supportTickets'
import { useAppStore } from '@/stores/app'
import { useSupportTicketStore } from '@/stores/supportTickets'
import { formatDateTimeToMinute } from '@/utils/format'

const POLL_MS = 30_000

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const ticketStore = useSupportTicketStore()

const detail = ref<SupportTicketDetail | null>(null)
const loading = ref(false)
const loadError = ref('')
const busy = ref(false)
const reply = ref('')
const replyStatus = ref<SupportTicketReplyStatus>('replied')
const confirmDelete = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const ticketId = computed(() => Number(route.params.id))
const closed = computed(() => detail.value?.ticket.status === 'closed')
const user = computed(() => detail.value?.ticket.user)
const accountStatus = computed(() => {
  const status = user.value?.status
  if (!status) return '—'
  return status === 'active' || status === 'disabled' ? t(`supportTickets.admin.accountStatuses.${status}`) : status
})
const replyStatusOptions = computed(() => [
  { value: 'replied', label: t('supportTickets.admin.replyStatusOptions.replied') },
  { value: 'processing', label: t('supportTickets.admin.replyStatusOptions.processing') },
  { value: 'closed', label: t('supportTickets.admin.replyStatusOptions.closed') },
])

async function load(quiet = false) {
  if (!quiet) {
    loading.value = true
    loadError.value = ''
  }
  try {
    detail.value = await getTicket(ticketId.value)
    if (!quiet) ticketStore.refresh(true)
  } catch (err) {
    if (!quiet) loadError.value = supportTicketErrorMessage(err, t, 'supportTickets.loadFailed')
  } finally {
    loading.value = false
  }
}

async function run(action: () => Promise<SupportTicketDetail>, successKey: string) {
  busy.value = true
  try {
    detail.value = await action()
    appStore.showSuccess(t(successKey))
    ticketStore.refresh(true)
    return true
  } catch (err) {
    appStore.showError(supportTicketErrorMessage(err, t))
    return false
  } finally {
    busy.value = false
  }
}

async function send(body: string) {
  if (await run(() => replyTicket(ticketId.value, body, replyStatus.value), 'supportTickets.detail.sent')) {
    reply.value = ''
    replyStatus.value = 'replied'
  }
}

function setStatus(status: 'processing' | 'closed' | 'pending') {
  return run(() => setTicketStatus(ticketId.value, status), 'supportTickets.admin.statusUpdated')
}

function remove() {
  if (busy.value) return
  return removeAction.run(async (isCurrent) => {
      const id = ticketId.value
      busy.value = true
      try {
        await deleteTicket(id)
        if (!isCurrent()) return
        appStore.showSuccess(t('supportTickets.admin.deleted'))
        ticketStore.refresh(true)
        confirmDelete.value = false
        router.push('/admin/support-tickets')
      } catch (err) {
        if (isCurrent()) appStore.showError(supportTicketErrorMessage(err, t))
      } finally { busy.value = false }
  })
}

async function copyEmail() {
  const email = user.value?.email
  if (!email) return
  try {
    await navigator.clipboard.writeText(email)
    appStore.showSuccess(t('supportTickets.admin.emailCopied'))
  } catch {
    appStore.showError(t('supportTickets.actionFailed'))
  }
}

function poll() {
  if (document.visibilityState === 'visible' && !busy.value && detail.value) load(true)
}

watch(ticketId, () => {
  confirmDelete.value = false
  detail.value = null
  load()
})

onMounted(() => {
  load()
  timer = setInterval(poll, POLL_MS)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
const removeAction = useConfirmedAction(() => ({ active: confirmDelete.value, identity: ticketId.value }))
watch(ticketId, () => { confirmDelete.value = false }, { flush: 'sync' })
</script>
