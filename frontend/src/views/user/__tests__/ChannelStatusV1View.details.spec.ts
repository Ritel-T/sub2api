import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ChannelStatusV1View from '../ChannelStatusV1View.vue'
import ChannelStatusV2View from '../ChannelStatusV2View.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), status: vi.fn() }))
vi.mock('@/api/channelMonitor', () => mocks)
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { channel_monitor_enabled: true }, showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, te: () => false, locale: ref('en') })
}))
const v2 = vi.hoisted(() => ({ getSnapshot: vi.fn(), getMatrix: vi.fn(), getModels: vi.fn(), getDimensions: vi.fn(), replace: vi.fn() }))
vi.mock('@/api/channelMonitorV2', () => v2)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: false }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }), useRouter: () => ({ replace: v2.replace }) }))
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers(); localStorage.clear() })

function detail(availability: number) {
  return { id: 1, models: [{ model: 'model', availability_15d: availability, availability_30d: availability }] }
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  localStorage.clear()
  localStorage.setItem('channel-status-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 30 }))
  mocks.list.mockImplementation(async () => ({ items: [{ id: 1, name: 'Monitor', primary_model: 'model', primary_status: 'operational', availability_7d: 99 }] }))
  mocks.status.mockResolvedValue(detail(90))
})

function mountView() {
  return shallowMount(ChannelStatusV1View, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      MonitorHero: { props: ['loading'], emits: ['update:window', 'refresh'], template: `<div :data-loading="loading">
        <button class="fifteen" @click="$emit('update:window', '15d')">15d</button>
        <button class="thirty" @click="$emit('update:window', '30d')">30d</button>
        <button class="refresh" @click="$emit('refresh')">Refresh</button>
      </div>` },
      MonitorCardGrid: false,
      MonitorCard: { props: ['availabilityValue'], template: '<output>{{ availabilityValue }}</output>' }
    } }
  })
}

describe('channel monitor automatic detail refresh', () => {
  it.each(['fifteen', 'thirty'])('refreshes the cached %s-day availability on timer ticks', async (tab) => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.' + tab).trigger('click')
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('90')
    mocks.status.mockResolvedValue(detail(98))
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('98')
    expect(mocks.status).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-loading]').attributes('data-loading')).toBe('false')
  })

  it('keeps the seven-day view on the list endpoint without extra detail requests', async () => {
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    expect(mocks.status).not.toHaveBeenCalled()
    expect(wrapper.get('output').text()).toBe('99')
  })

  it('still refreshes cached availability on manual reload', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.fifteen').trigger('click')
    await flushPromises()
    mocks.status.mockResolvedValue(detail(98))
    await wrapper.get('.refresh').trigger('click')
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('98')
  })
})


describe('V2 model drilldown selection access', () => {
  afterEach(() => { vi.restoreAllMocks() })
  const modelRows = ['openai', 'anthropic'].map(platform => ({
    get platform() { return platform }, model: 'shared-model', metrics: { has_samples: true, request_count: 3, error_rate: 0, cache_rate: 0.5, rpm: 1, tpm: 60, ttft: { p50_ms: 100, p95_ms: 150 } },
    health: { overall: 'healthy', error_rate: 'healthy', ttft: 'healthy' },
  }))
  beforeEach(() => {
    v2.getSnapshot.mockResolvedValue(null)
    v2.getMatrix.mockResolvedValue({ items: [], coverage: {} })
    v2.getModels.mockResolvedValue({ items: modelRows })
    v2.getDimensions.mockResolvedValue({ platforms: [], groups: [], models: [] })
    v2.replace.mockResolvedValue(undefined)
  })
  async function openAnalytics() {
    const wrapper = shallowMount(ChannelStatusV2View, {
      attachTo: document.body,
      global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } },
    })
    await flushPromises()
    await wrapper.get('[data-testid="monitor-layout-toggle"]').trigger('click')
    await flushPromises()
    v2.getSnapshot.mockClear(); v2.getMatrix.mockClear(); v2.getModels.mockClear(); v2.replace.mockClear()
    return wrapper
  }

  it.each([{ key: 'Enter', code: 'Enter' }, { key: ' ', code: 'Space' }])('uses a focusable native $code button for the exact platform/model and runs the original drilldown once', async ({ key, code }) => {
    const wrapper = await openAnalytics()
    const buttons = wrapper.findAll('tbody tr td:first-child button')
    expect(buttons).toHaveLength(2)
    expect(buttons[0].attributes('aria-label')).not.toBe(buttons[1].attributes('aria-label'))
    const button = buttons[1]
    expect(button.attributes('type')).toBe('button')
    expect(button.attributes('aria-label')).toContain('anthropic / shared-model')
    ;(button.element as HTMLButtonElement).focus()
    expect(document.activeElement).toBe(button.element)
    await button.trigger('keydown', { key, code }); await button.trigger('keyup', { key, code })
    const platformRead = vi.spyOn(modelRows[1], 'platform', 'get')
    // jsdom does not synthesize the browser's native keyboard click.
    button.element.dispatchEvent(new MouseEvent('click', { detail: 0, bubbles: true }))
    expect(platformRead).toHaveBeenCalledOnce()
    await flushPromises()
    expect(v2.replace).toHaveBeenCalledOnce()
    expect(v2.replace).toHaveBeenCalledWith(expect.objectContaining({ query: expect.objectContaining({ platform: 'anthropic', model: 'shared-model' }) }))
    expect(v2.getSnapshot).toHaveBeenCalledOnce()
    expect(v2.getMatrix).toHaveBeenCalledOnce()
    expect(v2.getModels).toHaveBeenCalledOnce()
    expect(v2.getModels.mock.calls[0][0]).toMatchObject({ platforms: ['anthropic'], models: ['shared-model'] })
    expect((wrapper.vm as any).modelRows).toEqual(modelRows)
  })

  it('keeps original pointer row selection and suppresses a second drilldown from the name button', async () => {
    const wrapper = await openAnalytics()
    await wrapper.findAll('tbody tr')[0].findAll('td')[1].trigger('click'); await flushPromises()
    expect(v2.replace).toHaveBeenCalledOnce()
    expect(v2.getModels.mock.calls[0][0]).toMatchObject({ platforms: ['openai'], models: ['shared-model'] })
    v2.replace.mockClear(); v2.getSnapshot.mockClear(); v2.getMatrix.mockClear(); v2.getModels.mockClear()
    const platformRead = vi.spyOn(modelRows[1], 'platform', 'get')
    wrapper.findAll('tbody tr td:first-child button')[1].element.dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true }))
    expect(platformRead).toHaveBeenCalledOnce()
    await flushPromises()
    expect(v2.replace).toHaveBeenCalledOnce()
    expect(v2.getModels).toHaveBeenCalledOnce()
    expect(v2.getModels.mock.calls[0][0]).toMatchObject({ platforms: ['anthropic'], models: ['shared-model'] })
  })
})
