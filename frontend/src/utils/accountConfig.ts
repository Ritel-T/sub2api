import { validateAccountOrigin } from './accountOrigin'

export const accountOrigin = validateAccountOrigin(import.meta.env.VITE_RITEL_ACCOUNT_ORIGIN)
export const centralAccountEnabled = accountOrigin !== null
export const consoleOrigin = validateAccountOrigin(import.meta.env.VITE_RITEL_CONSOLE_ORIGIN || 'https://console.ritel.si')!

export function accountPageURL(section: 'profile' | 'security' | 'sessions' = 'profile'): string {
  if (!accountOrigin) return '/profile'
  return `${accountOrigin}/account/#${section}`
}

/** Legacy consoles also expose the explicit central reset recovery link. */
export function accountReauthenticationURL(): string {
  return (accountOrigin ?? 'https://account.ritel.si') + '/account/#reauth'
}

/** The embedded bundle is served on legacy panel aliases too. Move documents
 * to the canonical UI before auth initialization; API/Key traffic is untouched. */
export function redirectConsoleAlias(): boolean {
  if (!centralAccountEnabled || location.origin === consoleOrigin) return false
  const target = new URL(consoleOrigin)
  target.pathname = location.pathname
  target.search = location.search
  target.hash = location.hash
  for (const key of ['access_token','refresh_token','id_token']) target.searchParams.delete(key)
  location.replace(target.href)
  return true
}
