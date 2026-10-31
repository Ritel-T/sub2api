import en from './locales/en/consoleUi'
import zh from './locales/zh/consoleUi'
import hk from './locales/zh-Hant-HK/consoleUi'

// Pure helpers receive a locale explicitly (navigation, titles, shell and overview).
// Keeping their copy here also puts every message through the locale build gate.
export function consoleUiMessages(locale: string): typeof en {
  if (/Hant|HK|TW|^yue/i.test(locale)) return hk
  return locale.startsWith('zh') ? zh : en
}
