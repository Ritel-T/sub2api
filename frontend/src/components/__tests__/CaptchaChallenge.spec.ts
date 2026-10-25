import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
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
