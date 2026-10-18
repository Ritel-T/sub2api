import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const copy = {
  en: { application: 'Application', connectionMethod: 'Connection method', connect: 'Connect an app', requests: 'Requests', advanced: 'Limits and advanced settings', copyKey: 'Copy API key', appConfiguration: 'Choose app and configure', chooseKey: 'Choose an API key', changeKey: 'Use another key', search: 'Search', previous: 'Previous', next: 'Next' },
  hk: { application: '應用程式', connectionMethod: '連線方式', connect: '連接應用程式', requests: '請求記錄', advanced: '限制與進階設定', copyKey: '複製 API Key', appConfiguration: '選擇應用程式並設定', chooseKey: '選擇 API Key', changeKey: '使用其他 Key', search: '搜尋', previous: '上一頁', next: '下一頁' },
  cn: { application: '应用', connectionMethod: '连接方式', connect: '连接应用', requests: '请求记录', advanced: '限制与高级设置', copyKey: '复制 API Key', appConfiguration: '选择应用并配置', chooseKey: '选择 API Key', changeKey: '使用其他 Key', search: '搜索', previous: '上一页', next: '下一页' },
}
export function useKeyConnectionCopy() {
  const { locale } = useI18n()
  return computed(() => /^zh/i.test(locale?.value || '') ? (/^zh[-_]CN$/i.test(locale?.value || '') ? copy.cn : copy.hk) : copy.en)
}
