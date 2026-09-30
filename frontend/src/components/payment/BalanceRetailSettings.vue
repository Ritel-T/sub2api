<template>
  <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" data-testid="balance-retail-settings">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('squarespaceProvider.retail.title') }}</h4>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('squarespaceProvider.retail.description') }}</p>
      </div>
      <ToggleSwitch :label="t('common.enabled')" :checked="modelValue.balance_retail_pricing_enabled"
        @toggle="patch({ balance_retail_pricing_enabled: !modelValue.balance_retail_pricing_enabled })" />
    </div>
    <div v-if="modelValue.balance_retail_pricing_enabled" class="mt-4 space-y-3">
      <p class="rounded-lg theme-info-panel border p-3 text-xs leading-relaxed">{{ t('squarespaceProvider.retail.uniformPriceHint') }}</p>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label for="retail-cost-rate" class="input-label">{{ t('squarespaceProvider.retail.costRate') }}</label>
          <input id="retail-cost-rate" :value="modelValue.balance_retail_cost_rate" type="number" min="0" max="99.99" step="0.01" class="input"
            @input="patchNumber('balance_retail_cost_rate', $event)" />
        </div>
        <div>
          <label for="retail-fixed-cost" class="input-label">{{ t('squarespaceProvider.retail.fixedCost') }}</label>
          <input id="retail-fixed-cost" :value="modelValue.balance_retail_fixed_cost_gbp" type="number" min="0" max="100" step="0.01" class="input"
            @input="patchNumber('balance_retail_fixed_cost_gbp', $event)" />
        </div>
        <div>
          <label for="retail-quote-ttl" class="input-label">{{ t('squarespaceProvider.retail.quoteTTL') }}</label>
          <input id="retail-quote-ttl" :value="modelValue.balance_retail_quote_ttl_seconds" type="number" min="60" max="3600" step="1" class="input"
            @input="patchNumber('balance_retail_quote_ttl_seconds', $event)" />
        </div>
        <div>
          <label for="retail-fx-age" class="input-label">{{ t('squarespaceProvider.retail.maxFXAge') }}</label>
          <input id="retail-fx-age" :value="modelValue.balance_retail_fx_max_age_hours" type="number" min="24" max="240" step="1" class="input"
            @input="patchNumber('balance_retail_fx_max_age_hours', $event)" />
        </div>
        <div class="sm:col-span-2">
          <label for="retail-fx-source" class="input-label">{{ t('squarespaceProvider.retail.fxSource') }}</label>
          <Select id="retail-fx-source" :model-value="modelValue.balance_retail_fx_source" :options="fxSourceOptions"
            @update:model-value="patch({ balance_retail_fx_source: String($event) })" />
        </div>
      </div>
      <div v-if="modelValue.balance_retail_fx_source === 'manual'" class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label for="retail-fx-usd" class="input-label">{{ t('squarespaceProvider.retail.usdPerGBP') }}</label>
          <input id="retail-fx-usd" :value="modelValue.balance_retail_fx_usd_per_gbp || ''" type="number" min="0.000001" step="any" class="input"
            @input="patchNumber('balance_retail_fx_usd_per_gbp', $event)" />
        </div>
        <div>
          <label for="retail-fx-cny" class="input-label">{{ t('squarespaceProvider.retail.cnyPerGBP') }}</label>
          <input id="retail-fx-cny" :value="modelValue.balance_retail_fx_cny_per_gbp || ''" type="number" min="0.000001" step="any" class="input"
            @input="patchNumber('balance_retail_fx_cny_per_gbp', $event)" />
        </div>
        <div class="sm:col-span-2">
          <label for="retail-fx-asof" class="input-label">{{ t('squarespaceProvider.retail.fxAsOf') }}</label>
          <input id="retail-fx-asof" :value="modelValue.balance_retail_fx_asof" type="text" class="input" placeholder="2026-09-30T00:00:00Z"
            @input="patch({ balance_retail_fx_asof: ($event.target as HTMLInputElement).value })" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('squarespaceProvider.retail.manualFXHint') }}</p>
        </div>
      </div>
      <p v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t('squarespaceProvider.retail.ecbFXHint') }}</p>
      <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('squarespaceProvider.retail.quoteFreshnessHint') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import ToggleSwitch from './ToggleSwitch.vue'
import type { BalanceRetailSettingsValue } from './balanceRetailSettings'

const props = defineProps<{ modelValue: BalanceRetailSettingsValue }>()
const emit = defineEmits<{ 'update:modelValue': [value: BalanceRetailSettingsValue] }>()
const { t } = useI18n()
const fxSourceOptions = computed(() => [
  { value: 'manual', label: t('squarespaceProvider.retail.fxManual') },
  { value: 'ecb', label: t('squarespaceProvider.retail.fxECB') },
])

function patch(value: Partial<BalanceRetailSettingsValue>) {
  emit('update:modelValue', { ...props.modelValue, ...value })
}
function patchNumber(key: keyof BalanceRetailSettingsValue, event: Event) {
  const raw = (event.target as HTMLInputElement).value
  patch({ [key]: raw.trim() ? Number(raw) : 0 })
}
</script>
