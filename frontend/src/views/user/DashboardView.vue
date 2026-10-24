<template>
  <AppLayout :show-page-title="false">
    <div class="open-page dashboard-overview">
      <header class="flex flex-wrap items-center justify-between gap-4">
        <AppHeading variant="page" as="h1">{{ copy.overview }}</AppHeading>
        <button type="button" class="btn btn-secondary" :disabled="refreshing" @click="refreshAll">{{ copy.refresh }}</button>
      </header>

      <section class="dashboard-summary" :class="{ 'dashboard-summary-simple': authStore.isSimpleMode }" aria-labelledby="overview-summary-title">
        <h2 id="overview-summary-title" class="sr-only">{{ copy.overview }}</h2>
        <div v-if="!authStore.isSimpleMode" class="dashboard-balance" data-test="balance">
          <AppHeading variant="subtle" as="h3" class="dashboard-metric-label">{{ copy.balance }}</AppHeading>
          <p v-if="states.balance.loading" class="dashboard-metric-state" role="status">{{ copy.loading }}</p>
          <div v-else-if="states.balance.error" class="dashboard-metric-state" role="alert"><p>{{ copy.loadFailed }}</p><button type="button" class="btn btn-secondary mt-3" @click="loadBalance">{{ copy.retry }}</button></div>
          <p v-else-if="balance !== null" class="ritel-number-display dashboard-balance-value">${{ money(balance, 2) }}</p>
        </div>
        <div class="dashboard-spending" data-test="spending">
          <AppHeading variant="subtle" as="h3" class="dashboard-metric-label">{{ copy.cost }}</AppHeading>
          <p v-if="states.stats.loading && !stats" class="dashboard-metric-state" role="status">{{ copy.loading }}</p>
          <div v-else-if="states.stats.error" class="dashboard-metric-state" role="alert"><p>{{ copy.loadFailed }}</p><button type="button" class="btn btn-secondary mt-3" @click="loadStats">{{ copy.retry }}</button></div>
          <p v-else-if="stats" class="ritel-number-display dashboard-spending-value">${{ money(stats.today_actual_cost) }}</p>
        </div>
        <div class="dashboard-activity">
          <div data-test="requests">
            <AppHeading variant="subtle" as="h3" class="dashboard-metric-label">{{ copy.requests }}</AppHeading>
            <p class="ritel-number-display dashboard-activity-value">{{ !states.stats.error && stats ? stats.today_requests.toLocaleString() : '—' }}</p>
          </div>
          <div data-test="tokens">
            <AppHeading variant="subtle" as="h3" class="dashboard-metric-label">{{ copy.tokens }}</AppHeading>
            <p class="ritel-number-display dashboard-activity-value">{{ !states.stats.error && stats ? stats.today_tokens.toLocaleString() : '—' }}</p>
          </div>
        </div>
      </section>

      <UserDashboardQuickActions :active-key="activeKey" :total-keys="totalKeys" :loading="states.keys.loading" :error="states.keys.error" @retry="loadKeys" />

      <section v-if="hasAttention" class="open-section dashboard-attention" aria-labelledby="attention-title">
        <AppHeading variant="section" as="h2" id="attention-title">{{ copy.attention }}</AppHeading>
        <ul class="divide-y divide-gray-200 dark:divide-dark-700">
          <li v-if="!states.errors.error && !states.errors.loading && errorCount !== null && errorCount > 0" class="dashboard-attention-item">
            <span>{{ copy.errors }} <strong class="tabular-nums">{{ errorCount.toLocaleString() }}</strong></span>
            <router-link :to="{ path: '/usage', query: { view: 'errors', start_date: today, end_date: today } }" class="underline underline-offset-4">{{ copy.viewErrors }}</router-link>
          </li>
          <li v-if="noCredit" class="dashboard-attention-item">
            <span>{{ copy.noCredit }}</span><router-link v-if="paymentEnabled" to="/purchase" class="underline underline-offset-4">{{ copy.billing }}</router-link>
          </li>
          <li v-for="subscription in endingSubscriptions" :key="subscription.id" class="dashboard-attention-item">
            <span>{{ copy.expires }} · {{ subscription.group_name }}<span v-if="subscription.expires_at"> · {{ formatDate(subscription.expires_at) }}</span></span>
            <router-link to="/subscriptions" class="underline underline-offset-4">{{ copy.subscriptions }}</router-link>
          </li>
        </ul>
      </section>
      <div v-if="states.errors.error" class="dashboard-load-error" role="alert"><span>{{ copy.errors }}: {{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="loadErrors">{{ copy.retry }}</button></div>
      <div v-if="subscriptionsEnabled && states.subscriptions.error" class="dashboard-load-error" role="alert"><span>{{ copy.subscriptions }}: {{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="loadSubscriptions">{{ copy.retry }}</button></div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import { keysAPI } from '@/api/keys'
import { getSubscriptionSummary, type SubscriptionSummary } from '@/api/subscriptions'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import { useOverviewCopy } from '@/components/user/dashboard/overviewCopy'
import { selectOverviewKey } from '@/components/user/dashboard/overviewState'
import type { ApiKey } from '@/types'
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
const today = ref(formatDateLocalInput(new Date()))
const sectionNames = ['stats', 'balance', 'keys', 'subscriptions', 'errors'] as const
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
const loadErrors = () => load('errors', () => usageAPI.listMyErrorRequests({ page: 1, page_size: 1, start_date: today.value, end_date: today.value, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }), data => { errorCount.value = data.total })
function refreshAll() {
  if (!authStore.user) return
  today.value = formatDateLocalInput(new Date())
  void loadStats(); void loadKeys(); void loadErrors()
  if (!authStore.isSimpleMode) void loadBalance(); else states.balance.loading = false
  if (subscriptionsEnabled.value) void loadSubscriptions(); else states.subscriptions.loading = false
}
const refreshing = computed(() => sectionNames.some(name => states[name].loading))
const endingSubscriptions = computed(() => !subscriptionsEnabled.value || states.subscriptions.error || states.subscriptions.loading ? [] : subscriptionSummary.value?.subscriptions.filter(subscription => subscription.status === 'active' && subscription.days_remaining !== null && subscription.days_remaining >= 0 && subscription.days_remaining <= 3) ?? [])
const noCredit = computed(() => !authStore.isSimpleMode && !states.balance.error && !states.subscriptions.error && !states.balance.loading && !states.subscriptions.loading && balance.value !== null && balance.value <= 0 && subscriptionSummary.value?.active_count === 0)
const hasAttention = computed(() => (!states.errors.error && !states.errors.loading && (errorCount.value ?? 0) > 0) || noCredit.value || endingSubscriptions.value.length > 0)
const money = (value: number, decimals = 4) => new Intl.NumberFormat('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals }).format(value)
const formatDate = (value: string) => new Date(value).toLocaleDateString()
watch(subscriptionsEnabled, enabled => {
  if (enabled && authStore.user) void loadSubscriptions()
  else { states.subscriptions.generation++; states.subscriptions.loading = false; states.subscriptions.error = false; subscriptionSummary.value = null }
})
watch([() => authStore.user?.id, () => authStore.token, () => authStore.isSimpleMode], () => {
  ownerGeneration++
  stats.value = null; balance.value = null; activeKey.value = null; totalKeys.value = null
  subscriptionSummary.value = null; errorCount.value = null
  for (const section of sectionNames) { states[section].loading = true; states[section].error = false }
  refreshAll()
}, { flush: 'sync' })
onMounted(refreshAll)
onBeforeUnmount(() => { alive = false; ownerGeneration++ })
</script>

<style scoped>
.dashboard-summary{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) minmax(0,.7fr);align-items:stretch;gap:2rem;padding:2rem 0 1rem}
.dashboard-summary>div{min-width:0}
.dashboard-metric-label{color:var(--ritel-muted)}
.dashboard-balance,.dashboard-spending{display:flex;flex-direction:column;justify-content:space-between}
.dashboard-balance-value,.dashboard-spending-value{margin-block:1rem 0;line-height:1.1;overflow-wrap:anywhere;font-size:clamp(2.75rem,4.5vw,4.5rem)}
.dashboard-activity{display:grid;grid-template-rows:1fr 1fr;gap:1rem}
.dashboard-activity-value{margin-block:.375rem 0;font-size:1.75rem;line-height:1.1;overflow-wrap:anywhere}
.dashboard-metric-state{margin-block:1.5rem}
.dashboard-attention{display:grid;grid-template-columns:minmax(9rem,.4fr) minmax(0,1fr);gap:2rem;padding-block:1.5rem}
.dashboard-attention-item{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.75rem;padding-block:.75rem}
.dashboard-attention-item:first-child{padding-block-start:0}
.dashboard-load-error{display:flex;flex-wrap:wrap;align-items:center;gap:.75rem}
.dashboard-summary-simple{grid-template-columns:minmax(0,1.5fr) minmax(0,1fr)}
@media(max-width:767px){.dashboard-summary{grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:2rem 1.5rem;padding-block:1rem 2rem}.dashboard-balance-value,.dashboard-spending-value{font-size:clamp(1.75rem,8vw,2.5rem)}.dashboard-activity{grid-column:1/-1;grid-template-columns:1fr 1fr;grid-template-rows:auto;gap:1.5rem}.dashboard-attention{grid-template-columns:minmax(0,1fr);gap:1rem}}
</style>
