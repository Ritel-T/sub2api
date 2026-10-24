<template>
  <section aria-labelledby="harvest-nodes-title">
    <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <div>
        <h3 id="harvest-nodes-title" class="sr-only">{{ t(`${prefix}.title`) }}</h3>
        <p class="text-[11px] text-gray-500 dark:text-gray-400">{{ t(`${prefix}.resetHint`) }} · {{ total ?? 0 }}</p>
      </div>
      <button type="button" data-testid="reset-all-nodes" class="btn btn-secondary btn-sm" :disabled="busy || !total" @click="reset(0)">{{ t(`${prefix}.resetAll`) }}</button>
    </div>
    <p v-if="error" role="alert" class="mb-3 text-sm text-red-600">{{ error }}</p>
    <div ref="listBody" class="divide-y divide-gray-200 dark:divide-dark-700">
      <div v-if="paddingTop" aria-hidden="true" :style="{ height: `${paddingTop}px` }" />
      <template v-for="{ row: node, index, gapBefore } in rendered" :key="node.id">
      <div v-if="gapBefore" aria-hidden="true" :style="{ height: `${gapBefore}px` }" />
      <article
        :ref="measure"
        :data-index="index"
        class="py-4"
      >
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0">
            <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ node.node_name }}</p>
            <p class="mt-1 truncate text-[11px] text-gray-400">
              {{ node.provider }} · #{{ node.account_id }} {{ accountNames?.[node.account_id] || '—' }} · {{ node.model }}
            </p>
          </div>
          <button
            type="button"
            data-testid="reset-one-node"
            class="shrink-0 text-xs text-primary-600 hover:underline disabled:opacity-50"
            :disabled="busy"
            @click="reset(node.id)"
          >
            {{ t(`${prefix}.resetOne`) }}
          </button>
        </div>
        <div class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-gray-500 dark:text-gray-400">
          <span><span class="text-emerald-600">{{ node.successes }}</span> / {{ node.misses }} / {{ node.network_errors }} / {{ node.account_errors }} · {{ resultLabel(node.last_result) }}</span>
          <span>{{ t(`${prefix}.lastSuccess`) }} {{ clock(node.last_success) }}</span>
          <span>{{ node.latency_ms }} ms</span>
          <span v-if="cooling(node.cooldown_until) !== '—'">{{ t(`${prefix}.cooldown`) }} {{ cooling(node.cooldown_until) }}</span>
        </div>
      </article>
      </template>
      <div v-if="paddingBottom" aria-hidden="true" :style="{ height: `${paddingBottom}px` }" />
      <p v-if="items.length === 0" class="py-8 text-center text-sm text-gray-500">{{ loading ? t(`${prefix}.loading`) : t(`${prefix}.empty`) }}</p>
    </div>
    <InfiniteListSentinel :loading="busy" :error="list.error.value" :has-more="list.hasMore.value && !resetting" @load-more="list.loadMore" @retry="list.retry" />
    <div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-[11px] text-gray-500">
      <p>{{ t(`${prefix}.legend`) }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexHarvestNodes, resetCodexHarvestNodes, type CodexHarvestNodeRecord } from '@/api/admin/codexHarvest'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useDocumentRows } from '@/composables/useDocumentRows'

const props = defineProps<{ refreshKey?: string; accountNames?: Record<number, string> }>()
const { t, te } = useI18n()
const prefix = 'admin.harvestFlow.nodes'
const limit = 8
const resetting = ref(false)
const resetError = ref('')
const list = useInfiniteList<CodexHarvestNodeRecord>({
  pageSize: limit, keyOf: item => item.id,
  fetchPage: page => getCodexHarvestNodes((page - 1) * limit, limit)
})
const { items, total, loading } = list
const busy = computed(() => loading.value || list.loadingMore.value || list.refreshing.value || resetting.value)
const error = computed(() => resetError.value || (list.error.value ? t(`${prefix}.loadFailed`) : ''))
const listBody = ref<HTMLElement | null>(null)
const { rendered, paddingTop, paddingBottom, measure } = useDocumentRows(items, listBody, item => item.id, computed(() => true), () => 124)
let disposed = false
const clock = (value: string | null) => value ? new Date(value).toLocaleString() : '—'
const cooling = (value: string | null) => value && new Date(value).getTime() > Date.now() ? clock(value) : '—'
const resultLabel = (result: string) => te(`admin.harvestFlow.results.${result}`) ? t(`admin.harvestFlow.results.${result}`) : result

async function refresh() {
  if (disposed || resetting.value) return
  resetError.value = ''
  await list.refresh()
}

async function reset(id: number) {
  if (resetting.value) return
  list.cancel()
  resetting.value = true
  resetError.value = ''
  try {
    await resetCodexHarvestNodes(id)
    if (disposed) return
  } catch {
    if (!disposed) resetError.value = t(`${prefix}.resetFailed`)
    return
  } finally {
    if (!disposed) resetting.value = false
  }
  if (!disposed) await refresh()
}
watch(() => props.refreshKey, () => { if (!busy.value) void refresh() })
onMounted(() => { void refresh() })
onBeforeUnmount(() => { disposed = true })
</script>
