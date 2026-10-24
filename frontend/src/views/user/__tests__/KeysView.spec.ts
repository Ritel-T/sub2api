import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey, ApiKeyConcurrencySnapshot, Group } from '@/types'
import { keysAPI } from '@/api'
import KeysView from '../KeysView.vue'

enableAutoUnmount(afterEach)

const {
  listKeys,
  getKeyById,
  routeState,
  routerPush,
  routerReplace,
  createKey,
  updateKey,
  getConcurrency,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  getKeyById: vi.fn(),
  routeState: { path: '/keys', query: {} as Record<string, string> },
  routerPush: vi.fn(),
  routerReplace: vi.fn(),
  createKey: vi.fn(),
  updateKey: vi.fn(),
  getConcurrency: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.concurrencyAndWaiting': 'Concurrency / Waiting',
  'keys.concurrencyCount': 'Concurrency',
  'keys.waitingCount': 'Waiting',
  'keys.queueFull': 'Full',
  'keys.queueOff': 'Queuing globally disabled',
  'keys.queuePolicy': 'This key allows {max} waiting requests for up to {seconds} seconds.',
  'keys.queuePolicyOff': 'Queuing globally disabled; new requests are rejected at the key limit.',
  'keys.queueNotApplicable': 'No key-level queue.',
  'keys.queuePolicyLoading': 'Loading shared queue settings…',
  'keys.queuePolicyUnavailable': 'Shared queue settings unavailable.',
  'keys.concurrencyLoading': 'Loading',
  'keys.concurrencyUnavailable': 'Statistics unavailable',
  'keys.concurrencyStale': 'Not updated',
  'keys.noAdditionalConcurrencyLimit': 'No additional limit',
  'keys.concurrencyLimitInvalid': 'Enter a nonnegative whole number for the concurrency limit.',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    getById: getKeyById,
    create: createKey,
    update: updateKey,
    getConcurrency,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('vue-router', () => ({ useRoute: () => routeState, useRouter: () => ({ push: routerPush, replace: routerReplace }) }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params: Record<string, unknown> = {}) =>
        (messages[key] ?? key).replace(/\{(\w+)\}/g, (_, name) => String(params[name] ?? name)),
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  concurrency_limit: 0,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: { columns: Array, data: Array, selectedKeys: Array, selectable: { type: Boolean, default: true }, busyKeys: Object, rename: Function, sortKey: String, sortOrder: String },
  emits: ['sort', 'update:selectedKeys', 'column-reorder'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row">{{ row.name }}</slot>
        <div data-test="key-status"><slot name="cell-status" :value="row.status" :row="row" /></div>
        <div data-test="key-group"><slot name="cell-group" :row="row" /></div>
        <slot name="cell-actions" :row="row" />
        <div data-test="current-concurrency">
          <slot v-if="$slots['details-extra']" name="details-extra" :row="row" />
          <slot v-else name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const InfiniteListSentinelStub = {
  name: 'InfiniteListSentinel',
  props: ['loading', 'error', 'hasMore'],
  emits: ['load-more', 'retry'],
  template: `
    <div>
      <button data-test="retry-page" @click="$emit('retry')">Retry</button>
      <button data-test="load-more" @click="$emit('load-more')">More</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        KeyWorkspaceTable: DataTableStub,
        ColumnSettings: {
          name: 'ColumnSettings',
          props: ['columns', 'hiddenColumns', 'requiredColumns'],
          emits: ['toggle', 'reorder', 'reset'],
          template: '<div data-test="column-settings" />'
        },
        InfiniteListSentinel: InfiniteListSentinelStub,
        BaseDialog: {
          props: ['show', 'title'],
          emits: ['close'],
          template: '<div v-if="show" role="dialog"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /><slot name="footer" /></div>',
        },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        BulkEditKeysModal: true,
        EndpointPopover: true,
        GroupBadge: {
          name: 'GroupBadge',
          props: ['name', 'rateMultiplier', 'userRateMultiplier'],
          template: '<span>{{ name }} {{ userRateMultiplier ?? rateMultiplier }}x</span>',
        },
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

const snapshot = (max = 7, waiting = 2): ApiKeyConcurrencySnapshot => ({
  queue_policy: { max_waiting: max, timeout_seconds: 12 },
  items: [{ id: 1, current_concurrency: 3, current_waiting: waiting }],
})

const deferred = <T,>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const setKeys = (...keys: ApiKey[]) => listKeys.mockResolvedValue({
  items: keys, total: keys.length, page: 1, page_size: 20, pages: 2,
})

describe('user KeysView column settings', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.clear()

    routeState.query = {}
    routerPush.mockReset()
    routerReplace.mockReset()
    getKeyById.mockReset().mockResolvedValue(createApiKey())
    listKeys.mockReset()
    createKey.mockReset().mockResolvedValue(createApiKey())
    updateKey.mockReset().mockResolvedValue(createApiKey())
    vi.mocked(keysAPI.toggleStatus).mockReset().mockResolvedValue(createApiKey())
    getConcurrency.mockReset().mockResolvedValue({
      queue_policy: { max_waiting: 7, timeout_seconds: 12 },
      items: [{ id: 1, current_concurrency: 3, current_waiting: 2 }],
    })
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
  })

  it('opens the real create form from the overview link and removes only its action query', async () => {
    routeState.query = { create: '1', preserved: 'yes' }
    const wrapper = await mountView()
    expect(wrapper.find('#key-form').exists()).toBe(true)
    expect(routerReplace).toHaveBeenCalledWith({ path: '/keys', query: { preserved: 'yes' } })
    expect(createKey).not.toHaveBeenCalled()
  })

  it('opens an authenticated key chooser without selecting a key or showing configuration', async () => {
    routeState.query = { connect: '1' }
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="choose-connection-key-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="connect-manual"]').exists()).toBe(false)
    expect(createKey).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="choose-connection-key-1"]').trigger('click')
    expect(wrapper.find('[data-testid="connect-manual"]').exists()).toBe(true)
  })

  it('resolves a requested key through getById even when absent from the current list', async () => {
    routeState.query = { connect: '1', key: '73' }
    getKeyById.mockResolvedValue({ ...createApiKey(), id: 73, name: 'Other page key' })
    const wrapper = await mountView()
    expect(getKeyById).toHaveBeenCalledWith(73)
    expect(wrapper.get('[role="dialog"]').text()).toContain('Other page key')
    await wrapper.get('[data-testid="connect-manual"]').trigger('click')
    expect(wrapper.findComponent({ name: 'UseKeyModal' }).props('apiKey')).toBe('sk-test-key')
  })

  it('does not configure a stale key when a directed API lookup is denied', async () => {
    routeState.query = { connect: '1', key: '73' }
    getKeyById.mockRejectedValue(new Error('Key unavailable'))
    const wrapper = await mountView()
    expect(wrapper.get('[role="alert"]').text()).toBe('Key unavailable')
    expect(wrapper.find('[data-testid="connect-manual"]').exists()).toBe(false)
  })

  it('connects only the returned persisted key after successful creation', async () => {
    createKey.mockResolvedValue({ ...createApiKey(), id: 27, group_id: 42, name: 'Created by server', key: 'sk-real-response' })
    const wrapper = await mountView()
    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('New key')
    await wrapper.getComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 42)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(createKey).toHaveBeenCalledOnce()
    expect(wrapper.get('[role="dialog"]').text()).toContain('Created by server')
    expect(wrapper.find('#key-form').exists()).toBe(false)
    await wrapper.get('[data-testid="connect-manual"]').trigger('click')
    expect(wrapper.findComponent({ name: 'UseKeyModal' }).props('apiKey')).toBe('sk-real-response')
  })

  it('keeps failed creates editable and never opens connection on failure', async () => {
    createKey.mockRejectedValue(new Error('Denied'))
    const wrapper = await mountView()
    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await wrapper.getComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 42)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('#key-form').exists()).toBe(true)
    expect(wrapper.find('[data-testid="connect-manual"]').exists()).toBe(false)
    expect(showError).toHaveBeenCalled()
  })

  it('leaves edits in the existing list and passes key context into request records', async () => {
    setKeys({ ...createApiKey(), group_id: 42 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="connect-manual"]').exists()).toBe(false)
    await getButtonByText(wrapper, 'Requests').trigger('click')
    expect(routerPush).toHaveBeenCalledWith({ path: '/usage', query: { view: 'records', api_key_id: '1' } })
  })

  it('supports key_id deep links from the overview', async () => {
    routeState.query = { connect: '1', key_id: '73' }
    const wrapper = await mountView()
    expect(getKeyById).toHaveBeenCalledWith(73)
    expect(wrapper.find('[data-testid="connect-manual"]').exists()).toBe(true)
  })

  it('loads another page of real keys while preserving chooser search', async () => {
    routeState.query = { connect: '1' }
    const wrapper = await mountView()
    await wrapper.get('#connection-key-search').setValue('Billing app')
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(), id: 42, name: 'Billing app' }], total: 12, page: 1, page_size: 10, pages: 2 })
    await wrapper.get('#connection-key-search').element.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(listKeys).toHaveBeenLastCalledWith(1, 10, expect.objectContaining({ search: 'Billing app' }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(), id: 43, name: 'Second billing key' }], total: 12, page: 2, page_size: 10, pages: 2 })
    await wrapper.get('#connection-key-search').setValue('Unsubmitted search')
    await wrapper.get('[role="dialog"] [data-test="load-more"]').trigger('click')
    await flushPromises()
    expect(listKeys).toHaveBeenLastCalledWith(2, 10, expect.objectContaining({ search: 'Billing app' }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.find('[data-testid="choose-connection-key-42"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="choose-connection-key-43"]').exists()).toBe(true)
    expect(wrapper.findAllComponents({ name: 'InfiniteListSentinel' }).at(-1)!.props('hasMore')).toBe(false)
  })


  it('retries a failed chooser append and rejects obsolete results after a new search', async () => {
    routeState.query = { connect: '1' }
    const wrapper = await mountView()
    const vm = wrapper.vm as any
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), id: 42 }], total: 12, page: 1, pages: 2 })
    await vm.loadConnectionKeys(); await flushPromises()
    listKeys.mockRejectedValueOnce(new Error('temporarily unavailable'))
    await wrapper.get('[role="dialog"] [data-test="load-more"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="choose-connection-key-42"]').exists()).toBe(true)
    expect(wrapper.findAllComponents({ name: 'InfiniteListSentinel' }).at(-1)!.props('error')).toBe('temporarily unavailable')
    let finishOld!: (data: any) => void
    listKeys.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
    await wrapper.get('[role="dialog"] [data-test="retry-page"]').trigger('click'); await flushPromises()
    expect(listKeys.mock.lastCall?.[0]).toBe(2)
    const oldSignal = listKeys.mock.lastCall?.[3]?.signal as AbortSignal
    await wrapper.get('#connection-key-search').setValue('new')
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), id: 99 }], total: 1, page: 1, pages: 1 })
    await vm.loadConnectionKeys(); await flushPromises()
    finishOld({ items: [{ ...createApiKey(), id: 43 }], total: 12, page: 2, pages: 2 }); await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.find('[data-testid="choose-connection-key-99"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="choose-connection-key-42"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="choose-connection-key-43"]').exists()).toBe(false)
  })

  it('clears a previously set quota when its amount is zero', async () => {
    setKeys({ ...createApiKey(), group_id: 42, quota: 25 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('input[placeholder="keys.quotaAmountPlaceholder"]').setValue(0)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ quota: 0 }))
  })

  it('hides CCS import when the administrator removes all platform profiles', async () => {
    getPublicSettings.mockResolvedValue({ ccs_import_config: { profiles: [] } })
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="ccs-import-1"]').exists()).toBe(false)
  })

  it('imports the clicked key using the saved model and metadata templates', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    getPublicSettings.mockResolvedValue({
      api_base_url: 'https://api.example.com', site_name: 'Ritel SI',
      ccs_import_config: { profiles: [{
        id: 'my-claude', label: 'Claude Code', platform: 'anthropic', app: 'claude',
        parameters: { model: 'configured-model', name: '{{siteName}} / {{keyName}}', usageAutoInterval: 0 }
      }] }
    })
    const wrapper = await mountView()
    await wrapper.get('[data-testid="connect-key-row"]').trigger('click')
    await wrapper.get('[data-testid="ccs-import-1"]').trigger('click')
    expect(open).toHaveBeenCalledOnce()
    const params = new URL(String(open.mock.calls[0]?.[0])).searchParams
    expect(params.get('model')).toBe('configured-model')
    expect(params.get('name')).toBe('Ritel SI / test-key')
    expect(params.get('apiKey')).toBe('sk-test-key')
    expect(params.get('usageAutoInterval')).toBe('0')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it('lets the user choose among matching saved profiles and uses that choice', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    getPublicSettings.mockResolvedValue({
      ccs_import_config: { profiles: [
        { id: 'claude', label: 'Claude Code', platform: 'anthropic', app: 'claude', parameters: {} },
        { id: 'opencode', label: 'OpenCode', platform: 'anthropic', app: 'opencode', parameters: { model: 'model-b' } },
        { id: 'other-platform', label: 'Codex', platform: 'openai', app: 'codex', parameters: {} }
      ] }
    })
    const wrapper = await mountView()
    await wrapper.get('[data-testid="connect-key-row"]').trigger('click')
    await wrapper.get('[data-testid="ccs-import-1"]').trigger('click')
    expect(open).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="ccs-profile-other-platform"]').exists()).toBe(false)
    await wrapper.get('[data-testid="ccs-profile-opencode"]').trigger('click')
    const params = new URL(String(open.mock.calls[0]?.[0])).searchParams
    expect(params.get('app')).toBe('opencode')
    expect(params.get('model')).toBe('model-b')
    expect(params.get('apiKey')).toBe('sk-test-key')
    expect(params.get('apiFormat')).toBe('anthropic')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it.each([
    { initialStatus: 'quota_exhausted', status: 'active', formStatus: 'active' },
    { initialStatus: 'inactive', status: 'inactive', formStatus: 'inactive' },
    { initialStatus: 'active', status: 'active', formStatus: 'inactive' },
  ] as const)('syncs quota reset from $initialStatus to $status with form status $formStatus', async ({ initialStatus, status, formStatus }) => {
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: initialStatus,
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status, quota_used: 0 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    const statusSelect = wrapper.findAllComponents({ name: 'Select' })
      .find((select) => select.props('options').length === 2 &&
        select.props('options')[0].value === 'active')!
    statusSelect.vm.$emit('update:modelValue', 'inactive')
    await wrapper.get('button[title="keys.resetQuotaUsed"]').trigger('click')
    const confirmation = wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')!
    confirmation.vm.$emit('confirm')
    await flushPromises()

    expect(updateKey).toHaveBeenNthCalledWith(1, key.id, { reset_quota: true })
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0])
      .toMatchObject({ status, quota_used: 0 })
    expect(statusSelect.props('modelValue')).toBe(formStatus)
    expect((wrapper.get('[data-tour="key-form-name"]').element as HTMLInputElement).value)
      .toBe('Unsaved name')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, expect.objectContaining({ name: 'Unsaved name', status: formStatus }))
    wrapper.unmount()
  })

  it('renames through a minimal PUT, keeps the saved name while pending, and ignores duplicate edits', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    const key = table.props('data')[0] as ApiKey
    const pending = deferred<ApiKey>()
    updateKey.mockReturnValueOnce(pending.promise)
    const rename = table.props('rename') as (row: ApiKey, name: string) => Promise<boolean>
    const result = rename(key, '  Production key  ')
    await nextTick()
    expect(updateKey).toHaveBeenCalledOnce()
    expect(updateKey).toHaveBeenCalledWith(key.id, { name: 'Production key' })
    expect(table.props('busyKeys').has(key.id)).toBe(true)
    expect(table.props('data')[0].name).toBe('test-key')
    expect(await rename(key, 'Duplicate request')).toBe(false)
    expect(updateKey).toHaveBeenCalledOnce()
    setKeys({ ...key, name: 'Production key' })
    pending.resolve({ ...key, name: 'Production key' })
    expect(await result).toBe(true)
    await flushPromises()
    expect(table.props('data')[0].name).toBe('Production key')
    expect(table.props('busyKeys').has(key.id)).toBe(false)
  })

  it('retains the real name after a failed rename and allows an explicit retry', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    const key = table.props('data')[0] as ApiKey
    const calls = listKeys.mock.calls.length
    const rename = table.props('rename') as (row: ApiKey, name: string) => Promise<boolean>
    updateKey.mockRejectedValueOnce({ response: { data: { detail: 'Name rejected' } } })
    expect(await rename(key, 'New name')).toBe(false)
    await flushPromises()
    expect(table.props('data')[0].name).toBe('test-key')
    expect(table.props('busyKeys').has(key.id)).toBe(false)
    expect(listKeys).toHaveBeenCalledTimes(calls)
    expect(showError).toHaveBeenCalled()
    updateKey.mockResolvedValueOnce({ ...key, name: 'New name' })
    setKeys({ ...key, name: 'New name' })
    expect(await rename(key, 'New name')).toBe(true)
    await flushPromises()
    expect(table.props('data')[0].name).toBe('New name')
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, { name: 'New name' })
  })

  it.each(['quota_exhausted', 'expired'] as const)('preserves the %s status without exposing it as an ordinary enable action', async state => {
    setKeys({ ...createApiKey(), status: state })
    const wrapper = await mountView()
    expect(wrapper.get('[data-test="key-status"]').text()).toContain(messages[`keys.status.${state}`])
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
    await wrapper.get('[data-test="key-status"] button').trigger('click')
    expect(wrapper.find('#key-form').exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0].status).toBe(state)
    expect(keysAPI.toggleStatus).not.toHaveBeenCalled()
    expect(updateKey).not.toHaveBeenCalled()
  })

  it.each(['active', 'inactive'] as const)('switches %s once while pending and keeps the original status after a rejected request', async initialStatus => {
    const key = { ...createApiKey(), status: initialStatus }
    setKeys(key)
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    const pending = deferred<ApiKey>()
    vi.mocked(keysAPI.toggleStatus).mockReturnValueOnce(pending.promise)
    const target = initialStatus === 'active' ? 'inactive' : 'active'
    const button = wrapper.get('[role="switch"]')
    expect(button.attributes('aria-checked')).toBe(initialStatus === 'active' ? 'true' : 'false')
    await button.trigger('click')
    await button.trigger('click')
    expect(keysAPI.toggleStatus).toHaveBeenCalledOnce()
    expect(keysAPI.toggleStatus).toHaveBeenCalledWith(key.id, target)
    expect(table.props('busyKeys').has(key.id)).toBe(true)
    expect(table.props('data')[0].status).toBe(initialStatus)
    pending.reject(new Error('Denied'))
    await flushPromises()
    expect(table.props('data')[0].status).toBe(initialStatus)
    expect(table.props('busyKeys').has(key.id)).toBe(false)
    expect(showError).toHaveBeenCalled()
    vi.mocked(keysAPI.toggleStatus).mockResolvedValueOnce({ ...key, status: target })
    setKeys({ ...key, status: target })
    await button.trigger('click')
    await flushPromises()
    expect(keysAPI.toggleStatus).toHaveBeenCalledTimes(2)
    expect(table.props('data')[0].status).toBe(target)
    expect(button.attributes('aria-checked')).toBe(target === 'active' ? 'true' : 'false')
  })

  it('shows all API key columns with frequently checked fields first', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'id', 'name', 'key', 'group', 'usage', 'current_concurrency', 'status', 'actions',
      'rate_limit', 'last_used_at', 'expires_at', 'last_used_ip', 'created_at',
    ])
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('opens bulk editing with only selected visible keys', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    expect(table.props('selectable')).toBe(true)
    table.vm.$emit('update:selectedKeys', [1, 99])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    const modal = wrapper.findComponent({ name: 'BulkEditKeysModal' })
    expect(modal.props('show')).toBe(true)
    expect(modal.props('selectedKeys').map((key: ApiKey) => key.id)).toEqual([1])
    wrapper.unmount()
  })

  it.each(['filter', 'sort'])('clears selection on %s changes', async (change) => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    if (change === 'filter') {
      wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')

    } else {
      table.vm.$emit('sort', 'created_at', 'asc')
    }
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    expect(wrapper.find('[data-test="bulk-edit-keys"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes successful keys from the selection and refreshes the table', async () => {
    listKeys.mockResolvedValue({
      items: [createApiKey(), { ...createApiKey(), id: 2, name: 'Second' }],
      total: 2, pages: 1
    })
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1, 2])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    wrapper.findComponent({ name: 'BulkEditKeysModal' }).vm.$emit('updated', [1])
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(table.props('selectedKeys')).toEqual([2])
    wrapper.unmount()
  })

  it('drops keys that are no longer visible after a refresh', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    listKeys.mockResolvedValue({ items: [], total: 0, pages: 0 })
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    wrapper.unmount()
  })

  it('promotes a detail field to the main row and persists placement without removing data', async () => {
    const wrapper = await mountView()
    const settings = wrapper.findComponent({ name: 'ColumnSettings' })
    const table = wrapper.findComponent({ name: 'DataTable' })
    const rateColumn = () => (table.props('columns') as Array<{ key: string; primary: boolean }>).find(column => column.key === 'rate_limit')
    expect(rateColumn()?.primary).toBe(false)
    settings.vm.$emit('toggle', 'rate_limit')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(rateColumn()?.primary).toBe(true)
    expect(JSON.parse(localStorage.getItem('table-columns:keys:v1') || '{}').primary).toEqual({ rate_limit: true })
    settings.vm.$emit('toggle', 'rate_limit')
    await nextTick()
    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(rateColumn()?.primary).toBe(false)
  })

  it('shows the last used IP by default', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1, page: 1, page_size: 20, pages: 1,
    })
    const wrapper = await mountView()
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column order and main-row placement while retaining detail fields', async () => {
    localStorage.setItem('table-columns:keys:v1', JSON.stringify({
      version: 2, order: ['status', 'actions', 'name'], primary: { group: false, created_at: true },
    }))
    const wrapper = await mountView()
    expect(visibleColumnKeys(wrapper).slice(0, 4)).toEqual(['status', 'actions', 'name', 'id'])
    const columns = wrapper.findComponent({ name: 'DataTable' }).props('columns') as Array<{ key: string; primary: boolean }>
    expect(columns.find(column => column.key === 'group')?.primary).toBe(false)
    expect(columns.find(column => column.key === 'created_at')?.primary).toBe(true)
    expect(visibleColumnKeys(wrapper)).toHaveLength(13)
  })

  it('keeps the identifier visible while accepting saved and newly requested positions', async () => {
    localStorage.setItem('table-columns:keys:v1', JSON.stringify({ order: ['name', 'id', 'actions'], hidden: ['id'] }))
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    const settings = wrapper.findComponent({ name: 'ColumnSettings' })
    expect(visibleColumnKeys(wrapper).indexOf('id')).toBe(1)
    settings.vm.$emit('toggle', 'id')
    const requested = [...visibleColumnKeys(wrapper).filter(key => key !== 'id'), 'id']
    table.vm.$emit('column-reorder', requested)
    await nextTick()
    expect(visibleColumnKeys(wrapper).at(-1)).toBe('id')
    expect(visibleColumnKeys(wrapper)).toHaveLength(13)
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('1')
  })

  it('ignores the obsolete automatic hiding preferences', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')
    const wrapper = await mountView()
    expect(visibleColumnKeys(wrapper)).toHaveLength(13)
    expect(visibleColumnKeys(wrapper)).toContain('group')
    expect(visibleColumnKeys(wrapper)).toContain('id')
  })

  it('lets the table reorder required columns without changing data sorting', async () => {
    const wrapper = await mountView()
    const calls = listKeys.mock.calls.length
    const keys = visibleColumnKeys(wrapper)
    const order = ['id', 'actions', ...keys.filter(key => key !== 'actions' && key !== 'id')]
    wrapper.findComponent({ name: 'DataTable' }).vm.$emit('column-reorder', order)
    await nextTick()
    expect(visibleColumnKeys(wrapper)).toEqual(order)
    expect(JSON.parse(localStorage.getItem('table-columns:keys:v1') || '{}').order).toEqual(order)
    expect(listKeys).toHaveBeenCalledTimes(calls)
  })

  it('offers every column for reordering and resets to all visible defaults', async () => {
    const wrapper = await mountView()
    const settings = wrapper.findComponent({ name: 'ColumnSettings' })
    expect(settings.props('columns').map((column: { key: string }) => column.key)).toEqual(visibleColumnKeys(wrapper))
    expect(settings.props('requiredColumns')).toEqual(['id', 'name', 'actions'])
    settings.vm.$emit('toggle', 'rate_limit')
    settings.vm.$emit('reorder', ['actions', 'name'])
    await nextTick()
    expect(visibleColumnKeys(wrapper).slice(0, 2)).toEqual(['id', 'actions'])
    settings.vm.$emit('reset')
    await nextTick()
    expect(visibleColumnKeys(wrapper)).toHaveLength(13)
    expect(visibleColumnKeys(wrapper)[0]).toBe('id')
  })

  it.each([0, 3])('keeps the concurrency badge at %s for an unlimited key', async (current) => {
    getConcurrency.mockResolvedValue({ ...snapshot(), items: [
      { id: 1, current_concurrency: current, current_waiting: 0 },
    ] })
    const wrapper = await mountView()
    const cell = wrapper.get('[data-testid="detail-concurrency"]')
    const badge = cell.get('[title="Concurrency"]')
    expect(badge.find('svg').exists()).toBe(true)
    expect(badge.classes()).toContain(current === 0 ? 'bg-gray-100' : 'bg-emerald-50')
    expect(cell.text()).toBe(`Concurrency ${current}`)
    expect(cell.text()).not.toMatch(/\/|No additional limit|Waiting/)
  })

  it('shows current / max for a key with an additional concurrency limit', async () => {
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), concurrency_limit: 8 }], total: 1 })
    const wrapper = await mountView()
    const cell = wrapper.get('[data-testid="detail-concurrency"]')
    expect(cell.text()).toContain('Concurrency 3 / 8')
    expect(cell.text()).toContain('Waiting 2 / 7')
  })

  it.each([[0, 'gray'], [2, 'emerald'], [4, 'amber'], [5, 'amber']])(
    'distinguishes concurrency %s against limit 4', async (current, color) => {
      setKeys({ ...createApiKey(), concurrency_limit: 4 })
      getConcurrency.mockResolvedValue({ ...snapshot(), items: [
        { id: 1, current_concurrency: current as number, current_waiting: 0 },
      ] })
      const wrapper = await mountView()
      const badge = wrapper.get('[data-test="current-concurrency"] [title="Concurrency"]')
      expect(badge.text()).toContain(`Concurrency ${current} / 4`)
      expect(badge.classes().join(' ')).toContain(`text-${color}-`)
    }
  )

  it('keeps the badge and switches its denominator after saving a changed limit', async () => {
    setKeys({ ...createApiKey(), group_id: 42, concurrency_limit: 0 })
    const wrapper = await mountView()
    for (const limit of [4, 0]) {
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      if (limit === 4) expect(wrapper.get('#key-queue-policy').text()).toBe('No key-level queue.')
      await wrapper.get('#key-concurrency-limit').setValue(limit)
      setKeys({ ...createApiKey(), group_id: 42, concurrency_limit: limit })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      const cell = wrapper.get('[data-testid="detail-concurrency"]')
      expect(cell.get('[title="Concurrency"]').find('svg').exists()).toBe(true)
      const text = cell.text()
      if (limit === 0) expect(text).toBe('Concurrency 3')
      else expect(text).toContain('Concurrency 3 / 4')
    }
  })

  it.each([8, 0, ''])('creates a key with concurrency input %s and resets the form', async (input) => {
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect((wrapper.get('#key-concurrency-limit').element as HTMLInputElement).value).toBe('0')
    await wrapper.get('[data-tour="key-form-name"]').setValue('new-key')
    await wrapper.getComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 42)
    await wrapper.get('#key-concurrency-limit').setValue(input)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(createKey).toHaveBeenCalledWith('new-key', 42, undefined, [], [], 0, undefined,
      { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }, Number(input), [42])
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect((wrapper.get('#key-concurrency-limit').element as HTMLInputElement).value).toBe('0')
  })

  it.each([12, 0, ''])('loads the saved limit and updates concurrency input %s', async (input) => {
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), group_id: 42, concurrency_limit: 8 }], total: 1 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect((wrapper.get('#key-concurrency-limit').element as HTMLInputElement).value).toBe('8')
    await wrapper.get('#key-concurrency-limit').setValue(input)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({ concurrency_limit: Number(input) }))
  })

  it.each([
    ['create', -1], ['create', 1.5], ['edit', -1], ['edit', 1.5],
  ])('rejects invalid concurrency in %s mode: %s', async (mode, input) => {
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), group_id: 42 }], total: 1 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, mode === 'edit' ? 'common.edit' : 'Create API Key').trigger('click')
    await wrapper.getComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 42)
    await wrapper.get('#key-concurrency-limit').setValue(input)
    expect(wrapper.get('#key-concurrency-limit').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('#key-concurrency-error').text()).toBe(messages['keys.concurrencyLimitInvalid'])
    await wrapper.get('#key-form').trigger('submit')
    expect(showError).toHaveBeenCalledWith(messages['keys.concurrencyLimitInvalid'])
    expect(createKey).not.toHaveBeenCalled()
    expect(updateKey).not.toHaveBeenCalled()
  })

  it('keeps the edit form and entered limit when the server rejects an update', async () => {
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), group_id: 42, concurrency_limit: 8 }], total: 1 })
    updateKey.mockRejectedValueOnce({ response: { data: { detail: 'Concurrency limit rejected' } } })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('#key-concurrency-limit').setValue(12)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('Concurrency limit rejected')
    expect(showSuccess).not.toHaveBeenCalled()
    expect((wrapper.get('#key-concurrency-limit').element as HTMLInputElement).value).toBe('12')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it('shows actual shared policy as read-only metadata and never submits it', async () => {
    setKeys({ ...createApiKey(), group_id: 42, concurrency_limit: 1 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.get('#key-queue-policy').text()).toBe('This key allows 7 waiting requests for up to 12 seconds.')
    expect(wrapper.get('#key-concurrency-limit').attributes('aria-describedby')).toContain('key-queue-policy')
    expect(wrapper.find('#key-queue-policy input').exists()).toBe(false)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = updateKey.mock.calls[0][1]
    expect(payload).toHaveProperty('concurrency_limit', 1)
    expect(Object.keys(payload).some(key => /queue|waiting|timeout/.test(key))).toBe(false)
  })

  it('loads policy without IDs on an empty page and keeps it out of the create payload', async () => {
    setKeys()
    getConcurrency.mockResolvedValue({ ...snapshot(), items: [] })
    const wrapper = await mountView()
    expect(getConcurrency).toHaveBeenCalledWith([], expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect(wrapper.get('#key-queue-policy').text()).toBe('No key-level queue.')
    await wrapper.get('#key-concurrency-limit').setValue(2)
    expect(wrapper.get('#key-queue-policy').text()).toContain('7 waiting requests')
    await wrapper.get('[data-tour="key-form-name"]').setValue('new-key')
    await wrapper.getComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', 42)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(createKey).toHaveBeenCalledWith('new-key', 42, undefined, [], [], 0, undefined,
      { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }, 2, [42])
  })

  it('distinguishes unknown statistics and policy from zero and still permits saving', async () => {
    setKeys({ ...createApiKey(), group_id: 42, concurrency_limit: 1 })
    const request = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(request.promise)
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.get('#key-queue-policy').text()).toBe('Loading shared queue settings…')
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).toContain('Concurrency — / 1')
    request.reject({ response: { status: 503 } })
    await flushPromises()
    expect(wrapper.get('#key-queue-policy').text()).toBe('Shared queue settings unavailable.')
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).not.toContain('/ 20')
    expect(showError).not.toHaveBeenCalled()
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenCalled()
  })

  it('uses one actual policy for independent counts, including over-capacity and unlimited keys', async () => {
    setKeys(
      { ...createApiKey(), concurrency_limit: 1 },
      { ...createApiKey(), id: 2, concurrency_limit: 4 },
      { ...createApiKey(), id: 3, concurrency_limit: 0 },
    )
    getConcurrency.mockResolvedValue({ ...snapshot(), items: [
      { id: 1, current_concurrency: 1, current_waiting: 2 },
      { id: 2, current_concurrency: 4, current_waiting: 9 },
      { id: 3, current_concurrency: 2, current_waiting: 0 },
    ] })
    const wrapper = await mountView()
    const rows = wrapper.findAll('[data-testid="detail-concurrency"]').map(row => row.text())
    expect(rows[0]).toContain('Waiting 2 / 7')
    expect(rows[0]).not.toContain('Full')
    expect(rows[1]).toContain('Waiting 9 / 7 · Full')
    expect(rows[2]).toBe('Concurrency 2')
  })

  it('shows global-off policy and actual residual waiting without a zero denominator', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    getConcurrency.mockResolvedValue(snapshot(0, 2))
    const wrapper = await mountView()
    const text = wrapper.get('[data-testid="detail-concurrency"]').text()
    expect(text).toContain('Queuing globally disabled')
    expect(text).toContain('Waiting 2')
    expect(text).not.toContain('/ 0')
    expect(text).not.toContain('Full')
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.get('#key-queue-policy').text()).toBe(messages['keys.queuePolicyOff'])
  })

  it('does not treat a missing key limit or missing counts as an unlimited idle key', async () => {
    const key = { ...createApiKey(), concurrency_limit: undefined } as unknown as ApiKey
    setKeys(key)
    getConcurrency.mockResolvedValue({ ...snapshot(), items: [] })
    const wrapper = await mountView()
    const text = wrapper.get('[data-testid="detail-concurrency"]').text()
    expect(text).toContain('Concurrency —')
    expect(text).toContain('Statistics unavailable')
    expect(text).not.toContain('No additional limit')
  })

  it('shows unknown waiting against the actual policy when a requested count is missing', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    getConcurrency.mockResolvedValue({ ...snapshot(), items: [] })
    const wrapper = await mountView()
    const text = wrapper.get('[data-testid="detail-concurrency"]').text()
    expect(text).toContain('Concurrency — / 1')
    expect(text).toContain('Waiting — / 7')
    expect(text).toContain('Statistics unavailable')
    expect(text).not.toContain('Full')
  })

  it('polls after completion without reloading the list, usage, row order or modal input', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('#key-concurrency-limit').setValue(6)
    const request = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(request.promise)
    await vi.advanceTimersByTimeAsync(5000)
    expect(getConcurrency).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(15000)
    expect(getConcurrency).toHaveBeenCalledTimes(2)
    request.resolve(snapshot(4, 4))
    await flushPromises()
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).toContain('Waiting 4 / 4 · Full')
    expect(wrapper.get('#key-queue-policy').text()).toContain('4 waiting requests')
    expect((wrapper.get('#key-concurrency-limit').element as HTMLInputElement).value).toBe('6')
    expect(listKeys).toHaveBeenCalledTimes(1)
    expect(getDashboardApiKeysUsage).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(4999)
    expect(getConcurrency).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(getConcurrency).toHaveBeenCalledTimes(3)
  })

  it('marks old counts and policy stale on failure and removes the full marker until recovery', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    getConcurrency.mockResolvedValue(snapshot(7, 7))
    const wrapper = await mountView()
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).toContain('Full')
    getConcurrency.mockRejectedValueOnce({ response: { status: 503 } })
    await vi.advanceTimersByTimeAsync(5000)
    const text = wrapper.get('[data-testid="detail-concurrency"]').text()
    expect(text).toContain('Waiting 7 / 7')
    expect(text).toContain('Not updated')
    expect(text).not.toContain('Full')
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.get('#key-queue-policy').text()).toContain('Not updated')
    expect(showError).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).toContain('Full')
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).not.toContain('Not updated')
  })

  it('aborts on hide, ignores a late reply, and resumes only after the old request settles', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    const request = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(request.promise)
    const wrapper = await mountView()
    const signal = getConcurrency.mock.calls[0][1].signal as AbortSignal
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(15000)
    expect(getConcurrency).toHaveBeenCalledTimes(1)
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getConcurrency).toHaveBeenCalledTimes(1)
    request.resolve(snapshot(99, 99))
    await flushPromises()
    expect(getConcurrency).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="detail-concurrency"]').text()).toContain('Waiting 2 / 7')
    expect(wrapper.text()).not.toContain('99')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getConcurrency).toHaveBeenCalledTimes(2)
  })

  it('cancels statistics during append and refreshes counts for the accumulated keys', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    const request = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(request.promise)
    const wrapper = await mountView()
    const signal = getConcurrency.mock.calls[0][1].signal as AbortSignal
    setKeys({ ...createApiKey(), id: 2, concurrency_limit: 2 })
    getConcurrency.mockResolvedValue({ ...snapshot(4), items: [{ id: 2, current_concurrency: 1, current_waiting: 0 }] })
    await wrapper.get('[data-test="load-more"]').trigger('click')
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(getConcurrency).toHaveBeenCalledTimes(1)
    request.resolve(snapshot(99, 99))
    await flushPromises()
    expect(getConcurrency).toHaveBeenLastCalledWith([1, 2], expect.any(Object))
    const cell = wrapper.findAll('[data-testid="detail-concurrency"]')[1]
    expect(cell.text()).toContain('Concurrency 1 / 2')
    expect(cell.text()).toContain('Waiting 0 / 4')
  })

  it('appends three key batches without losing selection and polls accumulated IDs in batches of 100', async () => {
    const keys = Array.from({ length: 125 }, (_, index) => ({ ...createApiKey(), id: index + 1, name: `Key ${index + 1}` }))
    listKeys.mockImplementation((page: number, size: number) => Promise.resolve({ items: keys.slice((page - 1) * size, page * size), total: 125, pages: 3 }))
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    await wrapper.get('[data-test="load-more"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="load-more"]').trigger('click')
    await flushPromises()
    expect(table.props('data')).toHaveLength(125)
    expect(table.props('selectedKeys')).toEqual([1])
    expect(getConcurrency.mock.calls.slice(-2).map(call => call[0].length)).toEqual([100, 25])
    expect(getDashboardApiKeysUsage.mock.calls.map(call => call[0].length)).toEqual([50, 50, 25])
    expect(wrapper.findComponent({ name: 'InfiniteListSentinel' }).props('hasMore')).toBe(false)
    expect(wrapper.findComponent({ name: 'Pagination' }).exists()).toBe(false)
  })

  it('retains loaded keys when the next batch fails and retries only on request', async () => {
    setKeys(createApiKey())
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    listKeys.mockRejectedValueOnce(new Error('Unavailable'))
    await wrapper.get('[data-test="load-more"]').trigger('click')
    await flushPromises()
    expect(table.props('data').map((key: ApiKey) => key.id)).toEqual([1])
    expect(wrapper.findComponent({ name: 'InfiniteListSentinel' }).props('loading')).toBe(false)
    const calls = listKeys.mock.calls.length
    await wrapper.get('[data-test="load-more"]').trigger('click')
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(calls)
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), id: 2 }], total: 2, pages: 2 })
    await wrapper.get('[data-test="retry-page"]').trigger('click')
    await flushPromises()
    expect(table.props('data').map((key: ApiKey) => key.id)).toEqual([1, 2])
    expect(listKeys.mock.calls.at(-1)?.slice(0, 2)).toEqual([2, 50])
  })

  it('discards an old append after a new search and never mixes its usage statistics', async () => {
    setKeys(createApiKey())
    const wrapper = await mountView()
    const pending = deferred<{ items: ApiKey[]; total: number; pages: number }>()
    listKeys.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-test="load-more"]').trigger('click')
    const signal = listKeys.mock.calls.at(-1)?.[3].signal as AbortSignal
    listKeys.mockResolvedValueOnce({ items: [{ ...createApiKey(), id: 9 }], total: 1, pages: 1 })
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'new')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()
    pending.resolve({ items: [{ ...createApiKey(), id: 2 }], total: 2, pages: 2 })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data').map((key: ApiKey) => key.id)).toEqual([9])
    expect(getDashboardApiKeysUsage.mock.calls.map(call => call[0])).toEqual([[1], [9]])
  })

  it('aborts an in-flight statistics request on unmount without scheduling another', async () => {
    const request = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(request.promise)
    const wrapper = await mountView()
    const signal = getConcurrency.mock.calls[0][1].signal as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    request.resolve(snapshot())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10000)
    expect(getConcurrency).toHaveBeenCalledTimes(1)
  })

  it('reads pages above 100 IDs serially and publishes policy and counts only when complete', async () => {
    setKeys(...Array.from({ length: 101 }, (_, i) => ({ ...createApiKey(), id: i + 1, concurrency_limit: 1 })))
    const first = deferred<ApiKeyConcurrencySnapshot>()
    const second = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = await mountView()
    expect(getConcurrency.mock.calls[0][0]).toHaveLength(100)
    expect(getConcurrency).toHaveBeenCalledTimes(1)
    first.resolve(snapshot(4, 2))
    await flushPromises()
    expect(getConcurrency.mock.calls[1][0]).toEqual([101])
    expect(wrapper.findAll('[data-testid="detail-concurrency"]')[0].text()).toContain('Concurrency — / 1')
    second.resolve({ ...snapshot(4), items: [{ id: 101, current_concurrency: 1, current_waiting: 3 }] })
    await flushPromises()
    const rows = wrapper.findAll('[data-testid="detail-concurrency"]')
    expect(rows[0].text()).toContain('Waiting 2 / 4')
    expect(rows[100].text()).toContain('Waiting 3 / 4')
  })

  it('does not combine counts from batches reporting different policies', async () => {
    setKeys(...Array.from({ length: 101 }, (_, i) => ({ ...createApiKey(), id: i + 1, concurrency_limit: 1 })))
    getConcurrency.mockResolvedValueOnce(snapshot(4)).mockResolvedValueOnce(snapshot(8))
    const wrapper = await mountView()
    const text = wrapper.findAll('[data-testid="detail-concurrency"]')[0].text()
    expect(text).toContain('Concurrency — / 1')
    expect(text).toContain('Shared queue settings unavailable.')
    expect(text).not.toContain('Waiting 2 / 4')
    expect(showError).not.toHaveBeenCalled()
  })

  it('cancels old statistics during a filter change and keeps filters and row order on polling', async () => {
    setKeys({ ...createApiKey(), concurrency_limit: 1 })
    const request = deferred<ApiKeyConcurrencySnapshot>()
    getConcurrency.mockReturnValueOnce(request.promise)
    const wrapper = await mountView()
    const signal = getConcurrency.mock.calls[0][1].signal as AbortSignal
    setKeys(
      { ...createApiKey(), id: 2, concurrency_limit: 1 },
      { ...createApiKey(), id: 3, concurrency_limit: 1 },
    )
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()
    expect(signal.aborted).toBe(true)
    getConcurrency.mockResolvedValue({ ...snapshot(), items: [
      { id: 3, current_concurrency: 8, current_waiting: 6 },
      { id: 2, current_concurrency: 1, current_waiting: 2 },
    ] })
    request.resolve(snapshot(99, 99))
    await flushPromises()
    expect(getConcurrency).toHaveBeenLastCalledWith([2, 3], expect.any(Object))
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.findComponent({ name: 'SearchInput' }).props('modelValue')).toBe('target')
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data').map((key: ApiKey) => key.id)).toEqual([2, 3])
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(getDashboardApiKeysUsage).toHaveBeenCalledTimes(2)
    const rows = wrapper.findAll('[data-testid="detail-concurrency"]')
    expect(rows[0].text()).toContain('Concurrency 1 / 1')
    expect(rows[1].text()).toContain('Concurrency 8 / 1')
  })

  it('does not poll a page mounted in the background until it becomes visible', async () => {
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    await mountView()
    await vi.advanceTimersByTimeAsync(10000)
    expect(getConcurrency).not.toHaveBeenCalled()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(getConcurrency).toHaveBeenCalledTimes(1)
  })

  it.each([
    { key: 'current_concurrency', order: 'asc' },
    { key: 'group', order: 'asc' },
    { key: 'group', order: 'desc' },
  ] as const)('keeps filters and resets pagination and selection when sorting $key $order', async ({ key, order }) => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    await wrapper.get('[data-test="load-more"]').trigger('click')
    await flushPromises()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    expect(table.props('selectedKeys')).toEqual([1])
    expect(visibleColumnMeta(wrapper).find((column) => column.key === key)?.sortable).toBe(true)
    listKeys.mockClear()

    table.vm.$emit('sort', key, order)
    await flushPromises()

    expect(table.props('selectedKeys')).toEqual([])
    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: key,
        sort_order: order,
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  describe('group pricing refresh', () => {
    const group = {
      id: 42, name: 'Example group', platform: 'openai',
      rate_multiplier: 0.3, subscription_type: 'standard',
    } as Group

    const formOption = (wrapper: VueWrapper) => wrapper
      .getComponent('[data-tour="key-form-group"]').props('options')
      .find((option: { value: number }) => option.value === group.id)

    beforeEach(() => {
      setKeys({ ...createApiKey(), group_id: group.id, group })
      getAvailableGroups.mockResolvedValue([group])
      getUserGroupRates.mockResolvedValue({ 42: 1.3 })
    })

    it.each([0.5, 0, null])('refreshes the row after an override changes to %s', async (rate) => {
      const wrapper = await mountView()
      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain('1.3x')
      getUserGroupRates.mockResolvedValue(rate === null ? {} : { 42: rate })

      await wrapper.get('button[title="Refresh"]').trigger('click')
      await flushPromises()

      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain(`${rate ?? 0.3}x`)
      expect(getAvailableGroups).toHaveBeenCalledTimes(2)
    })

    it.each(['create', 'edit'])('refreshes pricing when opening the %s dialog', async (mode) => {
      const wrapper = await mountView()
      getAvailableGroups.mockResolvedValue([{ ...group, rate_multiplier: 0.4 }])
      getUserGroupRates.mockResolvedValue({ 42: 0.5 })

      await getButtonByText(wrapper, mode === 'create' ? 'Create API Key' : 'common.edit').trigger('click')
      await flushPromises()

      expect(formOption(wrapper)).toMatchObject({ rate: 0.4, userRate: 0.5 })
      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain('0.5x')
      expect(getUserGroupRates).toHaveBeenCalledTimes(2)
    })

    it('refreshes the inline group selector without reloading the page', async () => {
      const wrapper = await mountView()
      getUserGroupRates.mockResolvedValue({ 42: 0.5 })

      await wrapper.get('[data-test="key-group"] button').trigger('click')
      await flushPromises()

      const option = wrapper.findComponent({ name: 'GroupOptionItem' })
      expect(option.props('userRateMultiplier')).toBe(0.5)
      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain('0.5x')
    })

    it('keeps the previous pricing together and reports a failed refresh', async () => {
      const wrapper = await mountView()
      getAvailableGroups.mockResolvedValue([{ ...group, rate_multiplier: 0.4 }])
      getUserGroupRates.mockRejectedValueOnce(new Error('Unavailable'))

      await wrapper.get('button[title="Refresh"]').trigger('click')
      await flushPromises()

      expect(showError).toHaveBeenCalledWith('keys.failedToLoad')
      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain('1.3x')
      getUserGroupRates.mockResolvedValue({ 42: 0.5 })
      await wrapper.get('button[title="Refresh"]').trigger('click')
      await flushPromises()
      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain('0.5x')
    })

    it('does not let an older refresh overwrite the dialog pricing', async () => {
      const wrapper = await mountView()
      const olderRates = deferred<Record<number, number>>()
      getUserGroupRates.mockReturnValueOnce(olderRates.promise)
      await wrapper.get('button[title="Refresh"]').trigger('click')

      getUserGroupRates.mockResolvedValue({ 42: 0.5 })
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      await flushPromises()
      olderRates.resolve({ 42: 1.3 })
      await flushPromises()

      expect(formOption(wrapper)).toMatchObject({ userRate: 0.5 })
      expect(wrapper.getComponent({ name: 'GroupBadge' }).text()).toContain('0.5x')
    })
  })

  describe('create provider selection', () => {
    const platforms = ['anthropic', 'openai', 'kimi', 'zhipu', 'deepseek', 'minimax', 'gemini', 'grok', 'antigravity', 'composite', 'opencode_go', 'typesafe']
    const availableGroups = platforms.map((platform, index) => ({
      id: index + 1,
      // Deliberately ambiguous names: classification must follow the platform.
      name: `Shared group ${index + 1}`,
      platform,
      rate_multiplier: 1,
      subscription_type: 'standard',
    }))
    const groupSelect = (wrapper: VueWrapper) => wrapper.findComponent('[data-tour="key-form-group"]')
    const optionIds = (wrapper: VueWrapper) => groupSelect(wrapper).props('options').map((option: { value: number }) => option.value)
    const chooseProvider = (wrapper: VueWrapper, value: string) => wrapper.get(`input[name="key-provider"][value="${value}"]`).setValue()
    const openCreate = async () => {
      const wrapper = await mountView()
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      return wrapper
    }

    beforeEach(() => {
      getAvailableGroups.mockResolvedValue(availableGroups)
    })

    it('classifies all configured platforms and retains the complete table filter', async () => {
      const wrapper = await openCreate()
      expect(wrapper.findAll('input[name="key-provider"]')).toHaveLength(4)
      expect(optionIds(wrapper)).toEqual([1])
      await chooseProvider(wrapper, 'openai')
      expect(optionIds(wrapper)).toEqual([2])
      await chooseProvider(wrapper, 'domestic')
      expect(optionIds(wrapper)).toEqual([3, 4, 5, 6])
      await chooseProvider(wrapper, 'other')
      expect(optionIds(wrapper)).toEqual([7, 8, 9, 10, 11, 12])
      expect(wrapper.findAllComponents({ name: 'Select' })[0].props('options')).toHaveLength(14)
    })

    it('clears the previous group on provider change and submits only the newly selected group', async () => {
      const wrapper = await openCreate()
      await wrapper.get('[data-tour="key-form-name"]').setValue('My key')
      await groupSelect(wrapper).vm.$emit('update:modelValue', 1)
      await chooseProvider(wrapper, 'domestic')
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await wrapper.get('#key-form').trigger('submit')
      expect(keysAPI.create).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('keys.groupRequired')

      await groupSelect(wrapper).vm.$emit('update:modelValue', 5)
      vi.mocked(keysAPI.create).mockResolvedValue({ ...createApiKey(), group_id: 5 })
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()
      expect(keysAPI.create).toHaveBeenCalledOnce()
      expect(vi.mocked(keysAPI.create).mock.calls[0].slice(0, 2)).toEqual(['My key', 5])
    })

    it('defaults to a provider with available groups and disables empty categories', async () => {
      getAvailableGroups.mockResolvedValue([availableGroups[5]])
      const wrapper = await openCreate()
      expect(wrapper.get<HTMLInputElement>('input[value="domestic"]').element.checked).toBe(true)
      expect(wrapper.get<HTMLInputElement>('input[value="anthropic"]').element.disabled).toBe(true)
      expect(optionIds(wrapper)).toEqual([6])
    })

    it('shows the empty state when no groups are available', async () => {
      getAvailableGroups.mockResolvedValue([])
      const wrapper = await openCreate()
      expect(wrapper.get('[data-tour="key-form-provider"]').text()).toContain('common.noGroupsAvailable')
      expect(optionIds(wrapper)).toEqual([])
      expect(wrapper.findAll<HTMLInputElement>('input[name="key-provider"]').every((input) => input.element.disabled)).toBe(true)
    })

    it('selects an available provider when groups arrive after opening', async () => {
      let resolveGroups!: (value: typeof availableGroups) => void
      getAvailableGroups.mockReturnValue(new Promise((resolve) => { resolveGroups = resolve }))
      const wrapper = await openCreate()
      resolveGroups([availableGroups[1]])
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('input[value="openai"]').element.checked).toBe(true)
      expect(optionIds(wrapper)).toEqual([2])
    })

    it('resets provider and group when reopening create, and preserves edit options', async () => {
      const wrapper = await openCreate()
      await chooseProvider(wrapper, 'domestic')
      await groupSelect(wrapper).vm.$emit('update:modelValue', 5)
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
      expect(optionIds(wrapper)).toEqual([1])
      expect(groupSelect(wrapper).props('modelValue')).toBeNull()
      await wrapper.get('[data-test="close-dialog"]').trigger('click')
      await getButtonByText(wrapper, 'common.edit').trigger('click')
      expect(wrapper.find('[data-tour="key-form-provider"]').exists()).toBe(false)
      expect(optionIds(wrapper)).toHaveLength(12)
    })
  })
})
