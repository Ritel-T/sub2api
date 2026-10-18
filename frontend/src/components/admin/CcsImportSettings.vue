<template>
  <section class="space-y-4 border-t border-gray-100 pt-5 dark:border-dark-700" aria-labelledby="ccs-import-settings-title">
    <div>
      <h3 id="ccs-import-settings-title" class="font-medium text-gray-900 dark:text-white">
        {{ localText('CC Switch 一键导入配置', 'CC Switch import profiles') }}
      </h3>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ localText('按 API Key 平台展示以下导入选项，顺序与此列表相同。删除全部配置可关闭导入选项。保存整页设置后生效。', 'Import options are shown for matching API key platforms in this order. Remove all profiles to disable import options. Save the settings page to apply changes.') }}
      </p>
    </div>
    <div class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
      {{ localText('这些模板会公开发送给用户，请勿填写管理员密钥、令牌或其他秘密。', 'These templates are published to users. Do not enter administrator keys, tokens, or other secrets.') }}
      {{ localText('元数据字段支持：', 'Metadata templates support:') }}
      <code v-for="token in templateTokens" :key="token" class="mr-2 inline-block">{{ token }}</code>
      <p class="mt-1">{{ localText('apiKey 为当前选中用户的 API Key；baseUrl 为站点 API 地址。原始 config 和 usageScript 不替换模板，以保留 CCS 自带占位符。链接中的 endpoint、apiKey 和 model 会覆盖内嵌配置的对应值。', 'apiKey is the selected user API key; baseUrl is the site API address. Raw config and usageScript retain CCS placeholders without template substitution. The link endpoint, apiKey, and model override matching embedded configuration values.') }}</p>
    </div>

    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ localText('未自定义的字段已填入当前默认值，可直接修改；清空后恢复默认。没有默认值的可选项保持空白。', 'Fields without overrides show their current defaults for editing. Clear a field to restore its default. Optional fields without defaults remain empty.') }}
    </p>

    <div class="flex flex-wrap items-center gap-2">
      <button type="button" class="btn btn-secondary btn-sm" data-testid="ccs-add-profile" :disabled="modelValue.profiles.length >= 64" @click="addProfile">
        {{ localText('添加导入配置', 'Add profile') }}
      </button>
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ modelValue.profiles.length }} / 64</span>
    </div>

    <p v-if="!modelValue.profiles.length" class="rounded-lg border border-dashed border-gray-300 p-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
      {{ localText('当前未配置任何导入选项。', 'No import options are configured.') }}
    </p>
    <div v-else class="grid gap-4 xl:grid-cols-[15rem_minmax(0,1fr)]">
      <div class="space-y-2">
        <div v-for="(profile, index) in modelValue.profiles" :key="index" class="flex items-center gap-1 rounded-lg border p-2" :class="index === selectedIndex ? 'border-primary-400 bg-primary-50 dark:bg-primary-900/10' : 'border-gray-200 dark:border-dark-600'">
          <button type="button" class="min-w-0 flex-1 text-left" :data-testid="`ccs-select-${index}`" :aria-pressed="index === selectedIndex" @click="selectedIndex = index">
            <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">{{ profile.label || localText('未命名配置', 'Unnamed profile') }}</span>
            <span class="block text-xs text-gray-500 dark:text-gray-400">{{ profile.platform }} / {{ profile.app }}</span>
          </button>
          <div class="flex flex-col">
            <button type="button" class="px-1 text-gray-500 disabled:opacity-30" :disabled="index === 0" :aria-label="localText('上移', 'Move up')" :data-testid="`ccs-up-${index}`" @click="moveProfile(index, -1)">↑</button>
            <button type="button" class="px-1 text-gray-500 disabled:opacity-30" :disabled="index === modelValue.profiles.length - 1" :aria-label="localText('下移', 'Move down')" :data-testid="`ccs-down-${index}`" @click="moveProfile(index, 1)">↓</button>
          </div>
          <button type="button" class="px-1 text-red-500" :aria-label="localText('删除配置', 'Remove profile')" :data-testid="`ccs-remove-${index}`" @click="removeProfile(index)">×</button>
        </div>
      </div>
      <div v-if="selectedProfile" class="min-w-0 space-y-5 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ localText('菜单名称', 'Menu label') }}</span>
            <input class="input" data-testid="ccs-label" :value="selectedProfile.label" @input="updateProfile({ label: inputValue($event) })" />
          </label>
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ localText('配置标识（唯一）', 'Profile ID (unique)') }}</span>
            <input class="input font-mono" data-testid="ccs-id" :value="selectedProfile.id" @input="updateProfile({ id: inputValue($event) })" />
          </label>
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ localText('API Key 平台', 'API key platform') }}</span>
            <select class="input" data-testid="ccs-platform" :value="selectedProfile.platform" @change="updateProfile({ platform: inputValue($event) as CcsImportPlatform })">
              <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
            </select>
          </label>
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ localText('CCS 客户端类型', 'CCS application') }}</span>
            <select class="input" data-testid="ccs-app" :value="selectedProfile.app" @change="updateProfile({ app: inputValue($event) as CcsImportApp })">
              <option v-for="app in apps" :key="app" :value="app">{{ app }}</option>
            </select>
          </label>
        </div>

        <details v-for="group in fieldGroups" :key="group.id" :open="group.id === 'provider'" class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <summary class="cursor-pointer text-sm font-medium text-gray-900 dark:text-white">{{ localText(group.zh, group.en) }}</summary>
          <div class="grid gap-4 pt-3 sm:grid-cols-2">
            <label v-for="field in group.fields" :key="field.key" class="space-y-1 text-sm text-gray-700 dark:text-gray-300" :class="field.multiline ? 'sm:col-span-2' : ''">
              <span>{{ localText(field.zh, field.en) }} <code class="text-xs text-gray-400">{{ field.key }}</code></span>
              <textarea v-if="field.multiline" class="input font-mono text-xs" :rows="field.key === 'config' || field.key === 'usageScript' ? 8 : 3" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" :placeholder="field.placeholder" spellcheck="false" @input="setParameter(field.key, inputValue($event))" />
              <select v-else-if="field.boolean" class="input" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" @change="setBooleanParameter(field.key, inputValue($event))">
                <option value="">{{ localText('留空（站点 / CCS 默认值）', 'Omit (site / CCS default)') }}</option>
                <option value="true">{{ localText('是', 'Yes') }}</option>
                <option value="false">{{ localText('否', 'No') }}</option>
              </select>
              <select v-else-if="field.key === 'apiFormat' || field.key === 'configFormat'" class="input" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" @change="setParameter(field.key, inputValue($event))">
                <option value="">{{ localText('留空（使用默认值）', 'Omit (use default)') }}</option>
                <option v-for="format in field.key === 'apiFormat' ? apiFormats : ['json', 'toml']" :key="format" :value="format">{{ format }}</option>
              </select>
              <input v-else class="input" :class="field.key === 'model' ? 'font-mono' : ''" :type="field.number ? 'number' : 'text'" :min="field.number ? 0 : undefined" :step="field.number ? 1 : undefined" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" :placeholder="field.placeholder" @input="setParameter(field.key, inputValue($event), field.number)" />
              <p v-if="field.hintZh" class="text-xs text-gray-500 dark:text-gray-400">{{ localText(field.hintZh, field.hintEn || '') }}</p>
            </label>
          </div>
        </details>
      </div>
    </div>
    <p v-if="validationError" role="alert" class="text-sm text-red-600 dark:text-red-400" data-testid="ccs-error">{{ validationError }}</p>
  </section>
</template>

<script setup lang="ts">
import { localizeInlineText } from '@/i18n/inline'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CcsImportApp, CcsImportConfig, CcsImportParameterValue, CcsImportPlatform, CcsImportProfile } from '@/types/ccswitch'
import { createDefaultCcSwitchImportConfig, getCcSwitchImportParameterDefaults } from '@/utils/ccswitchImport'

const props = defineProps<{ modelValue: CcsImportConfig; baseUrl?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: CcsImportConfig] }>()
const { locale } = useI18n()
const localText = (zh: string, en: string) => localizeInlineText(zh, en, locale.value)
const selectedIndex = ref(0)
const selectedProfile = computed(() => props.modelValue.profiles[selectedIndex.value])
const parameterDefaults = computed(() => selectedProfile.value
  ? getCcSwitchImportParameterDefaults(selectedProfile.value, props.baseUrl || '{{baseUrl}}') : {})
const templateTokens = ['{{apiKey}}', '{{baseUrl}}', '{{siteName}}', '{{keyName}}', '{{userId}}']
const platforms: CcsImportPlatform[] = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok']
const apps: CcsImportApp[] = ['claude', 'codex', 'gemini', 'grokbuild', 'opencode', 'openclaw', 'hermes', 'pi']
const apiFormats = computed(() => formatsForApp(selectedProfile.value?.app))

function formatsForApp(app?: CcsImportApp): string[] {
  if (app === 'gemini') return ['gemini_native']
  const formats = ['anthropic', 'openai_chat', 'openai_responses']
  return app && ['claude', 'opencode', 'openclaw', 'pi'].includes(app) ? [...formats, 'gemini_native'] : formats
}

interface ParameterField {
  key: string
  zh: string
  en: string
  multiline?: boolean
  boolean?: boolean
  number?: boolean
  placeholder?: string
  hintZh?: string
  hintEn?: string
}
const fieldGroups: Array<{ id: string; zh: string; en: string; fields: ParameterField[] }> = [
  {
    id: 'provider', zh: '提供商与模型', en: 'Provider and model', fields: [
      { key: 'name', zh: '提供商名称', en: 'Provider name', placeholder: '{{siteName}}' },
      { key: 'model', zh: '默认模型', en: 'Default model', placeholder: 'gpt-6-astra' },
      { key: 'endpoint', zh: 'API 地址 / 多地址', en: 'API endpoint / multiple endpoints', placeholder: '{{baseUrl}}', hintZh: '多个地址使用英文逗号分隔；具体 API 路径按客户端要求填写。', hintEn: 'Separate multiple endpoints with commas. Include the API path required by the selected client.' },
      { key: 'homepage', zh: '提供商主页', en: 'Provider homepage', placeholder: '{{baseUrl}}' },
      { key: 'apiFormat', zh: 'API 协议格式', en: 'API format', hintZh: '留空使用站点 / 客户端默认值。', hintEn: 'Leave blank for the site / application default.' },
      { key: 'enabled', zh: '导入后启用 / 切换', en: 'Enable / switch after import', boolean: true, hintZh: '显示 CCS 导入确认框的默认选项；用户仍可在导入时调整。', hintEn: 'Shows the default in the CCS import confirmation. Users can change it during import.' },
      { key: 'haikuModel', zh: 'Claude Haiku 模型别名', en: 'Claude Haiku model alias' },
      { key: 'sonnetModel', zh: 'Claude Sonnet 模型别名', en: 'Claude Sonnet model alias' },
      { key: 'opusModel', zh: 'Claude Opus 模型别名', en: 'Claude Opus model alias' },
    ],
  },
  {
    id: 'display', zh: '备注与图标', en: 'Notes and icon', fields: [
      { key: 'notes', zh: '备注', en: 'Notes', multiline: true },
      { key: 'icon', zh: '图标标识', en: 'Icon name' },
      { key: 'iconUrl', zh: '图标地址', en: 'Icon URL' },
    ],
  },
  {
    id: 'usage', zh: '用量查询', en: 'Usage query', fields: [
      { key: 'usageEnabled', zh: '启用用量查询', en: 'Enable usage query', boolean: true, hintZh: '留空使用站点默认值：启用。', hintEn: 'Site default when omitted: enabled.' },
      { key: 'usageAutoInterval', zh: '自动查询间隔（分钟）', en: 'Query interval (minutes)', number: true, placeholder: '30', hintZh: '留空使用站点默认 30 分钟；0 关闭自动查询。', hintEn: 'Site default: 30 minutes. 0 disables automatic queries.' },
      { key: 'usageApiKey', zh: '查询 Key 模板（公开）', en: 'Query key template (public)', placeholder: '{{apiKey}}', hintZh: '未自定义时继承提供商当前 Key。', hintEn: 'Inherits the current provider key when not customized.' },
      { key: 'usageBaseUrl', zh: '查询地址模板', en: 'Query base URL template', placeholder: '{{baseUrl}}', hintZh: '未自定义时跟随提供商当前地址。', hintEn: 'Follows the current provider endpoint when not customized.' },
      { key: 'usageAccessToken', zh: '查询令牌模板（公开）', en: 'Query token template (public)' },
      { key: 'usageUserId', zh: '查询用户 ID 模板', en: 'Query user ID template', placeholder: '{{userId}}' },
      { key: 'usageScript', zh: '原始用量查询脚本', en: 'Raw usage query script', multiline: true, hintZh: '留空使用站点内置余额脚本；自定义脚本在生成链接时自动编码。', hintEn: 'Leave blank for the built-in site balance script. Custom scripts are encoded when generating the link.' },
    ],
  },
  {
    id: 'config', zh: '客户端原始配置', en: 'Raw client configuration', fields: [
      { key: 'configFormat', zh: '配置格式', en: 'Configuration format', placeholder: 'json / toml', hintZh: '留空使用站点默认 JSON；填写 TOML 时请显式选择 toml。', hintEn: 'The site default is JSON. Select toml explicitly when entering TOML.' },
      { key: 'config', zh: '原始配置', en: 'Raw configuration', multiline: true, hintZh: '填写原始 JSON 对象 / TOML；生成链接时自动编码。可分发推理强度、上下文等客户端原生设置，例如 Codex TOML：model_reasoning_effort = "low"。仅填写希望分发给用户的设置。', hintEn: 'Enter a raw JSON object / TOML, encoded when generating the link. This can distribute native client settings such as reasoning effort and context size, e.g. Codex TOML: model_reasoning_effort = "low". Include only settings intended for users.' },
    ],
  },
]
const supportedParameters = new Set(fieldGroups.flatMap(group => group.fields.map(field => field.key)))
const booleanParameters = new Set(['enabled', 'usageEnabled'])

watch(() => props.modelValue.profiles.length, length => {
  selectedIndex.value = Math.max(0, Math.min(selectedIndex.value, length - 1))
})

function inputValue(event: Event): string {
  return (event.target as HTMLInputElement).value
}

function replaceProfiles(profiles: CcsImportProfile[]) {
  emit('update:modelValue', { profiles })
}

function updateProfile(patch: Partial<CcsImportProfile>) {
  replaceProfiles(props.modelValue.profiles.map((profile, index) => index === selectedIndex.value ? { ...profile, ...patch } : profile))
}

function parameterText(key: string): string {
  const value = selectedProfile.value?.parameters[key] ?? parameterDefaults.value[key]
  // These empty strings are accepted by the API and inherit the current default.
  if (value === '' && ['name', 'homepage', 'endpoint', 'apiFormat', 'configFormat', 'usageApiKey', 'usageBaseUrl'].includes(key)) return String(parameterDefaults.value[key] ?? '')
  return value === undefined ? '' : String(value)
}

function setParameter(key: string, value: string, number = false) {
  const parameters = { ...selectedProfile.value?.parameters }
  if (value === '') delete parameters[key]
  else parameters[key] = number ? Number(value) : value
  updateProfile({ parameters })
}

function setBooleanParameter(key: string, value: string) {
  const parameters: Record<string, CcsImportParameterValue> = { ...selectedProfile.value?.parameters }
  if (value === '') delete parameters[key]
  else parameters[key] = value === 'true'
  updateProfile({ parameters })
}

function addProfile() {
  if (props.modelValue.profiles.length >= 64) return
  const template = createDefaultCcSwitchImportConfig().profiles.find(profile => profile.platform === 'openai')
  let suffix = 1
  while (props.modelValue.profiles.some(profile => profile.id === `openai-codex-${suffix}`)) suffix++
  const profile: CcsImportProfile = {
    id: `openai-codex-${suffix}`, label: `OpenAI Codex ${suffix}`, platform: 'openai', app: 'codex',
    parameters: template ? { ...template.parameters } : { model: 'gpt-6-astra' },
  }
  selectedIndex.value = props.modelValue.profiles.length
  replaceProfiles([...props.modelValue.profiles, profile])
}

function removeProfile(index: number) {
  const profiles = props.modelValue.profiles.filter((_, position) => position !== index)
  if (index < selectedIndex.value) selectedIndex.value--
  selectedIndex.value = Math.max(0, Math.min(selectedIndex.value, profiles.length - 1))
  replaceProfiles(profiles)
}

function moveProfile(index: number, direction: number) {
  const next = index + direction
  if (next < 0 || next >= props.modelValue.profiles.length) return
  const profiles = [...props.modelValue.profiles]
  ;[profiles[index], profiles[next]] = [profiles[next]!, profiles[index]!]
  if (selectedIndex.value === index) selectedIndex.value = next
  else if (selectedIndex.value === next) selectedIndex.value = index
  replaceProfiles(profiles)
}

function validate(): string | null {
  const profiles = props.modelValue.profiles
  if (profiles.length > 64) return localText('CCS 最多支持 64 个导入配置。', 'CCS supports at most 64 import profiles.')
  const ids = new Set<string>()
  for (const [index, profile] of profiles.entries()) {
    const prefix = `${localText('CCS 配置', 'CCS profile')} ${index + 1}: `
    if (!profile.id.trim() || !profile.label.trim()) return prefix + localText('配置标识和菜单名称不能为空。', 'Profile ID and menu label are required.')
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(profile.id)) return prefix + localText('配置标识只能包含 1–64 个字母、数字、下划线或连字符。', 'Profile ID must contain 1–64 letters, digits, underscores, or hyphens.')
    if (new TextEncoder().encode(profile.label).length > 128) return prefix + localText('菜单名称不能超过 128 字节。', 'Menu label must not exceed 128 bytes.')
    if (ids.has(profile.id)) return prefix + localText('配置标识重复。', 'Profile IDs must be unique.')
    ids.add(profile.id)
    if (!platforms.includes(profile.platform) || !apps.includes(profile.app)) return prefix + localText('平台或客户端类型无效。', 'Invalid platform or application.')
    if (profile.platform === 'gemini' && profile.app === 'hermes' && !profile.parameters.apiFormat) return prefix + localText('Hermes 不支持 Gemini 原生协议，请明确选择兼容的 API 格式及地址。', 'Hermes does not support native Gemini. Select an explicit compatible API format and endpoint.')
    if (profile.app === 'pi' && (typeof profile.parameters.model !== 'string' || !profile.parameters.model.trim())) return prefix + localText('Pi 导入必须填写默认模型。', 'Pi imports require a default model.')
    for (const [key, value] of Object.entries(profile.parameters)) {
      if (!supportedParameters.has(key)) return prefix + localText('不支持的参数：', 'Unsupported parameter: ') + key
      if (booleanParameters.has(key)) {
        if (typeof value !== 'boolean') return prefix + key + localText(' 必须为布尔值。', ' must be a boolean.')
      } else if (key === 'usageAutoInterval') {
        if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) return prefix + localText('查询间隔必须为非负安全整数。', 'Query interval must be a non-negative safe integer.')
      } else if (typeof value !== 'string') {
        return prefix + key + localText(' 必须为文本。', ' must be text.')
      }
      if (key === 'apiFormat' && typeof value === 'string' && value !== '' && !formatsForApp(profile.app).includes(value)) return prefix + localText('该客户端不支持所选 API 协议格式。', 'This application does not support the selected API format.')
      if (key === 'configFormat' && value !== '' && value !== 'json' && value !== 'toml') return prefix + localText('配置格式必须为 json 或 toml。', 'Configuration format must be json or toml.')
      if ((key === 'config' || key === 'usageScript') && typeof value === 'string' && new TextEncoder().encode(value).length > 128 * 1024) {
        return prefix + key + localText(' 不能超过 128 KiB。', ' must not exceed 128 KiB.')
      }
      if (key !== 'config' && key !== 'usageScript' && typeof value === 'string' && new TextEncoder().encode(value).length > 16 * 1024) return prefix + key + localText(' 不能超过 16 KiB。', ' must not exceed 16 KiB.')
    }
    if (typeof profile.parameters.config === 'string' && profile.parameters.config.trim() && profile.parameters.configFormat !== 'toml') {
      try {
        const config: unknown = JSON.parse(profile.parameters.config)
        if (!config || typeof config !== 'object' || Array.isArray(config)) throw new Error('not an object')
      } catch {
        return prefix + localText('JSON 配置必须是有效的 JSON 对象；TOML 配置请先选择 toml 格式。', 'JSON configuration must be a valid JSON object. Select toml for TOML configuration.')
      }
    }
  }
  if (new TextEncoder().encode(JSON.stringify(props.modelValue)).length > 512 * 1024) return localText('CCS 导入配置总大小不能超过 512 KiB。', 'Total CCS import settings must not exceed 512 KiB.')
  return null
}

const validationError = computed(validate)
defineExpose({ validate })
</script>
