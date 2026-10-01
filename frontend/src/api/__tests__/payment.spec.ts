import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { paymentAPI } from '@/api/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('requests a server-priced USD balance quote without computing a gateway surcharge', async () => {
    const payload = { amount: 10, payment_type: 'squarespace', order_type: 'balance' as const }
    await paymentAPI.quote(payload)
    expect(post).toHaveBeenCalledWith('/payment/quote', payload)
  })

  it('uses a bound receipt challenge and verifies only the opaque challenge plus code', async () => {
    await paymentAPI.requestSquarespaceClaimCode(42, '#1234', 'actual-payer@example.com')
    expect(post).toHaveBeenCalledWith('/payment/squarespace/claim-challenge', { local_order_id: 42, receipt_order_number: '#1234', payer_email: 'actual-payer@example.com' })
    await paymentAPI.claimSquarespacePayment('bound-token', '123456')
    expect(post).toHaveBeenCalledWith('/payment/squarespace/claim', { challenge_token: 'bound-token', code: '123456' })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    })
  })
})
