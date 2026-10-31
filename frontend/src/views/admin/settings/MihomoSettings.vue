<template>
  <div class="space-y-5" :aria-busy="busy">
    <div class="open-section flex flex-wrap items-center justify-between gap-3">
      <div class="space-y-1">
        <p class="text-sm font-medium" role="status">Mihomo <span class="badge ml-2" :class="status?.running ? 'badge-success' : 'badge-gray'">{{ status?.phase || '—' }}</span></p>
        <p class="text-xs text-gray-500">{{ t('consoleInline.localEndpoint_23c834') }} <code>{{ status?.endpoint || '—' }}</code> · {{ status?.nodes || 0 }} {{ t('consoleInline.nodes_331f00') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="refresh">{{ t('consoleInline.checkStatus_cf8489') }}</button>
    </div>
    <div v-if="warmPools.length" class="grid gap-3 sm:grid-cols-2">
      <div v-for="pool in warmPools" :key="pool.source" class="open-section text-xs" data-testid="bps-warm-pool">
        <p class="font-medium">{{ pool.source }} · Warm IP pool</p>
        <p class="mt-2">{{ t('consoleInline.ready_8c5afc') }} {{ pool.ready }} / {{ t('consoleInline.target_246453') }} {{ pool.target }} · {{ t('consoleInline.checking_460181') }} {{ pool.checking }} · {{ t('consoleInline.cooling_34082c') }} {{ pool.cooling }}</p>
        <p v-if="pool.source === 'Mihomo' && pool.ready_subscription !== undefined" class="mt-1">{{ t('consoleInline.readySubscriptionExits_d048db') }} {{ pool.ready_subscription }} · {{ t('consoleInline.readyDynamicExits_be1395') }} {{ pool.ready_dynamic || 0 }}</p>
        <p class="mt-1 text-gray-500">{{ t('consoleInline.targetsFollowEnabledAccountConcurrencyReadyExits_606b56') }}</p>
        <p v-for="(count, reason) in pool.failure_reasons" :key="reason">{{ reason }}: {{ count }}</p>
        <p v-if="pool.target > 0 && pool.ready === 0" class="mt-1 text-amber-700">{{ t('consoleInline.backgroundWarmingContinuesDirectFallbackIsDisabled_c82045') }}</p>
      </div>
    </div>
    <p v-if="error || status?.error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20">{{ error || status?.error }}</p>
    <p v-if="success" role="status" class="text-sm text-green-700 dark:text-green-400">{{ success }}</p>
    <p :id="'field-' + fieldLabelScope + '-1'" v-if="busy" role="status" class="text-sm text-primary-600">{{ t('consoleInline.applyingChangesPleaseWait_b8bc1c') }}</p>

    <template v-if="section === 'subscriptions'">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div><AppHeading variant="section" as="h2">{{ t('consoleInline.mihomoSubscriptions_0e9ffb') }}</AppHeading><p class="mt-1 text-xs text-gray-500">{{ t('consoleInline.manageEachSourceIndependentlyURLsAndTokens_ee0f1d') }}</p></div>
        <button type="button" class="btn btn-primary" :disabled="busy || !status?.installed" @click="openSubscription()">{{ t('consoleInline.addSubscription_43aed6') }}</button>
      </div>
      <div class="open-section">
        <div class="flex flex-wrap items-center gap-3">
          <label for="subscription-download-mode" class="text-sm font-medium">{{ t('consoleInline.subscriptionDownloadMode_426f1d') }}</label>
          <select id="subscription-download-mode" v-model="downloadMode" class="input w-auto" :disabled="busy" @change="downloadModeDirty = true">
            <option value="auto">{{ t('consoleInline.autoProxyWithDirectFallback_513659') }}</option>
            <option value="proxy">{{ t('consoleInline.mihomoProxyOnly_269c14') }}</option>
            <option value="direct">{{ t('consoleInline.directOnly_465534') }}</option>
          </select>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !downloadModeDirty" @click="saveDownloadMode">{{ t('consoleInline.saveDownloadMode_ec07d0') }}</button>
        </div>
        <p class="mt-2 text-xs text-gray-500">{{ t('consoleInline.onlyAffectsSubscriptionDownloadsNotTicketOr_3c8804') }}</p>
      </div>
      <div class="open-filter-toolbar workspace-filter-controls flex flex-wrap items-center gap-3">
        <div ref="tableTools0" class="min-w-0" />
        <input :aria-labelledby="'field-' + fieldLabelScope + '-1'" v-model="search" type="search" class="input sm:max-w-xs" :placeholder="t('consoleInline.searchSubscriptionNames_dfbad7')" :aria-label="t('consoleInline.searchSubscriptions_f52920')" />
        <div v-if="selected.length" class="flex flex-wrap items-center gap-2 text-sm">
          <span>{{ t('consoleInline.selected_4b9216') }} {{ selected.length }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="batchSubscriptions('subscription_refresh')">{{ t('consoleInline.refreshSelected_f1bdd4') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="batchSubscriptions('subscription_enable')">{{ t('consoleInline.enableSelected_29a62d') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="batchSubscriptions('subscription_disable')">{{ t('consoleInline.disableSelected_7ae3da') }}</button>
          <button type="button" class="btn btn-danger btn-sm" :disabled="busy" @click="confirmRemoval(selected.map(id => 'subscription_remove/' + id))">{{ t('consoleInline.removeSelected_de2da4') }}</button>
        </div>
      </div>
      <DataTable :toolbar-target="tableTools0" column-order-key="table-columns:mihomo-subscriptions:v1" v-model:selected-keys="selected" :columns="subscriptionColumns" :data="filteredSubscriptions" row-key="id" selectable :loading="loading">
        <template #cell-label="{ row }"><span class="font-medium">{{ row.label }}</span><code class="ml-2 text-xs text-gray-400">{{ row.id.slice(0, 8) }}</code></template>
        <template #cell-enabled="{ row }"><span class="badge" :class="row.enabled ? 'badge-success' : 'badge-gray'">{{ row.enabled ? t('consoleInline.enabled_87a030') : t('consoleInline.disabled_c38905') }}</span></template>
        <template #cell-nodes="{ row }">{{ row.cached ? row.nodes : t('consoleInline.notRefreshed_0e9bfa') }}</template>
        <template #cell-updated_at="{ row }">{{ row.updated_at ? formatDisplayTimestamp(row.updated_at) : '—' }}</template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap gap-2">
            <button :title="t('consoleInline.refresh_4adf14')" :aria-label="t('consoleInline.refresh_4adf14')" type="button" class="workspace-icon-action theme-action-button" :disabled="busy || !row.enabled" @click="operate('subscription_refresh/' + row.id)"><Icon name="refresh" size="sm" /><span class="sr-only">{{ t('consoleInline.refresh_4adf14') }}</span></button>
            <button :title="t('consoleInline.edit_f7f5fa')" :aria-label="t('consoleInline.edit_f7f5fa')" type="button" class="workspace-icon-action theme-action-button" :disabled="busy" @click="openSubscription(row)"><Icon name="edit" size="sm" /><span class="sr-only">{{ t('consoleInline.edit_f7f5fa') }}</span></button>
            <button :title="row.enabled ? t('consoleInline.disable_4c7f46') : t('consoleInline.enable_836b3c')" :aria-label="row.enabled ? t('consoleInline.disable_4c7f46') : t('consoleInline.enable_836b3c')" type="button" class="workspace-icon-action theme-action-button" :disabled="busy" @click="operate((row.enabled ? 'subscription_disable/' : 'subscription_enable/') + row.id)"><Icon :name="row.enabled ? 'ban' : 'checkCircle'" size="sm" /><span class="sr-only">{{ row.enabled ? t('consoleInline.disable_4c7f46') : t('consoleInline.enable_836b3c') }}</span></button>
            <button :title="t('consoleInline.remove_ca30f1')" :aria-label="t('consoleInline.remove_ca30f1')" type="button" class="workspace-icon-action text-red-600 dark:text-red-400" :disabled="busy" @click="confirmRemoval(['subscription_remove/' + row.id])"><Icon name="trash" size="sm" /><span class="sr-only">{{ t('consoleInline.remove_ca30f1') }}</span></button>
          </div>
        </template>
        <template #empty><p class="p-8 text-center text-sm text-gray-500">{{ t('consoleInline.noMatchingSubscriptionsAddAClashMihomo_f4de84') }}</p></template>
      </DataTable>
      <p v-if="status?.subscriptions && !status.subscription_items" class="text-sm text-amber-700">{{ t('consoleInline.thisBackendDoesNotSupportSubscriptionLists_22fa30') }}</p>
    </template>

    <template v-if="section === 'dynamic' || section === 'nodes'">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div><AppHeading variant="section" as="h2">{{ section === 'dynamic' ? t('consoleInline.dynamicProxies_29dc5d') : t('consoleInline.nodeManagement_cf5881') }}</AppHeading><p class="mt-1 text-xs text-gray-500">{{ t('consoleInline.testConnectionAndQualityCheckRouteThrough_b28b88') }}</p></div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="!status?.installed || batchTesting || !nodeBatchTargets.length" @click="batchTestNodes">{{ t('admin.proxies.testConnection') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="!status?.installed || batchQualityChecking || !nodeBatchTargets.length" @click="batchCheckNodeQuality">{{ t('admin.proxies.batchQualityCheck') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="busy || !selectedNodeTargets.length" @click="nodeEditDialog = true">{{ t('consoleInline.batchEdit_46fce9') }}</button>
          <button type="button" class="btn btn-primary" :disabled="busy || !status?.running || !selectedNodeTargets.length" @click="openStaticProxyDialog(selectedNodeTargets)">{{ t('consoleInline.addSelectedAsStaticProxies_98e87f') }}</button>
          <button v-if="section === 'dynamic'" type="button" class="btn btn-primary" :disabled="busy || !status?.installed" @click="dynamicDialog = true">{{ t('consoleInline.importDynamicProxies_b1a061') }}</button>
        </div>
      </div>
      <div class="open-filter-toolbar workspace-filter-controls flex flex-wrap items-center gap-3">
        <div ref="tableTools1" class="min-w-0" />
        <input :aria-labelledby="'field-' + fieldLabelScope + '-1'" v-model="search" type="search" class="input sm:max-w-xs" :placeholder="t('consoleInline.searchNodeNamesOrRegions_942fed')" :aria-label="t('consoleInline.searchNodes_bddd47')" />
        <select :aria-labelledby="'field-' + fieldLabelScope + '-1'" v-model="nodeState" class="input w-auto" :aria-label="t('consoleInline.nodeStatus_b17731')">
          <option value="">{{ t('consoleInline.allStatuses_f426a4') }}</option><option v-for="state in nodeStates" :key="state" :value="state">{{ stateLabel(state) }}</option>
        </select>
        <select :aria-labelledby="'field-' + fieldLabelScope + '-1'" v-if="section === 'nodes'" v-model="nodeSource" class="input w-auto" :aria-label="t('consoleInline.nodeSource_93671f')">
          <option value="">{{ t('consoleInline.allSources_b3a114') }}</option><option value="dynamic">{{ t('consoleInline.dynamicProxies_29dc5d') }}</option>
          <option v-for="source in status?.subscription_items || []" :key="source.id" :value="source.id">{{ source.label }}</option>
        </select>
        <span class="text-xs text-gray-500" data-testid="node-batch-scope">{{ selectedNodes.length ? t('consoleFields.selectedNodeBatch', { count: nodeBatchTargets.length }) : t('consoleFields.filteredNodeBatch', { count: nodeBatchTargets.length }) }}</span>
      </div>
      <DataTable :toolbar-target="tableTools1" column-order-key="table-columns:mihomo-nodes:v1" v-model:selected-keys="selectedNodes" :columns="nodeColumns" :data="visibleNodes" row-key="name" selectable :loading="loading">
        <template #cell-display_name="{ row }"><span>{{ row.display_name || row.name }}</span><span v-if="row.static_proxy" class="badge badge-primary ml-2">{{ t('consoleInline.staticProxyAdded_63d0ca') }}</span></template>
        <template #cell-source="{ row }">{{ nodeSourceLabel(row) }}</template>
        <template #cell-state="{ row }"><span class="badge" :class="row.state === 'enabled' ? 'badge-success' : 'badge-gray'">{{ stateLabel(row.state) }}</span></template>
        <template #cell-country_code="{ row }">{{ row.dynamic ? t('consoleInline.providerManaged_77330d') : row.country_code || t('consoleInline.unknown_556aa9') }}</template>
        <template #cell-check="{ row }">
          <div class="flex flex-col gap-1" data-testid="node-check">
            <span v-if="row.check?.latency_status === 'failed'" class="badge badge-danger" :title="row.check.latency_message || undefined">{{ t('admin.proxies.latencyFailed') }}</span>
            <span v-else-if="typeof row.check?.latency_ms === 'number'" :class="['badge', row.check.latency_ms < 200 ? 'badge-success' : 'badge-warning']">{{ row.check.latency_ms }}ms</span>
            <span v-else class="text-sm text-gray-400">-</span>
            <div v-if="typeof row.check?.quality_checked === 'number'" class="flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400" :title="row.check.quality_summary || undefined">
              <span>{{ t('admin.proxies.qualityInline', { grade: row.check.quality_grade || '-', score: row.check.quality_score ?? '-' }) }}</span>
              <span class="badge" :class="proxyQualityOverallClass(row.check.quality_status)">{{ t(proxyQualityOverallLabelKey(row.check.quality_status)) }}</span>
            </div>
            <span v-if="row.check && checkExit(row.check)" class="text-xs text-gray-500 dark:text-gray-400" :title="checkTitle(row)">{{ checkExit(row.check) }}</span>
          </div>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap gap-2">
            <button :title="t('consoleInline.addAsStaticProxy_c19047')" :aria-label="t('consoleInline.addAsStaticProxy_c19047')" type="button" class="workspace-icon-action theme-action-button" :disabled="busy || !status?.running || row.state !== 'enabled'" @click="openStaticProxyDialog([row])"><Icon name="plus" size="sm" /><span class="sr-only">{{ t('consoleInline.addAsStaticProxy_c19047') }}</span></button>
            <button :title="testingNodes.has(row.name) ? t('admin.proxies.testing') : t('admin.proxies.testConnection')" :aria-label="testingNodes.has(row.name) ? t('admin.proxies.testing') : t('admin.proxies.testConnection')" type="button" class="workspace-icon-action theme-action-button" :disabled="!status?.installed || testingNodes.has(row.name)" :aria-busy="testingNodes.has(row.name)" @click="testNode(row)"><Icon name="checkCircle" size="sm" /><span class="sr-only">{{ testingNodes.has(row.name) ? t('admin.proxies.testing') : t('admin.proxies.testConnection') }}</span></button>
            <button :title="qualityNodes.has(row.name) ? t('admin.proxies.testing') : t('admin.proxies.qualityCheck')" :aria-label="qualityNodes.has(row.name) ? t('admin.proxies.testing') : t('admin.proxies.qualityCheck')" type="button" class="workspace-icon-action theme-action-button" :disabled="!status?.installed || qualityNodes.has(row.name)" :aria-busy="qualityNodes.has(row.name)" @click="checkNodeQuality(row)"><Icon name="shield" size="sm" /><span class="sr-only">{{ qualityNodes.has(row.name) ? t('admin.proxies.testing') : t('admin.proxies.qualityCheck') }}</span></button>
            <button :aria-label="t('consoleInline.probe_a35760')" type="button" class="workspace-icon-action theme-action-button" :disabled="busy || !status?.running" :title="t('consoleInline.measuresThroughTheRunningKernelFailureMarks_0d6637')" @click="operate('probe/' + row.name)"><Icon name="bolt" size="sm" /><span class="sr-only">{{ t('consoleInline.probe_a35760') }}</span></button>
            <button :title="t('consoleInline.checkRegion_f117c1')" :aria-label="t('consoleInline.checkRegion_f117c1')" v-if="!row.dynamic" type="button" class="workspace-icon-action theme-action-button" :disabled="busy || !status?.running" @click="operate('country_probe/' + row.name)"><Icon name="globe" size="sm" /><span class="sr-only">{{ t('consoleInline.checkRegion_f117c1') }}</span></button>
            <button :title="row.state === 'enabled' ? t('consoleInline.disable_4c7f46') : t('consoleInline.recover_707d81')" :aria-label="row.state === 'enabled' ? t('consoleInline.disable_4c7f46') : t('consoleInline.recover_707d81')" type="button" class="workspace-icon-action theme-action-button" :disabled="busy || row.state === 'country_excluded'" @click="operate((row.state === 'enabled' ? 'disable/' : 'recover/') + row.name)"><Icon :name="row.state === 'enabled' ? 'ban' : 'refresh'" size="sm" /><span class="sr-only">{{ row.state === 'enabled' ? t('consoleInline.disable_4c7f46') : t('consoleInline.recover_707d81') }}</span></button>
            <button :title="t('consoleInline.remove_ca30f1')" :aria-label="t('consoleInline.remove_ca30f1')" v-if="row.dynamic" type="button" class="workspace-icon-action text-red-600 dark:text-red-400" :disabled="busy" @click="confirmRemoval(['dynamic_remove/' + row.name])"><Icon name="trash" size="sm" /><span class="sr-only">{{ t('consoleInline.remove_ca30f1') }}</span></button>
          </div>
        </template>
      </DataTable>
      <p class="text-sm">{{ t('consoleInline.total_ca7a6a') }} {{ filteredNodes.length }} {{ t('consoleInline.nodes_331f00') }}</p>
      <InfiniteListSentinel :loading="loading" :has-more="visibleNodes.length < filteredNodes.length" :progress="visibleNodes.length" @load-more="visibleNodeCount += 50" />
    </template>

    <template v-if="section === 'kernel'">
      <AppHeading variant="section" as="h2">{{ t('consoleInline.kernelAndRegionRules_aa674b') }}</AppHeading>
      <div class="flex flex-wrap gap-2">
        <button v-if="status && !status.installed" type="button" class="btn btn-primary" :disabled="busy || !status.supported" @click="operate('install')">{{ t('consoleInline.installKernel_58fab8') }}</button>
        <button v-else-if="!status?.running" type="button" class="btn btn-secondary" :disabled="busy || !status?.nodes" @click="operate('start')">{{ t('consoleInline.startKernel_5d04e3') }}</button>
      </div>
      <label v-if="status?.installed" class="flex items-center gap-2 text-sm"><input type="checkbox" :checked="status.use_once" :disabled="busy" @change="operate(status.use_once ? 'once_off' : 'once_on')" />{{ t('consoleInline.retireHarvestNodesAfterUseManualRecovery_15251a') }}</label>
      <MihomoCountryFilter v-if="status?.installed" :filter="status.country_filter" :codes="status.country_codes || []" :nodes="status.node_states || []" :busy="busy" @save="operate('country_filter', { country_filter: $event })" @scan="operate('country_scan')" />
      <p class="text-xs text-gray-500">{{ t('consoleInline.configurationIsSavedHereIndependentlySelectThe_5d60b0') }}</p>
    </template>
    <p v-if="status && !status.installed && section !== 'kernel'" class="text-sm text-amber-700">{{ t('consoleInline.installTheKernelInKernelAndRules_6831bb') }}</p>

    <BaseDialog :show="staticProxyDialog" :title="t('consoleInline.addAsStaticProxies_2bbcb7')" @close="!pending && (staticProxyDialog = false)">
      <div class="space-y-3 text-sm">
        <p>{{ t('consoleFields.staticProxyConfirm', { count: staticProxyTargets.length }) }}</p>
        <p class="text-gray-500">{{ t('consoleInline.eachProxyUsesOnlyItsSelectedNode_ad5a4a') }}</p>
        <p v-if="staticProxyTargets.some(node => node.dynamic)" class="text-amber-700">{{ t('consoleInline.includesDynamicProxiesTheProxyConfigurationIs_973263') }}</p>
        <ul class="max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><li v-for="node in staticProxyTargets" :key="node.name">{{ node.display_name || node.name }}</li></ul>
        <p v-if="staticProxyTargets.some(node => node.state !== 'enabled')" role="alert" class="text-amber-700">{{ t('consoleInline.restoreSelectedNodesAndAllowTheirRegions_c3c694') }}</p>
        <p v-if="staticProxyError" role="alert" class="text-red-600">{{ staticProxyError }}</p>
      </div>
      <template #footer><button type="button" class="btn btn-primary" :disabled="busy || !staticProxyTargets.length || staticProxyTargets.some(node => node.state !== 'enabled')" @click="addStaticProxies">{{ pending ? t('consoleInline.adding_a7ba3c') : t('consoleInline.confirmAdd_4d2e1b') }}</button></template>
    </BaseDialog>
    <BaseDialog :show="nodeEditDialog" :title="t('consoleInline.batchEditNodes_b3df83')" @close="!pending && (nodeEditDialog = false)">
      <div class="space-y-3 text-sm">
        <p>{{ t('consoleFields.selectedNodeEdit', { count: selectedNodeTargets.length }) }}</p>
        <label class="block">{{ t('consoleInline.action_aca1a5') }}<select v-model="nodeEditAction" class="input mt-2" data-testid="node-batch-action"><option value="recover">{{ t('consoleInline.recoverNodes_56bf4b') }}</option><option value="disable">{{ t('consoleInline.disableNodes_fa9ec2') }}</option><option value="static">{{ t('consoleInline.addAsStaticProxies_2bbcb7') }}</option></select></label>
        <p class="text-gray-500">{{ t('consoleInline.recoveryStillFollowsRegionRulesDisablingA_a7cd0e') }}</p>
        <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      </div>
      <template #footer><button type="button" class="btn btn-primary" :disabled="busy || !selectedNodeTargets.length" @click="applyNodeEdit">{{ t('consoleInline.applyToSelected_0619ee') }}</button></template>
    </BaseDialog>
    <BaseDialog :show="subscriptionDialog" :title="editing ? t('consoleInline.editSubscription_026057') : t('consoleInline.addSubscription_43aed6')" @close="subscriptionDialog = false">
      <div class="space-y-4">
        <label class="block text-sm">{{ t('consoleInline.subscriptionNameSingleSourceOptional_12ef7e') }}<input v-model="subscriptionName" maxlength="80" class="input mt-2" data-testid="subscription-name" /></label>
        <label :id="'field-' + fieldLabelScope + '-2'" class="block text-sm" for="mihomo-subscriptions">{{ editing ? t('consoleInline.newURLLeaveBlankToRenameOnly_a5309e') : t('consoleInline.subscriptionURLsOnePerLine_caad76') }}</label>
        <textarea id="mihomo-subscriptions" v-model="subscriptions" class="input w-full font-mono text-xs" rows="5" autocomplete="off" spellcheck="false" />
        <p class="text-xs text-gray-500">{{ t('consoleInline.requiresClashMihomoYAMLKeepSecretsOut_32b3fa') }}</p>
        <input :aria-labelledby="'field-' + fieldLabelScope + '-2'" v-if="!editing" type="file" accept=".txt,text/plain" :aria-label="t('consoleInline.importSubscriptionTXT_cf0243')" @change="importFile" />
        <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      </div>
      <template #footer><button type="button" class="btn btn-primary" :disabled="busy || (!subscriptions.trim() && !editing)" @click="saveSubscription">{{ t('consoleInline.saveAndApply_d2151e') }}</button></template>
    </BaseDialog>
    <BaseDialog :show="dynamicDialog" :title="t('consoleInline.importDynamicProxies_b1a061')" @close="dynamicDialog = false">
      <div class="space-y-3">
        <label class="block text-sm" for="mihomo-dynamic-protocol">{{ t('consoleInline.defaultProtocol_73bd0a') }}</label>
        <select id="mihomo-dynamic-protocol" v-model="dynamicProtocol" class="input"><option value="http">HTTP</option><option value="https">HTTPS</option><option value="socks5">SOCKS5</option><option value="socks5h">SOCKS5H</option></select>
        <label class="block text-sm" for="mihomo-dynamic-proxies">{{ t('consoleInline.dynamicProxiesOnePerLine_239c7e') }}</label>
        <textarea id="mihomo-dynamic-proxies" v-model="dynamicProxies" class="input w-full font-mono text-xs" rows="6" autocomplete="off" spellcheck="false" />
        <details class="text-xs text-gray-500"><summary>{{ t('consoleInline.supportedFormats_59eeae') }}</summary><pre class="mt-2 whitespace-pre-wrap">hostname:port:username:password
username:password:hostname:port
username:password@hostname:port
hostname:port@username:password
http://username:password@hostname:port</pre></details>
        <label class="flex items-center gap-2 text-sm"><input v-model="replaceDynamic" type="checkbox" />{{ t('consoleInline.replaceSavedDynamicProxiesOtherwiseAppend_fbe6bb') }}</label>
        <p class="text-xs text-gray-500">{{ t('consoleInline.subscriptionsArePreservedExitRegionsRemainControlled_a29d04') }}</p>
        <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      </div>
      <template #footer><button type="button" class="btn btn-primary" :disabled="busy || !dynamicProxies.trim()" @click="saveDynamic">{{ t('consoleInline.applyDynamicProxies_2b94b8') }}</button></template>
    </BaseDialog>
    <ConfirmDialog :busy="confirmActionsAction.busy.value || busy" :show="!!confirmation" :title="t('consoleInline.confirmProxySourceChanges_39bbe6')" :message="confirmation?.message || ''" danger @cancel="confirmation = undefined" @confirm="confirmActions" />
    <ProxyQualityReportDialog :show="!!qualityReport" :name="qualityReportName" :report="qualityReport" @close="qualityReport = null" />
  </div>
</template>
<script setup lang="ts">
import { formatTimestamp as formatDisplayTimestamp } from "@/utils/consoleDisplay"
import { useId as useFieldLabelId } from 'vue'
const fieldLabelScope = useFieldLabelId()

import AppHeading from '@/components/common/AppHeading.vue'
import Icon from '@/components/icons/Icon.vue'
import { computed, ref, onMounted, onUnmounted, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { useAppStore } from '@/stores/app'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useConfirmedAction } from '@/composables/useConfirmation'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ProxyQualityReportDialog from '@/components/admin/proxy/ProxyQualityReportDialog.vue'
import MihomoCountryFilter from './MihomoCountryFilter.vue'
import type { CountryFilter, CountryNode } from './mihomoCountry'
import type { ProxyQualityCheckResult } from '@/types'
import { proxyQualityOverallClass, proxyQualityOverallLabelKey, summarizeProxyQuality } from '@/utils/proxyQuality'
const props = withDefaults(defineProps<{ section?: 'subscriptions' | 'dynamic' | 'nodes' | 'kernel' }>(), { section: 'subscriptions' })
const emit = defineEmits<{ 'static-proxies-added': [] }>()
const { t } = useI18n()
const tableTools1 = ref<HTMLElement | null>(null)
const tableTools0 = ref<HTMLElement | null>(null)
const appStore = useAppStore()
type SubscriptionDownloadMode = 'auto' | 'proxy' | 'direct'
interface WarmPoolStatus { ready_subscription?: number; ready_dynamic?: number; target: number; ready: number; checking: number; cooling: number; failure_reasons?: Record<string, number> }
interface Subscription { id: string; label: string; enabled: boolean; nodes: number; cached: boolean; updated_at?: string }
// Latest isolated-kernel check of a node, shaped like the static proxy list's latency fields.
interface NodeCheck { checked_at: number; latency_status?: 'success' | 'failed'; latency_ms?: number; latency_message?: string; ip_address?: string; country?: string; country_code?: string; region?: string; city?: string; quality_status?: string; quality_score?: number; quality_grade?: string; quality_summary?: string; quality_checked?: number }
interface NodeTestResult { success: boolean; message: string; latency_ms?: number; ip_address?: string; country?: string; country_code?: string; region?: string; city?: string }
interface ManagedNode extends CountryNode { subscription_ids?: string[]; check?: NodeCheck; static_proxy?: boolean }
interface Status { subscription_download_mode?: SubscriptionDownloadMode; bps_warm_pool?: WarmPoolStatus; bps_ip_warm_pool?: WarmPoolStatus; installed: boolean; running: boolean; busy: boolean; supported: boolean; phase: string; error?: string; nodes: number; subscriptions: number; subscription_items?: Subscription[]; dynamic_proxies?: number; endpoint: string; use_once?: boolean; node_states?: ManagedNode[]; country_filter?: CountryFilter; country_codes?: string[] }
interface Payload { subscriptions?: string[]; dynamic_proxies?: string[]; country_filter?: CountryFilter; name?: string }
const status = ref<Status>()
const loading = ref(true)
const pending = ref(false)
const busy = computed(() => pending.value || !!status.value?.busy)
const error = ref('')
const success = ref('')
const subscriptions = ref('')
const subscriptionName = ref('')
const subscriptionDialog = ref(false)
const downloadMode = ref<SubscriptionDownloadMode>('auto')
const downloadModeDirty = ref(false)
watch(() => status.value?.subscription_download_mode, mode => { if (!downloadModeDirty.value) downloadMode.value = mode || 'auto' })
const editing = ref<Subscription>()
const dynamicDialog = ref(false)
const dynamicProxies = ref('')
const dynamicProtocol = ref('http')
const replaceDynamic = ref(false)
const selected = ref<Array<string | number>>([])
const selectedNodes = ref<Array<string | number>>([])
const nodeEditDialog = ref(false)
const nodeEditAction = ref<'recover' | 'disable' | 'static'>('recover')
const staticProxyDialog = ref(false)
const staticProxyTargets = ref<ManagedNode[]>([])
const staticProxyError = ref('')
const testingNodes = ref(new Set<string>())
const qualityNodes = ref(new Set<string>())
const batchTesting = ref(false)
const batchQualityChecking = ref(false)
const qualityReport = ref<ProxyQualityCheckResult | null>(null)
const qualityReportName = ref('')
// Each check starts a private kernel and may queue behind other checks on the server.
const nodeTestTimeout = 60000
const nodeQualityTimeout = 150000
const search = ref('')
const nodeState = ref('')
const nodeSource = ref('')
const visibleNodeCount = ref(50)
const nodeStates = ['enabled', 'disabled', 'failed', 'used', 'country_excluded']
const confirmation = ref<{ actions: string[]; message: string; payload?: Payload }>()
const stateLabel = (state: string) => ({ enabled: t('consoleInline.enabled_87a030'), disabled: t('consoleInline.disabled_c38905'), failed: t('consoleInline.failed_9eca61'), used: t('consoleInline.used_f45ed8'), country_excluded: t('consoleInline.regionExcluded_2e7cc1') }[state] || state)
const warmPools = computed(() => [
  { source: 'Mihomo', pool: status.value?.bps_warm_pool },
  { source: t('consoleInline.iPManagement_0e4af9'), pool: status.value?.bps_ip_warm_pool }
].flatMap(item => item.pool ? [{ source: item.source, ...item.pool }] : []))
const subscriptionColumns = computed(() => [
  { key: 'label', label: t('consoleInline.name_5d4e8a'), sortable: true }, { key: 'enabled', label: t('consoleInline.status_4810a8') },
  { key: 'nodes', label: t('consoleInline.nodes_5e8e0c') }, { key: 'updated_at', label: t('consoleInline.lastRefreshed_3f7303') }, { key: 'actions', label: t('consoleInline.actions_41f1e6') }
])
const nodeColumns = computed(() => [
  { key: 'display_name', label: t('consoleInline.name_5d4e8a') }, { key: 'source', label: t('consoleInline.source_8354d1') },
  { key: 'state', label: t('consoleInline.status_4810a8') }, { key: 'country_code', label: t('consoleInline.region_9833d9') },
  { key: 'check', label: t('admin.proxies.columns.latency') }, { key: 'actions', label: t('consoleInline.actions_41f1e6') }
])
const filteredSubscriptions = computed(() => (status.value?.subscription_items || []).filter(s => (s.label + s.id).toLowerCase().includes(search.value.trim().toLowerCase())))
const filteredNodes = computed(() => (status.value?.node_states || []).filter(n =>
  (props.section !== 'dynamic' || n.dynamic) && (!nodeState.value || n.state === nodeState.value) &&
  (!nodeSource.value || (nodeSource.value === 'dynamic' ? n.dynamic : n.subscription_ids?.includes(nodeSource.value))) &&
  ((n.display_name || n.name) + ' ' + (n.country_code || '')).toLowerCase().includes(search.value.trim().toLowerCase())
))
const visibleNodes = computed(() => filteredNodes.value.slice(0, visibleNodeCount.value))
const selectedNodeTargets = computed(() => {
  const selected = new Set(selectedNodes.value)
  return filteredNodes.value.filter(node => selected.has(node.name))
})
// Like the static proxy list: selected nodes, otherwise every node matching the filters.
const nodeBatchTargets = computed(() => {
  if (!selectedNodes.value.length) return filteredNodes.value
  const chosen = new Set(selectedNodes.value)
  return filteredNodes.value.filter(n => chosen.has(n.name))
})
watch([search, nodeState, nodeSource, () => props.section], () => { visibleNodeCount.value = 50; selected.value = []; selectedNodes.value = [] })
watch(() => props.section, () => { search.value = ''; nodeSource.value = ''; nodeState.value = '' })
function nodeSourceLabel(node: ManagedNode) {
  if (node.dynamic) return t('consoleInline.dynamicProxy_11639b')
  return node.subscription_ids?.map(id => status.value?.subscription_items?.find(s => s.id === id)?.label || id.slice(0, 8)).join(', ') || t('consoleInline.subscriptionRefreshToIdentify_d2557a')
}
function openStaticProxyDialog(nodes: ManagedNode[]) {
  staticProxyTargets.value = [...nodes]
  staticProxyError.value = ''
  staticProxyDialog.value = true
}
async function addStaticProxies() {
  if (pending.value || !staticProxyTargets.value.length) return
  pending.value = true
  staticProxyError.value = ''
  let created = 0
  let existing = 0
  const failedNames = new Set<string>()
  const targets = [...staticProxyTargets.value]
  try {
    for (let offset = 0; offset < targets.length; offset += 100) {
      const { data } = await apiClient.post<{ created: unknown[]; existing: unknown[]; failed: Array<{ name: string; reason: string }> }>('/admin/system/mihomo/nodes/static-proxies', { names: targets.slice(offset, offset + 100).map(node => node.name) }, { timeout: 180000 })
      created += data.created.length
      existing += data.existing.length
      for (const failure of data.failed) failedNames.add(failure.name)
      if (disposed) return
    }
    emit('static-proxies-added')
    const summary = t('consoleFields.staticProxySummary', { created, existing, failed: failedNames.size })
    if (failedNames.size) {
      staticProxyTargets.value = targets.filter(node => failedNames.has(node.name))
      staticProxyError.value = summary
      appStore.showWarning(summary)
    } else {
      staticProxyDialog.value = false
      selectedNodes.value = []
      appStore.showSuccess(summary)
    }
  } catch (cause) {
    staticProxyError.value = causeMessage(cause, t('consoleInline.couldNotFinishAddingProxiesRetryTo_b9ad63'))
    emit('static-proxies-added')
  } finally {
    pending.value = false
    if (!disposed) await refresh()
  }
}
async function applyNodeEdit() {
  const targets = [...selectedNodeTargets.value]
  if (nodeEditAction.value === 'static') {
    nodeEditDialog.value = false
    openStaticProxyDialog(targets)
    return
  }
  for (const node of targets) {
    if (!await operate(nodeEditAction.value + '/' + node.name)) return
    selectedNodes.value = selectedNodes.value.filter(name => name !== node.name)
  }
  nodeEditDialog.value = false
}
const checkExit = (check: NodeCheck) => [check.ip_address, check.country, check.city].filter(Boolean).join(' · ')
function checkTitle(node: ManagedNode) {
  const checked = node.check ? t('consoleInline.checked_639fb0') + formatDisplayTimestamp(node.check.checked_at * 1000) : ''
  return node.dynamic ? checked + t('consoleInline.dynamicProxiesMayChangeExitOnEvery_591f6c') : checked
}
const nodeCheckPath = (name: string, check: 'test' | 'quality-check') => `/admin/system/mihomo/nodes/${encodeURIComponent(name)}/${check}`
const causeMessage = (cause: unknown, fallback: string) => (cause as { message?: string })?.message || fallback
function setNodeBusy(nodes: Ref<Set<string>>, name: string, active: boolean) {
  const next = new Set(nodes.value)
  if (active) next.add(name)
  else next.delete(name)
  nodes.value = next
}
// Show a result at once; the next status refresh returns the kernel's record.
function applyNodeCheck(name: string, update: (previous?: NodeCheck) => NodeCheck) {
  const node = status.value?.node_states?.find(n => n.name === name)
  if (node) node.check = update(node.check)
}
function previousQuality(check?: NodeCheck): Partial<NodeCheck> {
  if (typeof check?.quality_checked !== 'number') return {}
  return { quality_status: check.quality_status, quality_score: check.quality_score, quality_grade: check.quality_grade, quality_summary: check.quality_summary, quality_checked: check.quality_checked }
}
async function runNodeTest(node: ManagedNode): Promise<NodeTestResult> {
  setNodeBusy(testingNodes, node.name, true)
  try {
    const { data } = await apiClient.post<NodeTestResult>(nodeCheckPath(node.name, 'test'), undefined, { timeout: nodeTestTimeout })
    applyNodeCheck(node.name, previous => ({
      ...previousQuality(previous), checked_at: Math.floor(Date.now() / 1000), latency_status: data.success ? 'success' : 'failed', latency_message: data.message,
      ...(data.success ? { latency_ms: data.latency_ms, ip_address: data.ip_address, country: data.country, country_code: data.country_code, region: data.region, city: data.city } : {})
    }))
    return data
  } finally { setNodeBusy(testingNodes, node.name, false) }
}
async function runNodeQuality(node: ManagedNode): Promise<ProxyQualityCheckResult> {
  setNodeBusy(qualityNodes, node.name, true)
  try {
    const { data } = await apiClient.post<ProxyQualityCheckResult>(nodeCheckPath(node.name, 'quality-check'), undefined, { timeout: nodeQualityTimeout })
    const reachable = (data.items || []).some(item => item.target === 'base_connectivity' && item.status === 'pass')
    applyNodeCheck(node.name, previous => {
      const sameExit = !!data.exit_ip && previous?.ip_address === data.exit_ip
      return {
        checked_at: Math.floor(Date.now() / 1000), latency_status: reachable ? 'success' : 'failed', latency_ms: reachable && data.base_latency_ms ? data.base_latency_ms : undefined, latency_message: data.summary,
        ip_address: data.exit_ip, country: data.country, country_code: data.country_code, region: sameExit ? previous?.region : undefined, city: sameExit ? previous?.city : undefined,
        quality_status: summarizeProxyQuality(data), quality_score: data.score, quality_grade: data.grade, quality_summary: data.summary, quality_checked: data.checked_at
      }
    })
    return data
  } finally { setNodeBusy(qualityNodes, node.name, false) }
}
async function testNode(node: ManagedNode) {
  try {
    const result = await runNodeTest(node)
    if (result.success) appStore.showSuccess(result.latency_ms ? t('admin.proxies.proxyWorkingWithLatency', { latency: result.latency_ms }) : t('admin.proxies.proxyWorking'))
    else appStore.showError(result.message || t('admin.proxies.proxyTestFailed'))
  } catch (cause: unknown) {
    appStore.showError(causeMessage(cause, t('admin.proxies.failedToTest')))
  }
  if (!disposed) void refresh()
}
async function checkNodeQuality(node: ManagedNode) {
  try {
    const result = await runNodeQuality(node)
    if (disposed) return
    qualityReportName.value = node.display_name || node.name
    qualityReport.value = result
    appStore.showSuccess(t('admin.proxies.qualityCheckDone', { score: result.score, grade: result.grade }))
  } catch (cause: unknown) {
    appStore.showError(causeMessage(cause, t('admin.proxies.qualityCheckFailed')))
  }
  if (!disposed) void refresh()
}
async function runNodeBatch(nodes: ManagedNode[], concurrency: number, run: (node: ManagedNode) => Promise<void>) {
  let index = 0
  const worker = async () => {
    while (index < nodes.length && !disposed) await run(nodes[index++])
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, nodes.length) }, worker))
}
async function batchTestNodes() {
  const targets = [...nodeBatchTargets.value]
  if (batchTesting.value || !targets.length) return
  batchTesting.value = true
  let succeeded = 0
  let failed = 0
  try {
    await runNodeBatch(targets, 3, async node => {
      if (testingNodes.value.has(node.name)) return
      try {
        if ((await runNodeTest(node)).success) succeeded++
        else failed++
      } catch { failed++ }
    })
    if (disposed) return
    const summary = t('consoleFields.batchTestSummary', { total: succeeded + failed, succeeded, failed })
    if (failed) appStore.showWarning(summary)
    else appStore.showSuccess(summary)
  } finally {
    batchTesting.value = false
    if (!disposed) void refresh()
  }
}
async function batchCheckNodeQuality() {
  const targets = [...nodeBatchTargets.value]
  if (batchQualityChecking.value || !targets.length) return
  batchQualityChecking.value = true
  const counts = { healthy: 0, warn: 0, challenge: 0, failed: 0 }
  try {
    await runNodeBatch(targets, 2, async node => {
      if (qualityNodes.value.has(node.name)) return
      try { counts[summarizeProxyQuality(await runNodeQuality(node))]++ } catch { counts.failed++ }
    })
    if (disposed) return
    const total = counts.healthy + counts.warn + counts.challenge + counts.failed
    const summary = t('consoleFields.qualityTestSummary', { total, ...counts })
    if (counts.challenge || counts.failed) appStore.showWarning(summary)
    else appStore.showSuccess(summary)
  } finally {
    batchQualityChecking.value = false
    if (!disposed) void refresh()
  }
}
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
function scheduleRefresh() {
  if (timer) clearTimeout(timer)
  if (!disposed && !pending.value && (status.value?.busy || warmPools.value.some(p => p.target > 0))) timer = setTimeout(refresh, status.value?.busy ? 1500 : 5000)
}
async function refresh() {
  if (timer) clearTimeout(timer)
  try {
    const { data } = await apiClient.get<Status>('/admin/system/mihomo')
    if (!disposed) { status.value = data; error.value = '' }
  } catch { if (!disposed) error.value = t('consoleInline.cannotReadKernelStatus_723b46') }
  finally { loading.value = false; scheduleRefresh() }
}
async function operate(action: string, payload: Payload = {}, isCurrent: () => boolean = () => !disposed): Promise<boolean> {
  pending.value = true; error.value = ''; success.value = ''
  if (timer) clearTimeout(timer)
  try {
    const response = await apiClient.post<Status>('/admin/system/mihomo', { action, subscriptions: [], dynamic_proxies: [], append: false, ...payload })
    if (!isCurrent()) return false
    status.value = response.data
    while (status.value.busy && !disposed) {
      await new Promise(resolve => setTimeout(resolve, 1500))
      if (!isCurrent()) return false
      const response = await apiClient.get<Status>('/admin/system/mihomo')
      if (!isCurrent()) return false
      status.value = response.data
    }
    if (!isCurrent()) return false
    if (status.value.error) { error.value = status.value.error; return false }
    success.value = t('consoleInline.changesApplied_85149f')
    return true
  } catch (cause: unknown) {
    if (!isCurrent()) return false
    error.value = (cause as { message?: string })?.message || t('consoleInline.operationFailedPleaseRetry_aa79cd')
    return false
  } finally { pending.value = false; scheduleRefresh() }
}
async function saveDownloadMode() {
  pending.value = true; error.value = ''; success.value = ''
  if (timer) clearTimeout(timer)
  try {
    status.value = (await apiClient.put<Status>('/admin/system/mihomo/download-mode', { mode: downloadMode.value })).data
    downloadModeDirty.value = false
    downloadMode.value = status.value.subscription_download_mode || 'auto'
    success.value = t('consoleInline.downloadModeSaved_b11a60')
  } catch (cause: unknown) {
    error.value = (cause as { message?: string })?.message || t('consoleInline.cannotSaveDownloadMode_ccb013')
  } finally { pending.value = false; scheduleRefresh() }
}
function openSubscription(source?: Subscription) {
  editing.value = source; subscriptionName.value = source?.label || ''; subscriptions.value = ''; error.value = ''; subscriptionDialog.value = true
}
async function saveSubscription() {
  const urls = subscriptions.value.split(/\r?\n/).map(s => s.trim()).filter(Boolean)
  const action = editing.value ? (urls.length ? 'subscription_update/' : 'subscription_rename/') + editing.value.id : 'subscription_add'
  if (await operate(action, { subscriptions: urls, name: subscriptionName.value.trim() })) { subscriptions.value = ''; subscriptionDialog.value = false }
}
function dynamicPayload(): Payload {
  return { dynamic_proxies: dynamicProxies.value.split(/\r?\n/).map(s => s.trim()).filter(Boolean).map(s => s.includes('://') ? s : dynamicProtocol.value + '://' + s) }
}
async function saveDynamic() {
  if (replaceDynamic.value && status.value?.dynamic_proxies) {
    confirmation.value = { actions: ['dynamic_replace'], payload: dynamicPayload(), message: t('consoleInline.thisReplacesAllSavedDynamicProxiesAnd_200eab') }
  } else if (await operate(replaceDynamic.value ? 'dynamic_replace' : 'dynamic_append', dynamicPayload())) { dynamicProxies.value = ''; dynamicDialog.value = false }
}
function confirmRemoval(actions: string[]) {
  confirmation.value = { actions, message: t('consoleInline.removeTheseSourcesAndTheirExclusiveNodes_3df2c1') }
}
function confirmActions() {
 return confirmActionsAction.run(async (isCurrent) => {
  const task = confirmation.value
  if (!task || busy.value) return
  for (const action of task.actions) {
    if (!isCurrent() || !await operate(action, task.payload, isCurrent)) return
    if (!isCurrent()) return
    selected.value = selected.value.filter(id => !action.endsWith('/' + id))
  }
  if (task.actions.includes('dynamic_replace')) { dynamicProxies.value = ''; dynamicDialog.value = false }
  confirmation.value = undefined
 })
}
async function batchSubscriptions(action: string) {
  const targets = [...selected.value]
  for (const id of targets) {
    if (action === 'subscription_refresh' && !status.value?.subscription_items?.find(s => s.id === id)?.enabled) continue
    if (!await operate(action + '/' + id)) return
  }
  selected.value = []
}
async function importFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (file.size > 256 * 1024) { error.value = t('consoleInline.fileMustBeAtMost256KiB_590638'); return }
  try { subscriptions.value = await file.text(); input.value = '' } catch { error.value = t('consoleInline.cannotReadFile_027129') }
}
onMounted(refresh)
onUnmounted(() => { disposed = true; if (timer) clearTimeout(timer) })
const confirmActionsAction = useConfirmedAction(() => ({ active: !!confirmation.value, identity: [props.section, confirmation.value] }))
watch(() => props.section, () => { confirmation.value = undefined }, { flush: 'sync' })
watch(dynamicDialog, (show, previous) => { if (previous && !show && confirmation.value?.actions.includes('dynamic_replace')) confirmation.value = undefined }, { flush: 'sync' })
</script>
