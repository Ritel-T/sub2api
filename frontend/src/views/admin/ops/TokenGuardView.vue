<template>
  <AppLayout :show-page-title="false">
    <div class="open-page token-guard">
      <header class="ops-heading">
        <div>
          <AppHeading variant="page" as="h1">{{ t('tokenGuard.title') }}</AppHeading>
          <p class="subtitle">{{ t('tokenGuard.description') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <button class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load()">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />{{ t('tokenGuard.refresh') }}
          </button>
          <button class="btn btn-primary inline-flex items-center gap-2" :disabled="running || loading" @click="run">
            <Icon name="play" size="sm" />{{ t(running ? 'tokenGuard.running' : 'tokenGuard.runNow') }}
          </button>
        </div>
      </header>

      <p v-if="error" role="alert" class="error-banner">{{ error }}</p>
      <p v-if="notice" role="status" class="success-banner">{{ notice }}</p>

      <section class="summary-grid">
        <article class="summary-card"><span>{{ t('tokenGuard.statsProbed') }}</span><strong>{{ remote?.runtime.stats.probed ?? 0 }}</strong><small>{{ remote?.runtime.job?.status === 'running' ? `${remote.runtime.job.completed}/${remote.runtime.job.total}` : `${t('tokenGuard.interval')} ${draft?.interval_seconds ?? 0}s` }}</small></article>
        <article class="summary-card"><span>{{ t('tokenGuard.statsBad') }}</span><strong>{{ badCount }}</strong><small>{{ t('tokenGuard.failStreak') }} ≥ {{ draft?.fail_streak_threshold ?? 1 }}</small></article>
        <article class="summary-card"><span>{{ t('tokenGuard.statsRepaired') }}</span><strong>{{ remote?.runtime.stats.repaired ?? 0 }}</strong><small>{{ t('tokenGuard.stateFixed') }} {{ remote?.runtime.stats.state_fixed ?? 0 }}</small></article>
        <article class="summary-card"><span>{{ t('tokenGuard.lastRun') }}</span><strong class="text-base">{{ remote?.runtime.last_run ? date(remote.runtime.last_run) : t('tokenGuard.never') }}</strong><small>{{ remote?.runtime.last_message || '-' }}</small></article>
      </section>

      <div class="ops-columns">
        <section class="settings-card">
          <div class="section-title"><span class="icon-tile"><Icon name="shield" size="md" /></span><div><AppHeading variant="subsection" as="h3">{{ t('tokenGuard.title') }}</AppHeading><p>{{ t('tokenGuard.enabledHint') }}</p></div></div>
          <form v-if="draft" class="settings-form" @submit.prevent="save">
            <fieldset :disabled="saving">
              <label class="enable-row"><span><strong>{{ t('tokenGuard.enabled') }}</strong><small>{{ t('tokenGuard.enabledHint') }}</small></span><Toggle v-model="draft.enabled" :aria-label="t('tokenGuard.enabled')" /></label>
              <label class="field-label">{{ t('tokenGuard.groupIds') }}</label>
              <Select
                v-model="selectedGroupIds"
                multiple
                searchable
                :options="groupOptions"
                :placeholder="t('tokenGuard.allGroups')"
                :aria-label="t('tokenGuard.groupIds')"
                :disabled="groupsLoading"
              />
              <p v-if="groupsLoadError" class="field-hint text-amber-600 dark:text-amber-400">{{ t('tokenGuard.groupsLoadError') }}</p>
              <p class="field-hint">{{ t('tokenGuard.groupIdsHint') }}</p>

              <div class="grid-2">
                <label class="field-label">{{ t('tokenGuard.interval') }}<input v-model.number="draft.interval_seconds" type="number" min="30" max="86400" class="input w-full" /></label>
                <label class="field-label">{{ t('tokenGuard.probeConcurrency') }}<input v-model.number="draft.probe_concurrency" type="number" min="1" max="16" class="input w-full" /></label>
                <label class="field-label">{{ t('tokenGuard.probeTimeout') }}<input v-model.number="draft.probe_timeout_seconds" type="number" min="5" max="900" class="input w-full" /></label>
                <label class="field-label">{{ t('tokenGuard.maxProbePerCycle') }}<input v-model.number="draft.max_probe_per_cycle" type="number" min="1" max="100" class="input w-full" /></label>
                <label class="field-label">{{ t('tokenGuard.failStreak') }}<input v-model.number="draft.fail_streak_threshold" type="number" min="1" max="10" class="input w-full" /></label>
              </div>

              <label class="field-label">{{ t('tokenGuard.probeEndpoint') }}<input v-model.trim="draft.probe_endpoint" class="input w-full" /></label>
              <label class="field-label">{{ t('tokenGuard.probeModel') }}<input v-model.trim="draft.probe_model" class="input w-full" placeholder="gpt-6-astra" /></label>
              <p class="field-hint">{{ t('tokenGuard.probeModelHint') }}</p>
              <label class="field-label">{{ t('tokenGuard.probeHeaders') }}</label>
              <textarea v-model="probeHeadersText" rows="3" class="input w-full" placeholder="Header-Name: value"></textarea>
              <p class="field-hint">{{ t('tokenGuard.headersHint') }}</p>

              <label class="kind-option"><input v-model="draft.auto_relogin" type="checkbox" /><span><strong>{{ t('tokenGuard.autoRelogin') }}</strong><small>{{ t('tokenGuard.reloginEndpoint') }}</small></span></label>
              <input v-model.trim="draft.relogin_endpoint" class="input w-full" />
              <label class="field-label">{{ t('tokenGuard.reloginHeaders') }}</label>
              <textarea v-model="reloginHeadersText" rows="3" class="input w-full" placeholder="Header-Name: value"></textarea>
              <p class="field-hint">{{ t('tokenGuard.headersHint') }}</p>
              <label class="kind-option"><input v-model="draft.restore_schedulable" type="checkbox" /><span><strong>{{ t('tokenGuard.restoreSchedulable') }}</strong><small>{{ t('tokenGuard.scopeNote') }}</small></span></label>

              <label class="field-label">{{ t('tokenGuard.reloginAccounts') }}</label>
              <textarea v-model="reloginText" rows="7" class="input w-full font-mono text-xs" placeholder="user@example.com----password----JBSWY3DPEHPK3PXP"></textarea>
              <p class="field-hint">{{ t('tokenGuard.reloginAccountsHint') }}</p>
              <p v-if="managedCredentialCount" class="field-hint" data-testid="token-guard-managed-credentials">{{ t('tokenGuard.managedCredentialsHint', { count: managedCredentialCount }) }}</p>

              <div class="grid-2">
                <label class="field-label">{{ t('tokenGuard.barkKey') }}<input v-model.trim="draft.bark_key" class="input w-full" placeholder="留空则不推送" /></label>
                <div>
                  <label class="kind-option"><input v-model="draft.notify_on_fix" type="checkbox" /><span><strong>{{ t('tokenGuard.notifyOnFix') }}</strong><small>Bark</small></span></label>
                  <label class="kind-option"><input v-model="draft.notify_on_fail" type="checkbox" /><span><strong>{{ t('tokenGuard.notifyOnFail') }}</strong><small>Bark</small></span></label>
                </div>
              </div>

              <div class="settings-actions"><span>{{ dirty ? t('tokenGuard.unsaved') : t('tokenGuard.saved') }}</span><button class="btn btn-primary" :disabled="saving || !dirty">{{ t(saving ? 'qualityOps.saving' : 'tokenGuard.save') }}</button></div>
            </fieldset>
          </form>
        </section>

        <div class="stack">
          <section class="events-card" :aria-busy="loading" data-testid="token-guard-accounts">
            <header class="events-heading"><div><AppHeading variant="subsection" as="h3">{{ t('tokenGuard.accounts') }}<span>{{ accounts.length + managedAccounts.length }}</span></AppHeading><p>{{ remote?.runtime.last_message || t('tokenGuard.description') }}</p></div><div ref="accountToolbarTarget" /></header>
            <p v-if="managedAccounts.length" class="managed-hint" data-testid="token-guard-managed-hint">{{ t('tokenGuard.managedHint') }}</p>
            <p v-if="managedLoadFailed" class="managed-hint warn" role="alert">{{ t('tokenGuard.managedLoadFailed') }}</p>
            <DataTable :columns="accountColumns" :data="accountRows" row-key="key" column-order-key="admin-token-guard-accounts" :toolbar-target="accountToolbarTarget" :row-attrs="accountRowAttrs" :loading="loading && !accountRows.length">
              <template #cell-account="{ row }"><strong class="block max-w-56 truncate font-medium" :title="row.item.account_name">{{ row.item.account_name || '-' }}</strong><small class="mt-1 block text-xs text-gray-400">#{{ row.item.account_id }}<span v-if="row.kind === 'managed'" class="managed-badge">{{ t('tokenGuard.managedBadge') }}</span></small></template>
              <template #cell-account_status="{ row }"><span class="failure-badge" :class="row.item.account_status === 'error' ? 'danger' : ''">{{ row.item.account_status || '-' }}</span></template>
              <template #cell-schedulable="{ row }">{{ t(row.item.schedulable ? 'tokenGuard.on' : 'tokenGuard.off') }}</template>
              <template #cell-probe="{ row }"><span class="failure-badge" :class="row.kind === 'managed' ? row.item.enabled ? probeClass(row.item.probe_state) : '' : probeClass(row.item.probe_state)">{{ row.kind === 'managed' ? row.item.enabled ? probeLabel(row.item.probe_state) : t('tokenGuard.managedPaused') : t(`tokenGuard.probeStates.${row.item.probe_state || 'pending'}`) }}</span><small class="mt-1 block whitespace-normal break-words text-xs text-gray-400">{{ row.kind === 'managed' ? row.item.blocked_reason || row.item.probe_detail : row.item.probe_detail }}</small></template>
              <template #cell-latency="{ row }">{{ row.kind === 'managed' ? '-' : row.item.latency_ms ? row.item.latency_ms + 'ms' : '-' }}</template>
              <template #cell-last_fix="{ row }">{{ row.kind === 'managed' ? row.item.last_reauth_at ? `${t('tokenGuard.managedReauth')} · ${date(row.item.last_reauth_at)}` : '-' : row.item.last_fix_at ? `${row.item.last_fix_action} · ${date(row.item.last_fix_at)}` : '-' }}<small v-if="row.kind !== 'managed'" class="mt-1 block whitespace-normal break-words text-xs text-gray-400">{{ row.item.last_fix_result }}</small></template>
              <template #cell-actions="{ row }"><RouterLink v-if="row.kind === 'managed'" class="link-btn" to="/admin/token-guard-v2">{{ t('tokenGuard.managedOpen') }}</RouterLink><button v-else class="link-btn" :disabled="reloginBusy === row.item.account_id" @click.stop="relogin(row.item)">{{ t(reloginBusy === row.item.account_id ? 'tokenGuard.reloginBusy' : 'tokenGuard.relogin') }}</button></template>
              <template #empty><div class="empty-state"><Icon name="shield" size="xl" /><AppHeading variant="empty" as="h4">{{ t('tokenGuard.noAccounts') }}</AppHeading></div></template>
            </DataTable>
          </section>

          <section class="events-card" :aria-busy="loading" data-testid="token-guard-events">
            <header class="events-heading"><div><AppHeading variant="subsection" as="h3">{{ t('tokenGuard.events') }}<span>{{ events.length }}</span></AppHeading><p>{{ remote?.runtime.last_message || '-' }}</p></div><div ref="eventToolbarTarget" /></header>
            <DataTable :columns="eventColumns" :data="events" row-key="id" column-order-key="admin-token-guard-events" :toolbar-target="eventToolbarTarget" :loading="loading && !events.length">
              <template #cell-created_at="{ row }">{{ date(row.created_at) }}</template>
              <template #cell-account="{ row }">{{ row.account_name || '-' }}<small v-if="row.account_id" class="mt-1 block text-xs text-gray-400">#{{ row.account_id }}</small></template>
              <template #cell-kind="{ row }"><span class="failure-badge" :class="eventClass(row.kind)">{{ t(`tokenGuard.eventKinds.${row.kind}`) }}</span></template>
              <template #cell-detail="{ row }"><span class="whitespace-normal break-words">{{ row.detail }}</span></template>
              <template #empty><div class="empty-state"><Icon name="infoCircle" size="xl" /><AppHeading variant="empty" as="h4">{{ t('tokenGuard.noEvents') }}</AppHeading></div></template>
            </DataTable>
          </section>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import { groupsAPI } from '@/api/admin/groups'
import type { AdminGroup, SelectOption } from '@/types'
import {
  getTokenGuardStatus,
  formatTokenGuardReloginText,
  parseTokenGuardReloginText,
  reloginTokenGuardAccount,
  startTokenGuardRun,
  saveTokenGuardConfig,
  type TokenGuardConfig,
  type TokenGuardEvent,
  type TokenGuardStatus
} from '@/api/admin/accountTokenGuard'
import { listTokenGuardV2Accounts, type TokenGuardV2Account } from '@/api/admin/accountTokenGuardV2'

const { t, te } = useI18n()
const remote = ref<TokenGuardStatus | null>(null)
const draft = ref<TokenGuardConfig | null>(null)
const reloginText = ref('')
const probeHeadersText = ref('')
const reloginHeadersText = ref('')
const loading = ref(false), saving = ref(false), running = ref(false), reloginBusy = ref(0)
const error = ref(''), notice = ref('')
const managedList = ref<TokenGuardV2Account[]>([]), managedLoadFailed = ref(false)
const groups = ref<AdminGroup[]>([])
const groupsLoading = ref(false)
const groupsLoadError = ref(false)
let timer: ReturnType<typeof setInterval> | undefined
let alive = true

const accounts = computed(() => remote.value?.accounts ?? [])
const events = computed<TokenGuardEvent[]>(() => remote.value?.events ?? [])
// 凭证运营托管的账号（后端已把它们从旧守护列表里排除，这里再按 id 去重一次）。
const managedAccounts = computed(() => {
  const own = new Set(accounts.value.map(item => item.account_id))
  return managedList.value.filter(item => !own.has(item.account_id))
})
const accountToolbarTarget = ref<HTMLElement | null>(null), eventToolbarTarget = ref<HTMLElement | null>(null)
const accountRows = computed(() => [
  ...accounts.value.map(item => ({ kind: 'legacy' as const, key: `legacy-${item.account_id}`, item })),
  ...managedAccounts.value.map(item => ({ kind: 'managed' as const, key: `managed-${item.account_id}`, item }))
])
const accountRowAttrs = (row: typeof accountRows.value[number]) => row.kind === 'managed'
  ? { class: 'managed-row', 'data-managed-account-id': row.item.account_id } : {}
const accountColumns = computed(() => [
  { key: 'account', label: t('tokenGuard.account'), primary: true },
  { key: 'account_status', label: t('tokenGuard.dbStatus'), primary: true },
  { key: 'schedulable', label: t('tokenGuard.schedulable'), primary: true },
  { key: 'probe', label: t('tokenGuard.probe'), primary: true },
  { key: 'latency', label: t('tokenGuard.latency'), primary: true },
  { key: 'last_fix', label: t('tokenGuard.lastFix'), primary: true },
  { key: 'actions', label: t('tokenGuard.actions'), primary: true }
])
const eventColumns = computed(() => [
  { key: 'created_at', label: t('admin.ops.errorLog.time'), primary: true },
  { key: 'account', label: t('tokenGuard.account'), primary: true },
  { key: 'kind', label: t('admin.ops.errorLog.type'), primary: true },
  { key: 'detail', label: t('admin.ops.errorLog.details'), primary: true }
])
const managedCredentialCount = computed(() => managedAccounts.value.filter(item => item.login_config?.configured).length)
const badCount = computed(() => [...accounts.value, ...managedAccounts.value].filter(item => item.probe_state === 'auth' || item.account_status === 'error').length)
const selectedGroupIds = computed<number[]>({
  get: () => draft.value?.group_ids ?? [],
  set: (value) => {
    if (draft.value) {
      draft.value = {
        ...draft.value,
        group_ids: [...new Set(value.map(Number).filter(id => Number.isInteger(id) && id > 0))]
      }
    }
  }
})
const groupOptions = computed<SelectOption[]>(() => {
  const known = groups.value.map(group => ({
    value: group.id,
    label: `${group.name} (#${group.id})`
  }))
  const knownIds = new Set(groups.value.map(group => group.id))
  const missing = selectedGroupIds.value
    .filter(id => !knownIds.has(id))
    .map(id => ({ value: id, label: `#${id}` }))
  return [...missing, ...known]
})
const dirty = computed(() => {
  if (!draft.value || !remote.value) return false
  return JSON.stringify(collect()) !== JSON.stringify(normalize(remote.value.config))
})

const pad = (value: number) => String(value).padStart(2, '0')
const date = (value: string) => {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : `${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`
}
const message = (e: unknown) => (e as { message?: string })?.message || t('qualityOps.error')
const reloginTextOf = (config: TokenGuardConfig | null) => formatTokenGuardReloginText(config?.relogin_accounts)
const parseHeaders = (raw: string) => raw.split(/\r?\n/).reduce<Record<string, string>>((acc, line) => {
  const index = line.indexOf(':')
  if (index > 0) {
    const name = line.slice(0, index).trim()
    const value = line.slice(index + 1).trim()
    if (name && value) acc[name] = value
  }
  return acc
}, {})
const headersTextOf = (headers: Record<string, string> | undefined) => Object.entries(headers ?? {}).map(([name, value]) => `${name}: ${value}`).join('\n')
function normalize(config: TokenGuardConfig): TokenGuardConfig {
  return {
    ...config,
    group_ids: [...(config.group_ids ?? [])].sort((a, b) => a - b),
    relogin_accounts: [...(config.relogin_accounts ?? [])].map(item => ({ email: item.email.toLowerCase(), password: item.password, mfa_secret: item.mfa_secret })).sort((a, b) => a.email.localeCompare(b.email)),
    probe_headers: config.probe_headers ?? {},
    relogin_headers: config.relogin_headers ?? {}
  }
}

function collect(): TokenGuardConfig {
  const base = draft.value!
  return normalize({ ...base, group_ids: base.group_ids, relogin_accounts: parseTokenGuardReloginText(reloginText.value),
    probe_headers: parseHeaders(probeHeadersText.value), relogin_headers: parseHeaders(reloginHeadersText.value) })
}

const probeLabel = (state: string) => {
  const key = `tokenGuard.probeStates.${state || 'pending'}`
  return te(key) ? t(key) : state
}
const probeClass = (state: string) => (state === 'ok' ? 'ok' : state === 'auth' ? 'danger' : '')
const eventClass = (kind: string) => (kind === 'relogin_ok' || kind === 'state_fixed' || kind === 'probe_ok' ? 'ok' : kind === 'relogin_failed' || kind === 'state_failed' || kind === 'probe_auth' ? 'danger' : '')

async function load(silent = false) {
  if (loading.value || saving.value) return
  loading.value = true
  if (!silent) error.value = ''
  try {
    // 凭证运营读取失败不影响旧守护本身，只是暂时不显示托管账号。
    const [status, managed] = await Promise.all([getTokenGuardStatus(), listTokenGuardV2Accounts().catch(() => null)])
    if (!alive) return
    managedLoadFailed.value = !managed
    if (managed) managedList.value = managed.accounts ?? []
    const preserveDraft = dirty.value
    remote.value = status
    if (!preserveDraft) {
      draft.value = { ...status.config }
      reloginText.value = reloginTextOf(status.config)
      probeHeadersText.value = headersTextOf(status.config.probe_headers)
      reloginHeadersText.value = headersTextOf(status.config.relogin_headers)
    }
  } catch (e) {
    if (alive && !silent) error.value = message(e)
  } finally {
    loading.value = false
  }
}

async function loadGroups() {
  groupsLoading.value = true
  groupsLoadError.value = false
  try {
    groups.value = await groupsAPI.getAll('openai')
  } catch {
    groupsLoadError.value = true
  } finally {
    groupsLoading.value = false
  }
}

async function save() {
  if (!draft.value || saving.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const saved = await saveTokenGuardConfig(collect())
    if (!alive) return
    draft.value = { ...saved }
    reloginText.value = reloginTextOf(saved)
    probeHeadersText.value = headersTextOf(saved.probe_headers)
    reloginHeadersText.value = headersTextOf(saved.relogin_headers)
    if (remote.value) remote.value = { ...remote.value, config: saved }
    notice.value = t('tokenGuard.saved')
  } catch (e) {
    error.value = message(e)
  } finally {
    saving.value = false
  }
}

async function run() {
  if (running.value) return
  running.value = true; error.value = ''; notice.value = ''
  try {
    const job = await startTokenGuardRun()
    if (!alive) return
    let current = job
    while (alive && (current.status === 'pending' || current.status === 'running')) {
      await new Promise(resolve => setTimeout(resolve, 1000))
      await load(true)
      const polled = remote.value?.runtime.job
      if (polled?.id === job.id) current = polled
    }
    if (!alive) return
    if (current.status === 'failed') {
      throw new Error(current.error || t('qualityOps.error'))
    }
    if (current.status === 'canceled') {
      notice.value = t('tokenGuard.runCanceled')
      return
    }
    const stats = current.stats
    notice.value = t('tokenGuard.runDone', { probed: stats.probed, healthy: stats.healthy, repaired: stats.repaired, state_fixed: stats.state_fixed })
    await load(true)
  } catch (e) {
    error.value = message(e)
  } finally {
    running.value = false
  }
}

async function relogin(item: { account_id: number }) {
  if (reloginBusy.value) return
  reloginBusy.value = item.account_id; error.value = ''; notice.value = ''
  try {
    const result = await reloginTokenGuardAccount(item.account_id)
    if (!alive) return
    notice.value = t('tokenGuard.reloginDone', { action: result.action })
    await load(true)
  } catch (e) {
    error.value = message(e)
  } finally {
    reloginBusy.value = 0
  }
}

onMounted(() => {
  void load()
  void loadGroups()
  timer = setInterval(() => { if (document.visibilityState === 'visible') void load(true) }, 30_000)
})
onBeforeUnmount(() => { alive = false; if (timer) clearInterval(timer) })
</script>

<style scoped>
.token-guard { @apply w-full min-w-0 text-gray-900 dark:text-gray-100; }
.ops-heading { @apply mb-6 flex flex-wrap items-center justify-between gap-4; }
.eyebrow { @apply mb-1 text-[11px] font-semibold tracking-widest text-primary-600 dark:text-primary-400; }
.subtitle { @apply mt-2 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400; }

.summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 20px; @apply mb-5; }
.summary-card { @apply min-w-0 overflow-hidden  p-5    border-t border-gray-200 dark:border-dark-700; }
.summary-card > span { @apply block text-xs font-medium text-gray-400; }
.summary-card > strong { @apply mt-2 block text-2xl font-semibold tabular-nums tracking-tight; }
.summary-card > small { @apply mt-2 block truncate text-xs text-gray-400; }

.ops-columns { display: grid; grid-template-columns: minmax(320px,.32fr) minmax(0,.68fr); gap: 20px; }
.stack { display: flex; min-width: 0; flex-direction: column; gap: 20px; }
.settings-card, .events-card { @apply min-w-0 min-w-0 border-t border-gray-200 dark:border-dark-700; }
.section-title { @apply flex items-center gap-3 border-b border-gray-100 p-5 dark:border-dark-700; }
.icon-tile { @apply flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-950/30 dark:text-primary-400; }
.section-title p, .events-heading p { @apply mt-1 text-xs text-gray-400; }
.settings-form { @apply p-5; }
.enable-row { @apply mb-6 flex items-center justify-between gap-4; }
.enable-row strong, .kind-option strong { @apply block text-sm font-medium; }
.enable-row small, .kind-option small { @apply mt-1 block text-xs leading-relaxed text-gray-400; }
.field-label { @apply mb-2 mt-5 block text-xs font-medium text-gray-600 dark:text-gray-300; }
.field-label .input { @apply mt-2; }
.field-hint { @apply mt-2 text-xs leading-relaxed text-gray-400; }
.kind-option { @apply mb-2 flex items-start gap-3 rounded-xl border border-gray-100 p-3 dark:border-dark-700; }
.kind-option:has(input:checked) { @apply border-primary-200 bg-primary-50/30 dark:border-primary-800 dark:bg-primary-950/20; }
.kind-option input { @apply mt-1 rounded text-primary-600; }
.settings-actions { @apply mt-5 flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700; }
.settings-actions span { @apply text-xs text-gray-400; }
.grid-2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 16px; }
.grid-2 .field-label { @apply mt-0; }

.events-card { @apply flex flex-col; }
.events-heading { @apply flex flex-wrap items-start justify-between gap-3 p-5; }
.events-heading h3 span { @apply ml-2 rounded-md bg-gray-100 px-2 py-0.5 text-xs font-normal tabular-nums text-gray-500 dark:bg-dark-800; }
.failure-badge { @apply whitespace-nowrap rounded-md bg-amber-50 px-2 py-1 text-[11px] font-medium text-amber-700 dark:bg-amber-950/30 dark:text-amber-300; }
.failure-badge.ok { @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300; }
.failure-badge.danger { @apply bg-red-50 text-red-600 dark:bg-red-950/30 dark:text-red-300; }
.managed-hint { @apply mx-5 mb-3 rounded-lg bg-sky-50 px-3 py-2 text-xs leading-relaxed text-sky-800 dark:bg-sky-950/30 dark:text-sky-200; }
.managed-hint.warn { @apply bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300; }
.managed-badge { @apply ml-2 rounded bg-sky-50 px-1.5 py-0.5 text-sky-700 dark:bg-sky-950/40 dark:text-sky-300; }
.link-btn { @apply inline-block rounded-lg border border-gray-200 px-2.5 py-1 text-[11px] font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-40 dark:border-dark-600 dark:hover:bg-dark-800 dark:text-primary-400; }
.empty-state { @apply flex min-h-56 flex-col items-center justify-center gap-3 p-6 text-center text-gray-400; }
.empty-state p { @apply max-w-sm text-xs leading-relaxed; }

.error-banner { @apply mb-4 rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300; }
.success-banner { @apply mb-4 rounded-xl bg-emerald-50 p-3 text-sm text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300; }
button:disabled { @apply cursor-not-allowed opacity-40; }
textarea.input { @apply font-mono text-[11px] leading-5; }
@media(max-width:1100px) { .ops-columns {grid-template-columns:1fr;}  }
</style>
