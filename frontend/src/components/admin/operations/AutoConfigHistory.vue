<template>
  <section class="open-section" aria-labelledby="auto-config-history-title" data-testid="auto-config-history" :aria-busy="loading">
    <header class="flex flex-wrap items-start justify-between gap-4 p-5">
      <div class="min-w-0">
        <AppHeading as="h2" variant="section" id="auto-config-history-title" >{{ t('autoConfig.logs.title') }}</AppHeading>
        <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('autoConfig.logs.description') }}</p>
      </div>
      <div class="workspace-filter-controls flex w-full flex-wrap items-center gap-3 sm:w-auto">
        <div ref="tableToolbarTarget" class="flex items-center gap-2" />
        <label data-field-role="filter" class="block min-w-0 min-w-0 flex-1 sm:w-40">
          <span class="sr-only">{{ t('autoConfig.logs.filter') }}</span>
          <select v-model="kind" class="input min-w-0 flex-1 sm:w-40" :aria-label="t('autoConfig.logs.filter')" data-testid="history-kind">
          <option value="">{{ t('autoConfig.logs.all') }}</option>
          <option v-for="value in kinds" :key="value" :value="value">{{ t('autoConfig.logs.kinds.' + value) }}</option>
        </select>
        </label>
        <button type="button" class="btn btn-secondary shrink-0" :disabled="loading" data-testid="history-refresh" @click="historyList.refresh()">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('autoConfig.logs.refresh') }}
        </button>
      </div>
    </header>
    <div v-if="error" role="alert" class="mx-5 mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
      <span>{{ error }}</span>
      <button type="button" class="font-medium underline" :disabled="loading" data-testid="history-retry" @click="historyList.retry()">{{ t('autoConfig.retry') }}</button>
    </div>
    <DataTable :columns="columns" :data="events" :loading="loading && !events.length" row-key="id" column-order-key="admin-auto-config-history" :toolbar-target="tableToolbarTarget" :row-attrs="() => ({ 'data-testid': 'history-row' })">
      <template #cell-created_at="{ row }"><time class="text-xs tabular-nums text-gray-500 dark:text-gray-400" :datetime="row.created_at">{{ formatDateTime(row.created_at) }}</time></template>
      <template #cell-account="{ row }"><p class="break-words font-medium">{{ row.account_id ? row.account_name || '#' + row.account_id : t('autoConfig.logs.global') }}</p><p class="mt-1 text-xs text-gray-400">{{ row.platform }}<span v-if="row.account_id"> · #{{ row.account_id }}</span></p></template>
      <template #cell-kind="{ row }"><span class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium" :class="badgeClass(row.kind)">{{ t('autoConfig.logs.kinds.' + row.kind) }}</span></template>
      <template #cell-detail="{ row }">
        {{ details(row) }}
        <p v-if="!(row.kind === 'config_saved' && nativeDefaultsFor(row.details.config)) && row.details.model_mapping && Object.keys(row.details.model_mapping).length" class="mt-1 break-words text-xs">{{ t('autoConfig.mapping.applied') }}：{{ Object.entries(row.details.model_mapping).map(([from, to]) => from + ' → ' + to).join(' / ') }}</p>
                <details v-if="row.kind === 'config_saved' && row.details.config" class="mt-1 text-xs" @click.stop>
                  <summary class="w-fit cursor-pointer text-primary-600 dark:text-primary-400">{{ t('autoConfig.logs.snapshot') }}</summary>
                  <div class="mt-2 space-y-1 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
                    <template v-if="row.details.config.model_billing">
                      <p>{{ t('autoConfig.modelBilling.title') }}：{{ t(row.details.config.model_billing.enabled ? 'autoConfig.logs.on' : 'autoConfig.logs.off') }}</p>
                      <p v-for="rule in row.details.config.model_billing.rules" :key="rule.model" class="break-all">{{ rule.model }} · {{ rule.multiplier }}× · {{ rule.group_ids?.length ? t('autoConfig.modelBilling.groups') + '：' + groupIDs(rule.group_ids) : t('autoConfig.modelBilling.allGroups') }}</p>
                    </template>
                    <template v-if="nativeDefaultsFor(row.details.config)">
                      <p>{{ t('autoConfig.accountDefaults.title') }}：</p>
                      <template v-for="(template, plan) in nativeDefaultsFor(row.details.config)?.templates ?? {}" :key="plan">
                        <p class="break-words">{{ planLabel(String(plan)) }} · {{ t('autoConfig.accountDefaults.summary', { concurrency: template.concurrency, load: template.load_factor ?? t('autoConfig.accountDefaults.preserve'), priority: template.priority }) }}</p>
                        <p v-for="rule in template.model_mappings" :key="rule.from" class="break-words">{{ rule.from }} → {{ rule.to }}</p>
                      </template>
                      <p class="break-words">{{ t('autoConfig.accountDefaults.countries') }}：{{ nativeDefaultsFor(row.details.config)?.dedicated_proxy_country_codes?.join(' / ') || t('autoConfig.logs.importedProxy') }}</p>
                    </template>
                    <template v-else>
                      <p v-if="row.details.config.model_mappings?.length">{{ t('autoConfig.mapping.title') }}：</p>
                      <p v-for="rule in row.details.config.model_mappings ?? []" :key="rule.from" class="break-words">{{ rule.from }} → {{ rule.to }}</p>
                    </template>
                    <p>{{ t('autoConfig.groups') }}：{{ groupIDs(row.details.config.group_ids) }}</p>
                    <p>{{ t('autoConfig.upgradeGroups') }}：{{ groupIDs(row.details.config.upgrade_group_ids) }}</p>
                    <p>{{ t('autoConfig.logs.savedRule', { count: row.details.config.successes_per_step, step: row.details.config.upgrade_step, max: row.details.config.max_concurrency, seconds: row.details.config.cooldown_seconds }) }}</p>
                  </div>
                </details>
      </template>
      <template #empty><div v-if="!error" class="px-5 py-12 text-center" data-testid="history-empty">
        <Icon name="clock" size="lg" class="mx-auto mb-3 text-gray-400" />
        <p class="font-medium text-gray-700 dark:text-gray-200">{{ t(kind ? 'autoConfig.logs.emptyFiltered' : 'autoConfig.logs.empty') }}</p>
        <p class="mt-2 text-sm text-gray-500">{{ t('autoConfig.logs.emptyHint') }}</p>
      </div></template>
    </DataTable>
    <footer class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 px-5 py-4 text-xs text-gray-400 dark:border-dark-700">
      <span>{{ t('autoConfig.logs.scopeHint') }}</span>
      <InfiniteListSentinel :loading="loading" :error="historyList.error.value" :has-more="historyList.hasMore.value" :progress="events.length" @load-more="historyList.loadMore" @retry="historyList.retry" />
    </footer>
  </section>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import DataTable from '@/components/common/DataTable.vue'
import { computed, ref, watch } from 'vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import { OPENAI_TEMPLATE_PLANS } from '@/utils/openaiAccountDefaults'
import { getAutoConfigEvents, type AutoConfig, type AutoConfigEvent, type AutoConfigEventKind } from '@/api/admin/autoConfig'

const props = defineProps<{ refreshKey: number }>()
const { t } = useI18n()
const auth = useAuthStore()
const tableToolbarTarget = ref<HTMLElement | null>(null)
const columns = computed(() => [
  { key: 'created_at', label: t('autoConfig.logs.time') },
  { key: 'account', label: t('autoConfig.logs.account') },
  { key: 'kind', label: t('autoConfig.logs.event') },
  { key: 'detail', label: t('autoConfig.logs.detail') }
].map(column => ({ ...column, primary: true })))
const kinds: AutoConfigEventKind[] = ['config_saved', 'initial_applied', 'concurrency_upgraded', 'failure_cooldown']
const kind = ref<AutoConfigEventKind | ''>('')
// Keep the API cursor behind the shared page/refresh lifecycle.
const cursors = new Map<number, number | undefined>([[1, undefined]])
const historyList = useInfiniteList<AutoConfigEvent>({
  pageSize: 20,
  keyOf: event => event.id,
  fetchPage: async (page, signal) => {
    if (auth.user?.role !== 'admin') return { items: [], pages: page }
    const result = await getAutoConfigEvents({ limit: 20, kind: kind.value, before: cursors.get(page) })
    if (!signal.aborted) cursors.set(page + 1, result.items.at(-1)?.id)
    return { items: result.items, pages: result.has_more ? Number.MAX_SAFE_INTEGER : page }
  }
})
const events = historyList.items
const loading = computed(() => historyList.loading.value || historyList.loadingMore.value || historyList.refreshing.value)
const error = computed(() => historyList.error.value ? t('autoConfig.logs.loadFailed') : '')
function resetHistory() {
  cursors.clear()
  cursors.set(1, undefined)
  historyList.clear()
  if (auth.user?.role === 'admin') void historyList.reset()
}
watch(() => auth.user ? auth.user.id + ':' + auth.user.role : '', resetHistory, { immediate: true, flush: 'sync' })
watch(kind, resetHistory, { flush: 'sync' })
watch(() => props.refreshKey, () => {
  if (auth.user?.role === 'admin') void historyList.refresh()
})

function badgeClass(value: AutoConfigEventKind) {
  if (value === 'failure_cooldown') return 'bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300'
  if (value === 'concurrency_upgraded') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300'
  if (value === 'initial_applied') return 'bg-blue-50 text-blue-700 dark:bg-blue-950/30 dark:text-blue-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}
function details(event: AutoConfigEvent) {
  const d = event.details
  if (event.kind === 'config_saved' && d.config) {
    const native = nativeDefaultsFor(d.config)
    const switches = { initial: t(d.config.enabled ? 'autoConfig.logs.on' : 'autoConfig.logs.off'), upgrade: t(d.config.upgrade_enabled ? 'autoConfig.logs.on' : 'autoConfig.logs.off') }
    if (native) return t('autoConfig.logs.savedPlanDetail', { ...switches, count: Object.keys(native.templates ?? {}).length })
    return t('autoConfig.logs.savedDetail', { ...switches, priority: d.config.priority, load: d.config.load_factor, concurrency: d.config.concurrency })
  }
  if (event.kind === 'initial_applied') return t('autoConfig.logs.initialDetail', { priority: d.priority, load: d.load_factor, concurrency: d.concurrency, groups: groupIDs(d.group_ids) })
  if (event.kind === 'concurrency_upgraded') return t('autoConfig.logs.upgradeDetail', { before: d.previous_concurrency, after: d.concurrency, seconds: d.cooldown_seconds })
  if (event.kind === 'failure_cooldown') return t('autoConfig.logs.cooldownDetail', { concurrency: d.concurrency, seconds: d.cooldown_seconds })
  return '—'
}
function nativeDefaultsFor(config?: AutoConfig) {
  return config?.platform === 'openai' && config.openai_account_defaults?.enabled ? config.openai_account_defaults : null
}
function planLabel(plan: string) {
  return OPENAI_TEMPLATE_PLANS.includes(plan as typeof OPENAI_TEMPLATE_PLANS[number]) ? t('autoConfig.accountDefaults.plans.' + plan) : plan
}
function groupIDs(ids?: number[]) {
  return (ids ?? []).map(id => '#' + id).join(' / ') || '—'
}
</script>
