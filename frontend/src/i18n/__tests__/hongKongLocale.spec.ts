import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import zhHK from '../locales/zh-Hant-HK'
import { localizeInlineText } from '../inline'
import inlineSource from '../inline?raw'

vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: () => 'Localized page' }))
vi.mock('@/router', () => ({ default: { currentRoute: { value: {} } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {} }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: false }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))

beforeEach(() => {
  vi.restoreAllMocks()
  vi.resetModules()
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US'])
  vi.spyOn(navigator, 'language', 'get').mockReturnValue('en-US')
  localStorage.clear()
  document.documentElement.removeAttribute('lang')
})

afterEach(() => vi.restoreAllMocks())

describe('Hong Kong locale selection', () => {
  it.each([
    ['zh-CN', 'zh'], ['zh-SG', 'zh'], ['zh-Hans', 'zh'], ['zh', 'zh'],
    ['zh-HK', 'zh-Hant-HK'], ['zh-TW', 'zh-Hant-HK'], ['zh-MO', 'zh-Hant-HK'],
    ['zh-Hant', 'zh-Hant-HK'], ['zh-Hans-HK', 'zh'], ['zh-Hant-CN', 'zh-Hant-HK'],
    ['ZH_tw', 'zh-Hant-HK'], ['en-GB', 'en'], ['ja-JP', 'en'],
  ])('matches browser %s to %s without saving an automatic choice', async (browser, expected) => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue([browser])
    const { getLocale, initI18n } = await import('../index')
    await initI18n()
    expect(getLocale()).toBe(expected)
    expect(document.documentElement.lang).toBe(expected)
    expect(localStorage.getItem('sub2api_locale')).toBeNull()
  })

  it.each([
    [['fr-FR', 'zh-TW', 'en-US'], 'zh-Hant-HK'],
    [['en-US', 'zh-CN'], 'en'],
    [['zh-CN', 'zh-TW'], 'zh'],
    [['ja-JP', 'de-DE'], 'en'],
  ])('uses the first supported browser preference from %j', async (languages, expected) => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(languages)
    const { getLocale } = await import('../index')
    expect(getLocale()).toBe(expected)
  })

  it('falls back to navigator.language when the preference list is empty', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue([])
    vi.spyOn(navigator, 'language', 'get').mockReturnValue('zh-HK')
    const { getLocale } = await import('../index')
    expect(getLocale()).toBe('zh-Hant-HK')
  })

  it('uses browser preferences when reading storage throws', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['zh-CN'])
    const { getLocale, initI18n } = await import('../index')
    await initI18n()
    expect(getLocale()).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
  })

  it.each([['en', 'en'], ['zh-CN', 'zh'], ['zh-HK', 'zh-Hant-HK']])('keeps saved %s ahead of browser preferences', async (saved, expected) => {
    localStorage.setItem('sub2api_locale', saved)
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['zh-TW'])
    const { getLocale } = await import('../index')
    expect(getLocale()).toBe(expected)
  })

  it.each(['zh', 'zh-Hant-HK'] as const)('preserves the saved %s choice', async (locale) => {
    localStorage.setItem('sub2api_locale', locale)
    const { getLocale, initI18n, i18n } = await import('../index')
    await initI18n()
    expect(getLocale()).toBe(locale)
    expect(document.documentElement.lang).toBe(locale)
    const messages = i18n.global.getLocaleMessage(locale) as { common: { loading: string } }
    expect(messages.common.loading).toBe(locale === 'zh' ? '加载中...' : '載入中...')
  })

  it('ignores unknown saved values and follows the browser', async () => {
    localStorage.setItem('sub2api_locale', 'unexpected')
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['zh-CN'])
    const { getLocale } = await import('../index')
    expect(getLocale()).toBe('zh')
  })

  it('stores an explicit choice and restores it on the next load', async () => {
    const { setLocale, getLocale } = await import('../index')
    await setLocale('zh-Hant-HK')
    expect(getLocale()).toBe('zh-Hant-HK')
    expect(localStorage.getItem('sub2api_locale')).toBe('zh-Hant-HK')
    vi.resetModules()
    const restored = await import('../index')
    expect(restored.getLocale()).toBe('zh-Hant-HK')
  })

  it('applies an explicit choice and updates the page when saving is blocked', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const { setLocale, getLocale } = await import('../index')
    await setLocale('zh')
    expect(getLocale()).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
    expect(document.title).toBe('Localized page')
  })

  it('keeps the selected Traditional glyphs across all Hong Kong messages', () => {
    const inlineStart = inlineSource.indexOf(' = ') + 3
    const inlineEnd = inlineSource.indexOf('\n\nexport ')
    const inlineMessages = JSON.parse(inlineSource.slice(inlineStart, inlineEnd)) as Record<string, string>
    const forbidden = /[户帐賬设录电说阅兑脱温税悦卧啓説閲]/u
    const inspect = (node: unknown, path: string): void => {
      if (typeof node === 'string') {
        expect(node, path).not.toMatch(forbidden)
        return
      }
      for (const [key, child] of Object.entries(node as Record<string, unknown>)) {
        inspect(child, `${path}.${key}`)
      }
    }
    inspect(zhHK, 'zh-Hant-HK')
    inspect(inlineMessages, 'inline')
    expect(zhHK.auth.signInToAccount).toBe('登入您的帳戶以繼續')
    expect(zhHK.auth.dontHaveAccount).toBe('還沒有帳戶？')
    expect(localizeInlineText('查询用户 ID 模板', 'User ID template', 'zh-Hant-HK')).toBe('查詢用戶 ID 模板')
  })

  it('uses Hong Kong terminology without altering legacy Chinese', () => {
    expect(zhHK.common.settings).toBe('設定')
    expect(zhHK.common.email).toBe('電郵')
    expect(localizeInlineText('保存打票策略', 'Save harvest policy', 'zh-Hant-HK')).toBe('儲存打票策略')
    expect(localizeInlineText('保存打票策略', 'Save harvest policy', 'zh')).toBe('保存打票策略')
    expect(localizeInlineText('new source', 'New English text', 'zh-Hant-HK')).toBe('New English text')
  })
})
