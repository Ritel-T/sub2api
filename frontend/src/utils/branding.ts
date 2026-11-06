import { sanitizeUrl } from '@/utils/url'

export const BRAND_NAME = 'Ritel.SI'
export const BRAND_LOGO = '/brand/assets/ritel-r.svg'
export const BRAND_FAVICON = '/brand/assets/ritel-favicon.svg'

// Only the family name is a Cinzel wordmark; configured product names use display type.
export function splitBrandName(name: string): { brand: string; suffix: string } {
  return /^Ritel\.SI(?=\s|$)/.test(name)
    ? { brand: BRAND_NAME, suffix: name.slice(BRAND_NAME.length) }
    : { brand: '', suffix: name }
}

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (!sanitizedLogoUrl) {
    return
  }

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  link.type = sanitizedLogoUrl.endsWith('.svg') ? 'image/svg+xml' : 'image/x-icon'
  link.href = sanitizedLogoUrl
}
