import { beforeEach, describe, expect, it } from 'vitest'
import { splitBrandName, updateFavicon } from '@/utils/branding'

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.svg">'
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.svg')
  })
})

describe('family wordmark and product name', () => {
  it('keeps mixed case family identity separate from the configured product', () => {
    expect(splitBrandName('Ritel.SI API')).toEqual({ brand: 'Ritel.SI', suffix: ' API' })
    expect(splitBrandName('Ritel.SI')).toEqual({ brand: 'Ritel.SI', suffix: '' })
    expect(splitBrandName('Ritel.SI 账号')).toEqual({ brand: 'Ritel.SI', suffix: ' 账号' })
  })
  it('preserves arbitrary configured names without inventing a family prefix', () => {
    expect(splitBrandName('Updated API Console')).toEqual({ brand: '', suffix: 'Updated API Console' })
    expect(splitBrandName('Ritel.SIcustom')).toEqual({ brand: '', suffix: 'Ritel.SIcustom' })
  })
})
