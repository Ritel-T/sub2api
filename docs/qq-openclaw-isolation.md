# Existing QQ bot: OpenClaw isolation and transport candidate

Inspected 2026-10-23. Local candidate only: no remote changes, restarts, messages or deliveries.

## Inspected runtime and guard

Host verified `home-mini` at the requested private SSH address. Actual installed entry imports `dist/src/gateway.js` from `/var/lib/openclaw-qq-capabilities-20261023/extensions/openclaw-onebot`.

- Entry SHA256: `b5c3ed644926dbd3c5f3451cd4b7825f8ebea43aa1137388e4ce8749ce4716ca`.
- Original gateway SHA256: `0b8db1326e469f867c1c238b9bfb0fb7928dd3e4e600768e70c366a00c95ef16`.
- Candidate gateway SHA256: `5a3f38e183af2a384c0900ebcfda363380d4c6d090d1d50565ec2ce604fba4f6`.

`bufferMessage` first normalizes inbound events; the new guard immediately drops private commands matching case-sensitive `/^\/sub(?:\s|$)/` after leading whitespace removal. This precedes allowFrom, history lookup, batch state, reactions, and model dispatch. Normal private messages and all group messages follow existing routing. Text segments concatenate; mixed-media reserved commands remain excluded even when the bridge rejects their payload. Credentials and binding codes are never passed to an LLM by this route.

Reproduce with `python3 deploy/qq/openclaw-build-candidate.py`, `node --check deploy/qq/openclaw-gateway.candidate.js`, and `node deploy/qq/openclaw-sub-command-reservation.test.mjs`. Passed 14 prefix/segment cases plus the actual extracted buffer function: four reserved private cases stop before processing; three normal/group cases enter the existing pipeline.

Deploy later by fresh source SHA CAS; back up file bytes, owner/mode and hash before replacing only this file. Recheck actual installed plugin/runtime mounts and concurrent writers. The included patch is not a general installer or permission to replay historical deployment scripts. A later restart/channel check and real authorized user binding remain required.

## Proposed transport (not deployed or live validated)

Home receives native loopback WS events and posts HMAC-signed requests to the public API webhook. The other direction exposes a new dedicated relay on home loopback port 3102, implementing only authenticated POST `/send_private_msg`, friend verification, text-only payloads, bounded sizes/timeouts and no automatic retries. Use a distinct relay secret; never send the native OneBot token to API or expose full OneBot.

Home initiates a dedicated SSH reverse Unix socket forward, using a dedicated new key and pinned host key:

```sh
ssh -NT -o BatchMode=yes -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/sub2api-qq/known_hosts \
  -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=3 \
  -i /etc/sub2api-qq/tunnel_ed25519 -p VERIFIED_SSH_PORT \
  -R /run/sub2api-qq/relay.sock:127.0.0.1:3102 qq-relay@VERIFIED_API_HOST
```

API-side dedicated user configuration candidate:

```text
Match User qq-relay
    AuthenticationMethods publickey
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    AuthorizedKeysFile /etc/ssh/authorized_keys/qq-relay
    AllowTcpForwarding no
    AllowStreamLocalForwarding remote
    AllowAgentForwarding no
    X11Forwarding no
    PermitTTY no
    PermitTunnel no
    PermitUserRC no
    ForceCommand /bin/false
    MaxSessions 0
    StreamLocalBindMask 0117
    StreamLocalBindUnlink no
```

The root-owned authorized-key entry uses `restrict,port-forwarding` plus the new public key. Forced command/no sessions block shells; `-N` requires no session. Allow only the dedicated user/group to write the runtime socket directory and access its socket. Root owns credentials/config. Use a persistent tmpfiles/runtime-directory contract and inspect a stale socket before deliberate removal; automatic unlink is disabled.

**Unix path limitation:** `permitlisten`/`PermitListen` specify TCP host:port, and `PermitOpen` controls destination host:port. They are not Unix socket path allowlists. Therefore this recipe limits forward direction, TCP and shells but cannot claim per-key confinement to one Unix path. A compromised key can request remote Unix listeners at other writable filesystem paths. If that residual scope is unacceptable, use a reviewed filesystem/AppArmor boundary or change to an outbound pull queue; do not claim `permitlisten` resolves it.

API Compose can add a pinned socat sidecar with only the runtime directory mounted read-only, group permission to connect to the socket, no published host ports, and a dedicated internal network shared only with Sub2API. Sub2API calls `http://qq-relay:3102/send_private_msg` using the relay-specific token. Sidecar concept: `TCP-LISTEN:3102,reuseaddr,fork UNIX-CONNECT:/relay/relay.sock`. Bind-mount the directory, not the changing socket inode. Preserve all existing production networks, env and constraints.

Before deployment: verify exact server OpenSSH version and `sshd -t` / `sshd -T -C user=qq-relay,...`; test denial of shell, SFTP, TCP forwards and local Unix forwarding; verify Unix forward and permissions, reboot/reconnect/stale-socket recovery and sidecar isolation. A socket/HTTP test proves transport only, not QQ delivery. No production receipt has been collected here.

Primary specification references: OpenSSH `sshd_config(5)`, `sshd(8)`, `ssh(1)` at man.openbsd.org. The installed Ubuntu version must independently accept this configuration.
## Dispatch timing and unbind boundary (review update)

The home native friend lookup and single native send each use a 3-second socket timeout; API forwarding uses 8 seconds, backend HTTP dispatch 10 seconds, and the ingress webhook client 12 seconds. This reduces ordinary timeout inversions. These are socket-operation limits, not absolute wall-clock deadlines: slow partial reads and already accepted remote requests can outlive the caller. Serial ingress forwarding may also delay subsequent WS commands while one request is pending; commands older than five minutes are rejected instead of replayed.

Unbind prevents future new dispatches after the backend rechecks its binding. It cannot retract an already accepted in-flight QQ send; a disconnected or timed-out request may complete remotely after unbind. Preserve unknown status and do not automatically resend. Do not advertise atomic cancellation of in-flight messages.