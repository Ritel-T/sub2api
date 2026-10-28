import type { LocationQuery } from 'vue-router'

export function consoleRouteTitle(path: string, query: LocationQuery, locale: string, observer = false): string | undefined {
  const hk = locale.startsWith('zh-Hant')
  const chinese = locale.startsWith('zh')
  if (path === '/admin/usage' || (path === '/usage' && observer)) {
    const analysis = query.view === 'analysis' || query.view === 'ranking' || query.tab === 'ranking'
    return analysis ? chinese ? '用量分析' : 'Usage analytics'
      : chinese ? hk ? '請求記錄' : '请求记录' : 'Request records'
  }
  if (path !== '/usage') return undefined
  if (query.view === 'analysis') return chinese ? '用量分析' : 'Usage analytics'
  if (query.view === 'errors' || query.tab === 'errors') return chinese ? (hk ? '失敗請求' : '失败请求') : 'Failed requests'
  return chinese ? (hk ? '請求記錄' : '请求记录') : 'Request records'
}
