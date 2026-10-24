import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SecurityRecords from '../SecurityRecords.vue'
import * as api from '../api'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'

vi.mock('../api', () => ({ getSummary: vi.fn(), listEvents: vi.fn(), getEvent: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const event = (id: number, userId = 7): api.PenaltyEvent => ({ id, user_id: userId, api_key_id: 8, scope: 'session', policy: 'session_block', reason: `reason-${id}`, count: 3, model: 'model', source: 'prompt_audit', created_at: '2026-10-21T00:00:00Z' })
const pageResult = (page: number, ids: number[], total = ids.length): api.PenaltyPage => ({ page, page_size: 10, total, items: ids.map(id => event(id)) })

describe('personal security records', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.getSummary).mockResolvedValue({ enabled: true, observe_only: false, policy: 'session_block', threshold: 3, restricted: true, states: [{ scope: 'session', subject_key: 'hash-of-session', policy: 'session_block', count: 3, blocked_until: '2026-10-22T00:00:00Z', route_until: null, action: 'block', reason: 'jailbreak', updated_at: '2026-10-21T00:00:00Z' }] })
    vi.mocked(api.listEvents).mockResolvedValue({ page: 1, page_size: 10, total: 1, items: [{ id: 9, user_id: 7, api_key_id: 8, scope: 'session', policy: 'session_block', reason: 'jailbreak', count: 3, model: 'model', source: 'prompt_audit', created_at: '2026-10-21T00:00:00Z' }] })
  })
  it('shows restriction and cause, loading the original only on demand as text', async () => {
    vi.mocked(api.getEvent).mockResolvedValue({ id: 9, user_id: 7, api_key_id: 8, scope: 'session', policy: 'session_block', reason: 'jailbreak', count: 3, model: 'model', source: 'prompt_audit', created_at: '', full_prompt: '<script>danger()</script> original conversation' })
    const wrapper = mount(SecurityRecords)
    await flushPromises()
    expect(wrapper.get('[data-testid="restriction-status"]').text()).toBe('securityRecords.restricted')
    expect(wrapper.text()).toContain('jailbreak')
    expect(api.getEvent).not.toHaveBeenCalled()
    const expand = wrapper.findAll('button').find(button => button.text() === 'securityRecords.showPrompt')!
    await expand.trigger('click'); await flushPromises()
    expect(api.getEvent).toHaveBeenCalledWith(9, false)
    expect(wrapper.get('[data-testid="security-original-prompt"]').text()).toContain('<script>danger()</script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(api.listEvents).toHaveBeenCalledWith(1, false, undefined)
  })
  it('reports unavailable status instead of claiming unrestricted when status loading fails', async () => {
    vi.mocked(api.getSummary).mockRejectedValue(new Error('offline'))
    vi.mocked(api.getEvent).mockResolvedValue({ ...event(9), full_prompt: 'original conversation' })
    const wrapper = mount(SecurityRecords); await flushPromises()
    expect(wrapper.find('[data-testid="restriction-status"]').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toBe('securityRecords.loadFailed')
    await wrapper.findAll('button').find(button => button.text() === 'securityRecords.showPrompt')!.trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('securityRecords.loadFailed')
    wrapper.unmount()
  })
  it('retries append failures without losing earlier events and refreshes the loaded window', async () => {
    vi.mocked(api.listEvents).mockResolvedValueOnce(pageResult(1, [1], 2))
    const wrapper = mount(SecurityRecords, { props: { admin: true } }); await flushPromises()
    vi.mocked(api.listEvents).mockRejectedValueOnce(new Error('offline'))
    wrapper.findComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises()
    expect(wrapper.text()).toContain('reason-1')
    expect(wrapper.findComponent(InfiniteListSentinel).props('error')).toBeTruthy()
    vi.mocked(api.listEvents).mockResolvedValueOnce(pageResult(2, [1, 2], 2))
    wrapper.findComponent(InfiniteListSentinel).vm.$emit('retry'); await flushPromises()
    expect(api.listEvents).toHaveBeenLastCalledWith(2, true, undefined)
    expect(wrapper.findAll('article')).toHaveLength(2)
    vi.mocked(api.listEvents).mockResolvedValueOnce(pageResult(1, [3], 2)).mockResolvedValueOnce(pageResult(2, [4], 2))
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click'); await flushPromises()
    expect(vi.mocked(api.listEvents).mock.calls.slice(-2).map(([page]) => page)).toEqual([1, 2])
    expect(wrapper.text()).toContain('reason-3')
    expect(wrapper.text()).toContain('reason-4')
    expect(wrapper.text()).not.toContain('reason-1')
    wrapper.unmount()
  })
  it('discards late event pages and original prompts after the selected user changes', async () => {
    let resolveDetail!: (value: api.PenaltyEvent) => void
    let resolvePage!: (value: api.PenaltyPage) => void
    vi.mocked(api.listEvents).mockResolvedValueOnce(pageResult(1, [1], 2))
    vi.mocked(api.getEvent).mockImplementationOnce(() => new Promise(resolve => { resolveDetail = resolve }))
    const wrapper = mount(SecurityRecords, { props: { admin: true, userId: 7 } }); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'securityRecords.showPrompt')!.trigger('click')
    vi.mocked(api.listEvents).mockImplementationOnce(() => new Promise(resolve => { resolvePage = resolve }))
    wrapper.findComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises()
    vi.mocked(api.listEvents).mockResolvedValueOnce(pageResult(1, [8], 1))
    await wrapper.setProps({ userId: 8 }); await flushPromises()
    resolveDetail({ ...event(1), full_prompt: 'private original of user 7' })
    resolvePage(pageResult(2, [2], 2)); await flushPromises()
    expect(wrapper.text()).toContain('reason-8')
    expect(wrapper.text()).not.toContain('reason-2')
    expect(wrapper.text()).not.toContain('private original')
    expect(wrapper.find('[data-testid="security-original-prompt"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('discards personal summary and detail responses after switching into admin scope', async () => {
    let resolveSummary!: (value: api.PenaltySummary) => void
    let resolveDetail!: (value: api.PenaltyEvent) => void
    vi.mocked(api.getSummary).mockImplementationOnce(() => new Promise(resolve => { resolveSummary = resolve }))
    vi.mocked(api.getEvent).mockImplementationOnce(() => new Promise(resolve => { resolveDetail = resolve }))
    const wrapper = mount(SecurityRecords); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'securityRecords.showPrompt')!.trigger('click')
    await wrapper.setProps({ admin: true, userId: 8 }); await flushPromises()
    resolveSummary({ enabled: true, observe_only: false, policy: 'session_block', threshold: 3, restricted: true, states: [] })
    resolveDetail({ ...event(9), full_prompt: 'personal private original' }); await flushPromises()
    expect(wrapper.find('[data-testid="restriction-status"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="security-original-prompt"]').exists()).toBe(false)
    expect(api.listEvents).toHaveBeenLastCalledWith(1, true, 8)
    wrapper.unmount()
  })
  it('keeps only the most recently requested prompt expanded when detail requests race', async () => {
    let resolveFirst!: (value: api.PenaltyEvent) => void
    vi.mocked(api.listEvents).mockResolvedValueOnce(pageResult(1, [1, 2], 2))
    vi.mocked(api.getEvent).mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ ...event(2), full_prompt: 'second original' })
    const wrapper = mount(SecurityRecords, { props: { admin: true } }); await flushPromises()
    const buttons = wrapper.findAll('button').filter(button => button.text() === 'securityRecords.showPrompt')
    await buttons[0].trigger('click'); await buttons[1].trigger('click'); await flushPromises()
    resolveFirst({ ...event(1), full_prompt: 'first original' }); await flushPromises()
    expect(wrapper.get('[data-testid="security-original-prompt"]').text()).toBe('second original')
    wrapper.unmount()
  })
})
