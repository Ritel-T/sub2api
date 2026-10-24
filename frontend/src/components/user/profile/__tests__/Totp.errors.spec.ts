import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import TotpSetupModal from '../TotpSetupModal.vue'
import TotpDisableDialog from '../TotpDisableDialog.vue'

const mocks = vi.hoisted(() => ({
  showError: vi.fn(), showSuccess: vi.fn(),
  totpAPI: {
    getVerificationMethod: vi.fn(), sendVerifyCode: vi.fn(),
    initiateSetup: vi.fn(), enable: vi.fn(), disable: vi.fn()
  }
}))
vi.mock('@/api', () => ({ totpAPI: mocks.totpAPI }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,qr') } }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.resetAllMocks()
  mocks.totpAPI.getVerificationMethod.mockResolvedValue({ method: 'password' })
})

describe.each([
  { name: 'setup', component: TotpSetupModal, action: mocks.totpAPI.initiateSetup, fallback: 'profile.totp.setupFailed' },
  { name: 'disable', component: TotpDisableDialog, action: mocks.totpAPI.disable, fallback: 'profile.totp.disableFailed' }
])('TOTP $name errors', ({ name, component, action, fallback }) => {
  it('shows the verification-method API error before closing', async () => {
    mocks.totpAPI.getVerificationMethod.mockRejectedValue({ status: 503, message: 'Verification is temporarily unavailable' })
    const wrapper = mount(component, { global: { stubs: { teleport: true } } })
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('Verification is temporarily unavailable')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('shows why the email verification code cannot be sent', async () => {
    mocks.totpAPI.getVerificationMethod.mockResolvedValue({ method: 'email' })
    mocks.totpAPI.sendVerifyCode.mockRejectedValue({ status: 429, message: 'Please wait before requesting another code' })
    const wrapper = mount(component, { global: { stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'profile.totp.sendCode')!.trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('Please wait before requesting another code')
  })

  it.each([
    [{ status: 400, message: 'Incorrect password' }, 'Incorrect password'],
    [{ response: { data: { message: 'Legacy message' } } }, 'Legacy message'],
    [{}, fallback]
  ])('preserves the action error or localized fallback: %j', async (error, expected) => {
    action.mockRejectedValue(error)
    const wrapper = mount(component, { global: { stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.get('input[type="password"]').setValue('incorrect-password')
    if (name === 'setup') await wrapper.get('.btn-primary').trigger('click')
    else await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith(expected)
    expect(wrapper.emitted('success')).toBeUndefined()
  })
})

it('shows the reason a TOTP code was rejected', async () => {
  mocks.totpAPI.initiateSetup.mockResolvedValue({ secret: 'EXAMPLE', qr_code_url: '', setup_token: 'setup-token' })
  mocks.totpAPI.enable.mockRejectedValue({ status: 400, message: 'Invalid TOTP code' })
  const wrapper = mount(TotpSetupModal, { global: { stubs: { teleport: true } } })
  await flushPromises()
  await wrapper.get('input[type="password"]').setValue('password')
  await wrapper.get('.btn-primary').trigger('click')
  await flushPromises()
  await wrapper.get('.btn-primary').trigger('click')
  for (const input of wrapper.findAll('input[maxlength="1"]')) await input.setValue('1')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.totpAPI.enable).toHaveBeenCalledWith({ totp_code: '111111', setup_token: 'setup-token' })
  expect(mocks.showError).toHaveBeenCalledWith('Invalid TOTP code')
  expect(wrapper.emitted('success')).toBeUndefined()
})

it.each([
  { component: TotpSetupModal, action: mocks.totpAPI.initiateSetup },
  { component: TotpDisableDialog, action: mocks.totpAPI.disable }
])('prevents dismissal and repeated submission while a security change is pending', async ({ component, action }) => {
  let finish!: (value: unknown) => void
  action.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
  const wrapper = mount(component, { global: { stubs: { teleport: true } } })
  await flushPromises()
  await wrapper.get('input[type="password"]').setValue('password')
  const submit = async () => {
    if (component === TotpSetupModal) await wrapper.get('.btn-primary').trigger('click')
    else await wrapper.get('form').trigger('submit')
  }
  await submit()
  await submit()
  const dialog = wrapper.getComponent({ name: 'BaseDialog' })
  dialog.vm.$emit('close')
  await wrapper.vm.$nextTick()
  expect(action).toHaveBeenCalledTimes(1)
  expect(wrapper.emitted('close')).toBeUndefined()
  expect(dialog.props('closeOnEscape')).toBe(false)
  expect(dialog.props('showCloseButton')).toBe(false)
  expect(wrapper.get('.btn-secondary').attributes('disabled')).toBeDefined()
  finish({ secret: 'EXAMPLE', qr_code_url: '', setup_token: 'setup-token' })
  await flushPromises()
})
