<template>
  <section class="space-y-4 border-t border-gray-100 pt-5 dark:border-dark-700" aria-labelledby="ccs-import-settings-title">
    <div>
      <AppHeading as="h3" variant="subsection" id="ccs-import-settings-title" class="text-gray-900 dark:text-white">
        {{ t('consoleInline.cCSwitchImportProfiles_e493be') }}
      </AppHeading>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('consoleInline.importOptionsAreShownForMatchingAPI_0fee9d') }}
      </p>
    </div>
    <div class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
      {{ t('consoleInline.theseTemplatesArePublishedToUsersDo_8e5e78') }}
      {{ t('consoleInline.metadataTemplatesSupport_b4555e') }}
      <code v-for="token in templateTokens" :key="token" class="mr-2 inline-block">{{ token }}</code>
      <p class="mt-1">{{ t('consoleInline.apiKeyIsTheSelectedUserAPIKey_33a5c9') }}</p>
    </div>

    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ t('consoleInline.fieldsWithoutOverridesShowTheirCurrentDefaults_8c390a') }}
    </p>

    <div class="flex flex-wrap items-center gap-2">
      <button type="button" class="btn btn-secondary btn-sm" data-testid="ccs-add-profile" :disabled="modelValue.profiles.length >= 64" @click="addProfile">
        {{ t('consoleInline.addProfile_e7a2c7') }}
      </button>
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ modelValue.profiles.length }} / 64</span>
    </div>

    <p v-if="!modelValue.profiles.length" class="rounded-lg border border-dashed border-gray-300 p-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
      {{ t('consoleInline.noImportOptionsAreConfigured_743f59') }}
    </p>
    <div v-else class="grid gap-4 xl:grid-cols-[15rem_minmax(0,1fr)]">
      <div class="space-y-2">
        <div v-for="(profile, index) in modelValue.profiles" :key="index" class="open-section flex items-center gap-1" :class="index === selectedIndex ? 'border-primary-400 bg-primary-50 dark:bg-primary-900/10' : 'border-gray-200 dark:border-dark-600'">
          <button type="button" class="min-w-0 flex-1 text-left" :data-testid="`ccs-select-${index}`" :aria-pressed="index === selectedIndex" @click="selectedIndex = index">
            <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">{{ profile.label || t('consoleInline.unnamedProfile_5ba2ab') }}</span>
            <span class="block text-xs text-gray-500 dark:text-gray-400">{{ profile.platform }} / {{ profile.app }}</span>
          </button>
          <div class="flex flex-col">
            <button type="button" class="px-1 text-gray-500 disabled:opacity-30" :disabled="index === 0" :aria-label="t('consoleInline.moveUp_4a0439')" :data-testid="`ccs-up-${index}`" @click="moveProfile(index, -1)">↑</button>
            <button type="button" class="px-1 text-gray-500 disabled:opacity-30" :disabled="index === modelValue.profiles.length - 1" :aria-label="t('consoleInline.moveDown_388ab3')" :data-testid="`ccs-down-${index}`" @click="moveProfile(index, 1)">↓</button>
          </div>
          <button type="button" class="px-1 text-red-500" :aria-label="t('consoleInline.removeProfile_91e63a')" :data-testid="`ccs-remove-${index}`" @click="removeProfile(index)">×</button>
        </div>
      </div>
      <div v-if="selectedProfile" class="open-section min-w-0 space-y-5">
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ t('consoleInline.menuLabel_2c711e') }}</span>
            <input class="input" data-testid="ccs-label" :value="selectedProfile.label" @input="updateProfile({ label: inputValue($event) })" />
          </label>
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ t('consoleInline.profileIDUnique_195a00') }}</span>
            <input class="input font-mono" data-testid="ccs-id" :value="selectedProfile.id" @input="updateProfile({ id: inputValue($event) })" />
          </label>
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ t('consoleInline.aPIKeyPlatform_eb8cb9') }}</span>
            <select class="input" data-testid="ccs-platform" :value="selectedProfile.platform" @change="updateProfile({ platform: inputValue($event) as CcsImportPlatform })">
              <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
            </select>
          </label>
          <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
            <span>{{ t('consoleInline.cCSApplication_02eb70') }}</span>
            <select class="input" data-testid="ccs-app" :value="selectedProfile.app" @change="updateProfile({ app: inputValue($event) as CcsImportApp })">
              <option v-for="app in apps" :key="app" :value="app">{{ app }}</option>
            </select>
          </label>
        </div>

        <details v-for="group in fieldGroups" :key="group.id" :open="group.id === 'provider'" class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <summary class="cursor-pointer text-sm font-medium text-gray-900 dark:text-white">{{ t(group.labelKey) }}</summary>
          <div class="grid gap-4 pt-3 sm:grid-cols-2">
            <label v-for="field in group.fields" :key="field.key" class="space-y-1 text-sm text-gray-700 dark:text-gray-300" :class="field.multiline ? 'sm:col-span-2' : ''">
              <span>{{ t(field.labelKey) }} <code class="text-xs text-gray-400">{{ field.key }}</code></span>
              <textarea v-if="field.multiline" class="input font-mono text-xs" :rows="field.key === 'config' || field.key === 'usageScript' ? 8 : 3" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" :placeholder="field.placeholder" spellcheck="false" @input="setParameter(field.key, inputValue($event))" />
              <select v-else-if="field.boolean" class="input" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" @change="setBooleanParameter(field.key, inputValue($event))">
                <option value="">{{ t('consoleInline.omitSiteCCSDefault_1354e9') }}</option>
                <option value="true">{{ t('consoleInline.yes_a16401') }}</option>
                <option value="false">{{ t('consoleInline.no_6089d1') }}</option>
              </select>
              <select v-else-if="field.key === 'apiFormat' || field.key === 'configFormat'" class="input" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" @change="setParameter(field.key, inputValue($event))">
                <option value="">{{ t('consoleInline.omitUseDefault_ced412') }}</option>
                <option v-for="format in field.key === 'apiFormat' ? apiFormats : ['json', 'toml']" :key="format" :value="format">{{ format }}</option>
              </select>
              <input v-else class="input" :class="field.key === 'model' ? 'font-mono' : ''" :type="field.number ? 'number' : 'text'" :min="field.number ? 0 : undefined" :step="field.number ? 1 : undefined" :data-testid="`ccs-param-${field.key}`" :value="parameterText(field.key)" :placeholder="field.placeholder" @input="setParameter(field.key, inputValue($event), field.number)" />
              <p v-if="field.hintKey" class="text-xs text-gray-500 dark:text-gray-400">{{ t(field.hintKey!) }}</p>
            </label>
          </div>
        </details>
      </div>
    </div>
    <p v-if="validationError" role="alert" class="text-sm text-red-600 dark:text-red-400" data-testid="ccs-error">{{ validationError }}</p>
  </section>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CcsImportApp, CcsImportConfig, CcsImportParameterValue, CcsImportPlatform, CcsImportProfile } from '@/types/ccswitch'
import { createDefaultCcSwitchImportConfig, getCcSwitchImportParameterDefaults } from '@/utils/ccswitchImport'

const props = defineProps<{ modelValue: CcsImportConfig; baseUrl?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: CcsImportConfig] }>()
const { t } = useI18n()
const selectedIndex = ref(0)
const selectedProfile = computed(() => props.modelValue.profiles[selectedIndex.value])
const parameterDefaults = computed(() => selectedProfile.value
  ? getCcSwitchImportParameterDefaults(selectedProfile.value, props.baseUrl || '{{baseUrl}}') : {})
const templateTokens = ['{{apiKey}}', '{{baseUrl}}', '{{siteName}}', '{{keyName}}', '{{userId}}']
const platforms: CcsImportPlatform[] = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'deepseek', 'minimax', 'kimi', 'zhipu', 'opencode_go', 'composite']
const apps: CcsImportApp[] = ['claude', 'codex', 'gemini', 'agy', 'grokbuild', 'opencode', 'openclaw', 'hermes', 'pi', 'claude-desktop', 'mcode', 'kimi', 'dsh', 'omp']
const apiFormats = computed(() => formatsForApp(selectedProfile.value?.app))

function formatsForApp(app?: CcsImportApp): string[] {
  if (app === 'gemini' || app === 'agy') return ['gemini_native']
  const formats = ['anthropic', 'openai_chat', 'openai_responses']
  return app && ['claude', 'claude-desktop', 'opencode', 'openclaw', 'pi', 'kimi', 'omp'].includes(app) ? [...formats, 'gemini_native'] : formats
}

interface ParameterField {
  key: string
  labelKey: string
  multiline?: boolean
  boolean?: boolean
  number?: boolean
  placeholder?: string
  hintKey?: string
}
const fieldGroups: Array<{ id: string; labelKey: string; fields: ParameterField[] }> = [
  {
    id: 'provider', labelKey: 'consoleInline.providerAndModel_0cf867', fields: [
      { key: 'name', labelKey: 'consoleInline.providerName_6c5717', placeholder: '{{siteName}}' },
      { key: 'model', labelKey: 'consoleInline.defaultModel_aa5219', placeholder: 'gpt-6-astra' },
      { key: 'endpoint', labelKey: 'consoleInline.aPIEndpointMultipleEndpoints_a13df6', placeholder: '{{baseUrl}}', hintKey: 'consoleInline.separateMultipleEndpointsWithCommasIncludeThe_c1e40b' },
      { key: 'homepage', labelKey: 'consoleInline.providerHomepage_df4038', placeholder: '{{baseUrl}}' },
      { key: 'apiFormat', labelKey: 'consoleInline.aPIFormat_564ff6', hintKey: 'consoleInline.leaveBlankForTheSiteApplicationDefault_a5c940' },
      { key: 'enabled', labelKey: 'consoleInline.enableSwitchAfterImport_e864bc', boolean: true, hintKey: 'consoleInline.showsTheDefaultInTheCCSImport_ca426e' },
      { key: 'haikuModel', labelKey: 'consoleInline.claudeHaikuModelAlias_dd24c5' },
      { key: 'sonnetModel', labelKey: 'consoleInline.claudeSonnetModelAlias_e1d50e' },
      { key: 'opusModel', labelKey: 'consoleInline.claudeOpusModelAlias_417372' },
    ],
  },
  {
    id: 'display', labelKey: 'consoleInline.notesAndIcon_ccd74e', fields: [
      { key: 'notes', labelKey: 'consoleInline.notes_e409e6', multiline: true },
      { key: 'icon', labelKey: 'consoleInline.iconName_4412aa' },
      { key: 'iconUrl', labelKey: 'consoleInline.iconURL_e6b9fa' },
    ],
  },
  {
    id: 'usage', labelKey: 'consoleInline.usageQuery_7ca6ab', fields: [
      { key: 'usageEnabled', labelKey: 'consoleInline.enableUsageQuery_958eb3', boolean: true, hintKey: 'consoleInline.siteDefaultWhenOmittedEnabled_637767' },
      { key: 'usageAutoInterval', labelKey: 'consoleInline.queryIntervalMinutes_410164', number: true, placeholder: '30', hintKey: 'consoleInline.siteDefault30Minutes0DisablesAutomatic_99e2f7' },
      { key: 'usageApiKey', labelKey: 'consoleInline.queryKeyTemplatePublic_074cfc', placeholder: '{{apiKey}}', hintKey: 'consoleInline.inheritsTheCurrentProviderKeyWhenNot_cdd3f8' },
      { key: 'usageBaseUrl', labelKey: 'consoleInline.queryBaseURLTemplate_e55f8f', placeholder: '{{baseUrl}}', hintKey: 'consoleInline.followsTheCurrentProviderEndpointWhenNot_40f929' },
      { key: 'usageAccessToken', labelKey: 'consoleInline.queryTokenTemplatePublic_3e0c5b' },
      { key: 'usageUserId', labelKey: 'consoleInline.queryUserIDTemplate_5556a8', placeholder: '{{userId}}' },
      { key: 'usageScript', labelKey: 'consoleInline.rawUsageQueryScript_085c55', multiline: true, hintKey: 'consoleInline.leaveBlankForTheBuiltInSite_003eed' },
    ],
  },
  {
    id: 'config', labelKey: 'consoleInline.rawClientConfiguration_f2e339', fields: [
      { key: 'configFormat', labelKey: 'consoleInline.configurationFormat_e144c4', placeholder: 'json / toml', hintKey: 'consoleInline.theSiteDefaultIsJSONSelectToml_13ab7a' },
      { key: 'config', labelKey: 'consoleInline.rawConfiguration_ff42e9', multiline: true, hintKey: 'consoleInline.enterARawJSONObjectTOMLEncoded_c9d555' },
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
  // These clients require an explicit model in the import link.
  if (key === 'model' && selectedProfile.value && ['pi', 'mcode', 'kimi', 'dsh', 'omp'].includes(selectedProfile.value.app)) return String(selectedProfile.value.parameters.model || '')
  const value = selectedProfile.value?.parameters[key] ?? parameterDefaults.value[key]
  // These empty strings are accepted by the API and inherit the current default.
  if (value === '' && ['model', 'name', 'homepage', 'endpoint', 'apiFormat', 'configFormat', 'usageApiKey', 'usageBaseUrl'].includes(key)) return String(parameterDefaults.value[key] ?? '')
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
  if (profiles.length > 64) return t('consoleInline.cCSSupportsAtMost64ImportProfiles_3c8887')
  const ids = new Set<string>()
  for (const [index, profile] of profiles.entries()) {
    const prefix = `${t('consoleInline.cCSProfile_20d41e')} ${index + 1}: `
    if (!profile.id.trim() || !profile.label.trim()) return prefix + t('consoleInline.profileIDAndMenuLabelAreRequired_974fd3')
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(profile.id)) return prefix + t('consoleInline.profileIDMustContain164Letters_74828e')
    if (new TextEncoder().encode(profile.label).length > 128) return prefix + t('consoleInline.menuLabelMustNotExceed128Bytes_31902d')
    if (ids.has(profile.id)) return prefix + t('consoleInline.profileIDsMustBeUnique_89afca')
    ids.add(profile.id)
    if (!platforms.includes(profile.platform) || !apps.includes(profile.app)) return prefix + t('consoleInline.invalidPlatformOrApplication_245428')
    if (profile.platform === 'gemini' && profile.app === 'hermes' && !profile.parameters.apiFormat) return prefix + t('consoleInline.hermesDoesNotSupportNativeGeminiSelect_832744')
    if (['pi', 'mcode', 'kimi', 'dsh', 'omp'].includes(profile.app) && (typeof profile.parameters.model !== 'string' || !profile.parameters.model.trim())) return prefix + t('consoleInline.piImportsRequireADefaultModel_5a3ae3').replace('Pi', profile.app === 'pi' ? 'Pi' : profile.app)
    for (const [key, value] of Object.entries(profile.parameters)) {
      if (!supportedParameters.has(key)) return prefix + t('consoleInline.unsupportedParameter_99b8ea') + key
      if (booleanParameters.has(key)) {
        if (typeof value !== 'boolean') return prefix + key + t('consoleInline.mustBeABoolean_06a6d4')
      } else if (key === 'usageAutoInterval') {
        if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) return prefix + t('consoleInline.queryIntervalMustBeANonNegative_0a4bfa')
      } else if (typeof value !== 'string') {
        return prefix + key + t('consoleInline.mustBeText_56d4ea')
      }
      if (key === 'apiFormat' && typeof value === 'string' && value !== '' && !formatsForApp(profile.app).includes(value)) return prefix + t('consoleInline.thisApplicationDoesNotSupportTheSelected_a2bed6')
      if (key === 'configFormat' && value !== '' && value !== 'json' && value !== 'toml') return prefix + t('consoleInline.configurationFormatMustBeJsonOrToml_7269f1')
      if ((key === 'config' || key === 'usageScript') && typeof value === 'string' && new TextEncoder().encode(value).length > 128 * 1024) {
        return prefix + key + t('consoleInline.mustNotExceed128KiB_3da5cc')
      }
      if (key !== 'config' && key !== 'usageScript' && typeof value === 'string' && new TextEncoder().encode(value).length > 16 * 1024) return prefix + key + t('consoleInline.mustNotExceed16KiB_c156f1')
    }
    if (typeof profile.parameters.config === 'string' && profile.parameters.config.trim() && profile.parameters.configFormat !== 'toml') {
      try {
        const config: unknown = JSON.parse(profile.parameters.config)
        if (!config || typeof config !== 'object' || Array.isArray(config)) throw new Error('not an object')
      } catch {
        return prefix + t('consoleInline.jSONConfigurationMustBeAValidJSON_971271')
      }
    }
  }
  if (new TextEncoder().encode(JSON.stringify(props.modelValue)).length > 512 * 1024) return t('consoleInline.totalCCSImportSettingsMustNotExceed_c15c45')
  return null
}

const validationError = computed(validate)
defineExpose({ validate })
</script>
