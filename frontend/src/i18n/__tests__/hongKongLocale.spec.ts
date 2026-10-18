import { beforeEach, describe, expect, it, vi } from 'vitest'
import zhHK from '../locales/zh-Hant-HK'
import { localizeInlineText } from '../inline'
import inlineSource from '../inline?raw'

beforeEach(() => {
  vi.resetModules()
  localStorage.clear()
  document.documentElement.removeAttribute('lang')
})

describe('Hong Kong locale selection', () => {
  it('starts in English even when browser language is Chinese', async () => {
    vi.spyOn(navigator, 'language', 'get').mockReturnValueOnce('zh-HK')
    const { getLocale } = await import('../index')
    expect(getLocale()).toBe('en')
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

  it('rejects unknown persisted locale values', async () => {
    localStorage.setItem('sub2api_locale', 'unexpected')
    const { getLocale } = await import('../index')
    expect(getLocale()).toBe('en')
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
