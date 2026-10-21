import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminGroup } from '@/types'
import GroupsView from '../GroupsView.vue'
import DataTable from '@/components/common/DataTable.vue'

const {
  listGroups,
  getAllGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  listAccounts,
  showError,
  showSuccess,
  isCurrentStep,
  nextStep,
  authState,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getAllGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  listAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  authState: { isSimpleMode: false },
}))

const messages: Record<string, string> = {
  'common.columnSettings': 'Column Settings',
  'common.resetColumns': 'Reset columns',
  'admin.groups.columns.name': 'Name',
  'admin.groups.columns.id': 'ID',
  'admin.groups.columns.platform': 'Platform',
  'admin.groups.columns.billingType': 'Billing Type',
  'admin.groups.columns.rateMultiplier': 'Rate Multiplier',
  'admin.groups.columns.type': 'Type',
  'admin.groups.columns.accounts': 'Accounts',
  'admin.groups.columns.capacity': 'Capacity',
  'admin.groups.columns.usage': 'Usage',
  'admin.groups.columns.status': 'Status',
  'admin.groups.columns.actions': 'Actions',
  'admin.groups.usageToday': 'Today',
  'admin.groups.usageYesterday': 'Yesterday',
  'admin.groups.usageTotal': 'Total',
}

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: getAllGroups,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: listAccounts,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState,
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createGroup = (overrides: Partial<AdminGroup> = {}): AdminGroup => ({
  id: 1,
  name: 'Core Anthropic',
  description: null,
  platform: 'anthropic',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 3,
  active_account_count: 2,
  rate_limited_account_count: 1,
  model_allowlist: undefined,
  sort_order: 10,
  ...overrides,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="actions" /><slot name="filters" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort', 'column-reorder'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="rows">{{ data.map((row) => row.name).join(',') }}</div>
      <div v-if="data.length" data-test="usage-cell">
        <slot name="cell-usage" :row="data[0]" />
      </div>
    </div>
  `,
}

const SelectStub = {
  props: ['modelValue', 'options', 'placeholder'],
  emits: ['update:modelValue', 'change'],
  template: `
    <select
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value); $emit('change')"
    >
      <option v-for="option in options" :key="String(option.value)" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `,
}

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const wrappers: ReturnType<typeof mount>[] = []

const mountView = async () => {
  const wrapper = mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: IconStub,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        Teleport: true,
        VueDraggable: { template: '<div><slot /></div>' },
      },
    },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

const columnKeys = (wrapper: ReturnType<typeof mount>) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const primaryKeys = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findComponent(DataTable).props('columns').filter((column: { primary?: boolean }) => column.primary !== false).map((column: { key: string }) => column.key)

const openColumnSettings = async (wrapper: ReturnType<typeof mount>) => {
  await wrapper.get('button[title="Column Settings"]').trigger('click')
}

const clickColumnToggle = async (wrapper: ReturnType<typeof mount>, label: string) => {
  const checkbox = wrapper
    .findAll('label')
    .find((item) => item.text() === label)
    ?.find('input[type="checkbox"]')
  expect(checkbox?.exists(), `column toggle ${label}`).toBe(true)
  await checkbox!.trigger('change')
  await flushPromises()
}

const defaultColumns = [
  'name', 'status', 'platform', 'account_count', 'capacity', 'usage',
  'billing_type', 'rate_multiplier', 'is_exclusive', 'id', 'actions',
]
const storageKey = 'table-columns:groups:v1'
const saveColumns = (hidden: string[], order: string[] = []) => {
  localStorage.setItem(storageKey, JSON.stringify({ version: 2, primary: Object.fromEntries(hidden.map(key => [key, false])), order }))
}

describe('admin GroupsView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listGroups.mockReset()
    getAllGroups.mockReset()
    getModelAllowlistCandidates.mockReset()
    getUsageSummary.mockReset()
    getCapacitySummary.mockReset()
    getLiveCapability.mockReset()
    listAccounts.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()
    authState.isSimpleMode = false

    listGroups.mockResolvedValue({
      items: [createGroup()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getAllGroups.mockResolvedValue([])
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    isCurrentStep.mockReturnValue(false)
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    localStorage.clear()
  })

  it('keeps all simple mode columns and skips advanced group APIs and exclusive filter', async () => {
    authState.isSimpleMode = true
    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual(['name', 'status', 'platform', 'account_count', 'id', 'actions'])
    expect(getLiveCapability).not.toHaveBeenCalled()
    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()
    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(getCapacitySummary).not.toHaveBeenCalled()
    expect(listGroups).toHaveBeenCalledWith(
      expect.any(Number), expect.any(Number),
      expect.objectContaining({ is_exclusive: undefined }), expect.anything(),
    )
    expect(wrapper.find('select').text()).not.toContain('admin.groups.allGroups')
  })

  it('shows every column in the new default order and ignores legacy hidden defaults', async () => {
    localStorage.setItem('group-hidden-columns', JSON.stringify(['id', 'usage']))
    localStorage.setItem('group-column-settings-version', '2')
    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual(defaultColumns)
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem(storageKey)).toBeNull()
    expect(listGroups).toHaveBeenCalledWith(
      1, 50, expect.objectContaining({ sort_by: 'sort_order', sort_order: 'asc' }), expect.any(Object),
    )
  })

  it('applies new saved order and visibility while keeping required columns and ignoring unknown keys', async () => {
    saveColumns(['usage', 'capacity', 'removed_column', 'name', 'actions'], ['id', 'actions', 'removed_column'])
    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual(['id', 'actions', ...defaultColumns.filter(key => !['id', 'actions'].includes(key))])
    expect(primaryKeys(wrapper)).toEqual(['actions', 'name', 'status', 'platform', 'account_count', 'rate_multiplier'])
  })

  it('promotes a default detail field through settings and persists main-row placement', async () => {
    const wrapper = await mountView()
    expect(primaryKeys(wrapper)).not.toContain('usage')
    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'Usage')

    expect(columnKeys(wrapper)).toEqual(defaultColumns)
    expect(primaryKeys(wrapper)).toContain('usage')
    expect(JSON.parse(localStorage.getItem(storageKey)!)).toEqual({ version: 2, order: [], primary: { usage: true } })
  })

  it('can promote and demote ID while keeping required identity and action checkboxes disabled', async () => {
    const wrapper = await mountView()
    await openColumnSettings(wrapper)
    for (const label of ['Name', 'Actions']) {
      const input = wrapper.findAll('label').find(item => item.text() === label)!.get('input')
      expect(input.attributes('disabled')).toBeDefined()
    }
    await clickColumnToggle(wrapper, 'ID')
    expect(columnKeys(wrapper)).toContain('id')
    expect(primaryKeys(wrapper)).toContain('id')
    await clickColumnToggle(wrapper, 'ID')
    expect(columnKeys(wrapper)).toEqual(defaultColumns)
    expect(primaryKeys(wrapper)).not.toContain('id')
    expect(JSON.parse(localStorage.getItem(storageKey)!)).toEqual({ version: 2, order: [], primary: { id: false } })
  })

  it('loads detail summaries even when their columns are unchecked and avoids refetching for main-row changes', async () => {
    saveColumns(['billing_type', 'usage', 'capacity'])
    const wrapper = await mountView()
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)

    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'Usage')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getUsageSummary).toHaveBeenCalledWith()
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)

    await clickColumnToggle(wrapper, 'Capacity')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)
    await clickColumnToggle(wrapper, 'Billing Type')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
  })

  it('restores default order and main-row choices without discarding already loaded detail summaries', async () => {
    saveColumns(['billing_type', 'usage', 'capacity'], ['actions', 'id'])
    const wrapper = await mountView()
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)

    await openColumnSettings(wrapper)
    const reset = wrapper.findAll('button').find(button => button.text() === 'Reset columns')!
    await reset.trigger('click')
    await flushPromises()
    expect(columnKeys(wrapper)).toEqual(defaultColumns)
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)
    expect(JSON.parse(localStorage.getItem(storageKey)!)).toEqual({ version: 2, order: [], primary: {} })
  })

  it('persists both header and keyboard settings reordering across remounts', async () => {
    const wrapper = await mountView()
    const headerOrder = ['actions', ...defaultColumns.filter(key => key !== 'actions')]
    wrapper.findComponent(DataTable).vm.$emit('column-reorder', headerOrder)
    await flushPromises()
    expect(columnKeys(wrapper)).toEqual(headerOrder)

    await openColumnSettings(wrapper)
    await wrapper.get('[data-column-key="id"]').trigger('keydown', { key: 'Home' })
    await flushPromises()
    const finalOrder = ['id', ...headerOrder.filter(key => key !== 'id')]
    expect(columnKeys(wrapper)).toEqual(finalOrder)
    expect(JSON.parse(localStorage.getItem(storageKey)!).order).toEqual(finalOrder)
    expect(columnKeys(await mountView())).toEqual(finalOrder)
  })

  it('renders yesterday usage between today and total', async () => {
    getUsageSummary.mockResolvedValue([
      { group_id: 1, today_cost: 1.25, yesterday_cost: 2.5, total_cost: 9.75 },
    ])
    const wrapper = await mountView()
    const text = wrapper.get('[data-test="usage-cell"]').text()
    expect(text).toContain('Today$1.25')
    expect(text).toContain('Yesterday$2.50')
    expect(text).toContain('Total$9.75')
    expect(text.indexOf('Today')).toBeLessThan(text.indexOf('Yesterday'))
    expect(text.indexOf('Yesterday')).toBeLessThan(text.indexOf('Total'))
  })

  it('appends groups without losing earlier rows, deduplicates overlap, and resets after changing sort', async () => {
    listGroups.mockResolvedValueOnce({ items: [createGroup({ id: 1, name: 'First' })], total: 3, pages: 2 })
    const wrapper = await mountView()
    listGroups.mockResolvedValueOnce({ items: [createGroup({ id: 1, name: 'Updated' }), createGroup({ id: 2, name: 'Second' }), createGroup({ id: 3, name: 'Third' })], total: 3, pages: 2 })
    wrapper.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more')
    await flushPromises()
    expect(listGroups.mock.lastCall?.[0]).toBe(2)
    expect(wrapper.get('[data-test="rows"]').text()).toBe('Updated,Second,Third')
    expect(wrapper.getComponent({ name: 'InfiniteListSentinel' }).props('hasMore')).toBe(false)
    listGroups.mockResolvedValueOnce({ items: [createGroup({ id: 3, name: 'Third' })], total: 1, pages: 1 })
    wrapper.getComponent(DataTable).vm.$emit('sort', 'name', 'desc')
    await flushPromises()
    expect(listGroups.mock.lastCall?.[0]).toBe(1)
    expect(wrapper.get('[data-test="rows"]').text()).toBe('Third')
  })

  it('keeps already loaded groups on append failure and retries the same batch', async () => {
    listGroups.mockResolvedValueOnce({ items: [createGroup({ id: 1, name: 'First' })], total: 2, pages: 2 })
    const wrapper = await mountView()
    listGroups.mockRejectedValueOnce(new Error('temporary failure'))
    wrapper.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more')
    await flushPromises()
    expect(wrapper.get('[data-test="rows"]').text()).toBe('First')
    expect(wrapper.getComponent({ name: 'InfiniteListSentinel' }).props('error')).toBeTruthy()
    listGroups.mockResolvedValueOnce({ items: [createGroup({ id: 2, name: 'Second' })], total: 2, pages: 2 })
    wrapper.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('retry')
    await flushPromises()
    expect(listGroups.mock.calls.slice(-2).map(call => call[0])).toEqual([2, 2])
    expect(wrapper.get('[data-test="rows"]').text()).toBe('First,Second')
  })

})
