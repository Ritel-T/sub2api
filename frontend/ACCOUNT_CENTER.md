# Central account console integration

This frontend is opt-in at build time through `VITE_RITEL_ACCOUNT_ORIGIN` (see `.env.account.example`). The value must be the exact HTTPS origin, without a trailing slash, credentials, path, query, fragment or whitespace. Vite rejects an invalid configured value. Omitted/empty keeps native auth. Central builds go to `frontend/dist` by default; the legacy default output remains unchanged. For a scoped legacy check use `vite build --outDir dist` explicitly.

## Browser/BFF contract

- The same-origin Node console BFF serves `/auth/browser.mjs` and its relative shared modules. Its exported `bootstrapSub2apiConsumer({authorizeOrigin,onSession,onError})` performs the hidden iframe check and reads `/auth/session`. The frontend waits for it and the original `GET /api/v1/auth/me` before installing the router or mounting views. Failure shows an unavailable state with an explicit reload.
- Session JSON contains only `{user,csrf}`. `user.uid` must match `sub2api:<me.id>`. Business role, balance, run mode and all other User fields come from the existing `/api/v1/auth/me` response, never the thin central identity.
- Login/register/forgot/reset/legacy auth callbacks redirect to same-origin `/auth/login?returnPath=...`. Protected deep links preserve path/query/hash. Return paths are strict local paths; auth callback credentials are never replayed. A register entry returns to its validated redirect, or /dashboard by default; authenticated auth-entry visitors immediately go to their role default, avoiding a login/register loop. The frontend sends mode=register (or forgot) and only promo, aff and invite to the BFF login entry. The root-managed consumer/issuer accepts this protocol; actual referral/invitation benefit acceptance remains an integration check. The BFF owns the OIDC callback URLs and must intercept them before SPA fallback.
- Console axios and direct console fetch/SSE use same-origin cookies; unsafe methods add the current `X-CSRF-Token`. Browser bearer/refresh token persistence, refresh and automatic 401 replay are disabled in central mode. Existing legacy token keys are removed on startup. API Key/model paths such as `/v1`, batchImage, codex and KeyUsage retain their Bearer behavior.
- Protected `/api/v1/*` requests are proxied by the BFF with a server-only native AT. Public/settings/model-plaza/payment/public/webhook routing keeps the server contract. The BFF rejects native auth endpoints except GET me and directs credential/security mutations to account. It must enforce Origin/CSRF and remove/override incoming Authorization server-side. The frontend never obtains native AT.
- Ops WebSocket uses same-origin `/api/v1/admin/ops/ws/qps` with only `sub2api-admin` in central mode. The BFF validates the cookie and injects its server-only JWT subprotocol. Legacy WS settings and protocols remain for native mode.
- Logout invokes consumer.logout(): POST `/auth/logout`, CSRF and an explicit `{applied:true}` acknowledgement. Only confirmed success leaves for /home. Unknown outcome blocks further logout attempts until an explicit read-only session check, preserves drafts and never retries the POST. Central current-browser revocation is the server responsibility.
- Focus, visibility restore and BFCache pageshow recheck through the maintained consumer. During a check or error the mounted view is hidden behind a status gate; same-account forms remain mounted. Confirmed UID change/expiration unmounts the old view, clears auth and reloads to discard identity-owned in-memory stores and requests. In-flight axios/console-fetch responses are rejected after a confirmed identity change. Local Pelican/ModelTrace test history is scoped by central UID, so a new account does not adopt a previous account’s persisted results. Existing per-user balance-purchase recovery records retain their ownership and are not deleted.
- Activity collection is provided by the existing consumer (human activity POST `/auth/activity`), not a new renewal timer.

## Account links and sensitive operations

Header links and profile identity forms point to `<configured origin>/account/#profile`, `#security` and `#sessions`. Local `/profile` retains business overview, balance notifications, QQ notifications, support contact and security restriction records. Keys, balance, purchase and usage remain local.

Admin sensitive operations keep the original Go handler. `UserEditModal` recovery opens `<configured origin>/account/#reauth` in a new tab; the native-mode recovery link retains the existing account.ritel.si origin fallback with the new path. The account UI reauth updates proof for the same UID/SID; subsequent BFF requests acquire fresh AT. Returning to the console rechecks session without replacing the same-account dialog. The administrator must confirm and retry manually; reset is never replayed. Generic step-up UI follows the same explicit central reauth/manual retry path in central mode, keeping native TOTP behavior otherwise.

## Verification boundary

Validation performed on the final frontend:

- `pnpm run typecheck`: passed (after fixing the runtime module declaration by importing through a variable).
- `VITE_RITEL_ACCOUNT_ORIGIN=https://account.ritel.si pnpm exec vite build --outDir dist`: passed, 1618 modules, 38.85 seconds; outputs under frontend/dist. Log: `frontend/node_modules/.cache/account-center-build.log`.
- Only existing tooling was used. No new suite or mechanical tests, Go changes, commit or deployment. Build warnings: stale Browserslist data, mixed static/dynamic imports and chunks over 500 kB.

 Vite output alone does not verify cookies, CSP frame-src/script delivery, actual OIDC callbacks, cross-site browser logout, MFA/Passkey, WS upgrade or native fresh proof. These require real browser integration with the root-managed console BFF/account service. No Go/backend edits, commit, deployment or production build activation are part of this frontend change.

## Registration URL source mapping

Original RegisterView reads query `promo` into body `promo_code` when enabled; it resolves query `aff` then `aff_code` (trim/first value), falling back to the existing 30-day local referral record, and submits `aff_code`. The original invitation form submits `invitation_code` but reads no invitation URL field: neither `invite` nor `invitation_code` was recognized from query. The central route now sends `promo` unchanged, resolves original `aff/aff_code` and local referral fallback into `aff`, and maps explicit `invite` (or `invitation_code` alias) into `invite` for the new central prefill contract. It does not forward password, verification code, CAPTCHA proof or form body. The BFF accepts `mode=register|forgot` and maps the allowlisted fields through its validated issuer interaction; actual bonuses and invitation acceptance continue to use the original Sub rules and need browser verification.

The native embedded bundle is shared by console/dash/legacy panel vhosts. In central builds, a document opened on a different origin redirects to the explicit VITE_RITEL_CONSOLE_ORIGIN (default https://console.ritel.si) before mounting/auth initialization, retaining its local path and normal queries and dropping browser token query fields. This keeps old panel links usable without maintaining another login system; native API/Key/model traffic is not redirected or retired.
