<template>
  <div class="min-w-0 space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="openai-account-defaults">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <AppHeading as="h3" variant="subsection">{{ t('autoConfig.accountDefaults.title') }}</AppHeading>
      <label class="flex items-center gap-2 text-sm"><Toggle :model-value="draft.enabled" data-testid="openai-defaults-enabled" :aria-label="t('autoConfig.accountDefaults.title')" @update:model-value="update({ enabled: $event })" />{{ t('autoConfig.enable') }}</label>
    </header>
    <p class="text-sm leading-6 text-gray-500">{{ t('autoConfig.accountDefaults.hint') }}</p>
    <template v-if="draft.enabled">
      <p class="text-xs leading-5 text-gray-500">{{ t('autoConfig.accountDefaults.scopeHint') }}</p>
      <div v-if="!Object.keys(draft.templates).length" class="rounded-xl border border-dashed border-gray-300 p-4 text-sm text-gray-500 dark:border-dark-600">{{ t('autoConfig.accountDefaults.empty') }}</div>
      <details v-for="(template, plan) in draft.templates" :key="plan" class="rounded-xl border border-gray-200 dark:border-dark-700" :data-testid="'openai-tier-' + plan">
        <summary class="cursor-pointer px-4 py-3 text-sm font-medium">
          {{ tierLabel(String(plan)) }}
          <span class="ml-2 text-xs font-normal text-gray-500">{{ t('autoConfig.accountDefaults.summary', { concurrency: template.concurrency, load: template.load_factor ?? t('autoConfig.accountDefaults.preserve'), priority: template.priority }) }}</span>
        </summary>
        <div class="min-w-0 space-y-4 border-t border-gray-100 p-4 dark:border-dark-700">
          <div class="grid gap-3 sm:grid-cols-3">
            <label><span class="field-label">{{ t('autoConfig.concurrency') }}</span><input v-model.number="template.concurrency" :data-testid="'openai-tier-concurrency-' + plan" type="number" inputmode="numeric" min="1" max="10000" step="1" required class="input w-full" /></label>
            <label><span class="field-label">{{ t('autoConfig.load_factor') }}</span><input :value="template.load_factor ?? ''" :data-testid="'openai-tier-load-' + plan" type="number" inputmode="numeric" min="1" max="10000" step="1" class="input w-full" :placeholder="t('autoConfig.accountDefaults.preserve')" @input="setLoad(template, $event)" /></label>
            <label><span class="field-label">{{ t('autoConfig.priority') }}</span><input v-model.number="template.priority" :data-testid="'openai-tier-priority-' + plan" type="number" inputmode="numeric" min="0" max="10000" step="1" required class="input w-full" /></label>
          </div>
          <p class="text-xs leading-5 text-gray-500">{{ t('autoConfig.accountDefaults.loadHint') }}</p>
          <fieldset class="min-w-0 space-y-3">
            <legend class="field-label">{{ t('autoConfig.accountDefaults.models') }}</legend>
            <p class="text-xs leading-5 text-gray-500">{{ t('autoConfig.accountDefaults.modelsHint') }}</p>
            <div v-for="(rule, index) in template.model_mappings" :key="index" class="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
              <label class="min-w-0"><span class="field-label">{{ t('autoConfig.mapping.from') }}</span><input v-model="rule.from" :data-testid="'openai-tier-model-from-' + plan + '-' + index" class="input w-full" type="text" maxlength="256" required autocomplete="off" :spellcheck="false" /></label>
              <label class="min-w-0"><span class="field-label">{{ t('autoConfig.mapping.to') }}</span><input v-model="rule.to" :data-testid="'openai-tier-model-to-' + plan + '-' + index" class="input w-full" type="text" maxlength="256" required autocomplete="off" :spellcheck="false" /></label>
              <button type="button" class="btn btn-secondary" :data-testid="'openai-tier-model-remove-' + plan + '-' + index" @click="template.model_mappings.splice(index, 1)">{{ t('autoConfig.mapping.remove') }}</button>
            </div>
            <button type="button" class="btn btn-secondary" :data-testid="'openai-tier-model-add-' + plan" :disabled="template.model_mappings.length >= 100" @click="template.model_mappings.push({ from: '', to: '' })">{{ t('autoConfig.mapping.add') }}</button>
          </fieldset>
          <button type="button" class="text-sm text-red-600 hover:underline dark:text-red-400" :data-testid="'openai-tier-remove-' + plan" @click="removeTier(String(plan))">{{ t('autoConfig.accountDefaults.removeTier') }}</button>
        </div>
      </details>
      <div v-if="availablePlans.length" class="flex flex-wrap items-end gap-3">
        <label class="min-w-0 flex-1"><span class="field-label">{{ t('autoConfig.accountDefaults.plan') }}</span><select v-model="newPlan" class="input w-full" data-testid="openai-tier-plan"><option v-for="plan in availablePlans" :key="plan" :value="plan">{{ tierLabel(plan) }}</option></select></label>
        <button type="button" class="btn btn-secondary" data-testid="openai-tier-add" @click="addTier">{{ t('autoConfig.accountDefaults.addTier') }}</button>
      </div>
      <label class="block"><span class="field-label">{{ t('autoConfig.accountDefaults.countries') }}</span><input v-model="countryText" data-testid="openai-proxy-countries" class="input w-full" type="text" placeholder="US, GB, DE" autocomplete="off" autocapitalize="characters" :spellcheck="false" /></label>
      <p class="text-xs leading-5 text-gray-500">{{ t('autoConfig.accountDefaults.countriesHint') }}</p>
      <p v-if="Object.keys(draft.model_exclusions ?? {}).length" class="text-xs leading-5 text-gray-500">{{ t('autoConfig.accountDefaults.exclusionsHint', { count: Object.keys(draft.model_exclusions).length }) }}</p>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppHeading from '@/components/common/AppHeading.vue'
import Toggle from '@/components/common/Toggle.vue'
import { defaultOpenAIAccountDefaults, OPENAI_TEMPLATE_PLANS, type OAuthAccountTemplate, type OpenAIAccountDefaultsConfig } from '@/utils/openaiAccountDefaults'
const model = defineModel<OpenAIAccountDefaultsConfig | undefined>()
const { t } = useI18n()
const empty = defaultOpenAIAccountDefaults()
const draft = computed(() => ({
  ...empty,
  ...model.value,
  templates: model.value?.templates ?? empty.templates,
  model_exclusions: model.value?.model_exclusions ?? empty.model_exclusions ?? {},
  dedicated_proxy_country_codes: model.value?.dedicated_proxy_country_codes ?? []
}))
const availablePlans = computed(() => OPENAI_TEMPLATE_PLANS.filter(plan => !draft.value.templates[plan]))
const newPlan = ref<string>('free')
const countryText = ref('')
function parseCountries(value: string) { return [...new Set(value.split(/[,\s]+/).map(code => code.trim().toUpperCase()).filter(Boolean))] }
function update(patch: Partial<OpenAIAccountDefaultsConfig>) { model.value = { ...draft.value, ...patch } }
watch(availablePlans, plans => { if (!plans.includes(newPlan.value as typeof plans[number])) newPlan.value = plans[0] ?? '' }, { immediate: true })
watch(() => model.value?.dedicated_proxy_country_codes, codes => {
  if (JSON.stringify(codes ?? []) !== JSON.stringify(parseCountries(countryText.value))) countryText.value = (codes ?? []).join(', ')
}, { immediate: true })
watch(countryText, value => update({ dedicated_proxy_country_codes: parseCountries(value) }))
function tierLabel(plan: string) { return OPENAI_TEMPLATE_PLANS.includes(plan as typeof OPENAI_TEMPLATE_PLANS[number]) ? t('autoConfig.accountDefaults.plans.' + plan) : plan }
function setLoad(template: OAuthAccountTemplate, event: Event) {
  const value = (event.target as HTMLInputElement).value
  template.load_factor = value === '' ? null : Number(value)
}
function addTier() {
  if (!availablePlans.value.includes(newPlan.value as typeof availablePlans.value[number])) return
  update({ templates: { ...draft.value.templates, [newPlan.value]: { concurrency: 1, load_factor: null, priority: 50, model_mappings: [] } } })
}
function removeTier(plan: string) { const templates = { ...draft.value.templates }; delete templates[plan]; update({ templates }) }
</script>
<style scoped>
.field-label { @apply mb-2 block text-sm font-medium text-gray-600 dark:text-gray-300; }
</style>
