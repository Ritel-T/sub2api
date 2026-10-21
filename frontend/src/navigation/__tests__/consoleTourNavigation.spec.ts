import { defineComponent, h, reactive, computed } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { provideConsoleNav, type ConsoleNavSource } from '../useConsoleNavSource'

let auth: { user: { id: number; role: string }; isAdmin: boolean; isObserver: boolean; isSimpleMode: boolean }
let app: { mobileOpen: boolean; backendModeEnabled: boolean; cachedPublicSettings: Record<string, unknown>; setMobileOpen: (value: boolean) => void }
const route = reactive({ path: '/admin/dashboard', query: {}, meta: {}, params: {}, name: 'Dashboard' })
const mocks = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('@/stores', () => ({
  useAuthStore: () => auth, useAppStore: () => app,
  useAdminSettingsStore: () => ({ customMenuItems: [], fetch: vi.fn() }),
  useSupportTicketStore: () => ({ userUnread: 0, adminPending: 0 })
}))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: computed(() => false), refreshBatchImageAccess: vi.fn() }) }))
vi.mock('@/composables/useMerchantTestPaymentAccess', () => ({ useMerchantTestPaymentAccess: () => ({ merchantTestPaymentAccess: computed(() => false) }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: {}, resolveFeatureFlag: () => false, makeSidebarFlag: () => () => false }))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: mocks.push }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }), createI18n: () => ({}) }))

describe('real console tour navigation surfaces', () => {
  let nav: ConsoleNavSource
  let wrapper: ReturnType<typeof mount>
  function setup() {
    wrapper = mount(defineComponent({ setup() {
      nav = provideConsoleNav()
      return () => h('div', [
        h('div', { class: 'tabs' }, (nav.activeLocation.value?.section.tabs ?? []).length > 1 ? nav.activeLocation.value?.section.tabs.map(tab => h('a', { href: tab.path, id: nav.tourId(tab.path, 'tabs'), 'data-tour': nav.tourAttr(tab.path, 'tabs') }, tab.label)) : []),
        h('div', { class: 'console-drawer' }, nav.sections.value.flatMap(section => section.tabs.map(tab => h('a', { href: tab.path, id: nav.tourId(tab.path, 'drawer'), 'data-tour': nav.tourAttr(tab.path, 'drawer') }, tab.label))))
      ])
    } }), { attachTo: document.body })
  }
  beforeEach(() => {
    Object.defineProperty(window, 'innerWidth', { value: 1280, writable: true, configurable: true })
    auth = reactive({ user: { id: 41, role: 'admin' }, isAdmin: true, isObserver: false, isSimpleMode: false })
    app = reactive({ mobileOpen: false, backendModeEnabled: false, cachedPublicSettings: {}, setMobileOpen(value: boolean) { this.mobileOpen = value } })
    route.path = '/admin/dashboard'
    mocks.push.mockReset().mockImplementation(async (path: string) => { route.path = path })
  })
  afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
  it('enters the real destination section and anchors the desktop tab once', async () => {
    setup()
    expect(await nav.prepareTourTarget('#sidebar-group-manage')).toBe(true)
    expect(route.path).toBe('/admin/accounts')
    expect(app.mobileOpen).toBe(false)
    expect(document.querySelectorAll('#sidebar-group-manage')).toHaveLength(1)
    expect(document.querySelector('#sidebar-group-manage')?.closest('.tabs')).not.toBeNull()
  })
  it('keeps personal keys out of management resources and enters the personal workspace for its tour', async () => {
    route.path = '/admin/accounts'
    setup()
    expect(nav.sections.value.flatMap(section => section.tabs).some(tab => tab.path === '/keys')).toBe(false)
    expect(await nav.prepareTourTarget('[data-tour="sidebar-my-keys"]')).toBe(true)
    expect(route.path).toBe('/keys')
    expect(document.querySelectorAll('[data-tour="sidebar-my-keys"]')).toHaveLength(1)
  })
  it('opens the mobile drawer with one real destination anchor', async () => {
    window.innerWidth = 390
    setup()
    expect(await nav.prepareTourTarget('#sidebar-group-manage')).toBe(true)
    expect(app.mobileOpen).toBe(true)
    expect(document.querySelectorAll('#sidebar-group-manage')).toHaveLength(1)
    expect(document.querySelector('#sidebar-group-manage')?.closest('.console-drawer')).not.toBeNull()
  })
  it('uses the drawer for a single-page section even on desktop', async () => {
    auth.isAdmin = false
    auth.user.role = 'user'
    route.path = '/dashboard'
    setup()
    expect(await nav.prepareTourTarget('[data-tour="sidebar-my-keys"]')).toBe(true)
    expect(app.mobileOpen).toBe(true)
    expect(document.querySelectorAll('[data-tour="sidebar-my-keys"]')).toHaveLength(1)
    expect(document.querySelector('[data-tour="sidebar-my-keys"]')?.closest('.console-drawer')).not.toBeNull()
  })
  it('cannot prepare an administrator destination for a regular user', async () => {
    auth.isAdmin = false
    auth.user.role = 'user'
    route.path = '/dashboard'
    setup()
    expect(await nav.prepareTourTarget('#sidebar-group-manage')).toBe(false)
    expect(mocks.push).not.toHaveBeenCalled()
    expect(app.mobileOpen).toBe(false)
  })
  it('closes only a drawer automatically opened by the tour on finish', async () => {
    window.innerWidth = 390
    setup()
    await nav.prepareTourTarget('#sidebar-group-manage')
    expect(app.mobileOpen).toBe(true)
    nav.finishTour()
    expect(app.mobileOpen).toBe(false)
  })
  it.each([390, 1280])('preserves a drawer already open before the tour began at %ipx', async width => {
    window.innerWidth = width
    app.mobileOpen = true
    setup()
    await nav.prepareTourTarget('#sidebar-group-manage')
    nav.finishTour()
    expect(app.mobileOpen).toBe(true)
  })

})
