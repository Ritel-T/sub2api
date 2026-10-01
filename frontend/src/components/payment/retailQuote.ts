import type { RetailQuote } from '@/types/payment'

export function isRetailQuote(value: unknown): value is RetailQuote {
  if (!value || typeof value !== 'object') return false
  const quote = value as Partial<RetailQuote>
  const amounts = [quote.credited_amount_usd, quote.base_amount_gbp, quote.included_cost_gbp, quote.total_amount_gbp, quote.pay_amount]
  const validBasis = quote.pricing_basis_currency === undefined
    ? quote.base_amount_cny === undefined
    : quote.pricing_basis_currency === 'CNY' && typeof quote.base_amount_cny === 'number'
      && Number.isFinite(quote.base_amount_cny) && quote.base_amount_cny > 0
      && quote.base_amount_cny === quote.credited_amount_usd
  return validBasis && amounts.every(amount => typeof amount === 'number' && Number.isFinite(amount) && amount >= 0)
    && (quote.credited_amount_usd ?? 0) > 0
    && (quote.total_amount_gbp ?? 0) > 0
    && (quote.pay_amount ?? 0) > 0
    && ['GBP', 'USD', 'CNY'].includes(quote.currency || '')
    && !!quote.fx && quote.fx.GBP === 1
    && Number.isFinite(quote.fx.USD) && quote.fx.USD > 0
    && Number.isFinite(quote.fx.CNY) && quote.fx.CNY > 0
    && typeof quote.checkout_reference === 'string' && quote.checkout_reference.trim() !== ''
    && Number.isFinite(Date.parse(quote.expires_at || ''))
}

/** Fresh purchase quotes must use the current CNY principal policy; old snapshots remain displayable. */
export function isCurrentRetailQuote(value: unknown): value is RetailQuote {
  return isRetailQuote(value) && value.pricing_basis_currency === 'CNY'
}
