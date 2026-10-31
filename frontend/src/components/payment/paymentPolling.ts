// Read the persisted order only: settlement freshness comes from the backend worker.
export const GELV_ORDER_POLL_INTERVAL_MS = 2000

export function isGelvPaymentType(paymentType: string | null | undefined): boolean {
  return paymentType === 'gelv' || paymentType === 'gelv_alipay'
}

export function paymentPollInterval(paymentType: string | null | undefined, defaultInterval: number): number {
  return isGelvPaymentType(paymentType) ? GELV_ORDER_POLL_INTERVAL_MS : defaultInterval
}

/** Returning to a waiting checkout refreshes status; it never relaunches payment. */
export function onPaymentPageReturn(refresh: () => void): () => void {
  const onReturn = () => {
    if (!document.hidden) refresh()
  }
  window.addEventListener('focus', onReturn)
  document.addEventListener('visibilitychange', onReturn)
  return () => {
    window.removeEventListener('focus', onReturn)
    document.removeEventListener('visibilitychange', onReturn)
  }
}
