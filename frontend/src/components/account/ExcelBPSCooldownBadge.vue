<template>
  <span
    v-if="remainingMs > 0"
    data-test="excel-bps-cooldown-badge"
    class="inline-flex max-w-full flex-wrap items-center gap-x-1 rounded bg-amber-100 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
    :title="tooltip"
    :aria-label="tooltip"
  >
    <Icon name="exclamationTriangle" size="xs" :stroke-width="2" />
    <span>{{ t('admin.accounts.status.bpsCooldown') }}</span>
    <span class="tabular-nums">{{ countdown }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AccountListItem } from '@/types'
import { formatCountdown, formatDateTime } from '@/utils/format'

const props = defineProps<{
  account: Pick<AccountListItem, 'platform' | 'type' | 'extra'>
}>()

const { t } = useI18n()
const now = ref(Date.now())
const resetAt = computed(() => {
  const { platform, type, extra } = props.account
  if (platform !== 'openai' || type !== 'oauth' || extra?.openai_excel_bps !== true) return null
  const raw = extra.openai_excel_bps_rate_limit_reset_at
  if (typeof raw !== 'string') return null
  const date = new Date(raw)
  return Number.isFinite(date.getTime()) ? date : null
})
const remainingMs = computed(() => Math.max(0, (resetAt.value?.getTime() ?? 0) - now.value))

// Only active cooldowns need a clock; expiry hides the badge without a list refresh.
const { pause, resume } = useIntervalFn(() => {
  now.value = Date.now()
  if (remainingMs.value === 0) pause()
}, 1000, { immediate: false })

watch(resetAt, () => {
  now.value = Date.now()
  if (remainingMs.value > 0) resume()
  else pause()
}, { immediate: true })

const resetTime = computed(() => formatDateTime(resetAt.value))
const countdown = computed(() => {
  if (remainingMs.value <= 0) return ''
  if (remainingMs.value < 60_000) {
    return t('admin.accounts.status.bpsCooldownSeconds', { seconds: Math.ceil(remainingMs.value / 1000) })
  }
  return formatCountdown(resetAt.value) || resetTime.value
})
const tooltip = computed(() => t('admin.accounts.status.bpsCooldownUntil', {
  reason: t(props.account.extra?.openai_excel_bps_rate_limit_reason === 'quota_exhausted'
    ? 'admin.accounts.status.bpsQuotaExhausted'
    : 'admin.accounts.status.bpsRateLimited'),
  time: resetTime.value
}))
</script>
