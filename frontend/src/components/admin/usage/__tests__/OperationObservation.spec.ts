import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import OperationObservation from '../OperationObservation.vue'
import en from '@/i18n/locales/en'
import type { OperationObservation as Observation } from '@/api/admin/usageTiming'
const global = { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] }
describe('operation observation evidence', () => {
 it('keeps legacy completion unknown', () => {
  const wrapper = mount(OperationObservation, { global })
  expect(wrapper.text()).toContain('Unknown: no v2 evidence')
  expect(wrapper.text()).toContain('not client receipt')
 })
 it('shows full wait separately from final first usable and recovery', () => {
  const operation = { version: 2, final_outcome: 'incomplete', reason: 'reasoning_only', finish_class: 'length', server_elapsed_ms: 150000, first_usable_ms: null, final_first_usable_ms: null, attempt_count: 2, failed_attempt_count: 1, delivery: 'written', evidence_status: 'observed', protocol: 'chat', transport: 'http', scope: 'application', recovered: false } as Observation
  const wrapper = mount(OperationObservation, { props: { operation }, global })
  expect(wrapper.text()).toContain('150.000s')
  expect(wrapper.text()).toContain('reasoning_only · length')
  expect(wrapper.text()).toContain('2 / 1')
  expect(wrapper.text()).not.toContain('undefined')
 })
})
