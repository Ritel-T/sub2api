import { afterEach, describe, expect, it, vi } from 'vitest'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import ModelDistributionChart from '../ModelDistributionChart.vue'
import EndpointDistributionChart from '../EndpointDistributionChart.vue'

import GroupDistributionChart from '../GroupDistributionChart.vue'
import UserBreakdownSubTable from '../UserBreakdownSubTable.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'

const breakdown = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/dashboard', () => ({ getUserBreakdown: breakdown }))

const messages: Record<string, string> = {
  'admin.dashboard.groupDistribution': 'Group Distribution',
  'admin.dashboard.group': 'Group',
  'admin.dashboard.noGroup': 'No Group',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account Cost',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('GroupDistributionChart', () => {
  const groupStats = [
    {
      group_id: 1,
      group_name: 'group-a',
      requests: 9,
      total_tokens: 1200,
      cost: 1.8,
      actual_cost: 0.1,
    },
    {
      group_id: 2,
      group_name: 'group-b',
      requests: 4,
      total_tokens: 600,
      cost: 0.7,
      actual_cost: 0.9,
    },
  ]

  it('uses total_tokens and token ordering by default', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['group-a', 'group-b'])
    expect(chartData.datasets[0].data).toEqual([1200, 600])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('group-a')
    expect(rows[1].text()).toContain('group-b')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'group-a',
      raw: 1200,
      dataset: { data: [1200, 600] },
    })
    expect(label).toBe('group-a: 1.20K (66.7%)')
  })

  it('uses actual_cost and reorders rows in actual cost mode', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
        metric: 'actual_cost',
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['group-b', 'group-a'])
    expect(chartData.datasets[0].data).toEqual([0.9, 0.1])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('group-b')
    expect(rows[1].text()).toContain('group-a')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'group-b',
      raw: 0.9,
      dataset: { data: [0.9, 0.1] },
    })
    expect(label).toBe('group-b: $0.900 (90.0%)')
  })

  it('can hide account cost for user usage stats without account_cost', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
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
})


// Mount the actual DocumentRows and name buttons; only the nested user renderer is isolated.
describe.each([
  { name: 'group', component: GroupDistributionChart, prop: 'groupStats', key: 'group_id', source: undefined },
  { name: 'model', component: ModelDistributionChart, prop: 'modelStats', key: 'model', source: 'requested' },
  { name: 'endpoint', component: EndpointDistributionChart, prop: 'endpointStats', key: 'endpoint', source: 'inbound' },
])('$name distribution record behavior', ({ name, component, prop, key, source }) => {
  let wrapper: VueWrapper | undefined
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals(); breakdown.mockReset() })
  const row = (id: number) => ({
    group_id: id, group_name: `dimension-${id}`, model: `dimension-${id}`, endpoint: `dimension-${id}`,
    requests: id, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0,
    total_tokens: 1000 - id, cost: id, actual_cost: id, account_cost: id,
  })
  const user = (id: number) => ({ user_id: id, email: `user-${id}@example.com`, requests: id, total_tokens: id, actual_cost: id, cost: id })
  function deferred() {
    let resolve!: (value: { users: ReturnType<typeof user>[] }) => void
    let reject!: (error: Error) => void
    const promise = new Promise<{ users: ReturnType<typeof user>[] }>((res, rej) => { resolve = res; reject = rej })
    return { promise, resolve, reject }
  }
  function open(count = 2, attachTo: HTMLElement = document.body) {
    vi.stubGlobal('scrollTo', vi.fn())
    wrapper = mount(component as any, {
      attachTo,
      props: { [prop]: Array.from({ length: count }, (_, index) => row(index + 1)), startDate: '2026-10-01', endDate: '2026-10-24', filters: { user_id: 42 } },
      global: { stubs: {
        LoadingSpinner: true,
        UserBreakdownSubTable: { props: ['items', 'loading'], template: '<div data-test="breakdown" :data-loading="loading">{{ JSON.stringify(items) }}</div>' },
      } },
    })
    return wrapper
  }
  function buttons() { return wrapper!.findAll('tbody > [data-document-row] button') }
  it('uses focusable native name buttons and keeps breakdown beside its owner row', async () => {
    breakdown.mockResolvedValue({ users: [user(7)] })
    const page = open()
    const button = buttons()[0]!
    expect(button.element.tagName).toBe('BUTTON')
    expect(button.attributes('type')).toBe('button')
    expect(button.attributes('aria-expanded')).toBe('false')
    ;(button.element as HTMLButtonElement).focus()
    expect(document.activeElement).toBe(button.element)
    ;(button.element as HTMLButtonElement).click()
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('true')
    const main = button.element.closest('tr')!
    expect(main.nextElementSibling?.hasAttribute('data-document-detail')).toBe(true)
    expect(main.nextElementSibling?.textContent).toContain('user-7@example.com')
    const expected = { user_id: 42, start_date: '2026-10-01', end_date: '2026-10-24',
      ...(name === 'group' ? { group_id: 1 } : name === 'model' ? { model: 'dimension-1', model_source: source } : { endpoint: 'dimension-1', endpoint_type: source }),
    }
    expect(breakdown).toHaveBeenCalledOnce()
    expect(breakdown).toHaveBeenCalledWith(expected)
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(page.find('[data-document-detail]').exists()).toBe(false)
  })
  it.each(['resolve', 'reject'] as const)('ignores late A %s while B owns the spinner and records', async outcome => {
    const a = deferred(), b = deferred()
    breakdown.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const page = open()
    await buttons()[0]!.trigger('click')
    await buttons()[1]!.trigger('click')
    if (outcome === 'resolve') a.resolve({ users: [user(1)] })
    else a.reject(new Error('late A failure'))
    await flushPromises()
    expect(page.get('[data-test="breakdown"]').attributes('data-loading')).toBe('true')
    expect(page.get('[data-test="breakdown"]').text()).not.toContain('user-1@')
    b.resolve({ users: [user(2)] }); await flushPromises()
    expect(page.get('[data-test="breakdown"]').attributes('data-loading')).toBe('false')
    expect(page.get('[data-test="breakdown"]').text()).toContain('user-2@')
  })
  it('keeps a completed B result when A resolves afterwards', async () => {
    const a = deferred(), b = deferred()
    breakdown.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const page = open()
    await buttons()[0]!.trigger('click'); await buttons()[1]!.trigger('click')
    b.resolve({ users: [user(2)] }); await flushPromises()
    a.resolve({ users: [user(1)] }); await flushPromises()
    expect(page.get('[data-test="breakdown"]').text()).toContain('user-2@')
    expect(page.get('[data-test="breakdown"]').text()).not.toContain('user-1@')
  })
  it.each(['filters', 'startDate', 'endDate', ...(source ? ['source'] : [])])('revokes stale requests when %s changes, including A to B to A', async field => {
    const stale = deferred(), current = deferred()
    breakdown.mockReturnValueOnce(stale.promise).mockReturnValueOnce(current.promise)
    const page = open()
    await buttons()[0]!.trigger('click')
    const initial = field === 'filters' ? { user_id: 42 } : field === 'startDate' ? '2026-10-01' : field === 'endDate' ? '2026-10-24' : source
    const next = field === 'filters' ? { user_id: 99 } : field === 'source' ? 'upstream' : '2026-09-01'
    await page.setProps({ [field]: next }); await page.setProps({ [field]: initial })
    expect(page.find('[data-document-detail]').exists()).toBe(false)
    await buttons()[0]!.trigger('click')
    stale.resolve({ users: [user(1)] }); await flushPromises()
    expect(page.get('[data-test="breakdown"]').attributes('data-loading')).toBe('true')
    current.resolve({ users: [user(9)] }); await flushPromises()
    expect(page.get('[data-test="breakdown"]').text()).toContain('user-9@')
  })
  it('keeps generated horizontal overflow CSS off the vertical viewport and binds the outer scroller', async () => {
    const generated = await postcss([tailwindcss({
      content: [{ raw: '<div class="overflow-x-auto overflow-y-hidden">', extension: 'html' }],
      corePlugins: ['overflow'],
    })]).process('@tailwind utilities;', { from: undefined })
    const style = document.createElement('style')
    style.textContent = generated.css
    document.head.appendChild(style)
    const viewport = document.createElement('section')
    viewport.style.height = '600px'
    viewport.style.overflowY = 'auto'
    document.body.appendChild(viewport)
    const listeners = vi.spyOn(HTMLElement.prototype, 'addEventListener')
    try {
      const page = open(350, viewport)
      await flushPromises()
      const horizontal = page.get('.overflow-x-auto')
      expect(horizontal.classes()).toContain('overflow-y-hidden')
      expect(getComputedStyle(horizontal.element).overflowX).toBe('auto')
      expect(getComputedStyle(horizontal.element).overflowY).toBe('hidden')
      expect(getComputedStyle(viewport).overflowY).toBe('auto')
      expect(listeners.mock.calls.some((call, index) => call[0] === 'scroll' && listeners.mock.contexts[index] === viewport)).toBe(true)
      expect(page.findAll('[data-document-row]').length).toBeLessThan(100)
    } finally {
      wrapper?.unmount(); wrapper = undefined
      listeners.mockRestore(); viewport.remove(); style.remove()
    }
  })
  it('windows large record DOM while retaining all chart data and stable dimension keys', async () => {
    const page = open(350)
    await flushPromises()
    const chart = JSON.parse(page.get('.chart-data').text())
    expect(chart.labels).toHaveLength(350)
    expect(chart.datasets[0].data).toHaveLength(350)
    expect(chart.labels[0]).toBe('dimension-1')
    const records = page.findAll('[data-document-row]')
    expect(records.length).toBeGreaterThan(0)
    expect(records.length).toBeLessThan(100)
    expect(page.getComponent({ name: 'DocumentRows' }).props('itemKey')).toBe(key)
    expect(page.find('[data-document-spacer]').exists()).toBe(true)
  })
})


describe('UserBreakdownSubTable record rendering', () => {
  it('windows the full user breakdown while preserving metrics, account-cost visibility and states', async () => {
    const frames = new Map<number, FrameRequestCallback>()
    let nextFrame = 0
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    vi.stubGlobal('scrollTo', vi.fn())
    const host = document.createElement('div')
    host.style.overflowY = 'auto'; host.style.height = '240px'
    Object.defineProperties(host, {
      offsetHeight: { get: () => 240 }, clientHeight: { get: () => 240 },
      offsetWidth: { get: () => 800 }, clientWidth: { get: () => 800 }
    })
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
    document.body.append(host)
    const rectangle = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 800, width: 800, x: 0, y: top, toJSON() {} })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      if (this === host) return rectangle(0, 240)
      const index = this.hasAttribute('data-document-row') ? Number(this.dataset.index) : 0
      return rectangle(index * 28 - host.scrollTop, this.hasAttribute('data-document-row') ? 28 : 5600)
    })
    const settle = async () => {
      for (let pass = 0; pass < 30; pass++) {
        await flushPromises()
        if (!frames.size) return
        const queued = [...frames.values()]; frames.clear(); queued.forEach(callback => callback(0))
      }
      throw new Error('user breakdown layout did not settle')
    }
    const users = Array.from({ length: 200 }, (_, index) => ({
      user_id: 200 - index, email: index ? `user-${200 - index}@test.com` : '', requests: 1234,
      input_tokens: 1000, output_tokens: 200, cache_tokens: 0, total_tokens: 1200,
      actual_cost: 0.1234, account_cost: 1.23, cost: 1234
    }))
    const wrapper = mount(UserBreakdownSubTable, { props: { items: users }, attachTo: host, global: { stubs: { LoadingSpinner: true } } })
    try {
      await settle()
      const records = wrapper.getComponent(DocumentRows)
      expect(records.props('items')).toHaveLength(200)
      expect(records.props('items')).toEqual(users)
      expect(records.props('itemKey')).toBe('user_id')
      expect(wrapper.findAll('[data-document-row]').length).toBeGreaterThan(0)
      expect(wrapper.findAll('[data-document-row]').length).toBeLessThan(40)
      const first = wrapper.get('[data-document-row]')
      expect(first.findAll('td').map(cell => cell.text())).toEqual(['User #200', '1,234', '1.20K', '$0.123', '$1.23', '$1.23K'])
      host.scrollTop = 147 * 28; host.dispatchEvent(new Event('scroll')); await settle()
      expect(wrapper.get('[data-index="147"] td').text()).toBe('user-53@test.com')
      expect(wrapper.get('[data-index="147"] td').attributes('title')).toBe('user-53@test.com')
      await wrapper.setProps({ showAccountCost: false }); await settle()
      expect(wrapper.get('[data-index="147"]').findAll('td').map(cell => cell.text())).toEqual(['user-53@test.com', '1,234', '1.20K', '$0.123', '$1.23K'])
      expect(wrapper.findAll('[data-document-spacer]').every(gap => gap.get('td').attributes('colspan') === '5')).toBe(true)
      expect(wrapper.get('tbody').findAll(':scope > div')).toHaveLength(0)
      await wrapper.setProps({ loading: true }); await settle()
      expect(wrapper.find('loading-spinner-stub').exists()).toBe(true)
      expect(wrapper.find('table').exists()).toBe(false)
      await wrapper.setProps({ loading: false, items: [] }); await settle()
      expect(wrapper.text()).toBe('No data available')
      expect(wrapper.find('table').exists()).toBe(false)
      await wrapper.setProps({ items: users.slice(0, 2), showAccountCost: true }); await settle()
      expect(wrapper.findAll('[data-document-row]')).toHaveLength(2)
      expect(wrapper.get('[data-document-row]').findAll('td')).toHaveLength(6)
      expect(users).toHaveLength(200)
      expect(users[147].user_id).toBe(53)
    } finally {
      wrapper.unmount(); host.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals()
    }
  })
})
