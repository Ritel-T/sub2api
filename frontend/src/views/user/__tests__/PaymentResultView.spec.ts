import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

enableAutoUnmount(afterEach)

const { refreshLottery } = vi.hoisted(() => ({ refreshLottery: vi.fn() }))
vi.mock('@/stores/lottery', () => ({ useLotteryStore: () => ({ enabled: true, refresh: refreshLottery }) }))

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))

const routerPush = vi.hoisted(() => vi.fn())
const pollOrderStatus = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const verifyOrderPublic = vi.hoisted(() => vi.fn())
const resolveOrderPublicByResumeToken = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({ push: routerPush }),
  }
})

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

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser,
  }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    verifyOrder,
    verifyOrderPublic,
    resolveOrderPublicByResumeToken,
  },
}))

import PaymentResultView from '../PaymentResultView.vue'
import SquarespaceReceiptClaim from '@/components/payment/SquarespaceReceiptClaim.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import { formatPaymentAmount } from '@/components/payment/currency'

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
  expires_at: '2026-04-20T12:30:00Z',
  refund_amount: 0,
})

const recoverySnapshotFactory = (resumeToken: string) => ({
  orderId: 42,
  amount: 88,
  qrCode: '',
  expiresAt: '2099-01-01T00:10:00.000Z',
  paymentType: 'alipay',
  payUrl: 'https://pay.example.com/session/42',
  outTradeNo: 'sub2_20260420abcd1234',
  clientSecret: '',
  intentId: '',
  currency: '',
  countryCode: '',
  paymentEnv: '',
  payAmount: 88,
  orderType: 'balance',
  paymentMode: 'popup',
  resumeToken,
  createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
})

describe('PaymentResultView', () => {
  beforeEach(() => {
    routeState.query = {}
    routerPush.mockReset()
    pollOrderStatus.mockReset()
    verifyOrder.mockReset()
    verifyOrderPublic.mockReset()
    resolveOrderPublicByResumeToken.mockReset()
    refreshUser.mockReset()
    refreshLottery.mockReset()
    refreshUser.mockResolvedValue({})
    window.localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('offers receipt claim for an authenticated owned order even after its quote expired, then trusts only completed status', async () => {
    routeState.query = { order_id: '42' }
    const owned = { ...orderFactory('EXPIRED'), payment_type: 'squarespace', payment_claim_mode: 'receipt_otp', expires_at: '2020-01-01T00:00:00Z' }
    pollOrderStatus.mockResolvedValue(owned)
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    const claimForm = wrapper.findComponent(SquarespaceReceiptClaim)
    expect(claimForm.exists()).toBe(true)
    expect(claimForm.props('orderId')).toBe(42)
    expect(refreshUser).not.toHaveBeenCalled()
    claimForm.vm.$emit('updated', { ...owned, status: 'COMPLETED' })
    await flushPromises()
    expect(wrapper.text()).toContain('payment.result.success')
    expect(refreshUser).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('requires support for a cancelled owned order and never exposes an OTP claim form', async () => {
    routeState.query = { order_id: '42' }
    pollOrderStatus.mockResolvedValue({ ...orderFactory('CANCELLED'), payment_type: 'squarespace', payment_claim_mode: 'receipt_otp' })
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    expect(wrapper.findComponent(SquarespaceReceiptClaim).exists()).toBe(false)
    expect(wrapper.text()).toContain('paymentRetail.claim.cancelledManual')
    expect(refreshUser).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('never shows a receipt claim form from an anonymous result without an authenticated owned-order read', async () => {
    routeState.query = { resume_token: 'public-square' }
    resolveOrderPublicByResumeToken.mockResolvedValue({ data: { ...orderFactory('EXPIRED'), payment_type: 'squarespace', payment_claim_mode: 'receipt_otp' } })
    pollOrderStatus.mockResolvedValue(null)
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    expect(wrapper.findComponent(SquarespaceReceiptClaim).exists()).toBe(false)
    expect(refreshUser).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps an anonymous retail PAID or RECHARGING result pending until credit completes', async () => {
    vi.useFakeTimers()
    routeState.query = { resume_token: 'retail-resume', status: 'success' }
    const publicOrder = { out_trade_no: 'retail-42', paid: true, created_at: '2026-09-29T12:00:00Z', expires_at: '2099-01-01T00:00:00Z', retail_pricing: true }
    resolveOrderPublicByResumeToken.mockResolvedValueOnce({ data: { ...publicOrder, status: 'PAID' } })
      .mockResolvedValueOnce({ data: { ...publicOrder, status: 'RECHARGING' } })
      .mockResolvedValueOnce({ data: { ...publicOrder, status: 'COMPLETED' } })
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('paymentRetail.creditProcessing')
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(refreshUser).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).not.toContain('payment.result.success')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.text()).toContain('payment.result.success')
    expect(refreshUser).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('renders retail GBP product costs from the stored quote without reversing a gateway fee', async () => {
    routeState.query = { order_id: '42' }
    pollOrderStatus.mockResolvedValue({ ...orderFactory('COMPLETED'), amount: 10, pay_amount: 8.42, currency: 'GBP', fee_rate: 99, payment_type: 'squarespace',
      retail_quote: { credited_amount_usd: 10, base_amount_gbp: 8, included_cost_gbp: 0.42, total_amount_gbp: 8.42, pay_amount: 8.42, currency: 'GBP' } })
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('£8.42 GBP')
    expect(wrapper.text()).toContain('£0.42 GBP')
    expect(wrapper.text()).toContain('$10.00 USD')
    expect(wrapper.text()).not.toContain('99%')
    expect(wrapper.text()).not.toContain('payment.orders.baseAmount')
    wrapper.unmount()
  })

  it('renders a pending state instead of a failure state when the restored order is still pending', async () => {
    routeState.query = {
      resume_token: 'resume-42',
      order_id: '999',
      status: 'success',
    }
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 42,
      amount: 88,
      qrCode: '',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'alipay',
      payUrl: 'https://pay.example.com/session/42',
      outTradeNo: 'sub2_20260420abcd1234',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 88,
      orderType: 'balance',
      paymentMode: 'redirect',
      resumeToken: 'resume-42',
      createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    }))
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: orderFactory('PENDING'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-42')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.processing')
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(wrapper.text()).not.toContain('payment.result.failed')
  })

  it('prefers the public resume-token result over a stale restored DB snapshot', async () => {
    routeState.query = {
      resume_token: 'resume-authoritative',
      order_id: '42',
      status: 'success',
    }
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 42,
      amount: 88,
      qrCode: '',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'alipay',
      payUrl: 'https://pay.example.com/session/42',
      outTradeNo: 'sub2_20260420abcd1234',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 88,
      orderType: 'balance',
      paymentMode: 'popup',
      resumeToken: 'resume-authoritative',
      createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    }))
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: {
        ...orderFactory('COMPLETED'),
        amount: 100,
        pay_amount: 103,
        fee_rate: 3,
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-authoritative')
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).toContain('103.00')
    expect(wrapper.text()).toContain('100.00')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('waits for completed fulfillment before refreshing the user balance', async () => {
    vi.useFakeTimers()
    routeState.query = {
      resume_token: 'resume-77',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify(recoverySnapshotFactory('resume-77')),
    )
    resolveOrderPublicByResumeToken
      .mockResolvedValueOnce({
        data: orderFactory('PENDING'),
      })
      .mockResolvedValueOnce({
        data: orderFactory('COMPLETED'),
      })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(1)
    expect(refreshUser).not.toHaveBeenCalled()
    expect(refreshLottery).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.processing')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).not.toBeNull()

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledTimes(2)
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(refreshLottery).toHaveBeenCalledWith(true)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).not.toContain('payment.result.failed')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('keeps the successful result when refreshing the user balance fails', async () => {
    routeState.query = {
      resume_token: 'resume-refresh-failure',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })
    refreshUser.mockRejectedValueOnce(new Error('profile refresh failed'))

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).not.toContain('payment.result.failed')
  })

  it('falls back to order_id polling when resume-token recovery fails', async () => {
    routeState.query = {
      resume_token: 'resume-fail',
      order_id: '77',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify({
        ...recoverySnapshotFactory('resume-fail'),
        orderId: 42,
      }),
    )
    resolveOrderPublicByResumeToken.mockRejectedValueOnce(new Error('resume failed'))
    pollOrderStatus.mockResolvedValueOnce({
      ...orderFactory('PAID'),
      id: 77,
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-fail')
    expect(pollOrderStatus).toHaveBeenCalledWith(77)
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('falls back to public out_trade_no verification when resume_token recovery fails in legacy return flows', async () => {
    routeState.query = {
      resume_token: 'resume-fail',
      out_trade_no: 'legacy-should-not-run',
      trade_status: 'TRADE_SUCCESS',
    }
    resolveOrderPublicByResumeToken.mockRejectedValueOnce(new Error('resume failed'))
    verifyOrderPublic.mockResolvedValueOnce({
      data: {
        ...orderFactory('PAID'),
        out_trade_no: 'legacy-should-not-run',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-fail')
    expect(verifyOrderPublic).toHaveBeenCalledWith('legacy-should-not-run')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('ignores a stale global recovery snapshot when legacy return markers do not identify the order', async () => {
    routeState.query = {
      trade_status: 'TRADE_SUCCESS',
    }
    window.localStorage.setItem(
      PAYMENT_RECOVERY_STORAGE_KEY,
      JSON.stringify(recoverySnapshotFactory('resume-stale')),
    )

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).not.toHaveBeenCalled()
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.failed')
    expect(wrapper.text()).not.toContain('sub2_20260420abcd1234')
  })

  it('uses public out_trade_no verification when no signed resume context is available', async () => {
    routeState.query = {
      out_trade_no: 'legacy-123',
      trade_status: 'TRADE_SUCCESS',
    }
    verifyOrder.mockRejectedValue(new Error('auth required'))
    verifyOrderPublic.mockResolvedValue({
      data: orderFactory('PAID'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('legacy-123')
    expect(verifyOrderPublic).toHaveBeenCalledWith('legacy-123')
    expect(pollOrderStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('renders the minimal public out_trade_no verification result without payment_type', async () => {
    routeState.query = {
      out_trade_no: 'legacy-minimal',
      trade_status: 'TRADE_SUCCESS',
    }
    verifyOrder.mockRejectedValue(new Error('auth required'))
    verifyOrderPublic.mockResolvedValue({
      data: {
        out_trade_no: 'legacy-minimal',
        status: 'PAID',
        paid: true,
        created_at: '2026-04-20T12:00:00Z',
        expires_at: '2026-04-20T12:30:00Z',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.text()).toContain('legacy-minimal')
    expect(wrapper.text()).not.toContain('payment.orders.paymentMethod')
  })

  it('prefers authenticated order verification before falling back to public lookup', async () => {
    routeState.query = {
      out_trade_no: 'auth-verify-123',
      trade_status: 'TRADE_SUCCESS',
    }
    verifyOrder.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('auth-verify-123')
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('does not use public out_trade_no verification for bare order numbers without legacy return markers', async () => {
    routeState.query = {
      out_trade_no: 'legacy-bare',
    }

    mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(verifyOrderPublic).not.toHaveBeenCalled()
  })

  it('resolves order by resume token when local recovery snapshot is missing', async () => {
    routeState.query = {
      resume_token: 'resume-77',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: orderFactory('PAID'),
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(resolveOrderPublicByResumeToken).toHaveBeenCalledWith('resume-77')
    expect(wrapper.text()).toContain('payment.result.success')
  })

  it('uses the currency returned by the order API when rendering amounts', async () => {
    routeState.query = {
      resume_token: 'resume-hkd',
    }
    resolveOrderPublicByResumeToken.mockResolvedValue({
      data: {
        ...orderFactory('PAID'),
        currency: 'HKD',
        amount: 100,
        pay_amount: 103,
        fee_rate: 3,
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain(formatPaymentAmount(103, 'HKD'))
  })

  it('normalizes aliased payment methods before rendering the label', async () => {
    routeState.query = {
      resume_token: 'resume-88',
    }
    resolveOrderPublicByResumeToken.mockResolvedValueOnce({
      data: {
        ...orderFactory('PAID'),
        payment_type: 'alipay_direct',
      },
    })

    const wrapper = mount(PaymentResultView, {
      global: {
        stubs: {
          OrderStatusBadge: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.methods.alipay')
    expect(wrapper.text()).not.toContain('payment.methods.alipay_direct')
  })
})


describe.each(['gelv', 'gelv_alipay'])('%s persisted result polling', (paymentType) => {
  const owned = (status: string) => ({ ...orderFactory(status), payment_type: paymentType, refund_reconciliation_pending: true })
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllTimers()
    routeState.query = { order_id: '42' }
    pollOrderStatus.mockReset().mockResolvedValue(owned('PENDING'))
    verifyOrder.mockReset()
    verifyOrderPublic.mockReset()
    resolveOrderPublicByResumeToken.mockReset()
    refreshUser.mockReset().mockResolvedValue({})
    window.localStorage.clear()
  })
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

  it('keeps held orders polling at two seconds beyond the old attempt limit, and credits only COMPLETED', async () => {
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({ ...recoverySnapshotFactory('held'), paymentType }))
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1999)
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(32000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(18)
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(refreshUser).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).not.toBeNull()
    pollOrderStatus.mockResolvedValueOnce(owned('PAID')).mockResolvedValueOnce(owned('RECHARGING')).mockResolvedValueOnce(owned('COMPLETED'))
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(refreshUser).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).not.toContain('payment.result.success')
    expect(refreshUser).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).not.toBeNull()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    expect(verifyOrder).not.toHaveBeenCalled()
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(resolveOrderPublicByResumeToken).not.toHaveBeenCalled()
    const completedReads = pollOrderStatus.mock.calls.length
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(6000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(completedReads)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('refreshes only the owned order on return, serializes slow reads, and never opens checkout', async () => {
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    let resolveRead!: (value: ReturnType<typeof owned>) => void
    pollOrderStatus.mockReturnValueOnce(new Promise(resolve => { resolveRead = resolve }))
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(false)
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    expect(pollOrderStatus).toHaveBeenLastCalledWith(42)
    await vi.advanceTimersByTimeAsync(6000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    resolveRead(owned('PENDING'))
    await flushPromises()
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(1999)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(pollOrderStatus).toHaveBeenCalledTimes(3)
    expect(open).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
    expect(verifyOrder).not.toHaveBeenCalled()
    expect(verifyOrderPublic).not.toHaveBeenCalled()
    expect(resolveOrderPublicByResumeToken).not.toHaveBeenCalled()
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not apply a late response, clear recovery, refresh credit, or restart polling after unmount', async () => {
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({ ...recoverySnapshotFactory('late'), paymentType }))
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    let resolveRead!: (value: ReturnType<typeof owned>) => void
    pollOrderStatus.mockReturnValueOnce(new Promise(resolve => { resolveRead = resolve }))
    await vi.advanceTimersByTimeAsync(2000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    window.dispatchEvent(new Event('focus'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    resolveRead(owned('COMPLETED'))
    await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
    expect(refreshUser).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).not.toBeNull()
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(10000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
  })

  it('does not add a focus read while the initial owned lookup is pending', async () => {
    let resolveRead!: (value: ReturnType<typeof owned>) => void
    pollOrderStatus.mockReturnValueOnce(new Promise(resolve => { resolveRead = resolve }))
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    resolveRead(owned('COMPLETED'))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(refreshUser).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('non-Gelv result polling compatibility', () => {
  it('keeps the existing two-second cadence and fifteen-attempt limit without focus reads', async () => {
    vi.useFakeTimers()
    vi.clearAllTimers()
    routeState.query = { order_id: '42' }
    pollOrderStatus.mockReset().mockResolvedValue(orderFactory('PENDING'))
    const wrapper = mount(PaymentResultView, { global: { stubs: { OrderStatusBadge: true } } })
    await flushPromises()
    window.dispatchEvent(new Event('focus'))
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1999)
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(pollOrderStatus).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(40000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(16)
    wrapper.unmount()
    vi.useRealTimers()
  })
})
