export default {

  profit: '近期利润 / 利润率',
  economics: { usage: '使用记录', rate: '倍率估算', unknown: '利润样本不足' },
  priority: '优先级',

  title: '优先调度',
  description: '先满足体验目标，再平衡质量、延迟、并发与成本。',
  enabled: '启用优先调度',
  scopeNote: '适用于 OpenAI 文本请求的自由选路。会话绑定、协议优先级、模型权限、限流与利润门槛继续生效；图片、视频及其他平台沿用原调度。',
  modes: { experience: '体验优先', balanced: '均衡', profit: '利润优先', custom: '自定义' },
  strategy: '调度策略',
  weights: '评分权重',
  quality_weight: '质量', latency_weight: '延迟', load_weight: '并发余量', cost_weight: '利润 / 成本',
  thresholds: '体验目标与统计窗口',
  target_ttft_ms: '首 token P90 目标（毫秒）', max_load_percent: '并发占用上限（%）', min_quality_percent: '质量通过率目标（%）',
  window_minutes: '使用记录窗口（分钟）', min_samples: '延迟 / 利润最少样本数', quality_max_age_hours: '质量结果有效期（小时）',
  groupIds: '生效分组 ID', groupHint: '逗号分隔；留空表示所有分组。',
  models: '生效模型', modelHint: '每行一个请求模型名，精确匹配；留空表示所有模型。',
  rule: '排序顺序：体验达标 → 数据不足 → 体验未达标；同档先按优先级（越小越先），再按策略评分。拥堵或质量不佳时，低倍率不会越过体验分档。',
  costHint: '利润＝用户扣费−估算成本；估算成本＝同分组、同模型的（账号统计定价或基础费用）合计 × 账号当前成本倍率。成本倍率在账号编辑中设置，默认 0.1；开启跟随时，有效上游探测会更新该值；关闭跟随后使用手填成本，失败或过期时保留已保存值。独立于账号计费和用户扣费；修改后重估近期利润，不改历史账单。利润率＝利润÷用户扣费。',
  save: '保存配置', saving: '保存中…', saved: '配置已保存', loading: '加载中…', retry: '重试', error: '读取或保存失败，请重试', invalid: '请检查分组 ID 和参数范围',
  recent: '最近一次候选评分', refresh: '刷新评分', empty: '暂无评分。启用后，符合范围且需要自由选路的请求会生成评分。',
  historyPending: '历史统计尚未就绪，当前使用原有评分；后台每 30 秒刷新统计。',
  snapshotHint: '仅展示当前服务实例最近一次候选池，最多 100 个账号。分数用于同一协议与订阅优先池内排序，不代表最终选中；实时并发可能继续变化。',
  model: '模型', group: '分组', account: '账号', score: '得分', tier: '状态', latency: 'P90 首 token', load: '并发占用', rate: '成本倍率', quality: '质量通过', samples: '条样本', unknown: '未知',
  tiers: { eligible: '体验达标', insufficient: '数据不足', degraded: '体验未达标' },
  reasons: { quality_below_target: '质量低于目标', quality_unknown: '无有效质量结果', latency_above_target: '延迟超过目标', latency_insufficient: '延迟样本不足', busy: '并发偏高或有排队', load_unknown: '并发数据未知', cost_unknown: '成本未知', recent_errors: '近期错误偏多', historical_loss: '近期用户扣费低于理论成本', profit_insufficient: '利润样本不足' }
}
