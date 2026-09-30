import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import BalanceRetailSettings from '../BalanceRetailSettings.vue'
import {
  DEFAULT_BALANCE_RETAIL_SETTINGS,
  validateBalanceRetailSettings,
  type BalanceRetailSettingsValue,
} from '../balanceRetailSettings'
import en from '@/i18n/locales/en/squarespaceProvider'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      if (key === 'common.enabled') return 'Enabled'
      return key.replace(/^squarespaceProvider\./, '').split('.')
        .reduce<unknown>((value, segment) => (value as Record<string, unknown>)?.[segment], en) ?? key
    },
  }),
}))

const valid: BalanceRetailSettingsValue = {
  ...DEFAULT_BALANCE_RETAIL_SETTINGS,
  balance_retail_pricing_enabled: true,
  balance_retail_fx_usd_per_gbp: 1.34,
  balance_retail_fx_cny_per_gbp: 9.6,
  balance_retail_fx_asof: '2026-09-30T00:00:00Z',
}

describe('balance retail settings validation', () => {
  it('starts disabled without changing legacy pricing', () => {
    expect(DEFAULT_BALANCE_RETAIL_SETTINGS.balance_retail_pricing_enabled).toBe(false)
    expect(validateBalanceRetailSettings(DEFAULT_BALANCE_RETAIL_SETTINGS)).toBeNull()
    expect(validateBalanceRetailSettings(valid)).toBeNull()
  })

  it('uses ECB without manual rates or timestamp', () => {
    expect(validateBalanceRetailSettings({
      ...DEFAULT_BALANCE_RETAIL_SETTINGS,
      balance_retail_pricing_enabled: true,
      balance_retail_fx_source: 'ecb',
    })).toBeNull()
  })

  it.each([
    ['balance_retail_cost_rate', 100, 'validationCostRate'],
    ['balance_retail_fixed_cost_gbp', 0.251, 'validationFixedCost'],
    ['balance_retail_quote_ttl_seconds', 59, 'validationQuoteTTL'],
    ['balance_retail_quote_ttl_seconds', 3601, 'validationQuoteTTL'],
    ['balance_retail_fx_max_age_hours', 23, 'validationFXAge'],
    ['balance_retail_fx_usd_per_gbp', 0, 'validationFXRates'],
    ['balance_retail_fx_cny_per_gbp', Number.NaN, 'validationFXRates'],
    ['balance_retail_fx_asof', '2026-09-30', 'validationFXAsOf'],
    ['balance_retail_fx_source', 'untrusted', 'validationFXSource'],
  ] as const)('rejects invalid %s', (key, value, error) => {
    expect(validateBalanceRetailSettings({ ...valid, [key]: value })).toBe('squarespaceProvider.retail.' + error)
  })
})

describe('BalanceRetailSettings editing', () => {

  it('keeps USD/GBP and CNY/GBP directions explicit and emits edited numeric values', async () => {
    const wrapper = mount(BalanceRetailSettings, { props: { modelValue: valid } })
    expect(wrapper.text()).toContain('USD per 1 GBP')
    expect(wrapper.text()).toContain('CNY per 1 GBP')
    expect(wrapper.text()).toContain('Every payment method uses the same GBP retail price')
    await wrapper.get('#retail-fixed-cost').setValue('0')
    const value = wrapper.emitted('update:modelValue')?.[0]?.[0] as BalanceRetailSettingsValue
    expect(value.balance_retail_fixed_cost_gbp).toBe(0)
    expect(value.balance_retail_cost_rate).toBe(2)
    expect(value.balance_retail_fx_usd_per_gbp).toBe(1.34)
    wrapper.unmount()
  })

  it('hides manual input fields for ECB while preserving their stored values', () => {
    const wrapper = mount(BalanceRetailSettings, { props: { modelValue: { ...valid, balance_retail_fx_source: 'ecb' } } })
    expect(wrapper.find('#retail-fx-usd').exists()).toBe(false)
    expect(wrapper.find('#retail-fx-cny').exists()).toBe(false)
    expect(wrapper.text()).toContain('latest ECB reference rates')
    wrapper.unmount()
  })
})
