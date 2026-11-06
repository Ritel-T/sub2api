export default {
 operation: {"title": "操作觀測 v2", "scope": "應用入口 → 本機終態交付，不證明用戶端收到。來源終態與交付證據分開，恢復重試仍屬同一次操作。", "unknown": "未知：沒有 v2 證據。舊終態、帳單、HTTP 200 或輸出 token 均不證明答案完成。", "summaryTitle": "已觀測操作樣本 · 入站視窗不超過 1 小時", "coverage": "覆蓋率未知。僅統計已持久化的 v2 應用記錄；仍在途、晚寫、丟棄及未覆蓋入口不在樣本中，不代表全站完成率。", "shortWindow": "選擇不超過 1 小時的範圍查看樣本；查詢不可用不等於零樣本。", "sampleCount": "已觀測 {count} 個樣本", "recoveredCount": "{count} 次失敗後完成", "writer": "進程累計診斷：入隊 {enqueued} · 丟棄 {dropped} · 寫入失敗 {write_failed}；不能換算本視窗覆蓋率。", "legacy": "舊觀測；完成情況未知", "reason": "原因 / 結束類別", "elapsed": "整次操作等待（含失敗嘗試）", "first": "首個可用內容寫出", "finalFirst": "最終嘗試首個可用內容", "attempts": "嘗試數 / 失敗嘗試數", "delivery": "本機交付 / 證據", "protocol": "協定 / 傳輸 / 範圍", "recovered": "失敗後完成"},

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
 limited: '限制／策略拒絕',
 cancelled: '明確取消',
    all: '全部結果', abnormal: '異常結果', completed: '應用側完成', edge_completed: '入口響應結束', edge_failed: '入口異常',
    client_disconnected: '下游連接斷開', downstream_write_failed: '下游寫出失敗', failed: '請求失敗', incomplete: '流未完成', canceled: '請求被取消', unknown: '結果未確認'
  }
}
