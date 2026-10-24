import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ControlledExperimentsView from '../ControlledExperimentsView.vue'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'
import type { ExperimentReport, ExperimentRun } from '@/api/admin/controlledExperiments'

const { api, accounts } = vi.hoisted(() => ({
  api: { catalog: vi.fn(), list: vi.fn(), create: vi.fn(), report: vi.fn(), start: vi.fn(), stop: vi.fn() }, accounts: vi.fn()
}))
vi.mock('@/api/admin/controlledExperiments', () => ({ controlledExperimentsAPI: api }))
vi.mock('@/api/admin/accounts', () => ({ list: accounts, default: { list: accounts } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, values?: object) => values ? `${key} ${JSON.stringify(values)}` : key, te: () => true }) }))

const tasks = [
  { id: 'screen-tools-01', family: 'tools.fixture', split: 'screen', category: 'tools', prompt: 'Read the fixture', max_turns: 3 },
  { id: 'screen-constraints-01', family: 'constraints.test', split: 'screen', category: 'constraints', prompt: 'Solve it', max_turns: 1 },
  { id: 'confirm-tools-01', family: 'tools.confirm', split: 'confirm', category: 'tools', prompt: 'Confirm it', max_turns: 3 }
]
function run(status = 'draft'): ExperimentRun {
  return { id: 8, name: 'Frozen comparison', status, max_calls: 5, reserved_calls: 0, stop_reason: '', created_at: '2026-10-08T00:00:00Z', spec: { suite_version: 'fenjue-v1-20261007', model: 'gpt-6.1-sol', reasoning_effort: 'high', repetitions: 1, timeout_seconds: 720, planned_max_calls: 5, tasks: tasks.slice(0, 2) as ExperimentRun['spec']['tasks'], routes: [{ account_id: 2, account_name: 'native', channel: 'native_http', mapped_model: 'gpt-6.1-sol' }] } }
}
function report(status = 'draft'): ExperimentReport {
  return { run: run(status), attempts: [], preflight: [], comparisons: [], routes: [{ route_index: 0, eligibility: 'untested', calls: 0, completed_calls: 0, protocol_failures: 0, unknown_calls: 0, graded_tasks: 0, passed_tasks: 0, mean_score: null, cost_usd: 0, cost_incomplete: false }] }
}
const mountView = (attachTo?: HTMLElement) => mount(ControlledExperimentsView, { attachTo, global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, SmartOpsNav: true } } })
let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  vi.useFakeTimers()
  for (const fn of Object.values(api)) fn.mockReset()
  api.catalog.mockResolvedValue({ version: 'v1', seed: 20261007, tasks })
  api.list.mockResolvedValue([])
  api.create.mockResolvedValue(run())
  api.report.mockResolvedValue(report())
  api.start.mockResolvedValue(undefined)
  api.stop.mockResolvedValue(undefined)
  accounts.mockReset().mockResolvedValue({ items: [{ id: 2, name: 'native' }, { id: 23, name: 'Prism shadow' }], total: 2 })
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('ControlledExperimentsView', () => {
  it('keeps the loaded history on append failure and refreshes every loaded cursor', async () => {
    const first = Array.from({ length: 30 }, (_, index) => ({ ...run(), id: 100 - index, name: `Run ${100 - index}` }))
    const last = { ...run(), id: 70, name: 'Last run' }
    api.list.mockResolvedValueOnce(first).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce([last])
      .mockResolvedValueOnce(first).mockResolvedValueOnce([last])
    wrapper = mountView(); await flushPromises()
    const sentinel = wrapper.getComponent(InfiniteListSentinel)
    sentinel.vm.$emit('load-more'); await flushPromises()
    expect(wrapper.text()).toContain('Run 99')
    expect(sentinel.props('error')).toBeTruthy()
    sentinel.vm.$emit('retry'); await flushPromises()
    expect(wrapper.text()).toContain('Last run')
    expect(api.list.mock.calls.slice(0, 3).map(call => call[0])).toEqual([0, 71, 71])
    await wrapper.findAll('button').find(button => button.text() === 'controlledExperiments.refresh')!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('Last run')
    expect(api.list.mock.calls.slice(3).map(call => call[0])).toEqual([0, 71])
    expect(api.start).not.toHaveBeenCalled()
  })

  it('keeps a configuration failure visible and retries it on refresh', async () => {
    api.catalog.mockRejectedValueOnce(new Error('Task catalog unavailable'))
    wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('Task catalog unavailable')
    expect(wrapper.get('[data-testid="experiment-create"]').attributes('disabled')).toBeDefined()
    await wrapper.findAll('button').find(button => button.text() === 'controlledExperiments.refresh')!.trigger('click')
    await flushPromises()
    expect(api.catalog).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('Task catalog unavailable')
    await wrapper.get('[data-testid="experiment-account"]').setValue('2')
    expect(wrapper.get('[data-testid="experiment-create"]').attributes('disabled')).toBeUndefined()
  })

  it('shows the frozen credential parent, proxy and mapped model', async () => {
    const data = report()
    data.run.spec.routes[0] = { account_id: 23, account_name: 'shadow', channel: 'prism', parent_account_id: 4, proxy_id: 7, mapped_model: 'gpt-6-luna' }
    api.list.mockResolvedValue([data.run]); api.report.mockResolvedValue(data)
    wrapper = mountView(); await flushPromises()
    const frozen = wrapper.get('[data-testid="experiment-frozen-route"]').text()
    expect(frozen).toContain('gpt-6-luna')
    expect(frozen).toContain('controlledExperiments.parentAccount: 4')
    expect(frozen).toContain('controlledExperiments.proxy: 7')
  })

  it('requires an account and saves an immutable draft without starting requests', async () => {
    wrapper = mountView(); await flushPromises()
    expect(wrapper.get('[data-testid="experiment-create"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="experiment-planned"]').text()).toContain('"count":5')
    await wrapper.get('[data-testid="experiment-account"]').setValue('2')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ routes: [{ account_id: 2, channel: 'native_http' }], task_ids: ['screen-tools-01', 'screen-constraints-01'], max_calls: 50 }))
    expect(api.start).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="experiment-start"]').text()).toContain('"count":5')
  })

  it('explains WS prerequisites and distinguishes a skipped channel from a submitted test', async () => {
    const data = report('completed')
    data.run.spec.routes[0].channel = 'native_ws'
    data.preflight = [{ route_index: 0, available: false, reason: 'websocket_not_enabled', catalog: 'unknown' }]
    data.routes[0].eligibility = 'preflight_blocked'
    api.list.mockResolvedValue([data.run]); api.report.mockResolvedValue(data)
    wrapper = mountView(); await flushPromises()
    const channel = wrapper.findAll('select').find(select => select.find('option[value="native_ws"]').exists())!
    await channel.setValue('native_ws')
    expect(wrapper.get('[data-testid="experiment-ws-prerequisite"]').text()).toContain('controlledExperiments.wsPrerequisiteHint')
    const blocked = wrapper.get('[data-testid="experiment-route-blocked"]').text()
    expect(blocked).toContain('controlledExperiments.routeNotExecuted')
    expect(blocked).toContain('controlledExperiments.wsPrerequisiteHint')
    expect(wrapper.text()).toContain('controlledExperiments.preflightReason.websocket_not_enabled')
    expect(wrapper.get('[data-testid="experiment-score"]').text()).toBe('—')
    expect(api.start).not.toHaveBeenCalled()
  })

  it('switches split tasks and displays a budget below the complete plan', async () => {
    wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="experiment-split"]').setValue('confirm')
    await wrapper.get('[data-testid="experiment-budget"]').setValue('1')
    expect(wrapper.text()).toContain('controlledExperiments.limited')
    expect(wrapper.get('[data-testid="experiment-planned"]').text()).toContain('"count":4')
    expect(wrapper.findAll('input[type="checkbox"]').filter(input => (input.element as HTMLInputElement).checked)).toHaveLength(1)
  })

  it('starts only on the explicit action and polls until the run ends', async () => {
    api.list.mockResolvedValue([run()])
    wrapper = mountView(); await flushPromises()
    expect(api.start).not.toHaveBeenCalled()
    api.report.mockResolvedValue(report('running'))
    await wrapper.get('[data-testid="experiment-start"]').trigger('click'); await flushPromises()
    expect(api.start).toHaveBeenCalledOnce(); expect(api.start).toHaveBeenCalledWith(8)
    api.report.mockResolvedValue(report('completed'))
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    const count = api.report.mock.calls.length
    await vi.advanceTimersByTimeAsync(6000)
    expect(api.report.mock.calls.length).toBe(count)
  })

  it('keeps protocol failures unscored and renders provider answers as text', async () => {
    const data = report('completed')
    data.routes[0] = { ...data.routes[0], eligibility: 'failed', calls: 1, unknown_calls: 1, cost_incomplete: true }
    data.attempts = [{ sequence: 1, route_index: 0, phase: 'task', task_id: 'screen-tools-01', repetition: 1, turn: 1, status: 'unknown', started_at: '', answer: '<img src="x" onerror="alert(1)">', diagnostic: { code: 'missing_terminal', http_status: 200, actual_channel: 'native_http', upstream_endpoint: '/v1/responses', upstream_model: 'gpt-6.1-sol', response_model: '', effective_effort: 'high', effort_evidence: 'request_sent', terminal: '', identity_stable: true, duration_ms: 1000, submissions: 1, output_types: [], usage_source: 'unavailable', usage: { input_tokens: 0, output_tokens: 0 }, cost_incomplete: true } }]
    api.list.mockResolvedValue([run('completed')]); api.report.mockResolvedValue(data)
    wrapper = mountView(); await flushPromises()
    expect(wrapper.get('[data-testid="experiment-score"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="experiment-attempt"]').text()).toContain('controlledExperiments.noScore')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<img src="x" onerror="alert(1)">')
    expect(api.start).not.toHaveBeenCalled()
  })

  it('windows 220 attempts while retaining sequence, diagnostics, trace, prompt and focused details', async () => {
    const data = report('completed')
    data.run.max_calls = 220
    data.run.reserved_calls = 220
    data.attempts = Array.from({ length: 220 }, (_, index) => ({
      sequence: index + 1, route_index: 0, phase: 'task', task_id: 'screen-tools-01', repetition: 1,
      turn: index % 3 + 1, status: 'completed', started_at: '2026-10-24T00:00:00Z',
      answer: `Answer ${index + 1} <img src="x" onerror="alert(1)">`,
      grade: { passed: true, score: 1, reason: `Grade ${index + 1}` },
      diagnostic: {
        code: `diagnostic-${index + 1}`, http_status: 200, actual_channel: 'native_http',
        upstream_endpoint: '/v1/responses', upstream_model: 'gpt-6.1-sol', response_model: 'gpt-6.1-sol',
        effective_effort: 'high', effort_evidence: 'request_sent', terminal: 'response.completed',
        identity_stable: true, duration_ms: 1000, submissions: 1, output_types: ['message'],
        usage_source: 'response', usage: { input_tokens: 123, output_tokens: 456 },
        cost_usd: 0.012345, cost_incomplete: false, request_id: `request-${index + 1}`
      },
      tool_trace: [{ namespace: 'fixture', name: 'read', record_id: `record-${index + 1}`, ok: true }]
    }))
    data.comparisons = [{ left: 0, right: 1, paired_tasks: 100, excluded_tasks: 2, left_mean: 1, right_mean: 0.9, mean_difference: 0.1 }]
    api.list.mockResolvedValue([data.run]); api.report.mockResolvedValue(data)
    const frames = new Map<number, FrameRequestCallback>()
    let nextFrame = 0
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    vi.stubGlobal('scrollTo', vi.fn())
    const host = document.createElement('div')
    host.style.overflowY = 'auto'; host.style.height = '280px'
    Object.defineProperties(host, {
      offsetHeight: { get: () => 280 }, clientHeight: { get: () => 280 },
      offsetWidth: { get: () => 900 }, clientWidth: { get: () => 900 }
    })
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
    document.body.append(host)
    const rectangle = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 900, width: 900, x: 0, y: top, toJSON() {} })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      if (this === host) return rectangle(0, 280)
      const index = this.hasAttribute('data-document-row') ? Number(this.dataset.index) : 0
      const height = this.hasAttribute('data-document-row') ? (this.querySelector('details[open]') ? 420 : 100) : 22000
      return rectangle(index * 100 - host.scrollTop, height)
    })
    const settle = async () => {
      for (let pass = 0; pass < 30; pass++) {
        await flushPromises()
        if (!frames.size) return
        const queued = [...frames.values()]; frames.clear(); queued.forEach(callback => callback(0))
      }
      throw new Error('attempt layout did not settle')
    }
    try {
      wrapper = mountView(host); await settle()
      const attempts = wrapper.findAllComponents(DocumentRows).find(rows => rows.props('as') === 'tbody')!
      expect(attempts.props('items')).toHaveLength(220)
      expect(attempts.props('itemKey')).toBe('sequence')
      expect(attempts.findAll('[data-testid="experiment-attempt"]').length).toBeGreaterThan(0)
      expect(attempts.findAll('[data-testid="experiment-attempt"]').length).toBeLessThan(40)
      expect(attempts.find('[data-index="147"]').exists()).toBe(false)
      host.scrollTop = 14700; host.dispatchEvent(new Event('scroll')); await settle()
      const later = attempts.get('[data-index="147"]')
      expect(later.get('td').text()).toBe('148')
      expect(later.findAll('td')).toHaveLength(6)
      expect(later.text()).toContain('screen-tools-01')
      expect(later.text()).toContain('gpt-6.1-sol')
      expect(later.text()).toContain('100.0%')
      expect(later.text()).toContain('Grade 148')
      const details = later.get('td:last-child > details')
      const summary = details.get('summary')
      ;(summary.element as HTMLElement).focus()
      await summary.trigger('click'); await settle()
      expect(details.attributes('open')).toBeDefined()
      expect(details.text()).toContain('request-148')
      expect(details.text()).toContain('0.012345')
      expect(details.text()).toContain('record-148')
      expect(details.text()).toContain('"namespace": "fixture"')
      expect(details.text()).toContain('Answer 148 <img src="x" onerror="alert(1)">')
      expect(details.find('img').exists()).toBe(false)
      const prompt = details.get('details')
      await prompt.get('summary').trigger('click')
      expect(prompt.attributes('open')).toBeDefined()
      expect(prompt.text()).toContain('Read the fixture')
      host.scrollTop = 21900; host.dispatchEvent(new Event('scroll')); await settle()
      expect(document.activeElement).toBe(summary.element)
      expect(attempts.get('[data-index="147"] summary').element).toBe(summary.element)
      expect(details.attributes('open')).toBeDefined()
      expect(attempts.findAll('[data-testid="experiment-attempt"]').length).toBeLessThan(40)
      expect(attempts.get('tbody').findAll(':scope > div')).toHaveLength(0)
      expect(attempts.findAll('[data-document-spacer]').every(gap => gap.get('td').attributes('colspan') === '6')).toBe(true)
      const pairTable = wrapper.findAll('table').find(table => table.text().includes('controlledExperiments.pair'))!
      expect(pairTable.findAll('tbody tr')).toHaveLength(1)
      expect(pairTable.text()).toContain('100.0%')
      expect(api.start).not.toHaveBeenCalled()
      expect(api.create).not.toHaveBeenCalled()
    } finally {
      wrapper?.unmount(); host.remove()
    }
  })

})
