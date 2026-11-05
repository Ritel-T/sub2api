import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import QuickKeyDefaultSettings from '../QuickKeyDefaultSettings.vue'
vi.mock('@/api/admin/groups', () => ({ getAll: vi.fn().mockResolvedValue([
  { id: 23, name: 'Unlimited', platform: 'deepseek', status: 'active' },
  { id: 817, name: 'Renamed domestic', platform: 'kimi', status: 'active' },
  { id: 12, name: 'OpenAI', platform: 'openai', status: 'active' }
]) }))
describe('quick key settings', () => {
 it('shows actual group names, emits stable IDs and clears overrides', async () => {
  const wrapper = mount(QuickKeyDefaultSettings, { props: { modelValue: { domestic: 817 } }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { common: { loading: 'Loading' } } } })] } })
  await flushPromises()
  const select = wrapper.get('select[data-bucket="domestic"]')
  expect(select.text()).toContain('Renamed domestic · #817')
  expect(select.text()).not.toContain('OpenAI')
  await select.setValue('23')
  expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ domestic: 23 }])
  await select.setValue('')
  expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([{}])
 })
})
