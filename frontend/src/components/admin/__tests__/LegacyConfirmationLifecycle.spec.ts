import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import TLS from '../TLSFingerprintProfilesModal.vue'
import Rules from '../ErrorPassthroughRulesModal.vue'
import Templates from '../monitor/MonitorTemplateManagerDialog.vue'
import Scheduled from '../account/ScheduledTestsPanel.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const mocks = vi.hoisted(() => ({ remove: vi.fn(), success: vi.fn(), error: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  tlsFingerprintProfiles: { list: async () => [], delete: mocks.remove },
  errorPassthrough: { list: async () => [], delete: mocks.remove },
  channelMonitorTemplate: { list: async () => ({ items: [] }), del: mocks.remove },
  scheduledTests: { listByAccount: async () => [], delete: mocks.remove },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error }) }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => vi.clearAllMocks())
const cases = [
  { name: 'TLS profile', component: TLS, open: 'handleDelete', confirm: 'confirmDelete', props: {} },
  { name: 'error rule', component: Rules, open: 'handleDelete', confirm: 'confirmDelete', props: {} },
  { name: 'monitor template', component: Templates, open: 'handleDelete', confirm: 'doDelete', props: {} },
  { name: 'scheduled plan', component: Scheduled, open: 'confirmDeletePlan', confirm: 'handleDelete', props: { accountId: 1, modelOptions: [] } },
]

describe.each(cases)('$name confirmation lifecycle', ({ component, open, confirm, props }) => {
  async function mountDialog() {
    const wrapper = shallowMount(component as any, { props: { show: false, ...props }, global: { stubs: { BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' } } } })
    await wrapper.setProps({ show: true })
    await flushPromises()
    return wrapper
  }
  it('locks the shown target and rejects duplicate submission', async () => {
    let finish!: () => void
    mocks.remove.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const wrapper = await mountDialog()
    const vm = wrapper.vm as any
    const original = { id: 7, name: 'A', associated_monitors: 0 }
    vm[open](original)
    original.id = 99
    const request = vm[confirm]()
    vm[open]({ id: 8, name: 'B' })
    void vm[confirm]()
    await flushPromises()
    expect(mocks.remove.mock.calls).toEqual([[7]])
    expect(wrapper.getComponent(ConfirmDialog).props('busy')).toBe(true)
    finish()
    await request
    expect(mocks.success).toHaveBeenCalledTimes(1)
  })
  it.each(['success', 'failure'])('discards late %s after parent close and reopen', async outcome => {
    let finish!: () => void
    let fail!: (error: Error) => void
    mocks.remove.mockImplementationOnce(() => new Promise<void>((resolve, reject) => { finish = resolve; fail = reject }))
    const wrapper = await mountDialog()
    const vm = wrapper.vm as any
    vm[open]({ id: 7, name: 'A', associated_monitors: 0 })
    const request = vm[confirm]()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(false)
    vm[open]({ id: 8, name: 'B' })
    void vm[confirm]()
    expect(mocks.remove).toHaveBeenCalledTimes(1)
    if (outcome === 'success') finish()
    else fail(new Error('old operation failed'))
    await request
    expect(mocks.success).not.toHaveBeenCalled()
    expect(mocks.error).not.toHaveBeenCalled()
    vm[open]({ id: 8, name: 'B' })
    mocks.remove.mockResolvedValueOnce(undefined)
    await vm[confirm]()
    expect(mocks.remove.mock.calls).toEqual([[7], [8]])
  })
})
