import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { computed, nextTick, reactive } from 'vue'
import AppSidebar from '../AppSidebar.vue'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), fetchAdminSettings: vi.fn() }))

interface AuthState {
  user: { id: number; role: string } | null
  token: string | null
  readonly isAuthenticated: boolean
  readonly isAdmin: boolean
  readonly isObserver: boolean
  isSimpleMode: boolean
}

let authState: AuthState
let appState: {
  cachedPublicSettings: { payment_enabled?: boolean; subscription_enabled?: boolean; payment_balance_disabled?: boolean }
  publicSettingsLoaded: boolean
  sidebarCollapsed: boolean
  mobileOpen: boolean
  backendModeEnabled: boolean
  sidebarScrollTop: number
  siteName: string
  siteLogo: string
  siteVersion: string
}
let adminState: { paymentEnabled: boolean; customMenuItems: never[]; fetch: typeof mocks.fetchAdminSettings }
const route = reactive({ path: '/admin/dashboard' })

vi.mock('@/api/payment', () => ({ paymentAPI: { getConfig: mocks.getConfig } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appState }))
vi.mock('@/stores', () => ({
  useAuthStore: () => authState,
  useAppStore: () => appState,
  useAdminSettingsStore: () => adminState,
  useOnboardingStore: () => ({ isCurrentStep: () => false }),
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: computed(() => false), refreshBatchImageAccess: vi.fn() }),
}))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/common/VersionBadge.vue', () => ({ default: { template: '<span />' } }))

let wrapper: VueWrapper | undefined

function mountSidebar() {
  wrapper = shallowMount(AppSidebar, {
    global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
  })
  return wrapper
}

function hasLink(path: string): boolean {
  return wrapper?.find(`a[href="${path}"]`).exists() ?? false
}

function hasAdminPayments(): boolean {
  return wrapper?.findAll('button').some(button => button.text().includes('nav.orderManagement')) ?? false
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.clearAllMocks()
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
    publicSettingsLoaded: true, sidebarCollapsed: false, mobileOpen: false,
    backendModeEnabled: false, sidebarScrollTop: 0, siteName: 'RynexAI', siteLogo: '', siteVersion: 'test',
  })
  adminState = reactive({ paymentEnabled: false, customMenuItems: [], fetch: mocks.fetchAdminSettings })
  route.path = '/admin/dashboard'
})

afterEach(() => { wrapper?.unmount(); wrapper = undefined })

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
    expect(wrapper?.find('a[href="/purchase"]').text()).toBe('nav.recharge')
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
    appState.sidebarCollapsed = true
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
