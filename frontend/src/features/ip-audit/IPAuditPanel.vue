<template>
  <section class="space-y-4 py-5" data-testid="ip-audit-panel" :aria-busy="loading || releasing">
    <p class="text-sm text-gray-500">{{ t('ipAudit.scopeHint') }}</p>
    <p class="rounded bg-amber-50 p-3 text-sm text-amber-900" data-testid="host-disconnected">{{ t('ipAudit.externalHint') }}</p>
    <button type="button" class="inline-block text-sm text-primary-600 underline" data-testid="ip-open-settings" @click="$emit('configure')">{{ t('ipAudit.settings') }}</button>
    <details class="open-section py-3" data-testid="ip-source-inventory">
      <summary class="cursor-pointer">{{ t('ipAudit.inventory') }}</summary>
      <ul class="mt-3 space-y-2 text-sm"><li v-for="item in inventory" :key="item.id">
        <strong>{{ sourceName(item.id) }}</strong> · {{ label(item.scope) }} · {{ label(item.storage) }} · {{ label(item.enumeration) }} · {{ t(item.can_release ? 'ipAudit.sourceManaged' : 'ipAudit.readOnly') }}
        <a v-if="item.manage_url" :href="item.manage_url" class="ml-2 underline">{{ t('ipAudit.sourceManagement') }}</a>
      </li></ul>
    </details>
    <form class="workspace-filter-controls" @submit.prevent="apply">
      <fieldset :disabled="releasing" class="grid items-end gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <label class="text-sm">{{ t('ipAudit.source') }}<select v-model="source" class="input mt-1 w-full" data-testid="audit-source" @change="sourceChanged"><option v-for="item in inventory" :key="item.id" :value="item.id">{{ sourceName(item.id) }}</option></select></label>
      <label class="text-sm">IP / CIDR<input v-model="ip" class="input mt-1 w-full" maxlength="80" :disabled="source === 'admin_history' || source === 'security_penalty'" data-testid="audit-ip" /></label>
      <label v-if="source === 'security_penalty'" class="text-sm">{{ t('ipAudit.explicitUser') }}<input v-model="userId" class="input mt-1 w-full" type="number" min="1" data-testid="audit-user" /></label>
      <label class="text-sm">{{ t('ipAudit.state') }}<select v-model="state" class="input mt-1 w-full" data-testid="audit-state"><option value="">{{ t('ipAudit.allStates') }}</option><option v-for="s in states" :key="s" :value="s">{{ label(s) }}</option></select></label>
      <label class="text-sm">{{ t('ipAudit.order') }}<select v-model="sort" class="input mt-1 w-full" :disabled="source === 'admin_history'"><option value="expires_desc">{{ t('ipAudit.expiryDesc') }}</option><option value="expires_asc">{{ t('ipAudit.expiryAsc') }}</option></select></label>
      <label class="text-sm">{{ t('ipAudit.start') }}<input v-model="start" class="input mt-1 w-full" type="datetime-local" /></label>
      <label class="text-sm">{{ t('ipAudit.end') }}<input v-model="end" class="input mt-1 w-full" type="datetime-local" /></label>
      <div class="flex gap-2"><button type="submit" class="btn btn-secondary" :disabled="loading || releasing" data-testid="audit-filter">{{ t('common.search') }}</button><button type="button" class="btn btn-secondary" :disabled="loading || releasing" @click="refresh">{{ t('common.refresh') }}</button></div>
      <div ref="tableToolbarTarget" class="flex items-center gap-2 sm:col-span-2 lg:col-span-4" />
      </fieldset>
    </form>
    <p v-if="exactSource && !applied.ip" class="text-sm text-amber-700">{{ t('ipAudit.exactHint') }}</p>
    <p v-if="source === 'security_penalty'" class="text-sm text-gray-500">{{ t('ipAudit.penaltyHint') }}</p>
    <p v-if="source === 'admin_history'" class="text-sm text-gray-500">{{ t('ipAudit.historyHint') }}</p>
    <p v-if="selectedSource && !selectedSource.can_release" class="text-sm text-gray-500">{{ t('ipAudit.readOnlyHint') }} <a v-if="selectedSource.manage_url" :href="selectedSource.manage_url" class="underline">{{ t('ipAudit.sourceManagement') }}</a></p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="partial" class="text-sm text-amber-700">{{ t('ipAudit.partial') }}</p>
    <DataTable :columns="columns" :data="rows" :loading="loading && !rows.length" row-key="id" column-order-key="admin-ip-audit-records" :toolbar-target="tableToolbarTarget" :row-attrs="() => ({ 'data-testid': 'ip-audit-row' })">
      <template #cell-subject="{ row }"><code class="break-all">{{ row.network || (row.user_id ? `UID ${row.user_id}` : t('ipAudit.unavailable')) }}</code><p v-if="row.actor_id">{{ t('ipAudit.operator') }} #{{ row.actor_id }}</p></template>
      <template #cell-source="{ row }">{{ sourceName(row.origin_source || row.source) }}<p>{{ label(row.scope) }}</p><p v-if="row.manual">{{ t('ipAudit.manual') }}</p><p>{{ label(row.duration || 'unknown') }}</p></template>
      <template #cell-reason="{ row }">{{ label(row.reason) }}</template>
      <template #cell-state="{ row }">{{ label(row.state) }}<p>{{ label(row.effective) }}</p><p v-if="row.result">{{ label(row.result) }}</p></template>
      <template #cell-count="{ row }">{{ row.count ?? t('ipAudit.unavailable') }} / {{ row.threshold ?? t('ipAudit.unavailable') }}</template>
      <template #cell-time="{ row }">{{ date(row.started_at) }}<p>{{ date(row.expires_at) }}</p></template>
      <template #cell-actions="{ row }"><button v-if="row.can_release" type="button" class="btn btn-secondary btn-sm" :disabled="releasing || loading || listError" data-testid="release-source" @click.stop="confirmEntry = row">{{ t('ipAudit.release') }}</button><span v-else>{{ t('ipAudit.readOnly') }}</span></template>
      <template #empty><p class="p-4 text-sm text-gray-500">{{ t('ipAudit.empty') }}</p></template>
    </DataTable>
    <InfiniteListSentinel data-testid="audit-more" :loading="loading || releasing" :error="listError" :has-more="hasMore" :progress="nextCursor" @load-more="loadMore" @retry="retry" />
    <div v-if="confirmEntry" role="region" :aria-label="t('ipAudit.confirmTitle')" class="space-y-3 rounded border border-amber-300 p-4" data-testid="release-dialog">
      <p>{{ t('ipAudit.confirmTitle') }} · {{ sourceName(confirmEntry.source) }} · <code>{{ confirmEntry.network || `UID ${confirmEntry.user_id}` }}</code></p>
      <p class="text-sm">{{ t(confirmEntry.scope === 'all_user_states' ? 'ipAudit.confirmUserScope' : 'ipAudit.confirmHint') }}</p>
      <label class="block text-sm">{{ t('ipAudit.reason') }}<select v-model="reason" class="input ml-2"><option v-for="r in reasons" :key="r" :value="r">{{ label(r) }}</option></select></label>
      <button type="button" class="btn btn-primary btn-sm" :disabled="releasing" data-testid="release-confirm" @click="release">{{ t('ipAudit.confirm') }}</button>
      <button type="button" class="btn btn-secondary btn-sm ml-2" :disabled="releasing" @click="confirmEntry = null">{{ t('common.cancel') }}</button>
    </div>
    <div v-if="outcome" role="status" class="space-y-2 rounded border p-4" data-testid="release-outcome">
      <p>{{ t(outcome.readback === 'released' ? 'ipAudit.released' : outcome.readback === 'still_present' ? 'ipAudit.stillRestricted' : 'ipAudit.unverified') }}</p>
      <p>{{ t('ipAudit.notUnrestricted') }}</p>
      <p v-if="outcome.remaining.length">{{ t('ipAudit.remaining') }}: {{ outcome.remaining.map(r => `${sourceName(r.source)} (${r.network})`).join(', ') }}</p>
      <p v-if="outcome.unknown_sources.length">{{ t('ipAudit.unchecked') }}: {{ outcome.unknown_sources.map(sourceName).join(', ') }}</p>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import DataTable from '@/components/common/DataTable.vue'
import { listIPAudit, releaseIPAudit, type AuditEntry, type AuditQuery, type AuditSource, type ReleaseResult } from './api'
defineEmits<{ configure: [] }>()
const { t, te } = useI18n()
const tableToolbarTarget = ref<HTMLElement | null>(null)
const columns = computed(() => [
  { key: 'subject', label: `IP / ${t('ipAudit.subject')}` },
  { key: 'source', label: t('ipAudit.source') },
  { key: 'reason', label: t('ipAudit.reason') },
  { key: 'state', label: t('ipAudit.state') },
  { key: 'count', label: t('ipAudit.count') },
  { key: 'time', label: `${t('ipAudit.start')} / ${t('ipAudit.expiry')}` },
  { key: 'actions', label: t('common.actions') }
].map(column => ({ ...column, primary: true })))
const sourceName = (id: string) => t(`ipAudit.sources.${id}`)
const label = (value: string) => te(`ipAudit.labels.${value}`) ? t(`ipAudit.labels.${value}`) : value || t('ipAudit.unavailable')
const source = ref('anonymous_api'), ip = ref(''), state = ref(''), sort = ref('expires_desc'), start = ref(''), end = ref(''), userId = ref('')
const inventory = ref<AuditSource[]>([]), rows = ref<AuditEntry[]>([]), hasMore = ref(false), partial = ref(false)
const loading = ref(false), releasing = ref(false), error = ref(''), confirmEntry = ref<AuditEntry | null>(null), outcome = ref<ReleaseResult | null>(null)
const reason = ref('operator_review'), reasons = ['operator_review', 'false_positive', 'configuration_fix', 'support_request']
const states = ['active', 'disabled', 'expired', 'observed', 'historical', 'allowlisted']
const applied = ref<AuditQuery>({ source: 'anonymous_api', cursor: 0, limit: 20 }), nextCursor = ref(0)
const listError = ref(false)
let loadedPages = 0, failedOperation: 'reset' | 'append' | 'refresh' = 'reset'
const selectedSource = computed(() => inventory.value.find(s => s.id === source.value))
const exactSource = computed(() => source.value === 'login_failures' || source.value === 'login_limiter')
let controller: AbortController | undefined, disposed = false
const date = (value: number | null) => value == null ? t('ipAudit.unavailable') : new Date(value * 1000).toLocaleString()
const timestamp = (value: string) => value ? Math.floor(new Date(value).getTime() / 1000) : undefined
async function load(operation: 'reset' | 'append' | 'refresh' = 'reset') {
  if (operation === 'append' && (loading.value || releasing.value || !hasMore.value || listError.value)) return
  controller?.abort(); const current = new AbortController(); controller = current
  loading.value = true; error.value = ''; listError.value = false; confirmEntry.value = null
  failedOperation = operation
  const pagesToRead = operation === 'refresh' ? Math.max(1, loadedPages) : 1
  let cursor = operation === 'append' ? nextCursor.value : 0
  let nextRows = operation === 'append' ? [...rows.value] : []
  let nextPartial = operation === 'append' && partial.value
  if (operation === 'reset') { rows.value = []; hasMore.value = false; nextCursor.value = 0; loadedPages = 0; partial.value = false }
  try {
    let lastResult: Awaited<ReturnType<typeof listIPAudit>> | undefined
    let readPages = 0
    for (; readPages < pagesToRead;) {
      const result = await listIPAudit({ ...applied.value, cursor }, current.signal)
      if (current.signal.aborted || disposed) return
      const byId = new Map(nextRows.map(row => [row.id, row]))
      for (const row of result.items) byId.set(row.id, row)
      nextRows = [...byId.values()]
      nextPartial = nextPartial || !!result.partial
      // A stalled cursor cannot safely make progress; never loop on the same page.
      lastResult = { ...result, has_more: result.has_more && result.next_cursor !== cursor }
      cursor = result.next_cursor
      readPages++
      if (!lastResult.has_more) break
    }
    if (!lastResult || current.signal.aborted || disposed) return
    rows.value = nextRows; inventory.value = lastResult.sources; hasMore.value = lastResult.has_more
    nextCursor.value = cursor; partial.value = nextPartial
    loadedPages = operation === 'append' ? loadedPages + readPages : readPages
  } catch { if (!current.signal.aborted && !disposed) { error.value = t('ipAudit.loadError'); listError.value = true } }
  finally { if (controller === current && !disposed) loading.value = false }
}
function sourceChanged() { if (releasing.value) return; state.value = ''; ip.value = ''; sort.value = 'expires_desc'; outcome.value = null; apply() }
function apply() {
  if (releasing.value) return
  outcome.value = null
  applied.value = { source: source.value, ip: ip.value.trim() || undefined, state: state.value || undefined, sort: sort.value, start: timestamp(start.value), end: timestamp(end.value), user_id: userId.value ? Number(userId.value) : undefined, cursor: 0, limit: 20 }
  void load('reset')
}
function refresh() { if (!releasing.value) void load('refresh') }
function loadMore() { void load('append') }
function retry() { if (!loading.value && !releasing.value) { listError.value = false; void load(failedOperation) } }
async function release() {
  const entry = confirmEntry.value; if (!entry || releasing.value) return
  controller?.abort()
  releasing.value = true; error.value = ''; outcome.value = null
  try {
    const result = await releaseIPAudit(entry, reason.value)
    if (disposed) return
    outcome.value = result; confirmEntry.value = null
    await load('refresh')
  } catch {
    if (!disposed) { await load('refresh'); error.value = t('ipAudit.releaseError'); confirmEntry.value = null }
  }
  finally { if (!disposed) releasing.value = false }
}
onMounted(() => { void load() })
onUnmounted(() => { disposed = true; controller?.abort() })
</script>
