import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'
import { adminAPI } from '@/api/admin'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const {
  listCompositeRoutes,
  listGroups,
  duplicateGroup,
  updateGroup,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listCompositeRoutes: vi.fn(),
  listGroups: vi.fn(),
  duplicateGroup: vi.fn(),
  updateGroup: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

const authState = vi.hoisted(() => ({ isSimpleMode: false }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      listCompositeRoutes,
      updateCompositeRoute: vi.fn(),
      deleteCompositeRoute: vi.fn(),
      duplicate: duplicateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: vi.fn(),
      create: vi.fn(),
      update: updateGroup,
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const sourceGroup: AdminGroup = {
  id: 42,
  name: 'Primary',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-16T00:00:00Z',
  updated_at: '2026-07-16T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  model_allowlist: undefined,
  sort_order: 10
}

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>'
})

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
})

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountView(realTables = false) {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: realTables ? false : DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true
      }
    }
  })
}

describe('GroupsView duplicate action', () => {
  beforeEach(() => {
    authState.isSimpleMode = false
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listCompositeRoutes,
      listGroups,      duplicateGroup,
      updateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      showSuccess,
      showError
    ]) {
      fn.mockReset()
    }

    listGroups.mockResolvedValue({
      items: [sourceGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    duplicateGroup.mockResolvedValue({
      ...sourceGroup,
      id: 43,
      name: 'Primary (Copy)',
      status: 'inactive'
    })
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it.each(['success', 'error'])('ignores an old composite route load after switching groups: %s', async (outcome) => {
    let finishA!: (value: unknown) => void
    let failA!: (value: unknown) => void
    let finishB!: (value: unknown) => void
    listCompositeRoutes.mockImplementationOnce(() => new Promise((resolve, reject) => { finishA = resolve; failA = reject }))
      .mockImplementationOnce(() => new Promise(resolve => { finishB = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as any
    const loadingA = vm.handleCompositeRoutes(sourceGroup)
    const loadingB = vm.handleCompositeRoutes({ ...sourceGroup, id: 43 })
    if (outcome === 'success') finishA([{ id: 1, priority: 1 }])
    else failA(new Error('stale A error'))
    await loadingA
    await flushPromises()
    expect(vm.compositeRoutes).toEqual([])
    expect(vm.compositeRoutesLoading).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    finishB([{ id: 2, priority: 1 }])
    await loadingB
    expect(vm.compositeRoutes).toEqual([{ id: 2, priority: 1 }])
    expect(vm.compositeRoutesLoading).toBe(false)
    wrapper.unmount()
  })

  it('rejects an old route load after close and reopen of the same group', async () => {
    let finishOld!: (value: unknown) => void
    listCompositeRoutes.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce([{ id: 3, priority: 1 }])
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as any
    const oldLoad = vm.handleCompositeRoutes(sourceGroup)
    vm.closeCompositeRoutesModal()
    await vm.handleCompositeRoutes(sourceGroup)
    finishOld([{ id: 1, priority: 1 }])
    await oldLoad
    expect(vm.compositeRoutes).toEqual([{ id: 3, priority: 1 }])
    wrapper.unmount()
  })

  it('restores composite column preferences and retains exact update/delete targets from details', async () => {
    const route = { id: 77, public_model: 'model-77', match_type: 'exact', target_platform: 'openai', upstream_model: 'upstream-77', endpoint: 'any', priority: 7, enabled: false, notes: 'notes-77' }
    listCompositeRoutes.mockResolvedValue([route])
    vi.mocked(adminAPI.groups.updateCompositeRoute).mockResolvedValue(route as any)
    vi.mocked(adminAPI.groups.deleteCompositeRoute).mockResolvedValue(undefined)
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    const wrapper = mountView(true)
    await flushPromises()
    await (wrapper.vm as any).handleCompositeRoutes({ ...sourceGroup, platform: 'composite' })
    await flushPromises()
    const table = wrapper.findAllComponents(DataTable).find(table => table.props('columnOrderKey') === 'admin-group-composite-routes')!
    expect(table.props('columns').every(column => column.primary === true)).toBe(true)
    table.getComponent(ColumnSettings).vm.$emit('toggle', 'target')
    table.getComponent(ColumnSettings).vm.$emit('toggle', 'scope')
    table.getComponent(ColumnSettings).vm.$emit('toggle', 'actions')
    await flushPromises()
    wrapper.unmount()
    const restored = mountView(true)
    try {
      await flushPromises()
      await (restored.vm as any).handleCompositeRoutes({ ...sourceGroup, platform: 'composite' })
      await flushPromises()
      const records = restored.findAllComponents(DataTable).find(table => table.props('columnOrderKey') === 'admin-group-composite-routes')!
      expect(records.find('th[data-column-key="target"]').exists()).toBe(false)
      expect(records.find('th[data-column-key="actions"]').exists()).toBe(false)
      await records.get('[data-test="toggle-details"]').trigger('click')
      let detail = records.get('.table-detail-row')
      expect(detail.text()).toContain('upstream-77')
      expect(detail.text()).toContain('admin.groups.compositeRoutes.priority')
      expect(detail.text()).toContain('7')
      await detail.get('button[title="common.edit"]').trigger('click')
      expect((restored.vm as any).compositeRouteForm.public_model).toBe('model-77')
      expect((restored.vm as any).compositeRouteEditingId).toBe(77)
      await restored.findAll('form').find(form => form.text().includes('admin.groups.compositeRoutes.editRoute'))!.trigger('submit')
      await flushPromises()
      expect(adminAPI.groups.updateCompositeRoute).toHaveBeenCalledWith(42, 77, expect.objectContaining({ public_model: 'model-77', upstream_model: 'upstream-77', priority: 7, enabled: false, notes: 'notes-77' }))
      if (!records.find('.table-detail-row').exists()) await records.get('[data-test="toggle-details"]').trigger('click')
      detail = records.get('.table-detail-row')
      await detail.get('button[title="common.delete"]').trigger('click')
      const confirmation = restored.findAllComponents(ConfirmDialog).find(dialog => dialog.props('show'))!
      expect(confirmation.props('message')).toBe('admin.groups.compositeRoutes.deleteConfirm')
      expect(adminAPI.groups.deleteCompositeRoute).not.toHaveBeenCalled()
      confirmation.vm.$emit('confirm'); await flushPromises()
      expect(adminAPI.groups.deleteCompositeRoute).toHaveBeenCalledWith(42, 77)
      expect(localStorage.getItem('admin-group-composite-routes')).toBeTruthy()
      expect(localStorage.getItem('table-columns:groups:v1')).toBeNull()
    } finally { restored.unmount() }
  })

  it('windows 140 mobile composite routes in the original priority and ID order with every field', async () => {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: false, media: query, onchange: null, addEventListener: vi.fn(), removeEventListener: vi.fn(), addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn() }))
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    vi.stubGlobal('innerHeight', 240)
    const routes = Array.from({ length: 140 }, (_, index) => ({ id: 140 - index, public_model: `model-${140 - index}`, match_type: 'exact', target_platform: 'openai', upstream_model: `upstream-${140 - index}`, endpoint: 'responses', priority: (140 - index) % 3, enabled: false, notes: '' }))
    listCompositeRoutes.mockResolvedValue(routes)
    const wrapper = mountView(true)
    try {
      await flushPromises()
      await (wrapper.vm as any).handleCompositeRoutes({ ...sourceGroup, platform: 'composite' })
      await flushPromises()
      const table = wrapper.findAllComponents(DataTable).find(table => table.props('columnOrderKey') === 'admin-group-composite-routes')!
      expect(table.props('data')).toHaveLength(140)
      expect(table.props('data').map(route => route.id)).toEqual([...routes].sort((a, b) => a.priority - b.priority || a.id - b.id).map(route => route.id))
      expect(table.props('rowKey')).toBe('id')
      expect(table.findAll('.table-record').length).toBeGreaterThan(0)
      expect(table.findAll('.table-record').length).toBeLessThan(40)
      const first = table.findAll('.table-record')[0]
      expect(first.findAll('[data-field]').map(field => field.attributes('data-field'))).toEqual(['public_model', 'target', 'scope', 'actions'])
      expect(first.text()).toContain('model-3')
      expect(first.text()).toContain('upstream-3')
      expect(first.text()).toContain('admin.accounts.status.inactive')
      expect(first.get('button[title="common.edit"]').exists()).toBe(true)
      expect(first.get('button[title="common.delete"]').exists()).toBe(true)
    } finally { wrapper.unmount() }
  })

  it('duplicates the selected group, reports success, and refreshes the list', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(duplicateGroup).toHaveBeenCalledTimes(1)
    expect(duplicateGroup).toHaveBeenCalledWith(42)
    expect(showSuccess).toHaveBeenCalledWith('admin.groups.duplicateSuccess')
    expect(listGroups).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('hides advanced group actions in simple mode', async () => {
    authState.isSimpleMode = true
    const compositeGroup = { ...sourceGroup, platform: 'composite' }
    listGroups.mockResolvedValueOnce({ items: [compositeGroup], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="group-duplicate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-composite-routes"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-rate-multipliers"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-rpm-overrides"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores repeated clicks while the duplicate request is in flight', async () => {
    let resolveDuplicate!: (value: AdminGroup) => void
    duplicateGroup.mockImplementationOnce(
      () => new Promise<AdminGroup>((resolve) => { resolveDuplicate = resolve })
    )
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.get('[data-testid="group-duplicate"]')
    void button.trigger('click')
    void button.trigger('click')
    await wrapper.vm.$nextTick()

    expect(duplicateGroup).toHaveBeenCalledTimes(1)
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('title')).toBe('admin.groups.duplicating')

    resolveDuplicate({ ...sourceGroup, id: 43, name: 'Primary (Copy)', status: 'inactive' })
    await flushPromises()
    expect(wrapper.get('[data-testid="group-duplicate"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('shows the API error and restores the action when duplication fails', async () => {
    duplicateGroup.mockRejectedValueOnce(new Error('duplicate failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('duplicate failed')
    expect(wrapper.get('[data-testid="group-duplicate"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not report a successful duplicate as failed when the refresh fails', async () => {
    listGroups
      .mockResolvedValueOnce({
        items: [sourceGroup],
        total: 1,
        page: 1,
        page_size: 20,
        pages: 1
      })
      .mockRejectedValueOnce(new Error('refresh failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('admin.groups.duplicateSuccess')
    expect(showError).toHaveBeenCalledWith('admin.groups.failedToLoad')
    expect(showError).not.toHaveBeenCalledWith('admin.groups.duplicateFailed')
    wrapper.unmount()
  })

  it('shows the standardized API message when updating a group fails', async () => {
    updateGroup.mockRejectedValueOnce({
      status: 409,
      code: 409,
      message: 'group name already exists',
      reason: 'GROUP_EXISTS'
    })
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('group name already exists')
    wrapper.unmount()
  })

  it('loads, edits, and saves custom reasoning multipliers for group pricing', async () => {
    const group = {
      ...sourceGroup,
      model_pricing: [{
        platform: 'openai', models: ['example-model'], billing_mode: 'token',
        input_price: 3e-6, output_price: 15e-6, cache_write_price: null, cache_read_price: null,
        image_input_price: null, image_output_price: null, per_request_price: null,
        reasoning_effort_multipliers: { high: 1.5, max: 3 }, intervals: [], time_pricing: null,
      }],
    }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockResolvedValue(group)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()

    expect(wrapper.get<HTMLInputElement>('[data-reasoning-effort="high"]').element.value).toBe('1.5')
    await wrapper.get('[data-reasoning-effort="high"]').setValue('0.5')
    await wrapper.get('[data-reasoning-effort="max"]').setValue('')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      model_pricing: [expect.objectContaining({ reasoning_effort_multipliers: { high: 0.5 } })],
    }))
    wrapper.unmount()
  })

  it('blocks saving an invalid group reasoning multiplier and allows clearing it', async () => {
    const group = {
      ...sourceGroup,
      model_pricing: [{
        platform: 'openai', models: ['example-model'], billing_mode: 'token',
        input_price: 3e-6, output_price: 15e-6, cache_write_price: null, cache_read_price: null,
        image_input_price: null, image_output_price: null, per_request_price: null,
        reasoning_effort_multipliers: { high: 1.5 }, intervals: [], time_pricing: null,
      }],
    }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockResolvedValue(group)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-reasoning-effort="high"]').setValue('0')
    await wrapper.get('#edit-group-form').trigger('submit')
    expect(updateGroup).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith(expect.stringContaining('reasoningEffortMultiplierPositive'))

    await wrapper.get('[data-testid="reasoning-effort-multipliers"] button').trigger('click')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      model_pricing: [expect.objectContaining({ reasoning_effort_multipliers: null })],
    }))
    wrapper.unmount()
  })

  it('updates manifest controls immediately and submits the displayed selection', async () => {
    vi.useFakeTimers()
    vi.mocked(adminAPI.accounts.list).mockResolvedValue({
      items: [{ id: 5, name: 'Manifest account' }]
    } as never)
    updateGroup.mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    try {
      await flushPromises()
      const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')!
      await editButton.trigger('click')
      await flushPromises()

      const toggle = wrapper.get('[data-testid="codex-manifest-toggle"]')
      await toggle.trigger('click')
      expect(toggle.attributes('aria-checked')).toBe('true')
      const search = wrapper.get('[data-testid="codex-manifest-search"]')
      await search.trigger('focus')
      await vi.advanceTimersByTimeAsync(300)
      await flushPromises()
      expect(adminAPI.accounts.list).toHaveBeenCalledWith(
        1, 20, { search: '', platform: 'openai', group: '42' }, expect.anything()
      )
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')

      await wrapper.get('[aria-label="remove account 5"]').trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-selected-tags"]').exists()).toBe(false)
      await wrapper.get('#edit-group-form').trigger('submit')
      expect(updateGroup).not.toHaveBeenCalled()
      expect(wrapper.find('[data-testid="codex-manifest-validation-error"]').exists()).toBe(true)

      await search.trigger('focus')
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')
      const fallback = wrapper.get('[data-testid="codex-manifest-fallback-toggle"]')
      await fallback.trigger('click')
      expect(fallback.attributes('aria-checked')).toBe('true')
      await fallback.trigger('click')
      expect(fallback.attributes('aria-checked')).toBe('false')
      await fallback.trigger('click')

      await toggle.trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
      await toggle.trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
        codex_models_manifest_config: {
          enabled: true, account_ids: [5], fallback_to_scheduler: true
        }
      }))

      // Reopening reads the saved group afresh, without retaining the prior draft.
      await editButton.trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="codex-manifest-toggle"]').attributes('aria-checked')).toBe('false')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

})
