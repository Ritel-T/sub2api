<template>
  <section class="open-section space-y-4" data-testid="security-records">
    <div class="flex items-center justify-between gap-3">
      <AppHeading as="h2" variant="section" >{{ t('securityRecords.title') }}</AppHeading>
      <button class="btn btn-secondary btn-sm" :disabled="loading || refreshing || summaryLoading" @click="refresh">{{ t('common.refresh') }}</button>
    </div>
    <p v-if="total !== undefined" class="text-sm text-gray-500">{{ t('common.total') }}: {{ total }}</p>
    <p v-if="error || summaryError || detailError" role="alert" class="text-red-600">{{ t('securityRecords.loadFailed') }}</p>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <template v-if="!admin && summary">
      <p :class="summary.restricted ? 'font-medium text-red-600' : 'text-emerald-600'" data-testid="restriction-status">
        {{ summary.restricted ? t('securityRecords.restricted') : t('securityRecords.unrestricted') }}
      </p>
      <p class="text-sm text-gray-500">{{ t('securityRecords.loginUnaffected') }}</p>
      <p v-if="summary.observe_only && summary.enabled" class="text-sm">{{ t('securityRecords.observation') }}</p>
      <article v-for="state in summary.states" :key="state.scope + state.subject_key" class="border-b py-3 dark:border-dark-600">
        <p>{{ t(`securityRecords.scopes.${state.scope}`) }} · {{ t(`securityRecords.actions.${state.action}`) }} · {{ state.count }} / {{ summary.threshold }}</p>
        <p>{{ t('securityRecords.reason') }}: {{ state.reason || '—' }}</p>
        <p v-if="state.blocked_until || state.route_until">{{ t('securityRecords.until') }}: {{ date(state.blocked_until || state.route_until) }}</p>
        <p v-if="state.scope === 'session'" class="text-xs text-gray-500">{{ t('securityRecords.session') }}: {{ state.subject_key.slice(0, 12) }}</p>
      </article>
    </template>
    <p class="text-sm text-gray-500">{{ t('securityRecords.originalHint') }}</p>
    <p v-if="!loading && !error && !events.length">{{ t('securityRecords.empty') }}</p>
    <div ref="recordsBody">
    <div v-if="paddingTop" aria-hidden="true" :style="{ height: `${paddingTop}px` }" />
    <template v-for="{ row: event, index, gapBefore } in rendered" :key="event.id">
    <div v-if="gapBefore" aria-hidden="true" :style="{ height: `${gapBefore}px` }" />
    <article :ref="measure" :data-index="index" class="space-y-2 border-b py-3 dark:border-dark-600">
      <p class="text-sm text-gray-500">#{{ event.id }} · {{ date(event.created_at) }} · {{ event.model }}<span v-if="admin"> · UID {{ event.user_id }}</span></p>
      <p>{{ t('securityRecords.reason') }}: {{ event.reason }}</p>
      <p class="text-sm">{{ t(`securityRecords.scopes.${event.scope}`) }} · {{ t('securityRecords.count') }}: {{ event.count }}</p>
      <button class="btn btn-secondary btn-sm" :disabled="opening === event.id" @click="open(event.id)">{{ expanded === event.id ? t('securityRecords.hidePrompt') : t('securityRecords.showPrompt') }}</button>
      <pre v-if="expanded === event.id && details[event.id]" class="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900" data-testid="security-original-prompt">{{ details[event.id]?.full_prompt }}</pre>
    </article>
    </template>
    <div v-if="paddingBottom" aria-hidden="true" :style="{ height: `${paddingBottom}px` }" />
    </div>
    <InfiniteListSentinel :loading="loading || loadingMore || refreshing" :has-more="hasMore" :error="error" :progress="events.length" @load-more="loadMore" @retry="retry" />
  </section>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useDocumentRows } from '@/composables/useDocumentRows'
import { getEvent, getSummary, listEvents, type PenaltyEvent, type PenaltySummary } from './api'
const props = withDefaults(defineProps<{ admin?: boolean; userId?: number }>(), { admin: false })
const { t } = useI18n()
const summary = ref<PenaltySummary | null>(null)
const summaryError = ref(false)
const detailError = ref(false)
const summaryLoading = ref(false)
const { items: events, total, loading, loadingMore, refreshing, error, hasMore, reset, loadMore, refresh: refreshEvents, retry } = useInfiniteList<PenaltyEvent>({
  pageSize: 10,
  keyOf: event => event.id,
  // This API has no signal parameter; useInfiniteList discards obsolete generations.
  fetchPage: page => listEvents(page, props.admin, props.userId)
})
const recordsBody = ref<HTMLElement | null>(null)
const { rendered, paddingTop, paddingBottom, measure } = useDocumentRows(events, recordsBody, event => event.id, ref(true), () => 180)
const expanded = ref<number | null>(null)
const opening = ref<number | null>(null)
const details = ref<Record<number, PenaltyEvent>>({})
const date = (value: string | null) => value ? new Date(value).toLocaleString() : '—'
let generation = 0
let detailGeneration = 0
function invalidateDetails() {
  detailGeneration++
  details.value = {}
  expanded.value = null
  opening.value = null
  detailError.value = false
}
async function loadSummary(current: number) {
  summary.value = null
  summaryError.value = false
  summaryLoading.value = !props.admin
  if (props.admin) return
  try {
    const result = await getSummary()
    if (current === generation) summary.value = result
  } catch {
    if (current === generation) summaryError.value = true
  } finally {
    if (current === generation) summaryLoading.value = false
  }
}
async function refresh() {
  const current = ++generation
  invalidateDetails()
  await Promise.all([refreshEvents(), loadSummary(current)])
}
function resetScope() {
  const current = ++generation
  invalidateDetails()
  void reset()
  void loadSummary(current)
}
async function open(id: number) {
  const current = ++detailGeneration
  if (expanded.value === id) { expanded.value = null; opening.value = null; return }
  opening.value = id
  detailError.value = false
  try {
    const result = details.value[id] ?? await getEvent(id, props.admin)
    if (current !== detailGeneration || !events.value.some(event => event.id === id)) return
    details.value[id] = result
    expanded.value = id
  } catch { if (current === detailGeneration) detailError.value = true }
  finally { if (current === detailGeneration) opening.value = null }
}
watch(() => [props.admin, props.userId], resetScope, { immediate: true, flush: 'sync' })
onScopeDispose(() => { generation++; detailGeneration++ })
defineExpose({ refresh })
</script>
