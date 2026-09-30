<template>
  <div data-test="retail-quote" class="space-y-3 text-sm">
    <div class="flex justify-between gap-4">
      <span class="text-gray-500 dark:text-gray-400">{{ t('paymentRetail.credited') }}</span>
      <span class="font-semibold text-gray-900 dark:text-white">{{ money(quote.credited_amount_usd, 'USD') }} USD</span>
    </div>
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
import { useI18n } from 'vue-i18n'
import type { RetailQuote } from '@/types/payment'
import { formatPaymentAmount } from './currency'
defineProps<{ quote: RetailQuote }>()
const { t, locale } = useI18n()
const money = (amount: number, currency: string) => formatPaymentAmount(amount, currency, locale?.value)
</script>
