# 推理历史桥接修复与 Chat 往返边界

基线为 2026-11-05 已上线的 `2.18.0.29-ritel`，提交 `b3c72ec1f7a6718133e29e99899165dfd043d3c3`。本变更只修复两处确定的历史损失；不新增存储、公开配置、密文 envelope 或引用协议。验证使用合成上游，不证明模型任务质量提升。

## Messages 到 Responses

原来的通用转换器无条件丢弃 `gAAAA` 格式签名，本意是防止 xAI 解密外来状态失败，却也丢掉发回 OpenAI 的推理。现在由服务层选择转换策略：

- 已知 OpenAI Platform API Key 目标，以及现有 Codex OAuth / SetupToken 路径，允许 OpenAI 格式密文；其他厂商格式、原生 Claude 签名及无签名占位不会因此发送到 OpenAI。
- xAI 和未核实的兼容站保留原先的格式限制，不因为请求模型名是 GPT 就放行。
- 格式分类忽略首尾空白，防止绕过原过滤；允许回传的 opaque 原件不改写。格式识别不等于认证签发账号，实际密文有效性和账号兼容性由上游校验。本次没有引入或承诺跨账号来源恢复。
- 按 assistant 块原顺序产生 reasoning、text、function_call，避免将后置推理移到前面。相邻 text 仍沿用双换行，工具 ID/参数与对应结果保持。

入口是 `internal/service/apicompat_config.go` 的目标判定与 `openai_gateway_messages.go` 调用；转换选项和有序回放在 `internal/pkg/apicompat/anthropic_to_responses.go`。默认公共转换函数继续采用保守兼容策略，实际网关显式提供目标选项。

## Messages 到 DeepSeek Chat

原来只恢复有 tool_calls 的 assistant thinking，普通 assistant 轮次即使带原始推理也会丢失。现在在模型映射后确定实际 DeepSeek 目标，并在请求包含 tools 时启用普通轮回放。

新路径只读取 unsigned thinking 明文；signed、redacted 或只有密文的块不被当作 DeepSeek 原始推理。非目标、无 tools、原有工具轮和既有缺失占位行为保持。使用现有 DeepSeek 目标判据，避免另一个 alias 的 DeepSeek 映射改变当前非 DeepSeek 请求。

入口和选项位于 `internal/service/openai_gateway_messages_chat_fallback.go` 与 `internal/pkg/apicompat/chatcompletions_anthropic_bridge.go`。

## Chat 到 Responses 的实际客户端证据

使用本机现存 OpenCode `1.14.22`，隔离全部用户配置和环境，仅连接本地 fake SSE。每组执行一次真实 `read` 工具，再通过新的 CLI 进程续接同一 session；4 组共 12 次 localhost 请求、4 次 fixture 文件读取、8 个 CLI 进程正常退出。没有访问真实模型。该版本不代表上游最新版。

| 客户端配置 | 下一轮与跨进程续轮 |
|---|---|
| 默认 Chat | 回传 reasoning_content 和其中的 comment；丢弃独立 encrypted_content、reasoning_items、reasoning_details 和任意 extra 字段 |
| Chat 显式 interleaved | 同上 |
| 原生 Responses | 在 store:false 下回传 encrypted_content、summary 和工具关联；不回传原 rs_* id，不依赖 previous_response_id |
| Chat 实验 inline envelope | 296 字节的完整合成 reasoning item comment 原样回传，解码内容逐字段一致 |

因此，给 Chat delta 增加未知字段不能使现有客户端完整回传推理状态。当前网关的 `reasoning_content` 仍编码为普通 assistant `<thinking>` 文本，第三条转换路径在本次不变；实验 envelope 只证明小样本载体可行，没有实现入口解封，不能称已修复完整回放。

优先使用已有原生 Responses。若有必须使用 Chat 的实际调用方，最小备选是有版本的 inline envelope，保存完整原件、原顺序与必要来源信息，在入口解封并移除文本载体；它无需新增存储，但增加反复传输的体积，并需明确不能验证来源时的处理。仅在实际 payload 大小证明需要时，才考虑 principal/account 隔离的短引用和原件存储；后者还要定义 TTL、过期、丢引用、缓存丢失与持久保留合同。不能把当前可丢失缓存称为无损历史。

本次未选择或实现新承载协议。实测不覆盖任意大小、上下文压缩、所有客户端、真实密文解密或实际任务成功率。

## 验证与复现

本地报告目录 `reports/stravia-reasoning-implementation-20261105/`（servers-ws）保存修前失败、修后 JSONL、客户端请求/进程输出、源码与二进制 SHA；`client-evidence/probe-client.mjs` 可重跑隔离客户端，`verify-evidence.mjs` 对固定捕获逐字段验收。

新增回归覆盖：OpenAI API Key / Codex OAuth / SetupToken 与 xAI / 未知 relay 的格式隔离；空白不能绕过过滤；流式 signature_delta 回传；三轮有序推理与工具历史、签名-only、相邻正文保持、原输入不变；DeepSeek 映射前后目标隔离、普通轮 unsigned 明文及三次工具往返。

最终转换包完整回归 491 个顶层测试（含子例 640）通过；服务层定向回归 83 个顶层测试（含子例 114）通过；失败和跳过均为 0。两项缺口均先取得修前失败证据。格式检查与独立代码审阅完成，客户端捕获已独立逐字段复核。

从 backend 目录使用项目 Go 1.27.3：

```sh
GOMAXPROCS=2 GOGC=50 go test -p 1 ./internal/pkg/apicompat -count=1
GOMAXPROCS=2 GOGC=50 go test -p 1 -tags=unit ./internal/service -run 'Test(ForwardAsAnthropic|OpenAIGatewayService_ForwardAsAnthropic|ApplyAnthropicCompatFullReplayGuard|NormalizeOpenAIAPIKeyStoreFalseReasoningReplay|.*OpenAICompat.*Continuation|.*Grok.*Encrypted|.*StripAnthropicThinking)' -count=1
git diff --check
```

官方协议核对入口为 OpenAI Reasoning models 与 DeepSeek Thinking Mode；本次没有调整推理 effort、模型映射、账号数据、重试或计费策略。三轮协议保留测试与真实客户端对假上游测试均不是生产模型质量 A/B。
