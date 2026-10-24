import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub, type VueWrapper } from '@vue/test-utils'

import TokenGuardView from '../TokenGuardView.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'

const api = vi.hoisted(() => ({
  status: vi.fn(),
  relogin: vi.fn(),
  listManaged: vi.fn(),
  listGroups: vi.fn(),
}))

vi.mock('@/api/admin/accountTokenGuard', () => ({
  getTokenGuardStatus: api.status,
  reloginTokenGuardAccount: api.relogin,
  startTokenGuardRun: vi.fn(),
  saveTokenGuardConfig: vi.fn(),
  formatTokenGuardReloginText: () => '',
  parseTokenGuardReloginText: () => [],
}))
vi.mock('@/api/admin/accountTokenGuardV2', () => ({ listTokenGuardV2Accounts: api.listManaged }))
vi.mock('@/api/admin/groups', () => ({ groupsAPI: { getAll: api.listGroups }, default: { getAll: api.listGroups } }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${Object.values(params).join(' ')}` : key,
      te: () => true,
    }),
  }
})

const guardState = (id: number, name: string) => ({
  account_id: id, account_name: name, account_status: 'active', schedulable: true, probe_state: 'ok', probe_detail: '',
  latency_ms: 120, fail_streak: 0, last_probe_at: null, last_fix_at: null, last_fix_action: '', last_fix_result: '',
  needs_relogin: false, updated_at: '',
})
const managed = (id: number, name: string, extra: Record<string, unknown> = {}) => ({
  account_id: id, account_name: name, account_status: 'active', schedulable: true, enabled: true, auto_relogin_enabled: true,
  probe_state: 'ok', probe_detail: 'credential accepted', fail_streak: 0, next_probe_at: '',
  login_config: { account_id: id, login_email: `${name}@example.com`, credential_mode: 'password_totp', proxy_source: 'account',
    password_configured: true, totp_configured: true, configured: true },
  ...extra,
})

let wrapper: VueWrapper | undefined

const mountView = () => mount(TokenGuardView, {
  global: {
    stubs: {
      AppLayout: { template: '<main><slot /></main>' },
      SmartOpsNav: true,
      Icon: true,
      Select: true,
      RouterLink: RouterLinkStub,
    },
  },
})

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  api.status.mockResolvedValue({
    config: { enabled: true, group_ids: [], interval_seconds: 1800, probe_endpoint: '', probe_model: '', probe_headers: {}, probe_timeout_seconds: 20,
      probe_concurrency: 2, max_probe_per_cycle: 100, auto_relogin: true, relogin_endpoint: '', relogin_headers: {}, relogin_accounts: [],
      restore_schedulable: true, fail_streak_threshold: 2, bark_key: '', notify_on_fix: false, notify_on_fail: false },
    accounts: [guardState(1, 'legacy')],
    events: [],
    runtime: { running: false, last_run: null, last_message: '', stats: { probed: 0, healthy: 0, auth_failed: 0, transient: 0, repaired: 0, state_fixed: 0, failed: 0, duration_ms: 0, started_at: 0 } },
  })
  api.listManaged.mockResolvedValue({
    probe_interval_seconds: 1800, retry_interval_seconds: 300, relogin_cooldown_seconds: 1800, fail_streak_threshold: 2,
    accounts: [
      managed(42, 'twofa'),
      managed(43, 'paused', { enabled: false, account_status: 'error', login_config: undefined }),
      // 同一个账号不会在两边重复出现。
      managed(1, 'legacy'),
    ],
  })
  api.listGroups.mockResolvedValue([])
  api.relogin.mockResolvedValue({ account_id: 1, action: 'relogin' })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

describe('TokenGuardView', () => {
  it('lists 2FA-imported accounts managed by credential operations as read-only rows', async () => {
    wrapper = mountView()
    await flushPromises()

    const rows = wrapper.findAll('tr.managed-row')
    expect(rows.map(row => row.attributes('data-managed-account-id'))).toEqual(['42', '43'])
    expect(wrapper.get('.events-heading h3 span').text()).toBe('3')
    expect(wrapper.find('[data-testid="token-guard-managed-hint"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="token-guard-managed-credentials"]').text()).toBe('tokenGuard.managedCredentialsHint 1')
    expect(wrapper.text()).not.toContain('tokenGuard.noAccounts')

    expect(rows[0].text()).toContain('tokenGuard.managedBadge')
    expect(rows[0].text()).toContain('tokenGuard.probeStates.ok')
    expect(rows[1].text()).toContain('tokenGuard.managedPaused')
    // 托管账号只能去凭证运营重登，旧守护不给它们重登按钮。
    expect(rows.every(row => !row.find('button').exists())).toBe(true)
    expect(rows[0].findComponent(RouterLinkStub).props('to')).toBe('/admin/token-guard-v2')
    // 异常账号统计也算上托管账号。
    expect(wrapper.findAll('.summary-card')[1].get('strong').text()).toBe('1')
  })

  it('keeps the legacy guard working when credential operations cannot be loaded', async () => {
    api.listManaged.mockRejectedValue(new Error('offline'))
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('tr.managed-row')).toHaveLength(0)
    expect(wrapper.get('.managed-hint.warn').text()).toBe('tokenGuard.managedLoadFailed')
    expect(wrapper.find('.error-banner').exists()).toBe(false)

    await wrapper.get('tbody tr:not(.managed-row) button').trigger('click')
    await flushPromises()
    expect(api.relogin).toHaveBeenCalledWith(1)
  })

  it('shows the empty state only when neither guard has accounts', async () => {
    api.status.mockResolvedValue({ ...(await api.status()), accounts: [] })
    api.listManaged.mockResolvedValue({ accounts: [managed(42, 'twofa')] })
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).not.toContain('tokenGuard.noAccounts')
    expect(wrapper.findAll('tr.managed-row')).toHaveLength(1)

    wrapper.unmount()
    api.listManaged.mockResolvedValue({ accounts: [] })
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('tokenGuard.noAccounts')
    expect(wrapper.find('[data-testid="token-guard-managed-hint"]').exists()).toBe(false)
  })
})


describe('TokenGuardView shared record contracts', () => {
  it('persists secondary account fields and preserves managed read-only actions in details', async () => {
    wrapper = mountView()
    await flushPromises()
    const table = wrapper.findAllComponents(DataTable)[0]
    expect(table.props('rowKey')).toBe('key')
    expect(table.props('data').map(row => row.key)).toEqual(['legacy-1', 'managed-42', 'managed-43'])
    const settings = table.findComponent(ColumnSettings)
    settings.vm.$emit('toggle', 'probe')
    settings.vm.$emit('toggle', 'actions')
    await flushPromises()
    expect(table.find('th[data-column-key="probe"]').exists()).toBe(false)
    expect(table.find('th[data-column-key="actions"]').exists()).toBe(false)
    await table.get('tr[data-index="0"] [data-test="toggle-details"]').trigger('click')
    const legacyDetails = table.findAll('.table-detail-row')[0]
    await legacyDetails.findAll('button').find(button => button.text() === 'tokenGuard.relogin')!.trigger('click')
    await flushPromises()
    expect(api.relogin).toHaveBeenCalledWith(1)
    wrapper.unmount()
    wrapper = mountView()
    await flushPromises()
    const restored = wrapper.findAllComponents(DataTable)[0]
    expect(restored.find('th[data-column-key="actions"]').exists()).toBe(false)
    await restored.get('[data-managed-account-id="42"] [data-test="toggle-details"]').trigger('click')
    const detail = restored.findAll('.table-detail-row')[0]
    expect(detail.text()).toContain('tokenGuard.probeStates.ok')
    expect(detail.findComponent(RouterLinkStub).props('to')).toBe('/admin/token-guard-v2')
    expect(detail.findAll('button').some(button => button.text() === 'tokenGuard.relogin')).toBe(false)
  })

  it('bounds both mobile snapshots while retaining all account and event fields', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: false, media: query, onchange: null, addEventListener: vi.fn(), removeEventListener: vi.fn(), addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn() }))
    const status = await api.status()
    api.status.mockResolvedValue({ ...status,
      accounts: Array.from({ length: 120 }, (_, index) => guardState(index + 1, `legacy-${index + 1}`)),
      events: Array.from({ length: 130 }, (_, index) => ({ id: index + 1, account_id: 1, account_name: 'legacy-1', kind: 'probe_ok', detail: `Event detail ${index + 1}`, created_at: '2026-10-24T00:00:00Z' }))
    })
    api.listManaged.mockResolvedValue({ accounts: Array.from({ length: 40 }, (_, index) => managed(index + 121, `managed-${index + 121}`)) })
    wrapper = mountView()
    await flushPromises()
    const tables = wrapper.findAllComponents(DataTable)
    expect(tables.map(table => table.props('data').length)).toEqual([160, 130])
    for (const table of tables) {
      const rows = table.findAll('.table-record')
      expect(rows.length).toBeGreaterThan(0)
      expect(rows.length).toBeLessThan(40)
    }
    expect(tables[0].findAll('.table-record')[0].findAll('[data-field]').map(field => field.attributes('data-field'))).toEqual(['account', 'account_status', 'schedulable', 'probe', 'latency', 'last_fix', 'actions'])
    expect(tables[1].findAll('.table-record')[0].text()).toContain('Event detail 1')
    expect(tables[1].props('columns').every(column => column.primary && !column.sortable)).toBe(true)
  })
})
