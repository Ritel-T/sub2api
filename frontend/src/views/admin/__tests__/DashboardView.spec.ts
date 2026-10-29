import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('vue-chartjs', () => ({
  Line: { name: 'Line', props: ['data', 'options'], template: '<div class="line-chart" />' }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'en' }
    })
  }
})

enableAutoUnmount(afterEach)

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const mountDashboard = () => mount(DashboardView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      LoadingSpinner: true,
      Icon: true,
      Select: true,
      ModelDistributionChart: true,
      TokenUsageTrend: true,
      Line: true
    }
  }
})

const expectDashboardRange = (startDate: string, endDate: string) => {
  expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
    start_date: startDate,
    end_date: endDate,
    granularity: 'hour'
  }))
  expect(getUserUsageTrend).toHaveBeenCalledWith(expect.objectContaining({
    start_date: startDate,
    end_date: endDate,
    granularity: 'hour'
  }))
  expect(getUserSpendingRanking).toHaveBeenCalledWith(expect.objectContaining({
    start_date: startDate,
    end_date: endDate
  }))
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  afterEach(() => { vi.useRealTimers() })

  it('shows a persistent failure with recovery and prevents duplicate snapshot requests while retrying', async () => {
    getSnapshotV2.mockRejectedValueOnce(new Error('Unavailable'))
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.dashboard.failedToLoad')
    expect(wrapper.findAll('.open-metric')).toHaveLength(0)
    let resolveRetry!: (value: unknown) => void
    getSnapshotV2.mockImplementationOnce(() => new Promise(resolve => { resolveRetry = resolve }))
    await wrapper.get('[data-testid="dashboard-retry"]').trigger('click')
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
    await (wrapper.vm as any).loadDashboardStats()
    expect(getSnapshotV2).toHaveBeenCalledTimes(2)
    resolveRetry({ stats: createDashboardStats(), trend: [], models: [] })
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-load-error"]').exists()).toBe(false)
    expect(wrapper.findAll('.open-metric').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('keeps previous statistics visible when a refresh fails and clears the error after recovery', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    const count = wrapper.findAll('.open-metric').length
    getSnapshotV2.mockRejectedValueOnce(new Error('Unavailable'))
    await (wrapper.vm as any).loadDashboardStats()
    await flushPromises()
    expect(wrapper.get('[data-testid="dashboard-load-error"]').text()).toContain('admin.dashboard.failedToLoad')
    expect(wrapper.findAll('.open-metric')).toHaveLength(count)
    await wrapper.get('[data-testid="dashboard-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-load-error"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows recovery when a statistics snapshot omits its required statistics', async () => {
    getSnapshotV2.mockResolvedValueOnce({ trend: [], models: [] })
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-retry"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('switches metrics and ignores a stale tokens response', async () => {
    let resolveTokens!: (value: any) => void
    getUserUsageTrend.mockImplementationOnce(() => new Promise(resolve => { resolveTokens = resolve }))
    getUserUsageTrend.mockResolvedValueOnce({ trend: [{ date: '2026-01-01', user_id: 2, username: 'spender', email: '', requests: 1, tokens: 10, cost: 5, actual_cost: 5 }] })
    const wrapper = mount(DashboardView, {
      global: { stubs: {
        AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true,
        Icon: true, DateRangePicker: true, Select: true, ModelDistributionChart: true,
        TokenUsageTrend: true
      } }
    })
    await flushPromises()
    expect(getUserUsageTrend).toHaveBeenCalledWith(expect.objectContaining({ metric: 'tokens', limit: 12 }))
    await wrapper.findAll('button').find(button => button.text() === 'admin.dashboard.actualSpending')!.trigger('click')
    expect(getUserUsageTrend).toHaveBeenCalledWith(expect.objectContaining({ metric: 'actual_cost', limit: 12 }))
    await flushPromises()
    const chart = wrapper.findComponent({ name: 'Line' })
    expect(chart.props('data').datasets[0].data).toEqual([5])
    expect(chart.props('options').scales.y.ticks.callback(5)).toBe('$5.00')
    resolveTokens({ trend: [{ date: '2026-01-01', user_id: 1, username: 'tokens', email: '', requests: 1, tokens: 100, cost: 1, actual_cost: 1 }] })
    await flushPromises()
    expect(chart.props('data').datasets[0].data).toEqual([5])
  })

  it('requests an exact rolling 24-hour range initially and opts the picker into rolling ranges', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const now = new Date(2026, 9, 1, 0, 15, 30, 123)
    vi.setSystemTime(now)

    const wrapper = mountDashboard()
    await flushPromises()

    const startDate = new Date(now.getTime() - 86_400_000).toISOString()
    const endDate = now.toISOString()

    expect(Date.parse(endDate) - Date.parse(startDate)).toBe(86_400_000)
    expectDashboardRange(startDate, endDate)
    expect(wrapper.findComponent(DateRangePicker).props('rolling24Hours')).toBe(true)
  })

  it('sends exact timestamps for the dashboard Last 24 Hours preset', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const now = new Date(2026, 9, 1, 0, 15, 30, 123)
    vi.setSystemTime(now)

    const wrapper = mountDashboard()
    await flushPromises()
    getSnapshotV2.mockClear()
    getUserUsageTrend.mockClear()
    getUserSpendingRanking.mockClear()

    await wrapper.get('.date-picker-trigger').trigger('click')
    await wrapper.findAll('.date-picker-preset').find(button => button.text() === 'dates.last24Hours')!.trigger('click')
    await wrapper.get('.date-picker-apply').trigger('click')
    await flushPromises()

    const startDate = new Date(now.getTime() - 86_400_000).toISOString()
    const endDate = now.toISOString()
    expect(Date.parse(endDate) - Date.parse(startDate)).toBe(86_400_000)
    expectDashboardRange(startDate, endDate)
  })

  it('refreshes a rolling Last 24 Hours range from the current clock', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const initialNow = new Date(2026, 9, 1, 0, 15, 30, 123)
    vi.setSystemTime(initialNow)

    const wrapper = mountDashboard()
    await flushPromises()
    getSnapshotV2.mockClear()
    getUserUsageTrend.mockClear()
    getUserSpendingRanking.mockClear()

    const refreshedNow = new Date(initialNow.getTime() + 3 * 60 * 60 * 1000)
    vi.setSystemTime(refreshedNow)
    const refreshButton = wrapper.findAll('button').find(button => button.text() === 'common.refresh')
    expect(refreshButton).toBeDefined()
    await refreshButton!.trigger('click')
    await flushPromises()

    const startDate = new Date(refreshedNow.getTime() - 86_400_000).toISOString()
    const endDate = refreshedNow.toISOString()
    expect(Date.parse(endDate) - Date.parse(startDate)).toBe(86_400_000)
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getUserUsageTrend).toHaveBeenCalledTimes(1)
    expect(getUserSpendingRanking).toHaveBeenCalledTimes(1)
    expectDashboardRange(startDate, endDate)
  })

  it('keeps Today date-only and preserves custom minute ranges', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const now = new Date(2026, 9, 1, 0, 15, 30, 123)
    vi.setSystemTime(now)

    const wrapper = mountDashboard()
    await flushPromises()

    await wrapper.get('.date-picker-trigger').trigger('click')
    await wrapper.findAll('.date-picker-preset').find(button => button.text() === 'dates.today')!.trigger('click')
    await wrapper.get('.date-picker-apply').trigger('click')
    await flushPromises()

    const today = formatLocalDate(now)
    expectDashboardRange(today, today)

    getSnapshotV2.mockClear()
    getUserUsageTrend.mockClear()
    getUserSpendingRanking.mockClear()

    vi.setSystemTime(new Date(2026, 9, 1, 3, 15, 30, 123))
    const todayRefreshButton = wrapper.findAll('button').find(button => button.text() === 'common.refresh')
    expect(todayRefreshButton).toBeDefined()
    await todayRefreshButton!.trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getUserUsageTrend).toHaveBeenCalledTimes(1)
    expect(getUserSpendingRanking).toHaveBeenCalledTimes(1)
    expectDashboardRange(today, today)

    getSnapshotV2.mockClear()
    getUserUsageTrend.mockClear()
    getUserSpendingRanking.mockClear()

    await wrapper.get('.date-picker-trigger').trigger('click')
    const inputs = wrapper.findAll<HTMLInputElement>('input[type="datetime-local"]')
    await inputs[0].setValue('2026-09-28T00:00')
    await inputs[1].setValue('2026-09-29T00:00')
    await wrapper.get('.date-picker-apply').trigger('click')
    await flushPromises()

    expectDashboardRange(new Date('2026-09-28T00:00').toISOString(), new Date('2026-09-29T00:00').toISOString())

    getSnapshotV2.mockClear()
    getUserUsageTrend.mockClear()
    getUserSpendingRanking.mockClear()

    vi.setSystemTime(new Date(2026, 9, 1, 6, 15, 30, 123))
    const refreshButton = wrapper.findAll('button').find(button => button.text() === 'common.refresh')
    expect(refreshButton).toBeDefined()
    await refreshButton!.trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getUserUsageTrend).toHaveBeenCalledTimes(1)
    expect(getUserSpendingRanking).toHaveBeenCalledTimes(1)
    expectDashboardRange(new Date('2026-09-28T00:00').toISOString(), new Date('2026-09-29T00:00').toISOString())
  })
})
