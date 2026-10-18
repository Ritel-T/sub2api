import type { LocationQuery } from 'vue-router'

export function consoleRouteTitle(path: string, query: LocationQuery, locale: string, observer = false): string | undefined {
  if (path !== '/usage' || observer) return undefined
  const hk = locale.startsWith('zh-Hant')
  const chinese = locale.startsWith('zh')
  if (query.view === 'analysis') return chinese ? '用量分析' : 'Usage analytics'
  if (query.view === 'errors' || query.tab === 'errors') return chinese ? (hk ? '失敗請求' : '失败请求') : 'Failed requests'
  return chinese ? (hk ? '請求記錄' : '请求记录') : 'Request records'
}

export function consoleWorkspaceLabel(path: string, role: string | undefined, locale: string): string {
  const hk = locale.startsWith('zh-Hant')
  if (role === 'observer') return locale.startsWith('zh') ? (hk ? '觀察員' : '观察员') : 'Observer'
  if (path.startsWith('/admin')) return locale.startsWith('zh') ? '管理' : 'Administration'
  if (role === 'admin') return locale.startsWith('zh') ? (hk ? '我的帳戶' : '我的账户') : 'My account'
  return ''
}
