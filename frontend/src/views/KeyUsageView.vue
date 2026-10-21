<template>
  <div class="relative flex min-h-screen flex-col bg-gray-50 dark:bg-dark-950">
    <!-- Header (same pattern as HomeView) -->
    <header class="relative z-20 px-6 py-4">
      <nav class="mx-auto flex max-w-6xl items-center justify-between">
        <router-link to="/home" class="flex items-center gap-3">
          <div class="h-10 w-10">
            <img :src="siteLogo" :alt="siteName" class="ritel-logo h-full w-full object-contain" />
          </div>
          <span class="text-lg font-semibold tracking-tight text-gray-900 dark:text-white">{{ siteName }}</span>
        </router-link>
        <div class="flex items-center gap-3">
          <ThemeSwitcher />
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
        </div>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="key-usage-workspace flex-1 w-full max-w-[1520px] mx-auto px-5 py-12 sm:px-8">
      <!-- Hero -->
      <div class="text-center mb-12">
        <h1 class="text-3xl sm:text-4xl font-bold tracking-tight mb-3 text-gray-900 dark:text-white">
          {{ t('keyUsage.title') }}
        </h1>
        <p class="text-gray-500 dark:text-dark-400 text-base max-w-md mx-auto">
          {{ t('keyUsage.subtitle') }}
        </p>
      </div>

      <!-- Input Section -->
      <div class="max-w-xl mx-auto mb-14">
        <div class="flex gap-3">
          <div class="flex-1 relative">
            <div class="absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500">
              <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>
              </svg>
            </div>
            <input
              v-model="apiKey"
              :type="keyVisible ? 'text' : 'password'"
              :placeholder="t('keyUsage.placeholder')"
              class="input-ring w-full h-12 pl-12 pr-12 rounded-xl border border-gray-200 bg-white text-sm text-gray-900 placeholder:text-gray-400 transition-all dark:border-dark-700 dark:bg-dark-900 dark:text-white dark:placeholder:text-dark-500"
              @keydown.enter="queryKey"
            />
            <button
              @click="keyVisible = !keyVisible"
              class="absolute right-4 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-700 dark:text-dark-500 dark:hover:text-white transition-colors"
            >
              <svg v-if="!keyVisible" class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                <line x1="1" y1="1" x2="23" y2="23"/>
              </svg>
              <svg v-else class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>
              </svg>
            </button>
          </div>
          <button
            @click="queryKey"
            :disabled="isQuerying"
            class="h-12 px-7 rounded-xl bg-primary-500 hover:bg-primary-600 text-white font-medium text-sm transition-all active:scale-[0.97] flex items-center gap-2 whitespace-nowrap disabled:opacity-60"
          >
            <svg v-if="isQuerying" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none">
              <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" opacity="0.25"/>
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" stroke-width="3" stroke-linecap="round"/>
            </svg>
            <svg v-else class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
            {{ isQuerying ? t('keyUsage.querying') : t('keyUsage.query') }}
          </button>
        </div>

        <!-- Date Range Picker -->
        <div v-if="showDatePicker" class="mt-4">
          <div class="flex flex-wrap items-center gap-2 justify-center">
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('keyUsage.dateRange') }}</span>
            <button
              v-for="range in dateRanges"
              :key="range.key"
              @click="setDateRange(range.key)"
              class="text-xs px-3 py-1.5 rounded-lg border transition-all"
              :class="currentRange === range.key
                ? 'bg-primary-500 text-white border-primary-500'
                : 'border-gray-200 bg-white text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200 hover:border-primary-300 dark:hover:border-dark-600'"
            >{{ range.label }}</button>
            <div v-if="currentRange === 'custom'" class="flex items-center gap-2 ml-1">
              <input
                v-model="customStartDate"
                type="date"
                class="input-ring text-xs px-2 py-1.5 rounded-lg border border-gray-200 bg-white text-gray-900 dark:border-dark-700 dark:bg-dark-900 dark:text-white"
              />
              <span class="text-xs text-gray-400">-</span>
              <input
                v-model="customEndDate"
                type="date"
                class="input-ring text-xs px-2 py-1.5 rounded-lg border border-gray-200 bg-white text-gray-900 dark:border-dark-700 dark:bg-dark-900 dark:text-white"
              />
              <button
                @click="queryKey"
                class="text-xs px-3 py-1.5 rounded-lg bg-primary-500 text-white hover:bg-primary-600"
              >{{ t('keyUsage.apply') }}</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Results Container -->
      <div v-if="showResults">
        <!-- Loading Skeleton -->
        <div v-if="showLoading" class="open-page">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div class="open-section py-8">
              <div class="skeleton h-5 w-24 mb-6"></div>
              <div class="flex justify-center"><div class="skeleton w-44 h-44 rounded-full"></div></div>
            </div>
            <div class="open-section py-8">
              <div class="skeleton h-5 w-24 mb-6"></div>
              <div class="flex justify-center"><div class="skeleton w-44 h-44 rounded-full"></div></div>
            </div>
          </div>
          <div class="open-section py-8">
            <div class="skeleton h-5 w-32 mb-6"></div>
            <div class="space-y-4">
              <div class="skeleton h-4 w-full"></div>
              <div class="skeleton h-4 w-3/4"></div>
              <div class="skeleton h-4 w-5/6"></div>
              <div class="skeleton h-4 w-2/3"></div>
            </div>
          </div>
        </div>

        <!-- Result Content -->
        <div v-else-if="resultData" class="open-page">
          <!-- Status Badge -->
          <div v-if="statusInfo" class="flex items-center justify-center mb-2">
            <div class="inline-flex flex-wrap items-center gap-3 py-3">
              <span
                class="w-2.5 h-2.5 rounded-full"
                :class="statusInfo.isActive ? 'bg-emerald-500' : 'bg-rose-500'"
              ></span>
              <span class="text-sm font-medium text-gray-900 dark:text-white">{{ statusInfo.label }}</span>
              <span class="text-xs text-gray-400 dark:text-dark-500">|</span>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ statusInfo.statusText }}</span>
            </div>
          </div>

          <section v-if="ringItems.length" class="open-metric-strip key-usage-quotas">
            <div v-for="(ring, i) in ringItems" :key="ring.title" class="key-usage-quota">
              <h2 class="text-sm text-gray-500 dark:text-dark-400">{{ ring.title }}</h2>
              <p class="ritel-number-display mt-3 text-3xl">{{ ring.amount }}</p>
              <template v-if="!ring.isBalance">
                <div class="key-usage-meter" role="meter" :aria-label="ring.title" :aria-valuenow="ring.pct" aria-valuemin="0" aria-valuemax="100">
                  <span :style="{ width: `${displayPcts[i] ?? ring.pct}%` }"></span>
                </div>
                <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ displayPcts[i] ?? ring.pct }}% {{ t('keyUsage.used') }}</p>
                <p v-if="ring.resetAt && formatResetTime(ring.resetAt)" class="mt-2 text-sm tabular-nums text-gray-500 dark:text-gray-400">{{ formatResetTime(ring.resetAt) }}</p>
              </template>
            </div>
          </section>

          <div class="key-usage-context">
            <!-- Account details -->
            <div
              v-if="detailRows.length > 0"
              class="open-section"
            >
              <div class="pb-5">
                <h3 class="text-xl font-medium text-gray-900 dark:text-white">{{ t('keyUsage.detailInfo') }}</h3>
              </div>
              <div class="divide-y divide-gray-100 dark:divide-dark-800">
                <div
                  v-for="(row, i) in detailRows"
                  :key="i"
                  class="py-4 flex flex-wrap items-center justify-between gap-4"
                >
                  <div class="flex items-center gap-3">
                    <div class="w-8 h-8 rounded-lg flex items-center justify-center" :class="row.iconBg">
                      <svg
                        class="w-4 h-4"
                        :class="row.iconColor"
                        viewBox="0 0 24 24" fill="none" stroke="currentColor"
                        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                        v-html="row.iconSvg"
                      ></svg>
                    </div>
                    <span class="text-sm text-gray-700 dark:text-dark-200">{{ row.label }}</span>
                  </div>
                  <span class="text-sm font-semibold tabular-nums" :class="row.valueClass || 'text-gray-900 dark:text-white'">
                    {{ row.value }}
                  </span>
                </div>
              </div>
            </div>

            <!-- Token statistics -->
            <div
              v-if="usageStatCells.length > 0"
              class="open-section"
            >
              <div class="pb-5">
                <h3 class="text-xl font-medium text-gray-900 dark:text-white">{{ t('keyUsage.tokenStats') }}</h3>
              </div>
              <div class="key-usage-statistics">
                <div
                  v-for="(cell, i) in usageStatCells"
                  :key="i"
                  class="py-3"
                >
                  <div class="text-xs text-gray-500 dark:text-dark-400 mb-1">{{ cell.label }}</div>
                  <div class="text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ cell.value }}</div>
                </div>
              </div>
            </div>

          </div>

          <!-- Daily Usage Table -->
          <div
            v-if="showDailyUsage"
            class="open-section"
          >
            <div class="flex flex-col gap-3 pb-5 sm:flex-row sm:items-center sm:justify-between">
              <h3 class="text-xl font-medium text-gray-900 dark:text-white">{{ t('keyUsage.dailyDetail') }}</h3>
              <div class="inline-flex rounded-lg border border-gray-200 bg-white p-0.5 dark:border-dark-700 dark:bg-dark-950">
                <button
                  v-for="option in dailyUsageOptions"
                  :key="option.value"
                  @click="setDailyUsageDays(option.value)"
                  class="min-w-12 rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                  :class="dailyUsageDays === option.value
                    ? 'bg-primary-500 text-white'
                    : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800'"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <DataTable v-if="dailyUsageRows.length" :data="dailyUsageRows" :columns="dailyColumns" :primary-columns="['date', 'requests', 'input_tokens', 'output_tokens', 'cost']" row-key="date" column-order-key="public-key-usage-daily:v1">
              <template #cell-requests="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-input_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-output_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-cache_read_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-cache_write_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-cost="{ row }">{{ usd(row.actual_cost != null ? row.actual_cost : row.cost) }}</template>
            </DataTable>
            <div v-else class="px-8 py-8 text-center text-sm text-gray-500 dark:text-dark-400">
              {{ t('keyUsage.noDailyUsage') }}
            </div>
          </div>

          <!-- Model Stats Table -->
          <div
            v-if="modelStats.length > 0"
            class="open-section"
          >
            <div class="pb-5">
              <h3 class="text-xl font-medium text-gray-900 dark:text-white">{{ t('keyUsage.modelStats') }}</h3>
            </div>
            <DataTable :data="modelStats" :columns="modelColumns" :primary-columns="['model', 'requests', 'total_tokens', 'cost']" row-key="model" column-order-key="public-key-usage-models:v1">
              <template #cell-model="{ value }">{{ value || '-' }}</template>
              <template #cell-requests="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-input_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-output_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-cache_creation_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-cache_read_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-total_tokens="{ value }">{{ fmtNum(value) }}</template>
              <template #cell-cost="{ row }">{{ usd(row.actual_cost != null ? row.actual_cost : row.cost) }}</template>
            </DataTable>
          </div>
        </div>
      </div>
    </main>

    <!-- Footer (same pattern as HomeView) -->
    <footer class="relative z-10 border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50">
      <div class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left">
        <p class="text-sm text-gray-500 dark:text-dark-400">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex items-center gap-4">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >{{ t('home.docs') }}</a>
          <a
            :href="githubUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >GitHub</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import { buildGatewayUrl } from '@/api/client'
import { formatDateLocalInput } from '@/utils/format'
import { sanitizeUrl } from '@/utils/url'
import { BRAND_NAME, BRAND_LOGO } from '@/utils/branding'

const { t, locale } = useI18n()
const appStore = useAppStore()
const subscriptionFeatureEnabled = computed(() => resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription))

// ==================== Site Settings (same as HomeView) ====================

const siteName = BRAND_NAME
const siteLogo = BRAND_LOGO
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

const currentYear = computed(() => new Date().getFullYear())

const dailyColumns = computed(() => [
  { key: 'date', label: t('keyUsage.date') },
  { key: 'requests', label: t('keyUsage.requests') },
  { key: 'input_tokens', label: t('keyUsage.inputTokens') },
  { key: 'output_tokens', label: t('keyUsage.outputTokens') },
  { key: 'cost', label: t('keyUsage.cost') },
  { key: 'cache_read_tokens', label: t('keyUsage.cacheReadTokens') },
  { key: 'cache_write_tokens', label: t('keyUsage.cacheWriteTokens') },
])
const modelColumns = computed(() => [
  { key: 'model', label: t('keyUsage.model') },
  { key: 'requests', label: t('keyUsage.requests') },
  { key: 'total_tokens', label: t('keyUsage.totalTokens') },
  { key: 'cost', label: t('keyUsage.cost') },
  { key: 'input_tokens', label: t('keyUsage.inputTokens') },
  { key: 'output_tokens', label: t('keyUsage.outputTokens') },
  { key: 'cache_creation_tokens', label: t('keyUsage.cacheCreationTokens') },
  { key: 'cache_read_tokens', label: t('keyUsage.cacheReadTokens') },
])

// ==================== Key Query State ====================

const apiKey = ref('')
const keyVisible = ref(false)
const isQuerying = ref(false)
const showResults = ref(false)
const showLoading = ref(false)
const showDatePicker = ref(false)
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const resultData = ref<any>(null)
const now = ref(new Date())
let resetTimer: ReturnType<typeof setInterval> | null = null

// ==================== Date Range State ====================

type DateRangeKey = 'today' | '7d' | '30d' | 'custom'
const currentRange = ref<DateRangeKey>('today')
const customStartDate = ref('')
const customEndDate = ref('')
const dailyUsageDays = ref<7 | 30 | 90>(30)

const dateRanges = computed(() => [
  { key: 'today' as const, label: t('keyUsage.dateRangeToday') },
  { key: '7d' as const, label: t('keyUsage.dateRange7d') },
  { key: '30d' as const, label: t('keyUsage.dateRange30d') },
  { key: 'custom' as const, label: t('keyUsage.dateRangeCustom') },
])

const dailyUsageOptions = computed(() => [
  { value: 7 as const, label: t('keyUsage.dateRange7d') },
  { value: 30 as const, label: t('keyUsage.dateRange30d') },
  { value: 90 as const, label: t('keyUsage.dateRange90d') },
])

function setDateRange(key: DateRangeKey) {
  currentRange.value = key
  if (key !== 'custom') {
    queryKey()
  }
}

function getDateParams(): string {
  const now = new Date()
  const params = new URLSearchParams()

  if (currentRange.value === 'custom') {
    if (customStartDate.value && customEndDate.value) {
      params.set('start_date', customStartDate.value)
      params.set('end_date', customEndDate.value)
    }
  } else {
    const end = formatDateLocalInput(now)
    let start: string
    switch (currentRange.value) {
      case 'today': start = end; break
      case '7d': start = formatDateLocalInput(new Date(now.getTime() - 7 * 86400000)); break
      case '30d': start = formatDateLocalInput(new Date(now.getTime() - 30 * 86400000)); break
      default: start = formatDateLocalInput(new Date(now.getTime() - 30 * 86400000))
    }
    params.set('start_date', start)
    params.set('end_date', end)
  }
  params.set('days', String(dailyUsageDays.value))
  params.set('timezone', getBrowserTimezone())
  return params.toString()
}

function setDailyUsageDays(days: 7 | 30 | 90) {
  if (dailyUsageDays.value === days) return
  dailyUsageDays.value = days
  if (resultData.value && apiKey.value.trim()) {
    queryKey()
  }
}

// ==================== Ring Animation ====================


const displayPcts = ref<number[]>([])
let ringFrame: number | undefined
let ringTimer: ReturnType<typeof setTimeout> | undefined
let ringGeneration = 0
let viewDisposed = false

function cancelRingAnimation() {
  ringGeneration++
  if (ringFrame !== undefined) cancelAnimationFrame(ringFrame)
  if (ringTimer !== undefined) clearTimeout(ringTimer)
  ringFrame = undefined
  ringTimer = undefined
}

interface RingItem {
  title: string
  pct: number
  amount: string
  isBalance?: boolean
  iconType: 'clock' | 'calendar' | 'dollar'
  resetAt?: string | null
}

function triggerRingAnimation(items: RingItem[]) {
  if (viewDisposed) return
  cancelRingAnimation()
  const generation = ringGeneration
  displayPcts.value = items.map(() => 0)

  nextTick(() => {
    if (generation !== ringGeneration) return
    ringFrame = requestAnimationFrame(() => {
      ringFrame = undefined
      if (generation !== ringGeneration) return
      ringTimer = setTimeout(() => {
        ringTimer = undefined
        if (generation !== ringGeneration) return

        // Animate percentage numbers
        const duration = 1000
        const startTime = performance.now()
        const targets = items.map(item => item.isBalance ? 0 : item.pct)

        function tick() {
          ringFrame = undefined
          if (generation !== ringGeneration) return
          const elapsed = performance.now() - startTime
          const p = Math.min(elapsed / duration, 1)
          const ease = 1 - Math.pow(1 - p, 3)
          displayPcts.value = targets.map(target => Math.round(ease * target))
          if (p < 1) ringFrame = requestAnimationFrame(tick)
        }
        ringFrame = requestAnimationFrame(tick)
      }, 50)
    })
  })
}

// ==================== Computed Data ====================

const statusInfo = computed(() => {
  const data = resultData.value
  if (!data) return null

  if (data.mode === 'quota_limited') {
    const isValid = data.isValid !== false
    const statusMap: Record<string, string> = {
      active: 'Active',
      quota_exhausted: 'Quota Exhausted',
      expired: 'Expired',
    }
    return {
      label: t('keyUsage.quotaMode'),
      statusText: statusMap[data.status] || data.status || 'Unknown',
      isActive: isValid && data.status === 'active',
    }
  }

  return {
    label: data.planName || t('keyUsage.walletBalance'),
    statusText: 'Active',
    isActive: true,
  }
})

const ringItems = computed<RingItem[]>(() => {
  const data = resultData.value
  if (!data) return []

  const items: RingItem[] = []

  if (data.mode === 'quota_limited') {
    if (data.quota) {
      const pct = data.quota.limit > 0 ? Math.min(Math.round((data.quota.used / data.quota.limit) * 100), 100) : 0
      items.push({ title: t('keyUsage.totalQuota'), pct, amount: `${usd(data.quota.used)} / ${usd(data.quota.limit)}`, iconType: 'dollar' })
    }
    if (data.rate_limits) {
      const windowLabels: Record<string, string> = { '5h': t('keyUsage.limit5h'), '1d': t('keyUsage.limitDaily'), '7d': t('keyUsage.limit7d') }
      const windowIcons: Record<string, 'clock' | 'calendar'> = { '5h': 'clock', '1d': 'calendar', '7d': 'calendar' }
      for (const rl of data.rate_limits) {
        const pct = rl.limit > 0 ? Math.min(Math.round((rl.used / rl.limit) * 100), 100) : 0
        items.push({
          title: windowLabels[rl.window] || rl.window,
          pct,
          amount: `${usd(rl.used)} / ${usd(rl.limit)}`,
          iconType: windowIcons[rl.window] || 'clock',
          resetAt: rl.reset_at,
        })
      }
    }
  } else {
    if (data.subscription) {
      const sub = data.subscription
      const limits = [
        { label: t('keyUsage.limitDaily'), usage: sub.daily_usage_usd, limit: sub.daily_limit_usd },
        { label: t('keyUsage.limitWeekly'), usage: sub.weekly_usage_usd, limit: sub.weekly_limit_usd },
        { label: t('keyUsage.limitMonthly'), usage: sub.monthly_usage_usd, limit: sub.monthly_limit_usd },
      ]
      for (const l of limits) {
        if (l.limit != null && l.limit > 0) {
          const pct = Math.min(Math.round((l.usage / l.limit) * 100), 100)
          items.push({ title: l.label, pct, amount: `${usd(l.usage)} / ${usd(l.limit)}`, iconType: 'calendar' })
        }
      }
    }
    if (!data.subscription && data.balance != null) {
      items.push({ title: t('keyUsage.walletBalance'), pct: 0, amount: usd(data.balance), isBalance: true, iconType: 'dollar' })
    }
  }

  return items
})

interface DetailRow {
  iconBg: string
  iconColor: string
  iconSvg: string
  label: string
  value: string
  valueClass: string
}

function getUsageColor(pct: number): string {
  if (pct > 90) return 'text-rose-500'
  if (pct > 70) return 'text-amber-500'
  return 'text-emerald-500'
}

const detailRows = computed<DetailRow[]>(() => {
  const data = resultData.value
  if (!data) return []

  const rows: DetailRow[] = []
  const ICON_SHIELD = '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>'
  const ICON_CALENDAR = '<rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/>'
  const ICON_DOLLAR = '<line x1="12" y1="1" x2="12" y2="23"/><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/>'
  const ICON_CHECK = '<polyline points="20 6 9 17 4 12"/>'

  if (data.mode === 'quota_limited') {
    if (data.quota) {
      const remainColor = data.quota.remaining <= 0 ? 'text-rose-500'
        : data.quota.remaining < data.quota.limit * 0.1 ? 'text-amber-500'
        : 'text-emerald-500'
      rows.push({
        iconBg: 'bg-emerald-500/10', iconColor: 'text-emerald-500', iconSvg: ICON_SHIELD,
        label: t('keyUsage.remainingQuota'), value: usd(data.quota.remaining), valueClass: remainColor,
      })
    }
    if (data.expires_at) {
      const daysLeft = data.days_until_expiry
      let expiryStr = formatDate(data.expires_at)
      if (daysLeft != null) {
        expiryStr += daysLeft > 0 ? ` ${t('keyUsage.daysLeft', { days: daysLeft })}` : daysLeft === 0 ? ` ${t('keyUsage.todayExpires')}` : ''
      }
      rows.push({
        iconBg: 'bg-amber-500/10', iconColor: 'text-amber-500', iconSvg: ICON_CALENDAR,
        label: t('keyUsage.expiresAt'), value: expiryStr, valueClass: '',
      })
    }
    if (data.rate_limits) {
      const windowMap: Record<string, string> = { '5h': '5H', '1d': locale.value.startsWith('zh') ? '日' : 'D', '7d': '7D' }
      for (const rl of data.rate_limits) {
        const pct = rl.limit > 0 ? (rl.used / rl.limit) * 100 : 0
        let valueStr = `${usd(rl.used)} / ${usd(rl.limit)}`
        const resetStr = formatResetTime(rl.reset_at)
        if (resetStr) {
          valueStr += ` (⟳ ${resetStr})`
        }
        rows.push({
          iconBg: 'bg-primary-500/10', iconColor: 'text-primary-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${windowMap[rl.window] || rl.window})`,
          value: valueStr,
          valueClass: getUsageColor(pct),
        })
      }
    }
  } else {
    rows.push({
      iconBg: 'bg-emerald-500/10', iconColor: 'text-emerald-500', iconSvg: ICON_CHECK,
      // 订阅功能关闭后这一行只会是「钱包余额」，标签改用不带「订阅」字样的「计费方式」。
      label: subscriptionFeatureEnabled.value ? t('keyUsage.subscriptionType') : t('keyUsage.billingType'),
      value: data.planName || t('keyUsage.walletBalance'), valueClass: '',
    })

    if (data.subscription) {
      const sub = data.subscription
      if (sub.daily_limit_usd > 0) {
        const pct = (sub.daily_usage_usd / sub.daily_limit_usd) * 100
        rows.push({
          iconBg: 'bg-primary-500/10', iconColor: 'text-primary-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${locale.value.startsWith('zh') ? '日' : 'D'})`, value: `${usd(sub.daily_usage_usd)} / ${usd(sub.daily_limit_usd)}`, valueClass: getUsageColor(pct),
        })
      }
      if (sub.weekly_limit_usd > 0) {
        const pct = (sub.weekly_usage_usd / sub.weekly_limit_usd) * 100
        rows.push({
          iconBg: 'bg-indigo-500/10', iconColor: 'text-indigo-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${locale.value === 'zh-Hant-HK' ? '週' : locale.value === 'zh' ? '周' : 'W'})`, value: `${usd(sub.weekly_usage_usd)} / ${usd(sub.weekly_limit_usd)}`, valueClass: getUsageColor(pct),
        })
      }
      if (sub.monthly_limit_usd > 0) {
        const pct = (sub.monthly_usage_usd / sub.monthly_limit_usd) * 100
        rows.push({
          iconBg: 'bg-emerald-500/10', iconColor: 'text-emerald-500', iconSvg: ICON_DOLLAR,
          label: `${t('keyUsage.usedQuota')} (${locale.value.startsWith('zh') ? '月' : 'M'})`, value: `${usd(sub.monthly_usage_usd)} / ${usd(sub.monthly_limit_usd)}`, valueClass: getUsageColor(pct),
        })
      }
      if (sub.expires_at) {
        rows.push({
          iconBg: 'bg-amber-500/10', iconColor: 'text-amber-500', iconSvg: ICON_CALENDAR,
          label: t('keyUsage.subscriptionExpires'), value: formatDate(sub.expires_at), valueClass: '',
        })
      }
    }

    const remainColor = data.remaining != null
      ? (data.remaining <= 0 ? 'text-rose-500' : data.remaining < 10 ? 'text-amber-500' : 'text-emerald-500')
      : ''
    rows.push({
      iconBg: 'bg-emerald-500/10', iconColor: 'text-emerald-500', iconSvg: ICON_SHIELD,
      label: t('keyUsage.remainingQuota'), value: data.remaining != null ? usd(data.remaining) : '-', valueClass: remainColor,
    })
  }

  return rows
})

interface StatCell {
  label: string
  value: string
}

const usageStatCells = computed<StatCell[]>(() => {
  const usage = resultData.value?.usage
  if (!usage) return []

  const today = usage.today || {}
  const total = usage.total || {}

  return [
    { label: t('keyUsage.todayRequests'), value: fmtNum(today.requests) },
    { label: t('keyUsage.todayInputTokens'), value: fmtNum(today.input_tokens) },
    { label: t('keyUsage.todayOutputTokens'), value: fmtNum(today.output_tokens) },
    { label: t('keyUsage.todayTokens'), value: fmtNum(today.total_tokens) },
    { label: t('keyUsage.todayCacheCreation'), value: fmtNum(today.cache_creation_tokens) },
    { label: t('keyUsage.todayCacheRead'), value: fmtNum(today.cache_read_tokens) },
    { label: t('keyUsage.todayCost'), value: usd(today.actual_cost) },
    { label: t('keyUsage.rpmTpm'), value: `${usage.rpm || 0} / ${usage.tpm || 0}` },
    { label: t('keyUsage.totalRequests'), value: fmtNum(total.requests) },
    { label: t('keyUsage.totalInputTokens'), value: fmtNum(total.input_tokens) },
    { label: t('keyUsage.totalOutputTokens'), value: fmtNum(total.output_tokens) },
    { label: t('keyUsage.totalTokensLabel'), value: fmtNum(total.total_tokens) },
    { label: t('keyUsage.totalCacheCreation'), value: fmtNum(total.cache_creation_tokens) },
    { label: t('keyUsage.totalCacheRead'), value: fmtNum(total.cache_read_tokens) },
    { label: t('keyUsage.totalCost'), value: usd(total.actual_cost) },
    { label: t('keyUsage.avgDuration'), value: usage.average_duration_ms ? `${Math.round(usage.average_duration_ms)} ms` : '-' },
  ]
})

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const modelStats = computed<any[]>(() => resultData.value?.model_stats || [])

interface DailyUsageRow {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost: number
  actual_cost?: number
}

const dailyUsageRows = computed<DailyUsageRow[]>(() => {
  const rows = resultData.value?.daily_usage
  return Array.isArray(rows) ? rows : []
})

const showDailyUsage = computed(() => Boolean(resultData.value && Array.isArray(resultData.value.daily_usage)))

// ==================== Utility Functions ====================

function usd(value: number | null | undefined): string {
  if (value == null || value < 0) return '-'
  return '$' + Number(value).toFixed(2)
}

function fmtNum(val: number | null | undefined): string {
  if (val == null) return '-'
  return val.toLocaleString()
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return '-'
  const d = new Date(iso)
  const loc = locale.value === 'zh-Hant-HK' ? 'zh-Hant-HK' : locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return d.toLocaleDateString(loc, { year: 'numeric', month: 'long', day: 'numeric' })
}

function getBrowserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

// ==================== API Query ====================

async function fetchUsage(key: string) {
  const dateParams = getDateParams()
  const url = buildGatewayUrl('/v1/usage') + (dateParams ? '?' + dateParams : '')
  const res = await fetch(url, {
    headers: { 'Authorization': 'Bearer ' + key },
  })
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    const msg = body?.error?.message || body?.message || `${t('keyUsage.queryFailed')} (${res.status})`
    throw new Error(msg)
  }
  return await res.json()
}

async function queryKey() {
  if (isQuerying.value) return
  const key = apiKey.value.trim()
  if (!key) {
    appStore.showInfo(t('keyUsage.enterApiKey'))
    return
  }

  isQuerying.value = true
  showResults.value = true
  showLoading.value = true
  resultData.value = null

  try {
    const data = await fetchUsage(key)
    resultData.value = data
    showLoading.value = false
    showDatePicker.value = true

    // Trigger ring animations after DOM update
    nextTick(() => {
      triggerRingAnimation(ringItems.value)
    })

    appStore.showSuccess(t('keyUsage.querySuccess'))
  } catch (err) {
    showResults.value = false
    showLoading.value = false
    appStore.showError((err as Error).message || t('keyUsage.queryFailedRetry'))
  } finally {
    isQuerying.value = false
  }
}

// ==================== Lifecycle ====================

function formatResetTime(resetAt: string | null | undefined): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now.value.getTime()
  if (diff <= 0) return t('keyUsage.resetNow')
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  const mins = Math.floor((diff % 3600000) / 60000)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

onMounted(() => {
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
  resetTimer = setInterval(() => { now.value = new Date() }, 60000)
})

onUnmounted(() => {
  viewDisposed = true
  cancelRingAnimation()
  if (resetTimer) clearInterval(resetTimer)
})
</script>

<style scoped>
.key-usage-context{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1.6fr);gap:clamp(2rem,6vw,6rem)}
.key-usage-quotas{padding-block:2rem}.key-usage-quota{min-width:12rem;max-width:30rem;padding-block:1rem}
.key-usage-meter{height:3px;background:var(--theme-border);margin-top:1.5rem}.key-usage-meter>span{height:100%;display:block;background:var(--theme-primary)}
.key-usage-statistics{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));column-gap:2rem;row-gap:1rem}
@media(max-width:800px){.key-usage-context{grid-template-columns:1fr}.key-usage-statistics{grid-template-columns:repeat(2,minmax(0,1fr))}}

/* Input focus ring */
.input-ring {
  transition: box-shadow 0.2s ease, border-color 0.2s ease;
}
.input-ring:focus {
  box-shadow: 0 0 0 3px var(--theme-focus);
  border-color: var(--theme-primary);
  outline: none;
}

/* Skeleton loading */
@keyframes shimmer-kv {
  0%   { background-position: -200% 0; }
  100% { background-position: 200% 0; }
}
.skeleton {
  background: linear-gradient(90deg, var(--theme-border) 25%, var(--theme-surface-raised) 50%, var(--theme-border) 75%);
  background-size: 200% 100%;
  animation: shimmer-kv 1.8s ease-in-out infinite;
  border-radius: 8px;
}
:global(.dark) .skeleton {
  background: linear-gradient(90deg, var(--theme-border) 25%, var(--theme-surface-raised) 50%, var(--theme-border) 75%);
  background-size: 200% 100%;
}

/* Tabular nums */
.tabular-nums {
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}
</style>
