import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProfileQQNotifyCard from '../ProfileQQNotifyCard.vue'
import type { QQStatus } from '@/api/qq'
const { api, success } = vi.hoisted(() => ({
  api: { status: vi.fn(), createBinding: vi.fn(), confirm: vi.fn(), unlink: vi.fn(), preferences: vi.fn() },
  success: vi.fn()
}))
vi.mock('@/api/qq', () => ({ qqAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: success }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
enableAutoUnmount(afterEach)
const initial: QQStatus = { enabled: true, bot_qq: '123456789', qq: null, pending_qq: null, binding_id: 'challenge-id', expires_at: null, channel: 'email', balance_low: true, subscription_expiry: true }
const mountCard = () => mount(ProfileQQNotifyCard, { global: { stubs: { ConfirmDialog: { props: ['show'], template: '<button v-if="show" data-testid="confirm-unlink" @click="$emit(\'confirm\')">confirm</button>' } } } })
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
})
