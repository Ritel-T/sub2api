import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import CcsImportSettings from '../CcsImportSettings.vue'
import type { CcsImportConfig, CcsImportProfile } from '@/types/ccswitch'
import { buildCcSwitchImportDeeplink, createDefaultCcSwitchImportConfig } from '@/utils/ccswitchImport'

const openaiProfile = (): CcsImportProfile => ({
  id: 'openai-codex', label: 'Codex', platform: 'openai', app: 'codex', parameters: { model: 'gpt-6-astra' },
})

function mountEditor(initial: CcsImportConfig = { profiles: [openaiProfile()] }) {
  const value = ref(initial)
  const host = mount(defineComponent({
    setup: () => () => h(CcsImportSettings, {
      modelValue: value.value,
      'onUpdate:modelValue': (config: CcsImportConfig) => { value.value = config },
    }),
  }), {
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN' })] },
  })
  return { wrapper: host.findComponent(CcsImportSettings), value }
}

describe('CcsImportSettings', () => {
  it('accepts empty default formats loaded through the admin API', () => {
    const { wrapper } = mountEditor({ profiles: [{ ...openaiProfile(), parameters: { apiFormat: '', configFormat: '' } }] })
    expect(wrapper.vm.validate()).toBeNull()
  })

  it('requires an explicit supported format for Hermes on Gemini profiles', async () => {
    const { wrapper } = mountEditor({ profiles: [{
      id: 'gemini-hermes', label: 'Hermes', platform: 'gemini', app: 'hermes', parameters: {}
    }] })
    expect(wrapper.vm.validate()).toContain('Hermes')
    await wrapper.get('[data-testid="ccs-param-apiFormat"]').setValue('openai_chat')
    expect(wrapper.vm.validate()).toBeNull()
  })
  it('synchronizes a model edit immediately into the saved config and generated link', async () => {
    const { wrapper, value } = mountEditor({ profiles: [{ ...openaiProfile(), parameters: { model: 'gpt-5.5' } }] })
    await wrapper.get('[data-testid="ccs-param-model"]').setValue('gpt-6-astra')
    const saved: CcsImportConfig = JSON.parse(JSON.stringify(value.value))
    expect(saved.profiles[0]?.parameters.model).toBe('gpt-6-astra')
    expect(wrapper.vm.validate()).toBeNull()
    const link = buildCcSwitchImportDeeplink({
      settings: saved, platform: 'openai', baseUrl: 'https://api.example.test', providerName: 'Test', apiKey: 'sk-user',
    })
    expect(new URL(link).searchParams.get('model')).toBe('gpt-6-astra')
  })

  it('retains advanced fields and raw CCS placeholders when another field changes', async () => {
    const profile = openaiProfile()
    const rawConfig = '[model]\nname = "{{apiKey}}"\n# 原始配置'
    const rawScript = '({ request: { url: "{{baseUrl}}/usage" } })'
    profile.parameters = {
      ...profile.parameters, apiFormat: 'openai_responses', enabled: false,
      configFormat: 'toml', config: rawConfig, usageScript: rawScript,
      usageEnabled: false, usageAutoInterval: 0, usageApiKey: '{{apiKey}}',
      usageBaseUrl: '{{baseUrl}}', usageAccessToken: '{{apiKey}}', usageUserId: '{{userId}}',
      endpoint: '{{baseUrl}},https://backup.example.test', notes: '备注', icon: 'openai', iconUrl: 'https://example.test/icon.png',
      haikuModel: 'haiku', sonnetModel: 'sonnet', opusModel: 'opus', homepage: 'https://example.test', name: '{{siteName}}',
    }
    const { wrapper, value } = mountEditor({ profiles: [profile] })
    await wrapper.get('[data-testid="ccs-label"]').setValue('Custom Codex')
    expect(value.value.profiles[0]?.parameters).toEqual(profile.parameters)
    expect(value.value.profiles[0]?.parameters.config).toBe(rawConfig)
    expect(value.value.profiles[0]?.parameters.usageScript).toBe(rawScript)
    expect(wrapper.vm.validate()).toBeNull()
    for (const key of Object.keys(profile.parameters)) {
      expect(wrapper.find(`[data-testid="ccs-param-${key}"]`).exists(), key).toBe(true)
    }
  })

  it('stores false and zero explicitly and removes an omitted option', async () => {
    const { wrapper, value } = mountEditor()
    await wrapper.get('[data-testid="ccs-param-enabled"]').setValue('false')
    await wrapper.get('[data-testid="ccs-param-usageAutoInterval"]').setValue('0')
    expect(value.value.profiles[0]?.parameters.enabled).toBe(false)
    expect(value.value.profiles[0]?.parameters.usageAutoInterval).toBe(0)
    await wrapper.get('[data-testid="ccs-param-enabled"]').setValue('')
    expect(value.value.profiles[0]?.parameters).not.toHaveProperty('enabled')
  })

  it('adds unique profiles, changes selection, reorders, and supports removing every option', async () => {
    const { wrapper, value } = mountEditor()
    await wrapper.get('[data-testid="ccs-add-profile"]').trigger('click')
    expect(value.value.profiles).toHaveLength(2)
    expect(new Set(value.value.profiles.map(profile => profile.id)).size).toBe(2)
    await wrapper.get('[data-testid="ccs-label"]').setValue('Second')
    expect(value.value.profiles[1]?.label).toBe('Second')
    await wrapper.get('[data-testid="ccs-up-1"]').trigger('click')
    expect(value.value.profiles[0]?.label).toBe('Second')
    await wrapper.get('[data-testid="ccs-select-1"]').trigger('click')
    expect((wrapper.get('[data-testid="ccs-label"]').element as HTMLInputElement).value).toBe('Codex')
    await wrapper.get('[data-testid="ccs-remove-0"]').trigger('click')
    await wrapper.get('[data-testid="ccs-remove-0"]').trigger('click')
    expect(value.value).toEqual({ profiles: [] })
    expect(wrapper.vm.validate()).toBeNull()
  })

  it('blocks invalid current edits instead of saving a stale valid value', async () => {
    const { wrapper, value } = mountEditor()
    await wrapper.get('[data-testid="ccs-param-usageAutoInterval"]').setValue('-1')
    expect(value.value.profiles[0]?.parameters.usageAutoInterval).toBe(-1)
    expect(wrapper.vm.validate()).toContain('查询间隔')
    expect(wrapper.get('[data-testid="ccs-error"]').text()).toContain('查询间隔')
    await wrapper.get('[data-testid="ccs-param-usageAutoInterval"]').setValue('15')
    await wrapper.get('[data-testid="ccs-label"]').setValue('')
    expect(value.value.profiles[0]?.label).toBe('')
    expect(wrapper.vm.validate()).toContain('不能为空')
  })

  it('validates application format compatibility after changing applications', async () => {
    const { wrapper } = mountEditor()
    await wrapper.get('[data-testid="ccs-param-apiFormat"]').setValue('openai_responses')
    await wrapper.get('[data-testid="ccs-app"]').setValue('gemini')
    expect(wrapper.vm.validate()).toContain('协议格式')
    const formats = wrapper.get('[data-testid="ccs-param-apiFormat"]').findAll('option').map(option => option.attributes('value'))
    expect(formats).toEqual(['', 'gemini_native'])
    await wrapper.get('[data-testid="ccs-param-apiFormat"]').setValue('gemini_native')
    expect(wrapper.vm.validate()).toBeNull()
  })

  it('rejects duplicate identifiers and unsupported provider parameters', async () => {
    const { wrapper } = mountEditor({ profiles: [openaiProfile(), openaiProfile()] })
    expect(wrapper.vm.validate()).toContain('重复')
    await wrapper.get('[data-testid="ccs-remove-1"]').trigger('click')
    const unsupported = mountEditor({ profiles: [{ ...openaiProfile(), parameters: { configUrl: 'https://example.test/config' } }] })
    expect(unsupported.wrapper.vm.validate()).toContain('configUrl')
  })

  it('validates raw JSON config, explicit TOML format, identifiers, and required Pi models', async () => {
    const { wrapper } = mountEditor()
    await wrapper.get('[data-testid="ccs-param-config"]').setValue('model_reasoning_effort = "low"')
    expect(wrapper.vm.validate()).toContain('JSON')
    await wrapper.get('[data-testid="ccs-param-configFormat"]').setValue('toml')
    expect(wrapper.vm.validate()).toBeNull()
    await wrapper.get('[data-testid="ccs-id"]').setValue('invalid id')
    expect(wrapper.vm.validate()).toContain('配置标识')
    await wrapper.get('[data-testid="ccs-id"]').setValue('valid-id')
    await wrapper.get('[data-testid="ccs-app"]').setValue('pi')
    await wrapper.get('[data-testid="ccs-param-model"]').setValue('')
    expect(wrapper.vm.validate()).toContain('Pi')
    await wrapper.get('[data-testid="ccs-param-model"]').setValue('gpt-6-astra')
    expect(wrapper.vm.validate()).toBeNull()
  })

  it('loads default profiles with Astra and preserves an explicitly empty config', () => {
    const defaults = createDefaultCcSwitchImportConfig()
    expect(defaults.profiles.find(profile => profile.platform === 'openai')?.parameters.model).toBe('gpt-6-astra')
    const { wrapper, value } = mountEditor({ profiles: [] })
    expect(value.value.profiles).toEqual([])
    expect(wrapper.find('[data-testid="ccs-param-model"]').exists()).toBe(false)
  })
})
