import type { LocationQuery } from 'vue-router'
import { consoleUiMessages } from '@/i18n/consoleCopy'

export function consoleRouteTitle(path: string, query: LocationQuery, locale: string, observer = false): string | undefined {
  const copy = consoleUiMessages(locale).navigation
  if (path === '/admin/usage' || (path === '/usage' && observer)) {
    const analysis = query.view === 'analysis' || query.view === 'ranking' || query.tab === 'ranking'
    return analysis ? copy.analysis : copy.records
  }
  if (path !== '/usage') return undefined
  if (query.view === 'analysis') return copy.analysis
  if (query.view === 'errors' || query.tab === 'errors') return consoleUiMessages(locale).failedRequests
  return copy.records
}
