import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ServerlessSettings from '../ServerlessSettings.vue'
import en from '@/i18n/locales/en/consoleInline'
import zh from '@/i18n/locales/zh/consoleInline'
import hk from '@/i18n/locales/zh-Hant-HK/consoleInline'

const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: api }))
let wrapper: VueWrapper | undefined
beforeEach(() => {
  api.get.mockReset().mockResolvedValue({ data: { config: { enabled: false, pods: [], regions: [] }, pods: [], stats: {} } })
  api.put.mockReset().mockImplementation(async (_path, config) => ({ data: structuredClone(config) }))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('9b/9c Serverless merge contract', () => {
  for (const locale of ['en', 'zh', 'zh-Hant-HK']) {
    it.each([true, false])(`keeps partition ownership and translated captions in ${locale} (managed=%s)`, async managed => {
      const i18n = createI18n({ legacy: false, locale, messages: { en: { consoleInline: en }, zh: { consoleInline: zh }, 'zh-Hant-HK': { consoleInline: hk } } })
      const t = i18n.global.t
      wrapper = mount(ServerlessSettings, { props: { managed }, global: { plugins: [i18n], stubs: { AppHeading: { template: '<h3><slot /></h3>' }, ConfirmationPrompt: true } } })
      const vm = wrapper.vm as unknown as { dirty: boolean; save: () => Promise<void>; discard: () => void }
      const saveButtons = () => wrapper!.findAll('button').filter(button => button.text() === t('consoleInline.saveServerlessConfiguration_9c8473'))
      const details = wrapper.get('details')
      ;(details.element as HTMLDetailsElement).open = true
      await details.trigger('toggle')
      await flushPromises()
      expect(api.get).toHaveBeenCalledTimes(1)
      expect(vm.dirty).toBe(false)
      expect(saveButtons()).toHaveLength(0)
      const checkbox = wrapper.get<HTMLInputElement>('[data-testid="serverless-enabled"]')
      expect(checkbox.element.closest('label')?.textContent).toContain(t('consoleInline.enableRegionRouting_14520c'))
      await checkbox.setValue(true)
      expect(vm.dirty).toBe(true)
      expect(saveButtons()).toHaveLength(managed ? 0 : 1)
      if (managed) await vm.save()
      else await saveButtons()[0].trigger('click')
      await flushPromises()
      expect(api.put).toHaveBeenCalledTimes(1)
      expect(api.put).toHaveBeenCalledWith('/admin/serverless', { enabled: true, pods: [], regions: [] })
      expect(vm.dirty).toBe(false)
      expect(wrapper.text()).toContain(t('consoleInline.savedNewRulesTakeEffectWithinSeconds_89cf4e'))
      expect(saveButtons()).toHaveLength(0)
      await checkbox.setValue(false)
      expect(vm.dirty).toBe(true)
      vm.discard()
      await flushPromises()
      expect(vm.dirty).toBe(false)
      expect(checkbox.element.checked).toBe(true)
      expect(api.put).toHaveBeenCalledTimes(1)
    })
  }
})
