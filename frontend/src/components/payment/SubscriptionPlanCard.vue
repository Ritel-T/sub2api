<template>
  <div class="subscription-offer">
    <div class="subscription-offer-layout">
      <!-- Header: name + badge + price -->
      <div class="subscription-offer-identity">
        <div class="min-w-0 flex-1">
          <AppHeading as="h3" variant="subsection"
            :title="plan.name"
            class="min-w-0 break-words [overflow-wrap:anywhere] text-gray-900 dark:text-white"
          >
            {{ plan.name }}
          </AppHeading>
          <p v-if="plan.description" class="mt-2 text-sm leading-relaxed text-gray-500 dark:text-dark-400">
            {{ plan.description }}
          </p>
        </div>
        <div class="mt-4">
          <div class="flex items-baseline gap-1">
            <span class="text-xs text-gray-400 dark:text-dark-500">{{ planCurrencySymbol }}</span>
            <span :class="['text-2xl font-extrabold tracking-tight', textClass]">{{ plan.price }}</span>
            <span v-if="plan.currency" class="text-xs font-medium text-gray-400 dark:text-dark-500">{{ plan.currency }}</span>
          </div>
          <div class="flex items-center gap-1">
            <span :class="['inline-flex shrink-0 rounded-full px-2 py-0.5 text-xs font-medium', badgeLightClass]">
              {{ pLabel }}
            </span>
            <span class="text-xs text-gray-400 dark:text-dark-500">/ {{ validitySuffix }}</span>
          </div>
          <div v-if="plan.original_price" class="mt-0.5 flex items-center gap-1.5">
            <span class="text-xs text-gray-400 line-through dark:text-dark-500">{{ planCurrencySymbol }}{{ plan.original_price }}<template v-if="plan.currency"> {{ plan.currency }}</template></span>
            <span :class="['rounded px-1 py-0.5 text-xs font-semibold', discountClass]">{{ discountText }}</span>
          </div>
        </div>
      </div>

      <p v-if="plan.group_name" class="subscription-offer-group text-xs text-gray-500 dark:text-dark-400" data-testid="plan-group-name" :title="plan.group_name">
        {{ t('payment.admin.group') }}: {{ plan.group_name }}
      </p>

      <!-- Group quota info (compact) -->
      <div class="subscription-offer-terms grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
        <div class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.rate') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ rateDisplay }}</span>
        </div>
        <div v-if="hasPeakRate" class="col-span-2 flex items-center justify-between gap-2">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.peakRate') }}</span>
          <span class="text-right font-medium text-amber-700 dark:text-amber-300">{{ peakRateDisplay }}</span>
        </div>
        <div v-if="hasPositivePlanLimit(plan.daily_limit_usd)" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.dailyLimit') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">${{ plan.daily_limit_usd }}</span>
        </div>
        <div v-if="hasPositivePlanLimit(plan.weekly_limit_usd)" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.weeklyLimit') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">${{ plan.weekly_limit_usd }}</span>
        </div>
        <div v-if="hasPositivePlanLimit(plan.monthly_limit_usd)" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.monthlyLimit') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">${{ plan.monthly_limit_usd }}</span>
        </div>
        <div v-if="!hasPositivePlanLimit(plan.daily_limit_usd) && !hasPositivePlanLimit(plan.weekly_limit_usd) && !hasPositivePlanLimit(plan.monthly_limit_usd)" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.quota') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('payment.planCard.unlimited') }}</span>
        </div>
        <div v-if="modelScopeLabels.length > 0" class="col-span-2 flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.models') }}</span>
          <div class="flex flex-wrap gap-1">
            <span v-for="scope in modelScopeLabels" :key="scope"
              class="rounded bg-gray-200/80 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-600 dark:text-gray-300">
              {{ scope }}
            </span>
          </div>
        </div>
      </div>

      <!-- Features list (compact) -->
      <div v-if="displayFeatures.length > 0" class="subscription-offer-features space-y-2">
        <div v-for="feature in displayFeatures" :key="feature" class="flex items-start gap-1.5">
          <svg :class="['mt-0.5 h-3.5 w-3.5 flex-shrink-0', iconClass]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
          </svg>
          <span class="text-xs text-gray-600 dark:text-gray-300">{{ feature }}</span>
        </div>
      </div>

      <!-- Subscribe Button -->
      <button
        type="button"
        class="subscription-offer-action btn btn-primary"
        :disabled="!availableForSale"
        @click="availableForSale && emit('select', plan)"
      >
        {{ isRenewal ? t('payment.renewNow') : t('payment.subscribeNow') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'
import type { UserSubscription } from '@/types'
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { planValiditySuffix } from './validity'
import { currencySymbol } from '@/components/payment/currency'
import { hasPositivePlanLimit, planCustomFeatures } from './planGroupTerms'
import {
  platformBadgeLightClass,
  platformTextClass,
  platformIconClass,
  platformDiscountClass,
  platformLabel,
} from '@/utils/platformColors'

const props = defineProps<{ plan: SubscriptionPlan; activeSubscriptions?: UserSubscription[] }>()
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()

const platform = computed(() => props.plan.group_platform || '')
const displayFeatures = computed(() => planCustomFeatures(props.plan.features))
const availableForSale = computed(() => props.plan.for_sale !== false && (!props.plan.group_status || props.plan.group_status === 'active'))
const isRenewal = computed(() =>
  props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false
)

// Derived color classes from central config
const badgeLightClass = computed(() => platformBadgeLightClass(platform.value))
const textClass = computed(() => platformTextClass(platform.value))
const iconClass = computed(() => platformIconClass(platform.value))
const discountClass = computed(() => platformDiscountClass(platform.value))
const pLabel = computed(() => platformLabel(platform.value))

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const rateDisplay = computed(() => {
  const rate = props.plan.rate_multiplier ?? 1
  return `×${Number(rate.toPrecision(10))}`
})

const appStore = useAppStore()
const planCurrencySymbol = computed(() => currencySymbol(props.plan.currency || 'USD'))

const hasPeakRate = computed(() => groupHasPeakRate(props.plan))

const peakRateDisplay = computed(() => {
  return formatPeakRateWindow(props.plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
})

const MODEL_SCOPE_LABELS: Record<string, string> = {
  claude: 'Claude',
  gemini_text: 'Gemini',
  gemini_image: 'Imagen',
}

const modelScopeLabels = computed(() => {
  if (platform.value !== 'antigravity') return []
  const scopes = props.plan.supported_model_scopes
  if (!scopes || scopes.length === 0) return []
  return scopes.map(s => MODEL_SCOPE_LABELS[s] || s)
})

const validitySuffix = computed(() => planValiditySuffix(props.plan, t))
</script>

<style scoped>
.subscription-offer { border-top: 1px solid var(--ritel-border); padding-block: 2rem; }
.subscription-offer-layout { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 1.5fr) auto; gap: 1rem 3rem; align-items: start; }
.subscription-offer-identity { grid-column: 1; grid-row: 1 / 3; min-width: 0; }
.subscription-offer-terms { grid-column: 2; grid-row: 1 / 3; min-width: 0; }
.subscription-offer-terms > div { gap: .75rem; }
.subscription-offer-group { grid-column: 1; overflow-wrap: anywhere; }
.subscription-offer-features { grid-column: 2; }
.subscription-offer-action { grid-column: 3; grid-row: 1; align-self: start; white-space: nowrap; }
@media (max-width: 1100px) { .subscription-offer-layout { grid-template-columns: minmax(0, 1fr) minmax(0, 1.3fr); gap: 1.5rem; } .subscription-offer-action { grid-column: 1; grid-row: auto; justify-self: start; } }
@media (max-width: 639px) { .subscription-offer-layout { display: flex; flex-direction: column; gap: 1.5rem; } .subscription-offer-layout > * { width: 100%; } .subscription-offer-action { width: auto; } }
</style>
