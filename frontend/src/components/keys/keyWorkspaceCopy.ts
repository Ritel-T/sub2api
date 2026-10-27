import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const copy = {
  en: { recordedTotal: 'Recorded total', use: 'Use', rename: 'Rename', showDetails: 'Show details', hideDetails: 'Hide details', details: 'Details', nameRequired: 'Enter a name.', saving: 'Saving', running: 'Running', waiting: 'Waiting', concurrent: 'Concurrent', unlimited: 'Unlimited' },
  hk: { recordedTotal: '累計實扣（保留記錄）', use: '使用', rename: '重新命名', showDetails: '展開詳情', hideDetails: '收起詳情', details: '詳情', nameRequired: '請輸入名稱。', saving: '儲存中', running: '執行', waiting: '等候', concurrent: '並行', unlimited: '不限' },
  cn: { recordedTotal: '累计实扣（保留记录）', use: '使用', rename: '重命名', showDetails: '展开详情', hideDetails: '收起详情', details: '详情', nameRequired: '请输入名称。', saving: '保存中', running: '执行', waiting: '等待', concurrent: '并发', unlimited: '不限' },
}

export function useKeyWorkspaceCopy() {
  const { locale } = useI18n()
  return computed(() => /^zh/i.test(locale?.value || '') ? (/^zh[-_]CN$/i.test(locale?.value || '') ? copy.cn : copy.hk) : copy.en)
}
