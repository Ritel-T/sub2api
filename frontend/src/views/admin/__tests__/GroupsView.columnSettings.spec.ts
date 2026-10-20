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
      <slot name="filters" />
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
  localStorage.setItem(storageKey, JSON.stringify({ hidden, order }))
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
      1, 20, expect.objectContaining({ sort_by: 'sort_order', sort_order: 'asc' }), expect.any(Object),
    )
  })

  it('applies new saved order and visibility while keeping required columns and ignoring unknown keys', async () => {
    saveColumns(['usage', 'capacity', 'removed_column', 'name', 'actions'], ['id', 'actions', 'removed_column'])
    const wrapper = await mountView()

    expect(columnKeys(wrapper)).toEqual([
      'id', 'actions', 'name', 'status', 'platform', 'account_count',
      'billing_type', 'rate_multiplier', 'is_exclusive',
    ])
  })

  it('toggles a column through the shared settings and persists its visibility', async () => {
    const wrapper = await mountView()
    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'Usage')

    expect(columnKeys(wrapper)).toEqual(defaultColumns.filter(key => key !== 'usage'))
    expect(JSON.parse(localStorage.getItem(storageKey)!)).toEqual({ order: [], hidden: ['usage'] })
  })

  it('can hide and show ID while keeping required identity and action checkboxes disabled', async () => {
    const wrapper = await mountView()
    await openColumnSettings(wrapper)
    for (const label of ['Name', 'Actions']) {
      const input = wrapper.findAll('label').find(item => item.text() === label)!.get('input')
      expect(input.attributes('disabled')).toBeDefined()
    }
    await clickColumnToggle(wrapper, 'ID')
    expect(columnKeys(wrapper)).not.toContain('id')
    await clickColumnToggle(wrapper, 'ID')
    expect(columnKeys(wrapper)).toEqual(defaultColumns)
    expect(JSON.parse(localStorage.getItem(storageKey)!)).toEqual({ order: [], hidden: [] })
  })

  it('skips summary fetches while all consuming columns are hidden and lazily loads when shown', async () => {
    saveColumns(['billing_type', 'usage', 'capacity'])
    const wrapper = await mountView()
    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(getCapacitySummary).not.toHaveBeenCalled()

    await openColumnSettings(wrapper)
    await clickColumnToggle(wrapper, 'Usage')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getUsageSummary).toHaveBeenCalledWith()
    expect(getCapacitySummary).not.toHaveBeenCalled()

    await clickColumnToggle(wrapper, 'Capacity')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)
    await clickColumnToggle(wrapper, 'Billing Type')
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
  })

  it('restores default order and fetches hidden summaries when reset from the settings panel', async () => {
    saveColumns(['billing_type', 'usage', 'capacity'], ['actions', 'id'])
    const wrapper = await mountView()
    expect(getUsageSummary).not.toHaveBeenCalled()
    expect(getCapacitySummary).not.toHaveBeenCalled()

    await openColumnSettings(wrapper)
    const reset = wrapper.findAll('button').find(button => button.text() === 'Reset columns')!
    await reset.trigger('click')
    await flushPromises()
    expect(columnKeys(wrapper)).toEqual(defaultColumns)
    expect(getUsageSummary).toHaveBeenCalledTimes(1)
    expect(getCapacitySummary).toHaveBeenCalledTimes(1)
    expect(JSON.parse(localStorage.getItem(storageKey)!)).toEqual({ order: [], hidden: [] })
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
})
