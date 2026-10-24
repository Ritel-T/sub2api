<template>
  <AppLayout :show-page-title="false">
    <div class="open-page space-y-5">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div><AppHeading variant="page" as="h1" class="text-gray-900 dark:text-white">{{ t('admin.requestCapture.title') }}</AppHeading><p class="mt-1 text-sm text-gray-500">{{ t('admin.requestCapture.description') }}</p></div>
        <button class="btn btn-secondary" :disabled="loading" @click="refresh">{{ t('admin.requestCapture.refresh') }}</button>
      </div>
      <p class="rounded-lg bg-amber-50 p-4 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-200">{{ t('admin.requestCapture.privacy') }}</p>
      <p v-if="error || listError" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-200">{{ error || requestErrorMessage(listError) }}</p>
      <div v-if="stats" class="open-section flex flex-wrap gap-5 text-sm">
        <span>{{ t('admin.requestCapture.instance') }}: <code>{{ stats.instance_id }}</code></span>
        <span>{{ t('admin.requestCapture.used') }}: {{ bytes(stats.used_bytes) }}</span>
        <span>{{ t('admin.requestCapture.active') }}: {{ stats.active_requests }}</span>
        <span>{{ t('admin.requestCapture.buffer') }}: {{ bytes(stats.buffer_bytes) }}</span>
        <span v-if="stats.admission_skipped">{{ t('admin.requestCapture.admissionSkipped') }}: {{ stats.admission_skipped }}</span>
        <strong v-if="stats.storage_error" class="text-red-600">{{ t('admin.requestCapture.storageError') }}</strong>
      </div>
      <form class="open-section space-y-4 py-5" @submit.prevent="create">
        <AppHeading variant="section" as="h2">{{ t('admin.requestCapture.create') }}</AppHeading>
        <div class="grid gap-4 md:grid-cols-4">
          <label class="space-y-1"><span class="input-label">{{ t('admin.requestCapture.targetType') }}</span><select v-model="targetType" class="input"><option v-for="kind in kinds" :key="kind" :value="kind">{{ t('admin.requestCapture.' + kind) }}</option></select></label>
          <label class="space-y-1 md:col-span-2"><span class="input-label">{{ t('admin.requestCapture.search') }}</span><input v-model="search" class="input" type="search" :placeholder="t('admin.requestCapture.searchHint')" /></label>
          <label class="space-y-1"><span class="input-label">{{ t('admin.requestCapture.duration') }}</span><input v-model.number="duration" class="input" type="number" min="1" max="1440" step="1" required /></label>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <select v-model.number="targetID" class="input min-w-0 flex-1" :disabled="searching" required :aria-label="t('admin.requestCapture.target')"><option :value="0" disabled>{{ t('admin.requestCapture.selectTarget') }}</option><option v-for="option in targets" :key="option.id" :value="option.id">#{{ option.id }} {{ option.label }}</option></select>

        </div>
        <InfiniteListSentinel data-testid="capture-targets-more" :loading="searching" :error="targetList.error.value" :has-more="targetList.hasMore.value" :progress="targets.length" @load-more="targetList.loadMore" @retry="targetList.retry" />
        <label class="flex items-center gap-2 text-sm"><input v-model="saveMedia" type="checkbox" class="checkbox" />{{ t('admin.requestCapture.saveMedia') }}</label>
        <p class="text-xs text-gray-500">{{ t('admin.requestCapture.targetHint') }}</p>
        <button class="btn btn-primary" :disabled="busy || !targetID || !validDuration">{{ t('admin.requestCapture.start') }}</button>
      </form>
      <div class="open-section">
        <div class="overflow-x-auto overflow-y-hidden"><DataTable column-order-key="admin-request-capture-tasks"
 :columns="openRecordColumns"
 :primary-columns="['target','status','remaining','records','actions']"
 :data="tasks" :loading="taskList.loading.value" row-key="id" >
<template #cell-target="{ row: task }"><div class="p-3"><button class="text-primary-600 underline" @click="selectTask(task)">{{ t('admin.requestCapture.' + task.target_type) }} #{{ task.target_id }} {{ task.target_name }}</button><div class="text-xs text-gray-500">{{ task.save_media ? t('admin.requestCapture.mediaIncluded') : t('admin.requestCapture.metadataOnly') }}</div></div></template>
<template #cell-status="{ row: task }"><div class="p-3">{{ task.status }}<div v-if="task.reason" class="text-xs text-amber-700">{{ task.reason }}</div></div></template>
<template #cell-remaining="{ row: task }"><div class="p-3">{{ remaining(task) }}</div></template>
<template #cell-records="{ row: task }"><div class="p-3">{{ task.requests }}</div></template>
<template #cell-incomplete="{ row: task }"><div class="p-3">{{ task.partial }} / {{ task.skipped }}</div></template>
<template #cell-size="{ row: task }"><div class="p-3">{{ bytes(task.bytes) }}</div></template>
<template #cell-actions="{ row: task }"><div class="p-3"><div class="flex gap-2"><button :title="t('admin.requestCapture.stop')" :aria-label="t('admin.requestCapture.stop')" v-if="task.status === 'running'" class="workspace-icon-action theme-action-button" :disabled="busy" @click="stop(task)"><Icon name="ban" size="sm" /><span class="sr-only">{{ t('admin.requestCapture.stop') }}</span></button><button :title="t('admin.requestCapture.export')" :aria-label="t('admin.requestCapture.export')" v-else class="workspace-icon-action theme-action-button" :disabled="busy || !streamExport" @click="download(task.id)"><Icon name="download" size="sm" /><span class="sr-only">{{ t('admin.requestCapture.export') }}</span></button><button :title="t('admin.requestCapture.delete')" :aria-label="t('admin.requestCapture.delete')" class="workspace-icon-action text-red-600 dark:text-red-400" :disabled="busy" @click="remove(task)"><Icon name="trash" size="sm" /><span class="sr-only">{{ t('admin.requestCapture.delete') }}</span></button></div></div></template>
</DataTable></div>
        <InfiniteListSentinel data-testid="capture-tasks-more" :loading="taskList.loading.value || taskList.loadingMore.value || taskList.refreshing.value" :error="taskList.error.value" :has-more="taskList.hasMore.value" :progress="tasks.length" @load-more="taskList.loadMore" @retry="taskList.retry" />
      </div>
      <p v-if="!streamExport" class="text-sm text-gray-500">{{ t('admin.requestCapture.exportBrowser') }}</p>
      <section v-if="selectedTask" class="open-section space-y-4 py-5" data-testid="capture-records-section">
        <AppHeading variant="section" as="h2">{{ t('admin.requestCapture.records') }} - {{ selectedTask.target_name }}</AppHeading>
        <form class="workspace-filter-controls flex flex-wrap items-center gap-3" @submit.prevent="filterRecords"><input v-model.trim="requestID" class="input max-w-md" placeholder="Request ID" /><span class="text-sm text-gray-500">{{ t('admin.requestCapture.errorsOnly') }}</span><button class="btn btn-secondary">{{ t('admin.requestCapture.filter') }}</button><div ref="recordsToolbarTarget" /></form>
        <DataTable column-order-key="admin-request-capture-records" :columns="recordColumns" :data="records"
          :loading="recordList.loading.value" row-key="id" :toolbar-target="recordsToolbarTarget">
          <template #cell-request_id="{ row: record }">
            <button type="button" class="break-all text-primary-600 underline" @click="viewRecord(record)">{{ record.request_id || record.id }}</button>
            <div class="text-xs text-gray-500">{{ record.created_at }} / {{ record.protocol }}<span v-if="record.turn"> / {{ t('admin.requestCapture.turn') }} {{ record.turn }}</span></div>
          </template>
          <template #cell-status="{ row: record }">
            <span>HTTP {{ record.status }}</span><span v-if="record.is_error" class="ml-2 text-red-600" data-testid="capture-outcome">{{ captureOutcome(record) }}</span>
            <div v-if="record.error_code" class="break-all text-xs text-red-600">{{ record.error_code }}</div>
            <span v-if="record.partial" class="ml-2 text-amber-700">{{ record.reason || t('admin.requestCapture.partial') }}</span>
          </template>
          <template #cell-bytes="{ row: record }">{{ bytes(record.bytes) }}</template>
        </DataTable>
        <InfiniteListSentinel data-testid="capture-records-more" :loading="recordList.loading.value || recordList.loadingMore.value || recordList.refreshing.value" :error="recordList.error.value" :has-more="recordList.hasMore.value" :progress="records.length" @load-more="recordList.loadMore" @retry="recordList.retry" />
      </section>
      <section v-if="detail" class="open-section space-y-4 py-5">
        <div class="flex justify-between gap-3"><AppHeading variant="section" as="h2">{{ t('admin.requestCapture.detail') }} - {{ detail.request_id }}</AppHeading><button class="btn btn-secondary" :disabled="busy || !streamExport || selectedTask?.status === 'running'" @click="download(detail.task_id, detail.id)">{{ t('admin.requestCapture.export') }}</button></div>
        <p v-if="detail.is_error" class="text-sm text-red-600">HTTP {{ detail.status }} · {{ captureOutcome(detail) }}<span v-if="detail.error_code"> · {{ detail.error_code }}</span></p>
        <p v-if="detail.partial" class="text-sm text-amber-700">{{ t('admin.requestCapture.partial') }}: {{ detail.reason }}</p>
        <details><summary class="cursor-pointer text-sm">{{ t('admin.requestCapture.metadata') }}</summary><pre class="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ metadata }}</pre></details>
        <div class="flex flex-wrap gap-2"><button v-for="item in stages" :key="item" class="btn" :class="stage === item ? 'btn-primary' : 'btn-secondary'" @click="selectStage(item)">{{ t('admin.requestCapture.' + item) }}</button></div>
        <select v-if="stageParts.length" :value="part?.name" class="input" :aria-label="t('admin.requestCapture.segment')" @change="selectPart(($event.target as HTMLSelectElement).value)"><option v-for="item in stageParts" :key="item.name" :value="item.name">{{ item.name }} / {{ t('admin.requestCapture.attempt') }} {{ item.attempt }} / {{ t('admin.requestCapture.turn') }} {{ item.turn }} / {{ bytes(item.bytes) }}</option></select>
        <p v-else class="text-sm text-gray-500">{{ t('admin.requestCapture.noPart') }}</p>
        <p v-if="part?.omitted" class="text-sm text-amber-700">{{ t('admin.requestCapture.omitted') }}: {{ part.omitted }}</p>
        <pre v-if="part" class="max-h-[36rem] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-950 p-4 text-xs leading-5 text-gray-100">{{ content }}</pre>
        <div v-if="part" class="flex gap-3"><button class="btn btn-secondary" :disabled="contentOffset === 0 || contentLoading" @click="readContent(0)">{{ t('admin.requestCapture.firstSegment') }}</button><button class="btn btn-secondary" :disabled="!contentMore || contentLoading" @click="readContent(nextOffset)">{{ t('admin.requestCapture.nextSegment') }}</button></div>
      </section>
    </div>
  </AppLayout>

  <ConfirmationPrompt :confirmation="confirmation" />
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import ConfirmationPrompt from '@/components/common/ConfirmationPrompt.vue'
import { useConfirmation } from '@/composables/useConfirmation'

import Icon from '@/components/icons/Icon.vue'
import DataTable from '@/components/common/DataTable.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { adminAPI } from '@/api'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import * as api from '@/api/admin/requestCaptures'
import type { CaptureTask, CaptureRecord, CapturePart, CaptureStats, CaptureTarget } from '@/api/admin/requestCaptures'

const { t } = useI18n()
const router = useRouter()
const settings = useAdminSettingsStore()
const kinds: CaptureTarget[] = ['user', 'account', 'group']
const stages = ['client_request', 'upstream_request', 'upstream_response', 'client_response']
const targetType = ref<CaptureTarget>('user'), targetID = ref(0), duration = ref(10), saveMedia = ref(false), search = ref('')
const stats = ref<CaptureStats>(), selectedTask = ref<CaptureTask>()
const appliedRequestID = ref(''), targetDebouncing = ref(false)
const requestID = ref(''), detail = ref<CaptureRecord>(), stage = ref(stages[0]), part = ref<CapturePart>()
const content = ref(''), contentOffset = ref(0), nextOffset = ref(0), contentMore = ref(false), contentLoading = ref(false)
const busy = ref(false), error = ref(''), now = ref(Date.now())
const streamExport = api.canStreamExport()
const validDuration = computed(() => Number.isInteger(duration.value) && duration.value >= 1 && duration.value <= 1440)
const stageParts = computed(() => (detail.value?.parts || []).filter(item => item.stage === stage.value).sort((a, b) => a.name.localeCompare(b.name)))
const metadata = computed(() => { if (!detail.value) return ''; const { parts: _parts, ...meta } = detail.value; return JSON.stringify(meta, null, 2) })
let poll: ReturnType<typeof setInterval> | undefined, debounce: ReturnType<typeof setTimeout> | undefined
let disposed = false, detailVersion = 0, contentVersion = 0
const targetList = useInfiniteList<{ id: number; label: string }>({
  pageSize: 20, keyOf: item => item.id,
  fetchPage: async (page, signal) => {
    const kind = targetType.value, options = { signal }, filter = { search: search.value }
    const result = kind === 'user' ? await adminAPI.users.list(page, 20, filter, options) : kind === 'account' ? await adminAPI.accounts.list(page, 20, { ...filter, lite: 'true' }, options) : await adminAPI.groups.list(page, 20, filter, options)
    return { ...result, items: result.items.map(item => ({ id: item.id, label: 'email' in item ? item.email : item.name })) }
  }
})
const taskList = useInfiniteList<CaptureTask>({
  pageSize: 20, keyOf: item => item.id,
  fetchPage: async (page, signal) => {
    const result = await api.listTasks(page, signal)
    if (!signal.aborted) stats.value = result.stats
    return { items: result.items, pages: result.has_more ? page + 1 : page }
  }
})
const recordList = useInfiniteList<CaptureRecord>({
  pageSize: 20, keyOf: item => item.id,
  fetchPage: async (page, signal) => {
    if (!selectedTask.value) return { items: [], pages: 0 }
    const result = await api.listRecords(selectedTask.value.id, page, appliedRequestID.value, true, signal)
    return { items: result.items, pages: result.has_more ? page + 1 : page }
  }
})
const recordsToolbarTarget = ref<HTMLElement | null>(null)
const recordColumns = computed(() => [
  { key: 'request_id', label: 'Request ID', primary: true },
  { key: 'model', label: t('admin.requestCapture.model'), primary: true },
  { key: 'status', label: t('admin.requestCapture.status'), primary: true },
  { key: 'bytes', label: t('admin.requestCapture.size'), primary: true }
])
const targets = targetList.items, tasks = taskList.items, records = recordList.items
const searching = computed(() => targetDebouncing.value || targetList.loading.value || targetList.loadingMore.value || targetList.refreshing.value)
const loading = computed(() => taskList.loading.value || taskList.loadingMore.value || taskList.refreshing.value || recordList.loading.value || recordList.loadingMore.value || recordList.refreshing.value)
const listError = computed(() => targetList.error.value || taskList.error.value || recordList.error.value)
watch(listError, cause => { if (cause) report(cause, false) })
function captureOutcome(record: CaptureRecord) {
  if (record.status >= 400) return t('admin.requestCapture.requestFailed')
  if (record.client_outcome === 'failed' || (!record.client_outcome && record.error_code === 'basispoints_stream_incomplete')) return t('admin.requestCapture.streamFailed')
  if (record.client_outcome === 'completed') return t('admin.requestCapture.completedWithErrors')
  // Older exports do not distinguish final failure from a recovered attempt.
  return t('admin.requestCapture.containsErrors')
}
function bytes(value: number) { return (value / 1048576).toFixed(2) + ' MiB' }
function remaining(task: CaptureTask) { if (task.status !== 'running') return '-'; const seconds = Math.max(0, Math.ceil((Date.parse(task.expires_at) - now.value) / 1000)); return Math.floor(seconds / 60) + 'm ' + seconds % 60 + 's' }
function requestErrorMessage(err: unknown) { return (err as { message?: string })?.message || t('admin.requestCapture.failed') }
function report(err: unknown, publish = true) { const e = err as { message?: string; status?: number; name?: string; code?: string }; if (e.name === 'AbortError' || e.code === 'ERR_CANCELED') return; if (publish) error.value = requestErrorMessage(err); if (e.status === 404 && e.message === 'Request capture is disabled') { settings.setRequestCaptureEnabledLocal(false); void router.replace('/admin/settings') } }
function filterRecords() {
  appliedRequestID.value = requestID.value
  detail.value = undefined; detailVersion++; contentVersion++
  void recordList.reset()
}
async function refresh() {
  if (taskList.loading.value || taskList.loadingMore.value || taskList.refreshing.value || disposed) return
  await taskList.refresh()
  if (disposed) return
  if (selectedTask.value) {
    selectedTask.value = tasks.value.find(item => item.id === selectedTask.value?.id) || selectedTask.value
    if (!recordList.loading.value && !recordList.loadingMore.value && !recordList.refreshing.value) await recordList.refresh()
  }
}
async function action(fn: () => Promise<void>) { busy.value = true; error.value = ''; try { await fn(); await refresh() } catch (err) { report(err) } finally { busy.value = false } }
async function create() { if (!targetID.value || !validDuration.value) return; await action(async () => { const task = await api.createTask({ target_type: targetType.value, target_id: targetID.value, duration_minutes: duration.value, save_media: saveMedia.value }); selectTask(task) }) }
async function stop(task: CaptureTask) { await action(() => api.stopTask(task.id)) }
async function remove(task: CaptureTask) {
  const taskId = task.id
  await confirmation.run({ message: t('admin.requestCapture.deleteConfirm'), title: t('common.delete'), confirmText: t('common.delete'), danger: true }, async (isCurrent) => {
    busy.value = true; error.value = ''
    try {
      await api.deleteTask(taskId)
      if (!isCurrent()) return
      if (selectedTask.value?.id === taskId) {
        selectedTask.value = undefined; detail.value = undefined; recordList.clear(); detailVersion++; contentVersion++
      }
      await refresh()
    } catch (err) { if (isCurrent()) report(err) }
    finally { busy.value = false }
  })
}

async function download(task: string, record?: string) { await action(() => api.exportCapture(task, record)) }
function selectTask(task: CaptureTask) { selectedTask.value = task; appliedRequestID.value = requestID.value; detail.value = undefined; detailVersion++; contentVersion++; void recordList.reset() }
async function viewRecord(record: CaptureRecord) { const version = ++detailVersion; try { const result = await api.getRecord(record.task_id, record.id); if (version !== detailVersion || disposed) return; detail.value = result; selectStage(stages[0]) } catch (err) { report(err) } }
function selectStage(value: string) { stage.value = value; part.value = stageParts.value[0]; content.value = ''; contentMore.value = false; contentVersion++; if (part.value && part.value.bytes > 0) void readContent(0) }
function selectPart(name: string) { part.value = stageParts.value.find(item => item.name === name); content.value = ''; contentVersion++; if (part.value && part.value.bytes > 0) void readContent(0) }
async function readContent(offset: number) { if (!detail.value || !part.value) return; const version = ++contentVersion; contentLoading.value = true; try { const result = await api.getContent(detail.value.task_id, detail.value.id, part.value.name, offset); if (version !== contentVersion || disposed) return; content.value = result.text; contentOffset.value = offset; nextOffset.value = result.next_offset; contentMore.value = result.has_more } catch (err) { report(err) } finally { if (version === contentVersion) contentLoading.value = false } }
watch([targetType, search], () => { targetID.value = 0; targetList.clear(); targetDebouncing.value = true; clearTimeout(debounce); debounce = setTimeout(() => { targetDebouncing.value = false; void targetList.reset() }, 250) }, { flush: 'sync' })
onMounted(() => { void refresh(); void targetList.reset(); poll = setInterval(() => { now.value = Date.now(); if (!taskList.error.value && !recordList.error.value) void refresh() }, 3000) })
onUnmounted(() => { disposed = true; clearInterval(poll); clearTimeout(debounce); targetList.cancel(); taskList.cancel(); recordList.cancel(); detailVersion++; contentVersion++ })

const openRecordColumns = computed(() => [{ key: 'target', label: t('admin.requestCapture.target'), sortable: false }, { key: 'status', label: t('admin.requestCapture.status'), sortable: false }, { key: 'remaining', label: t('admin.requestCapture.remaining'), sortable: false }, { key: 'records', label: t('admin.requestCapture.records'), sortable: false }, { key: 'incomplete', label: t('admin.requestCapture.incomplete'), sortable: false }, { key: 'size', label: t('admin.requestCapture.size'), sortable: false }, { key: 'actions', label: t('admin.requestCapture.actions'), sortable: false }])
const confirmation = useConfirmation(() => ({ active: true, identity: selectedTask.value?.id }))
</script>
