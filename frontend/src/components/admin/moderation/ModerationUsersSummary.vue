<template>
  <section class="open-section space-y-3" data-test="moderation-user-summary">
    <AppHeading as="h2" variant="section" >{{ t('admin.moderationUsers.listTitle') }}</AppHeading>
    <p v-if="total !== undefined" class="text-sm text-gray-500">{{ t('common.total') }}: {{ total }}</p>
    <div class="flex flex-wrap gap-2" role="tablist" :aria-label="t('admin.moderationUsers.listTitle')">
      <button v-for="tab in tabs" :key="tab" type="button" role="tab" class="btn btn-secondary" :aria-selected="filter === tab" @click="filter = tab; search()">{{ t(`admin.moderationUsers.${tab}`) }}</button>
    </div>
    <form class="workspace-filter-controls flex flex-wrap items-end gap-2" @submit.prevent="search">
      <label class="text-sm">UID<input v-model="uid" type="number" min="1" step="1" class="input" /></label>
      <button type="submit" class="btn btn-secondary">{{ t('common.search') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="loading || refreshing" @click="refresh">{{ t('common.refresh') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-red-600">{{ errorMessage }}</p>
    <DataTable column-order-key="admin-moderation-users" :columns="columns" :data="items" :loading="loading" row-key="user_id">
      <template #cell-auto_ban_exempt="{ row }">{{ t(row.auto_ban_exempt ? 'common.yes' : 'common.no') }}</template>
      <template #cell-status="{ row }">{{ row.status }} · {{ row.current_count }}</template>
      <template #cell-reason="{ row }"><span class="break-words">{{ row.reason || '—' }}</span></template>
      <template #cell-actor_id="{ row }">{{ row.actor_id ?? '—' }}</template>
      <template #cell-updated_at="{ row }">{{ row.updated_at ? new Date(row.updated_at).toLocaleString() : '—' }}</template>
      <template #cell-actions="{ row }"><button type="button" class="workspace-icon-action" :title="t('common.edit')" :aria-label="t('common.edit')" @click="selected = row.user_id"><Icon name="edit" size="sm" /></button></template>
      <template #empty><p v-if="!error" data-test="empty">{{ t('admin.moderationUsers.empty') }}</p></template>
    </DataTable>
    <InfiniteListSentinel :loading="loading || loadingMore || refreshing" :has-more="hasMore" :error="error" :progress="items.length" @load-more="loadMore" @retry="retry" />
    <BaseDialog :show="selected !== null" :title="t('admin.moderationUsers.title')" @close="selected = null">
      <ModerationUserPanel v-if="selected !== null" :key="selected" :user-id="selected" @changed="refresh" />
    </BaseDialog>
  </section>
</template>
<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Icon from '@/components/icons/Icon.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import ModerationUserPanel from './ModerationUserPanel.vue'
import { listModerationUsers, type ModerationUserState } from '@/api/admin/riskControl'
const { t } = useI18n()
const tabs = ['exempt', 'all', 'enforced'] as const
const filter = ref<typeof tabs[number]>('exempt')
const uid = ref('')
const selected = ref<number | null>(null)
let query: { state: typeof tabs[number]; user_id?: number } = { state: 'exempt' }
const { items, total, loading, loadingMore, refreshing, error, hasMore, reset, loadMore, refresh, retry } = useInfiniteList<ModerationUserState>({
  pageSize: 20,
  keyOf: item => item.user_id,
  // The API does not accept AbortSignal; generation checks prevent stale publication.
  fetchPage: page => listModerationUsers({ ...query, page, page_size: 20 })
})
const columns = computed(() => [
  { key: 'user_id', label: 'UID' },
  { key: 'auto_ban_exempt', label: t('admin.moderationUsers.exemption') },
  { key: 'status', label: t('admin.moderationUsers.state') },
  { key: 'reason', label: t('admin.moderationUsers.reason') },
  { key: 'actor_id', label: t('admin.moderationUsers.actor') },
  { key: 'updated_at', label: t('admin.moderationUsers.time') },
  { key: 'actions', label: t('common.actions') }
])
const errorMessage = computed(() => {
  const value = error.value as { response?: { data?: { message?: string } }; message?: string } | null
  return value?.response?.data?.message || value?.message || t('admin.moderationUsers.failed')
})
function search() {
  query = { state: filter.value, ...(uid.value ? { user_id: Number(uid.value) } : {}) }
  selected.value = null
  void reset()
}
onMounted(search)
</script>
