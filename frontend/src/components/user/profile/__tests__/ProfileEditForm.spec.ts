import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import ProfileEditForm from '../ProfileEditForm.vue'

const mocks = vi.hoisted(() => ({ updateProfile: vi.fn(), showError: vi.fn(), state: { user: { username: 'alice' } } }))
vi.mock('@/api', () => ({ userAPI: { updateProfile: mocks.updateProfile } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => reactive(mocks.state) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  mocks.state.user = { username: 'alice' }
})

const Harness = defineComponent({
  components: { ProfileEditForm },
  setup: () => ({ auth: reactive(mocks.state) }),
  template: '<ProfileEditForm :initial-username="auth.user.username" />'
})

describe('profile username draft', () => {
  it.each(['next-draft', 'alice'])('preserves %s typed while the previous username is being saved', async (draft) => {
    let finish!: (value: unknown) => void
    mocks.updateProfile.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mount(Harness)
    await wrapper.get('#username').setValue('submitted')
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateProfile).toHaveBeenCalledWith({ username: 'submitted' })
    await wrapper.get('#username').setValue(draft)
    finish({ username: 'submitted' })
    await flushPromises()
    expect(mocks.state.user.username).toBe('submitted')
    expect((wrapper.get('#username').element as HTMLInputElement).value).toBe(draft)
  })

  it('still synchronizes a pristine input with profile updates', async () => {
    const wrapper = mount(Harness)
    reactive(mocks.state).user = { username: 'updated' }
    await flushPromises()
    expect((wrapper.get('#username').element as HTMLInputElement).value).toBe('updated')
  })

  it('shows the server username after saving without further edits', async () => {
    mocks.updateProfile.mockResolvedValue({ username: 'saved-name' })
    const wrapper = mount(Harness)
    await wrapper.get('#username').setValue(' saved-name ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect((wrapper.get('#username').element as HTMLInputElement).value).toBe('saved-name')
  })
})

describe('ProfileEditForm', () => {
  it.each([
    [{ status: 400, code: 'VALIDATION_ERROR', message: 'username is too long' }, 'username is too long'],
    [{ response: { data: { detail: 'backend failure' } } }, 'backend failure'],
    [{}, 'profile.updateFailed'],
  ])('shows API failure %j without changing the saved profile', async (error, expectedMessage) => {
    mocks.updateProfile.mockRejectedValue(error)
    const wrapper = mount(ProfileEditForm, { props: { initialUsername: 'alice' } })

    await wrapper.get('#username').setValue('new-name')
    await wrapper.get('form').trigger('submit.prevent')

    expect(mocks.updateProfile).toHaveBeenCalledWith({ username: 'new-name' })
    expect(mocks.showError).toHaveBeenLastCalledWith(expectedMessage)
    expect(mocks.state.user.username).toBe('alice')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
  })
})
