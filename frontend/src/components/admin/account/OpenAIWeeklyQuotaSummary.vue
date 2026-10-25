<template>
  <section class="weekly-quota" :aria-label="t('admin.accounts.weeklyQuota.title')" :aria-busy="loading">
    <div class="weekly-quota-heading">
      <button class="weekly-quota-toggle" :aria-expanded="expanded" @click="expanded = !expanded">
        <Icon :name="expanded ? 'chevronUp' : 'chevronDown'" size="sm" />
        <span>{{ t('admin.accounts.weeklyQuota.title') }}</span>
        <span v-if="data" class="weekly-quota-value">{{ number(data.total_remaining) }}</span>
        <span v-else-if="loading" class="weekly-quota-secondary">{{ t('common.loading') }}</span>
        <span v-else aria-hidden="true">—</span>
      </button>
      <span v-if="data" class="weekly-quota-secondary">{{ t('admin.accounts.weeklyQuota.accounts', { count: data.included_accounts }) }}</span>
      <button class="weekly-quota-refresh" :disabled="loading" :aria-label="t('admin.accounts.weeklyQuota.refresh')" :title="t('admin.accounts.weeklyQuota.refresh')" @click="load">
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
      </button>
    </div>
    <p v-if="error" role="alert" class="weekly-quota-error">
      {{ t('admin.accounts.weeklyQuota.error') }}
      <button class="underline underline-offset-2" :disabled="loading" @click="load">{{ t('admin.accounts.weeklyQuota.retry') }}</button>
    </p>
    <div v-if="expanded" class="weekly-quota-details">
      <p class="weekly-quota-secondary">{{ t('admin.accounts.weeklyQuota.global') }}</p>
      <p v-if="loading && !data" role="status" class="weekly-quota-secondary">{{ t('admin.accounts.weeklyQuota.loading') }}</p>
      <template v-if="data">
        <div class="weekly-quota-table-wrap">
          <table class="weekly-quota-table">
            <caption class="sr-only">{{ t('admin.accounts.weeklyQuota.title') }}</caption>
            <thead><tr>
              <th scope="col">{{ t('admin.accounts.weeklyQuota.tier') }}</th>
              <th scope="col">{{ t('admin.accounts.weeklyQuota.count') }}</th>
              <th scope="col">{{ t('admin.accounts.weeklyQuota.limit') }}</th>
              <th scope="col">{{ t('admin.accounts.weeklyQuota.remaining') }}</th>
            </tr></thead>
            <tbody><tr v-for="tier in data.tiers" :key="tier.plan">
              <th scope="row">{{ tier.label }}</th>
              <td>{{ tier.account_count }}</td>
              <td>{{ number(tier.weekly_limit) }}</td>
              <td>{{ number(tier.remaining) }}</td>
            </tr></tbody>
          </table>
        </div>
        <p v-if="!data.included_accounts" class="weekly-quota-secondary">{{ t('admin.accounts.weeklyQuota.empty') }}</p>
        <p class="weekly-quota-secondary">{{ t('admin.accounts.weeklyQuota.formula') }}</p>
        <p v-if="excludedReasons" class="weekly-quota-secondary">{{ t('admin.accounts.weeklyQuota.excluded', { reasons: excludedReasons }) }}</p>
        <p class="weekly-quota-secondary">
          {{ data.oldest_snapshot_at ? t('admin.accounts.weeklyQuota.snapshots', { time: date(data.oldest_snapshot_at) }) : t('admin.accounts.weeklyQuota.generated', { time: date(data.generated_at) }) }}
        </p>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getOpenAIWeeklyQuota, type OpenAIWeeklyQuotaSummary } from '@/api/admin/accounts'

const props = defineProps<{ refreshToken?: number; accountStateKey?: string }>()
const { t, locale } = useI18n()
const expanded = ref(false)
const data = ref<OpenAIWeeklyQuotaSummary | null>(null)
const loading = ref(false)
const error = ref(false)
let controller: AbortController | null = null
const excludedKeys = ['inactive', 'unsupported_plan', 'missing_quota', 'non_weekly_window', 'invalid_quota', 'expired_snapshot'] as const
const excludedReasons = computed(() => excludedKeys
  .filter(key => (data.value?.excluded[key] ?? 0) > 0)
  .map(key => t(`admin.accounts.weeklyQuota.${key}`, { count: data.value!.excluded[key] }))
  .join(' · '))
const number = (value: number) => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(value)
const date = (value: string) => new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = false
  data.value = null
  try {
    const result = await getOpenAIWeeklyQuota({ signal: request.signal })
    if (!request.signal.aborted) data.value = result
  } catch {
    if (!request.signal.aborted) error.value = true
  } finally {
    if (controller === request) {
      loading.value = false
      controller = null
    }
  }
}
watch([() => props.refreshToken, () => props.accountStateKey], () => { void load() })
onMounted(() => { void load() })
onUnmounted(() => { controller?.abort() })
</script>

<style scoped>
.weekly-quota { @apply mt-3 border-t border-gray-200 pt-3 text-sm text-gray-700 dark:border-dark-700 dark:text-gray-200; }
.weekly-quota-heading { @apply flex flex-wrap items-center gap-x-3 gap-y-1; }
.weekly-quota-toggle { @apply inline-flex min-h-9 items-center gap-2 rounded-md text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500; }
.weekly-quota-value { @apply ml-1 font-semibold text-gray-900 dark:text-gray-100; font-variant-numeric: tabular-nums; }
.weekly-quota-secondary { @apply text-sm leading-relaxed text-gray-500 dark:text-gray-400; }
.weekly-quota-refresh { @apply inline-flex h-9 w-9 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700; }
.weekly-quota-error { @apply mt-1 text-sm text-red-700 dark:text-red-300; }
.weekly-quota-details { @apply mt-2 flex max-w-3xl flex-col gap-2; }
.weekly-quota-table-wrap { @apply overflow-x-auto; }
.weekly-quota-table { @apply w-full text-sm; font-variant-numeric: tabular-nums; }
.weekly-quota-table th, .weekly-quota-table td { @apply border-b border-gray-100 px-2 py-2 text-right dark:border-dark-700; }
.weekly-quota-table th:first-child { @apply pl-0 text-left; }
.weekly-quota-table thead th { @apply font-normal text-gray-500 dark:text-gray-400; }
.weekly-quota-table tbody th { @apply font-medium; }
</style>
