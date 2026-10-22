import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Panel from '../ModerationUserPanel.vue'
import Summary from '../ModerationUsersSummary.vue'
const { get, manage, list } = vi.hoisted(() => ({ get: vi.fn(), manage: vi.fn(), list: vi.fn() }))
vi.mock('@/api/admin/riskControl', () => ({ getModerationUser: get, manageModerationUser: manage, listModerationUsers: list }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const initial = { user_id: 306, version: 2, generation: 1, current_count: 10, auto_ban_exempt: false, owned_disable: true, status: 'disabled', reason: '', actor_id: null, updated_at: null }
const options = { global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } } }
beforeEach(() => { vi.resetAllMocks(); get.mockResolvedValue({ ...initial }); manage.mockResolvedValue({ ...initial, version: 3, current_count: 0, generation: 2 }); list.mockResolvedValue({ items: [], total: 0 }) })
describe('moderation user controls', () => {
  it('requires confirmation and reason; resetting retains disable and does not send an exemption', async () => {
    const wrapper = mount(Panel, { props: { userId: 306 } })
    await flushPromises()
    expect(wrapper.get('[data-test="reset"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="reason"]').setValue('reviewed')
    await wrapper.get('[data-test="confirm"]').setValue(true)
    await wrapper.get('[data-test="reset"]').trigger('click'); await flushPromises()
    expect(manage).toHaveBeenCalledWith(306, { action: 'reset', version: 2, reason: 'reviewed', confirm: true })
    expect(wrapper.emitted('changed')?.[0]?.[0]).toMatchObject({ status: 'disabled', current_count: 0 })
    expect(wrapper.get('[data-test="confirm"]').element).toHaveProperty('checked', false)
  })
  it('does not offer restoration for another source, and allowlisting is an explicit separate action', async () => {
    get.mockResolvedValue({ ...initial, owned_disable: false })
    const wrapper = mount(Panel, { props: { userId: 306 } }); await flushPromises()
    expect(wrapper.get('[data-test="unban"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="exempt"]').setValue(true)
    await wrapper.get('[data-test="reason"]').setValue('trusted intermediary')
    await wrapper.get('[data-test="confirm"]').setValue(true)
    await wrapper.get('[data-test="save-exemption"]').trigger('click'); await flushPromises()
    expect(manage).toHaveBeenCalledWith(306, { action: 'exemption', version: 2, reason: 'trusted intermediary', confirm: true, auto_ban_exempt: true })
  })
  it('shows a failure without reporting success or overwriting current state', async () => {
    manage.mockRejectedValue({ response: { data: { message: 'MODERATION_STATE_CONFLICT' } } })
    const wrapper = mount(Panel, { props: { userId: 306 } }); await flushPromises()
    await wrapper.get('[data-test="reason"]').setValue('reviewed'); await wrapper.get('[data-test="confirm"]').setValue(true)
    await wrapper.get('[data-test="unban"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('MODERATION_STATE_CONFLICT')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })
  it('reloads persisted exemption and permits revocation', async () => {
    get.mockResolvedValue({ ...initial, auto_ban_exempt: true })
    const wrapper = mount(Panel, { props: { userId: 306 } }); await flushPromises()
    expect(wrapper.get('[data-test="exempt"]').element).toHaveProperty('checked', true)
    await wrapper.get('[data-test="exempt"]').setValue(false); await wrapper.get('[data-test="reason"]').setValue('revoked'); await wrapper.get('[data-test="confirm"]').setValue(true)
    await wrapper.get('[data-test="save-exemption"]').trigger('click'); await flushPromises()
    expect(manage.mock.calls[0][1]).toMatchObject({ action: 'exemption', auto_ban_exempt: false })
  })
  it('shows empty state, filters and paginates without requesting sensitive content', async () => {
    const wrapper = mount(Summary, options); await flushPromises()
    expect(wrapper.get('[data-test="empty"]').exists()).toBe(true)
    expect(list).toHaveBeenLastCalledWith({ page: 1, page_size: 20, state: 'exempt' })
    list.mockResolvedValue({ items: [initial], total: 21 })
    await wrapper.findAll('[role="tab"]')[1].trigger('click'); await flushPromises()
    await wrapper.get('[data-test="next"]').trigger('click'); await flushPromises()
    expect(list).toHaveBeenLastCalledWith({ page: 2, page_size: 20, state: 'all' })
    expect(wrapper.get('[data-test="next"]').attributes('disabled')).toBeDefined()
  })
})
