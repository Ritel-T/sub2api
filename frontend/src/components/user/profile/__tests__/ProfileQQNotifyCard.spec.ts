import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProfileQQNotifyCard from '../ProfileQQNotifyCard.vue'
import type { QQStatus } from '@/api/qq'
import { ref } from 'vue'
import { ConfirmationSessionKey } from '@/composables/useConfirmation'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
const { api, success } = vi.hoisted(() => ({
  api: { status: vi.fn(), createBinding: vi.fn(), confirm: vi.fn(), unlink: vi.fn(), preferences: vi.fn() },
  success: vi.fn()
}))
vi.mock('@/api/qq', () => ({ qqAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: success }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
enableAutoUnmount(afterEach)
const initial: QQStatus = { enabled: true, bot_qq: '123456789', qq: null, pending_qq: null, binding_id: 'challenge-id', expires_at: null, channel: 'email', balance_low: true, subscription_expiry: true }
const mountCard = (session = ref('user-a')) => mount(ProfileQQNotifyCard, { global: {
  provide: { [ConfirmationSessionKey as symbol]: () => ({ active: !!session.value, identity: session.value }) },
  stubs: { ConfirmDialog: { props: ['show', 'busy'], template: '<button v-if="show" data-testid="confirm-unlink" :disabled="busy" @click="$emit(\'confirm\')">confirm</button>' } }
} })
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const button = (wrapper: ReturnType<typeof mountCard>, text: string) => wrapper.findAll('button').find(b => b.text() === text)!

describe('ProfileQQNotifyCard', () => {
  beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers(); api.status.mockResolvedValue({ ...initial }) })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

  it('shows unavailable state with no binding or preference actions', async () => {
    api.status.mockResolvedValue({ ...initial, enabled: false })
    const wrapper = mountCard(); await flushPromises()
    expect(wrapper.text()).toContain('qq.unavailable')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(api.createBinding).not.toHaveBeenCalled()
  })

  it('issues a code, requires explicit candidate confirmation, and reads back binding', async () => {
    api.createBinding.mockResolvedValue({ code: 'ABCDEF', bot_qq: initial.bot_qq, expires_at: new Date(Date.now() + 60000).toISOString() })
    const wrapper = mountCard(); await flushPromises()
    expect(wrapper.get('select').attributes('disabled')).toBeDefined()
    expect(wrapper.get('h2').attributes('data-type-role')).toBe('section')
    expect(wrapper.get('label').attributes('for')).toBe(wrapper.get('select').attributes('id'))
    expect(wrapper.get('select').attributes('aria-describedby')).toBe(wrapper.findAll('p').find(p => p.text() === 'qq.channelHint')!.attributes('id'))
    await button(wrapper, 'qq.createCode').trigger('click'); await flushPromises()
    expect(wrapper.get('code').text()).toBe('/sub 绑定 ABCDEF')
    api.status.mockResolvedValueOnce({ ...initial, pending_qq: '12****89', expires_at: new Date(Date.now() + 60000).toISOString() })
    await button(wrapper, 'qq.refresh').trigger('click'); await flushPromises()
    expect(api.confirm).not.toHaveBeenCalled()
    api.status.mockResolvedValueOnce({ ...initial, qq: '12****89' })
    await button(wrapper, 'qq.confirm').trigger('click'); await flushPromises()
    expect(api.confirm).toHaveBeenCalledWith('12****89', 'challenge-id')
    expect(wrapper.text()).toContain('12****89')
    expect(wrapper.find('code').exists()).toBe(false)
  })

  it('expires a pending candidate locally and cleans up its timer', async () => {
    api.status.mockResolvedValue({ ...initial, pending_qq: '12****89', expires_at: new Date(Date.now() + 2000).toISOString() })
    const wrapper = mountCard(); await flushPromises()
    expect(button(wrapper, 'qq.confirm')).toBeTruthy()
    await vi.advanceTimersByTimeAsync(3000)
    expect(button(wrapper, 'qq.confirm')).toBeUndefined()
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shows fetch errors with retry and does not claim disabled configuration', async () => {
    api.status.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountCard(); await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('qq.unavailable')
    await button(wrapper, 'qq.retry').trigger('click'); await flushPromises()
    expect(wrapper.find('select').exists()).toBe(true)
  })

  it('keeps edited preferences after failed save and submits only notification fields', async () => {
    api.status.mockResolvedValue({ ...initial, qq: '12****89' })
    const wrapper = mountCard(); await flushPromises()
    await wrapper.get('select').setValue('both')
    api.preferences.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.preferences).toHaveBeenCalledWith({ channel: 'both', balance_low: true, subscription_expiry: true })
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('both')
    expect(success).not.toHaveBeenCalled()
    api.status.mockResolvedValue({ ...initial, qq: '12****89', channel: 'both' })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(success).toHaveBeenCalledWith('common.saved')
  })

  it('does not unlink before confirmation and reads back email channel after unlink', async () => {
    api.status.mockResolvedValue({ ...initial, qq: '12****89', channel: 'qq' })
    const wrapper = mountCard(); await flushPromises()
    await button(wrapper, 'qq.unlink').trigger('click')
    expect(api.unlink).not.toHaveBeenCalled()
    api.status.mockResolvedValue({ ...initial })
    await wrapper.get('[data-testid="confirm-unlink"]').trigger('click'); await flushPromises()
    expect(api.unlink).toHaveBeenCalledOnce()
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('email')
  })


  it('keeps edited preferences when a refresh returns and when edits arrive during save readback', async () => {
    const wrapper = mountCard(); await flushPromises()
    api.createBinding.mockResolvedValue({ code: 'ABCDEF', bot_qq: initial.bot_qq, expires_at: new Date(Date.now() + 60000).toISOString() })
    await button(wrapper, 'qq.createCode').trigger('click'); await flushPromises()
    const checkbox = wrapper.findAll('input[type="checkbox"]')[0]
    await checkbox.setValue(false)
    await button(wrapper, 'qq.refresh').trigger('click'); await flushPromises()
    expect((checkbox.element as HTMLInputElement).checked).toBe(false)
    const readback = deferred<QQStatus>()
    api.status.mockReturnValueOnce(readback.promise)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.preferences).toHaveBeenCalledWith({ channel: 'email', balance_low: false, subscription_expiry: true })
    await checkbox.setValue(true)
    readback.resolve({ ...initial, balance_low: false })
    await flushPromises()
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    expect(button(wrapper, 'common.save').attributes('disabled')).toBeUndefined()
  })

  it('owns unlink busy through readback, ignores duplicate acceptance and blocks cancellation', async () => {
    api.status.mockResolvedValue({ ...initial, qq: '12****89', channel: 'qq' })
    const wrapper = mountCard(); await flushPromises()
    const mutation = deferred<void>()
    const readback = deferred<QQStatus>()
    api.unlink.mockReturnValueOnce(mutation.promise)
    api.status.mockReturnValueOnce(readback.promise)
    await button(wrapper, 'qq.unlink').trigger('click')
    const dialog = wrapper.getComponent(ConfirmDialog)
    dialog.vm.$emit('confirm'); dialog.vm.$emit('confirm')
    await wrapper.vm.$nextTick()
    expect(api.unlink).toHaveBeenCalledOnce()
    expect(dialog.props('show')).toBe(true)
    expect(dialog.props('busy')).toBe(true)
    dialog.vm.$emit('cancel')
    await wrapper.vm.$nextTick()
    expect(dialog.props('show')).toBe(true)
    mutation.resolve(); await flushPromises()
    expect(dialog.props('show')).toBe(true)
    expect(dialog.props('busy')).toBe(true)
    readback.resolve({ ...initial }); await flushPromises()
    expect(dialog.props('show')).toBe(false)
    expect(dialog.props('busy')).toBe(false)
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('email')
  })

  it('cancels an unaccepted unlink on same-tick identity A to B to A', async () => {
    const session = ref('user-a')
    api.status.mockResolvedValue({ ...initial, qq: '12****89' })
    const wrapper = mountCard(session); await flushPromises()
    await button(wrapper, 'qq.unlink').trigger('click')
    const dialog = wrapper.getComponent(ConfirmDialog)
    session.value = 'user-b'; session.value = 'user-a'
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(api.unlink).not.toHaveBeenCalled()
    expect(dialog.props('show')).toBe(false)
  })

  it.each(['success', 'failure'])('suppresses stale initial load %s after identity replacement', async outcome => {
    const session = ref('user-a')
    const old = deferred<QQStatus>()
    api.status.mockReturnValueOnce(old.promise)
    const wrapper = mountCard(session); await flushPromises()
    session.value = 'user-b'; session.value = 'user-a'
    if (outcome === 'success') old.resolve({ ...initial, qq: 'old-user-qq' })
    else old.reject(new Error('old-user-error'))
    await flushPromises()
    expect(api.status).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('old-user')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('select').exists()).toBe(true)
  })

  it.each(['success', 'failure'])('suppresses stale save %s and defers current-context refresh until the old owner releases', async outcome => {
    const session = ref('user-a')
    api.status.mockResolvedValue({ ...initial, qq: '12****89' })
    const wrapper = mountCard(session); await flushPromises()
    await wrapper.get('select').setValue('both')
    const old = deferred<void>()
    api.preferences.mockReturnValueOnce(old.promise)
    await wrapper.get('form').trigger('submit')
    session.value = 'user-b'; session.value = 'user-a'
    await wrapper.vm.$nextTick()
    expect(wrapper.find('select').exists()).toBe(false)
    expect(api.status).toHaveBeenCalledTimes(1)
    if (outcome === 'success') old.resolve()
    else old.reject(new Error('old-user-error'))
    await flushPromises()
    expect(api.preferences).toHaveBeenCalledOnce()
    expect(api.status).toHaveBeenCalledTimes(2)
    expect(success).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('email')
  })

  it('rejects a nested save readback after the context changes and never announces old success', async () => {
    const session = ref('user-a')
    api.status.mockResolvedValue({ ...initial, qq: '12****89' })
    const wrapper = mountCard(session); await flushPromises()
    await wrapper.get('select').setValue('both')
    const readback = deferred<QQStatus>()
    api.status.mockReturnValueOnce(readback.promise)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.status).toHaveBeenCalledTimes(2)
    session.value = 'user-b'; session.value = 'user-a'
    readback.resolve({ ...initial, qq: 'old-user-qq', channel: 'both' })
    await flushPromises()
    expect(api.status).toHaveBeenCalledTimes(3)
    expect(wrapper.text()).not.toContain('old-user-qq')
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('email')
    expect(success).not.toHaveBeenCalled()
  })

  it.each(['success', 'failure'])('rejects stale unlink %s without a followup read or error in the replacement identity', async outcome => {
    const session = ref('user-a')
    api.status.mockResolvedValue({ ...initial, qq: '12****89' })
    const wrapper = mountCard(session); await flushPromises()
    const old = deferred<void>()
    api.unlink.mockReturnValueOnce(old.promise)
    await button(wrapper, 'qq.unlink').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    session.value = 'user-b'; session.value = 'user-a'
    if (outcome === 'success') old.resolve()
    else old.reject(new Error('old-user-error'))
    await flushPromises()
    expect(api.unlink).toHaveBeenCalledOnce()
    expect(api.status).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(false)
    expect(wrapper.getComponent(ConfirmDialog).props('busy')).toBe(false)
  })
})
