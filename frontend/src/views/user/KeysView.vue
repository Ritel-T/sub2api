<template>
  <AppLayout :show-page-title="false" wide>
    <TablePageLayout open>
      <template #filters>
        <div class="keys-filters flex flex-col gap-3">
          <div class="flex flex-wrap items-center gap-3">
            <SearchInput
              v-model="filterSearch"
              :placeholder="t('keys.searchPlaceholder')"
              class="keys-search w-full sm:w-80"
              @search="onFilterChange"
            />
            <Select
              :model-value="filterGroupId"
              :aria-label="t('keys.group')"
              class="w-40"
              :options="groupFilterOptions"
              @update:model-value="onGroupFilterChange"
            />
            <Select
              :model-value="filterStatus"
              :aria-label="t('common.status')"
              class="w-40"
              :options="statusFilterOptions"
              @update:model-value="onStatusFilterChange"
            />
            <div ref="sortToolbarTarget" class="inline-table-tools" />
            <div class="keys-table-tools">
              <button
                @click="refreshPage"
                :disabled="loading || groupPricingLoading"
                class="btn btn-secondary"
                :title="t('common.refresh')"
                :aria-label="t('common.refresh')"
              >
                <Icon name="refresh" size="md" :class="loading || groupPricingLoading ? 'animate-spin' : ''" />
              </button>
              <ColumnSettings
                :columns="orderedColumns"
                :hidden-columns="hiddenColumns"
                :required-columns="['id', 'name', 'actions']"
                @toggle="toggleColumn"
                @reorder="setColumnOrder"
                @reset="resetColumns"
              />
            </div>
          </div>
          <EndpointPopover
            class="keys-endpoints"
            v-if="publicSettings?.api_base_url || (publicSettings?.custom_endpoints?.length ?? 0) > 0"
            :api-base-url="publicSettings?.api_base_url || ''"
            :custom-endpoints="publicSettings?.custom_endpoints || []"
          />
          <div v-if="selectedIds.length" class="flex flex-wrap items-center gap-3 text-sm">
            <span class="text-gray-600 dark:text-gray-300">
              {{ t('keys.bulkEdit.selectedCount', { count: selectedIds.length }) }}
            </span>
            <button
              class="btn btn-primary btn-sm"
              :disabled="loading"
              data-test="bulk-edit-keys"
              @click="showBulkEditModal = true"
            >
              {{ t('keys.bulkEdit.title') }}
            </button>
            <button class="btn btn-secondary btn-sm" @click="selectedIds = []">
              {{ t('keys.bulkEdit.clearSelection') }}
            </button>
          </div>
        </div>
      </template>

      <template #actions>
        <div class="keys-page-heading">
          <AppHeading variant="page" as="h1">{{ t('keys.title') }}</AppHeading>
          <div class="keys-primary-actions">
            <button @click="openConnectionPicker" class="btn btn-secondary" data-testid="connect-key-entry">{{ kc.connect }}</button>
            <button @click="showCreateModal = true" class="btn btn-primary" data-tour="keys-create-btn">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('keys.createKey') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <KeyWorkspaceTable
          :toolbar-target="sortToolbarTarget"
          :columns="columns"
          @column-reorder="setColumnOrder"
          :data="apiKeys"
          :loading="loading"
          :busy-keys="busyKeyIds"
          :rename="renameKey"
          :sort-key="sortState.sort_by"
          :sort-order="sortState.sort_order"
          :selected-keys="selectedIds"
          @update:selected-keys="handleSelectionChange"
          @sort="handleSort"
        >
          <template #cell-id="{ value }">
            <span>{{ value }}</span>
          </template>

          <template #cell-key="{ row }">
            <button class="keys-key-copy" :disabled="busyKeyIds.has(row.id)" :aria-label="`${t('keys.copyToClipboard')}: ${row.name}`" @click.stop="copyToClipboard(row.key, row.id)">
              <span>{{ maskApiKey(row.key) }}</span><Icon v-if="copiedKeyId === row.id" name="check" size="xs" />
            </button>
          </template>

          <template #cell-name="{ row }">{{ row.name }}</template>
          <template #name-indicator="{ row }">
              <Icon
                v-if="row.ip_whitelist?.length > 0 || row.ip_blacklist?.length > 0"
                name="shield"
                size="sm"
                class="theme-info-icon"
                :title="t('keys.ipRestrictionEnabled')"
              />
          </template>

          <template #cell-group="{ row }">
            <div class="group/dropdown">
              <button :ref="el => setGroupButtonRef(row.id, el)" class="keys-group-button" :disabled="busyKeyIds.has(row.id)" :title="`${row.group?.name || t('keys.noGroup')} · ${t('keys.clickToChangeGroup')}`" @click.stop="(row.group_ids?.length ?? 0) > 1 ? editKey(row) : openGroupSelector(row)">
                <span>{{ row.group?.name || t('keys.noGroup') }}</span><span v-if="(row.group_ids?.length ?? 0) > 1">+{{ (row.group_ids?.length ?? 0) - 1 }}</span><Icon name="chevronDown" size="xs" />
              </button>
            </div>
          </template>

          <template #cell-current_concurrency="{ row }">
            <div class="keys-concurrency-main" :title="concurrencyRows[row.id]?.notice || t('keys.concurrencyAndWaiting')">
              <span :class="{ 'text-amber-600 dark:text-amber-400': concurrencyRows[row.id]?.concurrencyFull }" :aria-label="t('keys.concurrencyCount')">{{ concurrencyRows[row.id]?.current ?? '—' }}<template v-if="concurrencyRows[row.id]?.limit > 0"> / {{ concurrencyRows[row.id].limit }}</template></span>
              <span v-if="concurrencyRows[row.id]?.notice" class="keys-concurrency-notice">{{ concurrencyRows[row.id].notice }}</span>
              <span v-else-if="(queuePolicy?.max_waiting ?? 0) > 0 || (concurrencyRows[row.id]?.waiting ?? 0) > 0" class="keys-waiting" :title="t('keys.waitingCount')" :class="{ 'text-amber-600 dark:text-amber-400': concurrencyRows[row.id]?.full }"><Icon name="clock" size="xs" />{{ concurrencyRows[row.id]?.waiting ?? '—' }}</span>
            </div>
          </template>

          <template #cell-usage="{ row }">
            <span class="keys-quota-used" :title="t('keys.quota')">${{ (row.quota_used ?? 0).toFixed(2) }}</span><span class="keys-quota-total"> / {{ row.quota > 0 ? '$' + row.quota.toFixed(2) : kw.unlimited }}</span>
          </template>

          <template #cell-rate_limit="{ row }">
            <div v-if="row.rate_limit_5h > 0 || row.rate_limit_1d > 0 || row.rate_limit_7d > 0" class="space-y-1.5 min-w-[140px]">
              <!-- 5h window -->
              <div v-if="row.rate_limit_5h > 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">5h</span>
                  <span :class="[
                    'font-medium tabular-nums',
                    row.usage_5h >= row.rate_limit_5h ? 'text-red-500' :
                    row.usage_5h >= row.rate_limit_5h * 0.8 ? 'text-yellow-500' :
                    'text-gray-700 dark:text-gray-300'
                  ]">
                    ${{ row.usage_5h?.toFixed(2) || '0.00' }}/${{ row.rate_limit_5h?.toFixed(2) }}
                  </span>
                </div>
                <div class="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.usage_5h >= row.rate_limit_5h ? 'bg-red-500' :
                      row.usage_5h >= row.rate_limit_5h * 0.8 ? 'bg-yellow-500' :
                      'bg-emerald-500'
                    ]"
                    :style="{ width: Math.min((row.usage_5h / row.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_5h_at && formatResetTime(row.reset_5h_at)" class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">
                  ⟳ {{ formatResetTime(row.reset_5h_at) }}
                </div>
              </div>
              <!-- 1d window -->
              <div v-if="row.rate_limit_1d > 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">1d</span>
                  <span :class="[
                    'font-medium tabular-nums',
                    row.usage_1d >= row.rate_limit_1d ? 'text-red-500' :
                    row.usage_1d >= row.rate_limit_1d * 0.8 ? 'text-yellow-500' :
                    'text-gray-700 dark:text-gray-300'
                  ]">
                    ${{ row.usage_1d?.toFixed(2) || '0.00' }}/${{ row.rate_limit_1d?.toFixed(2) }}
                  </span>
                </div>
                <div class="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.usage_1d >= row.rate_limit_1d ? 'bg-red-500' :
                      row.usage_1d >= row.rate_limit_1d * 0.8 ? 'bg-yellow-500' :
                      'bg-emerald-500'
                    ]"
                    :style="{ width: Math.min((row.usage_1d / row.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_1d_at && formatResetTime(row.reset_1d_at)" class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">
                  ⟳ {{ formatResetTime(row.reset_1d_at) }}
                </div>
              </div>
              <!-- 7d window -->
              <div v-if="row.rate_limit_7d > 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">7d</span>
                  <span :class="[
                    'font-medium tabular-nums',
                    row.usage_7d >= row.rate_limit_7d ? 'text-red-500' :
                    row.usage_7d >= row.rate_limit_7d * 0.8 ? 'text-yellow-500' :
                    'text-gray-700 dark:text-gray-300'
                  ]">
                    ${{ row.usage_7d?.toFixed(2) || '0.00' }}/${{ row.rate_limit_7d?.toFixed(2) }}
                  </span>
                </div>
                <div class="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.usage_7d >= row.rate_limit_7d ? 'bg-red-500' :
                      row.usage_7d >= row.rate_limit_7d * 0.8 ? 'bg-yellow-500' :
                      'bg-emerald-500'
                    ]"
                    :style="{ width: Math.min((row.usage_7d / row.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_7d_at && formatResetTime(row.reset_7d_at)" class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">
                  ⟳ {{ formatResetTime(row.reset_7d_at) }}
                </div>
              </div>
              <!-- Reset button -->
              <button
                v-if="row.usage_5h > 0 || row.usage_1d > 0 || row.usage_7d > 0"
                @click.stop="confirmResetRateLimitFromTable(row)"
                class="mt-0.5 inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('keys.resetRateLimitUsage')"
              >
                <Icon name="refresh" size="xs" />
                {{ t('keys.resetUsage') }}
              </button>
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-expires_at="{ row }">
            <span v-if="row.expires_at" :class="[
              'text-sm',
              new Date(row.expires_at) < new Date() ? 'text-red-500 dark:text-red-400' : 'text-gray-500 dark:text-dark-400'
            ]">
              {{ formatDateTime(row.expires_at) }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">{{ t('keys.noExpiration') }}</span>
          </template>

          <template #cell-status="{ row }">
            <Toggle :model-value="row.status === 'active'" v-if="row.status === 'active' || row.status === 'inactive'" :aria-label="`${t('common.status')}: ${row.name}`" :title="t('keys.status.' + row.status)" :disabled="busyKeyIds.has(row.id) || loading" @click.stop="toggleKeyStatus(row)" />
            <button v-else class="keys-blocked-status" :class="row.status === 'expired' ? 'text-red-600 dark:text-red-400' : 'text-amber-700 dark:text-amber-400'" :disabled="busyKeyIds.has(row.id)" :title="t('common.edit')" @click.stop="editKey(row)">{{ t('keys.status.' + row.status) }}</button>
          </template>

          <template #cell-last_used_at="{ row }">
            <span v-if="row.last_used_at" class="text-sm text-gray-500 dark:text-dark-400">
              {{ formatDateTime(row.last_used_at) }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-last_used_ip="{ value }">
            <span v-if="value" class="text-sm text-gray-500 dark:text-dark-400">
              {{ value }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-created_at="{ row }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(row.created_at) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="keys-row-actions"><button data-testid="connect-key-row" :disabled="row.status !== 'active' || busyKeyIds.has(row.id)" @click.stop="openConnection(row)">{{ kw.use }}</button><button :disabled="busyKeyIds.has(row.id)" @click.stop="editKey(row)">{{ t('common.edit') }}</button></div>
          </template>

          <template #details-extra="{ row }">
            <div class="keys-extra-details">
              <section><AppHeading variant="subsection" as="h3">{{ t('keys.group') }}</AppHeading><GroupBadge
                  v-if="row.group"
                  :name="row.group.name"
                  :platform="row.group.platform"
                  :subscription-type="row.group.subscription_type"
                  :rate-multiplier="row.group.rate_multiplier"
                  :user-rate-multiplier="userGroupRates[row.group.id]"
                  :peak-rate-enabled="row.group.peak_rate_enabled"
                  :peak-start="row.group.peak_start"
                  :peak-end="row.group.peak_end"
                  :peak-rate-multiplier="row.group.peak_rate_multiplier"
                /><div v-if="(row.group_ids?.length ?? 0) > 1" class="keys-extra-groups">{{ (row.group_ids ?? []).map((id: number) => groups.find(group => group.id === id)?.name || '#' + id).join(' · ') }}</div></section>
              <section><AppHeading variant="subsection" as="h3">{{ t('keys.usage') }}</AppHeading>
            <div class="text-sm">
              <div class="flex items-center gap-1.5">
                <span class="text-gray-500 dark:text-gray-400">{{ t('keys.today') }}:</span>
                <span class="font-medium text-gray-900 dark:text-white">
                  ${{ (usageStats[row.id]?.today_actual_cost ?? 0).toFixed(4) }}
                </span>
              </div>
              <div class="mt-0.5 flex items-center gap-1.5">
                <span class="text-gray-500 dark:text-gray-400">{{ t('keys.total') }}:</span>
                <span class="font-medium text-gray-900 dark:text-white">
                  ${{ (usageStats[row.id]?.total_actual_cost ?? 0).toFixed(4) }}
                </span>
              </div>
              </div></section>
              <section><AppHeading variant="subsection" as="h3">{{ t('keys.concurrencyAndWaiting') }}</AppHeading><div data-testid="detail-concurrency">
            <span
              :title="t('keys.concurrencyCount')"
              :class="[
                'inline-flex items-center gap-1 rounded-md px-1.5 py-px text-xs font-normal leading-tight tabular-nums',
                concurrencyRows[row.id]?.concurrencyFull
                  ? 'bg-amber-50 text-amber-700 ring-1 ring-amber-200 dark:bg-amber-900/25 dark:text-amber-300 dark:ring-amber-800'
                  : (concurrencyRows[row.id]?.current ?? 0) > 0
                  ? 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200 dark:bg-emerald-900/25 dark:text-emerald-300 dark:ring-emerald-800'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'
              ]"
            >
              <svg
                class="h-2.5 w-2.5"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
                aria-hidden="true"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="M16 21v-2a4 4 0 00-4-4H6a4 4 0 00-4 4v2m8-8a4 4 0 100-8 4 4 0 000 8" />
              </svg>
              <span class="sr-only">{{ t('keys.concurrencyCount') }}{{ ' ' }}</span>
              <span class="whitespace-nowrap font-mono">
                {{ concurrencyRows[row.id]?.current ?? '—' }}
                <span v-if="concurrencyRows[row.id]?.limit > 0" class="ml-1">/ {{ concurrencyRows[row.id]?.limit }}</span>
              </span>
            </span>
            <div v-if="concurrencyRows[row.id]?.limit > 0" class="mt-1 text-xs tabular-nums text-gray-500 dark:text-dark-400">
              <template v-if="queuePolicy && concurrencyRows[row.id]?.limit > 0">
                <span v-if="queuePolicy.max_waiting === 0" class="block">{{ t('keys.queueOff') }}</span>
                <span v-if="queuePolicy.max_waiting > 0 || (concurrencyRows[row.id]?.waiting ?? 0) > 0"
                  :class="{ 'text-amber-600 dark:text-amber-400': concurrencyRows[row.id]?.full }">
                  <span :class="[
                    'inline-flex items-center gap-1 rounded-md px-1.5 py-px font-normal leading-tight tabular-nums',
                    concurrencyRows[row.id]?.full
                      ? 'bg-amber-50 text-amber-700 ring-1 ring-amber-200 dark:bg-amber-900/25 dark:text-amber-300 dark:ring-amber-800'
                      : concurrencyRows[row.id]?.waiting === 0
                        ? 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'
                        : 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200 dark:bg-emerald-900/25 dark:text-emerald-300 dark:ring-emerald-800'
                  ]" :title="t('keys.waitingCount')">
                    <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                      <circle cx="12" cy="12" r="9" />
                      <path stroke-linecap="round" d="M12 7v5l3 2" />
                    </svg>
                    <span class="sr-only">{{ t('keys.waitingCount') }}{{ ' ' }}</span>
                    <span class="whitespace-nowrap font-mono">{{ concurrencyRows[row.id]?.waiting ?? '—' }}<template v-if="queuePolicy.max_waiting > 0"> / {{ queuePolicy.max_waiting }}</template></span>
                  </span>
                  <span v-if="concurrencyRows[row.id]?.full"> · {{ t('keys.queueFull') }}</span>
                </span>
              </template>
              <span v-else>{{ t(concurrencyState.status === 'loading' ? 'keys.queuePolicyLoading' : 'keys.queuePolicyUnavailable') }}</span>
            </div>
            <span v-if="concurrencyRows[row.id]?.notice" class="mt-1 block text-xs text-gray-500 dark:text-dark-400">
              {{ concurrencyRows[row.id].notice }}
            </span>
          </div></section>
            </div>
            <div class="keys-detail-actions"><button class="btn btn-secondary btn-sm" @click="viewKeyRequests(row)">{{ kc.requests }}</button><button class="btn btn-secondary btn-sm" :disabled="busyKeyIds.has(row.id)" @click="confirmDelete(row)">{{ t('common.delete') }}</button></div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('keys.noKeysYet')"
              :description="t('keys.createFirstKey')"
              :action-text="t('keys.createKey')"
              @action="showCreateModal = true"
            />
          </template>
        </KeyWorkspaceTable>
      </template>

      <template #pagination>
        <InfiniteListSentinel
          :loading="loading || loadingMore || refreshing"
          :error="listError"
          :has-more="hasMoreKeys"
          @load-more="loadMoreKeys"
          @retry="retryKeys"
        />
      </template>
    </TablePageLayout>

    <!-- Create/Edit Modal -->
    <BaseDialog
      :show="showCreateModal || showEditModal"
      :title="showEditModal ? t('keys.editKey') : t('keys.createKey')"
      width="normal"
      @close="closeModals"
    >
      <form id="key-form" @submit.prevent="handleSubmit" class="space-y-5">
        <div>
          <label class="input-label">{{ t('keys.nameLabel') }}</label>
          <input
            v-model="formData.name"
            type="text"
            required
            class="input"
            :placeholder="t('keys.namePlaceholder')"
            data-tour="key-form-name"
          />
        </div>

        <fieldset v-if="!showEditModal" data-tour="key-form-provider">
          <legend class="input-label">{{ t('keys.providerLabel') }}</legend>
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <label
              v-for="provider in createProviderOptions"
              :key="provider.value"
              class="relative min-w-0"
              :class="provider.count === 0 ? 'cursor-not-allowed' : 'cursor-pointer'"
            >
              <input
                type="radio"
                name="key-provider"
                :value="provider.value"
                :checked="createProvider === provider.value"
                :disabled="provider.count === 0"
                class="peer sr-only"
                @change="selectCreateProvider(provider.value)"
              />
              <span
                class="flex h-full flex-col items-center gap-2 rounded-xl border border-gray-200 bg-white px-2 py-3 text-center transition-colors peer-checked:border-primary-500 peer-checked:bg-primary-50/60 peer-checked:ring-1 peer-checked:ring-primary-500 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary-500 peer-disabled:opacity-40 dark:border-dark-600 dark:bg-dark-800 dark:peer-checked:border-primary-500 dark:peer-checked:bg-primary-500/10"
                :class="provider.count > 0 && 'hover:border-primary-300 dark:hover:border-primary-700'"
              >
                <span class="flex h-8 items-center justify-center gap-1.5" aria-hidden="true">
                  <span
                    v-for="platform in KEY_GROUP_PROVIDER_ICONS[provider.value]"
                    :key="platform"
                    class="flex h-8 w-8 items-center justify-center rounded-lg"
                    :class="platformBadgeLightClass(platform)"
                  >
                    <PlatformIcon :platform="platform" size="lg" />
                  </span>
                </span>
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ provider.label }}</span>
              </span>
              <span
                v-if="createProvider === provider.value"
                class="absolute right-1.5 top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-primary-500 text-white"
                aria-hidden="true"
              >
                <Icon name="check" size="xs" :stroke-width="3" />
              </span>
            </label>
          </div>
          <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400" aria-live="polite">
            {{ groups.length === 0 ? t('common.noGroupsAvailable') : t(`keys.providerHints.${createProvider}`) }}
          </p>
        </fieldset>

        <div>
          <label class="input-label" for="key-form-group">{{ t('keys.groupLabel') }}</label>
          <Select
            :key="showEditModal ? 'edit' : createProvider"
            id="key-form-group"
            :aria-label="t('keys.groupLabel')"
            v-model="formData.group_id"
            :options="formGroupOptions"
            :placeholder="t('keys.selectGroup')"
            :empty-text="t('common.noGroupsAvailable')"
            :searchable="true"
            :search-placeholder="t('keys.searchGroup')"
            data-tour="key-form-group"
          >
            <template #selected="{ option }">
              <GroupBadge
                v-if="option"
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                :peak-rate-enabled="(option as unknown as GroupOption).peakRateEnabled"
                :peak-start="(option as unknown as GroupOption).peakStart"
                :peak-end="(option as unknown as GroupOption).peakEnd"
                :peak-rate-multiplier="(option as unknown as GroupOption).peakRateMultiplier"
              />
              <span v-else class="text-gray-400">{{ t('keys.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                :peak-rate-enabled="(option as unknown as GroupOption).peakRateEnabled"
                :peak-start="(option as unknown as GroupOption).peakStart"
                :peak-end="(option as unknown as GroupOption).peakEnd"
                :peak-rate-multiplier="(option as unknown as GroupOption).peakRateMultiplier"
                :description="(option as unknown as GroupOption).description"
                :selected="selected"
              />
            </template>
          </Select>
          <fieldset class="mt-3 space-y-2" data-testid="key-additional-groups">
            <legend class="input-label">{{ t('keys.additionalGroups') }}</legend>
            <p class="input-hint">{{ t('keys.multiGroupHint') }}</p>
            <div class="max-h-48 space-y-2 overflow-y-auto">
              <label v-for="option in groupOptions.filter(item => item.value !== null && item.value !== formData.group_id)"
                :key="String(option.value)" class="flex items-center gap-2 text-sm">
                <input v-model="formData.group_ids" type="checkbox" :value="option.value" :disabled="!formData.group_ids.includes(Number(option.value)) && selectedFormGroupIDs().length >= 20" />
                <GroupBadge :name="option.label" :platform="option.platform"
                  :subscription-type="option.subscriptionType" :rate-multiplier="option.rate"
                  :user-rate-multiplier="option.userRate" />
              </label>
            </div>
          </fieldset>
        </div>

        <details :open="showEditModal" class="border-t border-gray-200 pt-4 dark:border-dark-700" data-testid="key-advanced-settings">
          <summary class="cursor-pointer font-medium">{{ kc.advanced }}</summary>
          <div class="mt-4 space-y-5">
        <!-- Custom Key Section (only for create) -->
        <div v-if="!showEditModal" class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.customKeyLabel') }}</label>
            <Toggle :model-value="formData.use_custom_key" :aria-label="t('keys.customKeyLabel')" @click="formData.use_custom_key = !formData.use_custom_key" size="small" />
          </div>
          <div v-if="formData.use_custom_key">
            <input
              v-model="formData.custom_key"
              type="text"
              class="input font-mono"
              :placeholder="t('keys.customKeyPlaceholder')"
              :class="{ 'border-red-500 dark:border-red-500': customKeyError }"
            />
            <p v-if="customKeyError" class="mt-1 text-sm text-red-500">{{ customKeyError }}</p>
            <p v-else class="input-hint">{{ t('keys.customKeyHint') }}</p>
          </div>
        </div>

        <div v-if="showEditModal">
          <label class="input-label">{{ t('keys.statusLabel') }}</label>
          <Select :aria-label="t('keys.statusLabel')"
            v-model="formData.status"
            :options="statusOptions"
            :placeholder="t('keys.selectStatus')"
          />
        </div>

        <!-- IP Restriction Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.ipRestriction') }}</label>
            <Toggle :model-value="formData.enable_ip_restriction" :aria-label="t('keys.ipRestriction')" @click="formData.enable_ip_restriction = !formData.enable_ip_restriction" size="small" />
          </div>

          <div v-if="formData.enable_ip_restriction" class="space-y-4 pt-2">
            <div>
              <label class="input-label">{{ t('keys.ipWhitelist') }}</label>
              <textarea
                v-model="formData.ip_whitelist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipWhitelistPlaceholder')"
              />
              <p class="input-hint">{{ t('keys.ipWhitelistHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('keys.ipBlacklist') }}</label>
              <textarea
                v-model="formData.ip_blacklist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipBlacklistPlaceholder')"
              />
              <p class="input-hint">{{ t('keys.ipBlacklistHint') }}</p>
            </div>
          </div>
        </div>

        <div>
          <label for="key-concurrency-limit" class="input-label">{{ t('keys.concurrencyLimit') }}</label>
          <input
            id="key-concurrency-limit"
            v-model.number="formData.concurrency_limit"
            type="number"
            min="0"
            step="1"
            placeholder="0"
            class="input"
            :class="{ 'border-red-500 dark:border-red-500': concurrencyLimitError }"
            :aria-invalid="!!concurrencyLimitError"
            :aria-describedby="concurrencyLimitError ? 'key-concurrency-hint key-queue-policy key-concurrency-error' : 'key-concurrency-hint key-queue-policy'"
          />
          <p id="key-concurrency-hint" class="input-hint">{{ t('keys.concurrencyLimitHint') }}</p>
          <p id="key-queue-policy" class="input-hint">
            {{ queuePolicyDescription }}
            <span v-if="queuePolicy && concurrencyState.status === 'stale' && Number(formData.concurrency_limit) > 0"> {{ t('keys.concurrencyStale') }}</span>
          </p>
          <p v-if="queuePolicy && queuePolicy.max_waiting > 0 && Number(formData.concurrency_limit) > 0" class="input-hint">{{ t('keys.queueHint') }}</p>
          <p v-if="concurrencyLimitError" id="key-concurrency-error" class="mt-1 text-sm text-red-500" role="alert">
            {{ concurrencyLimitError }}
          </p>
        </div>

        <!-- Quota Limit Section -->
        <div class="space-y-3">
          <label class="input-label">{{ t('keys.quotaLimit') }}</label>
          <!-- Switch commented out - always show input, 0 = unlimited
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.quotaLimit') }}</label>
            <button
              type="button"
              role="switch"
              :aria-checked="formData.enable_quota"
              :aria-label="t('keys.quota')"
              @click="formData.enable_quota = !formData.enable_quota"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_quota ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_quota ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          -->

          <div class="space-y-4">
            <div>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.quota"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="t('keys.quotaAmountPlaceholder')"
                />
              </div>
              <p class="input-hint">{{ t('keys.quotaAmountHint') }}</p>
            </div>

            <!-- Quota used display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey && selectedKey.quota > 0">
              <label class="input-label">{{ t('keys.quotaUsed') }}</label>
              <div class="flex items-center gap-2">
                <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700">
                  <span class="font-medium text-gray-900 dark:text-white">
                    ${{ selectedKey.quota_used?.toFixed(4) || '0.0000' }}
                  </span>
                  <span class="mx-2 text-gray-400">/</span>
                  <span class="text-gray-500 dark:text-gray-400">
                    ${{ selectedKey.quota?.toFixed(2) || '0.00' }}
                  </span>
                </div>
                <button
                  type="button"
                  @click="confirmResetQuota"
                  class="btn btn-secondary text-sm"
                  :title="t('keys.resetQuotaUsed')"
                >
                  {{ t('keys.reset') }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Rate Limit Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.rateLimitSection') }}</label>
            <Toggle :model-value="formData.enable_rate_limit" :aria-label="t('keys.rateLimitColumn')" @click="formData.enable_rate_limit = !formData.enable_rate_limit" size="small" />
          </div>

          <div v-if="formData.enable_rate_limit" class="space-y-4 pt-2">
            <p class="input-hint -mt-2">{{ t('keys.rateLimitHint') }}</p>
            <!-- 5-Hour Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit5h') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.rate_limit_5h"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_5h > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'text-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_5h?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_5h?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'bg-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_5h / selectedKey.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Daily Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit1d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.rate_limit_1d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_1d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'text-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_1d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_1d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'bg-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_1d / selectedKey.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- 7-Day Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit7d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.rate_limit_7d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_7d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'text-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_7d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_7d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'bg-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_7d / selectedKey.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Reset Rate Limit button (edit mode only) -->
            <div v-if="showEditModal && selectedKey && (selectedKey.rate_limit_5h > 0 || selectedKey.rate_limit_1d > 0 || selectedKey.rate_limit_7d > 0)">
              <button
                type="button"
                @click="confirmResetRateLimit"
                class="btn btn-secondary text-sm"
              >
                {{ t('keys.resetRateLimitUsage') }}
              </button>
            </div>
          </div>
        </div>

        <!-- Expiration Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.expiration') }}</label>
            <Toggle :model-value="formData.enable_expiration" :aria-label="t('keys.expiresAt')" @click="formData.enable_expiration = !formData.enable_expiration" size="small" />
          </div>

          <div v-if="formData.enable_expiration" class="space-y-4 pt-2">
            <!-- Quick select buttons (for both create and edit mode) -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="days in ['7', '30', '90']"
                :key="days"
                type="button"
                @click="setExpirationDays(parseInt(days))"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === days
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600'
                ]"
              >
                {{ showEditModal ? t('keys.extendDays', { days }) : t('keys.expiresInDays', { days }) }}
              </button>
              <button
                type="button"
                @click="formData.expiration_preset = 'custom'"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === 'custom'
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600'
                ]"
              >
                {{ t('keys.customDate') }}
              </button>
            </div>

            <!-- Date picker (always show for precise adjustment) -->
            <div>
              <label class="input-label">{{ t('keys.expirationDate') }}</label>
              <input
                v-model="formData.expiration_date"
                type="datetime-local"
                class="input"
              />
              <p class="input-hint">{{ t('keys.expirationDateHint') }}</p>
            </div>

            <!-- Current expiration display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey?.expires_at" class="text-sm">
              <span class="text-gray-500 dark:text-gray-400">{{ t('keys.currentExpiration') }}: </span>
              <span class="font-medium text-gray-900 dark:text-white">
                {{ formatDateTime(selectedKey.expires_at) }}
              </span>
            </div>
          </div>
        </div>
          </div>
        </details>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeModals" type="button" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            form="key-form"
            type="submit"
            :disabled="submitting"
            class="btn btn-primary"
            data-tour="key-form-submit"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{
              submitting
                ? t('keys.saving')
                : showEditModal
                  ? t('common.update')
                  : t('common.create')
            }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BulkEditKeysModal
      :show="showBulkEditModal"
      :selected-keys="selectedApiKeys"
      :groups="groups"
      @close="showBulkEditModal = false"
      @updated="handleBulkUpdated"
    />

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog :busy="handleDeleteAction.busy.value"
      :show="showDeleteDialog"
      :title="t('keys.deleteKey')"
      :message="t('keys.deleteConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="handleDelete"
      @cancel="showDeleteDialog = false"
    />

    <!-- Reset Quota Confirmation Dialog -->
    <ConfirmDialog :busy="resetQuotaUsedAction.busy.value"
      :show="showResetQuotaDialog"
      :title="t('keys.resetQuotaTitle')"
      :message="t('keys.resetQuotaConfirmMessage', { name: selectedKey?.name, used: selectedKey?.quota_used?.toFixed(4) })"
      :confirm-text="t('keys.reset')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="resetQuotaUsed"
      @cancel="showResetQuotaDialog = false"
    />

    <!-- Reset Rate Limit Confirmation Dialog -->
    <ConfirmDialog :busy="resetRateLimitUsageAction.busy.value"
      :show="showResetRateLimitDialog"
      :title="t('keys.resetRateLimitTitle')"
      :message="t('keys.resetRateLimitConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('keys.reset')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="resetRateLimitUsage"
      @cancel="showResetRateLimitDialog = false"
    />


    <BaseDialog :show="showConnectionDialog" :title="kc.connect" width="normal" @close="closeConnection">
      <div v-if="connectionKey" class="space-y-5">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
          <div><AppHeading variant="subsection" as="h3">{{ connectionKey.name }}</AppHeading><code>{{ maskApiKey(connectionKey.key) }}</code></div>
          <button class="btn btn-secondary" @click="copyToClipboard(connectionKey.key, connectionKey.id)">{{ copiedKeyId === connectionKey.id ? t('keys.copied') : kc.copyKey }}</button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <button class="btn btn-primary" data-testid="connect-manual" @click="openConnectionMethod('manual')">{{ kc.appConfiguration }}</button>
          <button v-if="isCodexQuickConfigPlatform(connectionKey.group?.platform)" class="btn btn-secondary" data-testid="quick-config-button" @click="openConnectionMethod('quick')">{{ t('keys.quickConfigure') }}</button>
          <button v-if="!publicSettings?.hide_ccs_import_button && getCcSwitchImportProfiles(connectionKey.group?.platform, publicSettings?.ccs_import_config).length" class="btn btn-secondary" :data-testid="`ccs-import-${connectionKey.id}`" @click="openConnectionMethod('import')">{{ t('keys.importToCcSwitch') }}</button>
        </div>
        <button class="btn btn-secondary" @click="openConnectionPicker">{{ kc.changeKey }}</button>
      </div>
      <div v-else class="space-y-4">
        <label class="input-label" for="connection-key-search">{{ kc.chooseKey }}</label>
        <form class="workspace-filter-controls flex gap-2" @submit.prevent="loadConnectionKeys()">
          <input id="connection-key-search" v-model="connectionSearch" class="input" :placeholder="t('keys.searchPlaceholder')" />
          <button type="submit" class="btn btn-secondary">{{ kc.search }}</button>
        </form>
        <p v-if="connectionError" role="alert">{{ connectionError }}</p>
        <p v-if="connectionLoading" role="status">{{ t('common.loading') }}</p>
        <DocumentRows v-else as="ul" :items="connectionKeys" item-key="id" :estimate-height="48" class="divide-y divide-gray-200 dark:divide-dark-700">
          <template #default="{ item: key }"><button class="flex w-full items-center justify-between gap-3 py-3 text-left" :data-testid="`choose-connection-key-${key.id}`" @click="openConnection(key)"><span>{{ key.name }}</span><code>{{ maskApiKey(key.key) }}</code></button></template>
        </DocumentRows>
        <p v-if="!connectionLoading && !connectionError && !connectionKeys.length">{{ t('keys.noKeysYet') }}</p>
        <div class="flex flex-wrap justify-between gap-3">
          <InfiniteListSentinel :loading="connectionLoading" :error="connectionError" :has-more="connectionPickerActive && connectionHasMore" :progress="connectionKeys.length" @load-more="connectionList.loadMore" @retry="retryConnectionKeys" />
          <button class="btn btn-primary" @click="createFromConnection">{{ t('keys.createKey') }}</button>
        </div>
      </div>
    </BaseDialog>

    <!-- Use Key Modal -->
    <UseKeyModal
      :show="showUseKeyModal"
      :api-key="selectedKey?.key || ''"
      :base-url="publicSettings?.api_base_url || ''"
      :platform="selectedKey?.group?.platform || null"
      :allow-messages-dispatch="selectedKey?.group?.allow_messages_dispatch || false"
      @close="closeUseKeyModal"
    />

    <CodexQuickConfigModal
      :show="showCodexQuickConfigModal"
      :api-key="selectedKey?.key || ''"
      :base-url="publicSettings?.api_base_url || ''"
      :platform="selectedCodexQuickConfigPlatform"
      @close="closeCodexQuickConfigModal"
    />

    <!-- One profile launches directly; multiple profiles let the user choose. -->
    <BaseDialog
      :show="showCcsClientSelect"
      :title="t('keys.ccsClientSelect.title')"
      width="narrow"
      @close="closeCcsClientSelect"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          {{ t('keys.ccsClientSelect.description') }}
        </p>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <button
            v-for="profile in ccsProfileOptions"
            :key="profile.id"
            :data-testid="`ccs-profile-${profile.id}`"
            @click="handleCcsClientSelect(profile)"
            class="flex flex-col items-center gap-2 p-4 rounded-xl border-2 border-gray-200 dark:border-dark-600 hover:border-primary-500 dark:hover:border-primary-500 hover:bg-primary-50 dark:hover:bg-primary-900/20 transition-all"
          >
            <Icon :name="profile.app === 'gemini' ? 'sparkles' : 'terminal'" size="xl" class="text-gray-600 dark:text-gray-400" />
            <span class="font-medium text-gray-900 dark:text-white">{{ profile.label }}</span>
            <span v-if="profile.parameters.model" class="text-xs text-gray-500 dark:text-gray-400">{{ profile.parameters.model }}</span>
          </button>
        </div>
	      </div>
      <template #footer>
        <div class="flex justify-end">
          <button @click="closeCcsClientSelect" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Group Selector Dropdown (Teleported to body to avoid overflow clipping) -->
    <Teleport to="body">
      <div
        v-if="groupSelectorKeyId !== null && dropdownPosition"
        ref="dropdownRef"
        data-testid="key-group-selector"
        class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-max max-w-[calc(100vw-16px)] overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 sm:min-w-[380px] dark:bg-dark-800 dark:ring-white/10"
        style="pointer-events: auto !important;"
        :style="{
          top: dropdownPosition.top !== undefined ? dropdownPosition.top + 'px' : undefined,
          bottom: dropdownPosition.bottom !== undefined ? dropdownPosition.bottom + 'px' : undefined,
          left: dropdownPosition.left + 'px'
        }"
      >
        <!-- Search box -->
        <div class="border-b border-gray-100 p-2 dark:border-dark-700">
          <div class="relative">
            <svg class="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              v-model="groupSearchQuery"
              type="text"
              class="w-full rounded-lg border border-gray-200 bg-gray-50 py-1.5 pl-8 pr-3 text-sm text-gray-900 placeholder-gray-400 outline-none focus:border-primary-300 focus:ring-1 focus:ring-primary-300 dark:border-dark-600 dark:bg-dark-700 dark:text-white dark:placeholder-gray-500 dark:focus:border-primary-600 dark:focus:ring-primary-600"
              :placeholder="t('keys.searchGroup')"
              @click.stop
            />
          </div>
        </div>
        <!-- Group list -->
        <div class="max-h-80 overflow-y-auto p-1.5">
          <button
            v-for="option in filteredGroupOptions"
            :key="option.value ?? 'null'"
            @click="changeGroup(selectedKeyForGroup!, option.value)"
            :class="[
              'flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-sm transition-colors',
              'border-b border-gray-100 last:border-0 dark:border-dark-700',
              selectedKeyForGroup?.group_id === option.value ||
              (!selectedKeyForGroup?.group_id && option.value === null)
                ? 'bg-primary-50 dark:bg-primary-900/20'
                : 'hover:bg-gray-100 dark:hover:bg-dark-700'
            ]"
            :title="option.description || undefined"
          >
            <GroupOptionItem
              :name="option.label"
              :platform="option.platform"
              :subscription-type="option.subscriptionType"
              :rate-multiplier="option.rate"
              :user-rate-multiplier="option.userRate"
              :peak-rate-enabled="option.peakRateEnabled"
              :peak-start="option.peakStart"
              :peak-end="option.peakEnd"
              :peak-rate-multiplier="option.peakRateMultiplier"
              :description="option.description"
              :selected="
                selectedKeyForGroup?.group_id === option.value ||
                (!selectedKeyForGroup?.group_id && option.value === null)
              "
            />
          </button>
          <!-- Empty state when search has no results -->
          <div v-if="filteredGroupOptions.length === 0" class="py-4 text-center text-sm text-gray-400 dark:text-gray-500">
            {{ t('keys.noGroupFound') }}
          </div>
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
import AppHeading from '@/components/common/AppHeading.vue'
	import { ref, computed, watch, onMounted, onUnmounted, type ComponentPublicInstance } from 'vue'
	import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useKeyConnectionCopy } from '@/components/keys/keyConnectionCopy'
import { useKeyWorkspaceCopy } from '@/components/keys/keyWorkspaceCopy'
	import { useAppStore } from '@/stores/app'
	import { useOnboardingStore } from '@/stores/onboarding'
	import { useClipboard } from '@/composables/useClipboard'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useTableColumns } from '@/composables/useTableColumns'
import ColumnSettings from '@/components/common/ColumnSettings.vue'

const { t } = useI18n()
const sortToolbarTarget = ref<HTMLElement | null>(null)
const kc = useKeyConnectionCopy()
const kw = useKeyWorkspaceCopy()
const route = useRoute()
const router = useRouter()
import { keysAPI, authAPI, usageAPI, userGroupsAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import BulkEditKeysModal from '@/components/keys/BulkEditKeysModal.vue'
	import KeyWorkspaceTable from '@/components/keys/KeyWorkspaceTable.vue'
	import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'
	import BaseDialog from '@/components/common/BaseDialog.vue'
	import { useConfirmedAction } from '@/composables/useConfirmation'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
	import EmptyState from '@/components/common/EmptyState.vue'
	import Select from '@/components/common/Select.vue'
	import SearchInput from '@/components/common/SearchInput.vue'
	import Icon from '@/components/icons/Icon.vue'
	import UseKeyModal from '@/components/keys/UseKeyModal.vue'
	import CodexQuickConfigModal from '@/components/keys/CodexQuickConfigModal.vue'
	import EndpointPopover from '@/components/keys/EndpointPopover.vue'
	import GroupBadge from '@/components/common/GroupBadge.vue'
	import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
	import type { ApiKey, ApiKeyConcurrencySnapshot, Group, PublicSettings, SubscriptionType, GroupPlatform, UpdateApiKeyRequest } from '@/types'
import type { Column } from '@/components/common/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import { formatDateTime } from '@/utils/format'
import { maskApiKey } from '@/utils/maskApiKey'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBadgeLightClass } from '@/utils/platformColors'
import { KEY_GROUP_PROVIDERS, KEY_GROUP_PROVIDER_ICONS, getKeyGroupProvider, type KeyGroupProvider } from '@/utils/keyGroupProviders'
import {
  buildCcSwitchImportDeeplink,
  getCcSwitchImportProfiles
} from '@/utils/ccswitchImport'
import type { CcsImportProfile } from '@/types/ccswitch'
import { isCodexQuickConfigPlatform } from '@/utils/codexQuickConfig'

// Helper to format date for datetime-local input
const formatDateTimeLocal = (isoDate: string): string => {
  const date = new Date(isoDate)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

interface GroupOption {
  value: number
  label: string
  description: string | null
  rate: number
  userRate: number | null
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  subscriptionType: SubscriptionType
  platform: GroupPlatform
}

const appStore = useAppStore()
const onboardingStore = useOnboardingStore()
const { copyToClipboard: clipboardCopy } = useClipboard()

const allColumns = computed<Column[]>(() => [
  { key: 'id', label: t('keys.id'), sortable: true },
  { key: 'name', label: t('common.name'), sortable: true },
  { key: 'key', label: t('keys.apiKey'), sortable: false },
  { key: 'group', label: t('keys.group'), sortable: true },
  { key: 'usage', label: t('keys.usage'), sortable: false },
  { key: 'current_concurrency', label: kw.value.concurrent, sortable: true },
  { key: 'status', label: t('common.status'), sortable: true },
  { key: 'actions', label: t('common.actions'), sortable: false },
  { key: 'rate_limit', label: t('keys.rateLimitColumn'), sortable: false },
  { key: 'last_used_at', label: t('keys.lastUsedAt'), sortable: true },
  { key: 'expires_at', label: t('keys.expiresAt'), sortable: true },
  { key: 'last_used_ip', label: t('keys.lastUsedIP'), sortable: false },
  { key: 'created_at', label: t('keys.created'), sortable: true }
])

const {
  columns, orderedColumns, hiddenColumns, toggleColumn, setColumnOrder, resetColumns
} = useTableColumns('table-columns:keys:v1', allColumns, {
  required: ['id', 'name', 'actions'],
  primary: ['id', 'name', 'key', 'group', 'usage', 'current_concurrency', 'status', 'actions']
})

const busyKeyIds = ref(new Set<number>())
function setKeyBusy(id: number, busy: boolean) {
  const next = new Set(busyKeyIds.value)
  if (busy) next.add(id)
  else next.delete(id)
  busyKeyIds.value = next
}
const renameKey = async (key: ApiKey, value: string): Promise<boolean> => {
  const name = value.trim()
  if (!name || busyKeyIds.value.has(key.id)) return false
  if (name === key.name) return true
  setKeyBusy(key.id, true)
  try {
    const saved = await keysAPI.update(key.id, { name })
    apiKeys.value = apiKeys.value.map(item => item.id === key.id ? { ...item, ...saved } : item)
    appStore.showSuccess(t('keys.keyUpdatedSuccess'))
    return true
  } catch {
    appStore.showError(t('keys.failedToSave'))
    return false
  } finally {
    setKeyBusy(key.id, false)
  }
}
const selectedIds = ref<number[]>([])
const showBulkEditModal = ref(false)
const selectedApiKeys = computed(() => apiKeys.value.filter((key) => selectedIds.value.includes(key.id)))

const handleSelectionChange = (ids: Array<string | number>) => {
  const visibleIds = new Set(apiKeys.value.map((key) => key.id))
  selectedIds.value = [...new Set(ids.map(Number))].filter((id) => visibleIds.has(id))
}

const handleBulkUpdated = (succeededIds: number[]) => {
  const succeeded = new Set(succeededIds)
  selectedIds.value = selectedIds.value.filter((id) => !succeeded.has(id))
  loadApiKeys()
}

const groups = ref<Group[]>([])
const submitting = ref(false)
const now = ref(new Date())
let resetTimer: ReturnType<typeof setInterval> | null = null
const usageStats = ref<Record<string, BatchApiKeyUsageStats>>({})
const userGroupRates = ref<Record<number, number>>({})

// Policy and counts always belong to one completed refresh, never to editable key data.
const concurrencyState = ref<{
  snapshot: ApiKeyConcurrencySnapshot | null
  status: 'loading' | 'ready' | 'stale' | 'error'
}>({ snapshot: null, status: 'loading' })
const queuePolicy = computed(() => concurrencyState.value.snapshot?.queue_policy)
const concurrencyRows = computed(() => {
  const counts = new Map(concurrencyState.value.snapshot?.items.map(item => [item.id, item]))
  return Object.fromEntries(apiKeys.value.map(key => {
    const count = counts.get(key.id)
    const current = count?.current_concurrency
    const waiting = count?.current_waiting
    const known = typeof current === 'number' && typeof waiting === 'number'
    const status = concurrencyState.value.status
    const limit = key.concurrency_limit
    return [key.id, {
      limit,
      current,
      waiting,
      concurrencyFull: status === 'ready' && typeof current === 'number' && limit > 0 && current >= limit,
      full: status === 'ready' && known && limit > 0 &&
        (queuePolicy.value?.max_waiting ?? 0) > 0 && waiting >= queuePolicy.value!.max_waiting,
      notice: status === 'stale' && count ? t('keys.concurrencyStale') :
        !known ? t(status === 'loading' ? 'keys.concurrencyLoading' : 'keys.concurrencyUnavailable') : '',
    }]
  }))
})
let concurrencyTimer: ReturnType<typeof setTimeout> | null = null
let concurrencyController: AbortController | null = null
let concurrencyGeneration = 0
let concurrencyPageReady = false
let disposed = false

const stopConcurrencyRefresh = () => {
  concurrencyGeneration++
  if (concurrencyTimer) clearTimeout(concurrencyTimer)
  concurrencyTimer = null
  concurrencyController?.abort()
  concurrencyState.value = {
    ...concurrencyState.value,
    status: concurrencyState.value.snapshot ? 'stale' : 'loading',
  }
}

const refreshConcurrency = async () => {
  if (disposed || document.hidden || !concurrencyPageReady || concurrencyController) return
  const controller = new AbortController()
  concurrencyController = controller
  const generation = concurrencyGeneration
  const ids = apiKeys.value.map(key => key.id)
  try {
    let snapshot: ApiKeyConcurrencySnapshot | null = null
    // Configured page sizes can exceed the endpoint's 100-ID limit; read serially.
    for (let offset = 0; offset < Math.max(ids.length, 1); offset += 100) {
      const result = await keysAPI.getConcurrency(ids.slice(offset, offset + 100), { signal: controller.signal })
      if (disposed || controller.signal.aborted || generation !== concurrencyGeneration) return
      if (snapshot) {
        if (snapshot.queue_policy.max_waiting !== result.queue_policy.max_waiting ||
          snapshot.queue_policy.timeout_seconds !== result.queue_policy.timeout_seconds) {
          throw new Error('API key queue policy changed during the statistics refresh')
        }
        snapshot.items.push(...result.items)
      } else {
        snapshot = { queue_policy: result.queue_policy, items: [...result.items] }
      }
    }
    concurrencyState.value = { snapshot, status: 'ready' }
  } catch {
    if (!controller.signal.aborted && generation === concurrencyGeneration && !disposed) {
      concurrencyState.value = {
        ...concurrencyState.value,
        status: concurrencyState.value.snapshot ? 'stale' : 'error',
      }
    }
  } finally {
    concurrencyController = null
    if (!disposed && !document.hidden && concurrencyPageReady) {
      if (generation !== concurrencyGeneration) void refreshConcurrency()
      else concurrencyTimer = setTimeout(refreshConcurrency, 5000)
    }
  }
}

const handleConcurrencyVisibility = () => {
  stopConcurrencyRefresh()
  if (!document.hidden) void refreshConcurrency()
}

const sortState = ref({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

// Filter state
const filterSearch = ref('')
const filterStatus = ref('')
const filterGroupId = ref<string | number>('')

const showCreateModal = ref(false)
const showEditModal = ref(false)
const showDeleteDialog = ref(false)
const showResetQuotaDialog = ref(false)
const showResetRateLimitDialog = ref(false)
const showConnectionDialog = ref(false)
const connectionKey = ref<ApiKey | null>(null)
const connectionSearch = ref('')
const connectionQuery = ref('')
const connectionPickerActive = ref(false)
const connectionRouteLoading = ref(false)
const connectionRouteError = ref('')
const connectionList = useInfiniteList<ApiKey>({
  pageSize: 10,
  keyOf: key => key.id,
  fetchPage: (page, signal) => keysAPI.list(page, 10, {
    search: connectionQuery.value || undefined, sort_by: 'created_at', sort_order: 'desc'
  }, { signal }),
})
const { items: connectionKeys, hasMore: connectionHasMore } = connectionList
const connectionLoading = computed(() => connectionRouteLoading.value || connectionList.loading.value || connectionList.loadingMore.value)
const connectionError = computed(() => connectionRouteError.value || (connectionList.error.value ? (connectionList.error.value as Error)?.message || t('keys.failedToLoad') : ''))
const showUseKeyModal = ref(false)
const showCodexQuickConfigModal = ref(false)
const showCcsClientSelect = ref(false)
const pendingCcsRow = ref<ApiKey | null>(null)
const selectedKey = ref<ApiKey | null>(null)
const selectedCodexQuickConfigPlatform = computed(() => {
  const platform = selectedKey.value?.group?.platform
  return isCodexQuickConfigPlatform(platform) ? platform : null
})
const copiedKeyId = ref<number | null>(null)
const groupSelectorKeyId = ref<number | null>(null)
const publicSettings = ref<PublicSettings | null>(null)
const ccsProfileOptions = computed(() => getCcSwitchImportProfiles(
  pendingCcsRow.value?.group?.platform,
  publicSettings.value?.ccs_import_config
))
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownPosition = ref<{ top?: number; bottom?: number; left: number } | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())

// Get the currently selected key for group change
const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

const formData = ref({
  name: '',
  concurrency_limit: 0 as number | string,
  group_id: null as number | null,
  group_ids: [] as number[],
  status: 'active' as 'active' | 'inactive',
  use_custom_key: false,
  custom_key: '',
  enable_ip_restriction: false,
  ip_whitelist: '',
  ip_blacklist: '',
  // Quota settings (empty = unlimited)
  enable_quota: false,
  quota: null as number | null,
  // Rate limit settings
  enable_rate_limit: false,
  rate_limit_5h: null as number | null,
  rate_limit_1d: null as number | null,
  rate_limit_7d: null as number | null,
  enable_expiration: false,
  expiration_preset: '30' as '7' | '30' | '90' | 'custom',
  expiration_date: ''
})

const concurrencyLimitError = computed(() => {
  const limit = Number(formData.value.concurrency_limit)
  return Number.isSafeInteger(limit) && limit >= 0 ? '' : t('keys.concurrencyLimitInvalid')
})

const queuePolicyDescription = computed(() => {
  if (Number(formData.value.concurrency_limit) === 0) return t('keys.queueNotApplicable')
  if (!queuePolicy.value) return t(concurrencyState.value.status === 'loading' ? 'keys.queuePolicyLoading' : 'keys.queuePolicyUnavailable')
  if (queuePolicy.value.max_waiting === 0) return t('keys.queuePolicyOff')
  return t('keys.queuePolicy', { max: queuePolicy.value.max_waiting, seconds: queuePolicy.value.timeout_seconds })
})

// 自定义Key验证
const customKeyError = computed(() => {
  if (!formData.value.use_custom_key || !formData.value.custom_key) {
    return ''
  }
  const key = formData.value.custom_key
  if (key.length < 16) {
    return t('keys.customKeyTooShort')
  }
  // 检查字符：只允许字母、数字、下划线、连字符
  if (!/^[a-zA-Z0-9_-]+$/.test(key)) {
    return t('keys.customKeyInvalidChars')
  }
  return ''
})

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])

const shouldSubmitEditStatus = (key: ApiKey, status: 'active' | 'inactive') => {
  if (key.status === 'quota_exhausted' || key.status === 'expired') {
    return status === 'active'
  }
  return true
}

// Filter dropdown options
const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') },
  { value: 0, label: t('keys.noGroup') },
  ...groups.value.map((g) => ({ value: g.id, label: g.name }))
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('keys.allStatus') },
  { value: 'active', label: t('keys.status.active') },
  { value: 'inactive', label: t('keys.status.inactive') },
  { value: 'quota_exhausted', label: t('keys.status.quota_exhausted') },
  { value: 'expired', label: t('keys.status.expired') }
])

const onFilterChange = () => {
  selectedIds.value = []
  resetApiKeys()
}

const onGroupFilterChange = (value: string | number | boolean | null) => {
  filterGroupId.value = value as string | number
  onFilterChange()
}

const onStatusFilterChange = (value: string | number | boolean | null) => {
  filterStatus.value = value as string
  onFilterChange()
}

// Convert groups to Select options format with rate multiplier and subscription type
const groupOptions = computed(() =>
  groups.value.map((group) => ({
    value: group.id,
    label: group.name,
    description: group.description,
    rate: group.rate_multiplier,
    userRate: userGroupRates.value[group.id] ?? null,
    peakRateEnabled: group.peak_rate_enabled,
    peakStart: group.peak_start,
    peakEnd: group.peak_end,
    peakRateMultiplier: group.peak_rate_multiplier,
    subscriptionType: group.subscription_type,
    platform: group.platform
  }))
)

const createProvider = ref<KeyGroupProvider>('anthropic')
const createProviderOptions = computed(() => KEY_GROUP_PROVIDERS.map((value) => ({
  value,
  label: t(`keys.providers.${value}`),
  count: groups.value.filter((group) => getKeyGroupProvider(group.platform) === value).length
})))

const formGroupOptions = computed(() => showEditModal.value
  ? groupOptions.value
  : groupOptions.value.filter((group) => getKeyGroupProvider(group.platform) === createProvider.value)
)

const selectCreateProvider = (provider: KeyGroupProvider) => {
  if (createProvider.value === provider) return
  createProvider.value = provider
  formData.value.group_id = null
}

// Also handles groups arriving after the create dialog has already opened.
watch([showCreateModal, createProviderOptions], ([isOpen, providers], [wasOpen]) => {
  if (!isOpen) return
  if (!wasOpen || !providers.some((provider) => provider.value === createProvider.value && provider.count > 0)) {
    selectCreateProvider(providers.find((provider) => provider.count > 0)?.value ?? 'anthropic')
  }
  if (!formGroupOptions.value.some((group) => group.value === formData.value.group_id)) {
    formData.value.group_id = null
  }
})

// Group dropdown search
const groupSearchQuery = ref('')
const filteredGroupOptions = computed(() => {
  const query = groupSearchQuery.value.trim().toLowerCase()
  if (!query) return groupOptions.value
  return groupOptions.value.filter((opt) => {
    return opt.label.toLowerCase().includes(query) ||
      (opt.description && opt.description.toLowerCase().includes(query))
  })
})

const copyToClipboard = async (text: string, keyId: number) => {
  const success = await clipboardCopy(text, t('keys.copied'))
  if (success) {
    copiedKeyId.value = keyId
    setTimeout(() => {
      copiedKeyId.value = null
    }, 800)
  }
}

const isAbortError = (error: unknown) => {
  if (!error || typeof error !== 'object') return false
  const { name, code } = error as { name?: string; code?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED'
}

const {
  items: apiKeys, loading, loadingMore, refreshing, error: listError, hasMore: hasMoreKeys,
  reset: resetKeys, refresh: refreshKeys, loadMore: appendKeys, retry: retryKeyPage,
} = useInfiniteList<ApiKey>({
  pageSize: 50,
  keyOf: key => key.id,
  fetchPage: async (page, signal) => {
    const response = await keysAPI.list(page, 50, {
      search: filterSearch.value || undefined,
      status: filterStatus.value || undefined,
      group_id: filterGroupId.value === '' ? undefined : filterGroupId.value,
      sort_by: sortState.value.sort_by,
      sort_order: sortState.value.sort_order,
    }, { signal })
    if (response.items.length && !signal.aborted) {
      try {
        const stats = await usageAPI.getDashboardApiKeysUsage(response.items.map(key => key.id), { signal })
        if (!signal.aborted) usageStats.value = { ...usageStats.value, ...stats.stats }
      } catch (error) {
        if (!isAbortError(error)) console.error('Failed to load usage stats:', error)
      }
    }
    return response
  },
})

async function updateKeyList(load: () => Promise<void>) {
  concurrencyPageReady = false
  stopConcurrencyRefresh()
  await load()
  if (disposed || loading.value || loadingMore.value || refreshing.value) return
  handleSelectionChange(selectedIds.value)
  concurrencyPageReady = true
  void refreshConcurrency()
}
const loadApiKeys = () => updateKeyList(refreshKeys)
const resetApiKeys = () => {
  usageStats.value = {}
  return updateKeyList(resetKeys)
}
const loadMoreKeys = () => updateKeyList(appendKeys)
const retryKeys = () => updateKeyList(retryKeyPage)
watch(listError, error => { if (error) appStore.showError(t('keys.failedToLoad')) })

const groupPricingLoading = ref(false)
let groupPricingGeneration = 0

const loadGroupPricing = async () => {
  const generation = ++groupPricingGeneration
  groupPricingLoading.value = true
  try {
    const [availableGroups, rates] = await Promise.all([
      userGroupsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates()
    ])
    if (disposed || generation !== groupPricingGeneration) return
    // Apply both together so a failed refresh cannot mix new defaults with old overrides.
    groups.value = availableGroups
    userGroupRates.value = rates
  } catch (error) {
    if (disposed || generation !== groupPricingGeneration) return
    console.error('Failed to load group pricing:', error)
    appStore.showError(t('keys.failedToLoad'))
  } finally {
    if (generation === groupPricingGeneration) groupPricingLoading.value = false
  }
}

const refreshPage = async () => {
  await Promise.all([loadApiKeys(), loadGroupPricing()])
}

watch([showCreateModal, showEditModal], ([creating, editing]) => {
  if (creating || editing) void loadGroupPricing()
})

const loadPublicSettings = async () => {
  try {
    publicSettings.value = await authAPI.getPublicSettings()
  } catch (error) {
    console.error('Failed to load public settings:', error)
  }
}

const clearConnectionQuery = () => {
  if (!route?.query || !['create', 'connect', 'key', 'key_id'].some((key) => key in route.query)) return
  const query = { ...route.query }
  delete query.create
  delete query.connect
  delete query.key
  delete query.key_id
  void router.replace({ path: route.path, query })
}
const loadConnectionKeys = () => {
  connectionQuery.value = connectionSearch.value.trim()
  connectionPickerActive.value = true
  connectionRouteError.value = ''
  return connectionList.reset()
}
const retryConnectionKeys = () => connectionPickerActive.value ? connectionList.retry() : applyConnectionRoute()
const openConnectionPicker = () => {
  ++connectionRouteRequest
  connectionRouteLoading.value = false
  connectionKey.value = null
  showConnectionDialog.value = true
  void loadConnectionKeys()
}
const openConnection = (key: ApiKey) => {
  connectionPickerActive.value = false
  connectionList.cancel()
  connectionKey.value = key
  showConnectionDialog.value = true
}
const closeConnection = () => {
  showConnectionDialog.value = false
  connectionKey.value = null
  connectionList.cancel()
  ++connectionRouteRequest
  connectionRouteLoading.value = false
  clearConnectionQuery()
}
const createFromConnection = () => {
  closeConnection()
  showCreateModal.value = true
}
const openConnectionMethod = (method: 'manual' | 'quick' | 'import') => {
  const key = connectionKey.value
  if (!key) return
  showConnectionDialog.value = false
  if (method === 'manual') openUseKeyModal(key)
  else if (method === 'quick') openCodexQuickConfigModal(key)
  else {
    importToCcswitch(key)
    if (!showCcsClientSelect.value) showConnectionDialog.value = true
  }
}
const viewKeyRequests = (key: ApiKey) => router.push({ path: '/usage', query: { view: 'records', api_key_id: String(key.id) } })
let connectionRouteRequest = 0
const applyConnectionRoute = async () => {
  const request = ++connectionRouteRequest
  if (route?.query?.create === '1') {
    showCreateModal.value = true
    clearConnectionQuery()
    return
  }
  if (route?.query?.connect !== '1') return
  const id = Number(route.query.key_id ?? route.query.key)
  if (!Number.isSafeInteger(id) || id <= 0) {
    openConnectionPicker()
    return
  }
  // Resolve the requested object through the authenticated API, regardless of list filters/page.
  connectionKey.value = null
  connectionPickerActive.value = false
  connectionList.clear()
  connectionRouteError.value = ''
  connectionRouteLoading.value = true
  showConnectionDialog.value = true
  try {
    const key = await keysAPI.getById(id)
    if (request === connectionRouteRequest && !disposed) openConnection(key)
  } catch (error: any) {
    if (request === connectionRouteRequest) connectionRouteError.value = error?.message || t('keys.failedToLoad')
  } finally {
    if (request === connectionRouteRequest) connectionRouteLoading.value = false
  }
}
watch(() => [route?.query?.create, route?.query?.connect, route?.query?.key, route?.query?.key_id], () => { void applyConnectionRoute() })

const openUseKeyModal = (key: ApiKey) => {
  selectedKey.value = key
  showUseKeyModal.value = true
}

const openCodexQuickConfigModal = (key: ApiKey) => {
  selectedKey.value = key
  showCodexQuickConfigModal.value = true
}

const closeUseKeyModal = () => {
  showUseKeyModal.value = false
  selectedKey.value = null
  if (connectionKey.value) showConnectionDialog.value = true
}

const closeCodexQuickConfigModal = () => {
  showCodexQuickConfigModal.value = false
  selectedKey.value = null
  if (connectionKey.value) showConnectionDialog.value = true
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  selectedIds.value = []
  sortState.value.sort_by = key
  sortState.value.sort_order = order
  resetApiKeys()
}

const selectedFormGroupIDs = (): number[] => [...new Set([
  ...(formData.value.group_id === null ? [] : [formData.value.group_id]),
  ...formData.value.group_ids.filter(id => id !== formData.value.group_id)
])]

const editKey = (key: ApiKey) => {
  selectedKey.value = key
  const hasIPRestriction = (key.ip_whitelist?.length > 0) || (key.ip_blacklist?.length > 0)
  const hasExpiration = !!key.expires_at
  formData.value = {
    name: key.name,
    concurrency_limit: key.concurrency_limit ?? 0,
    group_id: key.group_id,
    group_ids: (key.group_ids ?? []).filter(id => id !== key.group_id),
    status: key.status === 'quota_exhausted' || key.status === 'expired' ? 'inactive' : key.status,
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: hasIPRestriction,
    ip_whitelist: (key.ip_whitelist || []).join('\n'),
    ip_blacklist: (key.ip_blacklist || []).join('\n'),
    enable_quota: key.quota > 0,
    quota: key.quota > 0 ? key.quota : null,
    enable_rate_limit: (key.rate_limit_5h > 0) || (key.rate_limit_1d > 0) || (key.rate_limit_7d > 0),
    rate_limit_5h: key.rate_limit_5h || null,
    rate_limit_1d: key.rate_limit_1d || null,
    rate_limit_7d: key.rate_limit_7d || null,
    enable_expiration: hasExpiration,
    expiration_preset: 'custom',
    expiration_date: key.expires_at ? formatDateTimeLocal(key.expires_at) : ''
  }
  showEditModal.value = true
}

const toggleKeyStatus = async (key: ApiKey) => {
  if (!['active', 'inactive'].includes(key.status) || busyKeyIds.value.has(key.id)) return
  const newStatus = key.status === 'active' ? 'inactive' : 'active'
  setKeyBusy(key.id, true)
  try {
    await keysAPI.toggleStatus(key.id, newStatus)
    appStore.showSuccess(newStatus === 'active' ? t('keys.keyEnabledSuccess') : t('keys.keyDisabledSuccess'))
    await loadApiKeys()
  } catch {
    appStore.showError(t('keys.failedToUpdateStatus'))
  } finally {
    setKeyBusy(key.id, false)
  }
}

const openGroupSelector = (key: ApiKey) => {
  if (groupSelectorKeyId.value === key.id) {
    groupSelectorKeyId.value = null
    dropdownPosition.value = null
  } else {
    void loadGroupPricing()
    const buttonEl = groupButtonRefs.value.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const dropdownEstHeight = 400 // estimated max dropdown height
      const dropdownEstWidth = Math.min(380, window.innerWidth - 16)
      const spaceBelow = window.innerHeight - rect.bottom
      const spaceAbove = rect.top
      // 夹取 left，避免窄屏下浮层超出视口右缘
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - dropdownEstWidth - 8))

      if (spaceBelow < dropdownEstHeight && spaceAbove > spaceBelow) {
        // Not enough space below, pop upward
        dropdownPosition.value = {
          bottom: window.innerHeight - rect.top + 4,
          left
        }
      } else {
        // Default: pop downward
        dropdownPosition.value = {
          top: rect.bottom + 4,
          left
        }
      }
    }
    groupSelectorKeyId.value = key.id
    groupSearchQuery.value = ''
  }
}

const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
  if (key.group_id === newGroupId || busyKeyIds.value.has(key.id)) return
  if ((key.group_ids?.length ?? 0) > 1) { editKey(key); return }
  setKeyBusy(key.id, true)
  try {
    await keysAPI.update(key.id, { group_id: newGroupId, group_ids: newGroupId === null ? [] : [newGroupId] })
    appStore.showSuccess(t('keys.groupChangedSuccess'))
    await loadApiKeys()
  } catch {
    appStore.showError(t('keys.failedToChangeGroup'))
  } finally {
    setKeyBusy(key.id, false)
  }
}

const closeGroupSelector = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  // Check if click is inside the dropdown or the trigger button
  if (!target.closest('.group\\/dropdown') && !dropdownRef.value?.contains(target)) {
    groupSelectorKeyId.value = null
    dropdownPosition.value = null
  }
}

const confirmDelete = (key: ApiKey) => {
  selectedKey.value = key
  showDeleteDialog.value = true
}

const handleSubmit = async () => {
  if (concurrencyLimitError.value) {
    appStore.showError(concurrencyLimitError.value)
    return
  }
  // An empty input explicitly clears the saved key limit.
  const concurrencyLimit = Number(formData.value.concurrency_limit)

  // Validate group_id is required
  if (formData.value.group_id === null) {
    appStore.showError(t('keys.groupRequired'))
    return
  }

  // Validate custom key if enabled
  if (!showEditModal.value && formData.value.use_custom_key) {
    if (!formData.value.custom_key) {
      appStore.showError(t('keys.customKeyRequired'))
      return
    }
    if (customKeyError.value) {
      appStore.showError(customKeyError.value)
      return
    }
  }

  // Parse IP lists only if IP restriction is enabled
  const parseIPList = (text: string): string[] =>
    text.split('\n').map(ip => ip.trim()).filter(ip => ip.length > 0)
  const ipWhitelist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_whitelist) : []
  const ipBlacklist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_blacklist) : []

  // Calculate quota value (null/empty/0 = unlimited, stored as 0)
  const quota = formData.value.quota && formData.value.quota > 0 ? formData.value.quota : 0

  // Calculate expiration
  let expiresInDays: number | undefined
  let expiresAt: string | null | undefined
  if (formData.value.enable_expiration && formData.value.expiration_date) {
    if (!showEditModal.value) {
      // Create mode: calculate days from date
      const expDate = new Date(formData.value.expiration_date)
      const now = new Date()
      const diffDays = Math.ceil((expDate.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
      expiresInDays = diffDays > 0 ? diffDays : 1
    } else {
      // Edit mode: use custom date directly
      expiresAt = new Date(formData.value.expiration_date).toISOString()
    }
  } else if (showEditModal.value) {
    // Edit mode: if expiration disabled or date cleared, send empty string to clear
    expiresAt = ''
  }

  // Calculate rate limit values (send 0 when toggle is off)
  const rateLimitData = formData.value.enable_rate_limit ? {
    rate_limit_5h: formData.value.rate_limit_5h && formData.value.rate_limit_5h > 0 ? formData.value.rate_limit_5h : 0,
    rate_limit_1d: formData.value.rate_limit_1d && formData.value.rate_limit_1d > 0 ? formData.value.rate_limit_1d : 0,
    rate_limit_7d: formData.value.rate_limit_7d && formData.value.rate_limit_7d > 0 ? formData.value.rate_limit_7d : 0,
  } : { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }

  submitting.value = true
  try {
    if (showEditModal.value && selectedKey.value) {
      const updates: UpdateApiKeyRequest = {
        name: formData.value.name,
        concurrency_limit: concurrencyLimit,
        group_id: formData.value.group_id,
        group_ids: selectedFormGroupIDs(),
        ip_whitelist: ipWhitelist,
        ip_blacklist: ipBlacklist,
        quota: quota,
        expires_at: expiresAt,
        rate_limit_5h: rateLimitData.rate_limit_5h,
        rate_limit_1d: rateLimitData.rate_limit_1d,
        rate_limit_7d: rateLimitData.rate_limit_7d,
      }
      if (shouldSubmitEditStatus(selectedKey.value, formData.value.status)) {
        updates.status = formData.value.status
      }
      await keysAPI.update(selectedKey.value.id, updates)
      appStore.showSuccess(t('keys.keyUpdatedSuccess'))
    } else {
      const customKey = formData.value.use_custom_key ? formData.value.custom_key : undefined
      const createdKey = await keysAPI.create(
        formData.value.name,
        formData.value.group_id,
        customKey,
        ipWhitelist,
        ipBlacklist,
        quota,
        expiresInDays,
        rateLimitData,
        concurrencyLimit,
        selectedFormGroupIDs()
      )
      // Use only the returned persisted object; keep its real group capability metadata.
      connectionKey.value = { ...createdKey, group: createdKey.group || groups.value.find((group) => group.id === createdKey.group_id) || undefined }
      appStore.showSuccess(t('keys.keyCreatedSuccess'))
      // Only advance tour if active, on submit step, and creation succeeded
      if (onboardingStore.isCurrentStep('[data-tour="key-form-submit"]')) {
        onboardingStore.nextStep(500)
      }
    }
    const created = !showEditModal.value ? connectionKey.value : null
    closeModals()
    if (created) openConnection(created)
    loadApiKeys()
  } catch (error: any) {
    const errorMsg = error.response?.data?.detail || t('keys.failedToSave')
    appStore.showError(errorMsg)
    // Don't advance tour on error
  } finally {
    submitting.value = false
  }
}

/**
 * 处理删除 API Key 的操作
 * 优化：错误处理改进，优先显示后端返回的具体错误消息（如权限不足等），
 * 若后端未返回消息则显示默认的国际化文本
 */
function handleDelete() {
  return handleDeleteAction.run(async (isCurrent) => {
    const targetId = selectedKey.value?.id
    if (targetId == null) return

    if (!selectedKey.value) return

    try {
      await keysAPI.delete(targetId)
      if (!isCurrent()) return
      appStore.showSuccess(t('keys.keyDeletedSuccess'))
      showDeleteDialog.value = false
      loadApiKeys()
    } catch (error: any) {
      if (!isCurrent()) return
      // 优先使用后端返回的错误消息，提供更具体的错误信息给用户
      const errorMsg = error?.message || t('keys.failedToDelete')
      appStore.showError(errorMsg)
    }
  })
}

const closeModals = () => {
  showCreateModal.value = false
  showEditModal.value = false
  selectedKey.value = null
  formData.value = {
    name: '',
    concurrency_limit: 0,
    group_id: null,
    group_ids: [],
    status: 'active',
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: false,
    ip_whitelist: '',
    ip_blacklist: '',
    enable_quota: false,
    quota: null,
    enable_rate_limit: false,
    rate_limit_5h: null,
    rate_limit_1d: null,
    rate_limit_7d: null,
    enable_expiration: false,
    expiration_preset: '30',
    expiration_date: ''
  }
}

// Show reset quota confirmation dialog
const confirmResetQuota = () => {
  showResetQuotaDialog.value = true
}

// Set expiration date based on quick select days
const setExpirationDays = (days: number) => {
  formData.value.expiration_preset = days.toString() as '7' | '30' | '90'
  const expDate = new Date()
  expDate.setDate(expDate.getDate() + days)
  formData.value.expiration_date = formatDateTimeLocal(expDate.toISOString())
}

// Reset quota used for an API key
function resetQuotaUsed() {
  return resetQuotaUsedAction.run(async (isCurrent) => {
    const key = selectedKey.value
    if (!key) return
    try {
      const updatedKey = await keysAPI.update(key.id, { reset_quota: true })
      if (!isCurrent()) return
      showResetQuotaDialog.value = false
      appStore.showSuccess(t('keys.quotaResetSuccess'))
      key.quota_used = updatedKey.quota_used
      if (key.status !== updatedKey.status) {
        key.status = updatedKey.status
        if (selectedKey.value?.id === key.id) {
          formData.value.status = updatedKey.status === 'active' ? 'active' : 'inactive'
        }
      }
    } catch (error: any) {
      if (!isCurrent()) return
      const errorMsg = error.response?.data?.detail || t('keys.failedToResetQuota')
      appStore.showError(errorMsg)
    }
  })
}

// Show reset rate limit confirmation dialog (from edit modal)
const confirmResetRateLimit = () => {
  showResetRateLimitDialog.value = true
}

// Show reset rate limit confirmation dialog (from table row)
const confirmResetRateLimitFromTable = (row: ApiKey) => {
  selectedKey.value = row
  showResetRateLimitDialog.value = true
}

// Reset rate limit usage for an API key
function resetRateLimitUsage() {
  return resetRateLimitUsageAction.run(async (isCurrent) => {
    const targetId = selectedKey.value?.id
    if (targetId == null) return

    if (!selectedKey.value) return
    try {
      await keysAPI.update(targetId, { reset_rate_limit_usage: true })
      if (!isCurrent()) return
      appStore.showSuccess(t('keys.rateLimitResetSuccess'))
      // Refresh key data
      await loadApiKeys()
      if (!isCurrent()) return
      // Update the editing key with fresh data
      const refreshedKey = apiKeys.value.find(k => k.id === targetId)
      if (refreshedKey) {
        selectedKey.value = refreshedKey
      }
      showResetRateLimitDialog.value = false
    } catch (error: any) {
      if (!isCurrent()) return
      const errorMsg = error.response?.data?.detail || t('keys.failedToResetRateLimit')
      appStore.showError(errorMsg)
    }
  })
}

const importToCcswitch = (row: ApiKey) => {
  const profiles = getCcSwitchImportProfiles(row.group?.platform, publicSettings.value?.ccs_import_config)
  if (profiles.length > 1) {
    pendingCcsRow.value = row
    showCcsClientSelect.value = true
    return
  }
  if (profiles[0]) executeCcsImport(row, profiles[0])
}

const executeCcsImport = (row: ApiKey, profile: CcsImportProfile) => {
  const baseUrl = publicSettings.value?.api_base_url || window.location.origin
  const platform = row.group?.platform || 'anthropic'
  const providerName = (publicSettings.value?.site_name || 'sub2api').trim() || 'sub2api'
  const deeplink = buildCcSwitchImportDeeplink({
    baseUrl,
    platform,
    profile,
    settings: publicSettings.value?.ccs_import_config,
    providerName,
    apiKey: row.key,
    keyName: row.name,
    userId: row.user_id
  })

  try {
    window.open(deeplink, '_self')

    // Check if the protocol handler worked by detecting if we're still focused
    setTimeout(() => {
      if (document.hasFocus()) {
        // Still focused means the protocol handler likely failed
        appStore.showError(t('keys.ccSwitchNotInstalled'))
      }
    }, 100)
  } catch (error) {
    appStore.showError(t('keys.ccSwitchNotInstalled'))
  }
}

const handleCcsClientSelect = (profile: CcsImportProfile) => {
  if (pendingCcsRow.value) {
    executeCcsImport(pendingCcsRow.value, profile)
  }
  showCcsClientSelect.value = false
  pendingCcsRow.value = null
  if (connectionKey.value) showConnectionDialog.value = true
}

const closeCcsClientSelect = () => {
  showCcsClientSelect.value = false
  pendingCcsRow.value = null
  if (connectionKey.value) showConnectionDialog.value = true
}

function formatResetTime(resetAt: string | null): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now.value.getTime()
  if (diff <= 0) return t('keys.resetNow')
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  const mins = Math.floor((diff % 3600000) / 60000)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

onMounted(() => {
  void applyConnectionRoute()
  loadApiKeys()
  loadGroupPricing()
  loadPublicSettings()
  document.addEventListener('click', closeGroupSelector)
  document.addEventListener('visibilitychange', handleConcurrencyVisibility)
  resetTimer = setInterval(() => { now.value = new Date() }, 60000)
})

onUnmounted(() => {
  disposed = true
  ++connectionRouteRequest
  connectionList.cancel()
  stopConcurrencyRefresh()
  document.removeEventListener('visibilitychange', handleConcurrencyVisibility)
  document.removeEventListener('click', closeGroupSelector)
  if (resetTimer) clearInterval(resetTimer)
})

// Direct confirmations share one execution and stale-context contract.
const handleDeleteAction = useConfirmedAction(() => ({ active: showDeleteDialog.value, identity: selectedKey.value?.id }))
watch(() => ({ active: showDeleteDialog.value, identity: JSON.stringify(selectedKey.value?.id) }), (next, previous) => { if (previous.active && next.active && next.identity !== previous.identity) { showDeleteDialog.value = false } }, { flush: 'sync' })
const resetQuotaUsedAction = useConfirmedAction(() => ({ active: showResetQuotaDialog.value, identity: selectedKey.value?.id }))
watch(() => ({ active: showResetQuotaDialog.value, identity: JSON.stringify(selectedKey.value?.id) }), (next, previous) => { if (previous.active && next.active && next.identity !== previous.identity) { showResetQuotaDialog.value = false } }, { flush: 'sync' })
const resetRateLimitUsageAction = useConfirmedAction(() => ({ active: showResetRateLimitDialog.value, identity: selectedKey.value?.id }))
watch(() => ({ active: showResetRateLimitDialog.value, identity: JSON.stringify(selectedKey.value?.id) }), (next, previous) => { if (previous.active && next.active && next.identity !== previous.identity) { showResetRateLimitDialog.value = false } }, { flush: 'sync' })
</script>

<style scoped>
.keys-page-heading { display: flex; align-items: center; justify-content: space-between; gap: 24px; }
.keys-page-heading h1 { margin: 0; }
.keys-primary-actions { display: flex; align-items: center; gap: 12px; }
.keys-table-tools { display: flex; align-items: center; gap: 12px; margin-left: auto; }
.keys-table-tools :deep(.btn){border-color:transparent;background:transparent;box-shadow:none}
@media (max-width: 767px) {
  .keys-page-heading { flex-direction: column; align-items: stretch; gap: 20px; }
  .keys-primary-actions { flex-wrap: wrap; }
  .keys-primary-actions .btn { flex: 1; }
  .keys-row-actions { flex-wrap: wrap; }
  .keys-endpoints { min-width: 0; max-width: 100%; }
  .keys-endpoints :deep(> div) { min-width: 0; max-width: 100%; flex-wrap: wrap; }
  .keys-endpoints :deep(code) { overflow-wrap: anywhere; }
}

:deep([data-field='current_concurrency']) {
  flex-wrap: wrap;
}

:deep([data-field='current_concurrency'] > div) {
  flex-shrink: 0;
  margin-left: auto;
}
.keys-key-copy{display:inline-flex;align-items:center;gap:6px;min-height:44px;max-width:100%;background:none;border:0;padding:0;color:var(--ritel-muted);font:12px var(--ritel-ui);cursor:pointer}
.keys-key-copy>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.keys-group-button{display:flex;align-items:center;gap:6px;min-height:44px;max-width:100%;border:0;background:none;padding:0;font:inherit;text-align:left;cursor:pointer}
.keys-group-button>span:first-child{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.keys-group-button>span:not(:first-child){flex:none;white-space:nowrap}.keys-group-button>svg{flex:none}
.keys-quota-used{font-weight:500}.keys-quota-total{color:var(--ritel-muted)}
.keys-concurrency-main{display:flex;align-items:center;gap:10px;max-width:100%;white-space:nowrap}.keys-waiting{display:inline-flex;align-items:center;gap:4px;color:var(--ritel-muted)}.keys-concurrency-notice{font-size:12px;overflow:hidden;text-overflow:ellipsis;color:var(--ritel-muted)}
.keys-row-actions{display:flex;align-items:center;gap:16px}.keys-row-actions>button{min-height:44px;border:0;background:none;padding:0;font:inherit;cursor:pointer}.keys-row-actions>button:last-child{color:var(--ritel-muted)}
.keys-status-switch{display:flex;align-items:center;justify-content:center;width:44px;height:44px;border:0;background:none;padding:0;cursor:pointer}.keys-switch-track{display:flex;align-items:center;width:32px;height:18px;border:1px solid var(--ritel-control-border);border-radius:999px;background:var(--ritel-surface-raised);padding:2px;box-sizing:border-box}.keys-switch-track>span{width:12px;height:12px;border-radius:50%;background:var(--ritel-muted);transition:transform .15s ease-out}.keys-status-switch[aria-checked=true] .keys-switch-track{background:#047857;border-color:#047857}.keys-status-switch[aria-checked=true] .keys-switch-track>span{transform:translateX(12px);background:#fffefa}.keys-blocked-status{min-height:44px;border:0;background:none;padding:0;font:inherit;font-size:12px;text-align:left;cursor:pointer}
.keys-extra-details{display:grid;grid-template-columns:1fr 1fr 1fr;gap:32px;padding-top:24px}.keys-extra-details h3{color:var(--ritel-muted);margin-bottom:10px}.keys-extra-groups{font-size:12px;color:var(--ritel-muted);margin-top:8px}.keys-detail-actions{display:flex;gap:12px;margin-top:24px}
.keys-key-copy:disabled,.keys-group-button:disabled,.keys-row-actions>button:disabled,.keys-status-switch:disabled{opacity:.5;cursor:wait}
@media(max-width:767px){.keys-extra-details{grid-template-columns:1fr;gap:24px}.keys-group-button{white-space:normal}.keys-row-actions{gap:24px}}
@media(prefers-reduced-motion:reduce){.keys-switch-track>span{transition:none}}
</style>

<style>
.dark .keys-status-switch[aria-checked=true] .keys-switch-track{background:#6ee7b7;border-color:#6ee7b7}
.dark .keys-status-switch[aria-checked=true] .keys-switch-track>span{background:#1b1b19}
</style>
