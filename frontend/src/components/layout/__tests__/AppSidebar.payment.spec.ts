import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { computed, nextTick, reactive } from 'vue'
import AppSidebar from '../AppSidebar.vue'
import type { CustomMenuItem } from '@/types'
import type { ConsoleNavSource } from '@/navigation/useConsoleNavSource'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), fetchAdminSettings: vi.fn(), push: vi.fn() }))

interface AuthState {
  user: { id: number; role: string } | null
  token: string | null
  readonly isAuthenticated: boolean
  readonly isAdmin: boolean
  readonly isObserver: boolean
  isSimpleMode: boolean
}

let authState: AuthState
let tourActive = false
let navigation: ConsoleNavSource | undefined
let appState: {
  cachedPublicSettings: { payment_enabled?: boolean; subscription_enabled?: boolean; payment_balance_disabled?: boolean; custom_menu_items?: CustomMenuItem[] }
  mobileOpen: boolean
  backendModeEnabled: boolean
  docUrl: string
  siteVersion: string
  setMobileOpen: (value: boolean) => void
}
let adminState: { paymentEnabled: boolean; customMenuItems: CustomMenuItem[]; fetch: typeof mocks.fetchAdminSettings }
const route = reactive({ path: '/admin/dashboard', query: {} as Record<string, string> })
vi.mock('@/navigation/useConsoleNavSource', async importOriginal => {
  const actual = await importOriginal<typeof import('@/navigation/useConsoleNavSource')>()
  return { ...actual, useConsoleNav: () => { navigation = actual.useConsoleNav(); return navigation } }
})

vi.mock('@/api/payment', () => ({ paymentAPI: { getConfig: mocks.getConfig } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appState }))
vi.mock('@/stores', () => ({
  useAuthStore: () => authState,
  useAppStore: () => appState,
  useAdminSettingsStore: () => adminState,
  useOnboardingStore: () => ({ isCurrentStep: () => false, isDriverActive: () => tourActive }),
  useSupportTicketStore: () => ({ userUnread: 0, adminPending: 0 }),
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: computed(() => false), refreshBatchImageAccess: vi.fn() }),
}))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: mocks.push }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }), createI18n: () => ({}) }))
vi.mock('@/components/common/VersionBadge.vue', () => ({ default: { template: '<span />' } }))

let wrapper: VueWrapper | undefined

function mountSidebar() {
  wrapper = shallowMount(AppSidebar, {
    attachTo: document.body,
    global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
  })
  return wrapper
}

interface TestNavItem { path: string; label: string; children?: TestNavItem[] }
function tree(name: 'userNavItems' | 'adminNavItems'): TestNavItem[] {
  return navigation?.[name].value ?? []
}
function hasLink(path: string): boolean {
  return tree('userNavItems').some(item => item.path === path)
}

function hasAdminPayments(): boolean {
  return authState.isAdmin && tree('adminNavItems').some(item => item.path === '/admin/orders')
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

beforeEach(() => {
  tourActive = false
  navigation = undefined
  vi.clearAllMocks()
  Object.defineProperty(window, 'innerWidth', { value: 390, writable: true, configurable: true })
  sessionStorage.clear()
  vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
  mocks.getConfig.mockReset().mockResolvedValue({ data: { enabled: false, merchant_test_access: false } })
  authState = reactive({
    user: { id: 41, role: 'admin' }, token: 'session-41',
    get isAuthenticated() { return !!this.token && !!this.user },
    get isAdmin() { return this.user?.role === 'admin' },
    get isObserver() { return this.user?.role === 'observer' },
    isSimpleMode: false,
  })
  appState = reactive({
    cachedPublicSettings: { payment_enabled: false, subscription_enabled: false, payment_balance_disabled: false },
    mobileOpen: false,
    setMobileOpen(value: boolean) { this.mobileOpen = value },
    backendModeEnabled: false, docUrl: '', siteVersion: 'test',
  })
  adminState = reactive({ paymentEnabled: false, customMenuItems: [], fetch: mocks.fetchAdminSettings })
  route.path = '/admin/dashboard'
  route.query = {}
})

afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks() })

describe('AppSidebar merchant payment pilot', () => {
  it('shows recharge, owned orders, and admin payments only after fresh explicit protected authorization', async () => {
    const pending = deferred<{ data: unknown }>()
    mocks.getConfig.mockReturnValue(pending.promise)
    mountSidebar()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasLink('/orders')).toBe(false)
    expect(hasAdminPayments()).toBe(false)
    pending.resolve({ data: { enabled: true, merchant_test_access: true } })
    await flushPromises()
    expect(hasLink('/purchase')).toBe(true)
    expect(hasLink('/orders')).toBe(true)
    expect(hasAdminPayments()).toBe(true)
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    expect(tree('userNavItems').find(item => item.path === '/purchase')?.label).toBe('nav.recharge')
  })

  it.each([
    { enabled: true, merchant_test_access: false },
    { enabled: false, merchant_test_access: true },
    { enabled: 'true', merchant_test_access: true },
    { enabled: true, merchant_test_access: 'true' },
    { merchant_test_access: true },
    {}, null, undefined,
  ])('keeps globally closed menus hidden for missing or malformed authorization %j', async data => {
    mocks.getConfig.mockResolvedValue({ data })
    // A previously cached admin flag cannot override the explicit public closure.
    adminState.paymentEnabled = true
    mountSidebar()
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasLink('/orders')).toBe(false)
    expect(hasAdminPayments()).toBe(false)
  })

  it.each([{ status: 401 }, new Error('network unavailable')])('fails closed after protected configuration errors %j', async error => {
    mocks.getConfig.mockRejectedValue(error)
    mountSidebar()
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasLink('/orders')).toBe(false)
    expect(hasAdminPayments()).toBe(false)
  })

  it('keeps public payments working without a merchant configuration request', async () => {
    appState.cachedPublicSettings.payment_enabled = true
    adminState.paymentEnabled = true
    mountSidebar()
    await flushPromises()
    expect(hasLink('/purchase')).toBe(true)
    expect(hasLink('/orders')).toBe(true)
    expect(hasAdminPayments()).toBe(true)
    expect(mocks.getConfig).not.toHaveBeenCalled()
  })

  it('allows an explicitly authorized regular merchant tester without granting admin menus', async () => {
    authState.user = { id: 52, role: 'user' }
    mocks.getConfig.mockResolvedValue({ data: { enabled: true, merchant_test_access: true } })
    mountSidebar()
    await flushPromises()
    expect(hasLink('/purchase')).toBe(true)
    expect(hasLink('/orders')).toBe(true)
    expect(hasAdminPayments()).toBe(false)
  })

  it('clears trial menus on logout without starting an anonymous request', async () => {
    mocks.getConfig.mockResolvedValue({ data: { enabled: true, merchant_test_access: true } })
    mountSidebar()
    await flushPromises()
    expect(hasLink('/purchase')).toBe(true)
    authState.token = null
    authState.user = null
    await nextTick()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasLink('/orders')).toBe(false)
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
  })

  it('rejects a prior account response after switching accounts', async () => {
    const oldAccount = deferred<{ data: unknown }>()
    const newAccount = deferred<{ data: unknown }>()
    mocks.getConfig.mockReturnValueOnce(oldAccount.promise).mockReturnValue(newAccount.promise)
    mountSidebar()
    authState.user = { id: 52, role: 'admin' }
    await nextTick()
    expect(hasLink('/purchase')).toBe(false)
    oldAccount.resolve({ data: { enabled: true, merchant_test_access: true } })
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasAdminPayments()).toBe(false)
    newAccount.resolve({ data: { enabled: false, merchant_test_access: false } })
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    expect(mocks.getConfig).toHaveBeenCalledTimes(2)
  })

  it('clears accepted authorization before awaiting the switched account response', async () => {
    const newAccount = deferred<{ data: unknown }>()
    mocks.getConfig.mockResolvedValueOnce({ data: { enabled: true, merchant_test_access: true } }).mockReturnValue(newAccount.promise)
    mountSidebar()
    await flushPromises()
    expect(hasLink('/purchase')).toBe(true)
    authState.user = { id: 52, role: 'admin' }
    await nextTick()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasAdminPayments()).toBe(false)
  })

  it('invalidates pending access even if logout and login restore the same identity and token in one tick', async () => {
    const oldSession = deferred<{ data: unknown }>()
    const newSession = deferred<{ data: unknown }>()
    mocks.getConfig.mockReturnValueOnce(oldSession.promise).mockReturnValue(newSession.promise)
    mountSidebar()
    authState.token = null
    authState.token = 'session-41'
    oldSession.resolve({ data: { enabled: true, merchant_test_access: true } })
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    newSession.reject({ status: 401 })
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    expect(mocks.getConfig).toHaveBeenCalledTimes(2)
  })

  it('does not query again for route, sidebar, or user profile refreshes with the same identity', async () => {
    mocks.getConfig.mockResolvedValue({ data: { enabled: true, merchant_test_access: true } })
    mountSidebar()
    await flushPromises()
    route.path = '/profile'
    appState.mobileOpen = true
    authState.user = { id: 41, role: 'admin' }
    await flushPromises()
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
  })

  it('revalidates when public payments close and preserves simple mode hiding', async () => {
    appState.cachedPublicSettings.payment_enabled = true
    mountSidebar()
    appState.cachedPublicSettings.payment_enabled = false
    mocks.getConfig.mockResolvedValue({ data: { enabled: true, merchant_test_access: true } })
    // Resolve the request already issued on the closing transition with no access.
    await flushPromises()
    expect(hasLink('/purchase')).toBe(false)
    appState.cachedPublicSettings.payment_enabled = true
    appState.cachedPublicSettings.payment_enabled = false
    await flushPromises()
    expect(hasLink('/purchase')).toBe(true)
    authState.isSimpleMode = true
    await nextTick()
    expect(hasLink('/purchase')).toBe(false)
    expect(hasLink('/orders')).toBe(false)
    expect(hasAdminPayments()).toBe(false)
    expect(mocks.getConfig).toHaveBeenCalledTimes(2)
  })
})

describe('console workspace navigation', () => {
  it('shows one management directory and searches exactly that tree', async () => {
    mountSidebar()
    // Admin workspace: accounts and keys should appear as section tab links.
    expect(wrapper?.find('a[href="/admin/accounts"]').exists()).toBe(true)
    expect(wrapper?.find('a[href="/keys"]').exists()).toBe(true)
    // Profile is hidden from nav (account section is hidden).
    expect(wrapper?.find('a[href="/profile"]').exists()).toBe(false)
    // Workspace switch: aria-pressed buttons inside the group.
    const ws = wrapper!.find('.console-drawer-workspace')
    expect(ws.exists()).toBe(true)
    const btns = ws.findAll('button')
    const mgmt = btns.find(b => b.attributes('aria-pressed') === 'true')
    expect(mgmt).toBeDefined()
    // Click personal button triggers switchWorkspace → router.push.
    const personal = btns.find(b => b.attributes('aria-pressed') === 'false')!
    await personal.trigger('click')
    expect(mocks.push).toHaveBeenCalled()
  })

  it('keeps payment routes hidden behind the feature flag', async () => {
    authState.user = { id: 52, role: 'user' }
    route.path = '/dashboard'
    appState.cachedPublicSettings.payment_enabled = true
    mountSidebar()
    // In user workspace: /purchase appears as a section tab link.
    expect(wrapper?.find('a[href="/purchase"]').exists()).toBe(true)
  })

  it('keeps observer and backend navigation within existing permissions', async () => {
    authState.user = { id: 52, role: 'observer' }
    appState.backendModeEnabled = true
    route.path = '/usage'
    mountSidebar()
    // Observer in backend mode: no workspace switch (single option).
    expect(wrapper?.find('.console-drawer-workspace').exists()).toBe(false)
    expect(wrapper?.find('a[href="/keys"]').exists()).toBe(false)
  })

  it('keeps simple mode admin key access in the management workspace', async () => {
    authState.isSimpleMode = true
    route.path = '/keys'
    mountSidebar()
    expect(wrapper?.find('a[href="/keys"]').exists()).toBe(true)
    expect(wrapper?.find('a[href="/admin/accounts"]').exists()).toBe(true)
    // Simple mode: only management workspace → no workspace switch.
    expect(wrapper?.find('.console-drawer-workspace').exists()).toBe(false)
  })

  it('distinguishes usage analytics and request records using the current query', async () => {
    authState.user = { id: 52, role: 'user' }
    route.path = '/usage'
    route.query = { view: 'analysis' }
    mountSidebar()
    expect(wrapper!.get('a[href="/usage?view=analysis"]').attributes('aria-current')).toBe('page')
    expect(wrapper!.get('a[href="/usage?view=records"]').attributes('aria-current')).toBeUndefined()
    route.query = {}
    await nextTick()
    expect(wrapper!.get('a[href="/usage?view=records"]').attributes('aria-current')).toBe('page')
  })
})

describe('mobile console directory', () => {
  it('shows section tab links when the drawer is open', async () => {
    authState.user = { id: 52, role: 'user' }
    route.path = '/dashboard'
    appState.mobileOpen = true
    mountSidebar()
    // The drawer has the console-drawer class and is not inert when open.
    expect(wrapper!.get('aside').attributes('inert')).toBeUndefined()
    // Links are rendered as section tab router-links.
    expect(wrapper?.find('a[href="/dashboard"]').exists()).toBe(true)
  })
})

describe('workspace session restoration', () => {
  it('restores the last granted destination with structural filters and never stores keys or free text', async () => {
    mountSidebar()
    route.path = '/usage'
    route.query = { view: 'analysis', api_key: 'secret', search: 'private-user', page: '3' }
    await nextTick()
    expect(sessionStorage.getItem('ritel-console:last:41:admin:standard:full:personal')).toBe('/usage?view=analysis&page=3')
    route.path = '/admin/accounts'
    route.query = {}
    await nextTick()
    // Click the personal workspace button.
    const personalBtn = wrapper!.findAll('.console-drawer-workspace button').find(b => b.attributes('aria-pressed') === 'false')!
    await personalBtn.trigger('click')
    expect(mocks.push).toHaveBeenLastCalledWith('/usage?view=analysis&page=3')
  })

  it('rejects a previously stored feature after its public switch is disabled', async () => {
    sessionStorage.setItem('ritel-console:last:41:admin:standard:full:personal', '/purchase')
    mountSidebar()
    const personalBtn = wrapper!.findAll('.console-drawer-workspace button').find(b => b.attributes('aria-pressed') === 'false')!
    await personalBtn.trigger('click')
    expect(mocks.push).toHaveBeenLastCalledWith('/dashboard')
  })

  it('does not copy an outgoing account location into the newly switched account session', async () => {
    mountSidebar()
    route.path = '/profile'
    await nextTick()
    authState.user = { id: 52, role: 'admin' }
    await nextTick()
    expect(sessionStorage.getItem('ritel-console:last:52:admin:standard:full:personal')).toBeNull()
    route.path = '/admin/accounts'
    await nextTick()
    const personalBtn = wrapper!.findAll('.console-drawer-workspace button').find(b => b.attributes('aria-pressed') === 'false')!
    await personalBtn.trigger('click')
    expect(mocks.push).toHaveBeenLastCalledWith('/dashboard')
  })

  it('does not infer management for a custom page that is also in the personal collection', async () => {
    const custom = { id: 'shared', label: 'Shared', icon_svg: '<svg />', url: 'https://example.test', visibility: 'user' as const, sort_order: 0 }
    appState.cachedPublicSettings.custom_menu_items = [custom]
    adminState.customMenuItems = [{ ...custom, visibility: 'admin' }]
    route.path = '/custom/shared'
    mountSidebar()
    // In the drawer, the active workspace button has aria-pressed="true".
    const activeBtn = wrapper!.findAll('.console-drawer-workspace button').find(b => b.attributes('aria-pressed') === 'true')
    expect(activeBtn?.text()).toBeTruthy()
  })
})

describe('mobile drawer keyboard behavior', () => {
  it('owns the active tour popover and includes its controls in the keyboard cycle', async () => {
    tourActive = true
    const popover = document.createElement('div')
    popover.id = 'driver-popover-content'
    popover.innerHTML = '<button>Close tour</button><button>Next step</button>'
    document.body.append(popover)
    try {
      mountSidebar()
      appState.mobileOpen = true
      await nextTick(); await nextTick()
      expect(wrapper!.get('aside').attributes('aria-owns')).toBe('driver-popover-content')
      const close = wrapper!.get('.sidebar-mobile-close').element as HTMLButtonElement
      close.focus()
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
      expect(document.activeElement).toBe(popover.lastElementChild)
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
      expect(document.activeElement).toBe(close)
    } finally { popover.remove() }
  })
  it('focuses its close control, traps Tab, closes with Escape and returns focus to its opener', async () => {
    const opener = document.createElement('button')
    opener.textContent = 'Toggle menu'
    document.body.appendChild(opener)
    const setOpen = vi.fn((value: boolean) => { appState.mobileOpen = value })
    Object.assign(appState, { setMobileOpen: setOpen })
    mountSidebar()
    opener.focus()
    appState.mobileOpen = true
    await nextTick()
    await nextTick()
    const close = wrapper!.get('.sidebar-mobile-close').element as HTMLButtonElement
    expect(document.activeElement).toBe(close)
    expect(wrapper!.get('aside').attributes('aria-modal')).toBe('true')
    const targets = wrapper!.get('aside').element.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),[tabindex="0"]')
    targets[targets.length - 1]!.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(close)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    await nextTick()
    expect(setOpen).toHaveBeenCalledWith(false)
    expect(document.activeElement).toBe(opener)
    opener.remove()
  })
})

describe('closed mobile navigation accessibility', () => {
  it('makes the closed drawer inert and hidden, and restores it when open', async () => {
    mountSidebar()
    const aside = wrapper!.get('aside')
    // Drawer is always inert when closed (not tied to viewport width).
    expect(aside.attributes('inert')).toBeDefined()
    expect(aside.attributes('aria-hidden')).toBe('true')
    appState.mobileOpen = true
    await nextTick()
    expect(aside.attributes('inert')).toBeUndefined()
    expect(aside.attributes('aria-hidden')).toBeUndefined()
    appState.mobileOpen = false
    await nextTick()
    expect(aside.attributes('inert')).toBeDefined()
    expect(aside.attributes('aria-hidden')).toBe('true')
  })
})
