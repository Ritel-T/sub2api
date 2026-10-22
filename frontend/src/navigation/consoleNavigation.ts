import type { SearchNavItem } from '@/utils/featureSearch'

export function consoleNavigationText(locale: string) {
  if (locale.startsWith('zh')) {
    const traditional = locale === 'zh-Hant' || /HK|TW/i.test(locale)
    return traditional
      ? {
          ownKeys: '我的 API Keys',
          workspace: '工作區',
          management: '管理',
          personal: '個人',
          navigation: '功能導覽',
          models: '模型',
          analysis: '用量分析',
          records: '請求記錄',
          sections: {
            overview: '總覽',
            access: '接入',
            usage: '用量',
            billing: '帳務',
            support: '支援',
            account: '帳戶',
            resources: '資源',
            users: '用戶',
            operations: '運維',
            revenue: '營收',
            system: '系統',
            extensions: '擴充',
            more: '更多',
          },
        }
      : {
          ownKeys: '我的 API Keys',
          workspace: '工作区',
          management: '管理',
          personal: '个人',
          navigation: '功能导航',
          models: '模型',
          analysis: '用量分析',
          records: '请求记录',
          sections: {
            overview: '总览',
            access: '接入',
            usage: '用量',
            billing: '账务',
            support: '支持',
            account: '账户',
            resources: '资源',
            users: '用户',
            operations: '运维',
            revenue: '营收',
            system: '系统',
            extensions: '扩展',
            more: '更多',
          },
        }
  }
  return {
    ownKeys: 'My API Keys',
    workspace: 'Workspace',
    management: 'Management',
    personal: 'Personal',
    navigation: 'Navigation',
    models: 'Models',
    analysis: 'Usage analytics',
    records: 'Request records',
    sections: {
      overview: 'Overview',
      access: 'Access',
      usage: 'Usage',
      billing: 'Billing',
      support: 'Support',
      account: 'Account',
      resources: 'Resources',
      users: 'Users',
      operations: 'Operations',
      revenue: 'Revenue',
      system: 'System',
      extensions: 'Extensions',
      more: 'More',
    },
  }
}
export type ConsoleNavigationText = ReturnType<typeof consoleNavigationText>
export type ConsoleWorkspace = 'personal' | 'management'

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
  '/admin/ip-audit': 'IP audit 风险 風險 封禁 审计 審計',
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
  '/security-records': 'Security records 安全 記錄 记录 限制',
  '/monitor': 'Service channel status 服務 服务 狀態 状态',
  '/support-tickets': 'Support help 工单 工單 帮助 幫助',
}

export interface ConsoleNavItem extends SearchNavItem {
  icon: unknown
  iconSvg?: string
  children?: ConsoleNavItem[]
  badge?: () => number
  mergeTabs?: boolean
}

export interface ConsoleSection {
  key: string
  label: string
  path: string
  tabs: ConsoleTab[]
  badge?: () => number
  hidden?: boolean
}

export interface ConsoleTab {
  item: ConsoleNavItem
  path: string
  label: string
  matches: string[]
  children?: ConsoleNavItem[]
}

type SectionKey = keyof ConsoleNavigationText['sections']

const personalTable: Record<string, SectionKey> = {
  '/dashboard': 'overview',
  '/keys': 'access',
  '/model-plaza': 'access',
  '/batch-image': 'access',
  '/available-channels': 'access',
  '/pelican-showcase': 'access',
  '/usage?view=analysis': 'usage',
  '/usage?view=records': 'usage',
  '/purchase': 'billing',
  '/subscriptions': 'billing',
  '/orders': 'billing',
  '/redeem': 'billing',
  '/affiliate': 'billing',
  '/support-tickets': 'support',
  '/monitor': 'support',
  '/profile': 'account',
  '/security-records': 'support',
}

const managementTable: Record<string, SectionKey> = {
  '/admin/dashboard': 'overview',
  '/admin/accounts': 'resources',
  '/admin/groups': 'resources',
  '/admin/channels/pricing': 'resources',
  '/admin/proxies': 'resources',
  '/keys': 'resources',
  '/admin/users': 'users',
  '/admin/subscriptions': 'users',
  '/admin/usage': 'operations',
  '/admin/ops': 'operations',
  '/admin/auto-config': 'operations',
  '/admin/priority-scheduling': 'operations',
  '/admin/account-quality': 'operations',
  '/admin/controlled-experiments': 'operations',
  '/admin/account-ops': 'operations',
  '/admin/token-guard': 'operations',
  '/admin/token-guard-v2': 'operations',
  '/admin/pelican-tests': 'operations',
  '/admin/group-cache-tests': 'operations',
  '/admin/request-captures': 'operations',
  '/admin/harvest-flow': 'operations',
  '/admin/channels/monitor': 'operations',
  '/admin/risk-control': 'operations',
  '/admin/prompt-audit': 'operations',
  '/admin/ip-audit': 'operations',
  '/admin/orders/dashboard': 'revenue',
  '/admin/orders': 'revenue',
  '/admin/orders/plans': 'revenue',
  '/admin/redeem': 'revenue',
  '/admin/promo-codes': 'revenue',
  '/admin/affiliates/invites': 'revenue',
  '/admin/affiliates/rebates': 'revenue',
  '/admin/affiliates/transfers': 'revenue',
  '/admin/support-tickets': 'support',
  '/admin/announcements': 'support',
  '/admin/settings': 'system',
  '/admin/plugins': 'system',
  '/admin/audit-logs': 'system',
}

const sectionOrder: Record<ConsoleWorkspace, SectionKey[]> = {
  personal: ['overview', 'access', 'usage', 'billing', 'support', 'account'],
  management: ['overview', 'resources', 'users', 'operations', 'revenue', 'support', 'system'],
}
// Reached through the avatar menu and search; a top-level link would duplicate them.
const hiddenSections = new Set<SectionKey>(['account'])

function lookup(table: Record<string, SectionKey>, path: string): [SectionKey, number] | undefined {
  const keys = Object.keys(table)
  for (const candidate of [path, path.split('?')[0]!]) {
    const index = keys.indexOf(candidate)
    if (index >= 0) return [table[candidate]!, index]
  }
  return undefined
}

function classify(path: string, workspace: ConsoleWorkspace): [SectionKey, number] {
  if (path.startsWith('/custom/')) return ['extensions', 0]
  const own = workspace === 'personal' ? personalTable : managementTable
  const other = workspace === 'personal' ? managementTable : personalTable
  return lookup(own, path) ?? lookup(other, path) ?? ['extensions', 0]
}

function itemBadge(item: ConsoleNavItem): number {
  return (item.badge?.() ?? 0) + (item.children?.reduce((sum, child) => sum + itemBadge(child), 0) ?? 0)
}

function withKeywords(item: ConsoleNavItem, parents: string[]): ConsoleNavItem {
  const alias = aliases[item.path] ?? aliases[item.path.split('?')[0]!] ?? ''
  return { ...item, children: undefined, keywords: [item.keywords, ...parents, alias].filter(Boolean).join(' ') }
}

// Source items must already be filtered by permissions, feature flags and run mode.
export function arrangeConsoleSections(
  source: ConsoleNavItem[], workspace: ConsoleWorkspace, text: ConsoleNavigationText,
): ConsoleSection[] {
  const placed: { key: SectionKey; index: number; order: number; tab: ConsoleTab }[] = []
  const visit = (items: ConsoleNavItem[], parents: string[]) => {
    for (const item of items) {
      if (item.mergeTabs && item.children?.length) {
        const children = item.children.map(child => withKeywords(child, [...parents, item.label]))
        const [key, index] = classify(children[0]!.path, workspace)
        placed.push({ key, index, order: placed.length, tab: { item, path: children[0]!.path, label: item.label, matches: children.map(child => child.path), children } })
        continue
      }
      if (!item.expandOnly) {
        const leaf = withKeywords(item, parents)
        const [key, index] = classify(item.path, workspace)
        placed.push({ key, index, order: placed.length, tab: { item: leaf, path: item.path, label: item.label, matches: [item.path] } })
      }
      if (item.children) visit(item.children, [...parents, item.label])
    }
  }
  visit(source, [])

  const own = sectionOrder[workspace]
  const other = sectionOrder[workspace === 'personal' ? 'management' : 'personal']
  const keys = [...own, ...other.filter(key => !own.includes(key)), 'extensions' as SectionKey]
  return keys.flatMap((key): ConsoleSection[] => {
    const tabs = placed
      .filter(entry => entry.key === key)
      .sort((a, b) => a.index - b.index || a.order - b.order)
      .map(entry => entry.tab)
    if (!tabs.length) return []
    return [{
      key,
      label: text.sections[key],
      path: tabs[0]!.path,
      tabs,
      badge: () => tabs.reduce((sum, tab) => sum + tabBadge(tab), 0),
      hidden: hiddenSections.has(key) || undefined,
    }]
  })
}

export function sectionBadge(section: ConsoleSection): number {
  return section.badge?.() ?? 0
}

export function tabBadge(tab: ConsoleTab): number {
  return itemBadge(tab.children ? { ...tab.item, children: tab.children } : tab.item)
}

// Longest matching base path wins; an exact query match breaks ties, so
// /usage?view=records is chosen over analysis when the query says so.
export function resolveConsoleLocation(sections: ConsoleSection[], path: string, query: Record<string, unknown>):
  { section: ConsoleSection; tab: ConsoleTab } | undefined {
  let best: { section: ConsoleSection; tab: ConsoleTab; score: number } | undefined
  for (const section of sections) {
    for (const tab of section.tabs) {
      for (const match of tab.matches) {
        const base = match.split('?')[0]!
        if (path !== base && !path.startsWith(`${base}/`)) continue
        const score = base.length * 2 + (isConsoleLocationActive(match, path, query) ? 1 : 0)
        if (!best || score > best.score) best = { section, tab, score }
      }
    }
  }
  return best && { section: best.section, tab: best.tab }
}

// Search keeps the section → tab hierarchy so results read "Billing / Orders".
export function consoleSearchTree(sections: ConsoleSection[]): ConsoleNavItem[] {
  return sections.map(section => ({
    path: `#console-section-${section.key}`,
    label: section.label,
    icon: null,
    expandOnly: true,
    children: section.tabs.map(tab => tab.children
      ? { path: `#console-tab-${tab.item.path.replace(/[^a-z0-9]/gi, '-')}`, label: tab.label, icon: null, expandOnly: true, children: tab.children }
      : tab.item),
  }))
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
