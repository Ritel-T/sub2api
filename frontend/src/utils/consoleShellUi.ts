import { consoleUiMessages } from '@/i18n/consoleCopy'
export function consoleShellCopy(locale: string) {
  const copy = consoleUiMessages(locale).shell
  return { ...copy, unread: (count: number) => copy.unread.replace('{count}', String(count)) }
}

export function formatNavBadge(count: number): string {
  return count > 99 ? '99+' : String(count)
}
