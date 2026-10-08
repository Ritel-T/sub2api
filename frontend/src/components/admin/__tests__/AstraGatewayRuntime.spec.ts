import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AstraGatewayRuntime from '../AstraGatewayRuntime.vue'
const mocks = vi.hoisted(() => ({ get: vi.fn(), test: vi.fn() }))
vi.mock('@/api/admin/astraGateway', () => ({ getAstraGatewayRuntime: mocks.get, testAstraGateway: mocks.test }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${JSON.stringify(values)}` : key, te: () => true }) }))
const settings = { cookie_pool: { enabled: true, source_account_ids: [299], target_account_ids: [300] }, ws_session: { enabled: true, account_ids: [300] }, revision: 'one' }
function snapshot() { return { generated_at: new Date().toISOString(), revision: 'one', sources: [{ account_id: 299, state: 'ready', reason: 'qualified', expires_at: new Date(Date.now() + 120000).toISOString(), gateway: 'chat.gateway.unified-196.api.openai.com' }], targets: [{ account_id: 300, reason: 'route_available' }], ws: [{ account_id: 300, ready: false, reason: 'account_disabled', active_sessions: 0 }], ready_routes: 1, preparing: false } }
beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks(); mocks.get.mockResolvedValue(snapshot()); mocks.test.mockResolvedValue({ action: 'prepare', success: true, reason: 'success', duration_ms: 1000 }) })
afterEach(() => vi.useRealTimers())
describe('Astra gateway runtime', () => {
  it('never shows a countdown for failed or unverified routes', async () => {
    const data = snapshot(); data.sources[0].state = 'candidate'; data.sources[0].reason = 'source_passed_target_failed'; data.ready_routes = 0
    mocks.get.mockResolvedValue(data)
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.find('tbody').text()).not.toContain('120 s'); expect(w.find('tbody').text()).toContain('—'); w.unmount()
  })
  it('shows automatic setup progress and disables competing tests', async () => {
    mocks.get.mockResolvedValue({ ...snapshot(), setup: { revision: 'one', state: 'running', phase: 'target', account_id: 300, reason: '' } })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.get('[data-testid="setup-status"]').text()).toContain('#300'); expect(w.get('[data-testid="prepare"]').attributes('disabled')).toBeDefined(); w.unmount()
  })
  it('verifies targets with the shared state probe and exposes no candy selector', async () => {
    mocks.test.mockResolvedValue({ action: 'verify', account_id: 300, test_kind: 'state_probe', success: false, reason: 'target_probe_degraded', duration_ms: 2000 })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.find('[data-testid="test-kind"]').exists()).toBe(false)
    expect(w.get('[data-testid="probe-hint"]').text()).toContain('admin.astraGateway.probeHint')
    await w.findAll('button').find(b => b.text() === 'admin.astraGateway.verifyTarget')!.trigger('click'); await flushPromises()
    expect(mocks.test).toHaveBeenCalledWith('verify', 300, 'state_probe')
    expect(w.find('[role="status"]').text()).toContain('admin.astraGateway.testReasons.ticketChanged'); w.unmount()
  })
  it('shows route lifetime, blocks invalid WS and refreshes status', async () => {
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.text()).toContain('120 s'); expect(w.text()).toContain('unified-196')
    const ws = w.findAll('button').find(b => b.text() === 'admin.astraGateway.verifyWS')!
    expect(ws.attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(5000); await flushPromises(); expect(mocks.get).toHaveBeenCalledTimes(2)
    w.unmount(); await vi.advanceTimersByTimeAsync(5000); expect(mocks.get).toHaveBeenCalledTimes(2)
  })
  it('prepares a route and shows the result', async () => {
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    await w.get('[data-testid="prepare"]').trigger('click'); await flushPromises()
    expect(mocks.test).toHaveBeenCalledWith('prepare', 0, 'state_probe'); expect(w.find('[role="status"]').exists()).toBe(true)
    await w.setProps({ dirty: true }); expect(w.get('[data-testid="prepare"]').attributes('disabled')).toBeDefined(); w.unmount()
  })
})

describe('Model route availability with shared account quality', () => {
  it('renders separate model admission states for the same target account', async () => {
    mocks.get.mockResolvedValue({ ...snapshot(), targets: [
      { account_id: 300, model: 'gpt-6-astra', state: 'ready', reason: 'target_probe_passed', expires_at: new Date(Date.now() + 120000).toISOString() },
      { account_id: 300, model: 'gpt-6.1-sol', state: 'failed', reason: 'answer_mismatch' }
    ] })
    const w = mount(AstraGatewayRuntime, { props: { settings: { ...settings, auto_quality: true }, dirty: false } }); await flushPromises()
    expect(w.text()).toContain('gpt-6-astra')
    expect(w.text()).toContain('gpt-6.1-sol')
    const targets = w.findAll('div.rounded-xl').filter(row => row.text().includes('admin.astraGateway.targets #300'))
    expect(targets).toHaveLength(2)
    expect(targets[0].text()).toContain('120 s')
    expect(targets[1].text()).not.toContain('120 s')
    expect(w.get('[data-testid="auto-runtime-hint"]').text()).toContain('autoRuntimeHint')
    w.unmount()
  })
})

describe('Incomplete preparation status', () => {
  it.each(['partial', 'waiting'])('shows %s preparation and a readable continuation reason', async state => {
    mocks.get.mockResolvedValue({ ...snapshot(), setup: { state, phase: 'target', account_id: 300, reason: 'warm_budget_exhausted', ready: state === 'partial' ? 1 : 0, pending: 2, blocked: 1 } })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    const status = w.get('[data-testid="setup-status"]').text()
    expect(status).toContain(`admin.astraGateway.setupStates.${state}`)
    expect(status).toContain('admin.astraGateway.testReasons.budgetPending')
    expect(status).not.toContain('setupStates.failed')
    expect(w.get('[data-testid="setup-counts"]').text()).toContain('admin.astraGateway.setupCounts')
    w.unmount()
  })
  it('shows an expired source instead of its historical passed reason', async () => {
    const data = snapshot(); data.sources[0].state = 'expired'; data.sources[0].reason = 'target_probe_passed'
    mocks.get.mockResolvedValue(data)
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    const source = w.find('tbody').text()
    expect(source).toContain('admin.astraGateway.testReasons.expired')
    expect(source).not.toContain('target_probe_passed')
    expect(source).not.toContain('120 s')
    w.unmount()
  })
})

describe('Borrowing status details', () => {
  it('counts distinct ready targets and model routes without calling sources targets', async () => {
    const future = new Date(Date.now() + 120000).toISOString()
    mocks.get.mockResolvedValue({ ...snapshot(), ready_routes: 2, targets: [
      { account_id: 300, model: 'gpt-6-astra', state: 'ready', reason: 'target_probe_passed', expires_at: future },
      { account_id: 300, model: 'gpt-6.1-sol', state: 'ready', reason: 'target_probe_passed', expires_at: future },
      { account_id: 301, model: 'gpt-6-astra', state: 'ready', reason: 'target_probe_passed', expires_at: new Date(Date.now() - 1000).toISOString() }
    ] })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    const counts = w.get('[data-testid="route-counts"]').text()
    expect(counts).toContain('readyRoutes {"n":2}')
    expect(counts).toContain('readyTargets {"n":1}')
    expect(counts).toContain('readyModels {"n":2}')
    w.unmount()
  })
  it('shows retry time, real attempts and a safe last integer answer', async () => {
    const retry = new Date(Date.now() + 30000).toISOString()
    mocks.get.mockResolvedValue({ ...snapshot(), targets: [
      { account_id: 300, state: 'failed', reason: 'target_quality_failed', answer: '29', attempts: 4, retry_at: retry },
      { account_id: 301, state: 'failed', reason: 'target_probe_failed', answer: 'provider-secret-message' },
      { account_id: 302, state: 'failed', reason: 'target_quality_failed', answer: '-21' }
    ] })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    const targets = w.findAll('[data-testid="target-route"]')
    expect(targets[0].get('[data-testid="target-answer"]').text()).toContain('29')
    expect(targets[0].get('[data-testid="target-probe-counts"]').text()).toContain('{"correct":0,"attempts":4}')
    expect(targets[0].get('[data-testid="target-retry"]').text()).toContain(new Date(retry).toLocaleString())
    expect(targets[1].find('[data-testid="target-answer"]').exists()).toBe(false)
    expect(targets[1].find('[data-testid="target-probe-counts"]').exists()).toBe(false)
    expect(targets[2].find('[data-testid="target-answer"]').exists()).toBe(false)
    expect(w.text()).not.toContain('provider-secret-message')
    w.unmount()
  })
})
