import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserEditModal from '../UserEditModal.vue'

const { resetAccountSecurity, getById, update, showError, stepUpRun, auth, locale } = vi.hoisted(() => ({
  resetAccountSecurity: vi.fn(), getById: vi.fn(), update: vi.fn(), showError: vi.fn(),
  stepUpRun: vi.fn(), auth: { isAdmin: true, isSimpleMode: false }, locale: { value: 'zh-CN' }
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }), isStepUpBlocked: () => false,
  isStepUpCancelled: () => false, stepUpBlockReason: () => ''
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  users: { resetAccountSecurity, getById, update },
  userAttributes: { updateUserAttributeValues: vi.fn() }
} }))
vi.mock('vue-i18n', async (original) => ({
  ...(await original<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key, locale })
}))

enableAutoUnmount(afterEach)
const user = { id: 7, email: 'owner@example.test', username: 'owner', role: 'user', concurrency: 3, status: 'active' }
const modal = (show = true) => mount(UserEditModal, {
  props: { show, user: user as never },
  global: { stubs: {
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
    Select: true, ObserverGroupSelector: true, Icon: true, UserAttributeForm: true,
    ModerationUserPanel: true, TotpStepUpDialog: true
  } }
})
const submit = '[data-test="security-reset-submit"]'
const note = '[data-test="security-reset-note"]'
const confirm = '[data-test="security-reset-confirm"]'
async function prepare(wrapper: ReturnType<typeof modal>, value = 'Verified account owner in person') {
  await wrapper.get(note).setValue(value)
  await wrapper.get(confirm).setValue(true)
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  vi.clearAllMocks()
  auth.isAdmin = true
  locale.value = 'zh-CN'
  resetAccountSecurity.mockReset().mockResolvedValue({ applied: true })
  getById.mockReset().mockResolvedValue(user)
})

describe('manual administrator MFA reset', () => {
  it('is absent for non-admins and closed modals without fetching or resetting', () => {
    auth.isAdmin = false
    expect(modal().find('[data-test="security-reset"]').exists()).toBe(false)
    auth.isAdmin = true
    expect(modal(false).find('[data-test="security-reset"]').exists()).toBe(false)
    expect(resetAccountSecurity).not.toHaveBeenCalled()
    expect(getById).not.toHaveBeenCalled()
  })

  it('shows the selected identity, all cleared factors, session revocation and preserved fields', () => {
    locale.value = 'en'
    const text = modal().get('[data-test="security-reset"]').text()
    for (const value of ['UID 7', user.email, 'TOTP', 'passkeys', 'recovery codes', 'every device', 'current session', 'password', 'balance', 'API keys', 'bound email alone']) expect(text).toContain(value)
  })

  it('requires a nonblank note and separate unchecked confirmation', async () => {
    const wrapper = modal()
    expect((wrapper.get(confirm).element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get(submit).attributes('type')).toBe('button')
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    await wrapper.get(confirm).setValue(true)
    await wrapper.get(note).setValue('  \n  ')
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    await wrapper.get(note).setValue('Identity checked')
    await wrapper.get(confirm).setValue(false)
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    expect(resetAccountSecurity).not.toHaveBeenCalled()
  })

  it('enforces the 1000-byte UTF-8 limit rather than a character limit', async () => {
    const wrapper = modal()
    await prepare(wrapper, '核'.repeat(334))
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    expect(wrapper.get(note).attributes('aria-invalid')).toBe('true')
    await wrapper.get(note).setValue('核'.repeat(333) + 'a')
    expect(wrapper.get(submit).attributes('disabled')).toBeUndefined()
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(resetAccountSecurity).toHaveBeenCalledWith(7, { confirmed: true, verification_note: '核'.repeat(333) + 'a' })
  })

  it('posts only confirmation/note for props.user, preserves unsaved edits, and refreshes only on applied true', async () => {
    const wrapper = modal()
    await wrapper.get('input[type="email"]').setValue('unsaved@example.test')
    await prepare(wrapper, '  Verified in person  ')
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(resetAccountSecurity).toHaveBeenCalledTimes(1)
    expect(resetAccountSecurity).toHaveBeenCalledWith(7, { confirmed: true, verification_note: 'Verified in person' })
    expect(update).not.toHaveBeenCalled()
    expect(stepUpRun).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('[data-test="security-reset-applied"]').exists()).toBe(true)
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    expect((wrapper.get('input[type="email"]').element as HTMLInputElement).value).toBe('unsaved@example.test')
  })

  it.each([{ code: 'REAUTH_REQUIRED', status: 403 }, { reason: 'REAUTH_REQUIRED', status: 403 }, { status: 401 }])('links to verification without replay for %j', async failure => {
    resetAccountSecurity.mockRejectedValueOnce(failure)
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    const alert = wrapper.get('[data-test="security-reset-reauth"]')
    expect(alert.get('a').attributes('href')).toBe('https://account.ritel.si/security')
    expect(alert.text()).toContain('5 分钟')
    expect((wrapper.get(confirm).element as HTMLInputElement).checked).toBe(false)
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(resetAccountSecurity).toHaveBeenCalledTimes(1)
    expect(stepUpRun).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toBeUndefined()
    await wrapper.get(confirm).setValue(true)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(resetAccountSecurity).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it.each([{ status: 0 }, { status: 503 }, { status: 408 }, { status: 503, code: 'ACCOUNT_SECURITY_RESET_OUTCOME_UNKNOWN' }, { status: 502, code: 'REAUTH_REQUIRED' }])('keeps %j unknown after independent basic-user GET and blocks resubmission', async failure => {
    resetAccountSecurity.mockRejectedValueOnce(failure)
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(1)
    expect(getById).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-test="security-reset-unknown"]').text()).toContain('不能据此确认重置成功')
    expect(wrapper.get('[data-test="security-reset-readback"]').text()).toContain('UID 7')
    expect(wrapper.find('[data-test="security-reset-applied"]').exists()).toBe(false)
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="security-reset-readback-button"]').trigger('click')
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(2)
    expect(resetAccountSecurity).toHaveBeenCalledTimes(1)
  })

  it.each([{}, { applied: false }, null])('does not treat an unconfirmed 2xx response %j as success', async response => {
    resetAccountSecurity.mockResolvedValueOnce(response)
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="security-reset-unknown"]').exists()).toBe(true)
    expect(getById).toHaveBeenCalledWith(7)
    expect(wrapper.emitted('success')).toBeUndefined()
  })

  it('keeps a failed or mismatched readback unknown', async () => {
    resetAccountSecurity.mockRejectedValueOnce({ status: 502 })
    getById.mockRejectedValueOnce({ status: 401 })
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="security-reset-readback-failed"]').exists()).toBe(true)
    getById.mockResolvedValueOnce({ ...user, id: 9 })
    await wrapper.get('[data-test="security-reset-readback-button"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="security-reset-readback"]').exists()).toBe(false)
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('success')).toBeUndefined()
  })

  it('shows a rejected 4xx request without claiming success, and requires confirmation again', async () => {
    resetAccountSecurity.mockRejectedValueOnce({ status: 403, code: 'ACCOUNT_SECURITY_RESET_ADMIN_REQUIRED' })
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="security-reset-rejected"]').exists()).toBe(true)
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(getById).not.toHaveBeenCalled()
  })

  it('guards concurrent clicks and normal form saves while the reset is pending', async () => {
    const pending = deferred<{ applied: boolean }>()
    resetAccountSecurity.mockReturnValueOnce(pending.promise)
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await wrapper.get(submit).trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(resetAccountSecurity).toHaveBeenCalledTimes(1)
    expect(update).not.toHaveBeenCalled()
    expect(wrapper.get(confirm).attributes('disabled')).toBeDefined()
    pending.resolve({ applied: true })
    await flushPromises()
  })

  it('clears identity verification on target changes and ignores stale outcomes for the next user', async () => {
    const pending = deferred<{ applied: boolean }>()
    resetAccountSecurity.mockReturnValueOnce(pending.promise)
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await wrapper.setProps({ user: { ...user, id: 9, email: 'other@example.test' } as never })
    expect((wrapper.get(note).element as HTMLTextAreaElement).value).toBe('')
    expect((wrapper.get(confirm).element as HTMLInputElement).checked).toBe(false)
    pending.resolve({ applied: true })
    await flushPromises()
    expect(wrapper.find('[data-test="security-reset-applied"]').exists()).toBe(false)
    expect(wrapper.emitted('success')).toHaveLength(1)
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    expect(resetAccountSecurity.mock.calls[1][0]).toBe(9)
  })

  it('preserves the applied result when parent refresh replaces the same user object', async () => {
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.get(submit).trigger('click')
    await flushPromises()
    await wrapper.setProps({ user: { ...user, username: 'refreshed' } as never })
    expect(wrapper.find('[data-test="security-reset-applied"]').exists()).toBe(true)
    expect(wrapper.get(submit).attributes('disabled')).toBeDefined()
  })

  it('clears notes and confirmation when closed and reopened', async () => {
    const wrapper = modal()
    await prepare(wrapper)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect((wrapper.get(note).element as HTMLTextAreaElement).value).toBe('')
    expect((wrapper.get(confirm).element as HTMLInputElement).checked).toBe(false)
    expect(resetAccountSecurity).not.toHaveBeenCalled()
  })
})
