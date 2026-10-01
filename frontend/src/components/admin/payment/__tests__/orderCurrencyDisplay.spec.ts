import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { PaymentOrder, RetailQuote } from '@/types/payment'
import AdminOrderDetail from '../AdminOrderDetail.vue'
import AdminOrderTable from '../AdminOrderTable.vue'
import AdminRefundDialog from '../AdminRefundDialog.vue'
import OrderTable from '@/components/payment/OrderTable.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-pay_amount" :value="row.pay_amount" :row="row" />
      </div>
    </div>
  `,
}

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 1,
    user_id: 10,
    amount: 100,
    pay_amount: 108,
    currency: 'USD',
    fee_rate: 8,
    payment_type: 'stripe',
    out_trade_no: 'sub2_202606250001',
    status: 'COMPLETED',
    order_type: 'subscription',
    created_at: '2026-06-25T10:00:00Z',
    expires_at: '2026-06-25T10:30:00Z',
    refund_amount: 25,
    ...overrides,
  }
}

describe('admin order currency display', () => {
  it('uses order currency for paid/base/fee amounts and USD for credited/refund amounts', () => {
    const wrapper = mount(AdminOrderDetail, {
      props: {
        show: true,
        order: orderFactory({ currency: 'CNY' }),
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('¥100.00')
    expect(text).toContain('¥8.00')
    expect(text).toContain('¥108.00')
    expect(text).toContain('$100.00')
    expect(text).toContain('$25.00')
  })

  it('uses order currency for pay_amount and USD for refundable balance amounts', () => {
    const wrapper = mount(AdminRefundDialog, {
      props: {
        show: true,
        order: orderFactory({
          currency: 'USD',
          status: 'PARTIALLY_REFUNDED',
          refund_amount: 20,
        }),
        userBalance: 200,
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$108.00')
    expect(text).toContain('$100.00')
    expect(text).toContain('$20.00')
    expect(text).toContain('$80.00')
    expect(text).toContain('$200.00')
  })

  it('renders payment currency consistently in the shared order table', () => {
    const wrapper = mount(OrderTable, {
      props: {
        orders: [
          orderFactory({ id: 1, currency: 'USD', amount: 100, pay_amount: 108 }),
          orderFactory({ id: 2, currency: 'CNY', amount: 100, pay_amount: 108 }),
        ],
        loading: false,
        showUser: true,
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          OrderStatusBadge: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$108.00')
    expect(text).toContain('¥108.00')
    expect(text).toContain('$100.00')
  })

  it('renders payment currency consistently in the admin order table', () => {
    const wrapper = mount(AdminOrderTable, {
      props: {
        orders: [
          orderFactory({ id: 1, currency: 'USD', amount: 100, pay_amount: 108 }),
          orderFactory({ id: 2, currency: 'CNY', amount: 100, pay_amount: 108 }),
        ],
        loading: false,
        page: 1,
        pageSize: 20,
        total: 2,
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          Icon: true,
          Pagination: true,
          Select: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$108.00')
    expect(text).toContain('¥108.00')
    expect(text).toContain('$100.00')
  })
})

const retailQuote: RetailQuote = {
  credited_amount_usd: 10,
  base_amount_gbp: 7.47,
  included_cost_gbp: 0.41,
  total_amount_gbp: 7.88,
  pay_amount: 75.65,
  currency: 'CNY',
  fx: { GBP: 1, USD: 1.34, CNY: 9.6 },
  fx_source: 'manual',
  fx_asof: '2026-09-30T00:00:00Z',
  issued_at: '2026-09-30T00:00:00Z',
  expires_at: '2026-09-30T00:15:00Z',
  checkout_reference: 'RXT-TEST',
}

describe('admin immutable retail order amounts', () => {
  const order = orderFactory({
    amount: 10,
    pay_amount: 75.65,
    currency: 'CNY',
    fee_rate: 88,
    order_type: 'balance',
    refund_amount: 0,
    retail_quote: retailQuote,
  })

  it('shows the stored USD credit, GBP total, included cost, and gateway amount without legacy fee inference', () => {
    const wrapper = mount(AdminOrderDetail, {
      props: { show: true, order },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    const text = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(text).toContain('$10.00 USD')
    expect(text).toContain('£7.88 GBP')
    expect(text).toContain('£0.41 GBP')
    expect(text).toContain('¥75.65 CNY')
    expect(wrapper.text()).not.toContain('payment.orders.baseAmount')
    expect(wrapper.text()).not.toContain('88%')
    expect(wrapper.text()).not.toContain('payment.orders.fee')
    wrapper.unmount()
  })

  it('labels new CNY-based site credit separately from cash principal and gateway currency', () => {
    const wrapper = mount(AdminOrderDetail, {
      props: { show: true, order: { ...order, retail_quote: { ...retailQuote, pricing_basis_currency: 'CNY', base_amount_cny: 10, base_amount_gbp: 1.05, total_amount_gbp: 1.36, included_cost_gbp: 0.31, pay_amount: 13.06 } } },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    const text = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(text).toContain('squarespaceProvider.order.creditedSite')
    expect(text).toContain('squarespaceProvider.order.principalCNY')
    expect(text).toContain('$10.00')
    expect(text).not.toContain('$10.00 USD')
    expect(text).toContain('¥10.00 CNY')
    expect(text).toContain('£1.36 GBP')
    expect(text).toContain('¥13.06 CNY')
    wrapper.unmount()
  })

  it('shows CNY principal and site credit units in the shared user order table', () => {
    const wrapper = mount(OrderTable, {
      props: { orders: [{ ...order, currency: 'GBP', pay_amount: 1.36, retail_quote: { ...retailQuote, pricing_basis_currency: 'CNY', base_amount_cny: 10, base_amount_gbp: 1.05, total_amount_gbp: 1.36, included_cost_gbp: 0.31, pay_amount: 1.36, currency: 'GBP' } }], loading: false },
      global: { stubs: { DataTable: DataTableStub, OrderStatusBadge: true } },
    })
    const text = wrapper.text()
    expect(text).toContain('paymentRetail.siteCredit')
    expect(text).toContain('paymentRetail.principalCNY')
    expect(text).toContain('$10.00')
    expect(text).not.toContain('$10.00 USD')
    expect(text).toContain('¥10.00 CNY')
    expect(text).toContain('£1.36 GBP')
    wrapper.unmount()
  })

  it('shows all four immutable snapshot values in the admin table for any payment method', () => {
    const wrapper = mount(AdminOrderTable, {
      props: { orders: [order], loading: false, page: 1, pageSize: 20, total: 1 },
      global: { stubs: { DataTable: DataTableStub, Icon: true, Pagination: true, Select: true } },
    })
    const text = wrapper.get('[data-testid="admin-retail-order-amounts"]').text()
    expect(text).toContain('$10.00 USD')
    expect(text).toContain('£7.88 GBP')
    expect(text).toContain('£0.41 GBP')
    expect(text).toContain('¥75.65 CNY')
    expect(wrapper.text()).not.toContain('88%')
    wrapper.unmount()
  })
})
