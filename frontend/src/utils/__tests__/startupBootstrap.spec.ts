import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => {
  const app = { use: vi.fn(), mount: vi.fn() }
  const router = {
    isReady: vi.fn(),
    replace: vi.fn(),
    options: { history: { location: '/dashboard?keep=1#usage' } }
  }
  return {
    app, router, pinia: {},
    createApp: vi.fn(() => app), createPinia: vi.fn(), watch: vi.fn(),
    initializeTheme: vi.fn(), initConfig: vi.fn(), initI18n: vi.fn(), auth: vi.fn()
  }
})
vi.mock('vue', () => ({ createApp: mocks.createApp, watch: mocks.watch }))
vi.mock('pinia', () => ({ createPinia: mocks.createPinia }))
vi.mock('../../App.vue', () => ({ default: {} }))
vi.mock('@/router', () => ({ default: mocks.router }))
vi.mock('@/i18n', () => ({ default: {}, getLocale: () => 'zh', initI18n: mocks.initI18n }))
vi.mock('@/utils/accountConfig', () => ({ centralAccountEnabled: true, redirectConsoleAlias: () => false }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ initializeCentralAuth: mocks.auth }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ initFromInjectedConfig: mocks.initConfig }) }))
vi.mock('@/utils/device', () => ({ isIOSDevice: () => false }))
vi.mock('@/composables/useTheme', () => ({ initializeTheme: mocks.initializeTheme, useTheme: () => ({ resolvedTheme: {} }) }))
vi.mock('@/utils/chartTheme', () => ({ getChartTheme: () => ({ text: '', grid: '' }) }))
vi.mock('chart.js', () => ({ Chart: { defaults: { font: {} } } }))

beforeEach(() => {
  vi.resetModules()
  vi.clearAllMocks()
  vi.useFakeTimers()
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
  mocks.createPinia.mockReturnValue(mocks.pinia)
  mocks.auth.mockReset().mockResolvedValue(undefined)
  mocks.initI18n.mockReset().mockResolvedValue(undefined)
  mocks.router.isReady.mockReset().mockResolvedValue(undefined)
  mocks.router.replace.mockReset().mockResolvedValue(undefined)
  document.body.innerHTML = '<div id="app"></div>'
  mocks.app.mount.mockImplementation(() => {
    document.getElementById('app')!.textContent = 'Mounted application'
  })
})
afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  document.body.replaceChildren()
})
const settle = () => vi.advanceTimersByTimeAsync(0)
function expectSetupOnce() {
  expect(mocks.createApp).toHaveBeenCalledTimes(1)
  expect(mocks.createPinia).toHaveBeenCalledTimes(1)
  expect(mocks.initializeTheme).toHaveBeenCalledTimes(1)
  expect(mocks.watch).not.toHaveBeenCalled()
  expect(mocks.initConfig).toHaveBeenCalledTimes(1)
  expect(mocks.initI18n).toHaveBeenCalledTimes(1)
  // Pinia, i18n and router each installed once.
  expect(mocks.app.use).toHaveBeenCalledTimes(3)
  expect(mocks.app.mount).toHaveBeenCalledTimes(1)
}

describe('startup entrypoint', () => {
  it('withholds mounting until hydration succeeds, then automatically resumes without repeating setup', async () => {
    mocks.auth.mockRejectedValueOnce(new Error('502'))
    await import('../../main')
    await settle()
    expect(mocks.app.mount).not.toHaveBeenCalled()
    expect(mocks.router.isReady).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('连接暂时不稳定')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.auth).toHaveBeenCalledTimes(2)
    expectSetupOnce()
    expect(document.body.textContent).toBe('Mounted application')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('retries rejected initial navigation on the same URL with one router install', async () => {
    mocks.router.isReady.mockRejectedValueOnce(new Error('temporary route failure'))
    await import('../../main')
    await settle()
    expect(mocks.app.mount).not.toHaveBeenCalled()
    document.querySelector('button')!.click()
    window.dispatchEvent(new Event('online'))
    await settle()
    expect(mocks.router.replace).toHaveBeenCalledTimes(1)
    expect(mocks.router.replace).toHaveBeenCalledWith('/dashboard?keep=1#usage')
    expect(mocks.router.isReady).toHaveBeenCalledTimes(2)
    expectSetupOnce()
    expect(vi.getTimerCount()).toBe(0)
  })
})
