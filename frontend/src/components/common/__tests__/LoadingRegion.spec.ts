import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import LoadingRegion from '../LoadingRegion.vue'

it('keeps one localized name for a group and reacts to locale changes', async () => {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { common: { loading: 'Loading' } }, zh: { common: { loading: '載入中' } } } })
  const wrapper = mount(LoadingRegion, { global: { plugins: [i18n] }, slots: { default: '<div></div><div></div><div></div>' } })
  expect(wrapper.findAll('[role="status"]')).toHaveLength(1)
  expect(wrapper.attributes('aria-label')).toBe('Loading')
  expect(wrapper.attributes('aria-busy')).toBe('true')
  expect(wrapper.findAll('.sr-only')).toHaveLength(1)
  i18n.global.locale.value = 'zh'
  await wrapper.vm.$nextTick()
  expect(wrapper.attributes('aria-label')).toBe('載入中')
  expect(wrapper.text()).toBe('載入中')
  wrapper.unmount()
})

it('lets repeated inline cells stay readable without creating live regions', () => {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { common: { loading: 'Loading' } } } })
  const wrapper = mount(LoadingRegion, { props: { as: 'span', announce: false, label: 'Loading usage' }, global: { plugins: [i18n] } })
  expect(wrapper.element.tagName).toBe('SPAN')
  expect(wrapper.attributes('role')).toBeUndefined()
  expect(wrapper.attributes('aria-live')).toBeUndefined()
  expect(wrapper.text()).toBe('Loading usage')
  wrapper.unmount()
})
