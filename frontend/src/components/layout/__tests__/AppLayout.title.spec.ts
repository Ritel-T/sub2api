import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import AppLayout from '../AppLayout.vue'
vi.mock('vue-router', async () => ({ ...(await vi.importActual<typeof import('vue-router')>('vue-router')), useRoute: () => ({ path: '/fixture' }) }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ locale: ref('en') }) }))
vi.mock('@/stores', () => ({ useAuthStore: () => ({ user: { role: 'admin' } }), useAppStore: () => ({ mobileOpen: false }), useOnboardingStore: () => ({ setReplayCallback: vi.fn() }) }))
vi.mock('@/composables/useOnboardingTour', () => ({ useOnboardingTour: () => ({ replayTour: vi.fn() }) }))
vi.mock('@/navigation/useConsoleNavSource', () => ({ provideConsoleNav: () => ({ activeLocation: ref(null), prepareTourTarget: vi.fn(), finishTour: vi.fn() }) }))

describe('page title ownership', () => {
  it('uses an explicit prop for local versus layout-owned titles', async () => {
    const wrapper = mount(AppLayout, { slots: { default: '<div>Page content</div>' }, global: { stubs: { AppSidebar: true, ConsoleTopBar: true, ConsoleSectionTabs: true, ConsoleBottomNav: true, ConsolePageTitle: { template: '<h1 data-test="layout-title">Route title</h1>' } } } })
    expect(wrapper.findAll('h1')).toHaveLength(1)
    await wrapper.setProps({ showPageTitle: false })
    expect(wrapper.findAll('h1')).toHaveLength(0)
    expect(wrapper.get('main').text()).toContain('Page content')
    await wrapper.setProps({ showPageTitle: true })
    expect(wrapper.findAll('h1')).toHaveLength(1)
    wrapper.unmount()
  })
})
