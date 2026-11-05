import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'
import type { OpsDashboardOverview, OpsJobState, OpsPercentileMethod } from '@/api/admin/ops'
import en from '@/i18n/locales/en/admin/ops'
import zh from '@/i18n/locales/zh/admin/ops'
import hk from '@/i18n/locales/zh-Hant-HK/admin/ops'

vi.mock('@/api', () => ({ adminAPI: { groups: { getAll: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/admin/ops', () => ({ opsAPI: { getRealtimeTrafficSummary: vi.fn().mockResolvedValue({ enabled: true, summary: null }) } }))
vi.mock('@/stores', () => ({ useAdminSettingsStore: () => ({ opsRealtimeMonitoringEnabled: true, setOpsRealtimeMonitoringEnabledLocal: vi.fn() }) }))
enableAutoUnmount(afterEach)

function mountHeader(overrides: Partial<OpsDashboardOverview> = {}, lang = 'en') {
  const overview = {
    qps: { current: 0, avg: 0, peak: 0 }, tps: { current: 0, avg: 0, peak: 0 },
    health_score: 100, health_status: 'healthy', health_score_version: 'legacy-v2-job-health',
    duration: { sample_count: 21, percentile_method: 'raw_exact', p99_ms: 900, p95_ms: 800, p90_ms: 700, p50_ms: 300 },
    ttft: { sample_count: 17, percentile_method: 'raw_exact', p99_ms: 200, p95_ms: 150, p90_ms: 100, p50_ms: 80 },
    ...overrides
  } as OpsDashboardOverview
  return mount(OpsDashboardHeader, {
    props: { overview, platform: '', groupId: null, timeRange: '24h', queryMode: 'auto', loading: false, lastUpdated: null },
    global: {
      plugins: [createI18n({ legacy: false, locale: lang, missingWarn: false, fallbackWarn: false, messages: { en: { admin: en }, zh: { admin: zh }, 'zh-Hant-HK': { admin: hk } } })],
      stubs: { Select: true, HelpTooltip: true, Icon: true, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } }
    }
  })
}

describe('Ops dashboard metric accuracy', () => {
  it('renders unknown as -- even with an old numeric score and idle traffic', async () => {
    const wrapper = mountHeader({ health_status: 'unknown', health_score: 100, health_score_details: [
      { component: 'ttft', score: 0, weight: 0.35, reason: 'insufficient_evidence_ttft' }
    ] })
    await flushPromises()
    expect(wrapper.get('[data-testid="health-score"]').text()).toBe('--')
    const details = wrapper.get('[data-testid="health-score-details"]').text()
    expect(details).toContain('legacy-v2-job-health')
    expect(details).toContain('Insufficient TTFT evidence')
    expect(details).toContain('0.35')
    await wrapper.setProps({ overview: { ...wrapper.props('overview')!, health_status: 'degraded', health_score: 42 } })
    expect(wrapper.get('[data-testid="health-score"]').text()).toBe('42')
  })

  it('hides an unavailable score while preserving degraded status and known fault reasons', async () => {
    const wrapper = mountHeader({ health_score_available: false, health_status: 'degraded', health_score: 0,
      health_score_details: [{ component: 'db', score: 0, weight: 0.1, reason: 'database_unhealthy' }]
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="health-score"]').text()).toBe('--')
    expect(wrapper.get('[data-testid="health-score-details"]').text()).toContain('Database is unhealthy')
    expect(wrapper.text()).toContain('At Risk')
  })

  it('keeps numeric score compatibility when legacy backends omit availability and status', async () => {
    const wrapper = mountHeader({ health_score_available: undefined, health_status: undefined, health_score: 95 })
    await flushPromises()
    expect(wrapper.get('[data-testid="health-score"]').text()).toBe('95')
  })

  it('uses every server job state without interpreting conflicting heartbeat dates', async () => {
    const states: OpsJobState[] = ['healthy', 'late', 'failed', 'pending', 'disabled', 'unknown']
    const wrapper = mountHeader({
      job_health: states.map(state => ({ job_name: state, state, reason: `server reason: ${state}`, evaluated_at: '2026-11-05T12:00:00Z', deadline_at: '2026-11-05T11:59:00Z' })),
      job_heartbeats: [{ job_name: 'healthy', last_error_at: '2099-01-01T00:00:00Z', updated_at: '2026-11-05T12:00:00Z' }]
    })
    await flushPromises()
    await wrapper.get('[data-testid="job-details-button"]').trigger('click')
    const rows = wrapper.findAll('[data-testid="job-health-row"]')
    expect(rows).toHaveLength(6)
    for (const [i, state] of states.entries()) {
      expect(rows[i].get('[title]').attributes('title')).toBe(`server reason: ${state}`)
      expect(rows[i].text()).toContain('Other / cannot be evaluated')
      expect(rows[i].text()).toContain(state[0].toUpperCase() + state.slice(1))
      expect(rows[i].text()).toContain('Evaluated at')
      expect(rows[i].text()).toContain('Deadline')
    }
  })

  it('does not turn legacy heartbeats alone into healthy jobs', async () => {
    const wrapper = mountHeader({ job_health: undefined, job_heartbeats: [{ job_name: 'legacy-only', updated_at: '2026-11-05T12:00:00Z' }] })
    await flushPromises()
    await wrapper.get('[data-testid="job-details-button"]').trigger('click')
    expect(wrapper.findAll('[data-testid="job-health-row"]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('legacy-only')
  })

  it.each(['raw_exact', 'legacy_subwindow_approx', 'partial_raw', 'unavailable'] as OpsPercentileMethod[])(
    'discloses sample counts and method %s separately for duration and TTFT', async method => {
      const wrapper = mountHeader({
        duration: { sample_count: null, percentile_method: method, p99_ms: 999 },
        ttft: { sample_count: 0, percentile_method: method, p99_ms: 222 }
      })
      await flushPromises()
      expect(wrapper.get('[data-testid="duration-accuracy"]').text()).toContain('Samples: --')
      expect(wrapper.get('[data-testid="ttft-accuracy"]').text()).toContain('Samples: 0')
      expect(wrapper.get('[data-testid="duration-accuracy"]').text()).toContain(method)
      expect(wrapper.get('[data-testid="ttft-accuracy"]').text()).toContain(method)
      if (method === 'legacy_subwindow_approx') {
        expect(wrapper.text()).toContain('Max subwindow P99')
        expect(wrapper.text()).toContain('Weighted approx. P50')
        expect(wrapper.text()).toContain('Weighted approx. P90')
        expect(wrapper.text()).toContain('not whole-window percentiles')
      }
      if (method === 'partial_raw') expect(wrapper.text()).toContain('Retained raw P99')
      if (method === 'unavailable') {
        expect(wrapper.text()).not.toContain('999')
        expect(wrapper.text()).not.toContain('222')
      }
      expect(wrapper.get('[data-testid="usage-count-semantics"]').text()).toContain('not strict terminal successes')
      expect(wrapper.get('[data-testid="usage-count-semantics"]').text()).toContain('must not be added together')
      await wrapper.setProps({ fullscreen: true })
      expect(wrapper.get('[data-testid="duration-accuracy"]').text()).toContain(method)
    }
  )
  it.each(['en', 'zh', 'zh-Hant-HK'])('translates every backend reason and component in %s', async lang => {
    const reasons = ["error_rate_within_legacy_threshold", "request_or_upstream_error_rate_above_1_percent", "insufficient_evidence_no_requests", "ttft_within_legacy_threshold", "ttft_p99_above_1000ms_legacy_threshold", "insufficient_evidence_ttft", "storage_healthy", "database_unhealthy", "redis_unhealthy", "insufficient_evidence_storage_missing_or_stale", "compute_within_legacy_threshold", "cpu_above_80_or_memory_above_85_percent", "insufficient_evidence_compute_missing_or_stale", "enabled_jobs_healthy", "background_jobs_failed_or_late", "insufficient_evidence_job_health", "schedule_unavailable", "disabled_by_configuration", "invalid_schedule", "no_completion_observed", "completion_in_future", "last_execution_failed", "within_completion_deadline", "completion_deadline_exceeded"]
    reasons.push('ttft_within_legacy_threshold;provisional_percentile_evidence:legacy_subwindow_approx')
    const components = ['error_rate', 'ttft', 'storage', 'compute', 'jobs']
    const wrapper = mountHeader({ health_status: 'unknown', health_score_available: false,
      health_score_details: reasons.map((reason, i) => ({ component: components[i % components.length], score: 0, weight: 0.1, reason })),
      job_health: reasons.map((reason, i) => ({ job_name: `job-${i}`, state: 'unknown', reason, evaluated_at: '2026-11-05T12:00:00Z' }))
    }, lang)
    await flushPromises()
    expect(wrapper.get('[data-testid="health-score"]').text()).toBe('--')
    await wrapper.get('[data-testid="job-details-button"]').trigger('click')
    const details = wrapper.get('[data-testid="health-score-details"]')
    for (const reason of reasons) {
      const detail = details.findAll('[title]').find(node => node.attributes('title') === reason)!
      expect(detail).toBeDefined()
      expect(detail.text()).not.toContain(reason.split(';')[0])
      expect(detail.text()).not.toContain('admin.ops.')
      expect(detail.text()).not.toMatch(/Other \/|其他/)
    }
    const rows = wrapper.findAll('[data-testid="job-health-row"]')
    for (const [i, reason] of reasons.entries()) {
      expect(rows[i].text()).not.toContain(reason.split(';')[0])
      expect(rows[i].text()).not.toMatch(/Other \/|其他/)
    }
    for (const component of components) {
      const label = details.findAll('[title]').find(node => node.attributes('title') === component)!
      expect(label.text()).not.toBe(component)
    }
  })

})
