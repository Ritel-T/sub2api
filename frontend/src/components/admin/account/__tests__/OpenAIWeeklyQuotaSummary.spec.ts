import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Component from '../OpenAIWeeklyQuotaSummary.vue'
import en from '@/i18n/locales/en'
import { getOpenAIWeeklyQuota } from '@/api/admin/accounts'

vi.mock('@/api/admin/accounts', () => ({ getOpenAIWeeklyQuota: vi.fn() }))
const read = vi.mocked(getOpenAIWeeklyQuota)
const result = {
  generated_at: '2026-10-25T07:30:33Z', total_remaining: 8611.4, total_weekly_limit: 9800,
  included_accounts: 20, oldest_snapshot_at: '2026-10-25T07:01:11Z',
  tiers: [
    { plan: 'plus', label: 'Plus', weekly_limit: 140, account_count: 13, remaining: 1667.4 },
    { plan: 'prolite', label: 'Pro100', weekly_limit: 700, account_count: 3, remaining: 1638 },
    { plan: 'pro', label: 'Pro200', weekly_limit: 1400, account_count: 4, remaining: 5306 },
    { plan: 'promax', label: 'Pro500', weekly_limit: 3500, account_count: 0, remaining: 0 }
  ],
  excluded: { inactive: 1, non_weekly_window: 1, unsupported_plan: 6 }
}
function render() {
  return mount(Component, { props: { refreshToken: 0 }, global: {
    plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })],
    stubs: { Icon: true }
  } })
}
beforeEach(() => { vi.clearAllMocks(); read.mockResolvedValue(result) })
describe('global OpenAI weekly allowance', () => {
  it('loads the global summary without account-table filters and discloses tier totals and exclusions', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('8,611.4')
    expect(wrapper.find('table').exists()).toBe(false)
    await wrapper.find('button[aria-expanded]').trigger('click')
    expect(wrapper.find('button[aria-expanded]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('tbody tr')).toHaveLength(4)
    expect(wrapper.text()).toContain('1 error or disabled')
    expect(wrapper.text()).toContain('1 non-weekly windows')
    expect(wrapper.text()).toContain('independent of table filters')
    expect(read.mock.calls[0]).toEqual([{ signal: expect.any(AbortSignal) }])
    wrapper.unmount()
  })
  it('refreshes when the page refresh token changes and removes the previous estimate on failure', async () => {
    const wrapper = render(); await flushPromises()
    read.mockRejectedValueOnce(new Error('unavailable'))
    await wrapper.setProps({ refreshToken: 1 }); await flushPromises()
    expect(wrapper.text()).not.toContain('8,611.4')
    expect(wrapper.find('[role="alert"]').text()).toContain('Could not load')
    await wrapper.find('[role="alert"] button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('8,611.4')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('updates the estimate after account eligibility or quota changes', async () => {
    const wrapper = render(); await flushPromises()
    read.mockResolvedValueOnce({ ...result, total_remaining: 8471.4, included_accounts: 19 })
    await wrapper.setProps({ accountStateKey: '1:active:false:plus' }); await flushPromises()
    expect(read).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('8,471.4')
    expect(wrapper.text()).toContain('19 accounts included')
    wrapper.unmount()
  })
  it('cancels superseded reads and ignores their late results', async () => {
    let resolve!: (value: typeof result) => void
    read.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = render(); const signal = read.mock.calls[0][0]!.signal!
    await wrapper.setProps({ refreshToken: 1 }); await flushPromises()
    expect(signal.aborted).toBe(true)
    resolve({ ...result, total_remaining: 99999 }); await flushPromises()
    expect(wrapper.text()).toContain('8,611.4')
    expect(wrapper.text()).not.toContain('99,999')
    wrapper.unmount()
  })
  it('aborts the read when leaving the account page', () => {
    read.mockReturnValueOnce(new Promise(() => {}))
    const wrapper = render(); const signal = read.mock.calls[0][0]!.signal!
    wrapper.unmount(); expect(signal.aborted).toBe(true)
  })
  it('shows a genuine zero for an empty eligible pool', async () => {
    read.mockResolvedValueOnce({ ...result, included_accounts: 0, total_remaining: 0 })
    const wrapper = render(); await flushPromises()
    await wrapper.find('button[aria-expanded]').trigger('click')
    expect(wrapper.find('.weekly-quota-value').text()).toBe('0')
    expect(wrapper.text()).toContain('No eligible weekly snapshots')
    wrapper.unmount()
  })
})
