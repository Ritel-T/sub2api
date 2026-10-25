<template>
  <AppLayout>
    <div class="open-page space-y-4">
      <ConsoleObjectLinks v-if="contextUserId" kind="user" :id="contextUserId" context />
      <!-- Filters -->
      <div class="open-section">
        <div class="open-filter-toolbar flex flex-wrap items-center gap-3">
          <div class="flex-1 sm:max-w-64">
            <input v-model="orderSearch" type="text" :placeholder="t('payment.admin.searchOrders')" class="input" @input="debounceLoadOrders" />
          </div>
          <Select :aria-label="t('payment.orders.status')" v-model="orderFilters.status" :options="statusFilterOptions" class="w-36" @change="resetOrders" />
          <Select :aria-label="t('payment.orders.paymentMethod')" v-model="orderFilters.payment_type" :options="paymentTypeFilterOptions" class="w-40" @change="resetOrders" />
          <Select :aria-label="t('payment.orders.orderType')" v-model="orderFilters.order_type" :options="orderTypeFilterOptions" class="w-36" @change="resetOrders" />
          <div ref="tableToolbarTarget" class="min-w-0" />
          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button @click="loadOrders" :disabled="ordersLoading" class="btn btn-secondary" :title="t('common.refresh')">
              <Icon name="refresh" size="md" :class="ordersLoading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </div>

      <!-- Table -->
      <OrderTable :orders="orders" :loading="ordersLoading" show-user>
        <template #actions="{ row }">
          <div class="flex items-center gap-1">
            <button @click="showOrderDetail(row)" class="theme-action-button inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
              <Icon name="eye" size="sm" />
              {{ t('common.view') }}
            </button>
            <button v-if="row.status === 'PENDING'" @click="handleCancelOrder(row)" class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-yellow-600 hover:bg-yellow-50 dark:text-yellow-400 dark:hover:bg-yellow-900/20">
              <Icon name="x" size="sm" />
              {{ t('payment.orders.cancel') }}
            </button>
            <button v-if="row.status === 'FAILED'" @click="handleRetryOrder(row)" class="theme-action-button inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
              <Icon name="refresh" size="sm" />
              {{ t('payment.admin.retry') }}
            </button>
            <template v-if="row.payment_type !== 'squarespace' && row.status === 'REFUND_REQUESTED'">
              <span v-if="row.refund_amount" class="rounded-full bg-purple-100 px-1.5 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/30 dark:text-purple-300">{{ creditedAmountSymbol }}{{ row.refund_amount.toFixed(2) }}</span>
              <button @click="openRefundDialog(row)" class="theme-action-button inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
                <Icon name="check" size="sm" />
                {{ t('payment.admin.approveRefund') }}
              </button>
            </template>
            <button v-else-if="row.payment_type !== 'squarespace' && row.status === 'REFUND_FAILED'" @click="openRefundDialog(row)" class="theme-action-button inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium">
              <Icon name="refresh" size="sm" />
              {{ t('payment.admin.retryRefund') }}
            </button>
            <button v-else-if="row.payment_type !== 'squarespace' && row.status === 'REFUND_PENDING'" :disabled="refundQueryingIds.has(row.id)" @click="handleQueryRefund(row)" class="theme-action-button inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium disabled:opacity-60">
              <Icon name="refresh" size="sm" :class="refundQueryingIds.has(row.id) ? 'animate-spin' : ''" />
              {{ t('payment.admin.queryRefundStatus') }}
            </button>
            <button v-else-if="row.payment_type !== 'squarespace' && row.payment_type !== 'balance' && (row.status === 'COMPLETED' || row.status === 'PARTIALLY_REFUNDED')" @click="openRefundDialog(row)" class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20">
              <Icon name="dollar" size="sm" />
              {{ t('payment.admin.refund') }}
            </button>
          </div>
        </template>
      </OrderTable>
      <InfiniteListSentinel :loading="ordersLoading || ordersLoadingMore || ordersRefreshing" :error="ordersError" :has-more="hasMoreOrders" @load-more="loadMoreOrders" @retry="retryOrders" />
    </div>

    <!-- Order Detail Dialog -->
    <BaseDialog :show="showDetailDialog" :title="t('payment.admin.orderDetail')" width="wide" @close="showDetailDialog = false">
      <div v-if="selectedOrder" class="space-y-4">
        <div class="grid grid-cols-2 gap-4">
          <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</p><p class="font-mono text-sm font-medium text-gray-900 dark:text-white">#{{ selectedOrder.id }}</p></div>
          <div class="min-w-0"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</p><p class="break-all text-sm font-medium text-gray-900 dark:text-white">{{ selectedOrder.out_trade_no }}</p></div>
          <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.status') }}</p><OrderStatusBadge :status="selectedOrder.status" /></div>
          <AdminRetailOrderAmounts v-if="selectedOrder.retail_quote" :quote="selectedOrder.retail_quote" :order="selectedOrder" class="col-span-2" />
          <div v-else><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.amount') }}</p><p class="text-sm font-medium text-gray-900 dark:text-white">{{ creditedAmountSymbol }}{{ selectedOrder.amount.toFixed(2) }}</p></div>
          <div v-if="!selectedOrder.retail_quote"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</p><p class="text-sm font-medium text-gray-900 dark:text-white">{{ paymentAmountSymbol(selectedOrder) }}{{ selectedOrder.pay_amount.toFixed(2) }}</p></div>
          <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.paymentMethod') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ selectedOrder.payment_type === 'gelv' ? t('gelv.name') : selectedOrder.payment_type === 'squarespace' ? t('squarespaceProvider.name') : t('payment.methods.' + selectedOrder.payment_type, selectedOrder.payment_type) }}</p></div>
          <div v-if="!selectedOrder.retail_quote"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.feeRate') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ selectedOrder.fee_rate }}%</p></div>
          <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.createdAt') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrder.created_at) }}</p></div>
          <div v-if="!hasOrderPayment(selectedOrder)"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.expiresAt') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrder.expires_at) }}</p></div>
          <div v-if="selectedOrder.paid_at"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.paidAt') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrder.paid_at) }}</p></div>
          <div v-if="selectedOrder.completed_at"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.completedAt') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrder.completed_at) }}</p></div>
          <div v-if="selectedOrder.refund_amount"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.refundAmount') }}</p><p class="text-sm font-medium text-red-600 dark:text-red-400">{{ creditedAmountSymbol }}{{ selectedOrder.refund_amount.toFixed(2) }}</p></div>
          <div v-if="selectedOrder.refund_reason" class="col-span-2"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.refundReason') }}</p><p class="text-sm text-gray-700 dark:text-gray-300">{{ selectedOrder.refund_reason }}</p></div>
          <!-- Refund request info -->
          <div v-if="selectedOrder.refund_requested_at" class="col-span-2 border-t border-gray-200 pt-3 dark:border-dark-600">
            <p class="mb-2 text-xs font-medium text-purple-600 dark:text-purple-400">{{ t('payment.admin.refundRequestInfo') }}</p>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.refundRequestedAt') }}</p>
                <p class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrder.refund_requested_at) }}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.refundRequestedBy') }}</p>
                <p class="text-sm text-gray-700 dark:text-gray-300">#{{ selectedOrder.refund_requested_by }}</p>
              </div>
              <div class="col-span-2">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.refundRequestReason') }}</p>
                <p class="text-sm text-gray-700 dark:text-gray-300">{{ selectedOrder.refund_request_reason }}</p>
              </div>
            </div>
          </div>
        </div>
        <!-- Audit Logs -->
        <div v-if="orderAuditLogs.length > 0" class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <p class="mb-2 text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('payment.admin.auditLogs') }}</p>
          <div class="max-h-48 space-y-2 overflow-y-auto">
            <div v-for="log in displayAuditLogs" :key="log.id" class="rounded-lg border border-gray-100 bg-gray-50 p-2.5 dark:border-dark-600 dark:bg-dark-800">
              <div class="flex items-center justify-between">
                <span class="text-xs font-medium text-gray-700 dark:text-gray-300">{{ t(log.titleKey) }}</span>
                <span class="text-xs text-gray-400">{{ formatDateTime(log.created_at) }}</span>
              </div>
              <p v-if="log.summaryKey" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t(log.summaryKey, log.params) }}</p>
              <details class="mt-1 text-xs text-gray-500 dark:text-gray-400" data-testid="audit-technical-detail">
                <summary class="cursor-pointer">{{ t('payment.admin.detail.technicalDetails') }}</summary>
                <p class="mt-1 break-all">{{ log.action }}</p>
                <pre v-if="log.detail" class="mt-1 whitespace-pre-wrap break-all font-mono">{{ formatAuditDetail(log.detail) }}</pre>
                <p v-if="log.operator" class="mt-1">{{ t('payment.admin.operator') }}: {{ log.operator }}</p>
              </details>
            </div>
          </div>
        </div>
      </div>
    </BaseDialog>

    <AdminRefundDialog :show="showRefundDialog" :order="refundOrder" :submitting="refundSubmitting" :require-force="refundRequireForce" :warning="refundWarning" @confirm="handleRefund" @cancel="closeRefundDialog" />
  </AppLayout>
</template>

<script setup lang="ts">
import { provide, ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { objectQueryId } from '../objectContext'
import ConsoleObjectLinks from '@/components/admin/ConsoleObjectLinks.vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatOrderDateTime } from '@/components/payment/orderUtils'
import type { PaymentOrder } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useConfirmedAction, useConfirmationContext } from '@/composables/useConfirmation'
import { useInfiniteList } from '@/composables/useInfiniteList'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import AdminRefundDialog from '@/components/admin/payment/AdminRefundDialog.vue'
import AdminRetailOrderAmounts from '@/components/admin/payment/AdminRetailOrderAmounts.vue'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import OrderTable from '@/components/payment/OrderTable.vue'
import { currencySymbol } from '@/components/payment/currency'

import { hasOrderPayment, summarizeOrderAuditLog, formatAuditDetail, type OrderAuditLog } from '@/components/admin/payment/adminOrderDetail'

const { t } = useI18n()
const tableToolbarTarget = ref<HTMLElement | null>(null)
provide('tableToolbarTarget', tableToolbarTarget)
const appStore = useAppStore()

const route = useRoute()
const contextUserId = computed(() => objectQueryId(route?.query?.user_id))
const orderSearch = ref('')
const appliedOrderSearch = ref('')
const orderFilters = reactive({ status: '', payment_type: '', order_type: '' })
const selectedOrder = ref<PaymentOrder | null>(null)
const showDetailDialog = ref(false)
const showRefundDialog = ref(false)
const refundOrder = ref<PaymentOrder | null>(null)
const refundRequireForce = ref(false)
const refundWarning = ref('')
const refundQueryingIds = ref(new Set<number>())
const orderAuditLogs = ref<OrderAuditLog[]>([])
const displayAuditLogs = computed(() => orderAuditLogs.value.map(log => ({
  ...log, ...summarizeOrderAuditLog(log, selectedOrder.value),
})))
const creditedAmountSymbol = currencySymbol('USD')

function paymentAmountSymbol(order: PaymentOrder | null | undefined): string {
  return currencySymbol(order?.currency)
}

let debounceTimer: ReturnType<typeof setTimeout> | null = null
function debounceLoadOrders() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => { appliedOrderSearch.value = orderSearch.value; void resetOrders() }, 300)
}

const {
  items: orders, loading: ordersLoading, loadingMore: ordersLoadingMore,
  refreshing: ordersRefreshing, error: ordersError, hasMore: hasMoreOrders,
  reset: resetOrders, refresh: loadOrders, loadMore: loadMoreOrders, retry: retryOrders,
} = useInfiniteList<PaymentOrder>({
  pageSize: 20,
  keyOf: order => order.id,
  fetchPage: async page => {
    const res = await adminPaymentAPI.getOrders({
      page, page_size: 20,
      user_id: contextUserId.value,
      keyword: appliedOrderSearch.value || undefined, status: orderFilters.status || undefined,
      payment_type: orderFilters.payment_type || undefined, order_type: orderFilters.order_type || undefined,
    })
    return { items: res.data.items || [], total: res.data.total }
  },
})
watch(contextUserId, () => { void resetOrders() })
watch(ordersError, error => {
  if (error) appStore.showError(extractI18nErrorMessage(error, t, 'payment.errors', t('common.error')))
})
onUnmounted(() => { if (debounceTimer) clearTimeout(debounceTimer) })

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

async function showOrderDetail(order: PaymentOrder) {
  selectedOrder.value = order
  orderAuditLogs.value = []
  showDetailDialog.value = true
  try {
    const res = await adminPaymentAPI.getOrder(order.id)
    if (selectedOrder.value?.id !== order.id || !showDetailDialog.value) return
    const data = res.data as unknown as Record<string, unknown>
    if (data.order) selectedOrder.value = data.order as PaymentOrder
    orderAuditLogs.value = ((data.auditLogs || data.audit_logs || []) as unknown) as OrderAuditLog[]
  } catch (_err: unknown) { /* keep cached order data */ }
}

async function handleCancelOrder(order: PaymentOrder) {
  try { await adminPaymentAPI.cancelOrder(order.id); appStore.showSuccess(t('payment.admin.orderCancelled')); loadOrders() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

async function handleRetryOrder(order: PaymentOrder) {
  try { await adminPaymentAPI.retryRecharge(order.id); appStore.showSuccess(t('payment.admin.retrySuccess')); loadOrders() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

function openRefundDialog(order: PaymentOrder) {
  refundOrder.value = { ...order }
  refundRequireForce.value = false
  refundWarning.value = ''
  showRefundDialog.value = true
}

function closeRefundDialog() {
  if (refundSubmitting.value) return
  resetRefundDialog()
}

function resetRefundDialog() {
  refundOrder.value = null
  showRefundDialog.value = false
  refundRequireForce.value = false
  refundWarning.value = ''
}

function isRefundPendingWarning(warning: string | undefined): boolean {
  return /pending|处理中|待/.test(String(warning || '').toLowerCase())
}

function handleRefund(data: { amount: number; reason: string; deduct_balance: boolean; force: boolean }, orderId: number) {
  if (refundOrder.value?.id !== orderId) return
  const payload = { amount: data.amount, reason: data.reason, deduct_balance: data.deduct_balance, force: data.force }
  return refundAction.run(async (isCurrent) => {
    try {
      const res = await adminPaymentAPI.refundOrder(orderId, payload)
      if (!isCurrent()) return
      if (res.data.success || isRefundPendingWarning(res.data.warning)) {
        appStore.showSuccess(t(res.data.success ? 'payment.admin.refundSuccess' : 'payment.admin.refundPending'))
        resetRefundDialog()
        loadOrders()
        return
      }
      if (res.data.require_force) {
        refundRequireForce.value = true
        refundWarning.value = res.data.warning || ''
        return
      }
      appStore.showError(res.data.warning || t('common.error'))
    } catch (err: unknown) {
      if (isCurrent()) appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    }
  })
}

async function handleQueryRefund(order: PaymentOrder) {
  refundQueryingIds.value = new Set(refundQueryingIds.value).add(order.id)
  try {
    const res = await adminPaymentAPI.queryRefund(order.id)
    if (res.data.success) {
      appStore.showSuccess(t('payment.admin.refundSuccess'))
    } else if (isRefundPendingWarning(res.data.warning)) {
      appStore.showSuccess(t('payment.admin.refundPending'))
    } else {
      appStore.showError(res.data.warning || t('common.error'))
    }
    loadOrders()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    const next = new Set(refundQueryingIds.value)
    next.delete(order.id)
    refundQueryingIds.value = next
  }
}

function formatDateTime(dateStr: string): string { return formatOrderDateTime(dateStr) }

onMounted(() => resetOrders())
const refundAction = useConfirmedAction(() => ({ active: showRefundDialog.value, identity: refundOrder.value?.id }))
const refundSubmitting = refundAction.busy
const refundSession = useConfirmationContext(() => ({ active: true, identity: contextUserId.value }))
watch(refundSession, resetRefundDialog, { deep: true, flush: 'sync' })
</script>
