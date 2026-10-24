<template>
  <section class="open-section space-y-3" aria-labelledby="astra-history-title" :aria-busy="loading || historyList.loadingMore.value || historyList.refreshing.value">
    <AppHeading as="h2" variant="section" id="astra-history-title" >{{ t(`${p}.historyTitle`) }}</AppHeading>
    <p class="text-xs text-gray-500">{{ t(`${p}.historyHint`) }}</p>
    <form class="workspace-filter-controls flex flex-wrap items-center gap-3" @submit.prevent="search">
      <input v-model="host" class="input w-72" :placeholder="t(`${p}.historySearch`)" :aria-label="t(`${p}.historySearch`)" maxlength="200" />
      <label class="flex items-center gap-2 text-sm"><input v-model="passed" type="checkbox" @change="search" />{{ t(`${p}.historyPassed`) }}</label>
      <button class="btn btn-secondary btn-sm" :disabled="loading">{{ t(`${p}.historyQuery`) }}</button>
      <div ref="tableToolbarTarget" class="flex items-center gap-2" />
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ t(`${p}.historyError`) }}</p>
    <p class="text-sm">{{ t(`${p}.historyCount`, { n: uniqueGateways, records: historyList.total.value ?? 0 }) }}</p>
    <DataTable :columns="columns" :data="items" :loading="loading && !items.length" :row-key="historyRowKey" column-order-key="admin-astra-gateway-history" :toolbar-target="tableToolbarTarget" :row-attrs="() => ({ 'data-testid': 'astra-history-row' })">
      <template #cell-gateway="{ row }"><code class="break-all text-xs">{{ row.gateway }}</code></template>
      <template #cell-source_account_id="{ row }">#{{ row.source_account_id }}</template>
      <template #cell-target_account_id="{ row }">{{ row.target_account_id ? `#${row.target_account_id}` : t(`${p}.historySourceOnly`) }}</template>
      <template #cell-results="{ row }">{{ row.passes }} / {{ row.failures }}</template>
      <template #cell-last_pass="{ row }">{{ clock(row.last_pass) }}</template>
      <template #cell-last_failure="{ row }">{{ clock(row.last_failure) }}</template>
      <template #cell-last_result="{ row }">{{ reason(row.last_reason) }}<span v-if="row.last_answer"> · {{ row.last_answer }}</span></template>
      <template #empty><p v-if="!error" class="text-sm text-gray-500">{{ t(`${p}.historyEmpty`) }}</p></template>
    </DataTable>
    <InfiniteListSentinel :loading="loading || historyList.loadingMore.value || historyList.refreshing.value" :error="error" :has-more="historyList.hasMore.value" @load-more="historyList.loadMore" @retry="historyList.retry" />
  </section>
</template>
<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import DataTable from '@/components/common/DataTable.vue'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAstraGatewayHistory, type AstraGatewayHistoryRecord } from '@/api/admin/astraGateway'
import { useInfiniteList } from '@/composables/useInfiniteList'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
const { t, te } = useI18n()
const p = 'admin.astraGateway'
const tableToolbarTarget = ref<HTMLElement | null>(null)
const historyRowKey = (row: AstraGatewayHistoryRecord) => `${row.gateway}/${row.source_account_id}/${row.target_account_id}`
const columns = computed(() => [
  { key: 'gateway', label: t(`${p}.gateway`) },
  { key: 'source_account_id', label: t(`${p}.sources`) },
  { key: 'target_account_id', label: t(`${p}.targets`) },
  { key: 'results', label: t(`${p}.historyResults`) },
  { key: 'last_pass', label: t(`${p}.historyLastPass`) },
  { key: 'last_failure', label: t(`${p}.historyLastFailure`) },
  { key: 'last_result', label: t(`${p}.historyLastResult`) }
].map(column => ({ ...column, primary: true })))
const host = ref(''), passed = ref(true), uniqueGateways = ref(0)
let query = { host: '', passed: true }
const historyList = useInfiniteList<AstraGatewayHistoryRecord>({
  pageSize: 20,
  keyOf: historyRowKey,
  fetchPage: async (page, signal) => {
    const result = await getAstraGatewayHistory(query.host, query.passed, page, { signal })
    if (!signal.aborted) uniqueGateways.value = result.unique_gateways
    return result
  }
})
const { items, loading, error } = historyList
let timer: ReturnType<typeof setInterval> | undefined
function clock(value?: string) { return value ? new Date(value).toLocaleString() : '—' }
function reason(code: string) { const key = `${p}.reasons.${code}`; return te(key) ? t(key) : code }
function search() {
  query = { host: host.value.trim(), passed: passed.value }
  uniqueGateways.value = 0
  void historyList.reset()
}
onMounted(() => {
  search()
  timer = setInterval(() => {
    if (!loading.value && !historyList.loadingMore.value && !historyList.refreshing.value && !historyList.error.value) void historyList.refresh()
  }, 15000)
})
onUnmounted(() => clearInterval(timer))
</script>
