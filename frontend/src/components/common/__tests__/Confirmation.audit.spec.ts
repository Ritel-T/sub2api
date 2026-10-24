import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, effectScope } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import ConfirmationPrompt from '@/components/common/ConfirmationPrompt.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useConfirmation } from '@/composables/useConfirmation'
import OllamaCloudUsageSettings from '@/components/account/OllamaCloudUsageSettings.vue'
import UserAttributesConfigModal from '@/components/user/UserAttributesConfigModal.vue'

const api = vi.hoisted(() => ({ deleteOllamaCloudUsageSession: vi.fn(async (id: number) => ({ account_id: id, eligible: true, configured: false, auto_refresh_enabled: false, encryption_key_configured: true })) }))
const attributeApi = vi.hoisted(() => ({listDefinitions: vi.fn(async () => [{id: 12, key: 'department', name: 'Department', type: 'text', enabled: true}]), deleteDefinition: vi.fn(() => new Promise(() => {}))}))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: api, userAttributes: attributeApi } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({showSuccess: vi.fn(), showError: vi.fn()}) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({t: (key: string) => key}) }))

enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = ''; vi.clearAllMocks() })
const press = (key: string, shiftKey = false) => {
  const event = new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })
  document.activeElement?.dispatchEvent(event)
  return event
}

describe('Independent dialog audit regressions - expected behavior', () => {
  it('Tab after a teleported Select should continue after its trigger, not jump to the modal close button', async () => {
    const host = defineComponent({
      setup: () => () => h(BaseDialog, {show: true, title: 'Form'}, {
        default: () => [
          h('input', {id: 'before'}),
          h(Select, {id: 'middle-select', modelValue: 'a', searchable: true, options: [{value:'a', label:'A'}, {value:'b', label:'B'}]}),
          h('input', {id: 'after'}),
        ],
      })
    })
    mount(host, {attachTo: document.body})
    await nextTick()
    const trigger = document.getElementById('middle-select') as HTMLButtonElement
    trigger.focus()
    trigger.click()
    await nextTick(); await nextTick()
    expect(document.activeElement?.classList.contains('select-search-input')).toBe(true)
    press('Tab')
    await nextTick()
    expect(document.activeElement?.getAttribute('aria-label')).not.toBe('Close modal')
  })

  it('closing a lower modal must keep the still-open top modal focus active', async () => {
    const opener = document.createElement('button'); opener.id = 'page-opener'; document.body.append(opener); opener.focus()
    const parent = mount(BaseDialog, {attachTo: document.body, props: {show: true, title: 'Parent'}, slots: {default: '<button id="parent-button">Parent</button>'}})
    await nextTick()
    mount(BaseDialog, {attachTo: document.body, props: {show: true, title: 'Child'}, slots: {default: '<button id="child-button" autofocus>Child</button>'}})
    await nextTick()
    expect(document.activeElement?.id).toBe('child-button')
    await parent.setProps({show: false})
    expect(document.activeElement?.id).toBe('child-button')
  })

  it('a destructive scoped confirmation should preserve danger action styling', async () => {
    const scope = effectScope()
    const confirmation = scope.run(() => useConfirmation())!
    const pending = confirmation.request({message: 'Delete selected accounts?', danger: true, confirmText: 'Delete'})
    mount(ConfirmationPrompt, {attachTo: document.body, props: {confirmation}})
    await nextTick()
    const primary = document.querySelector('.modal-footer button:last-child')!
    expect(primary.classList.contains('btn-danger')).toBe(true)
    confirmation.settle(false); await pending; scope.stop()
  })

  it('existing Ollama delete confirmation must be invalidated when its account identity changes', async () => {
    const account = (id: number) => ({id, name: 'Account ' + id, ollama_cloud_usage: {account_id: id, eligible: true, configured: true, auto_refresh_enabled: false, encryption_key_configured: true}})
    const wrapper = mount(OllamaCloudUsageSettings, {attachTo: document.body, props: {account: account(7) as any}})
    await wrapper.get('[data-testid="ollama-cloud-session-delete"]').trigger('click')
    await nextTick()
    expect(document.querySelector('[role=dialog]')).not.toBeNull()
    await wrapper.setProps({account: account(8) as any})
    const danger = document.querySelector('.modal-footer .btn-danger') as HTMLButtonElement | null
    if (danger) danger.click()
    await flushPromises()
    expect(api.deleteOllamaCloudUsageSession).not.toHaveBeenCalled()
  })

  it('existing attribute delete confirmation should prevent repeated submissions while its request is pending', async () => {
    const wrapper = mount(UserAttributesConfigModal, {attachTo: document.body, props: {show: false}})
    await wrapper.setProps({show: true}); await flushPromises()
    const remove = document.querySelector('button[title="common.delete"]') as HTMLButtonElement
    remove.click(); await nextTick()
    const danger = document.querySelector('.modal-footer .btn-danger') as HTMLButtonElement
    danger.click(); danger.click(); await nextTick()
    expect(attributeApi.deleteDefinition).toHaveBeenCalledTimes(1)
  })

  it('hiding the attribute parent should close its pending nested confirmation', async () => {
    const wrapper = mount(UserAttributesConfigModal, {attachTo: document.body, props: {show: false}})
    await wrapper.setProps({show: true}); await flushPromises()
    const remove = document.querySelector('button[title="common.delete"]') as HTMLButtonElement
    remove.click(); await nextTick()
    await wrapper.setProps({show: false})
    expect(wrapper.findComponent(ConfirmDialog).props('show')).toBe(false)
  })

  it('an accepted Ollama delete keeps its captured target and discards an ABA late response', async () => {
    let resolve!: (value: any) => void
    api.deleteOllamaCloudUsageSession.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const account = (id: number) => ({id, name: 'Account ' + id, ollama_cloud_usage: {account_id: id, eligible: true, configured: true, auto_refresh_enabled: false, encryption_key_configured: true}})
    const wrapper = mount(OllamaCloudUsageSettings, {attachTo: document.body, props: {account: account(7) as any}})
    await wrapper.get('[data-testid="ollama-cloud-session-delete"]').trigger('click')
    ;(document.querySelector('.modal-footer .btn-danger') as HTMLButtonElement).click()
    await nextTick()
    expect(api.deleteOllamaCloudUsageSession).toHaveBeenCalledWith(7)
    await wrapper.setProps({ account: account(8) as any })
    await wrapper.setProps({ account: account(7) as any })
    resolve({account_id: 7, eligible: true, configured: false, auto_refresh_enabled: false, encryption_key_configured: true})
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.find('[data-testid="ollama-cloud-session-delete"]').exists()).toBe(true)
  })

  it('attribute deletion does not refresh a newly reopened parent after an old response', async () => {
    let resolve!: () => void
    attributeApi.deleteDefinition.mockImplementationOnce(() => new Promise<void>(done => { resolve = done }))
    const wrapper = mount(UserAttributesConfigModal, {attachTo: document.body, props: {show: false}})
    await wrapper.setProps({show:true}); await flushPromises()
    ;(document.querySelector('button[title="common.delete"]') as HTMLButtonElement).click()
    await nextTick()
    ;(document.querySelector('.modal-footer .btn-danger') as HTMLButtonElement).click()
    await nextTick()
    await wrapper.setProps({show:false}); await wrapper.setProps({show:true}); await flushPromises()
    const calls = attributeApi.listDefinitions.mock.calls.length
    resolve(); await flushPromises()
    expect(attributeApi.listDefinitions).toHaveBeenCalledTimes(calls)
    expect(wrapper.findComponent(ConfirmationPrompt).props('confirmation').show.value).toBe(false)
  })

  it('same scoped helper rejects concurrent requests instead of queuing them', async () => {
    const scope = effectScope(); const confirmation = scope.run(() => useConfirmation())!
    const first = confirmation.request('first')
    expect(await confirmation.request('second')).toBe(false)
    confirmation.settle(true)
    expect(await first).toBe(true)
    scope.stop()
  })
})
