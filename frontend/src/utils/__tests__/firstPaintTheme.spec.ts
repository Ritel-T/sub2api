// @vitest-environment node
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { runInNewContext } from 'node:vm'
import { describe, expect, it } from 'vitest'

const html = readFileSync(resolve(process.cwd(), 'index.html'), 'utf8')
const bootstrap = html.match(/<script nonce="__CSP_NONCE_VALUE__">([\s\S]*?)<\/script>/)?.[1]

describe('first paint theme', () => {
  it('uses the existing server CSP nonce placeholder for the first-paint script', () => {
    expect(bootstrap).toBeTruthy()
    expect(html).toContain('<script nonce="__CSP_NONCE_VALUE__">')
  })
  it.each([
    ['dark', false, true], ['light', true, false], ['system', true, true],
    [null, false, false], ['unexpected', true, true],
  ])('resolves preference %s and dark system %s before loading styles', (saved, systemDark, expectedDark) => {
    const root = { classList: { toggle: (_key: string, value: boolean) => { applied = value } }, style: { colorScheme: '' } }
    let applied = false
    const meta = { content: '' }
    runInNewContext(bootstrap!, {
      localStorage: { getItem: () => saved },
      window: { matchMedia: () => ({ matches: systemDark }) },
      document: { documentElement: root, querySelector: () => meta },
    })
    expect(applied).toBe(expectedDark)
    expect(root.style.colorScheme).toBe(expectedDark ? 'dark' : 'light')
    expect(meta.content).toBe(expectedDark ? '#1b1b19' : '#f5f3ee')
    expect(html.indexOf(bootstrap!)).toBeLessThan(html.indexOf('rel="stylesheet"'))
  })

  it('supports privacy modes that deny local storage', () => {
    let applied = false
    runInNewContext(bootstrap!, {
      localStorage: { getItem: () => { throw new Error('Blocked') } },
      window: { matchMedia: () => ({ matches: true }) },
      document: { documentElement: { classList: { toggle: (_key: string, value: boolean) => { applied = value } }, style: {} }, querySelector: () => ({}) },
    })
    expect(applied).toBe(true)
  })
})
