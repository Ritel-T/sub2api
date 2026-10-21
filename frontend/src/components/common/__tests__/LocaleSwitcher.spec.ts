import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import LocaleSwitcher from '../LocaleSwitcher.vue'

const { setLocale } = vi.hoisted(() => ({ setLocale: vi.fn() }))

vi.mock('@/i18n', () => ({
  setLocale,
  availableLocales: [
    { code: 'en', name: 'English', flag: '🇺🇸' },
    { code: 'zh-Hant-HK', name: '繁體中文（香港）', flag: '🇭🇰' },
    { code: 'zh', name: '简体中文', flag: '🇨🇳' }
  ]
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'zh' } }) }))

const reload = vi.fn()

const mountSwitcher = () =>
  mount(LocaleSwitcher, { global: { stubs: { Icon: true, transition: false } } })

const choose = async (wrapper: ReturnType<typeof mountSwitcher>, name: string) => {
  await wrapper.get('button').trigger('click')
  const option = wrapper.findAll('button').find((button) => button.text().includes(name))
  await option!.trigger('click')
  await flushPromises()
}

describe('LocaleSwitcher', () => {
  beforeEach(() => {
    setLocale.mockReset().mockResolvedValue(undefined)
    reload.mockReset()
    vi.stubGlobal('location', { ...window.location, reload })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders bundled flags for the current language and all options without emoji glyphs', async () => {
    const wrapper = mountSwitcher()
    const trigger = wrapper.get('button')
    expect(trigger.attributes('aria-label')).toBe('简体中文')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(trigger.get('img').attributes('src')).toContain('/assets/flags/cn.svg')
    expect(trigger.get('img').attributes('alt')).toBe('')

    await trigger.trigger('click')

    expect(trigger.attributes('aria-expanded')).toBe('true')
    const flags = wrapper.findAll('img').map(flag => flag.attributes('src'))
    expect(flags).toEqual([
      expect.stringContaining('/assets/flags/cn.svg'),
      expect.stringContaining('/assets/flags/us.svg'),
      expect.stringContaining('/assets/flags/hk.svg'),
      expect.stringContaining('/assets/flags/cn.svg')
    ])
    expect(wrapper.text()).not.toMatch(/[\u{1F1E6}-\u{1F1FF}]/u)
  })

  // 分组名称等内容由后端按请求语言返回，已加载的数据要靠整页刷新换成新语言
  it('reloads the page after switching to another language', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, 'English')

    expect(setLocale).toHaveBeenCalledWith('en')
    expect(reload).toHaveBeenCalledTimes(1)
    expect(setLocale.mock.invocationCallOrder[0]).toBeLessThan(reload.mock.invocationCallOrder[0])
  })

  it('offers Hong Kong Traditional Chinese and reloads after selecting it', async () => {
    const wrapper = mountSwitcher()
    await choose(wrapper, '繁體中文（香港）')
    expect(setLocale).toHaveBeenCalledWith('zh-Hant-HK')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does nothing when the current language is chosen again', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, '简体中文')

    expect(setLocale).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
  })
})
