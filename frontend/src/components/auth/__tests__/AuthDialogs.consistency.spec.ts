import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import TotpLoginModal from '../TotpLoginModal.vue'
import TotpStepUpDialog from '../TotpStepUpDialog.vue'
import LoginAgreementPrompt from '../LoginAgreementPrompt.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useStepUp } from '@/composables/useStepUp'

const mocks = vi.hoisted(() => ({ stepUp: vi.fn(), showError: vi.fn() }))
vi.mock('@/api', () => ({ totpAPI: { stepUp: mocks.stepUp } }))
vi.mock('@/stores', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => vi.resetAllMocks())
const global = { stubs: { teleport: true, RouterLink: RouterLinkStub } }

describe('Authentication shared dialog behavior', () => {
  it('preserves OTP autofill, paste and cancellation while preventing cancellation during verification', async () => {
    const wrapper = mount(TotpLoginModal, { props: { tempToken: 'temporary' }, global, attachTo: document.body })
    await flushPromises()
    let inputs = wrapper.findAll('input[maxlength="1"]')
    expect(document.activeElement).toBe(inputs[0].element)
    await wrapper.get('input[autocomplete="one-time-code"]').setValue('123456')
    expect(wrapper.emitted('verify')).toEqual([['123456']])
    const controller = wrapper.vm as unknown as { setVerifying(value: boolean): void; setError(value: string): void }
    controller.setVerifying(true)
    await wrapper.vm.$nextTick()
    const dialog = wrapper.getComponent(BaseDialog)
    dialog.vm.$emit('close')
    expect(wrapper.emitted('cancel')).toBeUndefined()
    expect(dialog.props('closeOnEscape')).toBe(false)
    controller.setVerifying(false)
    controller.setError('Invalid code')
    await wrapper.vm.$nextTick()
    inputs = wrapper.findAll('input[maxlength="1"]')
    expect(inputs.map(input => (input.element as HTMLInputElement).value)).toEqual(['', '', '', '', '', ''])
    await inputs[0].trigger('paste', { clipboardData: { getData: () => '654321' } })
    expect(wrapper.emitted('verify')).toEqual([['123456'], ['654321']])
    await wrapper.get('.btn-secondary').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('keeps step-up open while busy and resets invalid input before a successful retry', async () => {
    const controller = useStepUp()
    const wrapper = mount(TotpStepUpDialog, { props: { controller }, global, attachTo: document.body })
    const result = controller.prompt()
    await flushPromises()
    let reject!: (reason: unknown) => void
    mocks.stepUp.mockReturnValueOnce(new Promise((_, rejectPromise) => { reject = rejectPromise }))
    await wrapper.get('input[autocomplete="one-time-code"]').setValue('123456')
    const dialog = wrapper.getComponent(BaseDialog)
    dialog.vm.$emit('close')
    expect(controller.visible.value).toBe(true)
    expect(dialog.props('closeOnEscape')).toBe(false)
    reject({ message: 'Invalid code' })
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('Invalid code')
    const inputs = wrapper.findAll('input[maxlength="1"]')
    expect(inputs.map(input => (input.element as HTMLInputElement).value)).toEqual(['', '', '', '', '', ''])
    expect(document.activeElement).toBe(inputs[0].element)
    mocks.stepUp.mockResolvedValueOnce({})
    await inputs[0].trigger('paste', { clipboardData: { getData: () => '654321' } })
    await flushPromises()
    expect(await result).toBe(true)
    expect(controller.visible.value).toBe(false)
    const nextResult = controller.prompt()
    await flushPromises()
    await wrapper.get('.btn-secondary').trigger('click')
    expect(await nextResult).toBe(false)
  })

  it('requires explicit agreement acceptance or rejection and preserves legal links', async () => {
    const wrapper = mount(LoginAgreementPrompt, {
      props: { accepted: false, visible: true, mode: 'modal', documents: [{ id: 'terms', title: 'Terms', content_md: 'Terms content' }] },
      global
    })
    const dialog = wrapper.getComponent(BaseDialog)
    expect(dialog.props('closeOnEscape')).toBe(false)
    expect(dialog.props('closeOnClickOutside')).toBe(false)
    expect(dialog.props('showCloseButton')).toBe(false)
    expect(wrapper.getComponent(RouterLinkStub).props('to')).toEqual({ name: 'LegalDocument', params: { documentId: 'terms' } })
    expect(wrapper.getComponent(RouterLinkStub).attributes('target')).toBe('_blank')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('accept')).toBeUndefined()
    expect(wrapper.emitted('reject')).toBeUndefined()
    await dialog.get('.btn-primary').trigger('click')
    expect(wrapper.emitted('accept')).toHaveLength(1)
    await dialog.get('.btn-secondary').trigger('click')
    expect(wrapper.emitted('reject')).toHaveLength(1)
    await wrapper.setProps({ visible: false, mode: 'checkbox' })
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('input[type="checkbox"]').setValue(false)
    expect(wrapper.emitted('accept')).toHaveLength(2)
    expect(wrapper.emitted('reject')).toHaveLength(2)
  })
})
