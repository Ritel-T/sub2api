import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const en = {
  overview: 'Overview', create: 'Create API key', connect: 'Connect an app', continue: 'Continue using',
  manageKeys: 'Manage API keys', loading: 'Loading…', retry: 'Retry', loadFailed: 'Could not load this section.',
  balance: 'Available balance', subscriptions: 'Active subscriptions', billing: 'Balance & billing',
  requests: 'Requests today', tokens: 'Tokens today', cost: 'Spent today', keys: 'Active API keys',
  recent: 'Recent requests', allRequests: 'View all requests', noRequests: 'No requests yet',
  model: 'Model', time: 'Time', actual: 'Charged', standard: 'Standard price', tokensUnit: 'tokens',
  attention: 'Needs attention', errors: 'Failed requests today', viewErrors: 'View failures',
  noCredit: 'No balance or active subscription', expires: 'Subscription ending soon',
  fullStats: 'All statistics & quotas', analysis: 'Usage analysis', refresh: 'Refresh',
  availableKey: 'API key', noActiveKey: 'No usable API key', subscriptionEnds: 'Ends',
  quotaFailed: 'Could not load platform quotas.',
}
const hk: Record<keyof typeof en, string> = {
  overview: '總覽', create: '建立 API Key', connect: '連接應用程式', continue: '繼續使用',
  manageKeys: '管理 API Key', loading: '載入中…', retry: '重試', loadFailed: '未能載入此部分。',
  balance: '可用餘額', subscriptions: '有效訂閱', billing: '餘額與帳務',
  requests: '今日請求', tokens: '今日 Token', cost: '今日支出', keys: '有效 API Key',
  recent: '最近請求', allRequests: '查看全部請求', noRequests: '暫無請求',
  model: '模型', time: '時間', actual: '實際扣款', standard: '標準價格', tokensUnit: 'Token',
  attention: '需要處理', errors: '今日失敗請求', viewErrors: '查看失敗請求',
  noCredit: '餘額不足且沒有有效訂閱', expires: '訂閱即將到期',
  fullStats: '完整統計與配額', analysis: '用量分析', refresh: '重新整理',
  availableKey: 'API Key', noActiveKey: '沒有可用的 API Key', subscriptionEnds: '到期',
  quotaFailed: '未能載入平台配額。',
}
const zh: Record<keyof typeof en, string> = {
  overview: '总览', create: '创建 API Key', connect: '连接应用', continue: '继续使用',
  manageKeys: '管理 API Key', loading: '加载中…', retry: '重试', loadFailed: '未能加载此部分。',
  balance: '可用余额', subscriptions: '有效订阅', billing: '余额与账务',
  requests: '今日请求', tokens: '今日 Token', cost: '今日支出', keys: '有效 API Key',
  recent: '最近请求', allRequests: '查看全部请求', noRequests: '暂无请求',
  model: '模型', time: '时间', actual: '实际扣款', standard: '标准价格', tokensUnit: 'Token',
  attention: '需要处理', errors: '今日失败请求', viewErrors: '查看失败请求',
  noCredit: '余额不足且没有有效订阅', expires: '订阅即将到期',
  fullStats: '完整统计与配额', analysis: '用量分析', refresh: '刷新',
  availableKey: 'API Key', noActiveKey: '没有可用的 API Key', subscriptionEnds: '到期',
  quotaFailed: '未能加载平台配额。',
}
export function useOverviewCopy() {
  const { locale } = useI18n()
  return computed(() => locale.value === 'zh' || locale.value === 'zh-CN' ? zh
    : /^(zh|yue)/i.test(locale.value) ? hk : en)
}
