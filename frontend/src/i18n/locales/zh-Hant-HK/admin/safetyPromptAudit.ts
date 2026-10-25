export default {
  safetyPromptAudit: {
    nav: '安全注入審計',
    title: '安全注入審計',
    description: '向 OpenAI OAuth 請求注入固定安全政策，並在模型按本次識別碼明確拒絕時，由宿主攔截及記錄可複核事件。',
    module: '模組設定',
    enabled: '啟用安全注入',
    enabledHint: '啟用後向本實例支援的 OpenAI OAuth 出站請求注入固定安全政策。預設關閉。',
    signalEnabled: '啟用獨立拒絕識別',
    signalHint: '模型完整傳回本次 sp- 識別碼時，宿主會替換為固定拒絕回答並記錄事件。僅支援 Responses 鏈路。',
    retentionDays: '命中事件保留天數',
    retentionHint: '範圍 1–90 天，最多保留最近 500 條；到期或因容量限制而移除後無法恢復。',
    save: '儲存設定',
    saving: '儲存中…',
    saved: '設定已儲存',
    refresh: '重新整理',
    loading: '正在讀取…',
    loadFailed: '讀取安全注入審計失敗，請重試。',
    saveFailed: '儲存安全注入設定失敗，請檢查輸入後重試。',
    storageReady: '事件持久儲存正常 · 目前 {count}/{limit} 條',
    storageWarning: '儲存警告：{warning}',
    disabledNotice: '安全注入模組目前關閉；歷史事件仍可查看。',
    filters: {
      from: '開始日期（本地時間）',
      to: '結束日期（含當天）',
      search: '搜尋事件編號 / 請求編號 / 模型 / 摘要',
      all: '全部保留記錄',
      today: '今天'
    },
    count: '顯示 {shown} / {total} 條。均為模型精確命中，尚未經人工確認。',
    badRange: '開始日期不能晚於結束日期。',
    empty: '沒有符合條件的記錄。',
    table: {
      time: '時間 / 事件編號',
      model: '模型 / 上游帳戶',
      user: '請求用戶',
      summary: '已隱去敏感資料的摘要'
    },
    unknownUser: '未知（宿主未關聯）',
    requestId: '請求編號',
    upstreamAccount: '上游帳戶',
    exportJson: '匯出 JSON',
    exportCsv: '匯出 CSV',
    noSummary: '無文字摘要',
    scopeNote: '僅涵蓋經過支援發送點的 OpenAI OAuth 請求；不修改上游 cyber_policy 判定，不自動封號。'
  }
}
