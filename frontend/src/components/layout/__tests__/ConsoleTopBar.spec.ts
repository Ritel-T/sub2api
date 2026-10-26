import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, type VueWrapper } from '@vue/test-utils'
import { computed, reactive, ref } from 'vue'
import ConsoleTopBar from '../ConsoleTopBar.vue'
import { consoleNavigationText } from '@/navigation/consoleNavigation'

const locale = ref('en')
const app = reactive({ mobileOpen: false, backendModeEnabled: false, docUrl: '', siteVersion: 'test', publicSettingsLoaded: true, contactInfo: '', toggleMobileSidebar: vi.fn() })
const auth = reactive({ isAdmin: false, isObserver: false, user: { username: 'Preview', email: 'preview@example.invalid', balance: 20, frozen_balance: 5, avatar_url: '' } })
const section = { key: 'support', label: 'Support', path: '/support-tickets', tabs: [], badge: () => 3 }
const nav = {
  navText: computed(() => consoleNavigationText(locale.value)), sections: ref([section]), activeLocation: ref(undefined),
  workspaceOptions: ref([]), workspace: ref('personal'), searchItems: ref([]), homePath: ref('/dashboard'),
  switchWorkspace: vi.fn(), handleNavigate: vi.fn(),
}
vi.mock('@/stores', () => ({ useAppStore: () => app, useAuthStore: () => auth, useOnboardingStore: () => ({ replay: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ currentRoute: ref({ fullPath: '/dashboard' }), push: vi.fn() }) }))
vi.mock('@/navigation/useConsoleNavSource', () => ({ useConsoleNav: () => nav }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: {} }, isFeatureFlagEnabled: () => false }))
vi.mock('@/components/common/VersionBadge.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/common/LocaleSwitcher.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/common/SubscriptionProgressMini.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/common/AnnouncementBell.vue', () => ({ default: { template: '<span />' } }))
vi.mock('../FeatureSearch.vue', () => ({ default: { name: 'FeatureSearch', template: '<span />' } }))

let wrapper: VueWrapper
beforeEach(() => { app.mobileOpen = false; app.toggleMobileSidebar.mockClear(); locale.value = 'en'; auth.isObserver = false; app.backendModeEnabled = false; auth.user.frozen_balance = 5; section.badge = () => 3 })
afterEach(() => { wrapper?.unmount() })
function mountBar() {
  wrapper = shallowMount(ConsoleTopBar, { attachTo: document.body, global: { stubs: { transition: false, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
  return wrapper
}
describe('console utility access and balance semantics', () => {
  it('caps the visible badge while retaining the full accessible count', () => {
    section.badge = () => 103
    const view = mountBar()
    expect(view.find('.console-nav-badge').text()).toBe('99+')
    expect(view.find('.console-nav-badge').attributes('aria-label')).toBe('103 pending')
  })
  it('keeps compact feature search mounted on every viewport', () => {
    const view = mountBar()
    expect(view.find('feature-search-stub').exists()).toBe(true)
    expect(view.find('feature-search-stub').classes()).not.toContain('hidden')
  })
  it('opens navigation from the leftmost control before the brand', async () => {
    const view = mountBar()
    const left = view.get('.console-top-bar-left')
    const toggle = left.get('button[aria-controls="console-sidebar"]')
    expect(left.element.firstElementChild).toBe(toggle.element)
    expect(view.find('.console-top-bar-right button[aria-controls="console-sidebar"]').exists()).toBe(false)
    await toggle.trigger('click')
    expect(app.toggleMobileSidebar).toHaveBeenCalledTimes(1)
  })
  it('shows available, frozen and total funds in the avatar menu', async () => {
    const view = mountBar()
    await view.find('.console-avatar-btn').trigger('click')
    expect(view.find('.console-menu-balances').text()).toContain('Available balance$20.00')
    expect(view.find('.console-menu-balances').text()).toContain('Frozen balance$5.00')
    expect(view.find('.console-menu-balances').text()).toContain('Total balance$25.00')
  })
  it.each(['observer', 'backend'])('omits financial details in %s mode', async mode => {
    auth.isObserver = mode === 'observer'; app.backendModeEnabled = mode === 'backend'
    const view = mountBar()
    await view.find('.console-avatar-btn').trigger('click')
    expect(view.find('.console-menu-balances').exists()).toBe(false)
  })
  it('does not present a frozen amount when none exists', async () => {
    auth.user.frozen_balance = 0
    const view = mountBar()
    await view.find('.console-avatar-btn').trigger('click')
    expect(view.findAll('.console-menu-balances dt').map(node => node.text())).toEqual(['Available balance'])
  })
  it('uses translated numeric badge names and Hong Kong balance terms', async () => {
    locale.value = 'zh-Hant-HK'
    const view = mountBar()
    expect(view.find('.console-nav-badge').attributes('aria-label')).toBe('3 項待處理')
    await view.find('.console-avatar-btn').trigger('click')
    expect(view.findAll('.console-menu-balances dt').map(node => node.text())).toEqual(['可用餘額', '凍結金額', '總額'])
  })
})
