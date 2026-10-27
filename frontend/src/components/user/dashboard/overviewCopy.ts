import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const en = {
  invite: 'Invite friends. Share the rewards.', inviteHint: 'Recommend Ritel.SI and earn rewards from qualifying referrals.', inviteAction: 'Invite friends',
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
  domestic: 'Chinese models', topUp: 'Top up', quickStart: 'Create a key by platform', quickHint: 'Choose a platform to create a key and get connection instructions.', recentKeys: 'Recent API keys', useKey: 'Use', noKeys: 'No API keys yet', noGroups: 'No available platform groups.', creating: 'Creating…', createFailed: 'Could not create the key. Please try again.', createUnknown: 'Creation could not be confirmed. Check your API keys before trying again.',
}
const hk: Record<keyof typeof en, string> = {
  invite: '推薦好友，共享獎勵', inviteHint: '分享 Ritel.SI，透過符合條件的推薦獲得獎勵。', inviteAction: '邀請好友',
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
  domestic: '國模', topUp: '充值', quickStart: '按平台建立 Key', quickHint: '選擇平台，即可建立 Key 並查看接入方法。', recentKeys: '最近建立的 Key', useKey: '使用', noKeys: '暫無 API Key', noGroups: '暫無可用的平台分組。', creating: '建立中…', createFailed: '未能建立 Key，請重試。', createUnknown: '尚未確認建立結果，請先查看 API Key 列表。',
}
const zh: Record<keyof typeof en, string> = {
  invite: '推荐好友，共享奖励', inviteHint: '分享 Ritel.SI，通过符合条件的推荐获得奖励。', inviteAction: '邀请好友',
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
  domestic: '国模', topUp: '充值', quickStart: '按平台创建 Key', quickHint: '选择平台，即可创建 Key 并查看接入方法。', recentKeys: '最近创建的 Key', useKey: '使用', noKeys: '暂无 API Key', noGroups: '暂无可用的平台分组。', creating: '创建中…', createFailed: '未能创建 Key，请重试。', createUnknown: '尚未确认创建结果，请先查看 API Key 列表。',
}
export function useOverviewCopy() {
  const { locale } = useI18n()
  return computed(() => locale.value === 'zh' || locale.value === 'zh-CN' ? zh
    : /^(zh|yue)/i.test(locale.value) ? hk : en)
}
