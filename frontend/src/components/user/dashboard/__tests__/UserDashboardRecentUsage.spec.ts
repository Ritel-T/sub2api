import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import UserDashboardRecentUsage from '../UserDashboardRecentUsage.vue'
import { overviewDuration, overviewTokenTotal, recentOverviewRequests } from '../overviewRequests'
import type { UsageLog, UserErrorRequest } from '@/types'

vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {} }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
enableAutoUnmount(afterEach)

function usage(id: number, minute: number, extra: Partial<UsageLog> = {}): UsageLog {
  return { id, model: `model-${id}`, created_at: `2026-10-25T12:${String(minute).padStart(2, '0')}:00Z`, input_tokens: 100,
    output_tokens: 20, cache_creation_tokens: 30, cache_read_tokens: 50, total_cost: .4, actual_cost: .2,
    duration_ms: 1234, ...extra } as UsageLog
}
function failure(id: number, minute: number): UserErrorRequest {
  return { id, model: `failed-${id}`, created_at: `2026-10-25T12:${String(minute).padStart(2, '0')}:00Z`,
    status_code: 429, category: 'rate_limit', message: 'Rate limit', platform: 'openai', inbound_endpoint: '/v1/responses', key_name: 'Key', key_deleted: false }
}
async function render(data: UsageLog[], errors: UserErrorRequest[] = [], locale = 'en', options = {}) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/usage', component: { template: '<div />' } }] })
  await router.push('/'); await router.isReady()
  const i18n = createI18n({ legacy: false, locale, messages: { en: {}, zh: {}, 'zh-Hant-HK': {} } })
  return mount(UserDashboardRecentUsage, { props: { data, errors, loading: false, ...options }, global: { plugins: [router, i18n] } })
}

describe('overview recent requests', () => {
  it('merges the latest five without dropping colliding source IDs or mutating input', () => {
    const data = [usage(1, 1), usage(2, 5), usage(3, 3), usage(4, 8)]
    const errors = [failure(4, 9), failure(2, 4), failure(6, 0)]
    expect(recentOverviewRequests(data, errors).map(row => row.key)).toEqual(['error:4', 'usage:4', 'usage:2', 'error:2', 'usage:3'])
    expect(data.map(row => row.id)).toEqual([1, 2, 3, 4])
    expect(overviewTokenTotal(usage(1, 1, { cache_creation_5m_tokens: 30, cache_creation_1h_tokens: 0 }))).toBe(200)
  })

  it('shows cache-inclusive tokens, actual charge, duration with plain model text', async () => {
    const wrapper = await render([usage(4, 8)])
    const row = wrapper.get('[data-request="usage:4"]')
    expect(row.get('[data-metric="tokens"] > span:last-child').text()).toBe('200')
    expect(row.get('[data-metric="duration"] > span:last-child').text()).toBe('1.23s')
    expect(row.get('[data-metric="cost"] > span:last-child').text()).toBe('$0.2000')
    expect(row.text()).toContain('Success')
    expect(row.find('a').exists()).toBe(false)
    expect(row.get('.recent-model').text()).toBe('model-4')
    expect(wrapper.get('header a').attributes('href')).toBe('/usage?view=records')
  })

  it('shows unknown failure metrics and a non-interactive model name', async () => {
    const wrapper = await render([usage(4, 8)], [failure(4, 9)])
    const row = wrapper.get('[data-request="error:4"]')
    expect(row.text()).toContain('Failed · 429')
    for (const metric of ['tokens', 'duration', 'cost']) expect(row.get(`[data-metric="${metric}"] > span:last-child`).text()).toBe('—')
    expect(row.find('button').exists()).toBe(false)
    expect(row.find('a').exists()).toBe(false)
    expect(row.get('.recent-model').text()).toBe('failed-4')
  })

  it.each([['zh', '状态', '失败'], ['zh-Hant-HK', '狀態', '失敗']])('localizes %s request labels', async (locale, status, failed) => {
    const wrapper = await render([], [failure(8, 1)], locale)
    expect(wrapper.text()).toContain(status)
    expect(wrapper.text()).toContain(failed)
  })

  it('distinguishes loading, failure and empty, and exposes retry', async () => {
    const wrapper = await render([], [], 'en', { loading: true })
    expect(wrapper.get('[role="status"]').text()).toBe('Loading…')
    expect(wrapper.text()).not.toContain('No requests yet')
    await wrapper.setProps({ loading: false, error: true })
    await wrapper.get('[role="alert"] button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
    await wrapper.setProps({ error: false })
    expect(wrapper.text()).toContain('No requests yet')
  })

  it('keeps missing duration distinct from zero and formats long durations', () => {
    expect(overviewDuration(null)).toBe('—')
    expect(overviewDuration(0)).toBe('0ms')
    expect(overviewDuration(61000)).toBe('1m 1s')
    expect(overviewDuration(3660000)).toBe('1h 1m')
  })
})

