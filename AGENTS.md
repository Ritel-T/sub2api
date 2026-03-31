# Sub2API (OpusClaw Fork) — Agent Reference

## 1. What This Is

Sub2API is a Go API gateway that proxies AI model requests through Google Antigravity accounts. It accepts Claude/Gemini API format requests, transforms them to Gemini v1internal format, and forwards to Antigravity upstream. This fork contains OpusClaw-specific patches for caching, sticky sessions, rate-limit handling, scheduling optimization, and token accounting.

Upstream repo: `github.com/Wei-Shaw/sub2api` (version 0.1.106)

## 2. Architecture

```
Client → OpusClaw Gateway → Sub2API → Antigravity (Gemini API)
                              ↓
                         PostgreSQL + Redis
```

### This Machine (vps5: 100.114.232.111)

| Path | Purpose |
|------|---------|
| `/srv/sub2api-dev/` | **Development source** (this repo, with `.git`) |
| `/srv/sub2api-c/` | Sub2API-C deployment (docker-compose + data) |
| `/srv/sub2api-c/src/` | Build context (rsync'd from dev, no `.git`) |
| `/srv/antigravity-manager/` | AGM instance (separate service, port 8045) |

### Containers (vps5)

| Container | Port | Purpose |
|-----------|------|---------|
| `sub2api-c` | `0.0.0.0:8082→8080` | Sub2API-C application |
| `sub2api-c-postgres` | internal 5432 | PostgreSQL |
| `sub2api-c-redis` | internal 6379 | Redis |
| `antigravity-manager` | host:8045 | AGM-A (unrelated) |

## 3. Source Structure

```
backend/
├── cmd/server/          # Entrypoint (main.go, VERSION)
├── internal/
│   ├── handler/         # HTTP handlers (gateway_handler.go is the main entry)
│   │   ├── gateway_handler.go        # Claude /v1/messages entry + failover loop
│   │   ├── gemini_v1beta_handler.go  # Gemini native API entry
│   │   └── failover_loop.go          # Account switch logic
│   ├── service/         # Business logic
│   │   ├── antigravity_gateway_service.go  # Core: Claude→Gemini transform + retry + rate-limit
│   │   ├── antigravity_credits_overages.go # AI Credits handling + aggressive marking
│   │   ├── antigravity_quota_scope.go      # IsSchedulableForModelWithContext() — credits+quota+rate-limit decision
│   │   ├── gateway_service.go              # Session hash, account selection, sticky sessions, pre-filter, concurrency
│   │   ├── gemini_native_signature_cleaner.go  # Gemini thought signature cleanup
│   │   ├── gemini_messages_compat_service.go   # Gemini messages compat + extractGeminiUsage()
│   │   ├── claude_signature_cleaner.go     # [OpusClaw] Claude thinking signature cleanup
│   │   └── concurrency_service.go          # Slot acquisition + concurrency limits
│   ├── pkg/antigravity/  # Request/response transformation
│   │   ├── request_transformer.go   # Claude→Gemini request conversion (SENSITIVE — do NOT modify without full chain understanding)
│   │   ├── response_transformer.go  # Gemini→Claude response conversion (non-streaming)
│   │   ├── stream_transformer.go    # Gemini→Claude streaming conversion (ProcessLine + Finish)
│   │   ├── claude_types.go          # Claude request/response types (ClaudeUsage struct)
│   │   └── gemini_types.go          # Gemini/v1internal types (GeminiUsageMetadata)
│   └── web/dist/        # Embedded frontend (built from frontend/)
frontend/                # Vue 3 + Vite admin panel
Dockerfile               # Multi-stage: node→go→alpine
```

## 4. OpusClaw Patches

All patches are tagged with `[OpusClaw Patch]` in comments. Key changes:

| Patch | File | Purpose |
|-------|------|---------|
| Sticky preserve on short rate-limit | `antigravity_gateway_service.go` | `antigravityStickyPreserveThreshold=60s`: don't delete sticky binding when rate-limit ≤60s |
| Sticky preserve in pre-check | `gateway_service.go` | `shouldClearStickySession`: short rate-limits don't clear sticky |
| Claude signature cleanup | `claude_signature_cleaner.go` + `gateway_handler.go` | Clear thinking signatures on account switch to prevent 400 |
| Signature retry skip smart wait | `antigravity_gateway_service.go` | Don't nest smart retry inside signature retry |
| Credits exhausted 30min cooldown | `antigravity_credits_overages.go` | Reduced from 5h to 30min |
| Aggressive credits exhaustion marking | `antigravity_credits_overages.go:231-238` | Mark credits exhausted on nil/error/429 responses |
| Failover exponential backoff | `failover_loop.go` | Exponential backoff with 4s cap (was linear) |
| First-message-only session hash | `gateway_service.go` | Use only first user message for stable multi-turn hash |
| Quota tier scheduling | `gateway_service.go` | Free-quota accounts prioritized over credits-only |
| Differentiated concurrency | `gateway_service.go:183` | `effectiveConcurrencyForSlot()`: credits=10, quota=5 slots |
| Gemini sticky skip | `gateway_service.go:1621,3119` | Skip sticky session lookup for Gemini (`platform != PlatformGemini`) |
| MODEL_CAPACITY retry cap | `antigravity_gateway_service.go:54` | Reduced from 60 to 5 retries |
| Credits-exhausted pre-filter removal | `gateway_service.go:2022` | Deferred to `IsSchedulableForModelWithContext()` to support refreshed quota |
| Token double-counting fix | `response_transformer.go:285`, `stream_transformer.go:89,163`, `gemini_messages_compat_service.go:2697` | `InputTokens=0` when `CacheCreationInputTokens>0` to prevent 2x billing |

## 5. Key Constants

| Constant | Value | File:Line | Purpose |
|----------|-------|-----------|---------|
| `antigravityRateLimitThreshold` | 7s | `antigravity_gateway_service.go:40` | Smart retry vs rate-limit boundary |
| `antigravityStickyPreserveThreshold` | 60s | `antigravity_gateway_service.go:49` | Short vs long rate-limit for sticky |
| `antigravitySmartRetryMaxAttempts` | 1 | `antigravity_gateway_service.go:42` | Max smart retry attempts |
| `antigravityModelCapacityRetryMaxAttempts` | 5 | `antigravity_gateway_service.go:54` | MODEL_CAPACITY_EXHAUSTED retries (was 60) |
| `creditsExhaustedDuration` | 30min | `antigravity_credits_overages.go:19` | Credits exhausted cooldown |
| `opusClawCreditsConcurrency` | 10 | `gateway_service.go:58` | Concurrency slots for credits-tier accounts |
| `opusClawQuotaConcurrency` | 5 | `gateway_service.go:59` | Concurrency slots for quota-tier accounts |
| `maxSameAccountRetries` | 3 | `failover_loop.go:33` | Same-account retry limit |

## 6. Request Flow (Claude /v1/messages)

```
gateway_handler.go:Messages()
  → SelectAccountWithLoadAwareness() // pick account (sticky or new)
  → if account switched: CleanClaudeThinkingSignatures(body)
  → antigravityGatewayService.Forward()
      → antigravityRetryLoop()
          → pre-check: model rate limit? → inject credits or switch
          → send to Antigravity upstream
          → handleSmartRetry() on 429/503
              → RATE_LIMIT_EXCEEDED + <7s: wait + retry
              → RATE_LIMIT_EXCEEDED + 7-60s: set rate-limit, switch account, KEEP sticky
              → RATE_LIMIT_EXCEEDED + >60s: set rate-limit, switch account, DELETE sticky
              → MODEL_CAPACITY_EXHAUSTED: retry up to 5x (was 60x)
          → if 400 + signature error: strip thinking → retry (2-stage)
      → response: Gemini→Claude transform
          → InputTokens=0, CacheCreationInputTokens=uncached (no double-counting)
  → on UpstreamFailoverError: HandleFailoverError() → select new account → loop
```

### Token Transform (Antigravity → Claude)

Gemini provides `promptTokenCount` (includes cached) and `cachedContentTokenCount`. The transform:

```
cached = geminiResp.UsageMetadata.CachedContentTokenCount
uncached = geminiResp.UsageMetadata.PromptTokenCount - cached

Claude response:
  input_tokens = 0                  (always 0 for Antigravity)
  cache_creation_input_tokens = uncached
  cache_read_input_tokens = cached
  output_tokens = candidatesTokenCount + thoughtsTokenCount
```

This happens in 4 locations:
1. `response_transformer.go:282-289` — Non-streaming response
2. `stream_transformer.go:86-92` — Streaming accumulation (`p.inputTokens`, `p.cacheCreationTokens`)
3. `stream_transformer.go:159-167` — Streaming message_start event
4. `gemini_messages_compat_service.go:2686-2702` — `extractGeminiUsage()` for Gemini native path

### Scheduling Flow

```
Pre-filter (gateway_service.go:1999-2033):
  → Skip: rate-limited accounts
  → Skip: temporarily unschedulable accounts
  → Skip: overloaded accounts
  → DO NOT pre-filter: credits-exhausted (deferred to downstream)

IsSchedulableForModelWithContext (antigravity_quota_scope.go:27-42):
  → If model NOT rate-limited → schedulable (quota is available)
  → If model IS rate-limited + overages enabled + credits NOT exhausted → schedulable
  → Otherwise → NOT schedulable

Concurrency (effectiveConcurrencyForSlot, gateway_service.go:183-198):
  → Non-Antigravity accounts: use account.Concurrency
  → Antigravity + model rate-limited (credits tier): 10 slots
  → Antigravity + model NOT rate-limited (quota tier): 5 slots
```

## 7. Build & Deploy

No Go installed locally. Build via Docker multi-stage.

### Dev → Deploy workflow

```bash
# 1. Edit code in /srv/sub2api-dev/
cd /srv/sub2api-dev/
# ... make changes ...

# 2. Sync to deployment build context
rsync -az --delete --exclude='.git' /srv/sub2api-dev/ /srv/sub2api-c/src/

# 3. Build new image
cd /srv/sub2api-c/
docker compose build --no-cache

# 4. Restart
docker compose up -d

# 5. Verify
curl -s http://localhost:8082/health
docker logs sub2api-c --tail 20
```

### Quick restart (no rebuild, for config changes only)
```bash
cd /srv/sub2api-c/
docker compose restart sub2api
```

### View logs
```bash
docker logs -f sub2api-c
docker logs sub2api-c --tail 100
```

## 8. Testing

```bash
# Run tests inside Docker (no local Go)
# Use volume cache for faster repeated runs
cd /srv/sub2api-dev/
docker run --rm -v $(pwd)/backend:/app -v go-mod-cache:/go/pkg/mod -w /app golang:1.26.1-alpine go test ./internal/service/... -count=1

# Run specific package tests
docker run --rm -v $(pwd)/backend:/app -v go-mod-cache:/go/pkg/mod -w /app golang:1.26.1-alpine go test ./internal/pkg/antigravity/... -count=1

# Run specific test by name
docker run --rm -v $(pwd)/backend:/app -v go-mod-cache:/go/pkg/mod -w /app golang:1.26.1-alpine go test -run "TestExtractGeminiUsage" ./internal/service/... -count=1 -v
```

**Note**: First run downloads all Go modules (~2-3 min). Use `-v go-mod-cache:/go/pkg/mod` volume to cache modules across runs.

## 9. Database

PostgreSQL inside `sub2api-c-postgres` container (not exposed to host).

```bash
# Access psql
docker exec -it sub2api-c-postgres psql -U sub2api -d sub2api

# Backup
docker exec sub2api-c-postgres pg_dump -U sub2api sub2api > /srv/sub2api-c/backup_$(date +%Y%m%d).sql
```

## 10. Credentials

| Service | Credential |
|---------|-----------|
| Sub2API-C Admin | admin@opusclaw.me / OpusClaw@Sub2C |
| PostgreSQL | sub2api / sub2apic_pg_29f8a1b3 |
| Redis | sub2apic_redis_4e7c2d91 |

## 11. Network Context

This machine (vps5) is part of the OpusClaw Tailscale network:

| Host | Tailscale IP | Role |
|------|-------------|------|
| Main (OpusClaw gateway) | 100.88.210.12 | Nginx + OpusClaw app (internal port 13000) |
| vps6 (Sub2API-A) | 100.114.245.91 | Sub2API-A primary (port 8080) — OLD code |
| **vps5 (this machine)** | **100.114.232.111** | AGM-A (8045) + Sub2API-C (8082) — NEW code |
| unnamed-2 (Sub2API-D) | 100.101.200.81 | Sub2API-D (port 8080) — NEW code |
| unnamed-1 (Sub2API-B) | 100.112.136.98 | Sub2API-B (port 8081) — OLD code |

### Sub2API Instances

| Instance | Host | Port | Code Version | Status |
|----------|------|------|-------------|--------|
| Sub2API-A | vps6 (100.114.245.91) | 8080 | OLD (pre-OpusClaw scheduling patches) | Active |
| Sub2API-B | unnamed-1 (100.112.136.98) | 8081 | OLD (pre-OpusClaw scheduling patches) | Active |
| Sub2API-C | vps5 (100.114.232.111) | 8082 | **NEW** (all OpusClaw patches) | Active |
| Sub2API-D | unnamed-2 (100.101.200.81) | 8080 | **NEW** (scheduling patches, not token fix) | Active |

### OpusClaw Channels

OpusClaw channels pointing to Sub2API-C:
- Ch 11: Sub2API-C Claude (status=2, disabled)
- Ch 12: Sub2API-C Gemini (status=2, disabled)
- Ch 18: Sub2API-C haiku→g-flash (status=2, disabled)

OpusClaw channels pointing to Sub2API-D:
- 3 channels configured (status=2, disabled)

### SSH Targets

```bash
ssh root@100.114.245.91  # Sub2API-A (container: sub2api-test)
ssh root@100.112.136.98  # Sub2API-B (container: sub2api-app)
# Sub2API-C: localhost (container: sub2api-c)
ssh root@100.101.200.81  # Sub2API-D (container: sub2api-d)
ssh root@100.88.210.12   # OpusClaw gateway (internal port 13000)
```

## 12. Important Notes

- All patches must be tagged with `[OpusClaw Patch]` comment for upstream tracking
- Never modify `request_transformer.go` without understanding the full Claude↔Gemini transform chain
- Rate-limit thresholds directly impact cache hit rates; changing them requires understanding the full sticky session lifecycle
- The `signature` field in Claude thinking blocks and `thoughtSignature` in Gemini are **not interchangeable** — different cleanup functions exist for each format
- Token transform must be consistent across ALL 4 locations (response_transformer, stream_transformer ×2, extractGeminiUsage)
- `IsSchedulableForModelWithContext()` is the single source of truth for scheduling decisions — do NOT duplicate its logic in pre-filters
- Do NOT change `concurrency_cache.go` or `concurrency_service.go` internals
- Sub2API-A/B are still running OLD code — update requires manual rsync + docker build on each host

## 13. Git History (OpusClaw Patches)

| Commit | Description |
|--------|-------------|
| `8a9dfa8b` | fix(scheduling+tokens): credits-exhausted pre-filter removal + token double-counting fix |
| `88638501` | fix(usage): estimate cache_creation_input_tokens from uncached input |
| `13ed03f1` | feat(scheduling): differentiated concurrency, Gemini sticky skip, anti-deadlock fixes |
| `3c120b73` | test: update fixtures for auto-scheduling behavior |
| `84cb24fa` | ui: remove Status/Schedulable/allow_overages manual toggles |
| `d1dee7a6` | fix(sync): CRS force Schedulable=true + migration for disabled accounts |
| `485c7ec7` | fix(scheduling): preserve quota tier ordering |
| `195c5187` | refactor(credits): force allow_overages=true for Antigravity |
| `8d937b5e` | refactor(scheduling): force Schedulable=true, remove manual toggle |

## 14. Pending Work

- **Token fix in-progress**: `gemini_messages_compat_service.go:extractGeminiUsage()` fixed but `TestExtractGeminiUsage` test expectations need to be updated (already done in working tree, needs commit + redeploy)
- **Sub2API-A/B update**: Need to sync OpusClaw patches to A and B instances (see `.sisyphus/notepads/scheduling-optimization/update-ab-steps.md`)
- **OpusClaw D channels**: 3 channels configured but status=2 (disabled), enable when ready
