import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import TurnstileWidget from '../TurnstileWidget.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const scriptSelector = 'script[src*="challenges.cloudflare.com/turnstile"]'
let wrapper: VueWrapper | undefined

function installSDK() {
  const render = vi.fn<NonNullable<typeof window.turnstile>['render']>(() => 'widget-1')
  window.turnstile = { render, reset: vi.fn(), remove: vi.fn() }
  return render
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  delete window.turnstile
  delete window.onTurnstileLoad
  document.querySelectorAll(scriptSelector).forEach((script) => script.remove())
  vi.restoreAllMocks()
})

describe('TurnstileWidget', () => {
  it('shows a loading placeholder until the SDK initializes the widget', async () => {
    wrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })

    expect(wrapper.get('[role="status"]').text()).toBe('auth.captchaLoading')
    expect(document.querySelector(scriptSelector)).not.toBeNull()
    const container = wrapper.get('.turnstile-container').element

    const render = installSDK()
    window.onTurnstileLoad?.()
    await flushPromises()

    expect(render).toHaveBeenCalledWith(
      container,
      expect.objectContaining({ sitekey: 'site-key', action: 'auth' })
    )
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.get('.turnstile-container').element).toBe(container)
  })

  it('initializes an already loaded SDK and still forwards verification', async () => {
    const render = installSDK()
    wrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })
    await flushPromises()

    expect(render).toHaveBeenCalledOnce()
    expect(render.mock.calls[0]![1].action).toBe('auth')
    expect(document.querySelector(scriptSelector)).toBeNull()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)

    render.mock.calls[0]![1].callback('verified-token')
    expect(wrapper.emitted('verify')).toEqual([['verified-token']])
  })

  it('initializes both concurrent widgets after one shared SDK load', async () => {
    wrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })
    const secondWrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })
    try {
      expect(document.querySelectorAll(scriptSelector)).toHaveLength(1)
      expect(wrapper.find('[role="status"]').exists()).toBe(true)
      expect(secondWrapper.find('[role="status"]').exists()).toBe(true)

      const render = installSDK()
      window.onTurnstileLoad?.()
      await flushPromises()

      expect(render).toHaveBeenCalledTimes(2)
      expect(render.mock.calls.map(([container]) => container)).toEqual([
        wrapper.get('.turnstile-container').element,
        secondWrapper.get('.turnstile-container').element,
      ])
      expect(wrapper.find('[role="status"]').exists()).toBe(false)
      expect(secondWrapper.find('[role="status"]').exists()).toBe(false)
    } finally {
      secondWrapper.unmount()
    }
  })

  it('lets concurrent widgets adopt an existing SDK script that is still loading', async () => {
    const script = document.createElement('script')
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js'
    document.head.appendChild(script)
    wrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })
    const secondWrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })
    try {
      expect(document.querySelectorAll(scriptSelector)).toHaveLength(1)
      const render = installSDK()
      script.dispatchEvent(new Event('load'))
      await flushPromises()

      expect(render).toHaveBeenCalledTimes(2)
      expect(wrapper.find('[role="status"]').exists()).toBe(false)
      expect(secondWrapper.find('[role="status"]').exists()).toBe(false)
    } finally {
      secondWrapper.unmount()
    }
  })

  it('ends loading and reports a script load failure', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    wrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key' } })

    document.querySelector(scriptSelector)!.dispatchEvent(new Event('error'))
    await flushPromises()

    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.emitted('error')).toEqual([[]])
  })

  it('does not show a placeholder or load the SDK without a site key', async () => {
    wrapper = mount(TurnstileWidget, { props: { siteKey: '' } })
    await flushPromises()

    expect(wrapper.find('.turnstile-wrapper').exists()).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(document.querySelector(scriptSelector)).toBeNull()
  })
})
