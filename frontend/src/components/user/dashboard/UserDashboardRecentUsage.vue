<template>
  <section class="card" aria-labelledby="recent-title">
    <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-6 py-4 dark:border-dark-700">
      <h2 id="recent-title" class="text-xl font-semibold">{{ copy.recent }}</h2>
      <router-link to="/usage?view=records" class="underline underline-offset-4">{{ copy.allRequests }}</router-link>
    </header>
    <div v-if="loading" class="p-6" role="status">{{ copy.loading }}</div>
    <div v-else-if="error" class="flex flex-wrap items-center gap-3 p-6" role="alert">
      <span>{{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="$emit('retry')">{{ copy.retry }}</button>
    </div>
    <p v-else-if="!data.length" class="p-6 text-gray-500 dark:text-gray-400">{{ copy.noRequests }}</p>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-sm">
        <thead><tr class="text-gray-500 dark:text-gray-400"><th class="px-6 py-3 text-left">{{ copy.model }}</th><th class="px-6 py-3 text-left">{{ copy.time }}</th><th class="px-6 py-3 text-right">{{ copy.tokensUnit }}</th><th class="px-6 py-3 text-right">{{ copy.actual }}</th></tr></thead>
        <tbody><tr v-for="log in data" :key="log.id" class="border-t border-gray-200 dark:border-dark-700">
          <td class="px-6 py-4"><router-link :to="{ path: '/usage', query: { view: 'records', usage_id: String(log.id) } }" class="underline underline-offset-4">{{ log.model }}</router-link></td>
          <td class="whitespace-nowrap px-6 py-4">{{ formatDateTime(log.created_at) }}</td>
          <td class="px-6 py-4 text-right tabular-nums">{{ (log.input_tokens + log.output_tokens).toLocaleString() }}</td>
          <td class="px-6 py-4 text-right tabular-nums" :title="`${copy.standard}: $${log.total_cost.toFixed(4)}`">${{ log.actual_cost.toFixed(4) }}</td>
        </tr></tbody>
      </table>
    </div>
  </section>
</template>
<script setup lang="ts">
import { formatDateTime } from '@/utils/format'
import type { UsageLog } from '@/types'
import { useOverviewCopy } from './overviewCopy'
defineProps<{ data: UsageLog[]; loading: boolean; error?: boolean }>()
defineEmits<{ retry: [] }>()
const copy = useOverviewCopy()
</script>
