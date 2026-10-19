import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Config, Driver, DriveStep } from 'driver.js'
import { useOnboardingStore } from '@/stores/onboarding'
import { useOnboardingTour } from '@/composables/useOnboardingTour'

const fake = vi.hoisted(() => ({ drivers: [] as Array<{ config: Config; api: Driver }> }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1, role: 'admin' }, isSimpleMode: false }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/Guide/steps', () => ({
  getAdminSteps: () => [
    { popover: {} },
    { element: '#sidebar-group-manage', popover: { showButtons: ['close'] } },
    { element: '[data-tour="groups-create-btn"]', popover: { showButtons: ['close'] } },
    { element: '[data-tour="group-form-submit"]', popover: { showButtons: ['close'] } }
  ],
  getUserSteps: () => []
}))
vi.mock('driver.js', () => ({
  driver: (config: Config) => {
    let active = false
    let index = 0
    const element = () => {
      const selector = config.steps?.[index]?.element
      return typeof selector === 'string' ? document.querySelector(selector) ?? undefined : undefined
    }
    const highlight = () => config.onHighlightStarted?.(element(), config.steps?.[index] as DriveStep, { config, state: { activeIndex: index }, driver: api })
    const api = {
      isActive: () => active,
      getActiveIndex: () => index,
      getActiveElement: element,
      drive: vi.fn((start = 0) => { active = true; index = start; highlight() }),
      moveTo: vi.fn((target: number) => { index = target; highlight() }),
      destroy: vi.fn(() => { active = false; config.onDestroyed?.(element(), config.steps?.[index] as DriveStep, { config, state: { activeIndex: index }, driver: api }) })
    } as unknown as Driver
    fake.drivers.push({ config, api })
    return api
  }
}))

describe('onboarding in the console shell', () => {
  const wrappers: Array<ReturnType<typeof mount>> = []
  let store: ReturnType<typeof useOnboardingStore>
  function setup(prepareElement = vi.fn(async () => true), onEnd = vi.fn()) {
    let tour!: ReturnType<typeof useOnboardingTour>
    const wrapper = mount(defineComponent({ setup() { tour = useOnboardingTour({ prepareElement, onEnd }); return () => null } }))
    wrappers.push(wrapper)
    return { wrapper, tour, prepareElement, onEnd }
  }
  function nextClick() {
    const { config, api } = fake.drivers.at(-1)!
    config.onNextClick?.(undefined, config.steps?.[api.getActiveIndex() ?? 0] as DriveStep, { config, state: { activeIndex: api.getActiveIndex() }, driver: api })
  }
  beforeEach(() => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    store = useOnboardingStore()
    fake.drivers.length = 0
    document.body.innerHTML = '<a id="sidebar-group-manage" href="/admin/groups">Groups</a><button data-tour="groups-create-btn">Create</button><form><button type="submit" data-tour="group-form-submit">Save</button></form>'
    document.querySelector('a')!.addEventListener('click', event => event.preventDefault())
    document.querySelector('form')!.addEventListener('submit', event => event.preventDefault())
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 80, height: 40, x: 0, y: 0, top: 0, left: 0, right: 80, bottom: 40, toJSON() {} })
  })
  afterEach(() => {
    store.getDriverInstance()?.destroy()
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
    vi.useRealTimers()
  })
  it('prepares the real navigation target before highlighting it', async () => {
    const { tour, prepareElement } = setup()
    await tour.startTour()
    nextClick()
    await flushPromises()
    expect(prepareElement).toHaveBeenCalledWith('#sidebar-group-manage')
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledWith(1)
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledTimes(1)
  })
  it('does not advance if the role-filtered destination is unavailable', async () => {
    const { tour } = setup(vi.fn(async () => false))
    await tour.startTour()
    nextClick()
    await flushPromises()
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
  })
  it('advances a clicked navigation link exactly once, including simultaneous business notifications', async () => {
    const { tour } = setup()
    await tour.startTour(1)
    document.querySelector<HTMLElement>('#sidebar-group-manage')!.click()
    void store.nextStep(0)
    await vi.advanceTimersByTimeAsync(800)
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledWith(2)
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledTimes(1)
  })
  it('retains its controller across route view replacement and advances only after submit success', async () => {
    const first = setup()
    await first.tour.startTour(3)
    first.wrapper.unmount()
    const replacement = setup()
    expect(fake.drivers).toHaveLength(1)
    // A submit click itself must never skip validation/business success.
    document.querySelector<HTMLElement>('[data-tour="group-form-submit"]')!.dispatchEvent(new MouseEvent('click'))
    await vi.advanceTimersByTimeAsync(900)
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
    await replacement.tour.nextStep(0)
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled() // Last interactive step has no next target.
    expect(store.getDriverInstance()).toBeNull()
    expect(fake.drivers[0].api.isActive()).toBe(false)
  })
  it('ignores a pending transition after the tour is closed', async () => {
    const { tour } = setup()
    await tour.startTour(1)
    document.querySelector<HTMLElement>('#sidebar-group-manage')!.click()
    fake.drivers[0].api.destroy()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
    expect(store.getDriverInstance()).toBeNull()
  })
  it('uses the original prepared session from a replacement layout', async () => {
    const first = setup()
    await first.tour.startTour(1)
    first.wrapper.unmount()
    const second = setup()
    await second.tour.nextStep(0)
    expect(first.prepareElement).toHaveBeenLastCalledWith('[data-tour="groups-create-btn"]')
    expect(second.prepareElement).not.toHaveBeenCalled()
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledWith(2)
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledTimes(1)
  })
  it('ends the owning session once when Escape closes a navigation step', async () => {
    const { tour, onEnd } = setup()
    await tour.startTour(1)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(onEnd).toHaveBeenCalledTimes(1)
    expect(store.getDriverInstance()).toBeNull()
    fake.drivers[0].api.destroy()
    expect(onEnd).toHaveBeenCalledTimes(1)
  })
  it('cleans up the original session when the last submit succeeds after a route replacement', async () => {
    const first = setup()
    await first.tour.startTour(3)
    first.wrapper.unmount()
    const replacement = setup()
    await replacement.tour.nextStep(0)
    expect(first.onEnd).toHaveBeenCalledTimes(1)
    expect(replacement.onEnd).not.toHaveBeenCalled()
    expect(store.getDriverInstance()).toBeNull()
  })
  it('keeps arrow navigation on an interactive target and triggers Enter exactly once', async () => {
    const { tour } = setup()
    await tour.startTour(1)
    const link = document.querySelector<HTMLElement>('#sidebar-group-manage')!
    const click = vi.fn()
    link.addEventListener('click', click)
    link.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await vi.advanceTimersByTimeAsync(900)
    expect(click).not.toHaveBeenCalled()
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
    link.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await vi.advanceTimersByTimeAsync(800)
    expect(click).toHaveBeenCalledTimes(1)
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledWith(2)
    expect(fake.drivers[0].api.moveTo).toHaveBeenCalledTimes(1)
  })
  it('leaves input navigation keys to the control and removes listeners on exit', async () => {
    document.querySelector('#sidebar-group-manage')!.outerHTML = '<input id="sidebar-group-manage" />'
    const { tour } = setup()
    await tour.startTour(1)
    const input = document.querySelector<HTMLInputElement>('#sidebar-group-manage')!
    for (const key of ['ArrowLeft', 'ArrowRight', 'Enter']) {
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
      input.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(false)
    }
    await vi.advanceTimersByTimeAsync(900)
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
    fake.drivers[0].api.destroy()
    input.dispatchEvent(new Event('input'))
    await vi.advanceTimersByTimeAsync(900)
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
  })
  it('preserves the tabindex of Select wrappers that use manual navigation', async () => {
    document.querySelector('#sidebar-group-manage')!.outerHTML = '<div id="sidebar-group-manage"><button class="select-trigger">Choose</button></div>'
    const { tour } = setup()
    await tour.startTour(1)
    const select = document.querySelector<HTMLElement>('#sidebar-group-manage')!
    expect(select.hasAttribute('tabindex')).toBe(false)
    select.click()
    await vi.advanceTimersByTimeAsync(900)
    expect(fake.drivers[0].api.moveTo).not.toHaveBeenCalled()
    fake.drivers[0].api.destroy()
    expect(select.hasAttribute('tabindex')).toBe(false)
  })
})
