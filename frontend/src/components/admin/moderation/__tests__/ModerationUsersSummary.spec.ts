import { beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModerationUsersSummary from '../ModerationUsersSummary.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
vi.mock('@/api/admin/riskControl', () => ({ listModerationUsers: vi.fn().mockResolvedValue({ items: [{ user_id: 12, status: 'active', current_count: 0 }], total: 1 }) }))
vi.mock('../ModerationUserPanel.vue', () => ({ default: { template: '<div />' } }))
beforeEach(() => localStorage.clear())
it('restores the real business table column order after remount', async () => {
  const wrapper = mount(ModerationUsersSummary); await flushPromises()
  await wrapper.get('th[data-column-key="status"] button').trigger('keydown', { key: 'Home' })
  expect(JSON.parse(localStorage.getItem('admin-moderation-users')!).order[0]).toBe('status')
  wrapper.unmount()
  const reopened = mount(ModerationUsersSummary); await flushPromises()
  expect(reopened.findAll('th[data-column-key]')[0].attributes('data-column-key')).toBe('status')
  expect(reopened.findAll('th[data-column-key]').at(-1)?.attributes('data-column-key')).toBe('actions')
  expect(reopened.text()).toContain('12')
  reopened.unmount()
})
