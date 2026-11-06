export default {
 operation: {"title": "Operation observation v2", "scope": "Application entry → local terminal delivery; not client receipt. Source evidence and delivery are distinct; retries remain one operation.", "unknown": "Unknown: no v2 evidence. Legacy terminal, usage, HTTP 200 and billed tokens do not prove an answer completed.", "summaryTitle": "Observed operation samples · arrival window ≤ 1 hour", "coverage": "Coverage unknown. Only persisted v2 application traces are counted; in-flight requests, late writes, dropped observations and uncovered paths are missing. This is not a site-wide completion rate.", "shortWindow": "Choose a range of one hour or less to see observed samples; unavailable queries are not zero samples.", "sampleCount": "{count} observed samples", "recoveredCount": "{count} completed after failure", "writer": "Process-lifetime diagnostics: {enqueued} enqueued · {dropped} dropped · {write_failed} writes failed. These counters do not measure window coverage.", "legacy": "Legacy observation; completion unknown", "reason": "Reason / finish class", "elapsed": "Whole operation wait (including failed attempts)", "first": "First usable content written", "finalFirst": "Final attempt first usable content", "attempts": "Attempts / failed attempts", "delivery": "Local delivery / evidence", "protocol": "Protocol / transport / scope", "recovered": "Completed after a failed attempt"},

  recognizedCredential: "Recognized key; authentication rejected",
  errorDetail: 'Open structured error details', logicalError: 'Recorded logical error',
  title: 'Connection diagnostics',
  scope: 'Inspect edge proxy and application observations. HTTP 200 or a successful write/flush does not prove client receipt or parsing. Reconnection is a client action and cannot be confirmed from server logs alone. DNS, TCP or TLS failures that never reach the edge may have no record; correlate client logs.',
  identityScope: 'Edge records may have no user identity. Clear user, key, model, account and group filters to investigate by host, time, IP and request ID. URLs omit query parameters and sensitive path tokens.',
  state: 'Observed result', source: 'Source', host: 'Ingress host (including port)', requestId: 'Request / client / trace ID',
  details: 'Connection details', legacy: 'Legacy record (fields may be missing)', unidentified: 'Unidentified user', bytes: '{count} bytes written',
  empty: 'No matching retained records. This does not prove connectivity: historical gaps, failures before ingress, unsupported protocols, retention or collection failures can leave no record.',
  loadError: 'Could not load connection records. Check the time range (maximum 31 days) and retry.',
  noTimeline: 'Application timeline was not collected by this source.', page: 'Page {page}', previous: 'Previous', next: 'Next',
  upstreamStatus: 'Backend status observed by ingress', networkError: 'Ingress network error kind',
  sources: { all: 'All sources', edge: 'Edge proxies', application: 'Application', nginx: 'Nginx ingress', caddy: 'Caddy ingress' },
  states: {
 limited: 'Limited / policy refusal',
 cancelled: 'Explicit cancellation',
    all: 'All results', abnormal: 'Abnormal results', completed: 'Application completed', edge_completed: 'Edge response ended', edge_failed: 'Edge failure',
    client_disconnected: 'Downstream disconnected', downstream_write_failed: 'Downstream write failed', failed: 'Request failed', incomplete: 'Incomplete stream', canceled: 'Request canceled', unknown: 'Unconfirmed result'
  }
}
