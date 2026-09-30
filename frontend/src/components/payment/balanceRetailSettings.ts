export interface BalanceRetailSettingsValue {
  balance_retail_pricing_enabled: boolean
  balance_retail_cost_rate: number
  balance_retail_fixed_cost_gbp: number
  balance_retail_quote_ttl_seconds: number
  balance_retail_fx_source: string
  balance_retail_fx_usd_per_gbp: number
  balance_retail_fx_cny_per_gbp: number
  balance_retail_fx_asof: string
  balance_retail_fx_max_age_hours: number
}

export const DEFAULT_BALANCE_RETAIL_SETTINGS: BalanceRetailSettingsValue = {
  balance_retail_pricing_enabled: false,
  balance_retail_cost_rate: 2,
  balance_retail_fixed_cost_gbp: 0.25,
  balance_retail_quote_ttl_seconds: 900,
  balance_retail_fx_source: 'manual',
  balance_retail_fx_usd_per_gbp: 0,
  balance_retail_fx_cny_per_gbp: 0,
  balance_retail_fx_asof: '',
  balance_retail_fx_max_age_hours: 120,
}

/** Validate only the active mode; the backend independently validates persisted settings. */
export function validateBalanceRetailSettings(value: BalanceRetailSettingsValue): string | null {
  if (!value.balance_retail_pricing_enabled) return null
  const rate = value.balance_retail_cost_rate
  if (!Number.isFinite(rate) || rate < 0 || rate >= 100) {
    return 'squarespaceProvider.retail.validationCostRate'
  }
  const fixed = value.balance_retail_fixed_cost_gbp
  if (!Number.isFinite(fixed) || fixed < 0 || fixed > 100 || Math.abs(fixed * 100 - Math.round(fixed * 100)) > 1e-8) {
    return 'squarespaceProvider.retail.validationFixedCost'
  }
  const ttl = value.balance_retail_quote_ttl_seconds
  if (!Number.isInteger(ttl) || ttl < 60 || ttl > 3600) {
    return 'squarespaceProvider.retail.validationQuoteTTL'
  }
  const maxAge = value.balance_retail_fx_max_age_hours
  if (!Number.isInteger(maxAge) || maxAge < 24 || maxAge > 240) {
    return 'squarespaceProvider.retail.validationFXAge'
  }
  if (!['manual', 'ecb'].includes(value.balance_retail_fx_source)) {
    return 'squarespaceProvider.retail.validationFXSource'
  }
  if (value.balance_retail_fx_source === 'manual') {
    if (!Number.isFinite(value.balance_retail_fx_usd_per_gbp) || value.balance_retail_fx_usd_per_gbp <= 0 ||
        !Number.isFinite(value.balance_retail_fx_cny_per_gbp) || value.balance_retail_fx_cny_per_gbp <= 0) {
      return 'squarespaceProvider.retail.validationFXRates'
    }
    const asof = value.balance_retail_fx_asof.trim()
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(asof) || !Number.isFinite(Date.parse(asof))) {
      return 'squarespaceProvider.retail.validationFXAsOf'
    }
  }
  return null
}
