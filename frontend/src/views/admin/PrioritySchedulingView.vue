<template>
  <AppLayout :show-page-title="false">
    <ConsoleObjectLinks v-if="contextAccountId" kind="account" :id="contextAccountId" context class="open-page mb-4" />
    <div class="open-page min-w-0 space-y-5">
      <header><AppHeading variant="page" as="h1">{{ t('priorityScheduling.title') }}</AppHeading><p class="mt-2 text-sm text-gray-500">{{ t('priorityScheduling.description') }}</p></header>
      <p class="rounded-xl bg-primary-50 p-4 text-sm leading-6 text-primary-800 dark:bg-primary-950/30 dark:text-primary-200">{{ t('priorityScheduling.scopeNote') }}</p>
      <div v-if="error" role="alert" class="text-red-600">{{ error }} <button v-if="!draft" class="underline" @click="load">{{ t('priorityScheduling.retry') }}</button></div>
      <p v-if="notice" role="status" class="text-emerald-600">{{ notice }}</p>
      <p v-if="loading">{{ t('priorityScheduling.loading') }}</p>
      <form v-if="draft" class="card p-5" @submit.prevent="save">
        <fieldset :disabled="saving" class="min-w-0 space-y-5">
          <label class="flex items-center gap-3 font-medium"><Toggle v-model="draft.enabled" data-testid="enabled" :aria-label="t('priorityScheduling.enabled')" />{{ t('priorityScheduling.enabled') }}</label>
          <label class="block text-sm"><span class="flex items-center gap-3"><input v-model="draft.balance_protocols" data-testid="balance-protocols" type="checkbox" />{{ t('priorityScheduling.balanceProtocols') }}</span><small class="mt-1 block text-gray-500">{{ t('priorityScheduling.balanceProtocolsHint') }}</small></label>
          <section class="open-section">
            <label class="flex items-center gap-3 font-medium"><Toggle :model-value="!!draft.oauth_quota_priority" @update:model-value="draft.oauth_quota_priority = $event" data-testid="oauth-quota-priority" :aria-label="t('priorityScheduling.oauthQuotaPriority')" />{{ t('priorityScheduling.oauthQuotaPriority') }}</label>
            <p class="mt-2 text-sm leading-6 text-gray-500">{{ t('priorityScheduling.oauthQuotaHint') }}</p>
            <label v-if="draft.oauth_quota_priority" class="mt-3 block max-w-xs"><span class="field-title">{{ t('priorityScheduling.oauthQuotaThreshold') }}</span><input v-model.number="draft.oauth_quota_threshold" data-testid="oauth-quota-threshold" type="number" min="1" max="100" step="1" required class="input w-full" /></label>
          </section>
          <div class="grid gap-5 lg:grid-cols-2">
            <section class="min-w-0 space-y-4">
              <label class="block"><span class="field-title">{{ t('priorityScheduling.strategy') }}</span><select v-model="draft.mode" class="input w-full" data-testid="mode"><option v-for="mode in modes" :key="mode" :value="mode">{{ t(`priorityScheduling.modes.${mode}`) }}</option></select></label>
              <div class="grid grid-cols-2 gap-3"><label v-for="(weight, index) in weightKeys" :key="weight"><span class="field-title">{{ t(`priorityScheduling.${weight}`) }}</span><input v-if="draft.mode === 'custom'" v-model.number="draft[weight]" type="number" min="0" max="100" step="0.1" required class="input w-full" /><div v-else class="open-section text-sm">{{ presets[draft.mode][index] }}%</div></label></div>
              <p class="text-sm leading-6 text-gray-500">{{ t('priorityScheduling.rule') }}</p>
              <p class="text-xs leading-5 text-gray-500">{{ t('priorityScheduling.costHint') }}</p>
              <label><span class="field-title">{{ t('priorityScheduling.error_rate_weight') }}</span><input v-model.number="draft.error_rate_weight" data-testid="error-rate-weight" type="number" min="0" max="100" step="1" required class="input w-full" /></label>
              <p class="text-xs leading-5 text-gray-500">{{ t('priorityScheduling.errorRateHint') }}</p>
            </section>
            <section>
              <div class="mb-3 grid gap-3 sm:grid-cols-2">
                <label><span class="field-title">{{ t('priorityScheduling.latencyPercentile') }}</span><select v-model.number="draft.latency_percentile" data-testid="latency-percentile" class="input w-full"><option v-for="q in [50,70,90,95]" :key="q" :value="q">P{{ q }}</option></select></label>
                <label><span class="field-title">{{ t('priorityScheduling.latencyMeasurement') }}</span><select v-model="draft.latency_measurement" data-testid="latency-measurement" class="input w-full"><option value="legacy_forward">{{ t('priorityScheduling.legacyForward') }}</option><option value="attributed_v1">{{ t('priorityScheduling.attributedV1') }}</option></select></label>
              </div>
              <label class="mb-3 block"><span class="field-title">{{ t('priorityScheduling.latencyPenaltyMode') }}</span><select v-model="draft.latency_penalty_mode" data-testid="latency-penalty-mode" class="input w-full"><option value="deprioritize">{{ t('priorityScheduling.latencyPenaltyModes.deprioritize') }}</option><option value="stop">{{ t('priorityScheduling.latencyPenaltyModes.stop') }}</option></select><small class="mt-2 block text-gray-500">{{ t('priorityScheduling.latencyPenaltyModeHint') }}</small></label>
              <p class="mb-3 text-xs leading-5 text-gray-500">{{ t('priorityScheduling.measurementHint') }}</p>
              <AppHeading variant="section" as="h2" class="mb-3">{{ t('priorityScheduling.thresholds') }}</AppHeading><div class="grid gap-3 sm:grid-cols-2"><label v-for="field in fields" :key="field.key"><span class="field-title">{{ t(`priorityScheduling.${field.key}`) }}</span><input v-model.number="draft[field.key]" type="number" :data-testid="field.key" :min="field.min" :max="field.max" step="1" required class="input w-full" /></label></div><p class="mt-3 text-xs leading-5 text-gray-500">{{ t('priorityScheduling.latencyPenaltyHint') }}</p></section>
          </div>
          <div class="grid gap-5 sm:grid-cols-2"><label><span class="field-title">{{ t('priorityScheduling.groupIds') }}</span><input v-model="groups" class="input w-full" data-testid="groups" /><small class="text-gray-500">{{ t('priorityScheduling.groupHint') }}</small></label><label><span class="field-title">{{ t('priorityScheduling.models') }}</span><textarea v-model="modelsText" class="input w-full" rows="3" /><small class="text-gray-500">{{ t('priorityScheduling.modelHint') }}</small></label></div>
          <button class="btn btn-primary" type="submit">{{ t(saving ? 'priorityScheduling.saving' : 'priorityScheduling.save') }}</button>
        </fieldset>
      </form>
      <section class="open-section p-5">
        <header class="flex flex-wrap items-center justify-between gap-3"><AppHeading variant="section" as="h2">{{ t('priorityScheduling.recent') }}</AppHeading><button class="btn btn-secondary" :disabled="refreshing" @click="refresh">{{ t('priorityScheduling.refresh') }}</button></header>
        <p class="my-3 text-xs leading-5 text-gray-500">{{ t('priorityScheduling.snapshotHint') }}</p>
        <p v-if="snapshot && contextAccountId && !visibleCandidates.length" class="py-4 text-sm">{{ t('common.noData') }}</p>
        <p v-if="snapshotError" role="alert" class="text-red-600">{{ snapshotError }}</p>
        <p v-if="!snapshot" class="py-8 text-center text-sm text-gray-500">{{ t('priorityScheduling.empty') }}</p>
        <template v-else>
          <p data-testid="priority-snapshot-at" class="mb-4 text-sm text-gray-500">{{ new Date(snapshot.at).toLocaleString() }} · {{ t('priorityScheduling.model') }}: {{ snapshot.model }} · {{ t('priorityScheduling.group') }}: {{ snapshot.group_id ?? '—' }} · {{ t(`priorityScheduling.modes.${snapshot.mode}`) }}</p>
          <p v-if="snapshot.history_status && snapshot.history_status !== 'ready'" role="status" class="text-sm text-amber-600">{{ t(`priorityScheduling.historyStates.${snapshot.history_status}`) }}</p>
          <p v-else-if="!snapshot.history_ready" class="text-sm text-amber-600">{{ t('priorityScheduling.historyPending') }}</p>
          <p v-if="snapshot.history_error && snapshot.history_ready" class="text-sm text-amber-600">{{ t('priorityScheduling.historyRetry') }}</p>
          <p v-if="snapshot.evaluated_at" data-testid="priority-evaluated-at" class="mb-3 text-xs text-gray-500">{{ t('priorityScheduling.evaluatedAt') }}: {{ new Date(snapshot.evaluated_at).toLocaleString() }}<template v-if="snapshot.history_observed_at"> · {{ t('priorityScheduling.historyObservedAt') }}: {{ new Date(snapshot.history_observed_at).toLocaleString() }}</template></p>
          <p class="mb-3 text-xs text-gray-500">P{{ snapshot.latency_percentile ?? 90 }} · {{ snapshot.latency_measurement ?? 'legacy_forward' }} · {{ t('priorityScheduling.window_minutes') }}: {{ snapshot.window_minutes ?? '—' }} · {{ t('priorityScheduling.configRevision') }}: {{ snapshot.config_revision ?? '—' }}<template v-if="snapshot.history_window_end"> · {{ t('priorityScheduling.windowEnd') }}: {{ new Date(snapshot.history_window_end).toLocaleString() }}</template></p>
          <div v-if="visibleCandidates.length" data-testid="priority-candidate-scroll" class="overflow-x-auto overflow-y-hidden"><DataTable column-order-key="admin-priority-candidates"
 :columns="openRecordColumns"
 :primary-columns="openRecordColumns.map(column => column.key)"
 :data="visibleCandidates" row-key="account_id" >
<template #cell-account="{ row: row }"><div class=""><strong class="font-medium">{{ row.account_name }}</strong><small class="block text-gray-400">#{{ row.account_id }}</small><small v-if="row.bound_groups != null" class="block text-gray-400">{{ t('priorityScheduling.boundGroups') }}: {{ row.bound_groups }}</small></div></template>
<template #cell-tier="{ row: row }"><div class=""><span :class="row.tier === 'eligible' ? 'text-emerald-600' : row.tier === 'degraded' ? 'text-amber-600' : 'text-gray-500'">{{ t(`priorityScheduling.tiers.${row.tier}`) }}</span><small v-if="row.oauth_quota_role" class="block font-medium text-primary-600">{{ t(`priorityScheduling.oauthQuotaRoles.${row.oauth_quota_role}`) }}</small><small v-if="row.history_status && row.history_status !== 'ready'" class="block text-amber-600">{{ t(`priorityScheduling.historyStates.${row.history_status}`) }}</small><small v-if="row.latency_stopped" class="block font-medium text-red-600">{{ t('priorityScheduling.latencyPenaltyModes.stop') }}</small><small v-if="row.latency_penalty_until && !row.latency_recovery" class="block text-gray-400">{{ t('priorityScheduling.latencyPenaltyUntil') }}: {{ new Date(row.latency_penalty_until).toLocaleString() }}</small><small v-for="reason in row.reasons" :key="reason" class="block text-gray-400">{{ t(`priorityScheduling.reasons.${reason}`) }}</small></div></template>
<template #cell-priority="{ row: row }"><div class="tabular-nums">{{ row.priority }}</div></template>
<template #cell-score="{ row: row }"><div class="tabular-nums">{{ row.score.toFixed(1) }}<small v-if="row.selection_weight != null" class="block text-gray-400">{{ t('priorityScheduling.selectionWeight') }}: {{ row.selection_weight.toFixed(2) }}</small><small v-if="row.exploration_eligible" class="block text-gray-400">{{ t('priorityScheduling.explorationEligible') }}</small></div></template>
<template #cell-errorRate="{ row }"><div :data-testid="`error-rate-${row.account_id}`" :title="t('priorityScheduling.errorRateHint')"><span v-if="row.error_rate == null">{{ t('priorityScheduling.errorNoSamples') }}</span><span v-else>{{ (row.error_rate * 100).toFixed(1) }}%</span><small v-if="(row.error_successes ?? 0) + (row.error_failures ?? 0) > 0" class="block text-gray-400">{{ t('priorityScheduling.errorAttempts') }}: {{ row.error_failures ?? 0 }} / {{ (row.error_successes ?? 0) + (row.error_failures ?? 0) }}</small><small v-if="errorSamplesLow(row)" class="block text-amber-600">{{ t('priorityScheduling.errorInsufficientSamples') }}</small><small v-if="row.error_observed_at" class="block text-gray-400">{{ t('priorityScheduling.errorObservedAt') }}: {{ new Date(row.error_observed_at).toLocaleString() }}</small><small class="block text-gray-400">{{ t('priorityScheduling.errorWindow') }}: {{ row.error_window_minutes ?? '—' }} min · {{ row.error_endpoint ?? '—' }}</small><small v-if="row.error_penalty != null" class="block text-gray-400">{{ t('priorityScheduling.errorPenalty') }}: {{ row.error_penalty.toFixed(3) }} · {{ t('priorityScheduling.baseScore') }}: {{ row.base_score?.toFixed(1) ?? '—' }}</small><small class="block text-gray-400">{{ t('priorityScheduling.errorLocalScope') }}</small></div></template>
<template #cell-quality="{ row: row }"><div class="">{{ row.quality_samples ? `${row.quality_passed}/${row.quality_samples}` : '—' }}</div></template>
<template #cell-latency="{ row }"><div>P{{ row.latency_percentile ?? 90 }}: {{ row.samples && selectedLatency(row) != null ? `${Math.round(selectedLatency(row)!)} ms` : t('priorityScheduling.unknown') }}<small class="block text-gray-400">{{ row.samples }} {{ t('priorityScheduling.samples') }}</small><small v-if="row.samples < (snapshot.min_samples ?? 5) || row.samples < tailSampleFloor(row.latency_percentile ?? 90)" class="block text-amber-600">{{ t('priorityScheduling.lowSamples') }}</small><small class="block text-gray-400">{{ t('priorityScheduling.unknown') }}: {{ row.latency_unknown_samples ?? '—' }}</small><small class="block text-gray-400">P90: {{ row.samples ? `${Math.round(row.p90_ttft_ms)} ms` : '—' }}</small><small class="block text-gray-400">{{ t('priorityScheduling.experienceP90') }}: {{ row.experience_samples ? `${Math.round(row.experience_p90_ms ?? 0)} ms (${row.experience_samples})` : '—' }}</small></div></template>
<template #cell-load="{ row: row }"><div class="">{{ row.load_percent == null ? '—' : `${row.load_percent}%` }}</div></template>
<template #cell-rate="{ row: row }"><div class="">{{ row.rate == null ? '—' : `${row.rate.toFixed(3)}×` }}</div></template>
<template #cell-profit="{ row: row }"><div class=""><template v-if="row.economics_source === 'usage'"><span :class="(row.profit ?? 0) < 0 ? 'text-red-600' : 'text-emerald-600'">${{ row.profit?.toFixed(4) }} · {{ row.margin == null ? '—' : `${(row.margin * 100).toFixed(1)}%` }}</span><small class="block text-gray-400">${{ row.revenue.toFixed(4) }} − ${{ row.theoretical_cost.toFixed(4) }}</small></template><span v-else class="text-xs text-gray-500">{{ t(`priorityScheduling.economics.${row.economics_source || 'unknown'}`) }}</span></div></template>
</DataTable></div><p v-else-if="!contextAccountId" class="py-4 text-sm text-gray-500" data-testid="priority-no-candidates">{{ t('priorityScheduling.noCandidates') }}</p>
        </template>
      </section>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
import AppHeading from '@/components/common/AppHeading.vue'
import DataTable from '@/components/common/DataTable.vue'
const contextRoute = useRoute()
const contextAccountId = computed(() => objectQueryId(contextRoute?.query?.account_id))
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { objectQueryId } from './objectContext'
import ConsoleObjectLinks from '@/components/admin/ConsoleObjectLinks.vue'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getPriorityConfig, savePriorityConfig, getPrioritySnapshot, type PrioritySchedulingConfig, type PrioritySnapshot, type PriorityCandidate } from '@/api/admin/priorityScheduling'
function selectedLatency(row: PriorityCandidate) {
  if (row.selected_ttft_ms !== undefined) return row.selected_ttft_ms
  return (row.latency_percentile ?? 90) === 90 && (row.latency_measurement ?? 'legacy_forward') === 'legacy_forward' ? row.p90_ttft_ms : null
}
function errorSamplesLow(row: PriorityCandidate) {
  const observed = (row.error_successes ?? 0) + (row.error_failures ?? 0)
  return observed > 0 && observed < (row.error_prior_samples ?? 0)
}
const tailSampleFloor = (q: number) => Math.max(20, Math.ceil(500 / (100 - q)))
const { t } = useI18n(), auth = useAuthStore()
const draft = ref<PrioritySchedulingConfig | null>(null), snapshot = ref<PrioritySnapshot | null>(null)
const visibleCandidates = computed(() => (snapshot.value?.candidates ?? []).filter(account => !contextAccountId.value || account.account_id === contextAccountId.value))
const groups = ref(''), modelsText = ref(''), error = ref(''), snapshotError = ref(''), notice = ref('')
const loading = ref(false), saving = ref(false), refreshing = ref(false)
const modes = ['experience', 'balanced', 'profit', 'custom'] as const
const presets = { experience: [40,35,20,5], balanced: [30,25,25,20], profit: [20,15,20,45] }
const weightKeys = ['quality_weight','latency_weight','load_weight','cost_weight'] as const
const fields = [
  { key: 'error_rate_window_minutes', min: 1, max: 60 },
  { key: 'error_rate_prior_samples', min: 1, max: 1000 },
  { key: 'recovery_exploration_percent', min: 0, max: 10 },
  { key: 'target_ttft_ms', min: 100, max: 120000 }, { key: 'latency_penalty_minutes', min: 0, max: 1440 }, { key: 'max_load_percent', min: 10, max: 100 },
  { key: 'min_quality_percent', min: 0, max: 100 }, { key: 'window_minutes', min: 5, max: 1440 },
  { key: 'min_samples', min: 1, max: 1000 }, { key: 'quality_max_age_hours', min: 1, max: 168 }
] as const
let alive = true, generation = 0
let historyTimer: ReturnType<typeof setTimeout> | undefined
function stopHistoryRefresh() { clearTimeout(historyTimer); historyTimer = undefined }
const active = (version: number) => alive && generation === version
async function load() {
  if (loading.value) return
  const version = generation; loading.value = true; error.value = ''
  try {
    const config = await getPriorityConfig()
    if (!active(version)) return
    const { teams: _legacyTeams, ...currentConfig } = config as PrioritySchedulingConfig & { teams?: unknown }
    draft.value = { ...currentConfig, error_rate_weight: currentConfig.error_rate_weight ?? 0, error_rate_window_minutes: currentConfig.error_rate_window_minutes ?? 10, error_rate_prior_samples: currentConfig.error_rate_prior_samples ?? 5, latency_percentile: currentConfig.latency_percentile ?? 90, latency_measurement: currentConfig.latency_measurement ?? 'legacy_forward', recovery_exploration_percent: currentConfig.recovery_exploration_percent ?? 0, latency_penalty_mode: currentConfig.latency_penalty_mode ?? 'deprioritize', latency_penalty_minutes: currentConfig.latency_penalty_minutes ?? 10, balance_protocols: currentConfig.balance_protocols ?? true, oauth_quota_priority: currentConfig.oauth_quota_priority ?? false, oauth_quota_threshold: currentConfig.oauth_quota_threshold ?? 90 }; groups.value = (config.group_ids ?? []).join(', '); modelsText.value = (config.models ?? []).join('\n')
  } catch { if (active(version)) error.value = t('priorityScheduling.error') }
  finally { if (active(version)) loading.value = false }
}
async function save() {
  if (!draft.value || saving.value) return
  const ids = groups.value.trim() ? groups.value.split(/[,，]/).map(v => Number(v.trim())) : []
  if (ids.some(id => !Number.isSafeInteger(id) || id <= 0)) { error.value = t('priorityScheduling.invalid'); return }
  const version = generation; saving.value = true; error.value = ''; notice.value = ''
  try {
    const config = await savePriorityConfig({ ...draft.value, group_ids: [...new Set(ids)], models: [...new Set(modelsText.value.split('\n').map(v => v.trim()).filter(Boolean))] })
    if (active(version)) { draft.value = config; notice.value = t('priorityScheduling.saved') }
  } catch { if (active(version)) error.value = t('priorityScheduling.error') }
  finally { if (active(version)) saving.value = false }
}
async function refresh() {
  stopHistoryRefresh()
  await loadSnapshot(0)
}
async function loadSnapshot(attempt: number) {
  if (refreshing.value) return
  const version = generation; refreshing.value = true; snapshotError.value = ''
  try {
    const value = await getPrioritySnapshot()
    if (active(version)) {
      snapshot.value = value
      // Short follow-ups collect an in-flight query; subsequent polling sees
      // usage committed after a successful empty read. Only one timer/request
      // exists, and hidden tabs pause reads. Backend cache bounds SQL work.
      const pending = value?.history_refreshing || value?.history_status === 'loading' || value?.history_status === 'partial'
      // Also discover the first selection when this page opened before any pool
      // existed. A null snapshot schedules no history SQL on the server.
      scheduleHistoryRefresh(version, pending && attempt < 3 ? 1000 : 5000, pending ? attempt + 1 : 0)
    }
  }
  catch { if (active(version)) { snapshotError.value = t('priorityScheduling.error'); scheduleHistoryRefresh(version, 15000, 0) } }
  finally { if (active(version)) refreshing.value = false }
}
function scheduleHistoryRefresh(version: number, delay: number, attempt: number) {
  stopHistoryRefresh()
  historyTimer = setTimeout(() => {
    if (!active(version)) return
    if (document.visibilityState === 'hidden') { scheduleHistoryRefresh(version, 5000, attempt); return }
    void loadSnapshot(attempt)
  }, delay)
}
watch(() => auth.user ? `${auth.user.id}:${auth.user.role}` : '', () => { stopHistoryRefresh(); generation++; draft.value = null; snapshot.value = null; groups.value = modelsText.value = error.value = notice.value = snapshotError.value = ''; loading.value = saving.value = refreshing.value = false }, { flush: 'sync' })
onMounted(() => { void load(); void refresh() })
onBeforeUnmount(() => { stopHistoryRefresh(); alive = false; generation++ })

const openRecordColumns = computed(() => [{ key: 'account', label: t('priorityScheduling.account'), sortable: false },
  { key: 'tier', label: t('priorityScheduling.tier'), sortable: false },
  { key: 'priority', label: t('priorityScheduling.priority'), sortable: false },
  { key: 'score', label: t('priorityScheduling.score'), sortable: false },
  { key: 'errorRate', label: t('priorityScheduling.errorRate'), sortable: false },
  { key: 'quality', label: t('priorityScheduling.quality'), sortable: false },
  { key: 'latency', label: t('priorityScheduling.latency'), sortable: false },
  { key: 'load', label: t('priorityScheduling.load'), sortable: false },
  { key: 'rate', label: t('priorityScheduling.rate'), sortable: false },
  { key: 'profit', label: t('priorityScheduling.profit'), sortable: false }])
</script>
<style scoped>
.field-title { @apply mb-2 block text-xs font-medium text-gray-600 dark:text-gray-300; }
input[type=checkbox] { @apply h-4 w-4 rounded text-primary-600; }
button:disabled { @apply cursor-not-allowed opacity-50; }
</style>
