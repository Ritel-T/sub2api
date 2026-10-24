import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ConfirmDialog from '../ConfirmDialog.vue'
import ConfirmationPrompt from '../ConfirmationPrompt.vue'
import { defineComponent, provide, ref } from 'vue'
import { ConfirmationSessionKey, useConfirmation, useConfirmedAction } from '@/composables/useConfirmation'
import { provideModalContext } from '@/composables/overlayLifecycle'

const wrappers: ReturnType<typeof mount>[] = []
const i18n = () => createI18n({ legacy: false, locale: 'en', messages: { en: { common: { confirm: 'Confirm', cancel: 'Cancel' } } } })
function dialog(extra = {}) {
  const wrapper = mount(ConfirmDialog, { props: { show: true, title: 'Delete A', message: 'Delete?', ...extra }, global: { plugins: [i18n()], stubs: { teleport: true } }, attachTo: document.body })
  wrappers.push(wrapper)
  return wrapper
}
afterEach(() => { wrappers.forEach(w => w.unmount()); wrappers.length = 0 })

describe('ConfirmDialog acceptance lifecycle', () => {
  it.each(['identity', 'active', 'modal'] as const)('blocks pending direct confirmation across %s replacement and ABA', async (change) => {
    const identity = ref('User A /route-a')
    const active = ref(true)
    const modalOpen = ref(true)
    const shown = ref(true)
    const confirm = vi.fn()
    const cancel = vi.fn()
    const Host = defineComponent({
      components: { ConfirmDialog },
      setup() {
        provide(ConfirmationSessionKey, () => ({ active: active.value, identity: identity.value }))
        provideModalContext(modalOpen)
        return { shown, confirm, cancel }
      },
      template: '<ConfirmDialog :show="shown" title="Delete A" message="A" @confirm="confirm" @cancel="cancel" />'
    })
    const wrapper = mount(Host, { global: { plugins: [i18n()], stubs: { teleport: true } }, attachTo: document.body })
    wrappers.push(wrapper)
    const oldButton = wrapper.get('.btn-primary').element as HTMLButtonElement
    if (change === 'identity') { identity.value = 'User B /route-b'; identity.value = 'User A /route-a' }
    if (change === 'active') { active.value = false; active.value = true }
    if (change === 'modal') { modalOpen.value = false; modalOpen.value = true }
    // Click the retained node before Vue removes it: synchronous guard still wins.
    oldButton.click()
    expect(confirm).not.toHaveBeenCalled()
    expect(cancel).toHaveBeenCalledTimes(1)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    shown.value = false
    await wrapper.vm.$nextTick()
    shown.value = true
    await wrapper.vm.$nextTick()
    await wrapper.get('.btn-primary').trigger('click')
    expect(confirm).toHaveBeenCalledTimes(1)
  })

  it('automatically invalidates caller decisions across session and parent modal changes', async () => {
    const identity = ref('User A /route-a')
    const active = ref(true)
    const modalOpen = ref(true)
    let confirmation!: ReturnType<typeof useConfirmation>
    let direct!: ReturnType<typeof useConfirmedAction>
    const Child = defineComponent({
      setup() { confirmation = useConfirmation(); direct = useConfirmedAction(); return () => null }
    })
    const Host = defineComponent({
      components: { Child },
      setup() {
        provide(ConfirmationSessionKey, () => ({ active: active.value, identity: identity.value }))
        provideModalContext(modalOpen)
      },
      template: '<Child />'
    })
    const wrapper = mount(Host)
    wrappers.push(wrapper)
    const first = confirmation.request('A')
    identity.value = 'User B /route-b'
    expect(await first).toBe(false)
    const second = confirmation.request('B')
    modalOpen.value = false
    expect(await second).toBe(false)
    expect(await direct.run(vi.fn())).toBe(false)
    modalOpen.value = true
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    const effect = vi.fn()
    const mutation = direct.run(async isCurrent => { await gate; if (isCurrent()) effect() })
    active.value = false
    release()
    expect(await mutation).toBe(false)
    expect(effect).not.toHaveBeenCalled()
  })

  it('latches confirm synchronously for one opening', async () => {
    const wrapper = dialog()
    const button = wrapper.get('.btn-primary').element as HTMLButtonElement
    button.click()
    button.click()
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    ;(wrapper.get('.btn-primary').element as HTMLButtonElement).click()
    expect(wrapper.emitted('confirm')).toHaveLength(2)
  })

  it('blocks same-stack cancellation before busy props render without trapping early-return handlers', async () => {
    const wrapper = dialog()
    const confirm = wrapper.get('.btn-primary').element as HTMLButtonElement
    const cancel = wrapper.get('.btn-secondary').element as HTMLButtonElement
    confirm.click()
    cancel.click()
    expect(wrapper.emitted('cancel')).toBeUndefined()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    cancel.click()
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('hands the synchronous cancellation guard to busy throughout an async mutation', async () => {
    const wrapper = dialog()
    ;(wrapper.get('.btn-primary').element as HTMLButtonElement).click()
    ;(wrapper.get('.btn-secondary').element as HTMLButtonElement).click()
    expect(wrapper.emitted('cancel')).toBeUndefined()
    await wrapper.setProps({ busy: true })
    await wrapper.vm.$nextTick()
    await wrapper.get('.btn-secondary').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('cancel')).toBeUndefined()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    await wrapper.setProps({ busy: false })
    await wrapper.get('.btn-secondary').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('blocks confirm/cancel/escape while busy and allows externally invalidated close', async () => {
    const wrapper = dialog({ busy: true })
    expect(wrapper.get('.btn-primary').attributes('aria-busy')).toBe('true')
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeDefined()
    expect(wrapper.get('.btn-secondary').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[aria-label="Close modal"]').exists()).toBe(false)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('cancel')).toBeUndefined()
    await wrapper.setProps({ show: false })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('releases acceptance latch after a busy operation fails without closing', async () => {
    const wrapper = dialog()
    await wrapper.get('.btn-primary').trigger('click')
    await wrapper.setProps({ busy: true })
    await wrapper.setProps({ busy: false })
    await wrapper.get('.btn-primary').trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(2)
  })

  it('renders supplied destructive intent through ConfirmationPrompt', async () => {
    let confirmation!: ReturnType<typeof useConfirmation>
    const Host = defineComponent({
      components: { ConfirmationPrompt },
      setup() { confirmation = useConfirmation(); return { confirmation } },
      template: '<ConfirmationPrompt :confirmation="confirmation" />'
    })
    const wrapper = mount(Host, { global: { plugins: [i18n()], stubs: { teleport: true } }, attachTo: document.body })
    wrappers.push(wrapper)
    const action = vi.fn().mockResolvedValue(undefined)
    const result = confirmation.run({ title: 'Delete account A', message: 'A only', confirmText: 'Delete account', cancelText: 'Keep account', danger: true }, action)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('h2').text()).toBe('Delete account A')
    expect(wrapper.get('.btn-danger').text()).toBe('Delete account')
    expect(wrapper.get('.btn-secondary').text()).toBe('Keep account')
    await wrapper.get('.btn-danger').trigger('click')
    expect(await result).toBe(true)
    expect(action).toHaveBeenCalledTimes(1)
  })
})
