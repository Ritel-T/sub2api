<template>
  <div>
    <dl class="usage-row-details">
      <div v-for="column in columns" :key="column.key" :data-field="column.key">
        <dt>{{ column.label }}</dt>
        <dd><CellContent :content="renderCell(column)" /></dd>
      </div>
    </dl>
    <RouterLink v-if="supportEnabled && row.request_id" class="mt-4 inline-flex items-center gap-2 text-sm underline underline-offset-4"
      :to="{ path: '/support-tickets', query: { request_id: row.request_id } }">
      {{ supportLabel }} <span aria-hidden="true">→</span>
    </RouterLink>
  </div>
</template>
<script setup lang="ts">
import { computed, type VNodeChild } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import type { UsageLog } from '@/types'
defineProps<{ row: UsageLog; columns: Column[]; renderCell: (column: Column) => VNodeChild; supportEnabled: boolean }>()
const CellContent = (props: { content: VNodeChild }) => props.content
const { locale } = useI18n()
const supportLabel = computed(() => locale?.value === 'en' ? 'Contact support' : locale?.value === 'zh' ? '联系支持' : '聯絡支援')
</script>
<style scoped>
.usage-row-details { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(14rem, 100%), 1fr)); gap: 1rem 2rem; }
.usage-row-details > div { min-width: 0; }
.usage-row-details dt { margin-bottom: .35rem; color: var(--ritel-muted); font-size: .8rem; }
.usage-row-details dd { overflow-wrap: anywhere; }
</style>
