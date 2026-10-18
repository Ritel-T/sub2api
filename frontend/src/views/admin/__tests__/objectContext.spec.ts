import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { objectQueryId } from '../objectContext'
import ConsoleObjectLinks from '@/components/admin/ConsoleObjectLinks.vue'
const flags = vi.hoisted(() => ({ public: { payment_enabled: true, subscription_enabled: true }, admin: { paymentEnabled: true }, pilot: { value: false } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: flags.public }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => flags.admin }))
vi.mock('@/composables/useMerchantTestPaymentAccess', () => ({ useMerchantTestPaymentAccess: () => ({ merchantTestPaymentAccess: flags.pilot }) }))
const auth = vi.hoisted(() => ({ isAdmin: true, isSimpleMode: false }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('exact object context', () => {
  it.each(['0','-1','1.5','12extra','Infinity','9007199254740993',''])('rejects invalid identifiers %s', value => {
    expect(objectQueryId(value)).toBeUndefined()
  })
  it('takes one safe positive identifier without broad numeric coercion', () => {
    expect(objectQueryId('42')).toBe(42)
    expect(objectQueryId(['84','99'])).toBe(84)
  })
  it('preserves unrelated query and hash when removing account scope', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push('/admin/usage?account_id=42&start_date=2026-10-01#errors')
    const wrapper = mount(ConsoleObjectLinks, { props: { kind: 'account', id: 42, context: true, proxyId: 84 }, global: { plugins: [router] } })
    expect(wrapper.find('a[href="/admin/account-quality?account_id=42"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/admin/proxies?proxy_id=84"]').exists()).toBe(true)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ start_date: '2026-10-01' })
    expect(router.currentRoute.value.hash).toBe('#errors')
    wrapper.unmount()
  })
  it('honors global subscription/payment closure even with stale admin cache and allows authorized merchant pilot', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push('/admin/users')
    flags.public.payment_enabled = false; flags.public.subscription_enabled = false
    const wrapper = mount(ConsoleObjectLinks, { props: { kind: 'user', id: 42 }, global: { plugins: [router] } })
    expect(wrapper.findAll('a').map(link => link.attributes('href'))).toEqual(['/admin/usage?user_id=42'])
    wrapper.unmount(); flags.pilot.value = true
    const pilot = mount(ConsoleObjectLinks, { props: { kind: 'user', id: 42 }, global: { plugins: [router] } })
    expect(pilot.find('a[href="/admin/orders?user_id=42"]').exists()).toBe(true)
    pilot.unmount(); flags.pilot.value = false; flags.public.payment_enabled = true; flags.public.subscription_enabled = true
  })
  it('hides unavailable subscription and privileged destinations', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push('/admin/users')
    auth.isSimpleMode = true
    const wrapper = mount(ConsoleObjectLinks, { props: { kind: 'user', id: 42 }, global: { plugins: [router] } })
    expect(wrapper.findAll('a').map(link => link.attributes('href'))).toEqual(['/admin/usage?user_id=42','/admin/orders?user_id=42'])
    wrapper.unmount(); auth.isAdmin = false
    const observer = mount(ConsoleObjectLinks, { props: { kind: 'account', id: 42, proxyId: 84 }, global: { plugins: [router] } })
    expect(observer.findAll('a')).toHaveLength(0)
    observer.unmount(); auth.isAdmin = true; auth.isSimpleMode = false
  })
})
