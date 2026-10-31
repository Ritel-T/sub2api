<template>
  <div class="space-y-1 text-sm" data-testid="order-amounts">
    <div data-testid="order-paid">
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.actualPaid') }}: </span>
      <span class="font-medium text-gray-900 dark:text-white">{{ paid ? cash : '—' }}</span>
    </div>
    <div data-testid="order-credited" class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('payment.orders.actualCredited') }}:
      {{ credited && order.order_type === 'balance' ? formatMoney(order.amount) : '—' }}
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PaymentOrder } from '@/types/payment'
import { formatPaymentAmount } from './currency'
import { formatMoney } from '@/utils/consoleDisplay'
import { hasOrderCredit, hasOrderPayment } from './orderPaymentState'
const props = defineProps<{ order: PaymentOrder }>()
const { t } = useI18n()
const paid = computed(() => hasOrderPayment(props.order))
const credited = computed(() => hasOrderCredit(props.order))
const cash = computed(() => {
  const currency = props.order.currency || props.order.retail_quote?.currency
  return formatPaymentAmount(props.order.pay_amount, currency)
})
</script>
