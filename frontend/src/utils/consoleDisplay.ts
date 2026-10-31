/** Display only; API/search/clipboard identities retain their stored value. */
export function displayOrderNumber(value?: string | null): string {
  return (value || '').replace(/^sub2_/, '')
}

export function formatAmount(value: number | null | undefined, digits = 2): string {
  return new Intl.NumberFormat('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value ?? 0)
}

export function formatMoney(value: number | null | undefined, currency = 'USD', digits = 2): string {
  const code = currency.toUpperCase() === 'RMB' ? 'CNY' : currency.toUpperCase()
  if (code === 'USD' || code === 'CNY') return `${code === 'USD' ? '$' : '¥'}${formatAmount(value, digits)}`
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: code, currencyDisplay: 'symbol', minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value ?? 0)
}

export function browserTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

export function formatTimestamp(value: string | Date | number | null | undefined, dateOnly = false): string {
  if (value === null || value === undefined || value === '') return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  const day = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
  return dateOnly ? day : `${day} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
