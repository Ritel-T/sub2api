# Sub2API (OpusClaw Fork) — Agent Reference

## 1. What This Is

Sub2API is a Go API gateway that proxies AI model requests through Google Antigravity accounts. It accepts Claude/Gemini API format requests, transforms them to Gemini v1internal format, and forwards to Antigravity upstream. This fork contains OpusClaw-specific patches for caching, sticky sessions, and rate-limit handling.

Upstream repo: `github.com/Wei-Shaw/sub2api` (version 0.1.105)

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
| `/srv/sub2api-c/` | Production deployment (docker-compose + data) |
| `/srv/sub2api-c/src/` | Build context (rsync'd from dev, no `.git`) |
| `/srv/antigravity-manager/` | AGM instance (separate service, port 8045) |

### Containers

| Container | Port | Purpose |
|-----------|------|---------|
| `sub2api-c` | `0.0.0.0:8082→8080` | Sub2API-C application |
| `sub2api-c-postgres` | internal 5432 | PostgreSQL |
| `sub2api-c-redis` | internal 6379 | Redis |
| `antigravity-manager` | host:8045 | AGM (unrelated) |

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
│   │   ├── antigravity_credits_overages.go # AI Credits handling
│   │   ├── gateway_service.go              # Session hash, account selection, sticky sessions
│   │   ├── gemini_native_signature_cleaner.go  # Gemini thought signature cleanup
│   │   ├── claude_signature_cleaner.go     # [OpusClaw] Claude thinking signature cleanup
│   │   └── gemini_messages_compat_service.go   # Gemini messages compat + signature retry
│   ├── pkg/antigravity/  # Request/response transformation
│   │   ├── request_transformer.go   # Claude→Gemini request conversion
│   │   ├── response_transformer.go  # Gemini→Claude response conversion
│   │   ├── claude_types.go          # Claude request/response types
│   │   └── gemini_types.go          # Gemini/v1internal types
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
| Failover exponential backoff | `failover_loop.go` | Exponential backoff with 4s cap (was linear) |
| First-message-only session hash | `gateway_service.go` | Use only first user message for stable multi-turn hash |
| Quota tier scheduling | `gateway_service.go` | Free-quota accounts prioritized over credits-only |

## 5. Key Constants

| Constant | Value | File | Purpose |
|----------|-------|------|---------|
| `antigravityRateLimitThreshold` | 7s | `antigravity_gateway_service.go:40` | Smart retry vs rate-limit boundary |
| `antigravityStickyPreserveThreshold` | 60s | `antigravity_gateway_service.go:47` | Short vs long rate-limit for sticky |
| `antigravitySmartRetryMaxAttempts` | 1 | `antigravity_gateway_service.go:42` | Max smart retry attempts |
| `antigravityModelCapacityRetryMaxAttempts` | 60 | `antigravity_gateway_service.go:48` | MODEL_CAPACITY_EXHAUSTED retries |
| `creditsExhaustedDuration` | 30min | `antigravity_credits_overages.go:19` | Credits exhausted cooldown |
| `stickySessionTTL` | (in gateway_service.go) | Sticky session cache TTL |
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
              → MODEL_CAPACITY_EXHAUSTED: retry up to 60x
          → if 400 + signature error: strip thinking → retry (2-stage)
      → response: Gemini→Claude transform
  → on UpstreamFailoverError: HandleFailoverError() → select new account → loop
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
cd /srv/sub2api-dev/
docker run --rm -v $(pwd)/backend:/app -w /app golang:1.26.1-alpine go test ./internal/service/... -count=1
```

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
| Main (OpusClaw gateway) | 100.88.210.12 | Nginx + OpusClaw app |
| vps6 (Sub2API main) | 100.114.245.91 | Sub2API primary (port 8080) |
| **vps5 (this machine)** | **100.114.232.111** | AGM-A (8045) + Sub2API-C (8082) |
| unnamed-2 | 100.101.200.81 | AGM-B (standby) |
| unnamed-1 | 100.112.136.98 | Sub2API-B (port 8081) |

OpusClaw channels pointing to this Sub2API-C instance:
- Ch 11: Sub2API-C Claude (status=2, disabled)
- Ch 12: Sub2API-C Gemini (status=2, disabled)
- Ch 18: Sub2API-C haiku→g-flash (status=2, disabled)

## 12. Important Notes

- All patches must be tagged with `[OpusClaw Patch]` comment for upstream tracking
- Never modify code under `internal/pkg/antigravity/` without understanding the Claude↔Gemini transform chain
- `request_transformer.go` is the most sensitive file — changes there affect caching, thinking, and signature behavior
- Rate-limit thresholds directly impact cache hit rates; changing them requires understanding the full sticky session lifecycle
- The `signature` field in Claude thinking blocks and `thoughtSignature` in Gemini are **not interchangeable** — different cleanup functions exist for each format
