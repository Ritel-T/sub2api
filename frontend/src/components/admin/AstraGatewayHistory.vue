<template>
  <section class="open-section space-y-3" aria-labelledby="astra-history-title">
    <h2 id="astra-history-title" class="font-semibold">{{ t(`${p}.historyTitle`) }}</h2>
    <p class="text-xs text-gray-500">{{ t(`${p}.historyHint`) }}</p>
    <form class="flex flex-wrap items-center gap-3" @submit.prevent="search">
      <input v-model="host" class="input w-72" :placeholder="t(`${p}.historySearch`)" :aria-label="t(`${p}.historySearch`)" maxlength="200" />
      <label class="flex items-center gap-2 text-sm"><input v-model="passed" type="checkbox" @change="search" />{{ t(`${p}.historyPassed`) }}</label>
      <button class="btn btn-secondary btn-sm" :disabled="loading">{{ t(`${p}.historyQuery`) }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ t(`${p}.historyError`) }}</p>
    <p class="text-sm">{{ t(`${p}.historyCount`, { n: uniqueGateways, records: historyList.total.value ?? 0 }) }}</p>
    <div class="overflow-x-auto">
      <table class="min-w-[640px] w-full text-left text-sm">
        <thead><tr class="border-b dark:border-dark-600"><th class="p-2">{{ t(`${p}.gateway`) }}</th><th class="p-2">{{ t(`${p}.sources`) }}</th><th class="p-2">{{ t(`${p}.targets`) }}</th><th class="p-2">{{ t(`${p}.historyResults`) }}</th><th class="p-2">{{ t(`${p}.historyLastPass`) }}</th><th class="p-2">{{ t(`${p}.historyLastFailure`) }}</th><th class="p-2">{{ t(`${p}.historyLastResult`) }}</th></tr></thead>
        <tbody><tr v-for="row in items" :key="`${row.gateway}/${row.source_account_id}/${row.target_account_id}`" class="border-b dark:border-dark-600"><td class="p-2 font-mono text-xs">{{ row.gateway }}</td><td class="p-2">#{{ row.source_account_id }}</td><td class="p-2">{{ row.target_account_id ? `#${row.target_account_id}` : t(`${p}.historySourceOnly`) }}</td><td class="p-2">{{ row.passes }} / {{ row.failures }}</td><td class="p-2">{{ clock(row.last_pass) }}</td><td class="p-2">{{ clock(row.last_failure) }}</td><td class="p-2">{{ reason(row.last_reason) }}<span v-if="row.last_answer"> · {{ row.last_answer }}</span></td></tr></tbody>
      </table>
    </div>
    <p v-if="!loading && !items.length && !error" class="text-sm text-gray-500">{{ t(`${p}.historyEmpty`) }}</p>
    <InfiniteListSentinel :loading="loading || historyList.loadingMore.value || historyList.refreshing.value" :error="error" :has-more="historyList.hasMore.value" @load-more="historyList.loadMore" @retry="historyList.retry" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAstraGatewayHistory, type AstraGatewayHistoryRecord } from '@/api/admin/astraGateway'
import { useInfiniteList } from '@/composables/useInfiniteList'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
const { t, te } = useI18n()
const p = 'admin.astraGateway'
const host = ref(''), passed = ref(true), uniqueGateways = ref(0)
let query = { host: '', passed: true }
const historyList = useInfiniteList<AstraGatewayHistoryRecord>({
  pageSize: 20,
  keyOf: row => `${row.gateway}/${row.source_account_id}/${row.target_account_id}`,
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
