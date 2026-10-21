<template>
  <div class="account-row-details">
    <div class="account-detail-overview">
      <section v-if="statColumns.length" class="account-detail-stats">
        <h3>{{ t('admin.accounts.details.statistics') }}</h3>
        <dl>
          <div v-for="column in statColumns" :key="column.key" :data-field="column.key">
            <dt v-if="column.key !== 'today_stats'">{{ column.label }}</dt>
            <dd><Cell :column="column" /></dd>
          </div>
        </dl>
      </section>
      <section v-if="propertyColumns.length" class="account-detail-properties">
        <h3>{{ t('admin.accounts.details.configuration') }}</h3>
        <dl>
          <div v-for="column in propertyColumns" :key="column.key" :data-field="column.key" :class="{ 'is-wide': column.key === 'notes' }">
            <dt>{{ column.label }}</dt>
            <dd><Cell :column="column" /></dd>
          </div>
        </dl>
      </section>
    </div>
    <section class="account-detail-actions">
      <h3>{{ t('admin.accounts.details.actions') }}</h3>
      <slot name="actions" />
      <div v-if="$slots.supplementary" class="account-detail-extra">
        <h4>{{ t('admin.accounts.details.extras') }}</h4>
        <slot name="supplementary" />
      </div>
    </section>
    <div v-if="linkColumn" class="account-detail-links">
      <h4>{{ t('admin.accounts.details.links') }}</h4>
      <Cell :column="linkColumn" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, type VNodeChild } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
const props = defineProps<{ columns: Column[]; renderCell: (column: Column) => VNodeChild }>()
const { t } = useI18n()
const stats = new Set(['today_stats', 'usage', 'capacity'])
const statColumns = computed(() => props.columns.filter(column => stats.has(column.key)))
const propertyColumns = computed(() => props.columns.filter(column => !stats.has(column.key) && column.key !== 'links').sort((a, b) => Number(a.key === 'notes') - Number(b.key === 'notes')))
const linkColumn = computed(() => props.columns.find(column => column.key === 'links'))
const Cell = ({ column }: { column: Column }) => props.renderCell(column)
</script>

<style scoped>
.account-row-details { display: grid; gap: 1rem; padding: 0; white-space: normal; }
h3 { margin: 0 0 .75rem; font-size: 13px; font-weight: 600; color: var(--ritel-text); }
h4, dt { color: var(--ritel-muted); font-size: 12px; font-weight: 400; }
h4 { margin: 0; padding-top: .45rem; }
dd { margin: .25rem 0 0; min-width: 0; font-size: 13px; overflow-wrap: anywhere; }
.account-detail-overview { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(0, 3fr); gap: 2rem; }
.account-detail-stats dl { display: grid; gap: .75rem; }
.account-detail-properties dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: .85rem 1.25rem; }
.account-detail-properties .is-wide { grid-column: 1 / -1; }
.account-detail-actions { border-top: 1px solid var(--ritel-line); padding-top: 1rem; }
.account-detail-extra, .account-detail-links { display: grid; grid-template-columns: 6.5rem minmax(0, 1fr); gap: .75rem; align-items: start; }
.account-detail-extra { margin-top: .9rem; }
.account-detail-links :deep(nav) { gap: .25rem 1rem; }
.account-detail-links :deep(a) { min-height: 32px; padding: .3rem 0; border: 0; border-radius: 0; background: transparent; font-size: 12px; box-shadow: none; }
@media (max-width: 1023px) { .account-detail-overview { grid-template-columns: 1fr; gap: 1.25rem; } .account-detail-properties dl { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 639px) { .account-detail-properties dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } .account-detail-extra, .account-detail-links { grid-template-columns: 1fr; gap: .25rem; } }
</style>
