import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { ConfirmationSessionKey } from '@/composables/useConfirmation'
import ConfirmationPrompt from '@/components/common/ConfirmationPrompt.vue'
import OpenAITOTPDialog from '../OpenAITOTPDialog.vue'

const api = vi.hoisted(() => ({ status: vi.fn(), rotate: vi.fn(), verify: vi.fn(), export: vi.fn(), session: vi.fn(), sessionStatus: vi.fn(), sessionDownload: vi.fn() }))
vi.mock('@/api/admin/openaiTotp', () => ({ getTOTPRotation: api.status, rotateTOTP: api.rotate, verifyTOTP: api.verify, exportTOTP: api.export, exportChatGPTSession: api.session, getChatGPTSessionExportTask: api.sessionStatus, downloadChatGPTSession: api.sessionDownload }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,synthetic') } }))
vi.mock('@/components/auth/TotpStepUpDialog.vue', () => ({ default: { name: 'TotpStepUpDialog', props: ['controller'], template: '<div />' } }))
let wrapper: VueWrapper
const dialog = { props: ['show'], emits: ['close'], template: '<div v-if="show"><slot/><slot name="footer"/><button data-testid="close" @click="$emit(\'close\')">close</button></div>' }
const exported = { email: 'owner@example.test', secret: 'SYNTHETIC-SECRET', otpauth_uri: 'otpauth://synthetic', source: 'current', state: 'succeeded' }
async function open(provide = {}) {
  wrapper = mount(OpenAITOTPDialog, { props: { accountId: 42, accountName: 'Test account', configured: true }, global: { provide, stubs: { BaseDialog: dialog } } })
  await wrapper.get('[data-testid="totp-manage"]').trigger('click'); await flushPromises()
}
beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers(); api.status.mockResolvedValue(null); api.export.mockResolvedValue(exported); api.session.mockResolvedValue({ id: 9, account_id: 42, status: 'queued', stage: 'queued', attempt: 1 }); api.sessionStatus.mockResolvedValue({ id: 9, account_id: 42, status: 'succeeded', stage: 'succeeded', attempt: 1 }); api.sessionDownload.mockResolvedValue({ accessToken: 'token', sessionToken: 'session' }) })
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.restoreAllMocks() })

describe('2FA rotation and credential export', () => {
  it('discards a rejected A rotation while the replacement account dialog is closed', async () => {
    let reject!: (error: Error) => void
    api.rotate.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail }))
    await open()
    await wrapper.get('[data-testid="totp-rotate"]').trigger('click')
    wrapper.getComponent(ConfirmationPrompt).props('confirmation').settle(true)
    expect(api.rotate).toHaveBeenCalledWith(42)
    await wrapper.setProps({ accountId: 43, accountName: 'B' })
    reject(new Error('Old A rotation failed'))
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it.each(['replace-account', 'session-close'])('ignores a late rotation rejection after %s', async (change) => {
    const session = reactive({ active: true, identity: 1 })
    let reject!: (reason: unknown) => void
    api.rotate.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail }))
    await open({ [ConfirmationSessionKey as symbol]: () => session })
    await wrapper.get('[data-testid="totp-rotate"]').trigger('click')
    wrapper.getComponent(ConfirmationPrompt).props('confirmation').settle(true)
    await flushPromises()
    expect(api.rotate).toHaveBeenCalledWith(42)
    if (change === 'replace-account') await wrapper.setProps({ accountId: 43, accountName: 'Replacement' })
    else { session.active = false; await flushPromises(); session.active = true }
    await wrapper.get('[data-testid="totp-manage"]').trigger('click')
    await flushPromises()
    reject(new Error('old rotation failure'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('tokenGuardV2.error')
    expect(wrapper.get('[data-testid="totp-rotate"]').attributes('disabled')).toBeUndefined()
  })

  it('clears account A credentials and ignores its late export under account B', async () => {
    let finish!: (value: typeof exported) => void
    api.export.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await open()
    await wrapper.get('[data-testid="totp-export"]').trigger('click')
    await wrapper.setProps({ accountId: 43, accountName: 'Replacement' })
    await wrapper.get('[data-testid="totp-manage"]').trigger('click')
    await flushPromises()
    finish(exported)
    await flushPromises()
    expect(wrapper.find('[data-testid="totp-export-result"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain(exported.secret)
    expect(api.status).toHaveBeenLastCalledWith(43)
  })

  it.each(['close', 'replace-account'])('rejects an old rotation confirmation after %s', async (change) => {
    await open()
    await wrapper.get('[data-testid="totp-rotate"]').trigger('click')
    const confirmation = wrapper.getComponent(ConfirmationPrompt).props('confirmation')
    expect(confirmation.show.value).toBe(true)
    if (change === 'close') await wrapper.get('[data-testid="close"]').trigger('click')
    else await wrapper.setProps({ accountId: 43, accountName: 'Replacement account' })
    confirmation.settle(true)
    await flushPromises()
    expect(api.rotate).not.toHaveBeenCalled()
    expect(confirmation.show.value).toBe(false)
  })

  it('does not fetch secrets until explicitly requested and omits password by default', async () => {
    await open(); expect(api.export).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="totp-export"]').trigger('click'); await flushPromises()
    expect(api.export).toHaveBeenCalledWith(42, 'current', false)
    expect(wrapper.get('[data-testid="totp-export-result"]').exists()).toBe(true)
    await wrapper.get('[data-testid="close"]').trigger('click'); await flushPromises()
    expect(wrapper.html()).not.toContain(exported.secret)
    expect(vi.getTimerCount()).toBe(0)
  })
  it('never offers another rotation while activation is uncertain', async () => {
    api.status.mockResolvedValue({ task_id: 7, account_id: 42, state: 'uncertain', action: 'rotate', has_candidate: true })
    await open()
    expect(wrapper.get('[data-testid="totp-rotate"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="totp-verify-new"]').trigger('click'); await flushPromises()
    expect(api.verify).toHaveBeenCalledWith(42, 7, 'verify_new')
    expect(api.rotate).not.toHaveBeenCalled()
  })
  it('exports the recovery candidate with an explicit source', async () => {
    api.status.mockResolvedValue({ task_id: 7, state: 'uncertain', has_candidate: true })
    await open(); await wrapper.get('[data-testid="totp-export"]').trigger('click'); await flushPromises()
    expect(api.export).toHaveBeenCalledWith(42, 'candidate', false)
  })
  it('ignores a late secret response after closing the dialog', async () => {
    let resolve!: (value: typeof exported) => void
    api.export.mockImplementation(() => new Promise(r => { resolve = r }))
    await open(); await wrapper.get('[data-testid="totp-export"]').trigger('click')
    await wrapper.get('[data-testid="close"]').trigger('click')
    resolve(exported); await flushPromises()
    await wrapper.get('[data-testid="totp-manage"]').trigger('click'); await flushPromises()
    expect(wrapper.html()).not.toContain(exported.secret)
    expect(wrapper.find('[data-testid="totp-export-result"]').exists()).toBe(false)
  })
  it('requires an explicit confirmation before queuing a replacement', async () => {
    await open(); await wrapper.get('[data-testid="totp-rotate"]').trigger('click'); expect(api.rotate).not.toHaveBeenCalled()
    const confirmation = wrapper.getComponent(ConfirmationPrompt)
    expect(confirmation.text()).toContain('tokenGuardV2')
    await confirmation.get('.btn-secondary').trigger('click'); await flushPromises()
    expect(api.rotate).not.toHaveBeenCalled()
    api.rotate.mockResolvedValue({ task_id: 7, state: 'queued' })
    await wrapper.get('[data-testid="totp-rotate"]').trigger('click')
    expect(api.rotate).not.toHaveBeenCalled()
    await confirmation.get('.btn-danger').trigger('click'); await flushPromises()
    expect(api.rotate).toHaveBeenCalledOnce(); expect(wrapper.get('[data-testid="totp-rotate"]').attributes('disabled')).toBeDefined()
  })
  it('starts ChatGPT Session export from the 2FA management dialog', async () => {
    await open()
    expect(wrapper.get('[data-testid="session-export"]').text()).toBe('tokenGuardV2.totpManage.sessionExport.button')
    await wrapper.get('[data-testid="session-export"]').trigger('click'); await flushPromises(); vi.advanceTimersByTime(1000); await flushPromises()
    expect(api.session).toHaveBeenCalledWith(42); expect(api.sessionStatus).toHaveBeenCalledWith(42, 9); expect(api.sessionDownload).toHaveBeenCalledWith(42, 9)
  })
})
