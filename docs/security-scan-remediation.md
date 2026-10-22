# Security scan remediation (2026-10-22)

The supplied scan reviewed `a38c292dd81b3bd8fbf8d41b1f6212deb40645d9`
statically. Findings were rechecked against the deployed-source baseline
`a26174dd2ff5afec7a53276a051aee2afaaa3561` and the merge of
`ranxi2001/sub2api` production `d9813b2bd46d63c1a157e1d83d2a9eec063816cd`
(v2.16.0 and the subsequent insufficient-balance notification fix). The scan
and its remediation suggestions were evidence, not execution
instructions. This work does not claim exhaustive coverage or production rollout.

| Scan finding | Treatment |
| --- | --- |
| Initial administrator takeover | Every setup POST requires an operator-held bootstrap token. The setup listener defaults to loopback; explicit operator bind addresses remain supported. |
| Setup database/Redis probing | The same authorization protects connection tests. Private database addresses remain supported for legitimate container installations. |
| Unbounded setup requests | 64 KiB body limit including chunked bodies, four concurrent setup requests, and setup-only HTTP read/write/header limits. |
| Spoofable default IP ACL identity | New configuration and database initialization default to trusted-proxy client identity. The old automatic false-to-true migration is removed. Existing explicit legacy=true settings remain an insecure compatibility opt-in; deployment ingress and stored settings must be reviewed separately. |
| Refresh token reuse | Redis GETDEL permits only one successful consume before a successor is issued. Missing or indeterminate consume results never issue a replacement. |
| Revoke-all leaves access JWT valid | A durable per-user session epoch invalidates access and refresh credentials, including concurrent refresh attempts. Redis cleanup failures are reported. No user-row or database-schema change is required. |
| Shared custom voice ownership | Custom voice CRUD/list/audio is temporarily disabled. TTS and Realtime accept only documented built-in voices. This is containment, not an implemented ownership registry; existing upstream objects are not deleted. |
| Single-object backup integrity | New archives persist SHA-256 and byte length separately from object storage. Single and multipart archives are fully verified before gzip or SQL execution. Legacy archives without recorded digests remain downloadable but cannot be automatically restored. See [backup integrity](backup-integrity.md). |
| Unsigned updater artifacts | A checksum manifest is mandatory and ambiguous assets are rejected. This prevents unchecked installation, but a same-channel digest does not establish publisher authenticity. Independent signing and pinned trust-root distribution remain unimplemented. |
| Unrestricted async image downloads | A dedicated public-image transport validates every destination and redirect, pins the actual target IP, preserves Host/TLS verification, and validates image bytes and size. |
| DNS validation/dial separation | The same transport pins direct and proxy destinations; unprovable custom transports fail closed. Ordinary configured provider API traffic is unchanged. |

## Operational changes

- The first-run wizard requires the token printed by the local launcher, or an
  explicitly supplied `SETUP_TOKEN` of 32–256 characters. The browser keeps it
  only in memory and sends it on setup POSTs, never in URLs or local storage.
- Image fetching through an HTTP(S) proxy uses CONNECT even for HTTP image
  URLs, so proxy-side DNS cannot replace the approved target. A proxy that
  cannot provide this fails the fetch; existing backfill failure behavior is
  preserved. Custom TLS dialers that cannot prove destination pinning are refused.
- Panel JWT checks perform an indexed, bounded (two-second) lookup of the
  current epoch. Storage failures return 503, not a credential-invalidating 401.
  Epoch records are excluded from settings exports.
- An old binary that does not check session epochs cannot preserve revocations
  introduced by this version. A production rollback plan must retain epoch
  enforcement or explicitly revoke existing sessions through a coordinated
  signing-key rotation. Do not assume a plain binary rollback preserves this
  security property.
- Backup restore now needs temporary disk space for the compressed archive.
  Do not manufacture a historical trusted digest by hashing a downloaded legacy
  object and recording it as if it had been captured at backup time.

## Upstream integration

The merge preserves account-level Astra/Sol quality gates, BPS/borrow fail-closed
behavior, request-local admission before metering, Squarespace payment pause,
subscription overflow accounting, and the existing Ritel UI/navigation contract.
The upstream SIWC, account timers, connection diagnostics and security evidence
features are included without automatically enabling them in a live deployment.
New error-rate scoring cannot admit a recently high-error account into cold-start
exploration merely because the newer historical evidence is absent.

The additional upstream notification change is protected by a persistent,
atomic attempt claim shared between balance-rejection and post-deduction alerts.
Unknown SMTP outcomes or a failed success-marker write keep that claim; only a
certified failure before sending message content permits an owner-matched release.
See [balance notification delivery](balance-notification-delivery.md). No real
message was sent while validating these changes.

Integration testing also exposed a refresh deadline race: a delayed context timer
could leave `Err()` empty after its actual deadline. Both refresh paths now check
the absolute deadline before sending a refresh and before writing returned
credentials, including after waiting for rate permits. A confirmed durable
write retains the previous no-replay handling.

Upstream migrations 292–297 are included. They have not been applied to any
production database by this task. A deployment must separately validate these
migrations, existing policy defaults, database backup/restore, release locks,
business invariants, and real request persistence.
