# Ops metric accuracy, phase 1

Candidate only: no database migration, rollout, production configuration or model behavior changes.

Based on production `b3c72ec1f7a6718133e29e99899165dfd043d3c3` (2.18.0.29-ritel). This change does not integrate the independent DeepSeek recovery candidate.

* Job health now uses the configured interval or the cleanup scheduler's effective cron/timezone, plus the existing run budget and one-minute grace. Enabled jobs without completion evidence are pending; invalid/missing configuration is unknown. Completion heartbeats do not imply currently running jobs.
* Dashboard and scoring consume the same job states. The integer health score remains compatible; `health_score_available=false` prevents presenting incomplete evidence as a valid total. Known faults remain degraded even when traffic is idle. Existing error, TTFT and infrastructure thresholds and weights are unchanged.
* Overview responses include score details/version and per-metric `sample_count`/`percentile_method`. Raw windows are exact. Legacy rollups remain weighted P50/P90 and maximum child P95/P99, clearly labelled as such. Failed raw fragments are marked partial. Legacy duration sample counts are unknown because the aggregates never stored COUNT(duration_ms).
* TPS eligibility matches the frontend: at least two output tokens, positive recorded duration, sync/stream/ws_v2 only, excluding image/video billing and image outputs. TPS remains output_tokens/duration, not a measurement of visible rendering speed or a request-success guarantee. Partially billed records may remain eligible; billing is unchanged.
* The page explains that legacy success counts are usage records and recovered upstream events must not be added to final failures. Request terminal-outcome accounting, new timing spans, mergeable percentile distributions and workload SLOs remain phase 2/3.

Verification: targeted Go unit and isolated PostgreSQL integration tests, Vue component/TPS/locale tests, lint/design/type checks and frontend production build. Local browser fixtures cover simplified/traditional Chinese and English at desktop/mobile sizes, unknown scores, legacy percentile labels and server-owned job states. These fixtures do not establish production performance improvements.

Rollback is code-only. No database or heartbeat rewrite, task rerun, account change or threshold adjustment is needed.
