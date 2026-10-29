import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const componentPath = resolve(here, '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const navSourcePath = resolve(here, '../../../navigation/useConsoleNavSource.ts')
const navSource = readFileSync(navSourcePath, 'utf8')
const stylePath = resolve(here, '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar subscription feature flag', () => {
  it('gates the My Subscriptions entry behind the subscription public-settings flag', () => {
    expect(navSource).toContain('const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)')
    expect(navSource).toMatch(/path: '\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('also hides the admin Subscription Management entry on recharge-only sites', () => {
    expect(navSource).toMatch(/path: '\/admin\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('derives the purchase entry label from the site billing mode', () => {
    expect(navSource).toContain("import { resolveSiteBillingMode } from '@/utils/siteBillingMode'")
    expect(navSource).toMatch(/case 'recharge_only':\s*return t\('nav\.recharge'\)/)
    expect(navSource).toMatch(/case 'subscription_only':\s*return t\('nav\.subscribe'\)/)
    expect(navSource).toMatch(/path: '\/purchase'[^\n]*label: purchaseNavLabel\.value/)
  })
})

describe('AppSidebar smart operations group', () => {
  const smartOpsBlock = navSource.match(/path: '\/admin\/smart-ops'[\s\S]*?\n {6}\] \},/)?.[0] ?? ''
  const pathsIn = (source: string) => [...source.matchAll(/path: '([^']+)'/g)].map(match => match[1])

  it('nests request capture and the ticket harvest flow under 智能运维', () => {
    expect(smartOpsBlock).not.toBe('')
    expect(smartOpsBlock).toContain("path: '/admin/profit-operations'")
    expect(navSource.match(/path: '\/admin\/profit-operations'/g)).toHaveLength(1)
    expect(smartOpsBlock).toMatch(/path: '\/admin\/request-captures'[^\n]*featureFlag: \(\) => adminSettingsStore\.requestCaptureEnabled/)
    expect(smartOpsBlock).toContain("path: '/admin/harvest-flow'")
    expect(navSource.match(/path: '\/admin\/request-captures'/g)).toHaveLength(1)
    expect(navSource.match(/path: '\/admin\/harvest-flow'/g)).toHaveLength(1)
  })

  it('keeps the group in the same order as the 智能运维 tab bar', () => {
    const navComponentSource = readFileSync(resolve(here, '../../../components/admin/operations/SmartOpsNav.vue'), 'utf8')
    expect(pathsIn(smartOpsBlock).slice(1)).toEqual(pathsIn(navComponentSource))
  })
})

describe('AppSidebar support tickets', () => {
  it('gates both entries behind the opt-in switch and keeps admins on their own page', () => {
    expect(navSource).toContain('const flagSupportTickets = makeSidebarFlag(FeatureFlags.supportTickets)')
    expect(navSource).toContain('const flagUserSupportTickets = () => flagSupportTickets() && !authStore.isAdmin')
    expect(navSource).toMatch(/path: '\/support-tickets'[^\n]*featureFlag: flagUserSupportTickets[^\n]*badge: \(\) => supportTicketStore\.userUnread/)
    expect(navSource).toMatch(/path: '\/admin\/support-tickets'[^\n]*featureFlag: flagSupportTickets[^\n]*badge: \(\) => supportTicketStore\.adminPending/)
  })

  it('renders badges for top-level and grouped destinations and caps the number', () => {
    expect(componentSource.match(/data-testid="sidebar-nav-badge"/g)).toHaveLength(1)
    expect(componentSource).toContain('formatNavBadge(tabBadge(tab))')
    expect(componentSource).toContain('.sidebar-nav-badge {')
  })
})
