<template>
  <div :class="compact ? 'space-y-1 text-xs' : 'grid grid-cols-2 gap-4'" data-testid="admin-retail-order-amounts">
    <div v-for="item in items" :key="item.key">
      <p :class="compact ? 'inline text-gray-500 dark:text-gray-400' : 'text-xs text-gray-500 dark:text-gray-400'">{{ t(item.label) }}<span v-if="compact">: </span></p>
      <p :class="compact ? 'inline font-medium text-gray-900 dark:text-white' : 'text-sm font-medium text-gray-900 dark:text-white'">
        {{ (item.currency ? currencySymbol(item.currency) : '$') + item.amount.toFixed(2) + (item.currency ? ' ' + item.currency : '') }}
      </p>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RetailQuote } from '@/types/payment'
import { currencySymbol } from '@/components/payment/currency'

const props = defineProps<{ quote: RetailQuote; compact?: boolean }>()
const { t } = useI18n()
const items = computed(() => [
  { key: 'credit', label: props.quote.pricing_basis_currency === 'CNY' ? 'squarespaceProvider.order.creditedSite' : 'squarespaceProvider.order.creditedUSD', amount: props.quote.credited_amount_usd, currency: props.quote.pricing_basis_currency === 'CNY' ? '' : 'USD' },
  ...(props.quote.pricing_basis_currency === 'CNY' ? [{ key: 'principal', label: 'squarespaceProvider.order.principalCNY', amount: props.quote.base_amount_cny!, currency: 'CNY' }] : []),
  { key: 'total', label: 'squarespaceProvider.order.totalGBP', amount: props.quote.total_amount_gbp, currency: 'GBP' },
  { key: 'cost', label: 'squarespaceProvider.order.includedCostGBP', amount: props.quote.included_cost_gbp, currency: 'GBP' },
  { key: 'paid', label: 'squarespaceProvider.order.gatewayPaid', amount: props.quote.pay_amount, currency: props.quote.currency },
])
</script>
