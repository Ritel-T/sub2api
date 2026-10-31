import type { createI18n as CreateI18n } from 'vue-i18n'
import enInline from '@/i18n/locales/en/consoleInline'
import zhInline from '@/i18n/locales/zh/consoleInline'
import hkInline from '@/i18n/locales/zh-Hant-HK/consoleInline'
import enFields from '@/i18n/locales/en/consoleFields'
import zhFields from '@/i18n/locales/zh/consoleFields'
import hkFields from '@/i18n/locales/zh-Hant-HK/consoleFields'

// Existing business tests deliberately echo unrelated translation keys. Render
// the migrated copy with the real composer so their behavior assertions remain
// about the same visible messages, including ICU interpolation.
export function consoleTestT(createI18n: typeof CreateI18n, locale: string | (() => string), fallback: (key: string, ...args: any[]) => string = key => key) {
  const composer = createI18n({ legacy: false, locale: 'en', messages: {
    en: { consoleInline: enInline, consoleFields: enFields },
    zh: { consoleInline: zhInline, consoleFields: zhFields },
    'zh-Hant-HK': { consoleInline: hkInline, consoleFields: hkFields }
  }}).global
  return (key: string, ...args: any[]) => {
    if (!key.startsWith('consoleInline.') && !key.startsWith('consoleFields.')) return fallback(key, ...args)
    const current = typeof locale === 'function' ? locale() : locale
    composer.locale.value = current === 'zh-Hant-HK' ? current : current.startsWith('zh') ? 'zh' : 'en'
    return (composer.t as (key: string, ...args: any[]) => string)(key, ...args)
  }
}
