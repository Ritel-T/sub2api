import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const { listAccounts, authState } = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  authState: { token: 'test-token', isSimpleMode: false }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getManagementCapabilities: vi.fn().mockResolvedValue({ web_search_enabled: false, account_quota_notify_enabled: false }),
      list: listAccounts,
      listWithEtag: vi.fn(),
      getBatchTodayStats: vi.fn().mockResolvedValue({ stats: {} }),
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: { getAll: vi.fn().mockResolvedValue([]) },
    groups: { getAll: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const DataTableStub = {
  name: 'DataTable',
  props: ['columns'],
  emits: ['sort', 'column-reorder'],
  template: `
    <div data-test="data-table">
      <span v-for="column in columns" :key="column.key" :data-column="column.key">
        {{ column.sortable ? 'sortable' : 'fixed' }}
      </span>
      <button data-test="sort-priority" @click="$emit('sort', 'priority', 'desc')" />
    </div>
  `
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        OpenAIWeeklyQuotaSummary: true,
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        ColumnSettings: true,
        AccountTableActions: { template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: true
      }
    }
  })
}

describe('admin AccountsView priority column preferences', () => {
  beforeEach(() => {
    localStorage.clear()
    authState.isSimpleMode = false
    listAccounts.mockReset().mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
  })

  it('shows priority as a sortable column for fresh preferences', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-column="priority"]').text()).toBe('sortable')

    await wrapper.get('[data-test="sort-priority"]').trigger('click')
    await flushPromises()

    expect(listAccounts).toHaveBeenLastCalledWith(
      1,
      20,
      expect.objectContaining({ sort_by: 'priority', sort_order: 'desc' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('preserves priority in details when unchecked and allows promotion without losing its sorting contract', async () => {
    const storageKey = 'table-columns:accounts:v1'
    localStorage.setItem(storageKey, JSON.stringify({ version: 2, order: [], primary: { priority: false, today_stats: false } }))
    const wrapper = mountView()
    await flushPromises()
    const priorityColumn = () => wrapper.getComponent({ name: 'DataTable' }).props('columns').find((column: { key: string }) => column.key === 'priority')
    expect(wrapper.get('[data-column="priority"]').text()).toBe('sortable')
    expect(priorityColumn().primary).toBe(false)
    wrapper.getComponent({ name: 'ColumnSettings' }).vm.$emit('toggle', 'priority')
    await flushPromises()
    expect(priorityColumn().primary).toBe(true)
    expect(priorityColumn().sortable).toBe(true)
    expect(JSON.parse(localStorage.getItem(storageKey)!).primary).toEqual({ priority: true, today_stats: false })
  })

  it('shows all columns without migrating obsolete automatic hiding preferences', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['today_stats']))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-column="priority"]').text()).toBe('sortable')
    expect(wrapper.findAll('[data-column]').map(column => column.attributes('data-column'))).toEqual([
      'id', 'platform_type', 'name', 'status', 'schedulable', 'usage', 'capacity', 'groups', 'links', 'today_stats',
      'priority', 'proxy', 'scheduler_score', 'rate_multiplier',
      'upstream_billing_rate', 'last_used_at', 'expires_at', 'notes', 'created_at', 'actions'
    ])
  })

  it('reorders required columns including ID without changing data sort', async () => {
    const wrapper = mountView()
    await flushPromises()
    const table = wrapper.findComponent({ name: 'DataTable' })
    const columns = table.props('columns').map((column: { key: string }) => column.key)
    const calls = listAccounts.mock.calls.length
    table.vm.$emit('column-reorder', ['actions', ...columns.filter((key: string) => key !== 'actions')])
    await flushPromises()
    expect(table.props('columns').slice(0, 3).map((column: { key: string }) => column.key)).toEqual(['actions', 'id', 'platform_type'])
    expect(listAccounts).toHaveBeenCalledTimes(calls)
  })


  it('retains the simple-mode restriction while otherwise showing all columns', async () => {
    authState.isSimpleMode = true
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-column="groups"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-column]')).toHaveLength(19)
  })

})
