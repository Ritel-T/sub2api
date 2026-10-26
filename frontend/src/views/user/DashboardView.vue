<template>
  <AppLayout :show-page-title="false">
    <div class="open-page dashboard-overview">
      <header class="flex flex-wrap items-center justify-between gap-4">
        <AppHeading variant="page" as="h1">{{ copy.overview }}</AppHeading>
        <div class="dashboard-page-actions">
          <router-link v-if="!authStore.isSimpleMode && paymentEnabled" to="/purchase" class="btn btn-primary">{{ copy.topUp }}</router-link>
          <button type="button" class="btn btn-secondary" :disabled="refreshing" @click="refreshAll">{{ copy.refresh }}</button>
        </div>
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

      <UserDashboardQuickActions v-if="authStore.user" :recent-keys="recentKeys" :groups="groups" :loading="states.keys.loading" :error="states.keys.error" :groups-loading="states.groups.loading" :groups-error="states.groups.error" @retry="loadKeys" @retry-groups="loadGroups" @created="onKeyCreated" />
      <UserDashboardRecentUsage :data="recentUsage" :errors="recentErrors" :loading="states.recent.loading" :error="states.recent.error" @retry="loadRecent" />

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
import AppLayout from '@/components/layout/AppLayout.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import { useOverviewCopy } from '@/components/user/dashboard/overviewCopy'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import { userGroupsAPI } from '@/api/groups'
import type { ApiKey, Group, UsageLog, UserErrorRequest } from '@/types'

const authStore = useAuthStore()
const appStore = useAppStore()
const paymentEnabled = computed(() => appStore.cachedPublicSettings?.payment_enabled !== false)
const copy = useOverviewCopy()
const stats = ref<UserStatsType | null>(null)
const balance = ref<number | null>(null)
const recentKeys = ref<ApiKey[]>([])
const groups = ref<Group[]>([])
const recentUsage = ref<UsageLog[]>([])
const recentErrors = ref<UserErrorRequest[]>([])
const canViewErrors = computed(() => appStore.cachedPublicSettings?.allow_user_view_error_requests === true)
const sectionNames = ['stats', 'balance', 'keys', 'groups', 'recent'] as const
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
const loadKeys = () => load('keys', () => keysAPI.list(1, 3, { sort_by: 'created_at', sort_order: 'desc' }), data => { recentKeys.value = data.items.slice(0, 3) })
const loadGroups = () => load('groups', userGroupsAPI.getAvailable, data => { groups.value = data })
const loadRecent = () => load('recent', async () => {
  const [usage, errors] = await Promise.all([
    usageAPI.query({ page: 1, page_size: 5, sort_by: 'created_at', sort_order: 'desc' }),
    canViewErrors.value ? usageAPI.listMyErrorRequests({ page: 1, page_size: 5, sort_by: 'created_at', sort_order: 'desc' }) : Promise.resolve({ items: [] as UserErrorRequest[] }),
  ])
  return { usage: usage.items, errors: errors.items }
}, data => { recentUsage.value = data.usage; recentErrors.value = data.errors })
function onKeyCreated(key: ApiKey) {
  // Invalidate an older list response before putting the successfully created key first.
  states.keys.generation++
  states.keys.loading = false
  states.keys.error = false
  recentKeys.value = [key, ...recentKeys.value.filter(item => item.id !== key.id)].slice(0, 3)
  void loadKeys()
}
function refreshAll() {
  if (!authStore.user) return
  void loadStats(); void loadKeys(); void loadGroups(); void loadRecent()
  if (!authStore.isSimpleMode) void loadBalance(); else states.balance.loading = false
}
const refreshing = computed(() => sectionNames.some(name => states[name].loading))
const money = (value: number, decimals = 4) => new Intl.NumberFormat('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals }).format(value)
watch([() => authStore.user?.id, () => Boolean(authStore.token), () => authStore.isSimpleMode], () => {
  ownerGeneration++
  stats.value = null; balance.value = null; recentKeys.value = []; groups.value = []; recentUsage.value = []; recentErrors.value = []
  for (const section of sectionNames) { states[section].loading = true; states[section].error = false }
  refreshAll()
}, { flush: 'sync' })
watch(canViewErrors, () => {
  recentErrors.value = []
  if (authStore.user) void loadRecent()
})
onMounted(refreshAll)
onBeforeUnmount(() => { alive = false; ownerGeneration++ })
</script>

<style scoped>
.dashboard-page-actions{display:flex;align-items:center;gap:.75rem}
.dashboard-summary{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) minmax(0,.7fr);align-items:stretch;gap:2rem;padding:2rem 0 1rem}
.dashboard-summary>div{min-width:0}
.dashboard-metric-label{color:var(--ritel-muted)}
.dashboard-balance,.dashboard-spending{display:flex;flex-direction:column;justify-content:space-between}
.dashboard-balance-value,.dashboard-spending-value{margin-block:1rem 0;line-height:1.1;overflow-wrap:anywhere;font-size:clamp(2.75rem,4.5vw,4.5rem)}
.dashboard-activity{display:grid;grid-template-rows:1fr 1fr;gap:1rem}
.dashboard-activity-value{margin-block:.375rem 0;font-size:1.75rem;line-height:1.1;overflow-wrap:anywhere}
.dashboard-metric-state{margin-block:1.5rem}
.dashboard-summary-simple{grid-template-columns:minmax(0,1.5fr) minmax(0,1fr)}
@media(max-width:767px){.dashboard-summary{grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:2rem 1.5rem;padding-block:1rem 2rem}.dashboard-balance-value,.dashboard-spending-value{font-size:clamp(1.75rem,8vw,2.5rem)}.dashboard-activity{grid-column:1/-1;grid-template-columns:1fr 1fr;grid-template-rows:auto;gap:1.5rem}}
</style>
