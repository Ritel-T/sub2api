import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'

const routeState = vi.hoisted(() => ({ query: {} as Record<string, string> }))
vi.mock('vue-router', () => ({ useRoute: () => routeState }))
vi.mock('@/components/admin/ConsoleObjectLinks.vue', () => ({ default: { template: '<nav />' } }))
const { list, bulkAction, bulkAssign, listUsers, showError } = vi.hoisted(() => ({
  list: vi.fn(), bulkAction: vi.fn(), bulkAssign: vi.fn(), listUsers: vi.fn(), showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: { list, bulkAction, bulkAssign },
    groups: { getAll: vi.fn().mockResolvedValue([]) },
    users: { list: listUsers }
  }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const rows = [
  { id: 1, user_id: 11, group_id: 1, status: 'active', user: { email: 'active@example.com' } },
  { id: 2, user_id: 22, group_id: 1, status: 'expired', user: { email: 'expired@example.com' } },
  { id: 3, user_id: 33, group_id: 1, status: 'revoked', user: { email: 'revoked@example.com' } }
]

function mountView() {
  return mount(SubscriptionsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: { name: 'DataTable', props: ['columns', 'data', 'selectedKeys'], emits: ['update:selectedKeys', 'sort', 'column-reorder'], template: '<div />' },
        BaseDialog: { name: 'BaseDialog', props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        Pagination: true, Select: true, ConfirmDialog: true, Icon: true,
        GroupBadge: true, GroupOptionItem: true, EmptyState: true, Teleport: true, RouterLink: true
      }
    }
  })
}

let wrapper: ReturnType<typeof mountView>

beforeEach(async () => {
  vi.clearAllMocks()
  Object.keys(routeState.query).forEach(key => delete routeState.query[key])
  localStorage.clear()
  sessionStorage.clear()
  localStorage.setItem('auth_user', JSON.stringify({ id: 777 }))
  list.mockResolvedValue({ items: rows, total: 60, pages: 3 })
  wrapper = mountView()
  await flushPromises()
})
afterEach(() => {
  wrapper.unmount()
  vi.useRealTimers()
})

async function select(ids: number[]) {
  wrapper.getComponent({ name: 'DataTable' }).vm.$emit('update:selectedKeys', ids)
  await flushPromises()
}

describe('subscription bulk operations', () => {
  it('uses the shared dialog for the subscription guide and closes through its event', async () => {
    (wrapper.vm as any).showGuideModal = true
    await flushPromises()
    const dialog = wrapper.findAllComponents({ name: 'BaseDialog' }).find(node => node.props('show'))!
    expect(dialog).toBeDefined()
    expect(dialog.text()).toContain('admin.subscriptions.guide.subtitle')
    dialog.vm.$emit('close')
    await flushPromises()
    expect((wrapper.vm as any).showGuideModal).toBe(false)
  })

  it('retains every field and persists main-row placement independently of free ordering and reset', async () => {
    wrapper.unmount()
    localStorage.setItem('subscription-hidden-columns', JSON.stringify(['usage', 'group']))
    wrapper = mountView()
    await flushPromises()
    const keys = () => wrapper.getComponent({ name: 'DataTable' }).props('columns').map((column: { key: string }) => column.key)
    const defaults = ['user', 'status', 'usage', 'expires_at', 'group', 'actions']
    expect(keys()).toEqual(defaults)
    const order = ['actions', 'group', 'user', 'status', 'usage', 'expires_at']
    wrapper.getComponent({ name: 'DataTable' }).vm.$emit('column-reorder', order)
    wrapper.getComponent(ColumnSettings).vm.$emit('toggle', 'usage')
    await flushPromises()
    expect(keys()).toEqual([...order.filter(key => key !== 'actions'), 'actions'])
    expect(wrapper.getComponent({ name: 'DataTable' }).props('columns').find((column: { key: string }) => column.key === 'usage').primary).toBe(false)
    wrapper.unmount()
    wrapper = mountView()
    await flushPromises()
    expect(keys()).toEqual([...order.filter(key => key !== 'actions'), 'actions'])
    expect(wrapper.getComponent({ name: 'DataTable' }).props('columns').find((column: { key: string }) => column.key === 'usage').primary).toBe(false)
    wrapper.getComponent(ColumnSettings).vm.$emit('reset')
    await flushPromises()
    expect(keys()).toEqual(defaults)
    expect(wrapper.getComponent({ name: 'DataTable' }).props('columns').find((column: { key: string }) => column.key === 'usage').primary).toBe(true)
  })

  it('retains the email or username display preference beside column settings', async () => {
    wrapper.unmount()
    localStorage.setItem('subscription-user-column-mode', 'username')
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.getComponent({ name: 'DataTable' }).props('columns')[0].label).toBe('admin.users.columns.username')
    const modeSelect = wrapper.findAllComponents({ name: 'Select' }).find(select => select.attributes('aria-label') === 'admin.subscriptions.columns.user')!
    modeSelect.vm.$emit('update:modelValue', 'email')
    await flushPromises()
    expect(localStorage.getItem('subscription-user-column-mode')).toBe('email')
    expect(wrapper.getComponent({ name: 'DataTable' }).props('columns')[0].label).toBe('admin.subscriptions.columns.user')
  })

  it('loads all statuses with an exact routed user, including an unavailable user label', async () => {
    wrapper.unmount()
    routeState.query.user_id = '42'
    wrapper = mountView()
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(1, expect.any(Number), expect.objectContaining({ user_id: 42, status: undefined }), expect.anything())
    expect((wrapper.vm as any).filterUserKeyword).toBe('#42')
  })

  it('uses eligible selected rows and retains failures and untouched selections after a partial result', async () => {
    bulkAction.mockResolvedValue({ success_count: 1, failed_count: 1, results: [
      { subscription_id: 1, success: true },
      { subscription_id: 2, success: false, error: 'Cannot adjust this subscription' }
    ] })
    await select([1, 2, 3])
    await wrapper.get('[data-test="bulk-extend"]').trigger('click')
    const form = wrapper.get('#bulk-subscription-action-form')
    expect(form.text()).toContain('active@example.com')
    expect(form.text()).toContain('expired@example.com')
    expect(form.text()).not.toContain('revoked@example.com')
    await form.trigger('submit')
    await flushPromises()

    expect(bulkAction).toHaveBeenCalledWith({ subscription_ids: [1, 2], action: 'extend', days: 30 }, expect.any(String))
    expect(wrapper.getComponent({ name: 'DataTable' }).props('selectedKeys')).toEqual([2, 3])
    expect(form.text()).toContain('Cannot adjust this subscription')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('disables operations that do not apply to the selected status', async () => {
    await select([3])
    for (const action of ['extend', 'reset_quota', 'revoke']) {
      expect(wrapper.get(`[data-test="bulk-${action}"]`).attributes('disabled')).toBeDefined()
    }
    expect(wrapper.get('[data-test="bulk-restore"]').attributes('disabled')).toBeUndefined()
  })

  it('retains selection on append, clears on sorting and filters, and rejects unloaded IDs', async () => {
    await select([1, 999])
    expect(wrapper.getComponent({ name: 'DataTable' }).props('selectedKeys')).toEqual([1])
    wrapper.getComponent({ name: 'InfiniteListSentinel' }).vm.$emit('load-more')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'DataTable' }).props('selectedKeys')).toEqual([1])
    await select([2])
    wrapper.getComponent({ name: 'DataTable' }).vm.$emit('sort', 'status', 'asc')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'DataTable' }).props('selectedKeys')).toEqual([])
    await select([3])
    wrapper.findAllComponents({ name: 'Select' }).find(select => select.attributes('placeholder') === 'admin.subscriptions.allStatus')!.vm.$emit('change')
    await flushPromises()
    expect(wrapper.getComponent({ name: 'DataTable' }).props('selectedKeys')).toEqual([])
  })

  it('caps selection at the existing 100-ID mutation contract after multiple batches', async () => {
    const loaded = Array.from({ length: 125 }, (_, index) => ({ ...rows[0], id: index + 1 }))
    list.mockResolvedValueOnce({ items: loaded, total: 125, pages: 1 })
    await (wrapper.vm as any).loadSubscriptions()
    await flushPromises()
    await select(loaded.map(row => row.id))
    expect(wrapper.getComponent({ name: 'DataTable' }).props('selectedKeys')).toEqual(loaded.slice(0, 100).map(row => row.id))
  })

  it('assigns multiple users once and retries only failed users', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    await wrapper.findAll('button').find(button => button.text() === 'admin.subscriptions.assignSubscription')!.trigger('click')
    const form = wrapper.get('#assign-subscription-form')
    await form.get('input[type="checkbox"]').setValue(true)
    form.getComponent({ name: 'Select' }).vm.$emit('update:modelValue', 7)
    const search = form.get('[data-assign-user-search] input')
    for (const id of [11, 22]) {
      listUsers.mockResolvedValue({ items: [{ id, email: `user${id}@example.com` }] })
      await search.trigger('focus')
      await search.setValue(`user${id}`)
      await vi.advanceTimersByTimeAsync(300)
      await flushPromises()
      await form.get('[data-assign-user-search] button').trigger('click')
    }
    expect(form.get('[data-test="assign-users"]').text()).toContain('user11@example.com')
    expect(form.get('[data-test="assign-users"]').text()).toContain('user22@example.com')
    let resolveAssign!: (result: unknown) => void
    bulkAssign.mockReturnValueOnce(new Promise(resolve => { resolveAssign = resolve }))
    await form.trigger('submit')
    await form.trigger('submit')
    expect(bulkAssign).toHaveBeenCalledTimes(1)
    expect(bulkAssign).toHaveBeenCalledWith({ user_ids: [11, 22], group_id: 7, validity_days: 30 })
    resolveAssign({ success_count: 1, failed_count: 1, subscriptions: [{ user_id: 11 }], errors: ['User 22: conflict'] })
    await flushPromises()
    expect(form.get('[data-test="assign-users"]').text()).not.toContain('user11@example.com')
    expect(form.get('[data-test="assign-users"]').text()).toContain('user22@example.com')
    expect(form.get('[data-test="batch-assign-result"]').text()).toContain('User 22: conflict')
    bulkAssign.mockResolvedValueOnce({ success_count: 1, failed_count: 0, subscriptions: [{ user_id: 22 }], errors: [] })
    await form.trigger('submit')
    await flushPromises()
    expect(bulkAssign).toHaveBeenLastCalledWith({ user_ids: [22], group_id: 7, validity_days: 30 })
    expect(form.find('[data-test="assign-users"]').exists()).toBe(false)
  })
})
