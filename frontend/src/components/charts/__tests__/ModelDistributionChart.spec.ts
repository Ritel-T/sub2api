import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import ModelDistributionChart from '../ModelDistributionChart.vue'

const messages: Record<string, string> = {
  'admin.dashboard.modelDistribution': 'Model Distribution',
  'admin.dashboard.spendingRankingTitle': 'User Spending Ranking',
  'admin.dashboard.viewModelDistribution': 'Model Distribution',
  'admin.dashboard.viewSpendingRanking': 'User Spending Ranking',
  'admin.dashboard.spendingRankingUser': 'User',
  'admin.dashboard.spendingRankingRequests': 'Requests',
  'admin.dashboard.spendingRankingTokens': 'Tokens',
  'admin.dashboard.spendingRankingSpend': 'Spend',
  'admin.dashboard.spendingRankingOther': 'Others',
  'admin.dashboard.model': 'Model',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account Cost',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
  'admin.redeem.userPrefix': 'User #{id}',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const message = messages[key] ?? key
        return params
          ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name]))
          : message
      },
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('ModelDistributionChart', () => {
  const modelStats = [
    {
      model: 'model-a',
      requests: 8,
      input_tokens: 100,
      output_tokens: 50,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 1000,
      cost: 1.5,
      actual_cost: 0.2,
    },
    {
      model: 'model-b',
      requests: 3,
      input_tokens: 40,
      output_tokens: 20,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 500,
      cost: 0.5,
      actual_cost: 1.4,
    },
  ]

  it('uses total_tokens and token ordering by default', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['model-a', 'model-b'])
    expect(chartData.datasets[0].data).toEqual([1000, 500])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('model-a')
    expect(rows[1].text()).toContain('model-b')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'model-a',
      raw: 1000,
      dataset: { data: [1000, 500] },
    })
    expect(label).toBe('model-a: 1.00K (66.7%)')
  })

  it('collapses long model lists without changing chart totals, and expands on demand', async () => {
    const wrapper = mount(ModelDistributionChart, { props: { modelStats, collapsedRows: 1 } })
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(JSON.parse(wrapper.get('.chart-data').text()).datasets[0].data).toEqual([1000, 500])
    const toggle = wrapper.get('[data-testid="model-list-toggle"]')
    expect(toggle.text()).toContain('+1')
    await toggle.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    await toggle.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  })

  it('bounds dashboard spending ranking rows without changing chart totals', async () => {
    const rankingItems = Array.from({ length: 8 }, (_, index) => ({ user_id: index + 1, email: `user${index}@example.com`, username: `user${index}`, actual_cost: 10, requests: 2, tokens: 100 }))
    const wrapper = mount(ModelDistributionChart, { props: { modelStats: [], enableRankingView: true,
      rankingItems, rankingTotalActualCost: 80, rankingTotalRequests: 16, rankingTotalTokens: 800, collapsedRows: 3 } })
    await wrapper.findAll('button').find(button => button.text() === 'User Spending Ranking')!.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    expect(JSON.parse(wrapper.get('.chart-data').text()).labels).toHaveLength(8)
    const toggle = wrapper.get('[data-testid="ranking-list-toggle"]')
    await toggle.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(8)
    await toggle.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
  })

  it('uses actual_cost and reorders rows in actual cost mode', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        metric: 'actual_cost',
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['model-b', 'model-a'])
    expect(chartData.datasets[0].data).toEqual([1.4, 0.2])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('model-b')
    expect(rows[1].text()).toContain('model-a')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'model-b',
      raw: 1.4,
      dataset: { data: [1.4, 0.2] },
    })
    expect(label).toBe('model-b: $1.40 (87.5%)')
  })

  it('can hide account cost for user usage stats without account_cost', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        showAccountCost: false,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Account Cost')
    expect(wrapper.findAll('thead th')).toHaveLength(5)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(5)
  })

  it('uses the dashboard user label policy and renders Others with a dedicated chart color', async () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats: [],
        enableRankingView: true,
        rankingItems: [
          { user_id: 1, email: 'alpha@example.com', username: 'alpha', actual_cost: 12, requests: 10, tokens: 1000 },
          { user_id: 2, email: 'beta@example.com', username: '   ', actual_cost: 8, requests: 6, tokens: 600 },
          { user_id: 3, email: '   ', username: '', actual_cost: 0, requests: 0, tokens: 0 },
        ],
        rankingTotalActualCost: 30,
        rankingTotalRequests: 20,
        rankingTotalTokens: 2000,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const rankingButton = wrapper.findAll('button').find((button) => button.text() === 'User Spending Ranking')
    expect(rankingButton).toBeTruthy()
    await rankingButton!.trigger('click')

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual([
      '#1 alpha',
      '#2 beta@example.com',
      '#3 User #3',
      'Others',
    ])
    expect(chartData.datasets[0].data).toEqual([12, 8, 0, 10])
    expect(chartData.datasets[0].backgroundColor[0]).toBe('#3b82f6')
    expect(chartData.datasets[0].backgroundColor[3]).toBe('#94a3b8')
    expect(chartData.datasets[0].backgroundColor[3]).not.toBe(chartData.datasets[0].backgroundColor[0])

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(4)
    expect(rows[0].text()).toContain('alpha')
    expect(rows[0].text()).not.toContain('alpha@example.com')
    expect(rows[1].text()).toContain('beta@example.com')
    expect(rows[2].text()).toContain('User #3')
    expect(rows[3].text()).toContain('Others')
    expect(rows[3].text()).toContain('4')
    expect(rows[3].text()).toContain('400')
    expect(rows[3].text()).toContain('$10.00')
  })
})


it('selects a ranked user through its native name button and leaves Others unselectable', async () => {
  const user = { user_id: 1, email: 'alpha@example.com', username: 'alpha', actual_cost: 12, requests: 10, tokens: 1000 }
  const wrapper = mount(ModelDistributionChart, { attachTo: document.body, props: {
    modelStats: [], enableRankingView: true, rankingItems: [user], rankingTotalActualCost: 20,
  } })
  try {
    await wrapper.findAll('button').find(button => button.text() === 'User Spending Ranking')!.trigger('click')
    expect(wrapper.get('.overflow-x-auto').classes()).toContain('overflow-y-hidden')
    const button = wrapper.get('tbody button')
    expect(button.element.tagName).toBe('BUTTON')
    expect(button.attributes('type')).toBe('button')
    ;(button.element as HTMLButtonElement).focus()
    expect(document.activeElement).toBe(button.element)
    ;(button.element as HTMLButtonElement).click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('ranking-click')).toEqual([[user]])
    const rows = wrapper.findAll('[data-document-row]')
    expect(rows[1]!.text()).toContain('Others')
    expect(rows[1]!.find('button').exists()).toBe(false)
  } finally { wrapper.unmount() }
})
