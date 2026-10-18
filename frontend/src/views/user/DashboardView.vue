<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-wrap items-center justify-between gap-4">
        <h1 class="text-3xl font-semibold">{{ copy.overview }}</h1>
        <div class="flex flex-wrap items-center gap-3">
          <router-link to="/usage?view=analysis" class="btn btn-secondary">{{ copy.analysis }}</router-link>
          <button type="button" class="btn btn-secondary" :disabled="refreshing" @click="refreshAll">{{ copy.refresh }}</button>
        </div>
      </header>

      <UserDashboardQuickActions :active-key="activeKey" :total-keys="totalKeys" :loading="states.keys.loading" :error="states.keys.error" @retry="loadKeys" />

      <section v-if="hasAttention" class="card p-6" aria-labelledby="attention-title">
        <h2 id="attention-title" class="mb-4 text-xl font-semibold">{{ copy.attention }}</h2>
        <ul class="divide-y divide-gray-200 dark:divide-dark-700">
          <li v-if="!states.errors.error && !states.errors.loading && errorCount !== null && errorCount > 0" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <span>{{ copy.errors }} <strong class="tabular-nums">{{ errorCount.toLocaleString() }}</strong></span>
            <router-link :to="{ path: '/usage', query: { view: 'errors', start_date: today, end_date: today } }" class="underline underline-offset-4">{{ copy.viewErrors }}</router-link>
          </li>
          <li v-if="noCredit" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <span>{{ copy.noCredit }}</span><router-link v-if="paymentEnabled" to="/purchase" class="underline underline-offset-4">{{ copy.billing }}</router-link>
          </li>
          <li v-for="subscription in endingSubscriptions" :key="subscription.id" class="flex flex-wrap items-center justify-between gap-3 py-3">
            <span>{{ copy.expires }} · {{ subscription.group_name }}<span v-if="subscription.expires_at"> · {{ formatDate(subscription.expires_at) }}</span></span>
            <router-link to="/subscriptions" class="underline underline-offset-4">{{ copy.subscriptions }}</router-link>
          </li>
        </ul>
      </section>

      <div v-if="!authStore.isSimpleMode" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <section class="card p-6">
          <h2 class="text-base text-gray-500 dark:text-gray-400">{{ copy.balance }}</h2>
          <p v-if="states.balance.loading" class="mt-3" role="status">{{ copy.loading }}</p>
          <div v-else-if="states.balance.error" class="mt-3" role="alert"><p>{{ copy.loadFailed }}</p><button type="button" class="btn btn-secondary mt-3" @click="loadBalance">{{ copy.retry }}</button></div>
          <p v-else-if="balance !== null" class="ritel-number-display mt-3 text-4xl">${{ money(balance, 2) }}</p>
          <router-link v-if="paymentEnabled" to="/purchase" class="mt-4 inline-block underline underline-offset-4">{{ copy.billing }}</router-link>
        </section>
        <section v-if="subscriptionsEnabled" class="card p-6">
          <h2 class="text-base text-gray-500 dark:text-gray-400">{{ copy.subscriptions }}</h2>
          <p v-if="states.subscriptions.loading" class="mt-3" role="status">{{ copy.loading }}</p>
          <div v-else-if="states.subscriptions.error" class="mt-3" role="alert"><p>{{ copy.loadFailed }}</p><button type="button" class="btn btn-secondary mt-3" @click="loadSubscriptions">{{ copy.retry }}</button></div>
          <p v-else-if="subscriptionSummary" class="ritel-number-display mt-3 text-4xl">{{ subscriptionSummary.active_count }}</p>
          <router-link to="/subscriptions" class="mt-4 inline-block underline underline-offset-4">{{ copy.subscriptions }}</router-link>
        </section>
      </div>

      <section v-if="states.stats.loading && !stats" class="card p-6" role="status">{{ copy.loading }}</section>
      <section v-else-if="states.stats.error" class="card flex flex-wrap items-center gap-3 p-6" role="alert"><span>{{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="loadStats">{{ copy.retry }}</button></section>
      <section v-else-if="stats" class="grid grid-cols-2 gap-4 lg:grid-cols-4" :aria-label="copy.fullStats">
        <div v-for="metric in metrics" :key="metric.label" class="card p-5"><h2 class="text-base text-gray-500 dark:text-gray-400">{{ metric.label }}</h2><p class="ritel-number-display mt-3 break-words text-3xl">{{ metric.value }}</p></div>
      </section>

      <div v-if="states.errors.error" class="card flex flex-wrap items-center gap-3 p-4" role="alert"><span>{{ copy.errors }}: {{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="loadErrors">{{ copy.retry }}</button></div>
      <UserDashboardRecentUsage :data="recentUsage" :loading="states.recent.loading" :error="states.recent.error" @retry="loadRecent" />

      <details class="card p-6" @toggle="onDetailsToggle">
        <summary class="cursor-pointer text-lg font-semibold">{{ copy.fullStats }}</summary>
        <div class="mt-6 space-y-6">
          <UserDashboardStats v-if="stats" :stats="stats" :balance="balance ?? undefined" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
          <p v-if="states.quotas.loading" role="status">{{ copy.loading }}</p>
          <div v-else-if="states.quotas.error" role="alert"><span>{{ copy.quotaFailed }}</span><button type="button" class="btn btn-secondary ml-3" @click="loadPlatformQuotas">{{ copy.retry }}</button></div>
          <div v-if="states.charts.error" role="alert"><span>{{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary ml-3" @click="loadCharts">{{ copy.retry }}</button></div>
          <UserDashboardCharts v-else v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="states.charts.loading" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="loadCharts" />
        </div>
      </details>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import { keysAPI } from '@/api/keys'
import { getSubscriptionSummary, type SubscriptionSummary } from '@/api/subscriptions'
import { getMyPlatformQuotas } from '@/api/user'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import { useOverviewCopy } from '@/components/user/dashboard/overviewCopy'
import { selectOverviewKey } from '@/components/user/dashboard/overviewState'
import type { ApiKey, UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { formatDateLocalInput } from '@/utils/format'

const authStore = useAuthStore()
const appStore = useAppStore()
const subscriptionsEnabled = computed(() => !authStore.isSimpleMode && appStore.cachedPublicSettings?.subscription_enabled !== false)
const paymentEnabled = computed(() => appStore.cachedPublicSettings?.payment_enabled !== false)
const copy = useOverviewCopy()
const stats = ref<UserStatsType | null>(null)
const balance = ref<number | null>(null)
const activeKey = ref<ApiKey | null>(null)
const totalKeys = ref<number | null>(null)
const subscriptionSummary = ref<SubscriptionSummary | null>(null)
const errorCount = ref<number | null>(null)
const recentUsage = ref<UsageLog[]>([])
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)
const today = ref(formatDateLocalInput(new Date()))
const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(today.value)
const granularity = ref('day')
const expanded = ref(false)
const sectionNames = ['stats', 'balance', 'keys', 'subscriptions', 'errors', 'recent', 'charts', 'quotas'] as const
type Section = typeof sectionNames[number]
const states = reactive(Object.fromEntries(sectionNames.map(name => [name, { loading: true, error: false, generation: 0 }])) as Record<Section, { loading: boolean; error: boolean; generation: number }>)
let alive = true
let ownerGeneration = 0

async function load<T>(section: Section, fetch: () => Promise<T>, accept: (data: T) => void) {
  const state = states[section]
  const generation = ++state.generation
  const owner = ownerGeneration
  state.loading = true
  state.error = false
  const current = () => alive && owner === ownerGeneration && generation === state.generation
  try {
    const data = await fetch()
    if (current()) accept(data)
  } catch {
    if (current()) state.error = true
  } finally {
    if (current()) state.loading = false
  }
}
const loadStats = () => load('stats', () => usageAPI.getDashboardStats(), data => { stats.value = data })
const loadBalance = () => load('balance', async () => {
  const data = await authStore.refreshUserIfCurrent(authStore.user!.id)
  if (!data) throw new Error('User refresh did not complete')
  return data.balance
}, data => { balance.value = data })
const loadKeys = () => load('keys', async () => {
  const all = await keysAPI.list(1, 1)
  if (all.total === 0) return { active: null, total: 0 }
  let page = 1
  let unusedKey: ApiKey | null = null
  while (true) {
    const active = await keysAPI.list(page, 50, { status: 'active', sort_by: 'last_used_at', sort_order: 'desc' })
    const key = selectOverviewKey(active.items)
    if (key?.last_used_at) return { active: key, total: all.total }
    unusedKey ??= key
    if (active.items.length === 0 || page * 50 >= active.total) return { active: unusedKey, total: all.total }
    page++
  }
}, data => { activeKey.value = data.active; totalKeys.value = data.total })
const loadSubscriptions = () => load('subscriptions', getSubscriptionSummary, data => { subscriptionSummary.value = data })
const loadRecent = () => load('recent', () => usageAPI.query({ page: 1, page_size: 5, sort_by: 'created_at', sort_order: 'desc' }), data => { recentUsage.value = data.items })
const loadErrors = () => load('errors', () => usageAPI.listMyErrorRequests({ page: 1, page_size: 1, start_date: today.value, end_date: today.value, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }), data => { errorCount.value = data.total })
const loadPlatformQuotas = () => load('quotas', getMyPlatformQuotas, data => { platformQuotas.value = data.platform_quotas ?? [] })
const loadCharts = () => load('charts', () => Promise.all([
  usageAPI.getDashboardTrend({ start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as 'day' | 'hour' }),
  usageAPI.getDashboardModels({ start_date: startDate.value, end_date: endDate.value }),
]), data => { trendData.value = data[0].trend ?? []; modelStats.value = data[1].models ?? [] })

function refreshAll() {
  if (!authStore.user) return
  today.value = formatDateLocalInput(new Date())
  void loadStats(); void loadKeys(); void loadRecent(); void loadErrors()
  if (!authStore.isSimpleMode) void loadBalance(); else states.balance.loading = false
  if (subscriptionsEnabled.value) void loadSubscriptions(); else states.subscriptions.loading = false
  if (expanded.value) { void loadCharts(); void loadPlatformQuotas() }
}
function onDetailsToggle(event: Event) {
  const open = (event.target as HTMLDetailsElement).open
  if (expanded.value === open) return
  expanded.value = open
  if (expanded.value) { void loadCharts(); if (!authStore.isSimpleMode) void loadPlatformQuotas(); else states.quotas.loading = false }
}
const refreshing = computed(() => sectionNames.filter(name => name !== 'charts' && name !== 'quotas').some(name => states[name].loading))
const endingSubscriptions = computed(() => !subscriptionsEnabled.value || states.subscriptions.error || states.subscriptions.loading ? [] : subscriptionSummary.value?.subscriptions.filter(subscription => subscription.status === 'active' && subscription.days_remaining !== null && subscription.days_remaining >= 0 && subscription.days_remaining <= 3) ?? [])
const noCredit = computed(() => !authStore.isSimpleMode && !states.balance.error && !states.subscriptions.error && !states.balance.loading && !states.subscriptions.loading && balance.value !== null && balance.value <= 0 && subscriptionSummary.value?.active_count === 0)
const hasAttention = computed(() => (!states.errors.error && !states.errors.loading && (errorCount.value ?? 0) > 0) || noCredit.value || endingSubscriptions.value.length > 0)
const money = (value: number, decimals = 4) => new Intl.NumberFormat('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals }).format(value)
const formatDate = (value: string) => new Date(value).toLocaleDateString()
const metrics = computed(() => !stats.value ? [] : [
  { label: copy.value.requests, value: stats.value.today_requests.toLocaleString() },
  { label: copy.value.tokens, value: stats.value.today_tokens.toLocaleString() },
  { label: copy.value.cost, value: `$${money(stats.value.today_actual_cost)}` },
  { label: copy.value.keys, value: stats.value.active_api_keys.toLocaleString() },
])
watch(subscriptionsEnabled, enabled => {
  if (enabled && authStore.user) void loadSubscriptions()
  else { states.subscriptions.generation++; states.subscriptions.loading = false; states.subscriptions.error = false; subscriptionSummary.value = null }
})
watch([() => authStore.user?.id, () => authStore.token], () => {
  ownerGeneration++
  stats.value = null; balance.value = null; activeKey.value = null; totalKeys.value = null
  subscriptionSummary.value = null; errorCount.value = null; recentUsage.value = []
  trendData.value = []; modelStats.value = []; platformQuotas.value = null
  for (const section of sectionNames) { states[section].loading = true; states[section].error = false }
  refreshAll()
}, { flush: 'sync' })
onMounted(refreshAll)
onBeforeUnmount(() => { alive = false; ownerGeneration++ })
</script>
