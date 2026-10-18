import type { SearchNavItem } from '@/utils/featureSearch'

export function consoleNavigationText(locale: string) {
  if (locale.startsWith('zh')) {
    const traditional = locale === 'zh-Hant' || /HK|TW/i.test(locale)
    return traditional
      ? { ownKeys: '我的 API Keys', workspace: '工作區', management: '管理', personal: '個人', navigation: '功能導覽', models: '模型', analysis: '用量分析', records: '請求記錄', billing: '帳務', applications: '應用與服務', operations: '運維與診斷', system: '系統管理', extensions: '擴充功能' }
      : { ownKeys: '我的 API Keys', workspace: '工作区', management: '管理', personal: '个人', navigation: '功能导航', models: '模型', analysis: '用量分析', records: '请求记录', billing: '账务', applications: '应用与服务', operations: '运维与诊断', system: '系统管理', extensions: '扩展功能' }
  }
  return { ownKeys: 'My API Keys', workspace: 'Workspace', management: 'Management', personal: 'Personal', navigation: 'Navigation', models: 'Models', analysis: 'Usage analytics', records: 'Request records', billing: 'Billing', applications: 'Apps & services', operations: 'Operations & diagnostics', system: 'System', extensions: 'Extensions' }
}
export type ConsoleNavigationText = ReturnType<typeof consoleNavigationText>

const aliases: Record<string, string> = {
  '/admin/dashboard': 'Overview 总览 總覽 仪表盘 儀表板',
  '/admin/users': 'Users 用户 用戶',
  '/admin/accounts': 'Upstream accounts 上游 账号 帳號 賬號',
  '/admin/groups': 'Groups 分组 分組',
  '/admin/channels/pricing': 'Channel pricing 渠道 价格 價格 定价 定價',
  '/admin/channels/monitor': 'Channel monitoring 渠道 监控 監控',
  '/admin/usage': 'Usage request logs 用量 请求 請求 记录 記錄',
  '/admin/settings': 'Settings 系统 系統 设置 設定 設置',
  '/admin/ops': 'Operations monitoring 运维 運維 监控 監控',
  '/admin/auto-config': 'Auto configuration 自动 自動 配置',
  '/admin/priority-scheduling': 'Priority scheduling 优先级 優先級 调度 調度',
  '/admin/account-quality': 'Account quality 账号 帳號 质量 質量',
  '/admin/controlled-experiments': 'Controlled experiments 对照 對照 实验 實驗',
  '/admin/account-ops': 'Account operations 账号 帳號 运维 運維',
  '/admin/token-guard': 'Token guard V1 凭据 憑據 保护 保護',
  '/admin/token-guard-v2': 'Credential protection Token guard V2 凭据 憑據 保护 保護',
  '/admin/pelican-tests': 'Pelican tests 鹈鹕 鵜鶘 测试 測試',
  '/admin/group-cache-tests': 'Group cache test 分组 分組 缓存 快取 测试 測試',
  '/admin/request-captures': 'Request captures 请求 請求 抓取 捕获 擷取',
  '/admin/harvest-flow': 'Harvest flow ticket 工单 工單 流程',
  '/admin/plugins': 'Plugins 插件 外掛',
  '/admin/announcements': 'Announcements 公告',
  '/admin/support-tickets': 'Support tickets 工单 工單 支援',
  '/admin/proxies': 'Proxies 代理',
  '/admin/risk-control': 'Risk control content moderation 风控 風控 内容 內容 审核 審核',
  '/admin/prompt-audit': 'Prompt audit 提示词 提示詞 审计 審計',
  '/admin/redeem': 'Redeem codes 兑换 兌換',
  '/admin/promo-codes': 'Promotion codes 优惠 優惠',
  '/admin/subscriptions': 'Subscriptions 订阅 訂閱 套餐',
  '/admin/affiliates/invites': 'Affiliate invites 邀请 邀請 记录 記錄',
  '/admin/affiliates/rebates': 'Affiliate rebates 返利 记录 記錄',
  '/admin/affiliates/transfers': 'Affiliate transfers 转账 轉帳 记录 記錄',
  '/admin/orders': 'Orders 訂單 订单',
  '/admin/orders/dashboard': 'Payment billing dashboard 支付 账务 帳務 总览 總覽',
  '/admin/orders/plans': 'Payment plans 套餐 方案',
  '/admin/audit-logs': 'Audit logs 审计 審計 日志 日誌',
  '/dashboard': 'Overview 总览 總覽 仪表盘 儀表板',
  '/keys': 'API keys 密钥 密鑰 接入 连接 連接 Connect SDK client',
  '/model-plaza': 'Models 模型 价格 價格 pricing capabilities',
  '/usage?view=analysis': 'Usage analytics 用量 分析 消耗 消費 統計 统计 tokens cost',
  '/usage?view=records': 'Request records logs 请求 請求 记录 記錄 日志 日誌 错误 錯誤',
  '/purchase': 'Balance top up recharge subscription 充值 余额 餘額 套餐',
  '/orders': 'Orders payment 支付 訂單 订单 付款',
  '/redeem': 'Redeem code 兑换 兌換',
  '/affiliate': 'Invite referral rebate 邀请 邀請 返利',
  '/profile': 'Account security password MFA 帳戶 账户 密码 密碼 安全',
  '/monitor': 'Service channel status 服務 服务 狀態 状态',
  '/support-tickets': 'Support help 工单 工單 帮助 幫助',
}

export interface ConsoleNavItem extends SearchNavItem {
  icon: unknown
  iconSvg?: string
  children?: ConsoleNavItem[]
  badge?: () => number
}

// Accept only the source already filtered for role, feature flags and run mode.
// Every navigable leaf survives once; structural groups carry no destination.
export function arrangeConsoleNavigation<T extends ConsoleNavItem>(
  source: T[], workspace: 'personal' | 'management', text: ConsoleNavigationText,
  icons: Record<'billing' | 'applications' | 'operations' | 'system' | 'extensions', unknown>,
): T[] {
  const leaves: T[] = []
  const visit = (items: T[], parents: string[] = []) => {
    for (const item of items) {
      if (!item.expandOnly) leaves.push({ ...item, children: undefined, keywords: [item.keywords, ...parents, aliases[item.path] ?? aliases[item.path.split('?')[0]!] ?? ''].filter(Boolean).join(' ') })
      if (item.children) visit(item.children as T[], [...parents, item.label])
    }
  }
  visit(source)
  const core: T[] = []
  const buckets: Record<string, T[]> = { applications: [], billing: [], operations: [], system: [], extensions: [] }
  for (const item of leaves) {
    const path = item.path.split('?')[0]!
    if (path.startsWith('/custom/')) { buckets.extensions!.push(item); continue }
    if (workspace === 'personal') {
      if (['/purchase', '/subscriptions', '/orders', '/redeem', '/affiliate'].includes(path)) buckets.billing!.push(item)
      else if (['/batch-image', '/available-channels', '/monitor', '/pelican-showcase'].includes(path)) buckets.applications!.push(item)
      else core.push(item)
    } else {
      if (['/admin/dashboard', '/admin/accounts', '/admin/groups', '/admin/users', '/admin/usage', '/admin/support-tickets', '/keys'].includes(path)) core.push(item)
      else if (path.startsWith('/admin/orders') || path.startsWith('/admin/affiliates/') || ['/admin/subscriptions', '/admin/redeem', '/admin/promo-codes'].includes(path)) buckets.billing!.push(item)
      else if (['/admin/settings', '/admin/plugins', '/admin/announcements', '/admin/audit-logs'].includes(path)) buckets.system!.push(item)
      else buckets.operations!.push(item)
    }
  }
  const ordered = workspace === 'personal'
    ? ['/dashboard', '/keys', '/model-plaza', '/usage?view=analysis', '/usage?view=records', '/support-tickets', '/profile']
    : ['/admin/dashboard', '/admin/accounts', '/admin/groups', '/admin/users', '/admin/usage', '/admin/support-tickets', '/keys']
  core.sort((a, b) => ordered.indexOf(a.path.startsWith('/model-plaza') ? '/model-plaza' : a.path) - ordered.indexOf(b.path.startsWith('/model-plaza') ? '/model-plaza' : b.path))
  const utilityPaths = new Set(workspace === 'personal' ? ['/support-tickets', '/profile'] : ['/keys'])
  const primary = core.filter(item => !utilityPaths.has(item.path))
  const utility = core.filter(item => utilityPaths.has(item.path))
  const groups = (Object.keys(buckets) as (keyof typeof icons)[]).flatMap(key => buckets[key]!.length ? [{ path: `#console-${workspace}-${key}`, label: text[key], icon: icons[key], expandOnly: true, children: buckets[key] } as unknown as T] : [])
  return [...primary, ...groups, ...utility]
}

export function isConsoleLocationActive(destination: string, currentPath: string, query: Record<string, unknown>): boolean {
  const [path, search] = destination.split('?')
  if (currentPath !== path && !currentPath.startsWith(`${path}/`)) return false
  if (!search) return true
  return [...new URLSearchParams(search)].every(([key, value]) => {
    const actual = query[key] ?? (key === 'view' && currentPath === '/usage' ? 'records' : undefined)
    return actual === value
  })
}


const workspaceQueryValues: Record<string, readonly string[]> = {
  view: ['analysis', 'records'], embedded: ['1'],
  tab: ['general', 'agreement', 'features', 'security', 'users', 'gateway', 'payment', 'email', 'backup'],
  status: ['active', 'inactive', 'enabled', 'disabled', 'success', 'error', 'failed', 'pending', 'paid', 'completed', 'cancelled', 'expired', 'all'],
  platform: ['claude', 'openai', 'gemini', 'antigravity', 'all'],
  type: ['recharge', 'subscription', 'payment', 'all'],
}
function navigableLeaves(items: ConsoleNavItem[]): ConsoleNavItem[] {
  return items.flatMap(item => [...(!item.expandOnly ? [item] : []), ...(item.children ? navigableLeaves(item.children) : [])])
}
// Store only recognized navigation paths and structural filters, never free text,
// API keys, request contents or arbitrary query values.
export function workspaceLocation(path: string, query: Record<string, unknown>, allowed: ConsoleNavItem[]): string | undefined {
  if (!navigableLeaves(allowed).some(item => item.path.split('?')[0] === path)) return undefined
  const clean = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (typeof value !== 'string') continue
    if (workspaceQueryValues[key]?.includes(value) || (['page', 'page_size'].includes(key) && /^[1-9][0-9]{0,5}$/.test(value))) clean.set(key, value)
  }
  if (path === '/model-plaza') clean.set('embedded', '1')
  const search = clean.toString()
  return path + (search ? `?${search}` : '')
}
export function legalWorkspaceDestination(saved: string | null, allowed: ConsoleNavItem[], fallback: string): string {
  if (!saved || !saved.startsWith('/') || saved.startsWith('//') || saved.length > 512) return fallback
  try {
    const parsed = new URL(saved, 'https://console.invalid')
    return workspaceLocation(parsed.pathname, Object.fromEntries(parsed.searchParams), allowed) ?? fallback
  } catch { return fallback }
}
