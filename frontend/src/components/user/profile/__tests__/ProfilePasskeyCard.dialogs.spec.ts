import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProfilePasskeyCard from '../ProfilePasskeyCard.vue'
import { ref } from 'vue'
import { ConfirmationSessionKey } from '@/composables/useConfirmation'
import BaseDialog from '@/components/common/BaseDialog.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), rename: vi.fn(), remove: vi.fn(), register: vi.fn(),
  showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api', () => ({ passkeyAPI: { ...mocks, isSupported: () => true } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.resetAllMocks()
  mocks.list.mockResolvedValue([{ id: 17, name: 'Laptop', created_at: '2026-10-20T12:00:00Z', backup: false }])
})

async function openCard() {
  const wrapper = mount(ProfilePasskeyCard, { props: { enabled: true }, attachTo: document.body, global: { stubs: { teleport: true } } })
  await flushPromises()
  return wrapper
}

describe('Passkey shared dialogs', () => {
  it('renames through the shared form with whitespace validation and disabled duplicate submission', async () => {
    const wrapper = await openCard()
    let finish!: () => void
    mocks.rename.mockReturnValueOnce(new Promise<void>(resolve => { finish = resolve }))
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    const dialog = wrapper.findAllComponents(BaseDialog).find(item => item.props('show'))!
    const input = dialog.get('input')
    expect((input.element as HTMLInputElement).value).toBe('Laptop')
    expect(dialog.get('[type="submit"]').attributes('disabled')).toBeDefined()
    await input.setValue('   ')
    await dialog.get('form').trigger('submit')
    expect(mocks.rename).not.toHaveBeenCalled()
    await input.setValue('  Work laptop  ')
    await dialog.get('form').trigger('submit')
    await dialog.get('form').trigger('submit')
    dialog.vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(mocks.rename).toHaveBeenCalledTimes(1)
    expect(mocks.rename).toHaveBeenCalledWith(17, 'Work laptop')
    expect(dialog.props('show')).toBe(true)
    expect(dialog.props('closeOnEscape')).toBe(false)
    expect(dialog.get('input').attributes('disabled')).toBeDefined()
    finish()
    await flushPromises()
    expect(dialog.props('show')).toBe(false)
    expect(wrapper.text()).toContain('Work laptop')
    expect(mocks.showSuccess).toHaveBeenCalledWith('profile.passkey.renamed')
  })

  it('keeps failed rename editable and cancels without requests', async () => {
    const wrapper = await openCard()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    const dialog = wrapper.findAllComponents(BaseDialog).find(item => item.props('show'))!
    mocks.rename.mockRejectedValueOnce(new Error('failed'))
    await dialog.get('input').setValue('Another name')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    expect(dialog.props('show')).toBe(true)
    expect(mocks.showError).toHaveBeenCalledWith('profile.passkey.renameFailed')
    await dialog.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    expect(dialog.props('show')).toBe(false)
    expect(mocks.rename).toHaveBeenCalledTimes(1)
  })

  it('requires the password for deletion and preserves failed confirmation for retry', async () => {
    const wrapper = await openCard()
    await wrapper.findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
    const dialog = wrapper.findAllComponents(BaseDialog).find(item => item.props('show'))!
    expect(dialog.get('[type="submit"]').attributes('form')).toBe(dialog.get('form').attributes('id'))
    await dialog.get('form').trigger('submit')
    expect(mocks.remove).not.toHaveBeenCalled()
    mocks.remove.mockRejectedValueOnce({ message: 'Incorrect password' })
    await dialog.get('input').setValue('wrong')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.remove).toHaveBeenCalledWith(17, 'wrong')
    expect(dialog.props('show')).toBe(true)
    expect(mocks.showError).toHaveBeenCalledWith('Incorrect password')
    mocks.remove.mockResolvedValueOnce({})
    await dialog.get('input').setValue('correct')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    expect(dialog.props('show')).toBe(false)
    expect(wrapper.text()).not.toContain('Laptop')
  })
})


describe('passkey deletion session boundary', () => {
  it.each(['success', 'failure'])('suppresses a late %s after same-tick session A to B to A', async outcome => {
    const user = ref(1)
    const wrapper = mount(ProfilePasskeyCard, { props: { enabled: true }, global: {
      provide: { [ConfirmationSessionKey as symbol]: () => ({ active: true, identity: user.value }) }, stubs: { teleport: true }
    } })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.deletePasskey({ id: 17, name: 'Laptop' })
    vm.deletePassword = 'secret'
    let finish!: () => void
    let fail!: (error: Error) => void
    mocks.remove.mockImplementationOnce(() => new Promise<void>((resolve, reject) => { finish = resolve; fail = reject }))
    const request = vm.confirmDelete()
    void vm.confirmDelete()
    expect(mocks.remove).toHaveBeenCalledTimes(1)
    user.value = 2; user.value = 1
    expect(vm.deleteTarget).toBe(null)
    expect(vm.deletePassword).toBe('')
    if (outcome === 'success') finish()
    else fail(new Error('old session'))
    await request
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(vm.credentials).toHaveLength(1)
  })
  it('cancels pending password confirmation before acceptance on session ABA', async () => {
    const user = ref(1)
    const wrapper = mount(ProfilePasskeyCard, { props: { enabled: true }, global: {
      provide: { [ConfirmationSessionKey as symbol]: () => ({ active: true, identity: user.value }) }, stubs: { teleport: true }
    } })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.deletePasskey({ id: 17, name: 'Laptop' }); vm.deletePassword = 'secret'
    user.value = 2; user.value = 1
    await vm.confirmDelete()
    expect(mocks.remove).not.toHaveBeenCalled()
  })
})
