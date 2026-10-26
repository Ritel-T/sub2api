import { validateAccountOrigin } from './accountOrigin'

export const accountOrigin = validateAccountOrigin(import.meta.env.VITE_RITEL_ACCOUNT_ORIGIN)
export const centralAccountEnabled = accountOrigin !== null

export function accountPageURL(section: 'profile' | 'security' | 'sessions' = 'profile'): string {
  if (!accountOrigin) return '/profile'
  return `${accountOrigin}/account/#${section}`
}

/** Legacy consoles also expose the explicit central reset recovery link. */
export function accountReauthenticationURL(): string {
  return (accountOrigin ?? 'https://account.ritel.si') + '/account/#reauth'
}
