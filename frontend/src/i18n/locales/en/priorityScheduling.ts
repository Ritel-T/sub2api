export default {

  profit: 'Recent profit / margin',
  economics: { usage: 'Usage records', rate: 'Rate estimate', unknown: 'Insufficient profit samples' },
  priority: 'Priority',

  title: 'Priority scheduling', description: 'Meet experience targets, then balance quality, latency, capacity and cost.', enabled: 'Enable priority scheduling',
  scopeNote: 'Applies to freely routed OpenAI text requests. Session binding, protocol preference, model permissions, rate limits and profit gates still apply. Images, video and other platforms keep their existing scheduling.',
  modes: { experience: 'Experience first', balanced: 'Balanced', profit: 'Profit first', custom: 'Custom' },
  strategy: 'Scheduling strategy', weights: 'Score weights', quality_weight: 'Quality', latency_weight: 'Latency', load_weight: 'Available capacity', cost_weight: 'Profit / cost',
  thresholds: 'Experience targets and observation window', target_ttft_ms: 'P90 first-token target (ms)', max_load_percent: 'Concurrency threshold (%)', min_quality_percent: 'Quality pass target (%)', window_minutes: 'Usage window (minutes)', min_samples: 'Minimum latency / profit samples', quality_max_age_hours: 'Quality freshness (hours)',
  groupIds: 'Group IDs', groupHint: 'Comma-separated; empty means all groups.', models: 'Models', modelHint: 'One exact requested model name per line; empty means all models.',
  rule: 'Order: targets met → insufficient data → targets missed. Within each tier, lower account priority comes first, then strategy scores. Low cost cannot override quality or congestion tiers.',
  costHint: 'Profit = user charges − estimated cost. Estimated cost = account statistics price or base cost summed for the same group and model × the current account cost multiplier. Set it in account editing (default 0.1). When following is enabled, successful upstream probes update the same value. Turn it off to retain a manual cost; failures or expiry keep the saved value. Account and user billing remain independent. Changes re-estimate recent profit without rewriting usage logs. Margin = profit / user charges.',
  save: 'Save configuration', saving: 'Saving…', saved: 'Configuration saved', loading: 'Loading…', retry: 'Retry', error: 'Could not load or save. Please retry.', invalid: 'Check group IDs and parameter ranges',
  recent: 'Latest candidate scores', refresh: 'Refresh scores', empty: 'No scores yet. Eligible requests requiring free routing generate scores after enabling.', historyPending: 'History is not ready; existing scores remain active. Statistics refresh in the background every 30 seconds.',
  snapshotHint: 'Shows the latest candidate pool on this service instance, up to 100 accounts. Scores rank within protocol and subscription-priority pools, not the final selection. Live concurrency may change.',
  model: 'Model', group: 'Group', account: 'Account', score: 'Score', tier: 'Status', latency: 'P90 first token', load: 'Concurrency', rate: 'Cost rate', quality: 'Quality passed', samples: 'samples', unknown: 'Unknown',
  tiers: { eligible: 'Targets met', insufficient: 'Insufficient data', degraded: 'Targets missed' },
  reasons: { quality_below_target: 'Quality below target', quality_unknown: 'No fresh quality result', latency_above_target: 'Latency above target', latency_insufficient: 'Insufficient latency samples', busy: 'High concurrency or queueing', load_unknown: 'Concurrency unknown', cost_unknown: 'Cost unknown', recent_errors: 'Elevated recent errors', historical_loss: 'Recent charges below theoretical cost', profit_insufficient: 'Insufficient profit samples' }
}
