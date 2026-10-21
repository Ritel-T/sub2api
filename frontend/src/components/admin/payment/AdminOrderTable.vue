<template>
  <div class="space-y-4">
    <div class="open-section">
      <div class="flex flex-wrap items-center gap-3">
        <div class="flex-1 sm:max-w-64">
          <input
            v-model="searchQuery"
            type="text"
            :placeholder="t('payment.admin.searchOrders')"
            class="input"
            @input="handleSearch"
          />
        </div>
        <Select
          v-model="filters.status"
          :options="statusFilterOptions"
          class="w-36"
          @change="emitFiltersChanged"
        />
        <Select
          v-model="filters.payment_type"
          :options="paymentTypeFilterOptions"
          class="w-40"
          @change="emitFiltersChanged"
        />
        <Select
          v-model="filters.order_type"
          :options="orderTypeFilterOptions"
          class="w-36"
          @change="emitFiltersChanged"
        />
        <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
          <button
            @click="emit('refresh')"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>
    </div>

    <DataTable :primary-columns="['id', 'status', 'pay_amount', 'user_id', 'created_at', 'actions']" column-order-key="table-columns:admin-orders:v1" :columns="columns" :data="orders" :loading="loading">
      <template #cell-id="{ value }">
        <span class="font-mono text-sm">#{{ value }}</span>
      </template>

      <template #cell-user_id="{ value }">
        <span class="text-sm text-gray-600 dark:text-gray-400">#{{ value }}</span>
      </template>

      <template #cell-pay_amount="{ value, row }">
        <AdminRetailOrderAmounts v-if="row.retail_quote" :quote="row.retail_quote" compact />
        <div v-else class="text-sm">
          <span class="font-medium text-gray-900 dark:text-white">{{ paymentAmountSymbol(row) }}{{ value.toFixed(2) }}</span>
          <span v-if="row.fee_rate > 0" class="ml-1 text-xs text-gray-400" :title="t('payment.orders.fee') + ': ' + row.fee_rate + '%'">
            ({{ row.fee_rate }}%)
          </span>
          <div v-if="row.amount !== row.pay_amount || (row.bonus_amount ?? 0) > 0" class="text-xs text-gray-500">
            {{ t('payment.orders.creditedAmount') }}: {{ creditedAmountSymbol }}{{ row.amount.toFixed(2) }}
            <span v-if="(row.bonus_amount ?? 0) > 0" class="ml-1 text-amber-600 dark:text-amber-400">
              ({{ t('payment.orders.bonusIncluded', { amount: creditedAmountSymbol + (row.bonus_amount ?? 0).toFixed(2) }) }})
            </span>
          </div>
        </div>
      </template>

      <template #cell-payment_type="{ value }">
        <span class="text-sm text-gray-700 dark:text-gray-300">
          {{ value === 'squarespace' ? t('squarespaceProvider.name') : t('payment.methods.' + value, value) }}
        </span>
      </template>

      <template #cell-status="{ value }">
        <span :class="['badge', statusBadgeClass(value)]">
          {{ t('payment.status.' + value.toLowerCase(), value) }}
        </span>
      </template>

      <template #cell-order_type="{ value }">
        <span class="text-sm text-gray-700 dark:text-gray-300">
          {{ t('payment.admin.' + value + 'Order', value) }}
        </span>
      </template>

      <template #cell-created_at="{ value }">
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ formatDateTime(value) }}</span>
      </template>

      <template #cell-actions="{ row }">
        <div class="workspace-row-actions">
          <button :title="t('common.view')" :aria-label="t('common.view')"
            @click="emit('detail', row)"
            class="theme-action-button transition-colors workspace-icon-action"
          >
            <Icon name="eye" size="sm" />
            <span class="sr-only">{{ t('common.view') }}</span>
          </button>
          <button :title="t('payment.orders.cancel')" :aria-label="t('payment.orders.cancel')"
            v-if="row.status === 'PENDING'"
            @click="emit('cancel', row)"
            class="text-gray-500 transition-colors hover:bg-yellow-50 hover:text-yellow-600 dark:hover:bg-yellow-900/20 dark:hover:text-yellow-400 workspace-icon-action"
          >
            <Icon name="x" size="sm" />
            <span class="sr-only">{{ t('payment.orders.cancel') }}</span>
          </button>
          <button :title="t('payment.admin.retry')" :aria-label="t('payment.admin.retry')"
            v-if="row.status === 'FAILED'"
            @click="emit('retry', row)"
            class="theme-action-button transition-colors workspace-icon-action"
          >
            <Icon name="refresh" size="sm" />
            <span class="sr-only">{{ t('payment.admin.retry') }}</span>
          </button>
          <button :title="t('payment.admin.refund')" :aria-label="t('payment.admin.refund')"
            v-if="canRefundRow(row)"
            @click="emit('refund', row)"
            class="text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400 workspace-icon-action"
          >
            <Icon name="dollar" size="sm" />
            <span class="sr-only">{{ t('payment.admin.refund') }}</span>
          </button>
        </div>
      </template>
    </DataTable>

    <InfiniteListSentinel
      :loading="loading"
      :error="loadError"
      :has-more="orders.length < total"
      @load-more="emit('update:page', page + 1)"
      @retry="emit('refresh')"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PaymentOrder } from '@/types/payment'
import type { Column } from '@/components/common/types'
import DataTable from '@/components/common/DataTable.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import AdminRetailOrderAmounts from './AdminRetailOrderAmounts.vue'
import { statusBadgeClass, canRefund, formatOrderDateTime } from '@/components/payment/orderUtils'
import { currencySymbol } from '@/components/payment/currency'

const { t } = useI18n()

defineProps<{
  orders: PaymentOrder[]
  loading: boolean
  page: number
  pageSize: number
  total: number
  loadError?: unknown
}>()

const emit = defineEmits<{
  (e: 'detail', order: PaymentOrder): void
  (e: 'cancel', order: PaymentOrder): void
  (e: 'retry', order: PaymentOrder): void
  (e: 'refund', order: PaymentOrder): void
  (e: 'refresh'): void
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', size: number): void
  (e: 'filter', filters: { keyword?: string; status?: string; payment_type?: string; order_type?: string }): void
}>()

const searchQuery = ref('')
const filters = reactive({ status: '', payment_type: '', order_type: '' })
const creditedAmountSymbol = currencySymbol('USD')

function paymentAmountSymbol(order: PaymentOrder): string {
  return currencySymbol(order.currency)
}

let debounceTimer: ReturnType<typeof setTimeout> | null = null
onUnmounted(() => { if (debounceTimer) clearTimeout(debounceTimer) })
function handleSearch() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => emitFiltersChanged(), 300)
}

function emitFiltersChanged() {
  emit('filter', {
    keyword: searchQuery.value || undefined,
    status: filters.status || undefined,
    payment_type: filters.payment_type || undefined,
    order_type: filters.order_type || undefined,
  })
}

const columns = computed<Column[]>(() => [
  { key: 'id', label: t('payment.orders.orderId') },
  { key: 'status', label: t('payment.orders.status') },
  { key: 'pay_amount', label: t('payment.orders.payAmount') },
  { key: 'user_id', label: t('payment.orders.userId') },
  { key: 'order_type', label: t('payment.orders.orderType') },
  { key: 'payment_type', label: t('payment.orders.paymentMethod') },
  { key: 'created_at', label: t('payment.orders.createdAt') },
  { key: 'actions', label: t('payment.orders.actions') }
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('payment.admin.allStatuses') },
  { value: 'PENDING', label: t('payment.status.pending') },
  { value: 'PAID', label: t('payment.status.paid') },
  { value: 'COMPLETED', label: t('payment.status.completed') },
  { value: 'EXPIRED', label: t('payment.status.expired') },
  { value: 'CANCELLED', label: t('payment.status.cancelled') },
  { value: 'FAILED', label: t('payment.status.failed') },
  { value: 'REFUNDED', label: t('payment.status.refunded') },
  { value: 'REFUND_REQUESTED', label: t('payment.status.refund_requested') },
  { value: 'REFUND_PENDING', label: t('payment.status.refund_pending') },
  { value: 'REFUND_FAILED', label: t('payment.status.refund_failed') },
])

const paymentTypeFilterOptions = computed(() => [
  { value: '', label: t('payment.admin.allPaymentTypes') },
  { value: 'alipay', label: t('payment.methods.alipay') },
  { value: 'wxpay', label: t('payment.methods.wxpay') },
  { value: 'stripe', label: t('payment.methods.stripe') },
  { value: 'airwallex', label: t('payment.methods.airwallex') },
  { value: 'squarespace', label: t('squarespaceProvider.name') },
])

const orderTypeFilterOptions = computed(() => [
  { value: '', label: t('payment.admin.allOrderTypes') },
  { value: 'balance', label: t('payment.admin.balanceOrder') },
  { value: 'subscription', label: t('payment.admin.subscriptionOrder') },
])

function canRefundRow(order: PaymentOrder): boolean {
  return order.payment_type !== 'squarespace' && canRefund(order.status)
}

function formatDateTime(dateStr: string): string {
  return formatOrderDateTime(dateStr)
}
</script>
