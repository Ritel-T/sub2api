<template>
  <BaseDialog :show="!!record" :title="copy.details" width="wide" @close="$emit('close')">
    <div v-if="record" class="space-y-6">
      <dl class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div v-for="entry in entries" :key="entry[0]"><dt class="text-gray-500">{{ entry[0] }}</dt><dd class="break-all">{{ entry[1] }}</dd></div>
      </dl>
      <RouterLink v-if="supportEnabled && record.request_id" class="btn btn-primary" :to="{path:'/support-tickets', query:{request_id:record.request_id}}">{{ copy.support }}</RouterLink>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { formatDateTime } from '@/utils/format'
import type { UsageLog } from '@/types'
const props = defineProps<{record: UsageLog | null; supportEnabled: boolean}>()
defineEmits<{close: []}>()
const {t, locale} = useI18n()
const copy = computed(() => locale?.value === 'en' ? {details:'Request details',support:'Contact support',request:'Request ID'} : locale?.value === 'zh' ? {details:'请求详情',support:'联系支持',request:'请求 ID'} : {details:'請求詳情',support:'聯絡支援',request:'請求 ID'})
const entries = computed(() => {
  const r = props.record
  if (!r) return []
  return [[copy.value.request,r.request_id || '—'],[t('usage.time'),formatDateTime(r.created_at)],[t('usage.model'),r.model],[t('usage.apiKeyFilter'),r.api_key?.name || '—'],[t('usage.endpoint'),r.inbound_endpoint || '—'],[t('usage.tokens'),String((r.input_tokens ?? 0)+(r.output_tokens ?? 0))],[t('usage.cost'),`$${r.actual_cost.toFixed(8)}`],[t('usage.duration'),r.duration_ms == null ? '—' : `${r.duration_ms} ms`]]
})
</script>
