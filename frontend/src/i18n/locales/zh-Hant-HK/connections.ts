export default {
  recognizedCredential: "已識別 key；認證被拒絕",
  errorDetail: '查看結構化錯誤詳情', logicalError: '記錄的業務錯誤',
  title: '連接診斷',
  scope: '查詢入口代理與應用記錄。HTTP 200、寫出或 Flush 成功不代表客戶端已收到或解析完成；“重連”是客戶端行為，不能僅憑服務端日志確認。DNS、TCP 或 TLS 階段完全未到入口的失敗可能沒有記錄，需結合客戶端日志。',
  identityScope: '入口記錄可能沒有用戶身份。排查網絡問題時，可清除用戶、密鑰、模型、帳號和分組篩選，再按域名、時間、IP、請求 ID 查找。URL 不包含查詢參數或敏感路徑令牌。',
  state: '觀察結果', source: '記錄來源', host: '入口域名（含端口）', requestId: '請求 ID / 客戶端 ID / Trace ID',
  details: '連接詳情', legacy: '歷史記錄（字段可能缺失）', unidentified: '未識別用戶', bytes: '寫出 {count} 字節',
  empty: '沒有匹配的已保留記錄。這不代表客戶端連接正常；歷史未采集、未到入口、未覆蓋協議、保留期結束或采集失敗均可能沒有記錄。',
  loadError: '連接記錄加載失敗，請檢查時間范圍（最多 31 天）并重試。',
  noTimeline: '該來源未采集應用時間線。', page: '第 {page} 頁', previous: '上一頁', next: '下一頁',
  upstreamStatus: '入口觀察的后端狀態', networkError: '入口網絡錯誤類型',
  sources: { all: '全部來源', edge: '入口代理', application: '應用', nginx: 'Nginx 入口', caddy: 'Caddy 入口' },
  states: {
    all: '全部結果', abnormal: '異常結果', completed: '應用側完成', edge_completed: '入口響應結束', edge_failed: '入口異常',
    client_disconnected: '下游連接斷開', downstream_write_failed: '下游寫出失敗', failed: '請求失敗', incomplete: '流未完成', canceled: '請求被取消', unknown: '結果未確認'
  }
}
