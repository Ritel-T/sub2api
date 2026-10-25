import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import CaptchaChallenge from '../CaptchaChallenge.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const props = { turnstileEnabled: false, turnstileSiteKey: '', tencentEnabled: false, tencentAppId: '', aliyunEnabled: false, aliyunSceneId: '', aliyunPrefix: '' }
const stubs = { TurnstileWidget: true, TencentCaptchaGate: true, AliyunCaptchaWidget: defineComponent({ props: ['sceneId', 'prefix', 'region'], template: '<div />' }), Icon: true }
afterEach(() => vi.restoreAllMocks())

describe('CAPTCHA browser notice on every shared verification surface', () => {
  it.each([
    ['Turnstile', { turnstileEnabled: true, turnstileSiteKey: 'public-site-key' }],
    ['Tencent', { tencentEnabled: true, tencentAppId: 'public-app-id' }],
    ['Aliyun', { aliyunEnabled: true, aliyunSceneId: 'scene', aliyunPrefix: 'prefix' }]
  ])('shows the advisory above %s in QQ', (_name, provider) => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Chrome/135.0 Safari/537.36 QQ/9.1.65')
    const wrapper = mount(CaptchaChallenge, { props: { ...props, ...provider }, global: { stubs } })
    expect(wrapper.get('[role="alert"]').text()).toContain('auth.captchaBrowserNoticeTitle')
    expect(wrapper.find('button[disabled]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('does not warn when verification is disabled or lacks configuration', () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('QQ/9.1.65')
    for (const change of [{}, { turnstileEnabled: true }]) {
      const wrapper = mount(CaptchaChallenge, { props: { ...props, ...change }, global: { stubs } })
      expect(wrapper.find('[role="alert"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })
  it('keeps the notice hidden in regular Chrome while forwarding the verification token', () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Chrome/135.0 Safari/537.36')
    const wrapper = mount(CaptchaChallenge, { props: { ...props, turnstileEnabled: true, turnstileSiteKey: 'site' }, global: { stubs } })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.getComponent({ name: 'TurnstileWidget' }).vm.$emit('verify', 'fresh-proof')
    expect(wrapper.emitted('verify')).toEqual([['fresh-proof', '']])
    wrapper.unmount()
  })
})


describe('verification failure advice in recommended browsers', () => {
  it.each([
    ['Chrome', 'Chrome/144.0.7559.60 Safari/537.36'],
    ['Edge', 'Chrome/144.0.7559.60 Safari/537.36 Edg/144.0'],
    ['Firefox', 'Gecko/20100101 Firefox/144.0'],
    ['Safari', 'Version/18.3 Mobile/15E148 Safari/604.1']
  ])('shows network and browser advice after a challenge error in %s', async (_name, ua) => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(ua)
    const reset = vi.fn()
    const turnstile = defineComponent({ name: 'TurnstileWidget', template: '<div />', methods: { reset } })
    const wrapper = mount(CaptchaChallenge, { props: { ...props, turnstileEnabled: true, turnstileSiteKey: 'site' }, global: { stubs: { ...stubs, TurnstileWidget: turnstile } } })
    const widget = wrapper.getComponent({ name: 'TurnstileWidget' })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    widget.vm.$emit('error')
    await nextTick()
    expect(wrapper.get('[role="alert"]').text()).toContain('auth.captchaBrowserNoticeNetworkHint')
    expect(wrapper.get('[role="alert"]').text()).toContain('auth.captchaBrowserNoticeTitle')
    expect(wrapper.emitted('error')).toHaveLength(1)
    wrapper.vm.reset()
    await nextTick()
    expect(reset).toHaveBeenCalledOnce()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    widget.vm.$emit('verify', 'fresh-proof')
    await nextTick()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.emitted('verify')).toEqual([['fresh-proof', '']])
    wrapper.unmount()
  })

  it('shows advice for a backend CAPTCHA rejection but not password or email-code errors', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Chrome/144.0 Safari/537.36')
    const wrapper = mount(CaptchaChallenge, { props: { ...props, turnstileEnabled: true, turnstileSiteKey: 'site' }, global: { stubs } })
    for (const reason of ['INVALID_CREDENTIALS', 'INVALID_VERIFY_CODE', 'ERR_NETWORK']) {
      wrapper.vm.reportFailure({ reason })
      await nextTick()
      expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    }
    wrapper.vm.reportFailure({ reason: 'TURNSTILE_VERIFICATION_FAILED' })
    await nextTick()
    expect(wrapper.get('[role="alert"]').text()).toContain('auth.captchaBrowserNoticeNetworkHint')
    wrapper.unmount()
  })

  it('keeps the QQ desktop browser advice after verification succeeds', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('QQEX/9.9.35 Chrome/144.0 Safari/537.36 QQWebContentView/1.0')
    const wrapper = mount(CaptchaChallenge, { props: { ...props, turnstileEnabled: true, turnstileSiteKey: 'site' }, global: { stubs } })
    const widget = wrapper.getComponent({ name: 'TurnstileWidget' })
    widget.vm.$emit('error')
    await nextTick()
    expect(wrapper.text()).toContain('auth.captchaBrowserNoticeNetworkHint')
    widget.vm.$emit('verify', 'fresh-proof')
    await nextTick()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('auth.captchaBrowserNoticeNetworkHint')
    wrapper.unmount()
  })

  it.each(['Tencent', 'Aliyun'])('shows advice if %s action verification rejects', async provider => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Chrome/144.0 Safari/537.36')
    const verify = vi.fn().mockRejectedValue(new Error('challenge failed'))
    const gate = defineComponent({ props: ['sceneId', 'prefix', 'region'], template: '<div />', methods: { verify } })
    const providerProps = provider === 'Tencent' ? { tencentEnabled: true, tencentAppId: 'app' } : { aliyunEnabled: true, aliyunSceneId: 'scene', aliyunPrefix: 'prefix' }
    const wrapper = mount(CaptchaChallenge, { props: { ...props, ...providerProps }, global: { stubs: { ...stubs, TencentCaptchaGate: gate, AliyunCaptchaWidget: gate } } })
    expect(await wrapper.vm.verifyAction()).toBeNull()
    expect(wrapper.get('[role="alert"]').text()).toContain('auth.captchaBrowserNoticeNetworkHint')
    expect(wrapper.emitted('error')).toHaveLength(1)
    wrapper.unmount()
  })
})


it('shows retry advice when a proof expires in Chrome', async () => {
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Chrome/144.0 Safari/537.36')
  const wrapper = mount(CaptchaChallenge, { props: { ...props, turnstileEnabled: true, turnstileSiteKey: 'site' }, global: { stubs } })
  wrapper.getComponent({ name: 'TurnstileWidget' }).vm.$emit('expire')
  await nextTick()
  expect(wrapper.get('[role="alert"]').text()).toContain('auth.captchaBrowserNoticeNetworkHint')
  expect(wrapper.emitted('expire')).toHaveLength(1)
  wrapper.unmount()
})
