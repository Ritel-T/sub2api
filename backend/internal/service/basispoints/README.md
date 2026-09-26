# Structured output compatibility

The adapter accepts Responses requests using text.format.type=json_object or
json_schema. BPS rejects native text/response_format request fields, so the
adapter sends the output requirements as developer instructions and validates
the final response locally. This does not provide upstream constrained decoding.

- Preserve the requested model and account. Model-access rejections remain
  upstream errors; structured output support does not grant model permissions.
- Return a successful final answer only when it is valid JSON and, for
  json_schema, satisfies the supplied schema. Do not retry, repair, strip fences,
  or replace invalid model output with fabricated data.
- Withhold structured message text until the terminal response is validated.
  Reconstruct message events from that validated response, rather than replaying
  unvalidated deltas. Ordinary text requests remain incremental.
- Preserve tool continuations and explicit refusals as protocol items, outside
  the final-answer JSON contract. Preserve upstream failure/incomplete status
  without exposing partial structured message text.
- Resolve schema references inside the submitted document only. Never fetch
  remote schemas or read local files. Limit schemas to 1 MiB and answer text to
  16 MiB, subject to the existing SSE event limit.

# Tool history and input boundaries

- Rebuild complete CUSTOM calls using their current catalog's exact
  `codex2api.custom/NAME` marker and raw input. Preserve cached native calls
  verbatim. Retain unavailable historical tools as recorded history; never
  infer a new tool target from executable text.
- Require a matching complete call or scoped replay-cache entry for each tool
  result. Report the input path when neither is available. Do not invent calls
  or discard their results.
- Reject encrypted message parts with an `encrypted_content` diagnostic and
  input path. Do not reinterpret ciphertext as plaintext. Top-level encrypted
  reasoning items retain their existing handling.
- Preserve `detail: original` on HTTPS images and inline images rewritten by
  the relay. Let the upstream model validate its supported detail levels; do
  not silently downgrade the requested detail.
- Enforce the existing 20-inline-image and 32 MiB per-request relay limits.
  A relay capacity error and an upstream overload are separate from a tool
  protocol error; HTTP 200 alone does not establish a successful SSE terminal.
