import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { useInfiniteList } from '@/composables/useInfiniteList'
import { useAuthStore } from './auth'
import { listQualityPlans, listQualityOperations, listQualityTemplates, listQualityPresets, type QualityOperation, type QualityRuleTemplate, type QualityRulePreset } from '@/api/admin/accountQuality'
import { getAllIncludingInactive } from '@/api/admin/groups'
import type { AdminGroup, ScheduledTestPlan } from '@/types'

// Session-only summaries survive route navigation. Answer bodies stay in the detail drawer.
export const useAccountQualityStore = defineStore('accountQuality', () => {
  const auth = useAuthStore()
  const plans = ref<ScheduledTestPlan[]>([]), groups = ref<AdminGroup[]>([]), templates = ref<QualityRuleTemplate[]>([])
  const presets = ref<QualityRulePreset[]>([]), presetsLoading = ref(false), presetsError = ref('')
  const cursor = ref(0)
  const operationCursors = new Map<number, number>([[1, 0]])
  const operationList = useInfiniteList<QualityOperation>({
    keyOf: operation => operation.id,
    fetchPage: async (page, signal) => {
      const result = await listQualityOperations(operationCursors.get(page) ?? 0)
      if (!signal.aborted) operationCursors.set(page + 1, result.next_cursor)
      return { items: result.items, pages: result.next_cursor ? Number.MAX_SAFE_INTEGER : page }
    }
  })
  const operations = operationList.items
  const rulesLoaded = ref(false), operationsLoaded = ref(false)
  const rulesLoading = ref(false)
  const operationsLoading = computed(() => operationList.loading.value || operationList.refreshing.value)
  const moreLoading = operationList.loadingMore
  const rulesError = ref(''), groupsError = ref('')
  const operationsError = computed(() => operationList.error.value ? errorText(operationList.error.value) : '')
  const updatedAt = ref(0)
  const search = ref(''), selectedPlanId = ref<number | null>(null), operationFilter = ref('all')
  let ruleVersion = 0, operationVersion = 0, groupVersion = 0, presetVersion = 0
  let rulesFlight: Promise<void> | null = null, groupsFlight: Promise<void> | null = null
  const errorText = (e: unknown) => e instanceof Error ? e.message : 'qualityOps.error'

  function reset() {
    ruleVersion++; operationVersion++; groupVersion++; presetVersion++
    plans.value = []; groups.value = []; templates.value = []; cursor.value = 0
    operationList.clear(); operationCursors.clear(); operationCursors.set(1, 0)
    presets.value = []; presetsLoading.value = false; presetsError.value = ''
    rulesLoaded.value = operationsLoaded.value = false
    rulesLoading.value = false
    rulesError.value = groupsError.value = ''
    search.value = ''; selectedPlanId.value = null; operationFilter.value = 'all'; updatedAt.value = 0
    rulesFlight = groupsFlight = null
  }
  async function refreshPresets() {
    const version = ++presetVersion
    presetsLoading.value = true; presetsError.value = ''
    try {
      const data = await listQualityPresets()
      if (version === presetVersion) presets.value = data
    } catch (e) { if (version === presetVersion) presetsError.value = errorText(e) }
    finally { if (version === presetVersion) presetsLoading.value = false }
  }
  watch(() => auth.user ? `${auth.user.id}:${auth.user.role}` : '', reset, { flush: 'sync' })

  function refreshRules(force = false): Promise<void> {
    if (rulesFlight && !force) return rulesFlight
    const version = ++ruleVersion
    rulesLoading.value = true; rulesError.value = ''
    const flight = (async () => {
      try {
        // 分组规则和它建出的账号规则一起刷新，卡片上的覆盖数和规则列表不会对不上。
        const [data, groupRules] = await Promise.all([listQualityPlans(), listQualityTemplates()])
        if (version !== ruleVersion) return
        plans.value = data; templates.value = groupRules; rulesLoaded.value = true
        if (selectedPlanId.value && !data.some(p => p.id === selectedPlanId.value)) selectedPlanId.value = null
        updatedAt.value = Date.now()
      } catch (e) { if (version === ruleVersion) rulesError.value = errorText(e) }
      finally { if (version === ruleVersion) { rulesLoading.value = false; rulesFlight = null } }
    })()
    rulesFlight = flight
    return flight
  }
  function refreshGroups(): Promise<void> {
    if (groupsFlight) return groupsFlight
    const version = ++groupVersion
    groupsError.value = ''
    const flight = (async () => {
      try { const data = await getAllIncludingInactive(); if (version === groupVersion) groups.value = data }
      catch (e) { if (version === groupVersion) groupsError.value = errorText(e) }
      finally { if (version === groupVersion) groupsFlight = null }
    })()
    groupsFlight = flight
    return flight
  }
  async function refreshOperations(append = false, retry = false) {
    if (append && (moreLoading.value || operationsLoading.value || !cursor.value)) return
    const version = ++operationVersion
    await (retry ? operationList.retry() : append ? operationList.loadMore() : operationList.refresh())
    if (version !== operationVersion || operationList.error.value) return
    cursor.value = operationCursors.get(operationList.page.value + 1) ?? 0
    operationsLoaded.value = true; updatedAt.value = Date.now()
  }
  function retryOperations() { return refreshOperations(false, true) }

  async function refresh() {
    await Promise.all([refreshRules(), refreshGroups(), refreshOperations(), refreshPresets()])
  }
  return { plans, groups, templates, operations, cursor, rulesLoaded, operationsLoaded, rulesLoading, operationsLoading, moreLoading,
    rulesError, operationsError, groupsError, updatedAt, search, selectedPlanId, operationFilter,
    presets, presetsLoading, presetsError, refreshPresets,
    refresh, refreshRules, refreshGroups, refreshOperations, retryOperations, reset }
})
