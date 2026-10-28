import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
enableAutoUnmount(afterEach)
import { defineComponent, ref, reactive } from 'vue'

import UsageView from '../UsageView.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import UsageCleanupDialog from '@/components/admin/usage/UsageCleanupDialog.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'

vi.mock('@/components/admin/ConsoleObjectLinks.vue', () => ({ default: { template: '<nav />' } }))

const { routerPush, list, exportList, getStats, getSnapshotV2, getById, getModelStats, listErrorLogs, routeQuery, aoaToSheet, sheetAddAoa, saveAs, xlsxWrite, listCleanupTasks, createCleanupTask, showWarning } = vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: vi.fn(() => null),
    setItem: vi.fn(),
    removeItem: vi.fn(),
  })

  return {
    routerPush: vi.fn(),
    listCleanupTasks: vi.fn(),
    createCleanupTask: vi.fn(),
    showWarning: vi.fn(),
    list: vi.fn(),
		exportList: vi.fn(),
    getStats: vi.fn(),
    getSnapshotV2: vi.fn(),
    getById: vi.fn(),
    getModelStats: vi.fn(),
    listErrorLogs: vi.fn(),
    routeQuery: {} as Record<string, string>,
		aoaToSheet: vi.fn(() => ({})),
		sheetAddAoa: vi.fn(),
		saveAs: vi.fn(),
		xlsxWrite: vi.fn(() => new Uint8Array([1, 2, 3])),
  }
})

const own = vi.hoisted(() => ({
  list: vi.fn(), getStats: vi.fn(), getModelStats: vi.fn(), getSnapshotV2: vi.fn(), listErrors: vi.fn(),
  settings: { allow_user_view_error_requests: true },
}))
vi.mock('@/api/observerUsage', () => ({ observerUsageAPI: own }))

const messages: Record<string, string> = {
  'admin.dashboard.timeRange': 'Time Range',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
  'admin.usage.failedToLoadUser': 'Failed to load user',
	'admin.usage.requestId': 'Request ID',
	'admin.usage.upstreamRequestId': 'Upstream ID',
	'usage.requestedModel': 'Requested model',
	'usage.sentUpstreamModel': 'Sent upstream model',
	'usage.upstreamResponseModel': 'Upstream response model',
	'usage.upstreamModelMismatch': 'Upstream model mismatch',
	'common.yes': 'Yes',
	'common.no': 'No',
}


vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: {
      list,
      getStats,
    },
    dashboard: {
      getSnapshotV2,
      getModelStats,
    },
    users: {
      getById,
    },
  },
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: {
    listCleanupTasks, createCleanupTask,
		list: exportList,
  },
}))

vi.mock('file-saver', () => ({ saveAs }))

vi.mock('xlsx', () => ({
	utils: {
		aoa_to_sheet: aoaToSheet,
		sheet_add_aoa: sheetAddAoa,
		book_new: vi.fn(() => ({})),
		book_append_sheet: vi.fn(),
	},
	write: xlsxWrite,
}))

vi.mock('@/api/admin/ops', () => ({
  listErrorLogs,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: own.settings,
    showError: vi.fn(),
    showWarning,
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/utils/format', () => ({
  formatReasoningEffort: (value: string | null | undefined) => value ?? '-',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
      locale: ref('en'),
    }),
  }
})

const mockRouteState = reactive({ path: '/admin/usage', query: routeQuery })
const normalizeQuery = (query: Record<string, unknown>) => Object.fromEntries(Object.entries(query).filter(([, value]) => value != null).map(([key, value]) => [key, String(value)]))
vi.mock('vue-router', () => ({
  useRoute: () => mockRouteState,
  useRouter: () => ({
    push: routerPush,
    resolve: ({ path, query }: { path: string; query: Record<string, unknown> }) => {
      const normalized = normalizeQuery(query)
      return { query: normalized, href: `${path}?${new URLSearchParams(normalized)}` }
    },
  }),
}))
beforeEach(() => {
  Object.keys(routeQuery).forEach(key => delete routeQuery[key])
  mockRouteState.query = routeQuery
  routerPush.mockReset().mockImplementation(({ query }) => { mockRouteState.query = normalizeQuery(query); return Promise.resolve() })
})
const showAnalysis = async (wrapper: ReturnType<typeof mount>) => {
  (wrapper.vm as any).switchPage('analysis')
  await flushPromises()
}

const AppLayoutStub = { template: '<div><slot /></div>' }
const UsageFiltersStub = defineComponent({
  props: { showRecordActions: { type: Boolean, default: true } },
  emits: ['cleanup', 'update:modelValue', 'change', 'export'],
  setup(_, { expose }) {
    const userKeyword = ref('')
    let userSearchRevision = 0
    const setUserKeyword = (email: string) => {
      userSearchRevision += 1
      userKeyword.value = email
    }
    expose({
      getUserSearchRevision: () => userSearchRevision,
      setUserKeyword,
      simulateUserInput: setUserKeyword,
    })
    return { userKeyword }
  },
  template: '<div><slot name="before-filters" /><button v-if="showRecordActions" data-test="cleanup-open" @click="$emit(\'cleanup\')">cleanup</button><span data-test="user-filter-label">{{ userKeyword }}</span><slot name="after-reset" /></div>',
})
const UsageTableStub = {
  props: ['columns'],
  emits: ['userClick', 'column-reorder'],
  template: '<div data-test="usage-table"><button class="user-click" @click="$emit(\'userClick\', 2)">user</button></div>',
}
const UsageConnectionsStub = { props: ['filters', 'refreshKey'], template: '<div data-test="connection-tab" />' }
const UserTokenRankingStub = {
  emits: ['select-user'],
  template: '<div data-test="ranking"><button class="pick-user" @click="$emit(\'select-user\', 5, \'rank@test.com\')">pick</button></div>',
}
const ModelDistributionChartStub = {
  props: ['metric'],
  emits: ['update:metric'],
  template: `
    <div data-test="model-chart">
      <span class="metric">{{ metric }}</span>
      <button class="switch-metric" @click="$emit('update:metric', 'actual_cost')">switch</button>
    </div>
  `,
}
const GroupDistributionChartStub = {
  props: ['metric'],
  emits: ['update:metric'],
  template: `
    <div data-test="group-chart">
      <span class="metric">{{ metric }}</span>
      <button class="switch-metric" @click="$emit('update:metric', 'actual_cost')">switch</button>
    </div>
  `,
}

const mountRouteFilteredUsageView = (realDatePicker = false, realCleanupDialog = false) => mount(UsageView, {
  global: { stubs: {
    AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
    UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: !realCleanupDialog,
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
    ConfirmDialog: { props: ['show'], emits: ['confirm'], template: '<button v-if="show" data-test="cleanup-confirm" @click="$emit(\'confirm\')">confirm</button>' },
    UserBalanceHistoryModal: true, Pagination: true, Select: true,
    DateRangePicker: !realDatePicker, Icon: true, TokenUsageTrend: true,
    ModelDistributionChart: true, GroupDistributionChart: true,
    EndpointDistributionChart: true, UserTokenRanking: true,
  } },
})

describe('admin UsageView route filters', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.keys(routeQuery).forEach((key) => delete routeQuery[key])
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({
      total_requests: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_tokens: 0,
      total_tokens: 0,
      total_cost: 0,
      total_actual_cost: 0,
      average_duration_ms: 0,
    })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
    getById.mockReset()
  })

  afterEach(() => {
    Object.keys(routeQuery).forEach((key) => delete routeQuery[key])
    vi.useRealTimers()
  })

  it('preserves a historical 24-hour deep link when refreshing', async () => {
    routeQuery.start_date = '2026-09-08T05:12:00Z'
    routeQuery.end_date = '2026-09-09T05:12:00Z'
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    vi.advanceTimersByTime(120000)
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({
      start_date: routeQuery.start_date, end_date: routeQuery.end_date
    }), expect.anything())
    wrapper.unmount()
  })

  it('advances the rolling preset on refresh, but keeps manual times fixed', async () => {
    vi.setSystemTime(new Date('2026-09-10T05:12:59Z'))
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    vi.setSystemTime(new Date('2026-09-10T05:15:10Z'))
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({
      start_date: '2026-09-09T05:15:10.000Z', end_date: '2026-09-10T05:15:10.000Z'
    }), expect.anything())
    wrapper.findComponent({ name: 'DateRangePicker' }).vm.$emit('change', {
      startDate: '2026-09-08T04:37:00.000Z', endDate: '2026-09-09T04:37:00.000Z', preset: null
    })
    await flushPromises()
    vi.advanceTimersByTime(120000)
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({
      start_date: '2026-09-08T04:37:00.000Z', end_date: '2026-09-09T04:37:00.000Z'
    }), expect.anything())
    wrapper.unmount()
  })

  it('scopes upstream request records, statistics and chart requests to the exact account', async () => {
    routeQuery.account_id = '84'
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    vi.advanceTimersByTime(120)
    await flushPromises()
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ account_id: 84 }), expect.anything())
    expect(getStats).not.toHaveBeenCalled()
    await showAnalysis(wrapper)
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ account_id: 84 }))
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ account_id: 84 }))
    wrapper.unmount()
  })

  it('separates record and analytics reads while preserving every shared filter in navigable URLs and browser back', async () => {
    Object.assign(routeQuery, { view: 'records', tab: 'usage', user_id: '12', api_key_id: '13', account_id: '14', group_id: '15',
      model: 'gpt-6-astra', request_id: 'request-123', request_type: 'ws_v2', native_compaction_v2: 'false', billing_type: '0',
      billing_mode: 'token', upstream_model_mismatch: 'true', start_date: '2026-10-25T12:34:00Z', end_date: '2026-10-26T12:34:00Z' })
    const expected = { user_id: 12, api_key_id: 13, account_id: 14, group_id: 15, model: 'gpt-6-astra', request_id: 'request-123',
      request_type: 'ws_v2', native_compaction_v2: false, billing_type: 0, billing_mode: 'token', upstream_model_mismatch: true,
      start_date: routeQuery.start_date, end_date: routeQuery.end_date }
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe('Request records')
    expect(wrapper.findAll('.usage-page-header')).toHaveLength(1)
    expect(wrapper.get('.usage-page-header').findAll('h1')).toHaveLength(1)
    expect(wrapper.get('.usage-page-header').find('.usage-page-links').exists()).toBe(true)
    expect(wrapper.get('.usage-page').classes()).not.toContain('space-y-6')
    expect(wrapper.findComponent(UsageFiltersStub).props('showRecordActions')).toBe(true)
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining(expected), expect.anything())
    for (const fn of [getStats, getModelStats, getSnapshotV2]) expect(fn).not.toHaveBeenCalled()
    expect(wrapper.findComponent(UsageStatsCards).exists()).toBe(false)
    const analysisLink = wrapper.get('.usage-page-links a:first-child')
    expect(analysisLink.attributes('href')).toContain('billing_mode=token')
    expect(analysisLink.attributes('href')).toContain('request_id=request-123')
    list.mockClear()
    await analysisLink.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe('Usage analytics')
    expect(wrapper.findComponent(UsageFiltersStub).props('showRecordActions')).toBe(false)
    expect(wrapper.find('[data-test="cleanup-open"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="usage-detail-tab"]')[0].text()).toBe('Overview')
    expect(list).not.toHaveBeenCalled()
    for (const fn of [getStats, getModelStats, getSnapshotV2]) expect(fn).toHaveBeenLastCalledWith(expect.objectContaining(expected))
    expect(wrapper.find('usage-table-stub').exists()).toBe(false)
    expect(mockRouteState.query).toMatchObject({ ...routeQuery, view: 'analysis', tab: 'analysis' })
    const analyticsLocation = { ...mockRouteState.query }
    for (const fn of [getStats, getModelStats, getSnapshotV2]) fn.mockClear()
    await wrapper.get('.usage-page-links a:last-child').trigger('click')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining(expected), expect.anything())
    for (const fn of [getStats, getModelStats, getSnapshotV2]) expect(fn).not.toHaveBeenCalled()
    list.mockClear()
    mockRouteState.query = analyticsLocation
    await flushPromises()
    expect(list).not.toHaveBeenCalled()
    expect((wrapper.vm as any).usagePage).toBe('analysis')
    expect(getSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining(expected))
  })

  it('clears former-scope costs and charts when new filter requests fail', async () => {
    routeQuery.view = 'analysis'
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => {})
    const oldTrend = [{ date: '2026-10-27', total_tokens: 100 }]
    const oldGroups = [{ group_id: 1, name: 'Former scope', actual_cost: 99 }]
    getStats.mockResolvedValueOnce({ total_requests: 3, total_actual_cost: 99 })
    getSnapshotV2.mockResolvedValueOnce({ trend: oldTrend, groups: oldGroups })
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    expect(wrapper.findComponent(UsageStatsCards).props('stats')).toMatchObject({ total_actual_cost: 99 })
    expect((wrapper.vm as any).trendData).toEqual(oldTrend)
    expect((wrapper.vm as any).groupStats).toEqual(oldGroups)
    getStats.mockRejectedValueOnce(new Error('new-scope stats failed'))
    getSnapshotV2.mockRejectedValueOnce(new Error('new-scope charts failed'))
    ;(wrapper.vm as any).filters.group_id = 2
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()
    expect(getStats).toHaveBeenLastCalledWith(expect.objectContaining({ group_id: 2 }))
    expect(getSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ group_id: 2 }))
    expect(wrapper.findComponent(UsageStatsCards).exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('Usage data could not be loaded.')
    expect(wrapper.get('[role="alert"] button').text()).toBe('common.refresh')
    expect((wrapper.vm as any).trendData).toEqual([])
    expect((wrapper.vm as any).groupStats).toEqual([])
    expect((wrapper.vm as any).inboundEndpointStats).toEqual([])
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findComponent(UsageStatsCards).exists()).toBe(true)
    errorLog.mockRestore()
  })

  it('distinguishes failed charts from an authoritative empty chart response', async () => {
    routeQuery.view = 'analysis'
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => {})
    getSnapshotV2.mockRejectedValueOnce(new Error('charts unavailable'))
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    expect(wrapper.findComponent(UsageStatsCards).exists()).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('Usage data could not be loaded.')
    getSnapshotV2.mockResolvedValueOnce({ trend: [], groups: [] })
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect((wrapper.vm as any).trendData).toEqual([])
    expect((wrapper.vm as any).groupStats).toEqual([])
    errorLog.mockRestore()
  })

  it('does not present the previous scope or zero totals while new totals are pending', async () => {
    routeQuery.view = 'analysis'
    getStats.mockResolvedValueOnce({ total_requests: 4, total_actual_cost: 123 })
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    expect(wrapper.findComponent(UsageStatsCards).props('stats')).toMatchObject({ total_actual_cost: 123 })
    let finishStats!: (result: unknown) => void
    getStats.mockReturnValueOnce(new Promise(resolve => { finishStats = resolve }))
    ;(wrapper.vm as any).filters.group_id = 2
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()
    expect(wrapper.findComponent(UsageStatsCards).exists()).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toBe('Loading usage totals…')
    finishStats({ total_requests: 1, total_actual_cost: 0 })
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.findComponent(UsageStatsCards).props('stats')).toMatchObject({ total_actual_cost: 0 })
  })

  it('does not let an older rejected chart request clear newer scoped results', async () => {
    routeQuery.view = 'analysis'
    let rejectOld!: (reason: unknown) => void
    getSnapshotV2.mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    const currentTrend = [{ date: '2026-10-28', total_tokens: 200 }]
    const currentGroups = [{ group_id: 2, name: 'Current scope' }]
    getSnapshotV2.mockResolvedValueOnce({ trend: currentTrend, groups: currentGroups })
    ;(wrapper.vm as any).filters.group_id = 2
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()
    rejectOld(new Error('late old-scope error'))
    await flushPromises()
    expect((wrapper.vm as any).trendData).toEqual(currentTrend)
    expect((wrapper.vm as any).groupStats).toEqual(currentGroups)
  })

  it('does not fetch records on a direct analytics URL and rejects late analytics after leaving', async () => {
    routeQuery.view = 'analysis'
    let finishModels!: (result: unknown) => void
    getModelStats.mockReturnValueOnce(new Promise(resolve => { finishModels = resolve }))
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    expect(list).not.toHaveBeenCalled()
    expect(getSnapshotV2).toHaveBeenCalledOnce()
    await wrapper.get('.usage-page-links a:last-child').trigger('click')
    await flushPromises()
    finishModels({ models: [{ model: 'outdated', total_tokens: 10 }] })
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([])
    expect(list).toHaveBeenCalledOnce()
    expect(wrapper.findComponent(UsageStatsCards).exists()).toBe(false)
  })

  it('ignores malformed object identifiers rather than silently narrowing to a different ID', async () => {
    routeQuery.user_id = '42.5'
    routeQuery.account_id = '-1'
    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ user_id: undefined, account_id: undefined }), expect.anything())
    wrapper.unmount()
  })

  it('shows the routed user while applying user_id to usage requests', async () => {
    routeQuery.user_id = '42'
    getById.mockResolvedValue({ id: 42, email: 'route-user@test.com' })

    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()

    expect(getById).toHaveBeenCalledWith(42, true)
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ user_id: 42 }), expect.anything())
    expect(wrapper.find('[data-test="user-filter-label"]').text()).toBe('route-user@test.com')
  })

  it('does not apply a stale routed user label after user_id changes', async () => {
    routeQuery.user_id = '42'
    let resolveLookup!: (user: { id: number; email: string }) => void
    getById.mockReturnValue(new Promise((resolve) => { resolveLookup = resolve }))

    const wrapper = mountRouteFilteredUsageView()
    await wrapper.vm.$nextTick()
    ;(wrapper.vm as any).filters.user_id = 84
    ;(wrapper.findComponent(UsageFiltersStub).vm as any).setUserKeyword('current-user@test.com')

    resolveLookup({ id: 42, email: 'stale-user@test.com' })
    await flushPromises()

    expect(wrapper.find('[data-test="user-filter-label"]').text()).toBe('current-user@test.com')
  })

  it('does not overwrite newer user input when the routed user lookup succeeds', async () => {
    routeQuery.user_id = '42'
    let resolveLookup!: (user: { id: number; email: string }) => void
    getById.mockReturnValue(new Promise((resolve) => { resolveLookup = resolve }))

    const wrapper = mountRouteFilteredUsageView()
    await wrapper.vm.$nextTick()
    ;(wrapper.findComponent(UsageFiltersStub).vm as any).simulateUserInput('new-search@test.com')

    resolveLookup({ id: 42, email: 'route-user@test.com' })
    await flushPromises()

    expect((wrapper.vm as any).filters.user_id).toBe(42)
    expect(wrapper.find('[data-test="user-filter-label"]').text()).toBe('new-search@test.com')
  })

  it('does not overwrite newer user input when the routed user lookup fails', async () => {
    routeQuery.user_id = '42'
    let rejectLookup!: (error: Error) => void
    getById.mockReturnValue(new Promise((_, reject) => { rejectLookup = reject }))

    const wrapper = mountRouteFilteredUsageView()
    await wrapper.vm.$nextTick()
    ;(wrapper.findComponent(UsageFiltersStub).vm as any).simulateUserInput('new-search@test.com')

    rejectLookup(new Error('lookup failed'))
    await flushPromises()

    expect((wrapper.vm as any).filters.user_id).toBe(42)
    expect(wrapper.find('[data-test="user-filter-label"]').text()).toBe('new-search@test.com')
  })

  it('shows the routed user ID when its label lookup fails', async () => {
    routeQuery.user_id = '42'
    getById.mockRejectedValue(new Error('lookup failed'))

    const wrapper = mountRouteFilteredUsageView()
    await flushPromises()

    expect(list).toHaveBeenCalledWith(expect.objectContaining({ user_id: 42 }), expect.anything())
    expect(wrapper.find('[data-test="user-filter-label"]').text()).toBe('42')
  })
})

describe('admin UsageView native compaction filter', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({
      total_requests: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_tokens: 0,
      total_tokens: 0,
      total_cost: 0,
      total_actual_cost: 0,
      average_duration_ms: 0,
    })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('propagates the filter to list/stats/model/snapshot requests and clears it on reset', async () => {
    const wrapper = mountRouteFilteredUsageView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    list.mockClear()
    getStats.mockClear()
    getModelStats.mockClear()
    getSnapshotV2.mockClear()

    ;(wrapper.vm as any).filters.native_compaction_v2 = true
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()

    expect((wrapper.vm as any).breakdownFilters.native_compaction_v2).toBe(true)
    expect(list).toHaveBeenCalledWith(
      expect.objectContaining({ native_compaction_v2: true }),
      expect.anything()
    )
    expect(getStats).not.toHaveBeenCalled()
    await showAnalysis(wrapper)
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))
    expect(getModelStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))

    list.mockClear()
    getStats.mockClear()
    getModelStats.mockClear()
    getSnapshotV2.mockClear()

    ;(wrapper.vm as any).resetFilters()
    await flushPromises()

    expect((wrapper.vm as any).filters.native_compaction_v2).toBeNull()
    expect((wrapper.vm as any).breakdownFilters).not.toHaveProperty('native_compaction_v2')
    expect(list).not.toHaveBeenCalled()
    ;(wrapper.vm as any).switchPage('records')
    await flushPromises()
    expect(list).toHaveBeenCalledWith(
      expect.objectContaining({ native_compaction_v2: null }),
      expect.anything()
    )
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
    expect(getModelStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
  })
})

describe('admin UsageView distribution metric toggles', () => {
  beforeEach(() => {
    routeQuery.view = 'analysis'
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getById.mockReset()
    getModelStats.mockReset()

    list.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
    getStats.mockResolvedValue({
      total_requests: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_tokens: 0,
      total_tokens: 0,
      total_cost: 0,
      total_actual_cost: 0,
      average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({
      trend: [],
      models: [],
      groups: [],
    })
    getModelStats.mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps previous model stats visible during refresh until new data arrives', async () => {
    // 首次加载返回 A
    getModelStats.mockResolvedValueOnce({ models: [{ model: 'A', total_tokens: 10 }] })

    const wrapper = mount(UsageView, {
      global: { stubs: {
        AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
        UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
        UserBalanceHistoryModal: true, AuditLogModal: true, Pagination: true, Select: true,
        DateRangePicker: true, Icon: true, TokenUsageTrend: true,
        ModelDistributionChart: ModelDistributionChartStub, GroupDistributionChart: GroupDistributionChartStub,
        EndpointDistributionChart: true, UserTokenRanking: true,
      } },
    })
    vi.advanceTimersByTime(120)
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([{ model: 'A', total_tokens: 10 }])

    // 刷新:让第二次 getModelStats 处于 pending,断言旧数据 A 仍在(不被清空成 [])
    let resolveSecond: (v: any) => void = () => {}
    getModelStats.mockReturnValueOnce(new Promise((res) => { resolveSecond = res }))
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([{ model: 'A', total_tokens: 10 }])

    // 新数据到达后替换为 B
    resolveSecond({ models: [{ model: 'B', total_tokens: 20 }] })
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([{ model: 'B', total_tokens: 20 }])
  })

  it('keeps model and group metric toggles independent without refetching chart data', async () => {
    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: true,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          UserBalanceHistoryModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: ModelDistributionChartStub,
          GroupDistributionChart: GroupDistributionChartStub,
          UserTokenRanking: true,
        },
      },
    })

    vi.advanceTimersByTime(120)
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    const now = new Date()
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: expect.stringContaining('T'),
      end_date: expect.stringContaining('T'),
      granularity: 'hour'
    }))

    const range = vi.mocked(getSnapshotV2).mock.calls[0]![0]!
    expect(new Date(range.end_date!).getTime() - new Date(range.start_date!).getTime()).toBe(86400000)
    expect(new Date(range.end_date!).getTime()).toBeGreaterThanOrEqual(now.getTime() - 1000)
    expect(new Date(range.end_date!).getTime()).toBeLessThanOrEqual(now.getTime() + 60000)

    const modelChart = wrapper.find('[data-test="model-chart"]')
    const groupChart = wrapper.find('[data-test="group-chart"]')

    expect(modelChart.find('.metric').text()).toBe('tokens')
    expect(groupChart.find('.metric').text()).toBe('tokens')

    await modelChart.find('.switch-metric').trigger('click')
    await flushPromises()

    expect(modelChart.find('.metric').text()).toBe('actual_cost')
    expect(groupChart.find('.metric').text()).toBe('tokens')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)

    await groupChart.find('.switch-metric').trigger('click')
    await flushPromises()

    expect(modelChart.find('.metric').text()).toBe('actual_cost')
    expect(groupChart.find('.metric').text()).toBe('actual_cost')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
  })
})

describe('admin UsageView column preferences', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(localStorage.getItem).mockReset().mockReturnValue(null)
    vi.mocked(localStorage.setItem).mockReset()
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('retains every column and moves request IDs between main rows and details', async () => {
    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: UsageTableStub,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          UserBalanceHistoryModal: true,
          AuditLogModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: true,
          GroupDistributionChart: true,
          EndpointDistributionChart: true,
          UserTokenRanking: true,
        },
      },
    })
    await wrapper.vm.$nextTick()

    const usageTable = wrapper.findComponent(UsageTableStub)
    expect(usageTable.props('columns').map((column: { key: string }) => column.key)).toEqual([
      'created_at', 'user', 'api_key', 'account', 'model', 'tokens', 'cost', 'latency',
      'group', 'reasoning_effort', 'endpoint', 'stream', 'billing_mode', 'ip_address',
      'request_id', 'upstream_request_id', 'user_agent',
    ])
    const requestIdColumn = () => usageTable.props('columns').find((column: { key: string }) => column.key === 'request_id')
    expect(requestIdColumn().primary).toBe(false)
    wrapper.findComponent(ColumnSettings).vm.$emit('toggle', 'request_id')
    await wrapper.vm.$nextTick()
    expect(requestIdColumn().primary).toBe(true)
    wrapper.findComponent(ColumnSettings).vm.$emit('toggle', 'request_id')
    await wrapper.vm.$nextTick()
    expect(requestIdColumn().primary).toBe(false)
    expect(usageTable.props('columns')).toHaveLength(17)
    expect(localStorage.setItem).toHaveBeenCalledWith('table-columns:admin-usage:v1', expect.any(String))
  })

  it('supports reordering all columns including the required time and user columns', async () => {
    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: UsageTableStub,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          UserBalanceHistoryModal: true,
          AuditLogModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: true,
          GroupDistributionChart: true,
          EndpointDistributionChart: true,
          UserTokenRanking: true,
        },
      },
    })
    await wrapper.vm.$nextTick()

    const usageTable = wrapper.findComponent(UsageTableStub)
    const originalKeys = usageTable.props('columns').map((column: { key: string }) => column.key)
    const reordered = [...originalKeys].reverse()
    usageTable.vm.$emit('column-reorder', reordered)
    await wrapper.vm.$nextTick()
    expect(usageTable.props('columns').map((column: { key: string }) => column.key)).toEqual(reordered)
    wrapper.findComponent(ColumnSettings).vm.$emit('reset')
    await wrapper.vm.$nextTick()
    expect(usageTable.props('columns').map((column: { key: string }) => column.key)).toEqual(originalKeys)
  })
})

describe('admin UsageView handleUserClick', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getById.mockReset()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('opens user via include_deleted when clicking a usage row user', async () => {
    getById.mockResolvedValue({ id: 2, email: 'd@test.com', deleted_at: '2026-05-28T00:00:00Z' })

    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: UsageTableStub,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          UserBalanceHistoryModal: true,
          AuditLogModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: true,
          GroupDistributionChart: true,
          EndpointDistributionChart: true,
          UserTokenRanking: true,
        },
      },
    })

    vi.advanceTimersByTime(120)
    await flushPromises()

    await wrapper.find('[data-test="usage-table"] .user-click').trigger('click')
    await flushPromises()

    expect(getById).toHaveBeenCalledWith(2, true)
  })
})

describe('admin UsageView errors tab filter forwarding', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getModelStats.mockReset()
    listErrorLogs.mockReset()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockResolvedValue({ models: [] })
    listErrorLogs.mockResolvedValue({ items: [], total: 0, pages: 0 })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('forwards model/account_id/group_id to listErrorLogs on the errors tab', async () => {
    const wrapper = mount(UsageView, {
      global: { stubs: {
        AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
        UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
        UserBalanceHistoryModal: true, AuditLogModal: true, Pagination: true, Select: true,
        DateRangePicker: true, Icon: true, TokenUsageTrend: true,
        ModelDistributionChart: true, GroupDistributionChart: true, EndpointDistributionChart: true,
        UserTokenRanking: true, OpsErrorLogTable: true, OpsErrorDetailModal: true,
      } },
    })
    vi.advanceTimersByTime(120)
    await flushPromises()

    // 模拟用户在过滤器里选择了模型/账户/分组
    const vm = wrapper.vm as any
    vm.filters.model = 'gpt-5.3-codex'
    vm.filters.account_id = 7
    vm.filters.group_id = 3
    await flushPromises()

    // 切换到「错误请求」标签（第二个 tab 按钮）触发 loadAdminErrors
    const tabs = wrapper.findAll('[data-testid="usage-detail-tab"]')
    await tabs[1].trigger('click')
    await flushPromises()

    expect(listErrorLogs).toHaveBeenCalledWith(expect.objectContaining({
      view: 'all',
      model: 'gpt-5.3-codex',
      account_id: 7,
      group_id: 3,
    }))
    wrapper.findComponent({ name: 'DateRangePicker' }).vm.$emit('change', {
      startDate: '2026-09-10', endDate: '2026-09-10', preset: 'today'
    })
    await flushPromises()
    expect(listErrorLogs).toHaveBeenLastCalledWith(expect.objectContaining({
      start_time: new Date('2026-09-10T00:00:00').toISOString(),
      end_time: new Date('2026-09-11T00:00:00').toISOString(),
    }))
    wrapper.findComponent({ name: 'DateRangePicker' }).vm.$emit('change', {
      startDate: '2026-09-10T05:37:00Z', endDate: '2026-09-10T06:42:00Z', preset: null
    })
    await flushPromises()
    expect(listErrorLogs).toHaveBeenLastCalledWith(expect.objectContaining({
      start_time: '2026-09-10T05:37:00.000Z', end_time: '2026-09-10T06:42:00.000Z',
    }))
    wrapper.unmount()
  })
})

describe('admin UsageView ranking tab', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getModelStats.mockReset()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('mounts ranking lazily and drill-down sets user filter then jumps back to usage tab', async () => {
    const wrapper = mount(UsageView, {
      global: { stubs: {
        AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
        UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
        UserBalanceHistoryModal: true, Pagination: true, Select: true,
        DateRangePicker: true, Icon: true, TokenUsageTrend: true,
        ModelDistributionChart: true, GroupDistributionChart: true, EndpointDistributionChart: true,
        UserTokenRanking: UserTokenRankingStub,
        UsageConnections: UsageConnectionsStub, OpsErrorLogTable: true, OpsErrorDetailModal: true,
      } },
    })
    vi.advanceTimersByTime(120)
    await flushPromises()

    // 懒挂载:切到排行 tab 前不渲染
    expect(wrapper.find('[data-test="ranking"]').exists()).toBe(false)

    const tabs = wrapper.findAll('[data-testid="usage-detail-tab"]')
    expect(tabs).toHaveLength(4)
    expect(wrapper.find('[data-test="connection-tab"]').exists()).toBe(false)
    await tabs[2].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="connection-tab"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ranking"]').exists()).toBe(false)
    await showAnalysis(wrapper)
    const rankingTab = wrapper.findAll('[data-testid="usage-detail-tab"]').find(tab => tab.text() === 'usage.tabs.ranking')!
    await rankingTab.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="ranking"]').exists()).toBe(true)

    // 下钻:设置 user_id、切回用量明细 tab 并按新筛选重新拉取列表
    list.mockClear()
    await wrapper.find('[data-test="ranking"] .pick-user').trigger('click')
    await flushPromises()

    expect((wrapper.vm as any).activeTab).toBe('usage')
    expect((wrapper.vm as any).filters.user_id).toBe(5)
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ user_id: 5 }), expect.anything())
  })
})

describe('admin UsageView model audit export', () => {
	beforeEach(() => {
		vi.useFakeTimers()
		list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
		exportList.mockReset().mockResolvedValue({
			items: [{
				id: 1,
				created_at: '2026-08-04T00:00:00Z',
				model: 'gpt-5.6-sol',
				upstream_model: 'gpt-5.5',
				upstream_response_model: 'gpt-5.4',
				upstream_model_mismatch: true,
				request_type: 'sync',
				input_tokens: 1,
				output_tokens: 1,
				cache_read_tokens: 0,
				cache_creation_tokens: 0,
				duration_ms: 10,
			}],
			total: 1,
			pages: 1,
		})
		getStats.mockReset().mockResolvedValue({
			total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
			total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
		})
		getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
		getModelStats.mockReset().mockResolvedValue({ models: [] })
		aoaToSheet.mockClear()
		sheetAddAoa.mockClear()
		saveAs.mockClear()
		xlsxWrite.mockClear()
	})

	afterEach(() => {
		vi.useRealTimers()
	})

	it('exports requested, sent, response, and mismatch as separate admin columns', async () => {
		const wrapper = mountRouteFilteredUsageView()
		vi.advanceTimersByTime(120)
		await flushPromises()
		;(wrapper.vm as any).filters.native_compaction_v2 = true

		await (wrapper.vm as any).exportToExcel()
		await flushPromises()

		expect(exportList).toHaveBeenCalledWith(
			expect.objectContaining({ native_compaction_v2: true }),
			expect.anything()
		)

		const headers = aoaToSheet.mock.calls[0][0][0]
		expect(headers.slice(4, 8)).toEqual([
			'Requested model',
			'Sent upstream model',
			'Upstream response model',
			'Upstream model mismatch',
		])
		const row = sheetAddAoa.mock.calls[0][1][0]
		expect(row.slice(4, 8)).toEqual(['gpt-5.6-sol', 'gpt-5.5', 'gpt-5.4', 'Yes'])
		const cacheRateIndex = headers.indexOf('usage.latencyCacheRate')
		expect(cacheRateIndex).toBe(headers.indexOf('admin.usage.cacheCreationTokens') + 1)
		expect(row[cacheRateIndex]).toBe('0%')
		expect(saveAs).toHaveBeenCalledTimes(1)
	})
})


describe('observer own usage view', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    Object.keys(routeQuery).forEach(key => delete routeQuery[key])
    own.settings.allow_user_view_error_requests = true
    own.list.mockResolvedValue({ items: [], total: 0 })
    own.getStats.mockResolvedValue({ total_requests: 2, total_account_cost: 3 })
    own.getModelStats.mockResolvedValue({ models: [] })
    own.getSnapshotV2.mockResolvedValue({ trend: [], groups: [] })
    own.listErrors.mockResolvedValue({ items: [], total: 0 })
  })
  afterEach(() => { vi.useRealTimers() })
  const mountObserver = () => mount(UsageView, {
    props: { observerMode: true },
    global: { stubs: {
      AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
      UsageTable: UsageTableStub, UsageExportProgress: true, UsageCleanupDialog: true,
      UserBalanceHistoryModal: true, Pagination: true, Select: true, DateRangePicker: true,
      Icon: true, TokenUsageTrend: true, ModelDistributionChart: true, GroupDistributionChart: true,
      EndpointDistributionChart: true, UserTokenRanking: true, OpsErrorLogTable: true, OpsErrorDetailModal: true,
    } },
  })
  it('uses own APIs for filters, charts and export, without admin actions', async () => {
    routeQuery.user_id = '999'
    const wrapper = mountObserver()
    await vi.advanceTimersByTimeAsync(150)
    await flushPromises()
    expect(own.list).toHaveBeenCalledWith(expect.objectContaining({ user_id: undefined }), expect.anything())
    expect(own.getStats).not.toHaveBeenCalled()
    await showAnalysis(wrapper)
    expect(own.getStats).toHaveBeenCalled()
    expect(own.getModelStats).toHaveBeenCalled()
    expect(own.getSnapshotV2).toHaveBeenCalled()
    expect(wrapper.find('usage-cleanup-dialog-stub').exists()).toBe(false)
    expect(wrapper.find('user-balance-history-modal-stub').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('usage.tabs.ranking')
    expect(wrapper.text()).not.toContain('connections.title')
    for (const name of ['model-distribution-chart-stub', 'group-distribution-chart-stub', 'endpoint-distribution-chart-stub']) {
      expect(wrapper.findComponent(name).props('enableBreakdown')).toBe(false)
    }
    (wrapper.vm as any).switchPage('records')
    await flushPromises()
    await wrapper.find('.user-click').trigger('click')
    const filter = wrapper.findComponent(UsageFiltersStub)
    filter.vm.$emit('update:modelValue', { account_id: 7, group_id: 8, api_key_id: 9, model: 'gpt', start_date: '2026-09-01', end_date: '2026-09-02' })
    filter.vm.$emit('change')
    await flushPromises()
    expect(own.list).toHaveBeenLastCalledWith(expect.objectContaining({ account_id: 7, group_id: 8, api_key_id: 9 }), expect.anything())
    filter.vm.$emit('cleanup')
    filter.vm.$emit('export')
    await flushPromises()
    expect(own.list).toHaveBeenLastCalledWith(expect.objectContaining({ page_size: 100, exact_total: true, account_id: 7 }), expect.anything())
    expect(saveAs).toHaveBeenCalled()
    const errorTab = wrapper.findAll('[data-testid="usage-detail-tab"]').find(tab => tab.text() === 'usage.tabs.errors')!
    await errorTab.trigger('click')
    await flushPromises()
    expect(own.listErrors).toHaveBeenCalledWith(expect.objectContaining({ start_date: '2026-09-01', account_id: 7, group_id: 8 }))
    for (const fn of [list, exportList, getStats, getModelStats, getSnapshotV2, getById, listErrorLogs]) expect(fn).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('honors the user error visibility switch', async () => {
    own.settings.allow_user_view_error_requests = false
    const wrapper = mountObserver()
    await vi.advanceTimersByTimeAsync(150)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="usage-detail-tab"]')).toHaveLength(1)
    expect(own.listErrors).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('admin UsageView rolling 24 hour requests', () => {
  let wrapper: ReturnType<typeof mountRouteFilteredUsageView> | undefined
  const now = new Date('2026-10-01T00:01:02.123+09:00')

  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
    Object.keys(routeQuery).forEach(key => delete routeQuery[key])
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    exportList.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({ total_requests: 2, total_cost: 12, total_actual_cost: 3, total_account_cost: 5 })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], groups: [] })
    listErrorLogs.mockReset().mockResolvedValue({ items: [], total: 0 })
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    Object.keys(routeQuery).forEach(key => delete routeQuery[key])
    vi.useRealTimers()
  })

  const expectRequests = (end: Date) => {
    const range = { start_date: new Date(end.getTime() - 86_400_000).toISOString(), end_date: end.toISOString() }
    for (const request of [list, getStats, getModelStats, getSnapshotV2]) {
      expect(request.mock.lastCall?.[0]).toMatchObject(range)
      const params = request.mock.lastCall![0]
      expect(Date.parse(params.end_date) - Date.parse(params.start_date)).toBe(86_400_000)
    }
    expect(getSnapshotV2.mock.lastCall?.[0].granularity).toBe('hour')
    return range
  }

  it.each([
    ['rolling', undefined, undefined],
    ['routed exact', '2026-09-30T00:00:00Z', '2026-10-01T00:00:00Z'],
    ['mixed', '2026-09-30', '2026-10-01T00:00:00Z'],
  ])('blocks %s cleanup until calendar dates are explicitly selected', async (_, start, end) => {
    if (start) routeQuery.start_date = start
    if (end) routeQuery.end_date = end
    listCleanupTasks.mockReset().mockResolvedValue({ items: [], total: 0 })
    createCleanupTask.mockReset()
    showWarning.mockClear()
    wrapper = mountRouteFilteredUsageView(true, true)
    await flushPromises()
    await wrapper.get('[data-test="cleanup-open"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('button').some(b => b.text() === 'admin.usage.cleanup.submit')).toBe(false)
    expect(wrapper.find('[data-test="cleanup-confirm"]').exists()).toBe(false)
    expect(listCleanupTasks).not.toHaveBeenCalled()
    expect(createCleanupTask).not.toHaveBeenCalled()
    expect(showWarning).toHaveBeenCalledWith('admin.usage.cleanup.calendarRangeRequired')
  })

  it('submits unchanged natural dates after an explicit picker selection', async () => {
    listCleanupTasks.mockReset().mockResolvedValue({ items: [], total: 0 })
    createCleanupTask.mockReset().mockResolvedValue({ id: 1 })
    wrapper = mountRouteFilteredUsageView(true, true)
    await flushPromises()
    await wrapper.get('[data-test="cleanup-open"]').trigger('click')
    const picker = wrapper.findComponent(DateRangePicker)
    await picker.get('.date-picker-trigger').trigger('click')
    await picker.findAll('.date-picker-preset').find(b => b.text() === 'dates.today')!.trigger('click')
    await picker.get('.date-picker-apply').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="cleanup-open"]').trigger('click')
    await flushPromises()
    const dialog = wrapper.findComponent(UsageCleanupDialog)
    const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    expect(dialog.props()).toMatchObject({ show: true, startDate: today, endDate: today })
    await dialog.findAll('button').find(b => b.text() === 'admin.usage.cleanup.submit')!.trigger('click')
    await wrapper.get('[data-test="cleanup-confirm"]').trigger('click')
    await flushPromises()
    expect(createCleanupTask).toHaveBeenCalledOnce()
    expect(createCleanupTask).toHaveBeenCalledWith({ start_date: today, end_date: today, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone })
  })

  it('uses the same precise cross-midnight window for list, cards and charts on initial load', async () => {
    wrapper = mountRouteFilteredUsageView(true)
    vi.advanceTimersByTime(120)
    await flushPromises()
    await showAnalysis(wrapper!)
    expectRequests(now)
    expect(wrapper.findComponent(DateRangePicker).get('.date-picker-trigger').text()).toContain('dates.last24Hours')
    expect(wrapper.findComponent(UsageStatsCards).props('stats')).toMatchObject({
      total_actual_cost: 3, total_cost: 12, total_account_cost: 5
    })
  })

  it('refreshes the rolling window once and keeps pagination/export on that applied snapshot', async () => {
    wrapper = mountRouteFilteredUsageView()
    await flushPromises()
    const refreshedAt = new Date('2026-10-02T00:00:00.456+09:00')
    vi.setSystemTime(refreshedAt)
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    await showAnalysis(wrapper!)
    const range = expectRequests(refreshedAt)
    vi.setSystemTime(new Date('2026-10-02T00:05:00.789+09:00'))
    expect((wrapper.vm as any).buildUsageListParams(2, 50, false)).toMatchObject({ ...range, page: 2 })
    await (wrapper.vm as any).exportToExcel()
    expect(exportList.mock.lastCall?.[0]).toMatchObject(range)
    expect((wrapper.vm as any).connectionFilters).toMatchObject({ start_time: range.start_date, end_time: range.end_date })
    ;(wrapper.vm as any).switchPage('records')
    ;(wrapper.vm as any).switchTab('errors')
    await flushPromises()
    expect(listErrorLogs.mock.lastCall?.[0]).toMatchObject({ start_time: range.start_date, end_time: range.end_date })
  })

  it('applies the actual picker selection after midnight and resets back to rolling time', async () => {
    routeQuery.start_date = '2026-09-20'
    routeQuery.end_date = '2026-09-25'
    wrapper = mountRouteFilteredUsageView(true)
    await flushPromises()
    const picker = wrapper.findComponent(DateRangePicker)
    await picker.get('.date-picker-trigger').trigger('click')
    await picker.findAll('.date-picker-preset').find(b => b.text() === 'dates.last24Hours')!.trigger('click')
    await picker.get('.date-picker-apply').trigger('click')
    await flushPromises()
    await showAnalysis(wrapper!)
    expectRequests(now)
    const resetAt = new Date('2026-10-02T00:00:00.789+09:00')
    vi.setSystemTime(resetAt)
    ;(wrapper.vm as any).resetFilters()
    await flushPromises()
    ;(wrapper.vm as any).switchPage('records')
    await flushPromises()
    await showAnalysis(wrapper!)
    expectRequests(resetAt)
  })

  it('preserves manual/routed calendar dates on refresh', async () => {
    routeQuery.start_date = '2026-09-30'
    routeQuery.end_date = '2026-10-01'
    wrapper = mountRouteFilteredUsageView(true)
    await flushPromises()
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    await showAnalysis(wrapper!)
    for (const request of [list, getStats, getModelStats, getSnapshotV2]) {
      expect(request.mock.lastCall?.[0]).toMatchObject({ start_date: '2026-09-30', end_date: '2026-10-01' })
    }
    expect(wrapper.findComponent(DateRangePicker).get('.date-picker-trigger').text()).not.toContain('dates.last24Hours')
  })
})
