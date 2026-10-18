import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { BRAND_LOGO } from '@/utils/branding'

const dir = dirname(fileURLToPath(import.meta.url))
const sidebarSource = readFileSync(resolve(dir, '../AppSidebar.vue'), 'utf8')
const homeViewSource = readFileSync(resolve(dir, '../../../views/HomeView.vue'), 'utf8')
const authSource = readFileSync(resolve(dir, '../AuthLayout.vue'), 'utf8')
const keyUsageViewSource = readFileSync(resolve(dir, '../../../views/KeyUsageView.vue'), 'utf8')

describe('site_logo sanitization and fixed brand boundary', () => {
  it('uses a same-origin selected asset with no executable or remote URL', () => {
    expect(BRAND_LOGO).toBe('/brand/assets/ritel-r.svg')
    expect(new URL(BRAND_LOGO, 'https://dash.ritel.si').origin).toBe('https://dash.ritel.si')
  })

  it('does not bind configurable logo URLs in shared branding surfaces', () => {
    for (const source of [sidebarSource, homeViewSource, authSource]) {
      expect(source).toContain('BRAND_LOGO')
      expect(source).not.toContain('appStore.siteLogo')
      expect(source).not.toContain('?.site_logo')
    }
  })

  it('pins KeyUsageView to the selected safe asset instead of configurable image URLs', () => {
    expect(keyUsageViewSource).toContain('BRAND_LOGO')
    expect(keyUsageViewSource).not.toContain('appStore.siteLogo')
    expect(keyUsageViewSource).not.toContain('?.site_logo')
  })
})
