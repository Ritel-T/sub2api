<template>
  <div data-test="retail-quote" class="space-y-3 text-sm">
    <div class="flex justify-between gap-4">
      <span class="text-gray-500 dark:text-gray-400">{{ t(cnyBasis ? 'paymentRetail.siteCredit' : 'paymentRetail.credited') }}</span>
      <span class="font-semibold text-gray-900 dark:text-white">{{ cnyBasis ? '$' + quote.credited_amount_usd.toFixed(2) : money(quote.credited_amount_usd, 'USD') + ' USD' }}</span>
    </div>
    <template v-if="cnyBasis">
      <div data-test="retail-cny-principal" class="flex justify-between gap-4">
        <span class="text-gray-500 dark:text-gray-400">{{ t('paymentRetail.principalCNY') }}</span>
        <span class="font-semibold text-gray-900 dark:text-white">{{ money(quote.base_amount_cny!, 'CNY') }} CNY</span>
      </div>
      <p data-test="retail-credit-policy" class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('paymentRetail.creditPolicy') }}</p>
      <p data-test="retail-gbp-cny-rate" class="text-xs text-gray-500 dark:text-gray-400">{{ t('paymentRetail.gbpCnyRate', { rate: rateLabel }) }}</p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('paymentRetail.fxAsOf', { time: fxAsOfLabel }) }}</p>
    </template>
    <div class="flex justify-between gap-4">
      <span class="text-gray-500 dark:text-gray-400">{{ t('paymentRetail.total') }}</span>
      <span class="font-semibold text-gray-900 dark:text-white">{{ money(quote.total_amount_gbp, 'GBP') }} GBP</span>
    </div>
    <div class="flex justify-between gap-4 text-xs">
      <span class="text-gray-500 dark:text-gray-400">{{ t('paymentRetail.includedCost') }}</span>
      <span class="text-gray-700 dark:text-gray-300">{{ money(quote.included_cost_gbp, 'GBP') }} GBP</span>
    </div>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('paymentRetail.priceHint') }}</p>
    <div class="flex justify-between gap-4 border-t border-gray-200 pt-3 dark:border-dark-600">
      <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('paymentRetail.actualPay') }}</span>
      <span class="text-lg font-bold text-primary-600 dark:text-primary-400">{{ money(quote.pay_amount, quote.currency) }} {{ quote.currency }}</span>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RetailQuote } from '@/types/payment'
import { formatPaymentAmount } from './currency'
const props = defineProps<{ quote: RetailQuote }>()
const cnyBasis = computed(() => props.quote.pricing_basis_currency === 'CNY')
const { t, locale } = useI18n()
const rateLabel = computed(() => props.quote.fx.CNY.toLocaleString(locale?.value, { maximumFractionDigits: 6 }))
const fxAsOfLabel = computed(() => new Date(props.quote.fx_asof).toLocaleString(locale?.value))
const money = (amount: number, currency: string) => formatPaymentAmount(amount, currency, locale?.value)
</script>
