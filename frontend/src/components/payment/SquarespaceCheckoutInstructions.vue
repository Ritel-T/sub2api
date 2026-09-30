<template>
  <div data-test="squarespace-instructions" class="card space-y-4 p-6">
    <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('paymentRetail.checkoutTitle') }}</h3>
    <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t(claimMode === 'receipt_otp' ? 'paymentRetail.receiptCheckoutHint' : claimMode === 'reference' ? 'paymentRetail.checkoutHint' : 'paymentRetail.claim.serviceUnavailable') }}</p>
    <p v-if="claimMode === 'receipt_otp' && accountEmail" class="break-all text-sm font-medium text-gray-900 dark:text-white">{{ accountEmail }}</p>
    <RetailQuoteSummary :quote="quote" />
    <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
      <div>
        <label class="mb-1 block text-sm font-medium">{{ t('paymentRetail.exactAmount') }}</label>
        <div class="flex gap-2">
          <input data-test="checkout-exact-amount" readonly :value="exactAmount" class="input min-w-0 flex-1 font-mono" @focus="selectValue" />
          <button data-test="copy-checkout-amount" class="btn btn-secondary" @click="copy(exactAmount, 'amount')">{{ t(amountCopied ? 'paymentRetail.copied' : 'paymentRetail.copyAmount') }}</button>
        </div>
      </div>
      <div v-if="claimMode === 'reference'">
        <label class="mb-1 block text-sm font-medium">{{ t('paymentRetail.reference') }}</label>
        <div class="flex gap-2">
          <input data-test="checkout-reference" readonly :value="quote.checkout_reference" class="input min-w-0 flex-1 font-mono text-xs" @focus="selectValue" />
          <button data-test="copy-checkout-reference" class="btn btn-secondary" @click="copy(quote.checkout_reference, 'reference')">{{ t(referenceCopied ? 'paymentRetail.copied' : 'paymentRetail.copyReference') }}</button>
        </div>
      </div>
      <p class="break-all text-xs text-gray-500">{{ t('payment.orders.orderNo') }}: {{ orderNumber }}</p>
      <label v-if="claimMode" class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
        <input v-model="confirmed" data-test="checkout-confirm" type="checkbox" class="mt-1" />
        <span>{{ t(claimMode === 'receipt_otp' ? 'paymentRetail.receiptCheckoutConfirm' : 'paymentRetail.checkoutConfirm') }}</span>
      </label>
      <button data-test="open-squarespace-checkout" class="btn btn-primary w-full" :disabled="!confirmed || !validPayUrl || quote.currency !== 'GBP' || !claimMode" @click="openCheckout">{{ t('paymentRetail.openCheckout') }}</button>
    </div>
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('paymentRetail.awaitingCredit') }}</p>
    <SquarespaceReceiptClaim v-if="claimMode === 'receipt_otp' && orderId" :order-id="orderId" :account-email="accountEmail" @updated="emit('updated', $event)" @refresh="emit('refresh')" />
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import type { RetailQuote, PaymentClaimMode, PaymentOrder } from '@/types/payment'
import RetailQuoteSummary from './RetailQuoteSummary.vue'
import { isValidSquarespacePayLink } from './providerConfig'
import SquarespaceReceiptClaim from './SquarespaceReceiptClaim.vue'
const props = defineProps<{ quote: RetailQuote; payUrl: string; orderNumber: string; orderId?: number; accountEmail?: string; paymentClaimMode?: PaymentClaimMode }>()
const emit = defineEmits<{ updated: [order: PaymentOrder]; refresh: [] }>()
const claimMode = computed(() => props.quote.payment_claim_mode || props.paymentClaimMode)
const { t } = useI18n()
const appStore = useAppStore()
const amountCopied = ref(false)
const referenceCopied = ref(false)
const confirmed = ref(false)
const exactAmount = computed(() => props.quote.pay_amount.toFixed(2))
const validPayUrl = computed(() => isValidSquarespacePayLink(props.payUrl))
function selectValue(event: Event) { (event.target as HTMLInputElement).select() }
async function copy(value: string, field: 'amount' | 'reference') {
  try {
    await navigator.clipboard.writeText(value)
    if (field === 'amount') amountCopied.value = true
    else referenceCopied.value = true
  } catch { appStore.showError(t('paymentRetail.copyFailed')) }
}
function openCheckout() {
  if (!confirmed.value || !validPayUrl.value || props.quote.currency !== 'GBP' || !claimMode.value) return
  window.open(props.payUrl, '_blank', 'noopener,noreferrer')
}
</script>
