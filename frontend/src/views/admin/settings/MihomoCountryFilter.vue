<template>
  <section class="space-y-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800" aria-labelledby="country-filter-title">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <AppHeading variant="minor" as="h4" id="country-filter-title">{{ t('consoleInline.countryRegionFilter_43432c') }}</AppHeading>
      <span class="text-xs text-gray-500">{{ t('consoleInline.eligible_6b481d') }} {{ eligible }} / {{ nodes.length }}</span>
    </div>
    <p :id="'field-' + fieldLabelScope + '-1'" class="text-xs text-gray-500">{{ t('consoleInline.subscriptionNodesUseTheLastExitIP_64244e') }}</p>
    <div class="flex flex-wrap items-center gap-2">
      <label class="sr-only" for="country-filter-mode">{{ t('consoleInline.filterMode_b7e673') }}</label>
      <select :aria-labelledby="'field-' + fieldLabelScope + '-1'" id="country-filter-mode" v-model="draft.mode" class="input w-auto" :disabled="busy" @change="dirty = true">
        <option value="off">{{ t('consoleInline.noRegionFilter_729515') }}</option>
        <option value="exclude">{{ t('consoleInline.excludeSelectedRegions_5de4e3') }}</option>
        <option value="include">{{ t('consoleInline.allowSelectedRegionsOnly_ed55fa') }}</option>
      </select>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="excludeHongKong">{{ t('consoleInline.excludeHongKong_7881c5') }}</button>
    </div>
    <template v-if="draft.mode !== 'off'">
      <div class="flex flex-wrap gap-1.5" aria-live="polite">
        <button v-for="code in draft.codes" :key="code" type="button" class="rounded-full bg-primary-100 px-2.5 py-1 text-xs text-primary-800 dark:bg-primary-900 dark:text-primary-200" :disabled="busy" :aria-label="t('consoleInline.remove_6ee924') + label(code)" @click="toggle(code)">{{ label(code) }} ×</button>
        <span v-if="!draft.codes.length" class="text-xs text-amber-700">{{ t('consoleInline.selectAtLeastOneCountryOrRegion_8d9aa6') }}</span>
      </div>
      <input :aria-labelledby="'field-' + fieldLabelScope + '-1'" v-model="search" type="search" class="input w-full" :placeholder="t('consoleInline.searchARegionOrCodeEG_8df8c0')" :aria-label="t('consoleInline.searchCountriesAndRegions_784b89')" />
      <div class="flex max-h-32 flex-wrap gap-1.5 overflow-y-auto" :aria-label="t('consoleInline.regionOptions_9c2775')">
        <button v-for="code in options" :key="code" type="button" class="rounded-md border px-2 py-1 text-xs" :class="draft.codes.includes(code) ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900 dark:text-primary-200' : 'border-gray-200 dark:border-dark-600'" :disabled="busy" :aria-pressed="draft.codes.includes(code)" :data-country="code" @click="toggle(code)">{{ label(code) }}</button>
      </div>
      <label class="flex items-start gap-2 text-xs"><input v-model="draft.allow_unknown" type="checkbox" class="mt-0.5" :disabled="busy" @change="dirty = true" />{{ t('consoleInline.allowUnknownRegionsUncheckedOrFailedChecks_8c9d91') }}</label>
      <label class="flex items-start gap-2 text-xs"><input v-model="draft.dynamic_provider_managed" type="checkbox" data-testid="dynamic-provider-managed" :disabled="busy" @change="dirty = true" />{{ t('consoleInline.providerControlsDynamicProxyRegionsAllowDynamic_44bb44') }}</label>
      <p class="text-xs text-amber-700">{{ t('consoleInline.whenEnabledRestrictRegionsAtYourProvider_1130c2') }}</p>
    </template>
    <p v-if="dynamicBlocked" class="text-xs text-amber-700" role="status">{{ t('consoleInline.dynamicProxiesAreExcludedEnableProviderManaged_95ee06') }}</p>
    <p v-if="eligible === 0 && nodes.length" class="text-xs text-amber-700 dark:text-amber-400" role="status">{{ t('consoleInline.noEligibleHarvestNodesAfterApplyingCheck_cc65b0') }}</p>
    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn btn-primary btn-sm" :disabled="busy || (draft.mode !== 'off' && !draft.codes.length)" @click="save">{{ t('consoleInline.saveRegionRules_391d7a') }}</button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !nodes.length" @click="$emit('scan')">{{ t('consoleInline.checkExitRegions_692ee9') }}</button>
    </div>
    <p class="text-xs text-gray-500">{{ t('consoleInline.checksExitRegionsViaCountryIsThrough_1179b7') }}</p>
  </section>
</template>
<script setup lang="ts">
import { useId as useFieldLabelId } from 'vue'
const fieldLabelScope = useFieldLabelId()

import AppHeading from '@/components/common/AppHeading.vue'
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CountryFilter, CountryNode } from './mihomoCountry'
const props = defineProps<{ filter?: CountryFilter; codes: string[]; nodes: CountryNode[]; busy: boolean }>()
const emit = defineEmits<{ save: [filter: CountryFilter]; scan: [] }>()
const { t, locale } = useI18n()
const draft = reactive<CountryFilter>({ mode: 'off', codes: [], allow_unknown: false })
const dirty = ref(false); const search = ref('')
watch(() => props.filter, value => {
  const next = value || { mode: 'off' as const, codes: [], allow_unknown: false }
  const equal = draft.mode === next.mode && draft.allow_unknown === next.allow_unknown && !!draft.dynamic_provider_managed === !!next.dynamic_provider_managed && [...draft.codes].sort().join(',') === [...(next.codes || [])].sort().join(',')
  if (!dirty.value || equal) { Object.assign(draft, { dynamic_provider_managed: false }, next, { codes: [...(next.codes || [])] }); dirty.value = false }
}, { immediate: true, deep: true })
const names = computed(() => new Intl.DisplayNames([locale.value === 'zh' ? 'zh' : locale.value === 'zh-Hant-HK' ? 'zh-Hant-HK' : 'en'], { type: 'region' }))
const englishNames = new Intl.DisplayNames(['en'], { type: 'region' })
const label = (code: string) => `${code === 'HK' ? t('consoleInline.hongKong_eff281') : names.value.of(code) || code} ${code}`
const common = ['HK', 'CN', 'MO', 'US', 'JP', 'SG', 'TW', 'KR', 'GB', 'DE', 'CA', 'AU']
const options = computed(() => {
  const codes = [...new Set([...common.filter(code => props.codes.includes(code)), ...props.codes])]
  const needle = search.value.trim().toLowerCase()
  return codes.filter(code => !needle || `${label(code)} ${englishNames.of(code) || ''}`.toLowerCase().includes(needle))
})
function toggle(code: string) { dirty.value = true; draft.codes = draft.codes.includes(code) ? draft.codes.filter(c => c !== code) : [...draft.codes, code] }
function excludeHongKong() { dirty.value = true; draft.codes = draft.mode === 'exclude' ? [...new Set([...draft.codes, 'HK'])] : ['HK']; draft.mode = 'exclude' }
function save() { emit('save', { ...draft, codes: [...draft.codes] }) }
const eligible = computed(() => props.nodes.filter(node => {
  if (node.state !== 'enabled' && node.state !== 'country_excluded') return false
  if (draft.mode === 'off') return true
  if (node.dynamic) return draft.dynamic_provider_managed || draft.allow_unknown
  if (!node.country_code) return draft.allow_unknown
  const selected = draft.codes.includes(node.country_code)
  return draft.mode === 'include' ? selected : !selected
}).length)
const dynamicBlocked = computed(() => props.nodes.some(n => n.dynamic) && draft.mode !== 'off' && !draft.dynamic_provider_managed && !draft.allow_unknown)
</script>
