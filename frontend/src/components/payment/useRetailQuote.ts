import { isCurrentRetailQuote } from './retailQuote'
import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import { paymentAPI } from '@/api/payment'
import type { PaymentQuoteRequest, PaymentQuoteResponse } from '@/types/payment'

export function useRetailQuote(request: Ref<PaymentQuoteRequest | null>) {
  const response = ref<PaymentQuoteResponse | null>(null)
  const loading = ref(false)
  const failed = ref(false)
  const confirmed = ref(false)
  const now = ref(Date.now())
  let sequence = 0
  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let clockTimer: ReturnType<typeof setInterval> | null = null
  const expired = computed(() => !!response.value && Date.parse(response.value.retail_quote.expires_at) <= now.value)
  const quote = computed(() => response.value?.retail_quote ?? null)

  function current(): boolean {
    return !!response.value && Date.parse(response.value.retail_quote.expires_at) > Date.now()
  }

  async function refresh(): Promise<void> {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = null
    const input = request.value
    const requestSequence = ++sequence
    response.value = null
    confirmed.value = false
    failed.value = false
    loading.value = !!input
    if (!input) return
    try {
      const result = await paymentAPI.quote({ ...input })
      if (requestSequence !== sequence) return
      if (!result.data?.quote_token || !isCurrentRetailQuote(result.data.retail_quote)
        || result.data.retail_quote.credited_amount_usd !== input.amount
        || Date.parse(result.data.retail_quote.expires_at) <= Date.now()) {
        failed.value = true
        return
      }
      response.value = result.data
      now.value = Date.now()
    } catch {
      if (requestSequence === sequence) failed.value = true
    } finally {
      if (requestSequence === sequence) loading.value = false
    }
  }

  watch(request, () => {
    ++sequence
    if (debounceTimer) clearTimeout(debounceTimer)
    response.value = null
    confirmed.value = false
    failed.value = false
    loading.value = !!request.value
    if (request.value) debounceTimer = setTimeout(() => { void refresh() }, 300)
  }, { immediate: true })
  watch(expired, value => { if (value) confirmed.value = false })
  onMounted(() => { clockTimer = setInterval(() => { now.value = Date.now() }, 1000) })
  onBeforeUnmount(() => {
    ++sequence
    if (debounceTimer) clearTimeout(debounceTimer)
    if (clockTimer) clearInterval(clockTimer)
  })
  return { response, quote, loading, failed, confirmed, expired, current, refresh }
}
