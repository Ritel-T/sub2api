<template>
  <div class="space-y-3">
    <label for="quality-account-search" class="block text-sm font-medium">{{ t('qualityOps.accounts') }}</label>
    <div class="workspace-filter-controls grid gap-3 sm:grid-cols-2">
      <label class="space-y-1 text-sm"><span>{{ t('qualityOps.accountGroup') }}</span>
        <select v-model="accountGroup" class="input" data-testid="quality-account-group" @change="emit('search')">
          <option value="">{{ t('qualityOps.allAccountGroups') }}</option>
          <option value="ungrouped">{{ t('qualityOps.ungroupedAccounts') }}</option>
          <option v-for="group in groups" :key="group.id" :value="String(group.id)">{{ group.name }} #{{ group.id }}</option>
        </select>
      </label>
      <label class="space-y-1 text-sm"><span>{{ t('qualityOps.accountType') }}</span>
        <select v-model="accountType" class="input" data-testid="quality-account-type" @change="emit('search')">
          <option value="">{{ t('qualityOps.allAccountTypes') }}</option>
          <option v-for="option in qualityAccountTypeOptions" :key="option.value" :value="option.value">{{ t(option.label) }}</option>
        </select>
      </label>
    </div>
    <div class="space-y-1 text-sm">
      <span>{{ t('qualityOps.accountStatus') }}</span>
      <div class="flex flex-wrap gap-x-4 gap-y-1">
        <label v-for="option in qualityAccountStatusOptions" :key="option.value" class="flex items-center gap-1.5">
          <input v-model="statuses" type="checkbox" :value="option.value" :data-testid="`quality-account-status-${option.value}`" @change="emit('search')" />{{ t(option.label) }}
        </label>
      </div>
      <p class="text-xs text-gray-500">{{ t('qualityOps.accountStatusHint') }}</p>
    </div>
    <div class="workspace-filter-controls flex gap-2"><input id="quality-account-search" v-model="search" class="input min-w-0" :placeholder="t('qualityOps.search')" @keydown.enter.prevent="emit('search')" /><button type="button" class="btn btn-secondary shrink-0 whitespace-nowrap" @click="emit('search')">{{ t('qualityOps.search') }}</button></div>
    <div v-if="!bulk && !scopeLocked" class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
      <span>{{ t('qualityOps.accountScope') }}</span>
      <label class="flex items-center gap-1.5"><input v-model="scope" type="radio" value="all" data-testid="quality-account-scope-all" />{{ t('qualityOps.accountScopeAll') }}</label>
      <label class="flex items-center gap-1.5"><input v-model="scope" type="radio" value="manual" data-testid="quality-account-scope-manual" />{{ t('qualityOps.accountScopeManual') }}</label>
    </div>
    <div v-if="!allScope" class="flex flex-wrap items-center gap-3 text-sm">
      <button type="button" class="text-primary-600 disabled:opacity-50" data-testid="quality-select-page" :disabled="accountsLoading || selectingAccounts || !accounts.some(account => !disabledReason(account))" @click="emit('selectPage')">{{ t('admin.accounts.bulkActions.selectCurrentPage') }}</button>
      <button type="button" class="text-primary-600 disabled:opacity-50" data-testid="quality-select-all" :disabled="accountsLoading || selectingAccounts || !accounts.length" @click="emit('selectAll')">{{ t(selectingAccounts ? 'qualityOps.selectingAccounts' : 'qualityOps.selectMatchingAccounts') }}</button>
      <button type="button" class="text-gray-500 disabled:opacity-50" data-testid="quality-clear-selection" :disabled="!selectedAccounts.length && !selectingAccounts" @click="emit('clear')">{{ t('qualityOps.clearAccountSelection') }}</button>
    </div>
    <p class="text-xs text-gray-500">{{ t(scopeLocked ? 'qualityOps.templateFilterHint' : allScope ? 'qualityOps.accountScopeAllHint' : bulk ? 'qualityOps.bulkAccountSelectionHint' : 'qualityOps.accountSelectionHint') }}</p>
    <p v-if="accountsError" role="alert" class="text-sm text-red-600">{{ accountsError }}</p>
    <p v-if="accountsLoading" role="status" class="text-sm text-gray-500">{{ t('qualityOps.loading') }}</p>
    <div class="grid max-h-52 gap-2 overflow-auto rounded border p-3 sm:grid-cols-2 dark:border-dark-600" :aria-busy="accountsLoading || selectingAccounts">
      <label v-for="account in accounts" :key="account.id" class="flex items-center gap-2 text-sm"><input v-model="selectedAccounts" type="checkbox" :value="account.id" :disabled="allScope || selectingAccounts || !!disabledReason(account)" /><span class="min-w-0 break-all">{{ account.name }} <span class="text-gray-500">#{{ account.id }}</span><span v-if="disabledReason(account)" class="ml-1 text-xs text-gray-500">{{ disabledReason(account) }}</span></span></label>
      <p v-if="!accountsLoading && !accountsError && !accounts.length" class="text-sm text-gray-500 sm:col-span-2">{{ t('qualityOps.noMatchingAccounts') }}</p>
      <InfiniteListSentinel class="sm:col-span-2" :loading="accountsLoading || selectingAccounts" :error="listError" :has-more="hasMore" :progress="accounts.length" @load-more="emit('loadMore')" @retry="emit('retry')" />
    </div>
    <span aria-live="polite" class="text-sm" data-testid="quality-account-count">{{ allScope ? t('qualityOps.accountsMatched', { count: accountsTotal }) : t('qualityOps.selected', { count: selectedAccounts.length }) }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import type { AccountListItem, AdminGroup } from '@/types'
import { qualityAccountStatusOptions, qualityAccountTypeOptions } from '@/utils/qualityAccountFilter'

const { t } = useI18n()
const selectedAccounts = defineModel<number[]>({ required: true })
const search = defineModel<string>('search', { required: true })
const accountGroup = defineModel<string>('group', { required: true })
const accountType = defineModel<string>('type', { required: true })
// 状态可多选（任一命中），全不勾 = 全部状态；范围默认「所有匹配账户」，保存时按筛选自动包含。
const statuses = defineModel<string[]>('statuses', { required: true })
const scope = defineModel<'all' | 'manual'>('scope', { required: true })
const props = defineProps<{
  accounts: AccountListItem[]
  groups: AdminGroup[]
  accountsLoading: boolean
  selectingAccounts: boolean
  accountsError: string
  hasMore: boolean
  listError?: unknown
  accountsTotal: number
  bulk: boolean
  // 编辑分组规则时范围固定为「所有匹配账户」，只能改筛选条件。
  scopeLocked?: boolean
  disabledReason: (account: AccountListItem) => string
}>()
// 批量修改改的是已有规则，范围选择只在新建时出现。
const allScope = computed(() => scope.value === 'all' && !props.bulk)
const emit = defineEmits<{
  search: []
  loadMore: []
  retry: []
  selectPage: []
  selectAll: []
  clear: []
}>()
</script>
