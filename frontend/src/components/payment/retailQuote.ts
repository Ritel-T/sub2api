import type { RetailQuote } from '@/types/payment'

export function isRetailQuote(value: unknown): value is RetailQuote {
  if (!value || typeof value !== 'object') return false
  const quote = value as Partial<RetailQuote>
  if (quote.pricing_mode === 'gelv_channel_cny') {
    return quote.currency === 'CNY' && quote.pricing_basis_currency === 'CNY'
      && typeof quote.credited_amount_usd === 'number' && Number.isFinite(quote.credited_amount_usd)
      && quote.credited_amount_usd > 0 && quote.base_amount_cny === quote.credited_amount_usd
      && [10, 30, 50, 100, 200, 500].includes(quote.pay_amount ?? 0)
      && quote.credited_amount_usd <= (quote.pay_amount ?? 0)
      && quote.credit_by_channel?.apple_iap === quote.credited_amount_usd
      && typeof quote.credit_by_channel?.wechat_virtual === 'number'
      && Math.abs(quote.credit_by_channel.wechat_virtual - (quote.pay_amount ?? 0) * 0.99) < 0.000001
      && Math.abs(quote.credit_by_channel.apple_iap - (quote.pay_amount ?? 0) * 0.88) < 0.000001
      && quote.base_amount_gbp === 0 && quote.total_amount_gbp === 0 && quote.included_cost_gbp === 0
      && typeof quote.checkout_reference === 'string' && quote.checkout_reference.trim() !== ''
      && Number.isFinite(Date.parse(quote.expires_at || ''))
  }
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
