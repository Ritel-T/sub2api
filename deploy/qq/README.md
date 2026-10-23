# Sub2API QQ candidate

This is local implementation material. Nothing here installs, enables or sends to a production QQ account automatically.

## User flow

Profile → QQ notifications → create a ten-minute one-time code. Add the existing bot as a QQ friend and send `/sub 绑定 CODE`. Return to the profile, check the masked QQ candidate and confirm that exact challenge. Then choose email, QQ or both for balance-low and subscription-expiry events. Email is the initial default. Account security and recovery mail continue through existing email handling.

Private friend commands: `/sub` (help), `/sub 余额`, `/sub 套餐`, `/sub 今日用量`. The first version uses explicit UTC for daily usage and subscription expiry displays. It cannot change balances, spend money, disclose API keys or execute administrative commands. QQ replies are currently Chinese. Existing notification thresholds and event scheduling apply; an event switch disables that event for this notification routing, including email.

## Services and credentials

- Sub2API: `QQ_BOT_ID`, `QQ_WEBHOOK_SECRET` (at least 32 characters), `QQ_ONEBOT_URL` pointing to the restricted internal relay, `QQ_ONEBOT_TOKEN` set to a separate relay token. All must be configured to enable the feature.
- home bridge: `QQ_BOT_ID`, the same `QQ_WEBHOOK_SECRET`, `QQ_RELAY_TOKEN` matching the application's relay token, `QQ_NATIVE_TOKEN` from the existing local OneBot service, `QQ_WEBHOOK_URL=https://dash.ritel.si/api/v1/qq/webhook`. The native token must not equal the relay token.
- Python 3 and `websockets==16.0`; install in an isolated venv. The bridge's native HTTP/WS destinations are fixed to loopback ports 3000/3001. Its relay listens only on loopback 3102 and accepts only authenticated `/send_private_msg`, plain text and confirmed friends. It does not accept friend requests automatically.
- The bridge uses a fresh friend-list check before forwarding a command or sending a message. No chat history, display names or native token are forwarded to Sub2API. Its HTTPS webhook must not redirect. Unknown HTTP/send outcomes are not replayed.
- `bridge.service` is a template requiring a new restricted service account, protected environment file and owned immutable program files. Do not place a real token in a repository, command argument or report.

## Existing chat isolation and transport

The deployed topology uses home-initiated SSH reverse TCP forwarding, limited to API loopback 127.0.0.1:3103 → home loopback 127.0.0.1:3102. The dedicated SSH key permits only that remote listen address, with local TCP/Unix forwards, shell and SFTP denied. The API host's separate unprivileged send proxy binds Docker gateway 172.20.0.1:3104, accepts only authenticated plain-text send requests from its internal subnet and forwards once to the tunnel. No public OneBot port, native token on API, new container or nginx modification is needed. The earlier Unix socket/sidecar design in the isolation document is historical, not the deployed design.

Home uses the actual `home-bridge.service` and `home-relay-tunnel.service` templates. `/sub` reservation runs before OpenClaw admission/history/model dispatch; other chats retain existing policy. Exact deployment, backup, transport checks and remaining real-user acceptance are recorded in the workspace report `reports/sub2api-qq-20261023/deployment.md`.

## Delivery semantics and operational boundaries

Database migration 298 adds binding, inbound-dedup and delivery-attempt tables. Each send is recorded before network I/O; duplicates with unknown/rejected/in-flight outcomes are not treated as sent. Binding, account state and notification preference are checked at send time. Unbinding prevents new dispatch, but cannot retract a send already accepted by a remote relay. Socket timeouts are not hard end-to-end deadlines. This is deliberately at-most-one attempted send, not a guarantee of delivery. Network loss may leave notifications unsent or unknown; there is no automatic retry or email fallback on QQ-only sends. Both mode independently attempts the existing email path. No admin delivery UI or retention cleanup is included in this first version.

Before production: reconcile against the then-current production commit and concurrent UI work; build with the exact required toolchain; back up current schema/image/config and bot files; validate the internal relay path, webhook routing and service permissions; independently read back. A real friend request/bind/query and actual notification need separately identified test users and explicit message-send authorization. Email/security/other-site behavior must also be checked. Rollback disables the feature/bridge and restores code/config, preserving new database business data rather than restoring an old dump.
