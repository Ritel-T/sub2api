import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

enableAutoUnmount(afterEach)

const pollOrderStatus = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const toCanvas = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({
    pollOrderStatus,
  }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
  }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    cancelOrder,
    verifyOrder,
  },
}))

vi.mock('qrcode', () => ({
  default: {
    toCanvas,
  },
}))

import PaymentStatusPanel from '../PaymentStatusPanel.vue'
import SquarespaceReceiptClaim from '../SquarespaceReceiptClaim.vue'

const orderFactory = (status: string) => ({
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  fee_rate: 0,
  payment_type: 'alipay',
  out_trade_no: 'sub2_20260420abcd1234',
  status,
  order_type: 'balance',
  created_at: '2026-04-20T12:00:00Z',
  expires_at: '2099-01-01T12:30:00Z',
  refund_amount: 0,
})

describe('PaymentStatusPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset()
    cancelOrder.mockReset()
    verifyOrder.mockReset()
    showError.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('allows an expired owned receipt order to be claimed without reopening the expired payment link', async () => {
    const wrapper = mount(PaymentStatusPanel, { props: { orderId: 42, qrCode: '', expiresAt: '2020-01-01T00:00:00Z', paymentType: 'squarespace', orderType: 'balance', paymentClaimMode: 'receipt_otp', accountEmail: 'mine@example.com' }, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.findComponent(SquarespaceReceiptClaim).exists()).toBe(true)
    expect(wrapper.find('[data-test="open-squarespace-checkout"]').exists()).toBe(false)
    expect(wrapper.emitted('success')).toBeUndefined()
    wrapper.findComponent(SquarespaceReceiptClaim).vm.$emit('updated', { ...orderFactory('COMPLETED'), payment_type: 'squarespace' })
    await flushPromises()
    expect(wrapper.emitted('success')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not offer receipt OTP after the server reports cancellation and asks for manual support', async () => {
    pollOrderStatus.mockResolvedValue({ ...orderFactory('CANCELLED'), payment_type: 'squarespace' })
    const wrapper = mount(PaymentStatusPanel, { props: { orderId: 42, qrCode: '', expiresAt: '2099-01-01T00:00:00Z', paymentType: 'squarespace', orderType: 'balance', paymentClaimMode: 'receipt_otp' }, global: { stubs: { Icon: true } } })
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.findComponent(SquarespaceReceiptClaim).exists()).toBe(false)
    expect(wrapper.text()).toContain('paymentRetail.claim.cancelledManual')
    expect(wrapper.emitted('success')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps retail PAID and RECHARGING pending until the balance is COMPLETED', async () => {
    pollOrderStatus.mockResolvedValueOnce({ ...orderFactory('PAID'), payment_type: 'squarespace' })
      .mockResolvedValueOnce({ ...orderFactory('RECHARGING'), payment_type: 'squarespace' })
      .mockResolvedValueOnce({ ...orderFactory('COMPLETED'), payment_type: 'squarespace' })
    const wrapper = mount(PaymentStatusPanel, { props: { orderId: 42, qrCode: '', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'squarespace', orderType: 'balance' }, global: { stubs: { Icon: true } } })
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.text()).toContain('paymentRetail.creditProcessing')
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.emitted('success')).toHaveLength(1)
    wrapper.unmount()
  })

  it('treats RECHARGING as a successful terminal state', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('RECHARGING'))

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('shows reopen button in QR mode when payUrl is also available', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as Window)

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        payUrl: 'https://pay.example.com/session/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('payment.qr.openPayWindow')

    await wrapper.get('button.btn.btn-secondary.text-sm').trigger('click')
    expect(openSpy).toHaveBeenCalledWith(
      'https://pay.example.com/session/42',
      'paymentPopup',
      expect.any(String),
    )

    openSpy.mockRestore()
  })

  it('uses generic QR copy for custom methods that contain built-in names', async () => {
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'card_alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.qr.scanToPay')
    expect(wrapper.text()).not.toContain('payment.qr.scanAlipay')
  })

  it('actively verifies a stuck pending order and settles it when upstream confirms payment', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'wxpay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('actively verifies a pending mobile Alipay precreate order', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: vi.fn() },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false,
    })
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('actively verifies a pending desktop Alipay order', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/desktop-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
  })

  it('keeps the QR fallback hidden until the Alipay app launch times out', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false,
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    expect(assign).toHaveBeenCalledWith(expect.stringContaining('alipays://platformapi/startapp?saId=10000007&qrcode='))
    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('payment.qr.saveQRCode')
    expect(wrapper.text()).toContain('sub2_20260420abcd1234')
    expect(toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), 'https://qr.alipay.com/dynamic-order-42', expect.any(Object))

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('does not show the QR fallback after the page enters the background', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    let hidden = false
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: vi.fn() },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => hidden,
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.qr.alipayContinueInApp')

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })
})


describe('Alipay hosted waiting and refund reconciliation', () => {
  const payUrl = 'https://alar1c.cn/partner-pay/#id=' + 'a'.repeat(24) + '&token=' + 'b'.repeat(64)
  const props = { orderId: 42, amount: 9.37, payAmount: 10, qrCode: '', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'gelv_alipay', orderType: 'balance', payUrl, currency: 'CNY' }
  beforeEach(() => { vi.useFakeTimers(); pollOrderStatus.mockReset() })
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })
  it('requires a fresh read before opening a recovered token, and opens only on click', async () => {
    let resolveOrder!: (value: unknown) => void
    pollOrderStatus.mockReturnValue(new Promise(resolve => { resolveOrder = resolve }))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const open = vi.spyOn(window, 'open')
    const wrapper = mount(PaymentStatusPanel, { props, global: { stubs: { Icon: true } } })
    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(wrapper.findAll('button').some(button => button.text() === 'gelv.openAlipay')).toBe(false)
    resolveOrder({ ...orderFactory('PENDING'), payment_type: 'gelv_alipay', pay_url: payUrl, refund_reconciliation_pending: false })
    await flushPromises()
    expect(click).not.toHaveBeenCalled()
    const launch = wrapper.findAll('button').find(button => button.text() === 'gelv.openAlipay')!
    expect(launch).toBeDefined()
    await launch.trigger('click')
    expect(click).toHaveBeenCalledTimes(1)
    expect(open).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
  it('suppresses held tokens, emits local recovery invalidation, and keeps polling', async () => {
    pollOrderStatus.mockResolvedValue({ ...orderFactory('PENDING'), payment_type: 'gelv_alipay', pay_url: payUrl, refund_reconciliation_pending: true })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(PaymentStatusPanel, { props, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.emitted('checkoutUnavailable')).toHaveLength(1)
    expect(wrapper.text()).toContain('gelv.alipayHoldHint')
    expect(wrapper.findAll('button').some(button => button.text() === 'gelv.openAlipay')).toBe(false)
    expect(wrapper.html()).not.toContain(payUrl)
    expect(click).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toBeUndefined()
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('gelv.alipayHoldHint')
    expect(click).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })
  it('clears locally stored links when the fresh order omits its checkout URL', async () => {
    pollOrderStatus.mockResolvedValue({ ...orderFactory('PENDING'), payment_type: 'gelv_alipay' })
    const wrapper = mount(PaymentStatusPanel, { props, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.emitted('checkoutUnavailable')).toHaveLength(1)
    expect(wrapper.findAll('button').some(button => button.text() === 'gelv.openAlipay')).toBe(false)
    wrapper.unmount()
  })
  it('releases a hold only after explicit fresh false and opens the newly returned URL', async () => {
    const freshUrl = payUrl.replace('token=' + 'b'.repeat(64), 'token=' + 'c'.repeat(64))
    pollOrderStatus.mockResolvedValueOnce({ ...orderFactory('PENDING'), payment_type: 'gelv_alipay', refund_reconciliation_pending: true })
      .mockResolvedValueOnce({ ...orderFactory('PENDING'), payment_type: 'gelv_alipay', pay_url: freshUrl })
      .mockResolvedValueOnce({ ...orderFactory('PENDING'), payment_type: 'gelv_alipay', pay_url: freshUrl, refund_reconciliation_pending: false })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(PaymentStatusPanel, { props, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('gelv.alipayHoldWaiting')
    expect(wrapper.text()).not.toContain('payment.qr.cancelOrder')
    expect(wrapper.text()).not.toContain('payment.qr.waitingPayment')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.findAll('button').some(button => button.text() === 'gelv.openAlipay')).toBe(false)
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.emitted('checkoutAvailable')).toEqual([[freshUrl]])
    const button = wrapper.findAll('button').find(button => button.text() === 'gelv.openAlipay')!
    expect(button).toBeDefined()
    expect(wrapper.text()).toContain('payment.qr.cancelOrder')
    expect(click).not.toHaveBeenCalled()
    await button.trigger('click')
    expect(click).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

})


describe.each(['gelv', 'gelv_alipay'])('%s persisted-order polling', (paymentType) => {
  const props = { orderId: 42, qrCode: '', expiresAt: '2099-01-01T12:30:00Z', paymentType, orderType: 'balance' }
  const owned = (status: string) => ({ ...orderFactory(status), payment_type: paymentType })
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset().mockResolvedValue(owned('PENDING'))
    verifyOrder.mockReset()
  })
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

  it('reads every two seconds, and waits through PAID and RECHARGING for COMPLETED', async () => {
    const wrapper = mount(PaymentStatusPanel, { props, global: { stubs: { Icon: true } } })
    await flushPromises()
    const initialReads = pollOrderStatus.mock.calls.length
    pollOrderStatus.mockResolvedValueOnce(owned('PAID')).mockResolvedValueOnce(owned('RECHARGING')).mockResolvedValueOnce(owned('COMPLETED'))
    await vi.advanceTimersByTimeAsync(1999)
    expect(pollOrderStatus).toHaveBeenCalledTimes(initialReads)
    await vi.advanceTimersByTimeAsync(1)
    expect(pollOrderStatus).toHaveBeenCalledTimes(initialReads + 1)
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(verifyOrder).not.toHaveBeenCalled()
    const terminalReads = pollOrderStatus.mock.calls.length
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(4000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(terminalReads)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('coalesces focus, visibility and timer reads without relaunching payment, and ignores late unmounted responses', async () => {
    const wrapper = mount(PaymentStatusPanel, { props, global: { stubs: { Icon: true } } })
    await flushPromises()
    const initialReads = pollOrderStatus.mock.calls.length
    let resolveRead!: (value: ReturnType<typeof owned>) => void
    pollOrderStatus.mockReturnValue(new Promise(resolve => { resolveRead = resolve }))
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(initialReads)
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('focus'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(initialReads + 1)
    await vi.advanceTimersByTimeAsync(6000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(initialReads + 1)
    expect(open).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
    resolveRead(owned('COMPLETED'))
    await flushPromises()
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(6000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(initialReads + 1)
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('checkoutAvailable')).toBeUndefined()
    expect(verifyOrder).not.toHaveBeenCalled()
  })
})

describe('non-Gelv polling compatibility', () => {
  it('keeps the three-second timer and does not add focus reads', async () => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset().mockResolvedValue({ ...orderFactory('PENDING'), payment_type: 'custom' })
    const wrapper = mount(PaymentStatusPanel, { props: { orderId: 42, qrCode: '', expiresAt: '2099-01-01T12:30:00Z', paymentType: 'custom' } })
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(2000)
    expect(pollOrderStatus).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    vi.useRealTimers()
  })
})
