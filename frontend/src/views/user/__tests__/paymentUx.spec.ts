import { describe, expect, it } from 'vitest'
import {
  SUBSCRIPTION_RETURN_KEY, saveSubscriptionReturn, readSubscriptionReturn, clearSubscriptionReturn,
  buildPaymentErrorToastMessage,
  describePaymentScenarioError,
  normalizePaymentMethodForDisplay,
} from '../paymentUx'

describe('normalizePaymentMethodForDisplay', () => {
  it('collapses visible payment aliases to canonical method ids', () => {
    expect(normalizePaymentMethodForDisplay(' alipay_direct ')).toBe('alipay')
    expect(normalizePaymentMethodForDisplay('wxpay_direct')).toBe('wxpay')
    expect(normalizePaymentMethodForDisplay('wechat_pay')).toBe('wxpay')
  })

  it('leaves non-aliased methods untouched', () => {
    expect(normalizePaymentMethodForDisplay('stripe')).toBe('stripe')
  })
})

describe('describePaymentScenarioError', () => {
  it('maps WeChat H5 authorization errors to explicit in-app guidance', () => {
    expect(describePaymentScenarioError(
      { reason: 'WECHAT_H5_NOT_AUTHORIZED' },
      { paymentMethod: 'wxpay', isMobile: true, isWechatBrowser: false },
    )).toEqual({
      messageKey: 'payment.errors.wechatH5NotAuthorized',
      hintKey: 'payment.errors.wechatOpenInWeChatHint',
    })
  })

  it('maps WeChat H5 authorization errors when provider aliases use wxpay_direct', () => {
    expect(describePaymentScenarioError(
      { reason: 'WECHAT_H5_NOT_AUTHORIZED' },
      { paymentMethod: 'wxpay_direct', isMobile: true, isWechatBrowser: false },
    )).toEqual({
      messageKey: 'payment.errors.wechatH5NotAuthorized',
      hintKey: 'payment.errors.wechatOpenInWeChatHint',
    })
  })

  it('maps missing WeixinJSBridge to a JSAPI-specific prompt', () => {
    expect(describePaymentScenarioError(
      new Error('WeixinJSBridge is unavailable'),
      { paymentMethod: 'wxpay', isMobile: true, isWechatBrowser: true },
    )).toEqual({
      messageKey: 'payment.errors.wechatJsapiUnavailable',
      hintKey: 'payment.errors.wechatOpenInWeChatHint',
    })
  })

  it('maps the internal JSAPI unavailable marker to the same prompt', () => {
    expect(describePaymentScenarioError(
      new Error('WECHAT_JSAPI_UNAVAILABLE'),
      { paymentMethod: 'wxpay', isMobile: true, isWechatBrowser: true },
    )).toEqual({
      messageKey: 'payment.errors.wechatJsapiUnavailable',
      hintKey: 'payment.errors.wechatOpenInWeChatHint',
    })
  })

  it('maps generic desktop Alipay failures to QR guidance', () => {
    expect(describePaymentScenarioError(
      { reason: 'PAYMENT_GATEWAY_ERROR' },
      { paymentMethod: 'alipay', isMobile: false, isWechatBrowser: false },
    )).toEqual({
      messageKey: 'payment.errors.alipayDesktopUnavailable',
      hintKey: 'payment.errors.alipayDesktopQrHint',
    })
  })
})

describe('buildPaymentErrorToastMessage', () => {
  it('returns the main message when no hint is present', () => {
    expect(buildPaymentErrorToastMessage('Payment failed')).toBe('Payment failed')
  })

  it('appends the hint to the toast body when present', () => {
    expect(buildPaymentErrorToastMessage('Payment failed', 'Open WeChat to continue.')).toBe(
      'Payment failed Open WeChat to continue.'
    )
  })
})


describe('subscription recharge return', () => {
  it('keeps a user-owned plan identity and renewal mode without storing prices', () => {
    sessionStorage.clear()
    expect(saveSubscriptionReturn(sessionStorage, { userId: 7, planId: 2, renewalMode: 'extend' })).toBe(true)
    expect(readSubscriptionReturn(sessionStorage, 7)).toMatchObject({ planId: 2, renewalMode: 'extend' })
    expect(readSubscriptionReturn(sessionStorage, 8)).toBeNull()
    expect(sessionStorage.getItem(SUBSCRIPTION_RETURN_KEY)).not.toContain('price')
    clearSubscriptionReturn(sessionStorage)
    expect(readSubscriptionReturn(sessionStorage, 7)).toBeNull()
  })
  it('rejects malformed, expired or future intent', () => {
    for (const savedAt of [Date.now() - 86400001, Date.now() + 60000, 'yesterday']) {
      sessionStorage.setItem(SUBSCRIPTION_RETURN_KEY, JSON.stringify({ userId: 7, planId: 2, renewalMode: 'extend', savedAt }))
      expect(readSubscriptionReturn(sessionStorage, 7)).toBeNull()
    }
    sessionStorage.setItem(SUBSCRIPTION_RETURN_KEY, '{')
    expect(readSubscriptionReturn(sessionStorage, 7)).toBeNull()
  })
})
