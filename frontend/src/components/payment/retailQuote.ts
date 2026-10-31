import type { RetailQuote } from '@/types/payment'

export function isRetailQuote(value: unknown): value is RetailQuote {
  if (!value || typeof value !== 'object') return false
  const quote = value as Partial<RetailQuote>
  if (quote.pricing_mode !== undefined && quote.pricing_mode !== 'gelv_channel_cny') return false
  if (quote.pricing_mode === 'gelv_channel_cny') {
    const channels = quote.credit_by_channel
    if (!channels || typeof channels !== 'object' || Array.isArray(channels)) return false
    const keys = Object.keys(channels)
    const alipay = keys.length === 1 && keys[0] === 'alipay_web'
      && channels.alipay_web === quote.credited_amount_usd
    const legacy = keys.length === 2 && keys.includes('wechat_virtual') && keys.includes('apple_iap')
      && channels.apple_iap === quote.credited_amount_usd
      && typeof channels.wechat_virtual === 'number' && typeof channels.apple_iap === 'number'
      && Math.abs(channels.wechat_virtual - (quote.pay_amount ?? 0) * 0.99) < 0.000001
      && Math.abs(channels.apple_iap - (quote.pay_amount ?? 0) * 0.88) < 0.000001
    return (alipay || legacy) && (quote.cash_amount_input === undefined || (alipay && quote.cash_amount_input === true)) && quote.currency === 'CNY' && quote.pricing_basis_currency === 'CNY'
      && typeof quote.credited_amount_usd === 'number' && Number.isFinite(quote.credited_amount_usd)
      && quote.credited_amount_usd > 0 && quote.base_amount_cny === quote.credited_amount_usd
      && (alipay
        ? typeof quote.pay_amount === 'number' && Number.isFinite(quote.pay_amount) && quote.pay_amount > 0
          && Math.abs(quote.pay_amount * 100 - Math.round(quote.pay_amount * 100)) < 0.000001
        : [10, 30, 50, 100, 200, 500].includes(quote.pay_amount ?? 0))
      && quote.credited_amount_usd <= (quote.pay_amount ?? 0)
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

export function isGelvAlipayQuote(value: unknown): value is RetailQuote & { pricing_mode: 'gelv_channel_cny'; credit_by_channel: { alipay_web: number } } {
  return isRetailQuote(value) && value.pricing_mode === 'gelv_channel_cny'
    && value.credit_by_channel?.alipay_web !== undefined
}
