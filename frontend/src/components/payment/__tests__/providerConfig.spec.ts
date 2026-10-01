import { describe, expect, it } from 'vitest'
import {
  PAYMENT_CURRENCY_OPTIONS,
  PROVIDER_CONFIG_FIELDS,
  PROVIDER_SUPPORTED_TYPES,
  getVisibleProviderConfigFields,
  WEBHOOK_PATHS,
  isValidSquarespacePayLink,
  isValidSquarespaceProductId,
  isBuiltInAlipayMethod,
  isBuiltInWxpayMethod,
  parseEasyPayCustomMethods,
  serializeEasyPayCustomMethods,
} from '@/components/payment/providerConfig'

function findField(providerKey: string, key: string) {
  const fields = PROVIDER_CONFIG_FIELDS[providerKey] || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('wxpay', 'publicKeyId')?.optional).toBeFalsy()
    expect(findField('wxpay', 'certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('wxpay', 'mpAppId')).toBeUndefined()
    expect(findField('wxpay', 'h5AppName')).toBeUndefined()
    expect(findField('wxpay', 'h5AppUrl')).toBeUndefined()
  })
})

describe('PROVIDER_CONFIG_FIELDS.airwallex', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('airwallex', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })

  it('marks accountId as optional and explains when it can be left blank', () => {
    const accountId = findField('airwallex', 'accountId')

    expect(accountId?.optional).toBe(true)
    expect(accountId?.clearable).toBe(true)
    expect(accountId?.hintKey).toBe('admin.settings.payment.field_accountIdHint')
  })

  it('explains that apiBase must match the Airwallex key environment', () => {
    expect(findField('airwallex', 'apiBase')?.hintKey).toBe('admin.settings.payment.field_airwallexApiBaseHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.stripe', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('stripe', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })
})

describe('EasyPay custom methods config', () => {
  it('parses customMethods from the JSON string stored in provider config', () => {
    expect(parseEasyPayCustomMethods(
      '[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":"USDT-TRC20"}]',
    )).toEqual([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: 'USDT-TRC20' },
    ])
  })

  it('serializes non-empty custom methods into the config string format', () => {
    expect(serializeEasyPayCustomMethods([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: '  ', upstreamType: 'ignored', displayName: 'Ignored' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: '' },
    ])).toBe('[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":""}]')
  })

  it('returns an empty string for invalid or empty custom methods', () => {
    expect(parseEasyPayCustomMethods('not-json')).toEqual([])
    expect(serializeEasyPayCustomMethods([{ type: '', upstreamType: 'epay', displayName: 'LDC' }])).toBe('')
  })
})

describe('built-in payment method helpers', () => {
  it('only treats exact built-in aliases as Alipay or WeChat Pay', () => {
    expect(isBuiltInAlipayMethod('alipay')).toBe(true)
    expect(isBuiltInAlipayMethod('alipay_direct')).toBe(true)
    expect(isBuiltInAlipayMethod('card_alipay')).toBe(false)

    expect(isBuiltInWxpayMethod('wxpay')).toBe(true)
    expect(isBuiltInWxpayMethod('wxpay_direct')).toBe(true)
    expect(isBuiltInWxpayMethod('card_wxpay')).toBe(false)
  })
})

describe('Squarespace public provider configuration', () => {
  it('exposes only hosted-link metadata and restricts currency to GBP', () => {
    expect(PROVIDER_SUPPORTED_TYPES.squarespace).toEqual(['squarespace'])
    expect(PROVIDER_CONFIG_FIELDS.squarespace.map(field => field.key)).toEqual([
      'websiteId', 'orderScopeMode', 'paymentPurpose', 'expectedServiceName', 'productId', 'payLinkUrl', 'currency', 'paymentClaimMode', 'referenceFieldLabel',
    ])
    expect(PROVIDER_CONFIG_FIELDS.squarespace.every(field => !field.sensitive)).toBe(true)
    expect(findField('squarespace', 'currency')?.options).toEqual([{ value: 'GBP', label: 'GBP' }])
    expect(findField('squarespace', 'currency')?.defaultValue).toBe('GBP')
    expect(WEBHOOK_PATHS.squarespace).toBeUndefined()
    expect(findField('squarespace', 'paymentClaimMode')?.defaultValue).toBe('receipt_otp')
    expect(findField('squarespace', 'productId')?.optional).toBeFalsy()
    expect(findField('squarespace', 'productId')?.defaultValue).toBeUndefined()
    expect(findField('squarespace', 'orderScopeMode')?.defaultValue).toBe('dedicated_site_service')
    expect(findField('squarespace', 'paymentPurpose')?.defaultValue).toBe('balance_topup_only')
    expect(findField('squarespace', 'expectedServiceName')?.defaultValue).toBe('Pay')
  })

  it('exposes dedicated service identity without requiring a fixed product, and keeps fixed-product compatibility fields', () => {
    const dedicated = getVisibleProviderConfigFields('squarespace', { orderScopeMode: 'dedicated_site_service' }).map(field => field.key)
    expect(dedicated).toContain('paymentPurpose')
    expect(dedicated).toContain('expectedServiceName')
    expect(dedicated).not.toContain('productId')
    const fixed = getVisibleProviderConfigFields('squarespace', { orderScopeMode: 'fixed_product' }).map(field => field.key)
    expect(fixed).toContain('productId')
    expect(fixed).not.toContain('paymentPurpose')
    expect(fixed).not.toContain('expectedServiceName')
  })

  it('only requires the reference field when reference claim mode is selected', () => {
    expect(getVisibleProviderConfigFields('squarespace', {}).map(field => field.key)).not.toContain('referenceFieldLabel')
    expect(getVisibleProviderConfigFields('squarespace', { paymentClaimMode: 'receipt_otp' }).map(field => field.key)).not.toContain('referenceFieldLabel')
    expect(getVisibleProviderConfigFields('squarespace', { paymentClaimMode: 'reference' }).map(field => field.key)).toContain('referenceFieldLabel')
  })

  it.each([
    'https://merchant.squarespace.com/pay-link/123',
    'https://ritelt.squarespace.com/pay-link/',
    ' https://merchant.squarespace.com/pay-link/123?source=website ',
  ])('accepts the existing hosted Pay Link %s', url => {
    expect(isValidSquarespacePayLink(url)).toBe(true)
  })

  it.each([
    'http://merchant.squarespace.com/pay-link/123',
    'https://squarespace.com/pay-link/123',
    'https://merchant.squarespace.com.evil.example/pay-link/123',
    'https://evil.example/pay-link/123',
    'https://merchant.squarespace.com/checkout/123',
    'https://user:secret@merchant.squarespace.com/pay-link/123',
    'https://merchant.squarespace.com:8443/pay-link/123',
    'https://merchant.squarespace.com:443/pay-link/123',
    'https://merchant.squarespace.com/pay-link/123#fragment',
    'not-a-url',
  ])('rejects an unsafe or unrelated Pay Link %s', url => {
    expect(isValidSquarespacePayLink(url)).toBe(false)
  })
})

describe('Squarespace top-up product ID validation', () => {
  it.each(['0123456789abcdef01234567', ' 0123456789ABCDEF01234567 '])('accepts a valid product ID %s', value => {
    expect(isValidSquarespaceProductId(value)).toBe(true)
  })
  it.each(['', '0123456789abcdef0123456', '0123456789abcdef012345678', '0123456789abcdef0123456z', 'another-product'])('rejects invalid product ID %s', value => {
    expect(isValidSquarespaceProductId(value)).toBe(false)
  })
})
