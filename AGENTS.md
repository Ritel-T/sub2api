# Sub2API (OpusClaw Fork) — Agent Reference

> **最后更新**：2026-04-03（模拟缓存韧性加固 `6c68919c` + 管理面板 `0f07f0bc` + 默认启用 `fce00844`）

## 1. What This Is

Sub2API is a Go API gateway that proxies AI model requests through Google Antigravity accounts. It accepts Claude/Gemini API format requests, transforms them to Gemini v1internal format, and forwards to Antigravity upstream. This fork contains OpusClaw-specific patches for caching, sticky sessions, rate-limit handling, scheduling optimization, and token accounting.

Upstream repo: `github.com/Wei-Shaw/sub2api` (merged up to `055c48ab`)

## 2. 开发环境

**所有源码统一在 oc-dev (100.114.232.111) 上开发，其他机器不保留源码。**

| 路径 | 用途 |
|------|------|
| `/root/src/sub2api/` | **本仓库**（开发源码，含 `.git`，branch: `opusclaw/v0.1.106-merge`） |
| `/root/src/opusclaw/` | OpusClaw 网关源码（另一个仓库，详见其 AGENTS.md） |
| `/srv/sub2api-c/` | Sub2API-C 部署目录（docker-compose + 数据卷，build context 指向 `/root/src/sub2api/`） |

### 构建与部署

```bash
# 1. 修改代码
cd /root/src/sub2api/
# ... 编辑 ...

# 2. 构建 Sub2API-C（本机直接 build）
cd /srv/sub2api-c/
docker compose build --no-cache
docker compose up -d

# 3. 验证
curl -s http://localhost:8000/health
docker logs sub2api-c --tail 20

# 4. 推送到其他实例（A/B/D）
docker tag sub2api:opusclaw-c sub2api:opusclaw-v6
docker save sub2api:opusclaw-v6 | gzip | ssh root@<目标IP> "gunzip | docker load"
# 目标机器更新 compose 镜像标签并重启
```

### 测试

```bash
# 本地 Go 可直接使用（/usr/local/go/bin/go 已链接到 PATH）
cd /root/src/sub2api/backend
go test ./internal/service/... -count=1

# 运行特定包
go test ./internal/pkg/antigravity/... -count=1

# 运行 unit 标签测试（antigravity 包的部分测试需要此标签）
go test -tags unit ./internal/pkg/antigravity/... -count=1

# 运行特定测试
go test -run "TestExtractGeminiUsage" ./internal/service/... -count=1 -v

# 备选：通过 Docker 运行测试（首次需下载模块 ~2-3 min）
cd /root/src/sub2api/
docker run --rm -v $(pwd)/backend:/app -v go-mod-cache:/go/pkg/mod -w /app golang:1.26.1-alpine go test ./internal/service/... -count=1
```

### 已知测试失败（预先存在）

以下 3 个测试因 OpusClaw 的 `InputTokens=0` patch 与上游测试期望值不一致而失败，**非 bug**：

| 测试 | 文件 | 原始作者 | 原因 |
|------|------|---------|------|
| `TestHandleGeminiStreamingResponse_ThoughtsTokenCount` | `antigravity_gateway_service_test.go` | sususu98 (2026-02-11, `d21d70a5`) | 期望 `InputTokens=8`，实际 `0` |
| `TestHandleClaudeStreamingResponse_ThoughtsTokenCount` | `antigravity_gateway_service_test.go` | sususu98 (2026-02-11, `d21d70a5`) | 期望 `InputTokens=5`，实际 `0` |
| `TestExtractGeminiUsage` (4 个子测试) | `gemini_messages_compat_service_test.go` | sususu98 (2026-02-11, `d21d70a5`) | 期望 `InputTokens` 非零，实际 `0` |

这些测试由上游贡献者 sususu98 在 `d21d70a5` (fix: include Gemini thoughtsTokenCount in output token billing) 中引入，断言的是上游的 token 计算逻辑。我们的 patch `8a9dfa8b` 将 `InputTokens` 固定为 0（uncached 归入 `CacheCreationInputTokens`），导致期望值不匹配。从该 patch 起就一直失败。待修复：更新测试期望值与 `InputTokens=0` 逻辑对齐。

## 3. Architecture

```
Client → OpusClaw Gateway → Sub2API → Antigravity (Gemini API)
                              ↓
                         PostgreSQL + Redis
```

### Sub2API 实例分布

| 实例 | 机器 | Tailscale IP | 端口 | 镜像标签 | Image ID | 容器名 | 部署方式 |
|------|------|-------------|------|---------|----------|--------|---------|
| **A** | oc-relay-a | `100.114.245.91` | `:8000` | `opusclaw-v6` | `fe6334bde19f` | `sub2api-test` | 镜像传入 |
| **B** | oc-relay-b | `100.112.136.98` | `:8000` | `opusclaw-v6` | `fe6334bde19f` | `sub2api-app` | 镜像传入 |
| **C** | oc-dev | `100.114.232.111` | `:8000` | `opusclaw-c` | `fe6334bde19f` | `sub2api-c` | `docker compose build` |
| **D** | oc-relay-d | `100.101.200.81` | `:8000` | `opusclaw-d` | `d1057e26657d` | `sub2api-d` | 镜像传入 |

- A、B 和 C：相同 Image ID（`fe6334bde19f`），构建于 2026-04-03，含模拟缓存计费 + 韧性加固 + 管理面板 + 默认启用
- D：构建于 2026-03-31 05:59，**源码较旧**（比 v5 早约 12 小时）

### oc-dev 上的容器

| 容器 | 镜像 | 端口 | 状态 |
|------|------|------|------|
| `sub2api-c` | `sub2api:opusclaw-c` | `0.0.0.0:8000→8080` | ✅ Healthy |
| `sub2api-c-postgres` | `postgres:18-alpine` | 内部 5432 | ✅ Healthy |
| `sub2api-c-redis` | `redis:8-alpine` | 内部 6379 | ✅ Healthy |

## 4. Source Structure

```
backend/
├── cmd/server/          # Entrypoint (main.go, VERSION)
├── ent/                 # Ent ORM schema + generated code
│   └── schema/group.go  # Group schema (含 require_oauth_only, require_privacy_set 字段)
├── internal/
│   ├── handler/         # HTTP handlers
│   │   ├── gateway_handler.go        # Claude /v1/messages entry + failover loop
│   │   ├── gemini_v1beta_handler.go  # Gemini native API entry (含 v1beta 404 回退)
│   │   ├── admin/group_handler.go    # Group CRUD (含分组过滤字段透传)
│   │   └── failover_loop.go          # Account switch logic
│   ├── service/         # Business logic
│   │   ├── antigravity_gateway_service.go  # Core: Claude→Gemini transform + retry + rate-limit
│   │   ├── antigravity_credits_overages.go # AI Credits handling + aggressive marking
│   │   ├── antigravity_quota_scope.go      # IsSchedulableForModelWithContext()
│   │   ├── gateway_service.go              # Session hash, account selection, sticky sessions
│   │   ├── account.go                      # Account model (含 IsPrivacySet, RPM buffer, customtools normalize)
│   │   ├── account_service.go              # Account CRUD (含 require_oauth_only 检查)
│   │   ├── admin_service.go                # Admin ops (含 CreateAccount 异步隐私设置)
│   │   ├── token_refresh_service.go        # Token refresh (含失败时 ensureAntigravityPrivacy)
│   │   ├── scheduler_snapshot_service.go   # Scheduler snapshots (含 privacy 过滤支持)
│   │   ├── openai_account_scheduler.go     # OpenAI scheduling (含 require_privacy_set 过滤)
│   │   ├── openai_gateway_service.go       # OpenAI compat layer
│   │   ├── openai_model_mapping.go         # OpenAI model mapping (含 gpt-5.4-mini/nano)
│   │   ├── gemini_native_signature_cleaner.go  # Gemini thought signature cleanup
│   │   ├── gemini_messages_compat_service.go   # Gemini messages compat + extractGeminiUsage()
│   │   ├── claude_signature_cleaner.go     # [OpusClaw] Claude thinking signature cleanup
│   │   ├── sim_cache_service.go            # [OpusClaw] SimCacheService (compute override + update state + circuit breaker)
│   │   ├── sim_cache_state.go              # [OpusClaw] SimCacheState + SimCacheRepository interface
│   │   └── concurrency_service.go          # Slot acquisition + concurrency limits
│   ├── pkg/
│   │   ├── antigravity/  # Request/response transformation
│   │   │   ├── request_transformer.go   # Claude→Gemini request (SENSITIVE)
│   │   │   ├── response_transformer.go  # Gemini→Claude response (non-streaming)
│   │   │   ├── stream_transformer.go    # Gemini→Claude streaming
│   │   │   ├── envelope.go             # [OpusClaw] Shared BuildV1InternalEnvelope
│   │   │   ├── sim_cache_override.go   # [OpusClaw] SimCacheOverride + ApplySimCacheOverride
│   │   │   ├── claude_types.go          # Claude types
│   │   │   ├── gemini_types.go          # Gemini types
│   │   │   └── testdata/               # Golden test fixtures (claude_*.golden.json, gemini_*.golden.json)
│   │   └── gemini/
│   │       └── models.go               # Gemini model metadata (含 customtools fallback)
│   └── web/dist/        # Embedded frontend
├── resources/migrations/
│   └── 081_add_group_account_filter.sql  # 分组过滤字段迁移
├── repository/
│   └── sim_cache_repo.go               # [OpusClaw] Redis Lua atomic update (HINCRBY + HSET + EXPIRE)
frontend/                # Vue 3 + Vite admin panel
Dockerfile               # Multi-stage: node→go→alpine
```

## 5. OpusClaw Patches

All patches are tagged with `[OpusClaw Patch]` in comments.

| Patch | File | Purpose |
|-------|------|---------| 
| Sticky preserve on short rate-limit | `antigravity_gateway_service.go` | `antigravityStickyPreserveThreshold=60s`: don't delete sticky when ≤60s |
| Sticky preserve in pre-check | `gateway_service.go` | `shouldClearStickySession`: short rate-limits don't clear sticky |
| Claude signature cleanup | `claude_signature_cleaner.go` + `gateway_handler.go` | Clear thinking signatures on account switch |
| Signature retry skip smart wait | `antigravity_gateway_service.go` | Don't nest smart retry inside signature retry |
| Credits exhausted 30min cooldown | `antigravity_credits_overages.go` | Reduced from 5h to 30min |
| Aggressive credits exhaustion marking | `antigravity_credits_overages.go:231` | Mark on nil/error/429 |
| Failover exponential backoff | `failover_loop.go` | Exponential backoff with 4s cap |
| First-message-only session hash | `gateway_service.go` | Stable multi-turn hash |
| Quota tier scheduling | `gateway_service.go` | Free-quota prioritized over credits-only |
| Differentiated concurrency | `gateway_service.go:183` | credits=10, quota=5 slots |
| Gemini sticky skip | `gateway_service.go:1648,3170,3292` | Skip sticky for Gemini |
| MODEL_CAPACITY retry cap | `antigravity_gateway_service.go:54` | Reduced from 60 to 5 retries |
| Credits-exhausted pre-filter removal | `gateway_service.go:2049` | Deferred to downstream |
| Token double-counting fix | `response_transformer.go:286`, `stream_transformer.go:131,165`, `gemini_messages_compat_service.go:2700` | `InputTokens=0` when `CacheCreation>0` |
| Thinking suffix restriction | Commit `2a32db04` | Restrict to supported claude-sonnet-4-5 only |
| Official request headers | `client.go` | Add X-Client-Name, X-Client-Version, x-goog-api-client, X-Machine-Session-Id |
| Official UA version | `oauth.go` | Updated default from `1.20.5 windows/amd64` to `1.107.0 linux/amd64` |
| Official systemInstruction format | `request_transformer.go` | Compact identity + `[ignore]` wrapper (replaces boundary markers) |
| Conditional stopSequences/toolConfig | `request_transformer.go` | stopSequences not sent by default; toolConfig only when tools exist |
| Per-process stable sessionId | `request_transformer.go` | UUID+timestamp format (replaces SHA256 hash) |
| Shared envelope builder | `envelope.go` | `BuildV1InternalEnvelope()` used by Claude + Gemini-native paths |
| Numeric loadCodeAssist metadata | `client.go` | ideType=9, platform=3, pluginType=2, mode=1 |
| Simulated cache billing | `sim_cache_override.go`, `sim_cache_service.go`, `sim_cache_state.go`, `response_transformer.go`, `stream_transformer.go`, `gemini_messages_compat_service.go`, `antigravity_gateway_service.go`, `gateway_handler.go` | Per-session Redis-backed token accumulator; probability-based cache miss simulation; override injected via context; Claude `/v1/messages` path only |
| Simulated cache admin settings | `setting_service.go`, `setting_handler.go`, `admin.go`, `SettingsView.vue` | GET/PUT `/api/v1/admin/settings/simulated-cache`; DB persistence + atomic config sync via `SimCacheService.UpdateConfig()` |
| Simulated cache resilience | `sim_cache_service.go`, `sim_cache_repo.go`, `sim_cache_state.go` | Lua atomic update (HINCRBY+HSET+EXPIRE); atomic.Value config snapshot; 50ms Redis timeout; circuit breaker (5 failures → 30s cooldown) |

## 6. Key Constants

| Constant | Value | File | Purpose |
|----------|-------|------|---------|
| `antigravityRateLimitThreshold` | 7s | `antigravity_gateway_service.go:40` | Smart retry vs rate-limit boundary |
| `antigravityStickyPreserveThreshold` | 60s | `antigravity_gateway_service.go:49` | Short vs long rate-limit for sticky |
| `antigravitySmartRetryMaxAttempts` | 1 | `antigravity_gateway_service.go:42` | Max smart retry |
| `antigravityModelCapacityRetryMaxAttempts` | 5 | `antigravity_gateway_service.go:54` | MODEL_CAPACITY retries (was 60) |
| `creditsExhaustedDuration` | 30min | `antigravity_credits_overages.go:19` | Credits cooldown |
| `opusClawCreditsConcurrency` | 10 | `gateway_service.go:58` | Credits-tier slots |
| `opusClawQuotaConcurrency` | 5 | `gateway_service.go:59` | Quota-tier slots |
| `maxSameAccountRetries` | 3 | `failover_loop.go:33` | Same-account retry limit |
| `antigravityHeaderClientVersion` | `1.107.0` | `client.go` | X-Client-Version header value |
| `antigravityHeaderGoogAPIClient` | `gl-node/18.18.2 fire/0.8.6 grpc/1.10.x` | `client.go` | x-goog-api-client header value |
| `antigravityIDETypeAntigravity` | 9 | `client.go` | loadCodeAssist IDE type enum |
| `antigravityLoadCodeAssistMode` | 1 | `client.go` | loadCodeAssist mode field |
| `simCacheRedisTimeout` | 50ms | `sim_cache_service.go` | Redis op timeout for simcache (fail-open) |
| `simCacheBreakerThreshold` | 5 | `sim_cache_service.go` | Consecutive Redis failures before circuit opens |
| `simCacheBreakerCooldownNs` | 30s | `sim_cache_service.go` | Circuit breaker cooldown duration |

## 7. Request Flow (Claude /v1/messages)

```
gateway_handler.go:Messages()
  → SelectAccountWithLoadAwareness()
  → if account switched: CleanClaudeThinkingSignatures(body)
  → [SimCache] ComputeOverride(groupID, sessionHash) → inject into context
  → antigravityGatewayService.Forward()
      → antigravityRetryLoop()
          → pre-check: model rate limit? → inject credits or switch
          → send to Antigravity upstream
          → handleSmartRetry() on 429/503
              → RATE_LIMIT + <7s: wait + retry
              → RATE_LIMIT + 7-60s: switch account, KEEP sticky
              → RATE_LIMIT + >60s: switch account, DELETE sticky
              → MODEL_CAPACITY: retry up to 5x
          → if 400 + signature error: strip thinking → retry
      → response: Gemini→Claude transform
          → if SimCacheOverride: ApplySimCacheOverride(override, promptTokenCount)
          → else: InputTokens=0, CacheCreationInputTokens=uncached
  → [SimCache] UpdateState(groupID, sessionHash, totalPromptTokens) — only on success
  → on UpstreamFailoverError: HandleFailoverError() → select new account → loop
```

### Token Transform (Antigravity → Claude)

```
cached = geminiResp.UsageMetadata.CachedContentTokenCount
uncached = geminiResp.UsageMetadata.PromptTokenCount - cached

Claude response:
  input_tokens = 0                  (always 0 for Antigravity)
  cache_creation_input_tokens = uncached
  cache_read_input_tokens = cached
  output_tokens = candidatesTokenCount + thoughtsTokenCount
```

4 locations: `response_transformer.go:282-289`, `stream_transformer.go:127-134,161-168`, `gemini_messages_compat_service.go:2696-2705`

**Simulated Cache Override（`gateway.simulated_cache.enabled=true` 时）**：
上述 4 处 token transform 在检测到 `SimCacheOverride` 时跳过 `SplitUncachedTokens()`，
改用 `ApplySimCacheOverride(override, promptTokenCount)` 计算 usage 分配：
- 第 1 轮（`IsFirstTurn`）：`cache_read=0, cache_creation=全部 prompt`
- 命中轮（`!IsMiss`）：`cache_read=min(历史累积, prompt), cache_creation=剩余`
- 丢失轮（`IsMiss`）：`cache_read=0, cache_creation=全部 prompt`
Override 通过 `context.Value` 从 Handler 层传入，Service 层取出后以函数参数传递给 transform。
Gemini 原生路径（`gemini_v1beta_handler.go`）不注入 override。

### Scheduling Flow

```
Pre-filter (gateway_service.go:2026-2060):
  → Skip: rate-limited / temporarily unschedulable / overloaded
  → DO NOT pre-filter: credits-exhausted (deferred)

IsSchedulableForModelWithContext (antigravity_quota_scope.go:27-42):
  → model NOT rate-limited → schedulable
  → model rate-limited + overages + credits NOT exhausted → schedulable
  → Otherwise → NOT schedulable

Concurrency (effectiveConcurrencyForSlot, gateway_service.go:183-198):
  → Antigravity + rate-limited (credits tier): 10 slots
  → Antigravity + NOT rate-limited (quota tier): 5 slots
```

### SelectAccountWithLoadAwareness 选号层级

```
Layer 1:   模型路由优先选择（分组配置了 model routing 时）
  Layer 1.5: 路由范围内的粘性检查
    → gatePass（6 项检查）+ rpmPass（RPM 检查）二阶段
    → tryAcquireAccountSlot（effectiveConcurrencyForSlot）
    → 失败时记录 [StickyCacheMiss] 日志
      reason: gate_check / rpm_red / session_limit / wait_queue_full / account_cleared
Layer 1.5（独立）: 非路由的粘性会话（仅 Claude，Gemini 跳过）
  → shouldClearStickySession() 检查
  → tryAcquireAccountSlot（effectiveConcurrencyForSlot）
Layer 2:   负载感知选择
  → 排序：quotaTier > Priority > LoadRate > LRU
Layer 3:   兜底排队（所有账号槽位满时返回 WaitPlan）
```

### 分组账号过滤（上游 `aeed2eb9` 引入）

```
Group 字段:
  require_oauth_only:  创建/更新账号绑定分组时拒绝 apikey 类型
  require_privacy_set: 调度选号时跳过 privacy 未设置的账号并标记 error

过滤生效位置:
  → gateway_service.go: selectAccountForModelWithPlatform() × 2 处
  → gateway_service.go: selectAccountWithMixedScheduling() × 2 处
  → openai_account_scheduler.go: OpenAI 调度路径
  → account_service.go: Create/Update 时 require_oauth_only 检查
```

### Cache-Driven RPM Buffer（上游 `72e5876c` 引入）

```
GetRPMStickyBuffer() 公式:
  手动 override (rpm_sticky_buffer) > cache-driven > floor
  cache-driven: buffer = concurrency + maxSessions
  floor: baseRPM / 5（至少 1，向后兼容）

典型配置 buffer 从 ~3 提升至 ~13，减少高峰 Prompt Cache Miss
```

## 8. Database

PostgreSQL inside `sub2api-c-postgres` (not exposed to host).

```bash
# Access psql
docker exec -it sub2api-c-postgres psql -U sub2api -d sub2api

# Backup
docker exec sub2api-c-postgres pg_dump -U sub2api sub2api > /srv/sub2api-c/backup_$(date +%Y%m%d).sql
```

**注意**：合并引入了 migration `081_add_group_account_filter.sql`（给 Group 表加 `require_oauth_only` + `require_privacy_set` 字段），首次启动新镜像时会自动执行。

## 9. Credentials

| Service | Credential |
|---------|-----------|
| Sub2API-A Admin | admin@opusclaw.me / OpusClaw@Sub2Test |
| Sub2API-C Admin | admin@opusclaw.me / OpusClaw@Sub2C |
| Sub2API-C PostgreSQL | sub2api / sub2apic_pg_29f8a1b3 |
| Sub2API-C Redis | sub2apic_redis_4e7c2d91 |

## 10. Production Safety — No Unconfirmed Disruptive Operations

**Any operation that could cause service downtime, data loss, or corruption is FORBIDDEN without explicit user confirmation.** This includes but is not limited to:

- Restarting, stopping, or upgrading production containers (`docker restart`, `docker stop`, `docker compose up`, etc.)
- Directly reading, writing, copying, or replacing database files on disk
- Modifying database schemas or running migrations against production databases
- Changing environment variables or configs that require a container restart to take effect
- Any `docker exec` command that writes to persistent storage inside a running container

**Preferred safe alternatives (MUST be attempted first):**

- **Configuration/settings changes**: Use the application's Admin API (e.g. `PUT /api/v1/admin/settings/*`) which takes effect at runtime without restart
- **Database record updates**: Use the application's CRUD API endpoints, not direct SQL
- **Reading production data**: Use API endpoints or read-only SQL queries against a *copy* of the database, never against the live file while the application is running
- **If no safe API exists**: Inform the user and ask how they want to proceed before touching any production resource

**If a disruptive operation is truly unavoidable:**

1. Explain the risk clearly (downtime duration, data loss potential, rollback plan)
2. Propose the exact commands you will run
3. **Wait for explicit user confirmation** — a clear "yes", "go ahead", "do it", or equivalent
4. Execute with proper safety steps (e.g., stop container before touching database files; verify integrity before and after)

**Incident reference**: On 2026-04-03, directly overwriting a SQLite database file via `docker cp` while a sibling service container (opusclaw-app) was running caused WAL/DB mismatch corruption, resulting in ~22 hours of production downtime. Always use runtime APIs when available, and always stop containers before touching persistent storage files.

## 11. Important Notes

- All patches must be tagged with `[OpusClaw Patch]` comment
- **Never modify `request_transformer.go`** without understanding the full Claude↔Gemini transform chain
- Rate-limit thresholds directly impact cache hit rates; changing them requires understanding sticky session lifecycle
- `signature` (Claude thinking) and `thoughtSignature` (Gemini) need different cleanup functions
- Token transform must be consistent across ALL 4 locations
- `IsSchedulableForModelWithContext()` is the single source of truth for scheduling — do NOT duplicate in pre-filters
- Do NOT change `concurrency_cache.go` or `concurrency_service.go` internals
- `effectiveConcurrencyForSlot()` 覆盖了所有 `tryAcquireAccountSlot` 和 `WaitPlan.MaxConcurrency` 调用点，新增选号路径必须使用此函数而非 `account.Concurrency`
- 上游新增的 `[StickyCacheMiss]` 日志与 OpusClaw 的 `effectiveConcurrencyForSlot` 已合并共存，修改 sticky 路径时需同时维护两者

## 11. Git History (OpusClaw Patches)

| Commit | Description |
|--------|-------------|
| `fce00844` | **chore(simcache): default enabled=true, miss_probability=0** |
| `6c68919c` | **fix(simcache): harden for production** — Lua atomic update, atomic.Value config, 50ms Redis timeout, circuit breaker |
| `0f07f0bc` | **feat(simcache): admin settings UI** — GET/PUT `/api/v1/admin/settings/simulated-cache`, DB persistence, Vue Gateway tab card |
| `211b96e3` | **fix(simcache): update wire_gen.go** — inject SimCacheService into GatewayHandler |
| `94d3b8f7` | **feat(simcache): simulated cache billing** — per-session Redis accumulator, probability miss, context-carried override, Claude /v1/messages only |
| `7f7f83e7` | **feat(antigravity): align request profile with official client** — headers, systemInstruction, sessionId, envelope |
| `fb56c842` | **merge: upstream `origin/main` (055c48ab)** — RPM buffer, group filter, privacy, customtools, OpenAI refactor |
| `2a32db04` | fix(incident): restrict thinking suffix to supported claude-sonnet-4-5 |
| `698cef81` | fix(scheduling): prevent account-wide rate limit for Antigravity 429 fallback |
| `373b650a` | fix(tokens): apply randomized cache estimation across all 4 transform locations |
| `dacf0281` | fix(scheduling): expand applyThinkingModelSuffix to all models + cross-model isolation tests |
| `9bdf0ec6` | feat(ui): replace schedulable toggle with model-family status indicators |
| `a9433c20` | feat(tokens): add SplitUncachedTokens utility with randomized 90-95% cache estimation |
| `8a9dfa8b` | fix(scheduling+tokens): credits-exhausted pre-filter removal + token double-counting fix |
| `88638501` | fix(usage): estimate cache_creation_input_tokens from uncached input |
| `13ed03f1` | feat(scheduling): differentiated concurrency, Gemini sticky skip, anti-deadlock fixes |

## 12. Upstream Merge Log

### `fb56c842` — Merge upstream `origin/main` (2026-04-02)

**合并范围**: 21 commits (11 non-merge), 48 files, +1236/-131 lines

**上游变更摘要**:

| 提交 | 作者 | 内容 |
|------|------|------|
| `72e5876c` | DaydreamCoding | Cache-Driven RPM Buffer: buffer = concurrency + maxSessions |
| `aeed2eb9` | DaydreamCoding | 分组账号过滤: require_oauth_only + require_privacy_set |
| `46bc5ca7` | DaydreamCoding | 令牌刷新失败及创建账号时设置隐私 |
| `649afef5` | YanzheL | Gemini v1beta 404 回退已知模型 |
| `4514f3fc` | YanzheL | Gemini customtools alias 解析 (normalizeRequestedModelForLookup) |
| `095bef95` | YanzheL | Gemini customtools fallback metadata |
| `995ef134` | InCerryGit | OpenAI 模型解析重构 |
| `0b3feb9d` | InCerryGit | OpenAI Anthropic compat mapping 修复 |
| `c810cad7` | remx | gpt-5.4-mini/nano 模型支持 |
| `a025a15f` | Wei-Shaw | Dashboard 刷新按钮 |
| `318aa5e0` | Wei-Shaw | Token usage trend cache hit rate 折线图 |

**冲突解决**（仅 `gateway_service.go`，2 处）:
- 冲突 #1: 取上游 `gatePass`/`rpmPass` 二阶段拆分 + 保留 OpusClaw `effectiveConcurrencyForSlot()`
- 冲突 #2: 取上游 `wait_queue_full` 逻辑（等待队列满不返回 WaitPlan，fall through 到 Layer 2）+ WaitPlan 中保留 `effectiveConcurrencyForSlot()`

**自动合并成功的 OpusClaw 文件**: `account.go`, `openai_gateway_service.go`, `openai_ws_forwarder.go`, `gateway_multiplatform_test.go`, `openai_account_scheduler.go`

**新增 DB migration**: `081_add_group_account_filter.sql`
