export default {
  "started": "請求開始（本地時間）",
  "finished": "請求完成（本地時間）",
  "total": "總耗時",
  "first_content": "首內容 token（可見文本）",
  "connection": "獲取連接",
  "response_headers": "等待響應頭",
  "stream_output": "流輸出",
  "local_processing": "本地處理（事件編碼）",
  "details": "耗時詳情",
  "overlap": "所有偏移相對測試開始。總耗時包含各階段；首內容包含連接與響應頭等待，流輸出與本地處理有重疊。各項不可相加，也不能據此推導模型計算時間。",
  "range": "觀測偏移：{start} → {end}",
  "segments": "累計 {count} 個編碼片段",
  "missingFinal": "— 未取得服務端最終指標，不推算完成時間或耗時。",
  "delivery": {
    "idle": "尚未開始",
    "running": "等待服務端最終指標",
    "finished": "已取得服務端計時",
    "legacy": "此服務端未提供計時數據",
    "incomplete": "事件流在完成前結束",
    "canceled": "已在此瀏覽器取消，尚未確認服務端完成",
    "error": "連接中斷，最終指標可能不可用"
  },
  "outcome": {
    "success": "成功",
    "error": "錯誤",
    "timeout": "超時",
    "canceled": "取消"
  },
  "boundaries": {
    "connection": "HTTP GetConn → GotConn，包含連接池、DNS、TCP、TLS 等待；連接複用單獨標註。",
    "response_headers": "請求完整寫出 → 完整響應頭可用，包含上游等待，並非純網絡傳輸時間。",
    "first_content": "單次上游調用開始 → 首次解析出非空白可見文本。註釋、心跳、元數據、首字節均不計；這是內容觀測，不是 tokenizer 內部計時。",
    "stream_output": "首個可見文本 → 解析器因完成、錯誤或取消返回，包含讀取、解析、調度和本地寫出，並非客戶端收到時間。",
    "local_processing": "僅記錄各測試 SSE 事件寫出前的 JSON 編碼。時長累加不連續片段，偏移為片段外包區間；排除上游等待、寫出、最終計時事件和其他未採集本地工作。"
  },
  "reasons": {
    "not_observed": "— 未觀測到邊界或尚未到達該階段。",
    "no_content": "— 未觀測到非空白可見文本。",
    "not_isolated": "— 未單獨測量本地處理。",
    "invalid_observation": "— 觀測值超出本次測試區間。",
    "connection_reused": "— 複用已有連接，不聲明新建連接耗時。",
    "multiple_requests": "— 包含多次上游請求或重試，無法表示為單一階段。",
    "non_stream": "— 非流式響應不適用。",
    "unsupported_path": "— 此路徑未暴露實時內容解析邊界。",
    "collector_limit": "— 已達到採集上限，省略不完整指標。"
  }
}
