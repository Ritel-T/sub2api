<script setup lang="ts">
import { inject, type Ref, computed, onMounted, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { useElementSize } from '@vueuse/core'
import { useDocumentRows } from '@/composables/useDocumentRows'
import Icon from '@/components/icons/Icon.vue'
import TableSortControl from '@/components/common/TableSortControl.vue'
import type { Column } from '@/components/common/types'
import type { ApiKey } from '@/types'
import { useKeyWorkspaceCopy } from './keyWorkspaceCopy'

const props = withDefaults(defineProps<{
  columns: Column[]
  toolbarTarget?: string | HTMLElement | null
  data: ApiKey[]
  loading?: boolean
  selectedKeys: Array<string | number>
  busyKeys?: Set<number>
  rename: (row: ApiKey, name: string) => Promise<boolean>
  sortKey: string
  sortOrder: 'asc' | 'desc'
}>(), { loading: false })
const emit = defineEmits<{
  'update:selectedKeys': [keys: Array<string | number>]
  columnReorder: [keys: string[]]
  sort: [key: string, order: 'asc' | 'desc']
}>()
const { t } = useI18n()
const inheritedToolbarTarget = inject<Ref<HTMLElement | null> | null>('tableToolbarTarget', null)
const toolbarReady = ref(false)
onMounted(async () => { await nextTick(); toolbarReady.value = true })
const resolvedToolbarTarget = computed(() => {
  if (!toolbarReady.value) return undefined
  const target = props.toolbarTarget || inheritedToolbarTarget?.value
  return typeof target === 'string' ? document.querySelector(target) || undefined : target || undefined
})
const kw = useKeyWorkspaceCopy()
const componentId = useId()
const host = ref<HTMLElement>()
const { width: hostWidth } = useElementSize(host)
const expanded = ref(new Set<number>())
const documentBody = ref<HTMLElement | null>(null)
const { rendered: renderRows, paddingTop, paddingBottom, measure: measureRow } = useDocumentRows(
  computed(() => props.data), documentBody, row => row.id, computed(() => true),
  () => hostWidth.value < 768 ? 500 : 68
)
const mainKeys = new Set(['id', 'name', 'key', 'group', 'usage', 'current_concurrency', 'status', 'actions'])
const mainColumns = computed(() => {
  const columns = props.columns.filter(column => column.key === 'id' || (column.primary ?? mainKeys.has(column.key)))
  const fixed = columns.some(column => column.key === 'id') ? columns : [{ key: 'id', label: 'ID', sortable: true }, ...columns]
  return [...fixed.filter(column => column.key !== 'actions'), ...fixed.filter(column => column.key === 'actions')]
})
const detailColumns = computed(() => props.columns.filter(column => column.key !== 'id' && !(column.primary ?? mainKeys.has(column.key))))
const sortableColumns = computed(() => props.columns.filter(column => column.sortable))
const allSelected = computed(() => props.data.length > 0 && props.data.every(row => props.selectedKeys.includes(row.id)))
const someSelected = computed(() => props.data.some(row => props.selectedKeys.includes(row.id)))
const selectionState = computed(() => allSelected.value ? true : someSelected.value ? 'mixed' : false)
const fixedWidths: Record<string, number> = { id: 104, status: 112, actions: 188 }
const columnWeights: Record<string, number> = { name: 1.7, key: 1.3, group: 1.5, usage: 1.4, current_concurrency: 1 }
const fixedWidth = computed(() => mainColumns.value.reduce((sum, column) => sum + (fixedWidths[column.key] || 0), 40))
const totalWeight = computed(() => mainColumns.value.reduce((sum, column) => sum + (fixedWidths[column.key] ? 0 : columnWeights[column.key] || 1), 0))
function columnWidth(key: string) {
  if (fixedWidths[key]) return `${fixedWidths[key]}px`
  const ratio = (columnWeights[key] || 1) / (totalWeight.value || 1)
  return `${(Math.max(1080, hostWidth.value) - fixedWidth.value) * ratio}px`
}
function valueFor(row: ApiKey, key: string): unknown {
  return (row as unknown as Record<string, unknown>)[key]
}
function fallbackValue(row: ApiKey, column: Column) {
  const value = valueFor(row, column.key)
  if (column.formatter) return column.formatter(value, row)
  if (column.key === 'group') return row.group?.name || '—'
  return value === null || value === undefined || value === '' ? '—' : String(value)
}
function toggleSelected(id: number) {
  emit('update:selectedKeys', props.selectedKeys.includes(id)
    ? props.selectedKeys.filter(value => value !== id)
    : [...props.selectedKeys, id])
}
function toggleAll() {
  const visibleIds = new Set(props.data.map(row => row.id))
  emit('update:selectedKeys', allSelected.value
    ? props.selectedKeys.filter(id => !visibleIds.has(id as number))
    : [...new Set([...props.selectedKeys, ...visibleIds])])
}
function toggleDetails(id: number) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}
function rowClick(event: MouseEvent, id: number) {
  const target = event.target
  if (!(target instanceof Element)) return
  if (target.closest('button,input,select,a,textarea,label,[role="button"],[role="switch"],[role="checkbox"],[contenteditable="true"]')) return
  if (window.getSelection()?.type === 'Range') return
  toggleDetails(id)
}
function requestSort(key: string) {
  if (!sortableColumns.value.some(column => column.key === key)) return
  emit('sort', key, props.sortKey === key && props.sortOrder === 'asc' ? 'desc' : 'asc')
}
function changeMobileSort(key: string) {
  if (sortableColumns.value.some(column => column.key === key)) emit('sort', key, 'asc')
}
function reverseMobileSort() {
  const key = sortableColumns.value.find(column => column.key === props.sortKey)?.key || sortableColumns.value[0]?.key
  if (key) emit('sort', key, props.sortOrder === 'asc' ? 'desc' : 'asc')
}
function ariaSort(key: string): 'none' | 'ascending' | 'descending' {
  return props.sortKey === key ? props.sortOrder === 'asc' ? 'ascending' : 'descending' : 'none'
}
function applyColumnOrder(columns: Column[]) {
  const movable = columns.map(column => column.key)
  let index = 0
  const keys = props.columns.map(column =>
    mainColumns.value.some(main => main.key === column.key) ? movable[index++] || column.key : column.key)
  emit('columnReorder', keys)
}
const draggableColumns = computed({ get: () => mainColumns.value, set: applyColumnOrder })
function canMoveHeaderColumn(event: { related: HTMLElement; dragged: HTMLElement }) {
  return !!event.related.dataset.columnKey && !!event.dragged.dataset.columnKey
}
async function moveHeaderColumn(key: string, delta: number) {
  const columns = [...mainColumns.value]
  const from = columns.findIndex(column => column.key === key)
  if (from < 0) return
  const to = Math.max(0, Math.min(columns.length - 1, from + delta))
  if (from === to) return
  columns.splice(to, 0, columns.splice(from, 1)[0]!)
  applyColumnOrder(columns)
  await nextTick()
  Array.from(host.value?.querySelectorAll<HTMLElement>('th[data-column-key]') || [])
    .find(cell => cell.dataset.columnKey === key)?.querySelector<HTMLButtonElement>('.table-column-drag-handle')?.focus()
}

type NameSession = { id: number; original: string; draft: string }
const nameSession = ref<NameSession | null>(null)
const savingId = ref<number | null>(null)
const nameError = ref('')
function rowBusy(row: ApiKey) {
  return props.loading || savingId.value === row.id || !!props.busyKeys?.has(row.id)
}
async function focusName(id: number) {
  await nextTick()
  host.value?.querySelector<HTMLButtonElement>(`[data-test="rename-key"][data-key-id="${id}"]`)?.focus()
}
async function beginName(row: ApiKey) {
  if (rowBusy(row) || savingId.value !== null) return
  nameError.value = ''
  nameSession.value = { id: row.id, original: row.name, draft: row.name }
  await nextTick()
  const input = host.value?.querySelector<HTMLInputElement>('[data-test="rename-input"]')
  input?.focus()
  input?.select()
}
async function cancelName(row: ApiKey) {
  if (savingId.value !== null || nameSession.value?.id !== row.id) return
  nameSession.value = null
  nameError.value = ''
  await focusName(row.id)
}
async function saveName(row: ApiKey, restoreFocus = false) {
  const session = nameSession.value
  if (!session || session.id !== row.id || savingId.value !== null || props.busyKeys?.has(row.id)) return
  const value = session.draft.trim()
  if (!value) {
    nameError.value = kw.value.nameRequired
    return
  }
  nameError.value = ''
  if (value === session.original) {
    nameSession.value = null
    if (restoreFocus) await focusName(row.id)
    return
  }
  savingId.value = row.id
  let succeeded = false
  try {
    succeeded = await props.rename(row, value)
  } catch {
    succeeded = false
  } finally {
    savingId.value = null
  }
  if (nameSession.value !== session) return
  if (succeeded) {
    nameSession.value = null
    if (restoreFocus) await focusName(row.id)
  } else {
    nameError.value = t('common.tryAgain')
    if (restoreFocus) {
      await nextTick()
      host.value?.querySelector<HTMLInputElement>('[data-test="rename-input"]')?.focus()
    }
  }
}
function nameBlur(event: FocusEvent, row: ApiKey) {
  const editor = (event.currentTarget as HTMLElement).closest('.workspace-name-editor')
  if (event.relatedTarget instanceof Node && editor?.contains(event.relatedTarget)) return
  void saveName(row)
}
watch(() => props.data.map(row => row.id), ids => {
  const visible = new Set(ids)
  expanded.value = new Set([...expanded.value].filter(id => visible.has(id)))
  if (nameSession.value && !visible.has(nameSession.value.id)) {
    nameSession.value = null
    nameError.value = ''
  }
})
</script>

<template>
  <div ref="host" class="key-workspace-table" :aria-busy="loading">
    <Teleport :to="resolvedToolbarTarget || 'body'" :disabled="!resolvedToolbarTarget">
    <div class="workspace-tools" :class="{ 'is-inline': resolvedToolbarTarget }">
      <button type="button" class="workspace-id-button" role="checkbox" :aria-checked="selectionState" :aria-label="t('common.selectAll')" :disabled="loading || !data.length" data-test="select-all-mobile" @click="toggleAll">ID <span>{{ t('common.selectAll') }}</span></button>
      <TableSortControl v-if="sortableColumns.length" :columns="sortableColumns" :sort-key="sortKey" :sort-order="sortOrder" :disabled="loading" @select="changeMobileSort" @toggle="reverseMobileSort" />
    </div>
    </Teleport>
    <div class="workspace-table-scroll">
      <table :aria-busy="loading" :aria-rowcount="data.length + 1">
        <colgroup><col v-for="column in mainColumns" :key="column.key" :style="{ width: columnWidth(column.key) }"><col style="width:40px"></colgroup>
        <thead>
          <VueDraggable v-model="draggableColumns" tag="tr" handle=".table-column-drag-handle" draggable="th[data-column-key]" direction="horizontal" :force-fallback="true" :fallback-on-body="true" :animation="150" :disabled="loading" :on-move="canMoveHeaderColumn">
            <th v-for="column in mainColumns" :key="column.key" scope="col" :data-column-key="column.key" :aria-sort="column.sortable ? ariaSort(column.key) : undefined">
              <div class="workspace-column-heading">
                <button type="button" class="table-column-drag-handle" :aria-label="t('common.moveColumn', { column: column.label })" :title="t('common.moveColumn', { column: column.label })" :disabled="loading" @click.stop @keydown.left.prevent.stop="moveHeaderColumn(column.key, -1)" @keydown.up.prevent.stop="moveHeaderColumn(column.key, -1)" @keydown.right.prevent.stop="moveHeaderColumn(column.key, 1)" @keydown.down.prevent.stop="moveHeaderColumn(column.key, 1)" @keydown.home.prevent.stop="moveHeaderColumn(column.key, -mainColumns.length)" @keydown.end.prevent.stop="moveHeaderColumn(column.key, mainColumns.length)"><span aria-hidden="true">⋮⋮</span></button>
                <button v-if="column.sortable" type="button" class="workspace-sort-button" :data-column-key="column.key" data-test="sort-column" :disabled="loading" @click="requestSort(column.key)">{{ column.label }}<Icon v-if="sortKey === column.key" :name="sortOrder === 'asc' ? 'chevronUp' : 'chevronDown'" size="xs" aria-hidden="true" /></button>
                <span v-else>{{ column.label }}</span>
                <button v-if="column.key === 'id'" type="button" class="workspace-select-all" role="checkbox" :aria-checked="selectionState" :aria-label="t('common.selectAll')" :title="t('common.selectAll')" :disabled="loading || !data.length" data-test="select-all" @click.stop="toggleAll"><svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" aria-hidden="true"><rect x="1.5" y="1.5" width="13" height="13" rx="2"/><path v-if="allSelected" d="m4 8 3 3 5-6"/><path v-else-if="someSelected" d="M4 8h8"/></svg></button>
              </div>
            </th>
            <th scope="col" class="workspace-expand-heading"><span class="sr-only">{{ kw.details }}</span></th>
          </VueDraggable>
        </thead>
        <tbody ref="documentBody">
          <tr v-if="loading && !data.length" class="workspace-message-row"><td :colspan="mainColumns.length + 1" role="status">{{ t('common.loading') }}</td></tr>
          <template v-else>
            <tr v-if="paddingTop" aria-hidden="true"><td :colspan="mainColumns.length + 1" :style="{ height: `${paddingTop}px`, padding: 0, border: 0 }" /></tr>
            <template v-for="{ row, index, gapBefore } in renderRows" :key="row.id">
              <tr v-if="gapBefore" aria-hidden="true"><td :colspan="mainColumns.length + 1" :style="{ height: `${gapBefore}px`, padding: 0, border: 0 }" /></tr>
              <tr :ref="measureRow" :data-index="index" :aria-rowindex="index + 2" class="key-main-row" :data-key-id="row.id" :aria-busy="rowBusy(row)" :class="{ 'is-selected': selectedKeys.includes(row.id), 'is-expanded': expanded.has(row.id) }" @click="rowClick($event, row.id)">
                <td v-for="column in mainColumns" :key="column.key" :data-column-key="column.key" :class="[`cell-${column.key}`, column.class]">
                  <button v-if="column.key === 'id'" type="button" class="workspace-id-button" role="checkbox" :aria-checked="selectedKeys.includes(row.id)" :aria-label="t('keys.bulkEdit.selectKey', { name: row.name })" data-test="select-row" :data-key-id="row.id" @click.stop="toggleSelected(row.id)"><slot name="cell-id" :row="row" :value="row.id">{{ row.id }}</slot></button>
                  <template v-else-if="column.key === 'name'">
                    <div v-if="nameSession?.id === row.id" class="workspace-name-editor" @click.stop>
                      <div class="workspace-name-controls"><input v-model="nameSession.draft" class="workspace-name-input" :aria-label="`${kw.rename}: ${row.name}`" :aria-invalid="!!nameError" :aria-describedby="nameError ? `${componentId}-name-error` : undefined" :disabled="rowBusy(row)" :aria-busy="savingId === row.id" maxlength="100" data-test="rename-input" @input="nameError = ''" @blur="nameBlur($event, row)" @keydown.enter.prevent.stop="saveName(row, true)" @keydown.esc.prevent.stop="cancelName(row)"><button type="button" class="workspace-icon-button" :aria-label="t('common.save')" :disabled="rowBusy(row)" data-test="save-name" @click="saveName(row, true)"><Icon name="check" size="sm" aria-hidden="true" /></button><button type="button" class="workspace-icon-button" :aria-label="t('common.cancel')" :disabled="rowBusy(row)" data-test="cancel-name" @click="cancelName(row)"><Icon name="x" size="sm" aria-hidden="true" /></button></div>
                      <span v-if="savingId === row.id" class="sr-only" role="status">{{ kw.saving }}</span><p v-if="nameError" :id="`${componentId}-name-error`" class="workspace-name-error" role="alert">{{ nameError }}</p>
                    </div>
                    <div v-else class="workspace-name-display"><button type="button" class="workspace-name-button" :data-key-id="row.id" data-test="rename-key" :title="row.name" :aria-label="`${kw.rename}: ${row.name}`" :disabled="rowBusy(row)" @click.stop="beginName(row)"><slot name="cell-name" :row="row" :value="row.name">{{ row.name }}</slot></button><slot name="name-indicator" :row="row" /></div>
                  </template>
                  <template v-else><span class="workspace-cell-label">{{ column.label }}</span><slot :name="`cell-${column.key}`" :row="row" :value="valueFor(row, column.key)">{{ fallbackValue(row, column) }}</slot></template>
                </td>
                <td class="workspace-expand-cell"><button type="button" class="workspace-icon-button" :data-key-id="row.id" data-test="toggle-details" :aria-label="`${expanded.has(row.id) ? kw.hideDetails : kw.showDetails}: ${row.name}`" :aria-expanded="expanded.has(row.id)" :aria-controls="`${componentId}-key-details-${row.id}`" @click.stop="toggleDetails(row.id)"><Icon :name="expanded.has(row.id) ? 'chevronUp' : 'chevronDown'" size="sm" aria-hidden="true" /></button></td>
              </tr>
              <tr v-if="expanded.has(row.id)" :ref="measureRow" data-document-detail :id="`${componentId}-key-details-${row.id}`" class="key-detail-row" :data-key-id="row.id"><td :colspan="mainColumns.length + 1"><dl v-if="detailColumns.length"><div v-for="column in detailColumns" :key="column.key" :data-column-key="column.key"><dt>{{ column.label }}</dt><dd><slot :name="`cell-${column.key}`" :row="row" :value="valueFor(row, column.key)">{{ fallbackValue(row, column) }}</slot></dd></div></dl><slot name="details-extra" :row="row" /></td></tr>
            </template>
            <tr v-if="paddingBottom" aria-hidden="true"><td :colspan="mainColumns.length + 1" :style="{ height: `${paddingBottom}px`, padding: 0, border: 0 }" /></tr>
          </template>
        </tbody>
      </table>
    </div>
    <div v-if="!loading && !data.length" class="workspace-empty"><slot name="empty">{{ t('common.noData') }}</slot></div>
  </div>
</template>

<style scoped>
.key-workspace-table{min-width:0;color:var(--ritel-fg);font-family:var(--ritel-ui);font-size:14px;line-height:1.5;font-variant-numeric:lining-nums tabular-nums}
.workspace-table-scroll{position:relative;overflow-x:auto;overflow-y:hidden;scrollbar-width:thin;scrollbar-color:var(--ritel-control-border) var(--ritel-bg)}
table{width:100%;min-width:1080px;table-layout:fixed;border-collapse:collapse;background:transparent}
th,td{padding:10px 8px;box-sizing:border-box;text-align:left;vertical-align:middle;border-bottom:1px solid var(--ritel-border)}
th{height:52px;color:var(--ritel-muted);font-size:12px;font-weight:400;white-space:normal}td{min-height:68px;height:68px;overflow-wrap:anywhere}
th:first-child,td:first-child{padding-left:0}th:last-child,td:last-child{padding-right:0}
button,input,select{font:inherit;color:inherit}button{border:0;background:none;cursor:pointer}button:disabled{opacity:.5;cursor:not-allowed}
:is(button,input,select):focus-visible{outline:2px solid var(--ritel-focus);outline-offset:3px}
.key-main-row{cursor:pointer}.key-main-row:hover{background:var(--ritel-hover)}.key-main-row.is-selected{background:var(--ritel-selected)}.key-main-row.is-selected:hover{background:var(--ritel-selected-hover)}
.workspace-select-all{display:inline-flex;align-items:center;justify-content:center;flex:none;width:24px;min-height:28px;padding:0}
.workspace-id-button{display:inline-flex;justify-content:center;align-items:center;gap:8px;min-width:44px;min-height:44px;padding:0 4px;color:var(--ritel-muted);border-radius:6px;white-space:nowrap}
.workspace-id-button:hover,.workspace-id-button[aria-checked=true],.workspace-id-button[aria-checked=mixed]{background:var(--ritel-selected);color:var(--ritel-fg)}
.workspace-id-button[aria-checked=true],.workspace-id-button[aria-checked=mixed]{box-shadow:inset 0 0 0 1px var(--ritel-control-border);font-weight:600}
.workspace-column-heading{display:flex;align-items:center;gap:4px;min-width:0}.workspace-sort-button{display:inline-flex;align-items:center;gap:4px;text-align:left;min-height:36px;padding:0}
.table-column-drag-handle{display:inline-flex;align-items:center;justify-content:center;flex:none;width:20px;min-height:32px;padding:0;color:var(--ritel-muted);cursor:grab;touch-action:none}.table-column-drag-handle:active{cursor:grabbing}
.workspace-name-display{display:flex;align-items:center;gap:6px;min-width:0}.workspace-name-button{display:block;min-width:0;max-width:100%;min-height:44px;padding:0;text-align:left;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.workspace-name-button:hover:not(:disabled){text-decoration:underline;text-underline-offset:4px}
.workspace-name-editor{min-width:0}.workspace-name-controls{display:flex;align-items:center;gap:4px}.workspace-name-input{width:100%;min-width:0;min-height:44px;padding:6px 8px;border:1px solid var(--ritel-control-border);border-radius:var(--ritel-radius-field);background:var(--ritel-surface-inset)}
.workspace-icon-button{display:inline-flex;align-items:center;justify-content:center;flex:none;width:32px;min-height:44px;padding:0;color:var(--ritel-muted)}.workspace-icon-button:hover:not(:disabled){color:var(--ritel-fg)}
.workspace-name-error{margin:6px 0 0;color:var(--ritel-fg);font-size:12px}.workspace-cell-label{display:none}
.key-main-row.is-expanded td{border-bottom-color:transparent}.key-detail-row td{height:auto;padding:20px 16px 24px 60px;background:var(--ritel-surface-inset)}
dl{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:20px 24px;margin:0}dt{margin-bottom:6px;color:var(--ritel-muted);font-size:12px}dd{margin:0;overflow-wrap:anywhere}
.workspace-message-row td{height:160px;text-align:center;color:var(--ritel-muted)}.workspace-empty{padding:48px 20px;text-align:center;color:var(--ritel-muted)}
.workspace-tools{display:flex;align-items:center;justify-content:flex-end;gap:12px;margin-bottom:8px}.workspace-tools>.workspace-id-button{display:none}.workspace-tools>.workspace-sort-control{width:12rem;max-width:100%}
@media(max-width:767px){
  .workspace-tools{justify-content:space-between;flex-wrap:wrap;margin:0 0 12px}.workspace-tools>.workspace-id-button{display:inline-flex}
  .workspace-table-scroll{overflow:visible}table{min-width:0}table,tbody{display:block}colgroup,thead{display:none}
  .key-main-row{display:grid;grid-template-columns:minmax(52px,max-content) minmax(0,1fr) 40px;gap:4px 8px;padding:18px 0;border-bottom:1px solid var(--ritel-border)}
  td,td:first-child,td:last-child{display:block;min-width:0;width:auto;height:auto;min-height:0;padding:0;border:0}
  .cell-id{grid-column:1;grid-row:1}.cell-name{grid-column:2;grid-row:1}.cell-key{grid-column:2;grid-row:2}.cell-group{grid-column:2;grid-row:3}.cell-usage{grid-column:2;grid-row:4}.cell-current_concurrency{grid-column:2;grid-row:5}.cell-status{grid-column:2;grid-row:6}.cell-actions{grid-column:2;grid-row:7}.workspace-expand-cell{grid-column:3;grid-row:1}
  .workspace-cell-label{display:block;margin:8px 0 3px;color:var(--ritel-muted);font-size:12px}.workspace-name-input{font-size:16px}.workspace-name-controls{flex-wrap:wrap}.workspace-name-input{flex:1 0 100%}.workspace-name-button{width:100%;line-height:1.5;white-space:normal;overflow-wrap:anywhere}
  .key-detail-row{display:block}.key-detail-row td{display:block;padding:20px 12px 24px 60px;border-bottom:1px solid var(--ritel-border)}dl{grid-template-columns:minmax(0,1fr);gap:18px}.key-main-row.is-expanded{border-bottom:0}
  .workspace-message-row{display:block}.workspace-message-row td{display:block;padding:36px 20px}
}
.workspace-tools.is-inline { margin: 0; padding: 0; }
</style>
