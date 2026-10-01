import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PaymentProviderDialog from '@/components/payment/PaymentProviderDialog.vue'
import { STRIPE_SDK_API_VERSION } from '@/components/payment/providerConfig'
import type { ProviderInstance } from '@/types/payment'

const messages: Record<string, string> = {
  'admin.settings.payment.providerConfig': 'Credentials',
  'admin.settings.payment.easypayCustomMethods': 'Custom EasyPay methods',
  'admin.settings.payment.easypayCustomMethodsHint': 'Add provider-specific EasyPay type values.',
  'admin.settings.payment.addCustomMethod': 'Add method',
  'admin.settings.payment.customMethodType': 'Payment type',
  'admin.settings.payment.customMethodUpstreamType': 'Upstream type',
  'admin.settings.payment.customMethodDisplayName': 'Display name',
  'admin.settings.payment.customMethodDisplayNamePlaceholder': '信用卡',
  'admin.settings.payment.paymentGuideTrigger': 'View payment guide',
  'admin.settings.payment.alipayGuideSummary': 'Desktop prefers QR precreate and falls back to cashier; mobile prefers WAP checkout.',
  'admin.settings.payment.wxpayGuideSummary': 'Desktop prefers Native QR; mobile routes to JSAPI or H5 based on browser context.',
  'squarespaceProvider.name': 'Card / Squarespace',
  'squarespaceProvider.guideSummary': 'Use the hosted Pay Link and verify the receipt email.',
  'squarespaceProvider.guideNote': 'Merchant authorization is securely imported. Automatic refunds and Webhooks are unavailable.',
  'squarespaceProvider.guideReferenceSummary': 'Enter the top-up reference in the required configured Pay Link field.',
  'squarespaceProvider.guideReferenceNote': 'Reference mode requires a configured required field.',
  'squarespaceProvider.field_paymentClaimMode': 'Payment claim method',
  'squarespaceProvider.claimModeReceiptOTP': 'Receipt email verification',
  'squarespaceProvider.claimModeReference': 'Required payment form reference',
  'squarespaceProvider.field_productId': 'Pay Link product ID',
  'squarespaceProvider.validationProductId': 'Use a 24-character hexadecimal product ID.',
  'squarespaceProvider.field_websiteId': 'Website ID',
  'squarespaceProvider.field_payLinkUrl': 'Pay Link',
  'squarespaceProvider.field_referenceFieldLabel': 'Top-up reference field',
  'admin.settings.payment.airwallexGuideSummary': 'Use Payment Acceptance read/write only.',
  'admin.settings.payment.stripeWebhookHint': 'Configure Stripe webhook.',
  'admin.settings.payment.stripeWebhookApiVersionHint': 'Use Stripe API version {version}.',
  'admin.settings.payment.airwallexWebhookHint': 'Select payment_intent.succeeded and use the latest stable API version.',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      const message = messages[key] ?? key
      if (!params) return message
      return Object.entries(params).reduce(
        (value, [name, replacement]) => value.replaceAll(`{${name}}`, replacement),
        message,
      )
    },
  }),
}))

function providerFactory(overrides: Partial<ProviderInstance> = {}): ProviderInstance {
  return {
    id: 1,
    provider_key: 'airwallex',
    name: 'Airwallex',
    config: {},
    supported_types: ['airwallex'],
    enabled: true,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: 0,
    ...overrides,
  }
}

function mountDialog(options: { editing?: ProviderInstance | null } = {}) {
  return mount(PaymentProviderDialog, {
    props: {
      show: true,
      saving: false,
      editing: options.editing ?? null,
      allKeyOptions: [
        { value: 'easypay', label: 'EasyPay' },
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'stripe', label: 'Stripe' },
        { value: 'airwallex', label: 'Airwallex' },
        { value: 'squarespace', label: 'Card / Squarespace' },
      ],
      enabledKeyOptions: [
        { value: 'easypay', label: 'EasyPay' },
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'airwallex', label: 'Airwallex' },
        { value: 'squarespace', label: 'Card / Squarespace' },
      ],
      allPaymentTypes: [
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
      ],
      redirectLabel: 'Redirect',
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>',
        },
        Select: {
          name: 'Select',
          props: ['modelValue', 'options', 'disabled'],
          template: '<div />',
        },
        ToggleSwitch: {
          template: '<div />',
        },
      },
    },
  })
}

describe('PaymentProviderDialog callback URLs', () => {
  it.each([
    ['https://notify.example.com/', 'https://return.example.com///', 'https://notify.example.com', 'https://return.example.com'],
    [' https://notify.example.com/sub/ ', ' https://return.example.com/site/ ', 'https://notify.example.com/sub', 'https://return.example.com/site'],
    ['https://notify.example.com', 'https://return.example.com', 'https://notify.example.com', 'https://return.example.com'],
    ['', '', window.location.origin, window.location.origin],
  ])('joins callback paths to %s and %s', async (notify, returnUrl, expectedNotify, expectedReturn) => {
    const provider = providerFactory({
      provider_key: 'easypay', name: 'EasyPay',
      config: { pid: 'pid-1', apiBase: 'https://pay.example.com' },
      supported_types: ['alipay'], payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    const bases = wrapper.findAll('input').filter(input => input.classes().includes('!rounded-r-none'))
    await bases[0].setValue(notify)
    await bases[1].setValue(returnUrl)
    await wrapper.find('form').trigger('submit')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.notifyUrl).toBe(expectedNotify + '/api/v1/payment/webhook/easypay')
    expect(payload.config.returnUrl).toBe(expectedReturn + '/payment/result')
    wrapper.unmount()
  })
})

describe('PaymentProviderDialog payment guide', () => {
  it('shows no payment guide for providers without a flow guide', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.alipayGuideSummary'])
    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.wxpayGuideSummary'])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(false)
  })

  it.each([
    ['alipay', 'admin.settings.payment.alipayGuideSummary'],
    ['wxpay', 'admin.settings.payment.wxpayGuideSummary'],
    ['airwallex', 'admin.settings.payment.airwallexGuideSummary'],
    ['squarespace', 'squarespaceProvider.guideSummary'],
  ])('shows the payment guide summary for %s', async (providerKey, summaryKey) => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset(providerKey)
    await nextTick()

    expect(wrapper.text()).toContain(messages[summaryKey])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(true)
  })

  it('shows Airwallex webhook event and API version guidance with the webhook URL', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('airwallex')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.airwallexWebhookHint'])
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/airwallex')
  })

  it('shows Stripe webhook API version guidance with the integrated SDK version', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('stripe')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.stripeWebhookHint'])
    expect(wrapper.text()).toContain(`Use Stripe API version ${STRIPE_SDK_API_VERSION}.`)
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/stripe')
  })

  it('emits an empty Airwallex accountId when the admin clears it', async () => {
    const provider = providerFactory({
      config: {
        clientId: 'cid_123',
        apiBase: 'https://api.airwallex.com/api/v1',
        countryCode: 'CN',
        currency: 'CNY',
        accountId: 'acct_123',
      },
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const accountIdInput = wrapper
      .findAll('input[type="text"]')
      .find(input => (input.element as HTMLInputElement).value === 'acct_123')
    if (!accountIdInput) throw new Error('accountId input not found')

    await accountIdInput.setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.accountId).toBe('')
  })

  it.each(['epay', 'usdt.trc20'])('serializes EasyPay upstream type %s and adds the local type to supported_types', async (upstreamType) => {
    const provider = providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
      },
      supported_types: ['alipay', 'wxpay'],
      payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.find('button.btn-sm').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('input[type="text"]')
    const customTypeInputs = inputs.filter(input => (input.element as HTMLInputElement).placeholder === 'credit_card')
    const ldcTypeInput = customTypeInputs[0]
    const upstreamTypeInput = customTypeInputs[1]
    const displayNameInput = inputs.find(input => (input.element as HTMLInputElement).placeholder === '信用卡')
    if (!ldcTypeInput || !upstreamTypeInput || !displayNameInput) {
      throw new Error('custom method inputs not found')
    }

    await ldcTypeInput.setValue('ldc')
    await upstreamTypeInput.setValue(upstreamType)
    await displayNameInput.setValue('LDC')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as {
      config: Record<string, string>
      supported_types: string[]
    }
    expect(JSON.parse(payload.config.customMethods)).toEqual([{ type: 'ldc', upstreamType, displayName: 'LDC' }])
    expect(payload.supported_types).toEqual(['alipay', 'wxpay', 'ldc'])
  })

  it.each([
    ['alipay_hk', 'hkpay'],
    ['usdt.trc20', 'usdt.trc20'],
    ['usdt_trc20', 'usdt/trc20'],
  ])('rejects invalid EasyPay mapping %s to %s', async (type, upstreamType) => {
    const provider = providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
      },
      supported_types: ['alipay', 'wxpay'],
      payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.find('button.btn-sm').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('input[type="text"]')
    const customTypeInputs = inputs.filter(input => (input.element as HTMLInputElement).placeholder === 'credit_card')
    const typeInput = customTypeInputs[0]
    const upstreamTypeInput = customTypeInputs[1]
    const displayNameInput = inputs.find(input => (input.element as HTMLInputElement).placeholder === '信用卡')
    if (!typeInput || !upstreamTypeInput || !displayNameInput) {
      throw new Error('custom method inputs not found')
    }

    await typeInput.setValue(type)
    await upstreamTypeInput.setValue(upstreamType)
    await displayNameInput.setValue('Custom payment')
    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('save')).toBeUndefined()
  })
})

describe('PaymentProviderDialog Squarespace', () => {
  it('saves GBP public metadata without refund flags, OAuth credentials, or callbacks', async () => {
    const provider = providerFactory({
      provider_key: 'squarespace',
      name: 'Card / Squarespace',
      supported_types: ['squarespace'],
      refund_enabled: true,
      allow_user_refund: true,
      config: {
        websiteId: 'site_123',
        productId: ' 0123456789ABCDEF01234567 ',
        payLinkUrl: 'https://merchant.squarespace.com/pay-link/123',
        currency: 'CNY',
        referenceFieldLabel: 'RynexAI top-up reference',
        accessToken: 'must-not-submit',
        notifyUrl: 'https://site.example/api/v1/payment/webhook/squarespace',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    expect(wrapper.text()).not.toContain('admin.settings.payment.refundEnabled')
    expect(wrapper.text()).not.toContain('admin.settings.payment.allowUserRefund')
    expect(wrapper.findAll('textarea')).toHaveLength(0)
    expect(wrapper.text()).not.toContain(messages['squarespaceProvider.field_referenceFieldLabel'])
    expect(wrapper.text()).toContain(messages['squarespaceProvider.guideSummary'])
    expect(wrapper.text()).not.toContain('/api/v1/payment/webhook/squarespace')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as {
      provider_key: string
      config: Record<string, string>
      supported_types: string[]
      refund_enabled: boolean
      allow_user_refund: boolean
      payment_mode: string
    }
    expect(payload.provider_key).toBe('squarespace')
    expect(payload.supported_types).toEqual(['squarespace'])
    expect(payload.refund_enabled).toBe(false)
    expect(payload.allow_user_refund).toBe(false)
    expect(payload.payment_mode).toBe('')
    expect(payload.config).toEqual({
      websiteId: 'site_123',
      productId: '0123456789abcdef01234567',
      orderScopeMode: 'fixed_product',
      paymentPurpose: '',
      expectedServiceName: '',
      payLinkUrl: 'https://merchant.squarespace.com/pay-link/123',
      currency: 'GBP',
      paymentClaimMode: 'receipt_otp',
    })
    wrapper.unmount()
  })

  it('creates an explicit dedicated-site service scope without a product ID or public approval flag', async () => {
    const wrapper = mountDialog()
    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('squarespace')
    await nextTick()
    await wrapper.find('form input[type="text"]').setValue('Card / Squarespace')
    await wrapper.get('[data-config-field="websiteId"] input').setValue('site_123')
    await wrapper.get('[data-config-field="payLinkUrl"] input').setValue('https://merchant.squarespace.com/pay-link/')
    expect(wrapper.find('[data-config-field="productId"]').exists()).toBe(false)
    expect(wrapper.find('[data-config-field="paymentPurpose"]').exists()).toBe(true)
    expect(wrapper.find('[data-config-field="expectedServiceName"]').exists()).toBe(true)
    await wrapper.find('form').trigger('submit.prevent')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config).toMatchObject({
      orderScopeMode: 'dedicated_site_service',
      paymentPurpose: 'balance_topup_only',
      expectedServiceName: 'Pay',
      productId: '',
      paymentClaimMode: 'receipt_otp',
      currency: 'GBP',
    })
    expect(payload.config).not.toHaveProperty('productionApproved')
    wrapper.unmount()
  })

  it('requires an explicit scope change to move a legacy fixed product into dedicated-site service mode', async () => {
    const provider = providerFactory({
      provider_key: 'squarespace',
      config: {
        websiteId: 'site_123',
        productId: '0123456789abcdef01234567',
        payLinkUrl: 'https://merchant.squarespace.com/pay-link/',
        currency: 'GBP',
        productionApproved: 'false',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    expect(wrapper.find('[data-config-field="productId"]').exists()).toBe(true)
    expect(wrapper.find('[data-config-field="paymentPurpose"]').exists()).toBe(false)
    const selector = wrapper.findAllComponents({ name: 'Select' }).find(component =>
      (component.props('options') as Array<{ value: string }>).some(option => option.value === 'dedicated_site_service'),
    )
    if (!selector) throw new Error('Order scope selector missing')
    selector.vm.$emit('update:modelValue', 'dedicated_site_service')
    await nextTick()
    expect(wrapper.find('[data-config-field="productId"]').exists()).toBe(false)
    expect(wrapper.find('[data-config-field="paymentPurpose"]').exists()).toBe(true)
    await wrapper.find('form').trigger('submit.prevent')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.orderScopeMode).toBe('dedicated_site_service')
    expect(payload.config.productId).toBe('')
    expect(payload.config.paymentPurpose).toBe('balance_topup_only')
    expect(payload.config.expectedServiceName).toBe('Pay')
    expect(payload.config).not.toHaveProperty('productionApproved')
    wrapper.unmount()
  })

  it.each([
    { orderScopeMode: 'whole_store' },
    { orderScopeMode: 'dedicated_site_service', paymentPurpose: 'general_goods', expectedServiceName: 'Pay' },
    { orderScopeMode: 'dedicated_site_service', paymentPurpose: 'balance_topup_only', expectedServiceName: 'Other service' },
  ])('rejects unsupported merchant scope identity %j', async identity => {
    const provider = providerFactory({
      provider_key: 'squarespace',
      config: {
        websiteId: 'site_123',
        payLinkUrl: 'https://merchant.squarespace.com/pay-link/',
        currency: 'GBP',
        ...identity,
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    await wrapper.find('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')).toBeUndefined()
    wrapper.unmount()
  })

  it('retains the future reference mode and its exact form field when explicitly configured', async () => {
    const provider = providerFactory({
      provider_key: 'squarespace',
      supported_types: ['squarespace'],
      config: {
        websiteId: 'site_123',
        productId: '0123456789abcdef01234567',
        payLinkUrl: 'https://merchant.squarespace.com/pay-link/',
        currency: 'GBP',
        paymentClaimMode: 'reference',
        referenceFieldLabel: 'Configured top-up reference',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    expect(wrapper.text()).toContain(messages['squarespaceProvider.field_referenceFieldLabel'])
    expect(wrapper.text()).toContain(messages['squarespaceProvider.guideReferenceSummary'])
    expect(wrapper.text()).not.toContain(messages['squarespaceProvider.guideSummary'])
    await wrapper.find('form').trigger('submit.prevent')
    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.paymentClaimMode).toBe('reference')
    expect(payload.config.referenceFieldLabel).toBe('Configured top-up reference')
    wrapper.unmount()
  })

  it.each(['', 'another-product', '0123456789abcdef0123456'])('blocks missing or invalid product ID %s before saving', async productId => {
    const provider = providerFactory({
      provider_key: 'squarespace',
      config: {
        websiteId: 'site_123',
        productId,
        payLinkUrl: 'https://merchant.squarespace.com/pay-link/',
        currency: 'GBP',
        paymentClaimMode: 'receipt_otp',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    await wrapper.find('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')).toBeUndefined()
    wrapper.unmount()
  })

  it('blocks unrelated or insecure Pay Links before emitting a save', async () => {
    const provider = providerFactory({
      provider_key: 'squarespace',
      config: {
        websiteId: 'site_123',
        productId: '0123456789abcdef01234567',
        payLinkUrl: 'https://evil.example/pay-link/123',
        currency: 'GBP',
        referenceFieldLabel: 'RynexAI top-up reference',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()
    await wrapper.find('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')).toBeUndefined()
    wrapper.unmount()
  })
})
