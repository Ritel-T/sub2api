<template>
  <div data-test="retail-quote" class="space-y-3 text-sm">
    <div class="flex justify-between gap-4">
      <span class="text-gray-500 dark:text-gray-400">{{ t(cnyBasis ? 'paymentRetail.siteCredit' : 'paymentRetail.credited') }}</span>
      <span v-if="fixedCNY" class="text-right font-semibold text-gray-900 dark:text-white">
        <span class="block">{{ t('gelv.androidCredit', { amount: quote.credit_by_channel!.wechat_virtual.toFixed(2) }) }}</span>
        <span class="block">{{ t('gelv.iosCredit', { amount: quote.credit_by_channel!.apple_iap.toFixed(2) }) }}</span>
      </span>
      <span v-else class="font-semibold text-gray-900 dark:text-white">{{ cnyBasis ? '$' + quote.credited_amount_usd.toFixed(2) : money(quote.credited_amount_usd, 'USD') + ' USD' }}</span>
    </div>
    <div data-test="retail-included-fee" class="flex justify-between gap-4 text-xs">
      <span class="text-gray-500 dark:text-gray-400">{{ t('paymentRetail.includedCost') }}</span>
      <span class="text-gray-700 dark:text-gray-300">{{ fixedCNY ? t('gelv.feeRates') : money(quote.included_cost_gbp, 'GBP') + ' GBP' }}</span>
    </div>
    <p v-if="fixedCNY" class="text-xs text-gray-500">{{ t('gelv.channelRule') }}</p>
    <div class="flex justify-between gap-4 border-t border-gray-200 pt-3 dark:border-dark-600">
      <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('paymentRetail.actualPay') }}</span>
      <span class="text-lg font-bold text-primary-600 dark:text-primary-400">{{ money(quote.pay_amount, quote.currency) }} {{ quote.currency }}</span>
    </div>
    <details v-if="!fixedCNY && (cnyBasis || quote.currency !== 'GBP')" data-test="retail-price-details" class="border-t border-gray-200 pt-3 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">
      <summary class="cursor-pointer select-none">{{ t('paymentRetail.priceDetails') }}</summary>
      <div class="mt-3 space-y-2">
        <template v-if="cnyBasis">
          <div data-test="retail-cny-principal" class="flex justify-between gap-4">
            <span>{{ t('paymentRetail.principalCNY') }}</span>
            <span>{{ money(quote.base_amount_cny!, 'CNY') }} CNY</span>
          </div>
          <p data-test="retail-credit-policy">{{ t('paymentRetail.creditPolicy') }}</p>
          <p data-test="retail-gbp-cny-rate">{{ t('paymentRetail.gbpCnyRate', { rate: rateLabel }) }}</p>
          <p>{{ t('paymentRetail.fxAsOf', { time: fxAsOfLabel }) }}</p>
        </template>
        <div v-if="quote.currency !== 'GBP'" class="flex justify-between gap-4">
          <span>{{ t('paymentRetail.total') }}</span>
          <span>{{ money(quote.total_amount_gbp, 'GBP') }} GBP</span>
        </div>
      </div>
    </details>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RetailQuote } from '@/types/payment'
import { formatPaymentAmount } from './currency'
const props = defineProps<{ quote: RetailQuote }>()
const fixedCNY = computed(() => props.quote.pricing_mode === 'gelv_channel_cny')
const cnyBasis = computed(() => props.quote.pricing_basis_currency === 'CNY')
const { t, locale } = useI18n()
const rateLabel = computed(() => props.quote.fx.CNY.toLocaleString(locale?.value, { maximumFractionDigits: 6 }))
const fxAsOfLabel = computed(() => new Date(props.quote.fx_asof).toLocaleString(locale?.value))
const money = (amount: number, currency: string) => formatPaymentAmount(amount, currency, locale?.value)
</script>
