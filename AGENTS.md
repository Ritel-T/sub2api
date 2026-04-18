# Sub2API (OpusClaw Fork) — Agent Reference

> **最后更新**：2026-04-18（v0.1.114 上游合并）

## 1. What This Is

Sub2API is a Go API gateway that proxies AI model requests through Google Antigravity accounts. It accepts Claude/Gemini API format requests, transforms them to Gemini v1internal format, and forwards to Antigravity upstream. This fork contains OpusClaw-specific patches for caching, sticky sessions, rate-limit handling, scheduling optimization, and token accounting.

Upstream repo: `github.com/Wei-Shaw/sub2api` (merged up to `6c73b621`，对应 v0.1.114)

## 2. 开发环境

**所有源码统一在 oc-dev (100.114.232.111) 上开发，其他机器不保留源码。**

| 路径 | 用途 |
|------|------|
| `/root/src/sub2api/` | **本仓库**（开发源码，含 `.git`，branch: `opusclaw/v0.1.110-merge`） |
| `/root/src/opusclaw/` | OpusClaw 网关源码（另一个仓库，详见其 AGENTS.md） |
| `/srv/sub2api-c/` | Sub2API-C 部署目录（docker-compose + 数据卷，build context 指向 `/root/src/sub2api/`） |

### 本地 merge 分支约定

- 长期维护分支名称跟随**已完成 upstream merge 的最高版本**，例如当前为 `opusclaw/v0.1.110-merge`
- 完成新的 upstream merge 后，应同步更新本地分支名与本文件顶部元数据，避免分支名滞后于实际代码基线
- 当前本地 merge 分支应跟踪 `origin/main` 作为对比/合并基线；**不要**将其直接 push 到 `origin/main`
- 如需发布到个人 fork，请显式创建/推送同名远端分支，而不是依赖过期的 fork tracking 配置

### 构建与部署

使用 `deploy.sh`（仓库根目录）进行标准化构建与部署：

```bash
./deploy.sh build          # 构建（自动以 git commit hash 打标签）
./deploy.sh deploy-c       # 部署到 C 并验证
./deploy.sh push a         # 推送到 A（传输镜像 + compose 重建 + 健康检查）
./deploy.sh push b         # 推送到 B
./deploy.sh push ab        # 同时推送 A 和 B
./deploy.sh status         # 查看所有实例状态
./deploy.sh rollback a opusclaw-<hash>  # 回滚
```

镜像标签策略：每次 build 生成 `sub2api:opusclaw-<git-short-hash>`（不可变），
同时更新别名 `opusclaw`（A/B/C compose 统一引用此标签）。

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

以下测试因 OpusClaw 自有 patch 与上游测试期望值不一致而失败，**非 bug**：

| 测试 | 文件 | 原因 |
|------|------|------|
| `TestHandleGeminiStreamingResponse_ThoughtsTokenCount` | `antigravity_gateway_service_test.go` | OpusClaw patch `8a9dfa8b` 固定 `InputTokens=0`；上游期望 `InputTokens=8` |
| `TestHandleClaudeStreamingResponse_ThoughtsTokenCount` | `antigravity_gateway_service_test.go` | 同上，期望 `InputTokens=5` |
| `TestExtractGeminiUsage` (4 个子测试) | `gemini_messages_compat_service_test.go` | 同上，期望 `InputTokens` 非零 |
| `TestHandleFailoverError_BasicSwitch/非重试错误_Antigravity_第一次切换无延迟` | `failover_loop_test.go` | OpusClaw 指数退避 patch（`failover_loop.go:116-128`）首次切换即应用 1s 延迟；上游期望首次延迟 0 |
| `TestHandleFailoverError_IntegrationScenario/模拟Antigravity平台完整流程` | `failover_loop_test.go` | 同上根因 |

这 5 类失败均在 v0.1.114 合并前的 `bb29002` 基线上即存在，**非本次合并引入**。待修复：更新这些测试的期望值以匹配 OpusClaw 行为。

`TestConstants_值正确`（`oauth_test.go`）原本也属此类（OpusClaw UA `1.107.0 linux/amd64` vs 上游期望 `1.21.9 windows/amd64`），v0.1.114 合并时已就地修正断言以匹配 OpusClaw UA patch。

## 3. Architecture

```
Client → OpusClaw Gateway → Sub2API → Antigravity (Gemini API)
                              ↓
                         PostgreSQL + Redis
```

### Sub2API 实例分布

| 实例 | 机器 | Tailscale IP | 端口 | 镜像标签 | Image ID | 容器名 | 部署方式 |
|------|------|-------------|------|---------|----------|--------|---------|
| **A** | oc-relay-a | `100.114.245.91` | `:8000` | `opusclaw` | `a2c780234791` | `sub2api-test` | 镜像传入 |
| **B** | oc-relay-b | `100.112.136.98` | `:8000` | `opusclaw` | `a2c780234791` | `sub2api-app` | 镜像传入 |
| **C** | oc-dev | `100.114.232.111` | `:8000` | `opusclaw` | `a2c780234791` | `sub2api-c` | `docker compose build` |
| **D** | oc-relay-d | `100.101.200.81` | `:8000` | `opusclaw-d` | `d1057e26657d` | `sub2api-d` | 镜像传入 |

- A、B、C：Image ID `a2c780234791`，构建于 2026-04-05，含积分误判修复 + 调度加固 + 模拟缓存计费 + SimCache TTL 1h + ephemeral_1h 自动分类 + SimCache key 解耦
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
│   │   ├── admin/channel_handler.go  # [Upstream] Channel CRUD
│   │   └── failover_loop.go          # Account switch logic
│   ├── service/         # Business logic
│   │   ├── channel_service.go              # [Upstream] Channel management (渠道管理)
│   │   ├── channel_stats_service.go        # [Upstream] Channel usage stats
│   │   ├── model_pricing_resolver.go       # [Upstream] Channel-based model pricing
│   │   ├── antigravity_gateway_service.go  # Core: Claude→Gemini transform + retry + rate-limit
│   │   ├── antigravity_credits_overages.go # AI Credits handling + aggressive marking
│   │   ├── credits_policy.go               # [OpusClaw] Typed credits-policy accessors over legacy AICredits key
│   │   ├── antigravity_quota_scope.go      # IsSchedulableForModelWithContext()
│   │   ├── antigravity_quota_fetcher.go    # [Upstream] Verified AI Credits / usage truth from loadCodeAssist
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
│   ├── 090_drop_sora.sql                 # Sora 彻底移除
│   └── 091_add_group_account_filter.sql  # 分组过滤字段迁移 (renumbered from 081)
├── repository/
│   ├── channel_repo.go                 # [Upstream] Channel storage
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
| Aggressive credits exhaustion marking | `antigravity_credits_overages.go:239` | Mark on nil/error only (429 removed) |
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
| Credits false-positive fix | `antigravity_credits_overages.go` | Remove 429 from aggressive marking; remove ambiguous `"resource has been exhausted"` keyword; symmetric cache update in `clearCreditsExhausted()` |
| Post-injection 429 marking | `antigravity_gateway_service.go:719` | When `overagesInjected=true` and upstream still 429, mark credits exhausted to break hot-loop |
| Second-stage credits retry 429 | `antigravity_credits_overages.go:208` | Mark exhausted when `attemptCreditsOveragesRetry` returns 429 |
| Credits policy typing + UI semantics | `credits_policy.go`, DTO mappers, `AccountStatusIndicator.vue` | Separate local credits-path pause policy from verified AI Credits balance truth; preserve legacy AICredits key only as compatibility storage |
| Rate-limit window preservation | `antigravity_gateway_service.go:153` | `nextModelRateLimitReset()`: use `max(existing, proposed)` instead of blind overwrite |
| WaitPlan revalidation | `gateway_handler.go:396,643` | After `AcquireAccountSlotWithWaitTimeout`, re-fetch account and verify still schedulable |
| Sticky context propagation | `antigravity_gateway_service.go` + `gateway_handler.go` | Pass `groupID/sessionHash` via context into `Forward()`/`ForwardGemini()` for immediate sticky cleanup |
| Non-Claude-Code system→messages rewrite | `gateway_service.go` | Adopts upstream `rewriteSystemForNonClaudeCode()` — moves non-CC system content to synthetic user/assistant messages to bypass third-party app detection |

## 6. Key Constants

| Constant | Value | File | Purpose |
|----------|-------|------|---------|
| `antigravityRateLimitThreshold` | 7s | `antigravity_gateway_service.go:40` | Smart retry vs rate-limit boundary |
| `antigravityStickyPreserveThreshold` | 60s | `antigravity_gateway_service.go:49` | Short vs long rate-limit for sticky |
| `antigravitySmartRetryMaxAttempts` | 1 | `antigravity_gateway_service.go:42` | Max smart retry |
| `antigravityModelCapacityRetryMaxAttempts` | 5 | `antigravity_gateway_service.go:54` | MODEL_CAPACITY retries (was 60) |
| `creditsExhaustedDuration` | 30min | `credits_policy.go` | Local credits-path cooldown policy duration |
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
| `simCacheEphemeral1hThreshold` | 1800 (30min) | `sim_cache_service.go` | TTL >= this → cache_creation classified as ephemeral_1h |
| default simcache TTL | 3600 (1h) | `sim_cache_service.go` | Default TTL when config TTLSeconds <= 0 |

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

**SimCache TTL → ephemeral_1h 自动分类（`TTLSeconds >= 1800` 时）**：
当 SimCache TTL >= 30 分钟时，gateway_service 层自动将 cache_creation tokens 归入 `ephemeral_1h` 桶：
- Handler 层将 `override.TTLSeconds` 通过 `WithSimCacheTTL(ctx, ttl)` 写入 context
- 流式响应：SSE rewrite 在 `message_start` / `message_delta` 事件中调用 `rewriteCacheCreationJSON(u, "1h")`，生成嵌套 `cache_creation.ephemeral_1h_input_tokens` 字段
- 非流式响应：`applyCacheTTLOverride(&response.Usage, "1h")` + sjson 更新 body JSON
- RecordUsage（2 条路径）：`applyCacheTTLOverride(&result.Usage, "1h")` 确保计费用 `CacheCreation1hTokens`
- 优先级：account-level `IsCacheTTLOverrideEnabled()` > SimCache TTL（若 account-level 已启用则跳过 SimCache）
- `rewriteCacheCreationJSON` 可从扁平 `cache_creation_input_tokens` 自动合成嵌套 `cache_creation` 对象（兼容 Antigravity transform 层只输出扁平字段的情况）

### Scheduling Flow

```
Pre-filter (gateway_service.go:2026-2060):
  → Skip: rate-limited / temporarily unschedulable / overloaded
  → DO NOT pre-filter: credits-path-paused policy (deferred)

IsSchedulableForModelWithContext (antigravity_quota_scope.go:27-42):
  → model NOT rate-limited → schedulable
  → model rate-limited + overages + credits path NOT paused → schedulable
  → Otherwise → NOT schedulable

Concurrency (effectiveConcurrencyForSlot, gateway_service.go:183-198):
  → Antigravity + rate-limited (credits tier): 10 slots
  → Antigravity + NOT rate-limited (quota tier): 5 slots
```

### Credits State Model (Truth vs Policy vs Presentation)

```
Truth layer:
  → `antigravity_quota_fetcher.go` / `UsageInfo.AICredits`
  → upstream-reported AI Credits balance / credit type / minimum balance

Policy layer:
  → `credits_policy.go`
  → local scheduler cooldown stored via legacy `model_rate_limits["AICredits"]`
  → semantic meaning: "credits path paused", NOT authoritative balance exhaustion

Presentation layer:
  → DTO: `credits_policy_status`, `credits_policy_reset_at`
  → Frontend: `AccountStatusIndicator.vue`, `AccountActionMenu.vue`
  → UI wording: "积分通道暂停 / Credits Path Paused"
```

**Important:** `model_rate_limits["AICredits"]` remains a backward-compatible storage key only.
Do NOT present it as factual "credits exhausted" in new code. Verified credits truth comes from usage/quota fetch data, not from this local policy key.

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

## 10.1 Source Code Safety — Commit Early, Never Discard

**所有代码修改必须及时 commit，禁止执行会丢失未提交代码的 git 操作。**

**及时 Commit 规则：**
- 每完成一个逻辑单元的修改（编辑 + 测试通过 + 诊断通过），**立即 commit**，不等用户要求
- Commit 是免费的安全网——可以 amend、squash、revert，但未提交的工作区变更丢了就无法恢复
- 未提交的修改不受 `git reflog` 保护

**绝对禁止的 git 操作（无例外）：**
- `git checkout -- <file>` 或 `git checkout .`（还原工作区文件）
- `git reset --hard`（丢弃所有未提交修改）
- `git clean -fd`（删除未跟踪文件）
- `git stash drop`（丢弃 stash 内容）

**核心原则：未提交的工作区修改永远不属于你。**
- 工作区中的未提交修改可能来自其他 session、其他 agent、或用户手动编辑
- 即使修改看起来是子 agent 的 scope creep，也**绝对不能丢弃**——子 agent 改了额外文件通常有其原因（依赖联动、类型定义、配置同步）
- 你无法判断未提交修改的来源和意图，因此**没有资格决定丢弃**

**如果工作区有不属于当前任务的未提交修改：**
1. **不要动它们**——直接在其基础上工作
2. 只 `git add` 并 commit 你自己修改的文件
3. 如果你的修改与已有未提交修改冲突 → 停下来，告知用户，等待指示

**事故参考**：2026-04-05，一个 agent 看到工作区有 14 个文件的未提交修改，误判为子 agent 的 scope creep，执行 `git checkout` 全部丢弃。实际上其中包含另一个 session 完成的 retention_ratio 功能实现（含测试、部署），导致全部修改不可恢复地丢失，需要完整重新实现。

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
| (待填) | **merge: upstream Wei-Shaw/main (`6c73b621`) — v0.1.114** — 213 commits / 458 files (+71542/-6356)；payment system v2、balance/quota notify、websearch quota & failover、OIDC login、Anthropic credit-balance 400 处理、Cursor body compat、KYC 标记停止调度、opus-4.7 支持、scheduler cache loadFactor 同步等。冲突 3 文件（`setting_handler.go`、`server/routes/admin.go`、`setting_service.go`），均为上游新增 WebSearch/OIDC 路由 + handler/service 与 OpusClaw SimCache admin settings 共存合并；oauth_test UA 断言就地适配 OpusClaw `1.107.0 linux/amd64` patch |
| `86757918` | fix(scheduling): revalidate stale sticky account selections |
| `a96939cd` | fix(gemini): fast-fail invalid function response ordering |
| `e506112d` | **merge: upstream origin/main (1b79f6a7) — v0.1.110** — Redis snapshot meta fix; sync VERSION to 0.1.110; CCH signing + billing header sync; Go 1.26.2 CVE bump; empty output rebuild fix remains preserved from upstream |
| `30de5c50` | **merge: upstream origin/main (00aaf0f7) — v0.1.109** — upstream sync before v0.1.110 porting; follow-up cleanup landed in `520bbee4` / `a8f360e8` |
| `4166a6de` | **merge: upstream origin/main (8fa61516) — v0.1.107+v0.1.108** — Channel management; Sora removal; system→messages migration; renumbered migrations; 429/529 failover fixes |
| `06030f19` | fix(test): restore status=disabled detail in diagnoseSelectionFailure |
| `0744ffb1` | chore: renumber migration 081→091 to avoid upstream collision |
| `40aeeecf` | **fix(scheduling): harden antigravity retry and wait-path handling** — second-stage credits retry 429 marks exhausted; `nextModelRateLimitReset()` preserves later windows; WaitPlan revalidation after slot acquisition; sticky context propagation via `WithAntigravityStickyContext()` |
| `7930b2a9` | **fix(credits): break hot-loop by marking exhausted on post-injection 429** — when `overagesInjected=true` and upstream still 429, `setCreditsExhausted()` breaks cross-request scheduling loop |
| `ca0aaeb3` | **fix(credits): prevent false-positive credits exhaustion marking on new accounts** — remove 429 from aggressive marking; remove ambiguous `"resource has been exhausted"` keyword; symmetric cache update in `clearCreditsExhausted()` |
| `30107375` | **feat(simcache): upgrade TTL to 1h and auto-classify ephemeral_1h tokens** — default TTL 300→3600, TTL>=1800 auto-promotes cache_creation to ephemeral_1h in SSE/non-streaming/RecordUsage, rewriteCacheCreationJSON synthesizes nested object from flat aggregate |
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

### v0.1.114 merge (2026-04-18)

**对应提交**: `(待填)` — `merge: upstream Wei-Shaw/main (6c73b621) — v0.1.114`

**上游基线**:

- 起点：`1b79f6a7` (v0.1.110)
- 终点：`6c73b621`（main HEAD，对应 tag v0.1.114 + 后续未发版提交）
- 跨 4 个发版：v0.1.111 / v0.1.112 / v0.1.113 / v0.1.114
- 规模：213 个非合并 commits、458 文件、+71542 / −6356 行

**上游主要变更**:

- **Payment System v2** (touwaeriol & 团队，~50 commits)：完整支付重构，多 provider（Alipay / Wxpay / Stripe / EasyPay），订单生命周期、退款、负载均衡、H5/移动端、推荐链接、费率乘数等；新增 `internal/payment/` 包及 ent schema `payment_order` / `payment_audit_log` / `payment_provider_instance` / `subscription_plan`
- **Balance/Quota Notify** (~20 commits)：邮件提醒系统，支持余额低位、配额阈值、百分比、per-recipient timeout、saved-email 验证、tri-state、`balance_notify_service.go` 等
- **WebSearch 模拟** (~15 commits)：管理员可配置 Brave / Tavily 等 provider，失败转移、超时、配额加权负载均衡、admin test 接口；新增 `internal/pkg/websearch/` 包；新增 `gateway_websearch_emulation.go` 用于注入到 Anthropic API Key 账号
- **OIDC Login** (`02a66a01`, `8e1a7bdf`)：完整 OIDC 协议支持，登录回调、provider metadata 解析、PKCE、ID Token 验证；新增 `auth_oidc_oauth.go` handler、settings 端「最终生效」OIDC 配置解析
- **Channel 改进**：`account_stats_pricing` 自定义计费规则、features_config、channel_features 列；`account_test_service` 与 channel restrict 改造
- **OpenAI / Cursor 兼容**：Cursor `/v1/chat/completions` Responses API body 兼容、Codex transform 重构、Anthropic credit balance 400 转账号错误、Cursor warmup pipeline、ws-codex scheduler cache 修复、OpenAI 模型解析重构
- **Scheduler / 调度**：scheduler cache loadFactor 同步、KYC 上游响应停止调度（`5d586a9f`）、stop ratelimit miswrite、outbox watermark 修复、ctx pool ws mode 选项恢复
- **表格/前端**：表格排序与搜索后端化、全局表格分页配置、移动端账号 Usage cell 懒加载、QR code 密度优化、sidebar smooth collapse、版本下拉裁剪修复
- **Account 字段**：账号成本（account_cost）展示到 dashboard / usage 表格、QuotaDimensionRow / QuotaNotifyToggle UI 拆分

**冲突解决**（3 文件，均为 OpusClaw SimCache admin settings 与上游新增配置 API 在同位置追加）:

| 文件 | 解决方式 |
|------|---------|
| `backend/internal/server/routes/admin.go` | 同时保留 OpusClaw `/simulated-cache` GET/PUT 与上游 `/web-search-emulation` GET/PUT/POST(test/reset-usage) 路由 |
| `backend/internal/handler/admin/setting_handler.go` | 文件末尾并存 OpusClaw `GetSimCacheSettings` / `UpdateSimCacheSettings` 与上游 `GetWebSearchEmulationConfig` / `UpdateWebSearchEmulationConfig` / `ResetWebSearchUsage` / `TestWebSearchEmulation` |
| `backend/internal/service/setting_service.go` | `SettingService` struct 字段同时保留上游新增的 `proxyRepo` / `webSearchManagerBuilder` 与 OpusClaw 的 `simCacheService`；方法体中保留 OpusClaw `GetSimCacheSettings` / `SetSimCacheSettings` 与上游 `GetOIDCConnectOAuthConfig` 一族 |

**Migration**：

- 上游新增 17 个迁移文件（091–107），其中上游自身已存在重复前缀（091/095/101/102 各有多个文件，按完整文件名字典序排序运行）。
- OpusClaw 既有 `091_add_group_account_filter.sql` 与上游新 `091_add_group_messages_dispatch_model_config.sql` 共存。两者文件名不同、内容互不重叠且 OpusClaw 那条幂等（`ADD COLUMN IF NOT EXISTS`），按字典序「091_add_group_account_filter.sql」先于「091_add_group_messages_dispatch_model_config.sql」运行，**无需重命名**。
- 跟踪键是完整 filename（`schema_migrations.filename` 主键），任何重命名都会导致旧条目孤立 + 新文件再跑一遍，故按现状保留。

**测试结果**：

- `go build ./...`：通过
- `go vet ./...`：通过
- `go test ./internal/service/...`：仅 3 个 v0.1.110 已知失败（`TestHandleGeminiStreamingResponse_ThoughtsTokenCount` / `TestHandleClaudeStreamingResponse_ThoughtsTokenCount` / `TestExtractGeminiUsage`），无新增回归
- `go test -tags unit ./internal/pkg/antigravity/...`：1 个失败 `TestConstants_值正确`，已就地修正断言适配 OpusClaw UA `1.107.0 linux/amd64` patch
- `go test ./internal/handler/...`：2 个 pre-existing 失败 `TestHandleFailoverError_BasicSwitch/非重试错误_Antigravity_第一次切换无延迟` 和 `TestHandleFailoverError_IntegrationScenario/模拟Antigravity平台完整流程`，根因为 OpusClaw 指数退避 patch（`failover_loop.go:116-128`）首次切换即注入 1s 延迟；与本次合并无关，已记入 §2 已知失败列表

**维护提示**:

- 当前长期 merge 分支建议更名为 `opusclaw/v0.1.114-merge`（与既有命名约定一致）
- 本仓库 `claude/merge-upstream-updates-RVze7` 是本次任务的工作分支
- v0.1.114 引入大量新前端/新表，部署前请确认 17 个新 migration 在生产 DB 顺利执行，并且 ent generated 代码已同步（本次合并已纳入 `backend/ent/` 全套生成产物）

### v0.1.110 merge (2026-04-09)

**对应提交**: `e506112d` — `merge: upstream origin/main (1b79f6a7) — v0.1.110`

**上游基线**:

- tag: `v0.1.110`
- merge target: `origin/main@1b79f6a7`

**合并后本地追加修复**:

- `a96939cd` — `fix(gemini): fast-fail invalid function response ordering`
- `86757918` — `fix(scheduling): revalidate stale sticky account selections`

**维护提示**:

- 当前长期 merge 分支名已规范为 `opusclaw/v0.1.110-merge`
- 本地分支跟踪 `origin/main` 仅用于查看 ahead/behind 与继续做 upstream merge，**不是**为了直接 push 到 `main`

### v0.1.109 merge (2026-04-08)

**对应提交**: `30de5c50` — `merge: upstream origin/main (00aaf0f7) — v0.1.109`

**后续清理**:

- `520bbee4` / `a8f360e8` — `fix(simcache): clean v0.1.109 merge fallout`

### v0.1.107+v0.1.108 merge (2026-04-07)

**合并范围**: 109 commits (non-merge), 150+ files

**上游变更摘要**:

- **渠道管理系统 (Channel Management)** — erio, ~60 commits
  - 支持模型映射 (Model Mapping)、价格配置 (Pricing)、使用量统计 (Usage Stats)
  - 引入 `channels`, `channel_models`, `model_prices` 等表
  - 路由逻辑重构以支持渠道选择
- **Sora 彻底移除** — erio, ~16 commits
  - 移除所有 Sora 相关代码、配置及前端页面
- **OAuth/OpenAI 改进** — DaydreamCoding
  - 增强隐私模式、令牌刷新优化、plan_type 同步
- **Bug Fixes & 优化**
  - **system→messages 迁移** (`f3aa54b7`): 将 system 指令重写为 synthetic messages 以绕过第三方应用检测
  - **响应重建** (`8fa61516`): 非流式路径在 output 为空时从 delta 事件重建响应
  - **Failover 增强**: 透传 429/529 错误以触发上游 failover 逻辑

**冲突解决 & 调整**:
- **Migration 冲突**: 将 OpusClaw 的 `081_add_group_account_filter.sql` 重编号为 `091`，避开上游 `081-090` 渠道及 Sora 移除的迁移脚本
- **代码冲突**: 解决 `gateway_service.go`, `account_service.go` 等 9 个核心文件的逻辑冲突，并修复 3 处损坏的自动合并
- **Patch 保留**: 完整保留模拟缓存计费 (SimCache)、Antigravity 专有转换、细粒度并发控制等 OpusClaw 核心补丁

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
