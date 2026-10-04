import { computed, onUnmounted, ref, watch, type Ref } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { getAstraGatewayRuntime, type AstraRouteStatus } from '@/api/admin/astraGateway'
import { canonicalBorrowModel } from '@/utils/gatewayBorrowStatus'

// One runtime snapshot for the visible account table, never one request per row.
export function useGatewayBorrowRuntime(enabled: Ref<boolean>) {
  const rows = ref<AstraRouteStatus[]>([])
  const now = ref(Date.now())
  const loaded = ref(false)
  const failed = ref(false)
  let active = true
  let refreshing = false
  async function refresh() {
    if (!active || !enabled.value || refreshing || (typeof document !== 'undefined' && document.hidden)) return
    refreshing = true
    try {
      const runtime = await getAstraGatewayRuntime()
      if (active && enabled.value) { rows.value = runtime.targets || []; loaded.value = true; failed.value = false }
    } catch { if (active) { rows.value = []; failed.value = true; loaded.value = false } }
    finally { refreshing = false }
  }
  watch(enabled, value => { if (value) void refresh(); else { rows.value = []; loaded.value = false; failed.value = false } }, { immediate: true })
  useIntervalFn(() => { if (enabled.value) now.value = Date.now() }, 1000)
  useIntervalFn(() => { void refresh() }, 10000)
  onUnmounted(() => { active = false })
  const byAccount = computed(() => {
    const result: Record<string, AstraRouteStatus[]> = {}
    for (const row of rows.value) (result[String(row.account_id)] ||= []).push({ ...row, model: canonicalBorrowModel(row.model) })
    return result
  })
  return { byAccount, now, loaded, failed, refresh }
}
