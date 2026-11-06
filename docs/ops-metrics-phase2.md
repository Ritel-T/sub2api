# Operation observations v2

Diagnostic-only, no migration or billing/scheduler/alert changes. Optional
`request_connection_traces.detail.operation` stores source semantic evidence
separately from local delivery. HTTP receipt and each accepted WS model turn
own one monotonic elapsed interval, including retries and recovery waits.
Physical leaf attempt intervals are unioned, never added to total elapsed.

Responses source parsing and raw single-choice Chat text/nonstream tools are
observed where wired. DeepSeek Chat/Messages recovery uses its existing full
source validator and real sink commit. Native WS, ctx-pool and WS-to-HTTP bridge
turns use source and actual socket-delivery callbacks. Fragmented generic Chat
tools, generic Messages/Gemini semantics, compact and uninstrumented conversion
paths remain unknown; HTTP lifecycle coverage does not imply semantic coverage.
No HTTP200, DONE, usage row or output token count proves completion.

Connection detail and usage timing expose the summary. Opt-in
`GET /admin/usage/connections?operation_summary=true` accepts at most one hour
by ingress started_at and reads only version2 application traces, not the
legacy union. Coverage is unknown: bounded async persistence, late completion,
process restart and unsupported paths can omit observations. This is an
observed sample summary, never an exact site completion rate. Historical data
is not backfilled. Legacy success/SLA/duration/TPS and long windows retain their
existing semantics. Terminal delivery proves local write, not client receipt.
