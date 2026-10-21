import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AdminGroup } from '@/types'
import GroupsView from '../GroupsView.vue'
import zh from '@/i18n/locales/zh/admin/overview'
import en from '@/i18n/locales/en/admin/overview'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), getAll: vi.fn(), create: vi.fn(), update: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  groups: {
    ...mocks,
    getModelAllowlistCandidates: vi.fn().mockResolvedValue([]),
    getUsageSummary: vi.fn().mockResolvedValue([]),
    getCapacitySummary: vi.fn().mockResolvedValue([]),
    getLiveCapability: vi.fn().mockResolvedValue({ supported: false }),
  },
  accounts: { list: vi.fn(), getById: vi.fn() },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: mocks.showError }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: false }) }))
vi.mock('@/stores/onboarding', () => ({ useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const group = (id: number, values: Partial<AdminGroup> = {}): AdminGroup => ({
  id, name: `Group ${id}`, description: null, platform: 'anthropic', rate_multiplier: 2,
  is_exclusive: false, status: 'active', subscription_type: 'standard',
  daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null,
  peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
  claude_code_only: false, fallback_group_id: null, fallback_group_id_on_invalid_request: null,
  model_routing: null, model_routing_enabled: false, supported_model_scopes: [],
  created_at: '', updated_at: '', ...values,
} as AdminGroup)

const SelectStub = defineComponent({
  props: ['modelValue', 'options', 'disabled'],
  emits: ['update:modelValue'],
  template: `<select :value="modelValue == null ? '' : String(modelValue)" :disabled="disabled"
    @change="$emit('update:modelValue', options.find(option => String(option.value ?? '') === $event.target.value)?.value)">
    <option v-for="option in options" :key="String(option.value)" :value="option.value == null ? '' : String(option.value)">{{ option.label }}</option>
  </select>`,
})
const wrappers: ReturnType<typeof mount>[] = []
function mountView() {
  const wrapper = mount(GroupsView, { global: { stubs: {
    AppLayout: defineComponent({ template: '<main><slot /></main>' }),
    TablePageLayout: defineComponent({ template: '<section><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>' }),
    DataTable: defineComponent({ props: ['data'], template: '<div><div v-for="row in data" :key="row.id" :data-testid="`row-${row.id}`"><slot name="cell-actions" :row="row" /></div></div>' }),
    BaseDialog: defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }),
    Select: SelectStub, Icon: true, Pagination: true, ConfirmDialog: true, EmptyState: true,
    PlatformIcon: true, GroupCapacityBadge: true, GroupRateMultipliersModal: true,
    GroupRPMOverridesModal: true, VueDraggable: true,
  } } })
  wrappers.push(wrapper)
  return wrapper
}
const button = (wrapper: ReturnType<typeof mountView>, text: string) => wrapper.findAll('button').find(item => item.text() === text)!
async function createSubscription(wrapper: ReturnType<typeof mountView>) {
  await button(wrapper, 'admin.groups.createGroup').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="create-subscription-type"]').setValue('subscription')
  await wrapper.get('#create-group-form input[type="text"]').setValue('Subscription')
}

describe('GroupsView subscription balance continuation', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    mocks.list.mockResolvedValue({ items: [group(1, { subscription_type: 'subscription', subscription_fallback_group_id: 8 })], total: 1, page: 1, pages: 1, page_size: 20 })
    mocks.getAll.mockResolvedValue([
      group(8), group(9, { platform: 'openai' }), group(10, { status: 'inactive' }),
      group(11, { subscription_type: 'subscription' }),
      group(12, { platform: 'composite' }),
    ])
    mocks.create.mockResolvedValue(group(100))
    mocks.update.mockResolvedValue(group(1))
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('shows only active standard groups on the same platform from the complete catalog and saves the target', async () => {
    const wrapper = mountView()
    await flushPromises()
    await button(wrapper, 'admin.groups.createGroup').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="create-subscription-fallback"]').exists()).toBe(false)
    await wrapper.get('[data-testid="create-subscription-type"]').setValue('subscription')
    const select = wrapper.get('[data-testid="create-subscription-fallback-select"]')
    expect(select.findAll('option').map(option => option.attributes('value'))).toEqual(['', '8'])
    expect(select.text()).toContain('Group 8 (2x)')
    expect(mocks.getAll).toHaveBeenCalledOnce()
    await select.setValue('8')
    await wrapper.get('#create-group-form input[type="text"]').setValue('Subscription')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ subscription_type: 'subscription', subscription_fallback_group_id: 8 }))
  })

  it('loads the saved target and sends zero when the administrator disables continuation', async () => {
    const wrapper = mountView()
    await flushPromises()
    await button(wrapper.get('[data-testid="row-1"]') as ReturnType<typeof mountView>, 'common.edit').trigger('click')
    await flushPromises()
    const select = wrapper.get<HTMLSelectElement>('[data-testid="edit-subscription-fallback-select"]')
    expect(select.element.value).toBe('8')
    await select.setValue('')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ subscription_fallback_group_id: 0 }))
  })

  it('clears a stale target when switching away from subscription billing', async () => {
    const wrapper = mountView()
    await flushPromises()
    await createSubscription(wrapper)
    await wrapper.get('[data-testid="create-subscription-fallback-select"]').setValue('8')
    await wrapper.get('[data-testid="create-subscription-type"]').setValue('standard')
    expect(wrapper.find('[data-testid="create-subscription-fallback"]').exists()).toBe(false)
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ subscription_fallback_group_id: null }))
  })

  it('clears a selected target when changing platforms and offers the new platform catalog', async () => {
    const wrapper = mountView()
    await flushPromises()
    await createSubscription(wrapper)
    await wrapper.get('[data-testid="create-subscription-fallback-select"]').setValue('8')
    await wrapper.get('[data-testid="create-group-platform"]').setValue('openai')
    await flushPromises()
    const select = wrapper.get<HTMLSelectElement>('[data-testid="create-subscription-fallback-select"]')
    expect(select.element.value).toBe('')
    expect(select.findAll('option').map(option => option.attributes('value'))).toEqual(['', '9'])
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ platform: 'openai', subscription_fallback_group_id: null }))
  })

  it('hides continuation for an edited standard group and clears a legacy setting', async () => {
    mocks.list.mockResolvedValueOnce({ items: [group(1, { subscription_fallback_group_id: 8 })], total: 1, page: 1, pages: 1, page_size: 20 })
    const wrapper = mountView()
    await flushPromises()
    await button(wrapper.get('[data-testid="row-1"]') as ReturnType<typeof mountView>, 'common.edit').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="edit-subscription-fallback"]').exists()).toBe(false)
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ subscription_fallback_group_id: 0 }))
  })

  it('disables continuation and excludes Composite targets when creating a Composite subscription', async () => {
    const wrapper = mountView()
    await flushPromises()
    await createSubscription(wrapper)
    await wrapper.get('[data-testid="create-subscription-fallback-select"]').setValue('8')
    await wrapper.get('[data-testid="create-group-platform"]').setValue('composite')
    await flushPromises()
    const select = wrapper.get<HTMLSelectElement>('[data-testid="create-subscription-fallback-select"]')
    expect(select.element.disabled).toBe(true)
    expect(select.findAll('option').map(option => option.attributes('value'))).toEqual([''])
    expect(wrapper.get('[data-testid="create-subscription-fallback"]').text()).toContain('admin.groups.subscription.fallbackCompositeUnsupported')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ platform: 'composite', subscription_fallback_group_id: null }))
  })

  it('disables continuation for an edited Composite subscription and clears a legacy target', async () => {
    mocks.list.mockResolvedValueOnce({ items: [group(1, { platform: 'composite', subscription_type: 'subscription', subscription_fallback_group_id: 12 })], total: 1, page: 1, pages: 1, page_size: 20 })
    const wrapper = mountView()
    await flushPromises()
    await button(wrapper.get('[data-testid="row-1"]') as ReturnType<typeof mountView>, 'common.edit').trigger('click')
    await flushPromises()
    const select = wrapper.get<HTMLSelectElement>('[data-testid="edit-subscription-fallback-select"]')
    expect(select.element.disabled).toBe(true)
    expect(select.findAll('option').map(option => option.attributes('value'))).toEqual([''])
    expect(wrapper.get('[data-testid="edit-subscription-fallback"]').text()).toContain('admin.groups.subscription.fallbackCompositeUnsupported')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(1, expect.objectContaining({ subscription_fallback_group_id: 0 }))
  })

  it('describes exhaustion of a valid subscription, balance billing, recovery, email, and insufficient balance in both locales', () => {
    expect(zh.groups.subscription.fallbackGroupHint).toContain('有效订阅的日、周或月额度耗尽')
    expect(zh.groups.subscription.fallbackGroupHint).toContain('目标分组倍率或用户专属倍率')
    expect(zh.groups.subscription.fallbackGroupHint).toContain('邮件通知')
    expect(zh.groups.subscription.fallbackGroupHint).toContain('额度恢复后自动回到订阅')
    expect(zh.groups.subscription.fallbackGroupHint).toContain('余额不足仍停止')
    expect(en.groups.subscription.fallbackGroupHint).toContain('valid subscription exhausts')
    expect(en.groups.subscription.fallbackGroupHint).toContain('insufficient balance still stops')
  })
})
