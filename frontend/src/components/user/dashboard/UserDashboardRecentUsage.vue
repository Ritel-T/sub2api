<template>
  <section class="open-section" aria-labelledby="recent-title">
    <header class="flex flex-wrap items-center justify-between gap-3 pb-4">
      <AppHeading as="h2" variant="section" id="recent-title">{{ copy.recent }}</AppHeading>
      <router-link to="/usage?view=records" class="inline-flex min-h-[44px] items-center gap-2 no-underline">{{ copy.allRequests }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></router-link>
    </header>
    <div v-if="loading" class="py-6" role="status">{{ copy.loading }}</div>
    <div v-else-if="error" class="flex flex-wrap items-center gap-3 py-6" role="alert">
      <span>{{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="$emit('retry')">{{ copy.retry }}</button>
    </div>
    <p v-else-if="!rows.length" class="py-6 text-gray-500 dark:text-gray-400">{{ copy.noRequests }}</p>
    <table v-else class="recent-table">
      <thead><tr>
        <th scope="col">{{ copy.model }}</th><th scope="col">{{ copy.time }}</th>
        <th scope="col">{{ requestCopy.status }}</th><th scope="col">{{ requestCopy.duration }}</th>
        <th scope="col">{{ copy.tokensUnit }}</th><th scope="col">{{ copy.actual }}</th>
      </tr></thead>
      <tbody>
        <tr v-for="row in rows" :key="row.key" :data-request="row.key">
          <td class="recent-model">
            <span>{{ row.record.model || '—' }}</span>
            <span v-if="row.kind === 'usage' && visibleResponseModel(row.record)" class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('usage.upstreamResponseModel') }}: {{ visibleResponseModel(row.record) }}</span>
          </td>
          <td class="recent-time"><span class="recent-label">{{ copy.time }}</span><time :datetime="row.record.created_at">{{ formatDateTime(row.record.created_at) }}</time></td>
          <td><span class="recent-label">{{ requestCopy.status }}</span><span :class="row.kind === 'error' ? 'text-red-600 dark:text-red-400' : 'text-emerald-700 dark:text-emerald-400'">{{ row.kind === 'error' ? `${requestCopy.failed} · ${row.record.status_code || '—'}` : requestCopy.success }}</span></td>
          <td class="tabular-nums" data-metric="duration"><span class="recent-label">{{ requestCopy.duration }}</span><span>{{ row.kind === 'usage' ? overviewDuration(row.record.duration_ms) : '—' }}</span></td>
          <td class="tabular-nums" data-metric="tokens"><span class="recent-label">{{ copy.tokensUnit }}</span><span>{{ row.kind === 'usage' ? overviewTokenTotal(row.record).toLocaleString() : '—' }}</span></td>
          <td class="tabular-nums" data-metric="cost" :title="row.kind === 'usage' ? `${copy.standard}: $${row.record.total_cost.toFixed(4)}` : undefined"><span class="recent-label">{{ copy.actual }}</span><span>{{ row.kind === 'usage' ? `$${row.record.actual_cost.toFixed(4)}` : '—' }}</span></td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import AppHeading from '@/components/common/AppHeading.vue'
import Icon from '@/components/icons/Icon.vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import { userVisibleUpstreamResponseModel } from '@/utils/upstreamResponseModel'
import type { UsageLog, UserErrorRequest } from '@/types'
import { useOverviewCopy } from './overviewCopy'
import { overviewDuration, overviewTokenTotal, recentOverviewRequests } from './overviewRequests'
const props = defineProps<{ data: UsageLog[]; errors?: UserErrorRequest[]; loading: boolean; error?: boolean }>()
defineEmits<{ retry: [] }>()
const copy = useOverviewCopy()
const { t, locale } = useI18n()
const appStore = useAppStore()
const rows = computed(() => recentOverviewRequests(props.data, props.errors))
const requestCopy = computed(() => locale.value === 'zh'
  ? { status: '状态', duration: '耗时', success: '成功', failed: '失败' }
  : /^(zh|yue)/i.test(locale.value)
    ? { status: '狀態', duration: '耗時', success: '成功', failed: '失敗' }
    : { status: 'Status', duration: 'Duration', success: 'Success', failed: 'Failed' })
const visibleResponseModel = (log: UsageLog) => userVisibleUpstreamResponseModel(appStore.cachedPublicSettings, log)
</script>
<style scoped>
.recent-table{width:100%;table-layout:fixed;text-align:left;font-size:.875rem}
.recent-table th{color:var(--ritel-muted);font-weight:400;padding:.5rem .75rem .75rem}
.recent-table td{padding:1rem .75rem;vertical-align:top;overflow-wrap:anywhere}
.recent-table th:first-child,.recent-table td:first-child{padding-left:0;width:25%}
.recent-table th:nth-child(2){width:22%}
.recent-table th:last-child,.recent-table td:last-child{padding-right:0}
.recent-table tr{border-bottom:1px solid var(--ritel-border)}
.recent-table tbody tr:last-child{border-bottom:0}
.recent-label{display:none}
@media(max-width:767px){
  .recent-table,.recent-table tbody{display:block}
  .recent-table thead{display:none}
  .recent-table tbody tr{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:.75rem 1.5rem;padding:1rem 0}
  .recent-table td,.recent-table td:first-child,.recent-table td:last-child{display:flex;flex-direction:column;gap:.25rem;min-width:0;width:auto;padding:0}
  .recent-table .recent-model,.recent-table .recent-time{grid-column:1/-1}
  .recent-label{display:block;color:var(--ritel-muted)}
}
</style>
