// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { injectBranding } from '../../../build/firstPaintBranding'

const fixedHTML = '<head><meta name="ritel-brand" content="fixed" /><title>Ritel.SI</title><link rel="icon" href="/brand/assets/ritel-favicon.svg" /></head>'
const legacyHTML = '<head><title>Sub2API</title><link rel="icon" href="/logo.svg" /></head>'

describe('first paint brand boundary', () => {
  it('preserves the selected title and favicon before JavaScript executes', () => {
    expect(injectBranding(fixedHTML, { site_name: 'Old brand', site_logo: 'https://example.com/old.png' })).toBe(fixedHTML)
  })

  it('does not insert executable image URLs or title markup into a fixed page', () => {
    expect(injectBranding(fixedHTML, { site_name: '</title><script>alert(1)</script>', site_logo: 'javascript:alert(1)' })).toBe(fixedHTML)
  })

  it('retains HTML escaping for unmarked generic deployments', () => {
    const result = injectBranding(legacyHTML, { site_name: '</title><script>alert(1)</script>' })
    expect(result).not.toContain('<script>')
    expect(result).toContain('&lt;script&gt;')
  })

  it('rejects executable logo URLs in unmarked deployments', () => {
    const result = injectBranding(legacyHTML, { site_logo: 'javascript:alert(1)' })
    expect(result).toContain('href="/logo.svg"')
    expect(result).not.toContain('javascript:')
  })

  it('escapes accepted logo query parameters in unmarked deployments', () => {
    expect(injectBranding(legacyHTML, { site_logo: 'https://example.com/logo.svg?a=1&b=2' })).toContain('a=1&amp;b=2')
  })
})
