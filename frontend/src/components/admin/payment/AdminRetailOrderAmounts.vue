<template>
  <div :class="compact ? 'space-y-1 text-xs' : 'grid grid-cols-2 gap-4'" data-testid="admin-retail-order-amounts">
    <div v-for="item in items" :key="item.key">
      <p :class="compact ? 'inline text-gray-500 dark:text-gray-400' : 'text-xs text-gray-500 dark:text-gray-400'">{{ t(item.label) }}<span v-if="compact">: </span></p>
      <p :class="compact ? 'inline font-medium text-gray-900 dark:text-white' : 'text-sm font-medium text-gray-900 dark:text-white'">
        {{ (item.currency ? currencySymbol(item.currency) : '$') + item.amount.toFixed(2) + (item.currency ? ' ' + item.currency : '') }}
      </p>
    </div>
    <div v-if="verifiedSettlement" data-testid="verified-payment-platform">
      <p :class="compact ? 'inline text-gray-500 dark:text-gray-400' : 'text-xs text-gray-500 dark:text-gray-400'">{{ t('payment.admin.detail.paymentPlatform') }}<span v-if="compact">: </span></p>
      <p :class="compact ? 'inline font-medium text-gray-900 dark:text-white' : 'text-sm font-medium text-gray-900 dark:text-white'">
        {{ t(verifiedSettlement.channel === 'wechat_virtual' ? 'payment.admin.detail.android' : 'payment.admin.detail.ios') }} · {{ t('payment.admin.detail.platformFee', { rate: verifiedSettlement.fee_rate_bps / 100 }) }}
      </p>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PaymentOrder, RetailQuote } from '@/types/payment'
import { currencySymbol } from '@/components/payment/currency'
import { hasOrderPayment, hasOrderCredit, getVerifiedSettlement } from './adminOrderDetail'

const props = defineProps<{ quote: RetailQuote; order?: PaymentOrder; compact?: boolean }>()
const { t } = useI18n()
const paid = computed(() => !!props.order && hasOrderPayment(props.order))
const verifiedSettlement = computed(() => props.order && getVerifiedSettlement(props.order))
const items = computed(() => {
  if (paid.value && props.quote.pricing_mode === 'gelv_channel_cny') {
    return [
      { key: 'paid', label: 'payment.admin.detail.actualPaid', amount: props.order!.pay_amount, currency: props.order!.currency || props.quote.currency },
      { key: 'credit', label: hasOrderCredit(props.order!) ? 'payment.admin.detail.actualCredited' : 'payment.admin.detail.awaitingCredit', amount: props.order!.amount, currency: '' },
    ]
  }
  const gelvEstimate = props.quote.pricing_mode === 'gelv_channel_cny'
  return [
    { key: 'credit', label: gelvEstimate ? 'payment.admin.detail.estimatedCredit' : props.quote.pricing_basis_currency === 'CNY' ? 'squarespaceProvider.order.creditedSite' : 'squarespaceProvider.order.creditedUSD', amount: paid.value ? props.order!.amount : props.quote.credited_amount_usd, currency: props.quote.pricing_basis_currency === 'CNY' ? '' : 'USD' },
    ...(props.quote.pricing_basis_currency === 'CNY' ? [{ key: 'principal', label: 'squarespaceProvider.order.principalCNY', amount: props.quote.base_amount_cny!, currency: 'CNY' }] : []),
    ...(props.quote.pricing_mode === 'gelv_channel_cny'
      ? [{ key: 'android', label: 'gelv.android', amount: props.quote.credit_by_channel!.wechat_virtual, currency: '' },
        { key: 'ios', label: 'gelv.ios', amount: props.quote.credit_by_channel!.apple_iap, currency: '' }]
      : [{ key: 'total', label: 'squarespaceProvider.order.totalGBP', amount: props.quote.total_amount_gbp, currency: 'GBP' },
        { key: 'cost', label: 'squarespaceProvider.order.includedCostGBP', amount: props.quote.included_cost_gbp, currency: 'GBP' }]),
    { key: 'paid', label: gelvEstimate ? 'payment.admin.detail.expectedPayment' : 'squarespaceProvider.order.gatewayPaid', amount: paid.value ? props.order!.pay_amount : props.quote.pay_amount, currency: paid.value ? props.order!.currency || props.quote.currency : props.quote.currency },
  ]
})
</script>
