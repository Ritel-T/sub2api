<template>
  <div data-test="squarespace-receipt-claim" class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <button v-if="!expanded" data-test="start-receipt-claim" class="btn btn-secondary w-full" @click="expanded = true">{{ t('paymentRetail.claim.start') }}</button>
    <template v-else>
      <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('paymentRetail.claim.title') }}</h3>
      <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('paymentRetail.claim.hint') }}</p>
      <p v-if="accountEmail" class="break-all text-sm font-medium">{{ t('paymentRetail.balanceAccount') }}: {{ accountEmail }}</p>
      <div v-if="checking" class="space-y-2 text-sm text-gray-600 dark:text-gray-300" role="status">
        <p>{{ t('paymentRetail.creditProcessing') }}</p>
        <button class="btn btn-secondary" @click="emit('refresh')">{{ t('paymentRetail.claim.refreshStatus') }}</button>
      </div>
      <template v-else>
        <label :for="`receipt-number-${orderId}`" class="block text-sm font-medium">{{ t('paymentRetail.claim.receiptNumber') }}</label>
        <input :id="`receipt-number-${orderId}`" v-model="receiptNumber" data-test="receipt-order-number" class="input w-full" maxlength="64" autocomplete="off" :disabled="verifying" :placeholder="t('paymentRetail.claim.receiptPlaceholder')" />
        <label :for="`payer-email-${orderId}`" class="block text-sm font-medium">{{ t('paymentRetail.claim.payerEmail') }}</label>
        <input :id="`payer-email-${orderId}`" v-model="payerEmail" data-test="receipt-payer-email" type="email" class="input w-full" maxlength="254" autocomplete="email" :disabled="verifying" :placeholder="t('paymentRetail.claim.payerEmailPlaceholder')" />
        <p class="text-xs leading-5 text-gray-500">{{ t('paymentRetail.claim.payerEmailHint') }}</p>
        <button data-test="request-claim-code" class="btn btn-secondary w-full" :disabled="requesting || verifying || cooldown > 0 || !receiptNumber.trim() || !validPayerEmail" @click="requestCode">
          {{ requesting ? t('common.processing') : cooldown > 0 ? t('paymentRetail.claim.resendIn', { seconds: cooldown }) : t(challengeToken ? 'paymentRetail.claim.resend' : 'paymentRetail.claim.requestCode') }}
        </button>
        <p v-if="sent" class="text-xs leading-5 text-gray-500" role="status">{{ t('paymentRetail.claim.codeRequested') }}</p>
        <template v-if="challengeToken">
          <label :for="`claim-code-${orderId}`" class="block text-sm font-medium">{{ t('paymentRetail.claim.code') }}</label>
          <input :id="`claim-code-${orderId}`" v-model="code" data-test="receipt-claim-code" class="input w-full font-mono" maxlength="6" inputmode="numeric" autocomplete="one-time-code" :disabled="verifying" />
          <p v-if="challengeExpired" class="text-sm text-amber-600">{{ t('paymentRetail.claim.codeExpired') }}</p>
          <button data-test="confirm-receipt-claim" class="btn btn-primary w-full" :disabled="!canClaim" @click="verifyClaim">{{ verifying ? t('common.processing') : t('paymentRetail.claim.confirm') }}</button>
        </template>
      </template>
      <p v-if="errorKey" class="text-sm leading-6 text-amber-700 dark:text-amber-300" role="alert">{{ t(errorKey) }}</p>
      <details class="text-xs leading-5 text-gray-500">
        <summary class="cursor-pointer select-none">{{ t('paymentRetail.claim.paidOnTitle') }}</summary>
        <p class="mt-2">{{ t('paymentRetail.claim.paidOnHint') }}</p>
      </details>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { paymentAPI } from '@/api/payment'
import type { PaymentOrder } from '@/types/payment'

const props = defineProps<{ orderId: number; accountEmail?: string }>()
const emit = defineEmits<{ updated: [order: PaymentOrder]; refresh: [] }>()
const { t } = useI18n()
const expanded = ref(false)
const receiptNumber = ref('')
const payerEmail = ref('')
const validPayerEmail = computed(() => /^[^\s<>@]+@[^\s<>@]+\.[^\s<>@]+$/.test(payerEmail.value.trim()))
const code = ref('')
const challengeToken = ref('')
const challengeExpiresAt = ref(0)
const resendAt = ref(0)
const now = ref(Date.now())
const requesting = ref(false)
const verifying = ref(false)
const checking = ref(false)
const sent = ref(false)
const errorKey = ref('')
let revision = 0
let clock: ReturnType<typeof setInterval> | null = null
const cooldown = computed(() => Math.max(0, Math.ceil((resendAt.value - now.value) / 1000)))
const challengeExpired = computed(() => !!challengeToken.value && challengeExpiresAt.value <= now.value)
const canClaim = computed(() => !!challengeToken.value && /^\d{6}$/.test(code.value.trim()) && !challengeExpired.value && !verifying.value && !requesting.value)

function safeErrorKey(error: unknown): string {
  const reason = error && typeof error === 'object' && 'reason' in error ? String(error.reason) : ''
  if (reason === 'SQUARESPACE_CLAIM_UNAVAILABLE') return 'paymentRetail.claim.unavailable'
  if (reason === 'SQUARESPACE_CLAIM_CODE_INVALID') return 'paymentRetail.claim.invalidCode'
  if (reason === 'SQUARESPACE_CLAIM_RATE_LIMITED') return 'paymentRetail.claim.rateLimited'
  return 'paymentRetail.claim.serviceUnavailable'
}

watch([receiptNumber, payerEmail, () => props.orderId, () => props.accountEmail], () => {
  ++revision
  challengeToken.value = ''
  challengeExpiresAt.value = 0
  code.value = ''
  sent.value = false
  errorKey.value = ''
  checking.value = false
}, { flush: 'sync' })

async function requestCode() {
  if (requesting.value || verifying.value || resendAt.value > Date.now() || !receiptNumber.value.trim() || !validPayerEmail.value || props.orderId <= 0) return
  const currentRevision = revision
  const receipt = receiptNumber.value.trim()
  requesting.value = true
  errorKey.value = ''
  try {
    const response = await paymentAPI.requestSquarespaceClaimCode(props.orderId, receipt, payerEmail.value.trim())
    if (currentRevision !== revision) return
    const challenge = response.data
    const expiry = Date.parse(challenge?.expires_at || '')
    if (!challenge?.challenge_token || !Number.isFinite(expiry) || expiry <= Date.now()) throw new Error('invalid challenge response')
    challengeToken.value = challenge.challenge_token
    challengeExpiresAt.value = expiry
    code.value = ''
    now.value = Date.now()
    const delay = Number.isFinite(challenge.resend_after_seconds) ? Math.max(0, Math.min(300, challenge.resend_after_seconds)) : 60
    resendAt.value = Date.now() + delay * 1000
    sent.value = true
  } catch (error: unknown) {
    if (currentRevision !== revision) return
    errorKey.value = safeErrorKey(error)
    if (errorKey.value === 'paymentRetail.claim.rateLimited') resendAt.value = Date.now() + 60000
  } finally { requesting.value = false }
}

async function verifyClaim() {
  if (!canClaim.value || challengeExpiresAt.value <= Date.now()) return
  verifying.value = true
  errorKey.value = ''
  const currentRevision = revision
  try {
    const response = await paymentAPI.claimSquarespacePayment(challengeToken.value, code.value.trim())
    if (currentRevision !== revision) return
    const order = response.data
    if (!order || order.id !== props.orderId || order.payment_type !== 'squarespace' || !['PENDING', 'PAID', 'RECHARGING', 'COMPLETED'].includes(order.status)) {
      throw new Error('invalid claim result')
    }
    challengeToken.value = ''
    code.value = ''
    checking.value = order.status !== 'COMPLETED'
    emit('updated', order)
  } catch (error: unknown) {
    if (currentRevision !== revision) return
    errorKey.value = safeErrorKey(error)
    emit('refresh')
  } finally { verifying.value = false }
}

onMounted(() => { clock = setInterval(() => { now.value = Date.now() }, 1000) })
onBeforeUnmount(() => { ++revision; if (clock) clearInterval(clock) })
</script>
