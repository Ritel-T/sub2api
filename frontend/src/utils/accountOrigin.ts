/** A configured value must be an exact origin, never a URL prefix or fallback. */
export function validateAccountOrigin(value: string | undefined): string | null {
  if (value === undefined || value === '') return null
  const url = new URL(value)
  if (url.protocol !== 'https:' || url.origin !== value || url.username || url.password) {
    throw new Error('VITE_RITEL_ACCOUNT_ORIGIN must be an exact HTTPS origin without path, query or fragment')
  }
  return value
}
