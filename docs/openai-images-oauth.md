# OpenAI OAuth 图片接口

客户端使用 OpenAI SDK 的 Images generation/edit 接口访问本站。OpenAI OAuth 账号通过 ChatGPT Codex 后台认证，API Key 账号使用其原有 Platform/自定义上游；两者保留独立的模型权限与价格合同。

## OAuth 转发

`gpt-image-2.5` 使用既有原生 `/images/generations`、`/images/edits` 路径。编辑附件转换为原生 images/image_url 数据；纯图片调用不额外使用文本主模型。原生端点明确不支持且另一协议可完整表达原请求时才允许协议回退；不丢失图数、附件或编辑参数。未知发送结果不能视为没有执行。

网关自建的裸 2.5 Responses 图片请求不显式注入 reasoning.effort。2026-11-05 同账号单变量上游验证中，省略该字段成功，显式 medium 则返回 complexity 不支持。此规则不修改用户在真实文本 Responses 请求中明确选择的推理强度。

## 模型与输出边界

`gpt-image-2.5` 是 OAuth 上游接受的兼容调用名。当前无法独立证明它等同于 Platform API 的 `gpt-image-2.5-sunburst` 或 `gpt-image-2.5-flare`；请求模型、上游发送模型和实际返回模型应分别解释，上游未回显不能伪造确认。OAuth 账号不会因此自动取得其他模型权限或切换到 API Key 供给。

2026-11-05 官方 SDK 经本站的验收发现，裸 `gpt-image-2.5` 的 OAuth 上游不兑现明确的尺寸与较高画质：low/medium/high/auto 均曾返回 quality=low，1024x1024 与 1024x1536 请求均曾返回 1536x1024；Responses 高画质对照也未绕过该限制。

因此，本模型的 OAuth Images 接口仅接受 quality 缺省、auto 或 low，以及 size 缺省或 auto。其他明确 quality/size 在凭据获取和生成前返回 HTTP 400，错误码分别为 `unsupported_image_quality` / `unsupported_image_size`，param 指向相应字段。客户端必须明确改成 auto 才能继续；本站不静默降画质、缩放、换模型或切 API Key 账号。此限制在渠道/账号模型映射后判定，适用于生成和编辑；API Key、其他模型以及用户真实文本 Responses 请求保留独立合同。

输出尺寸以实际图片和返回元数据为准，auto 不承诺具体方向或尺寸。需精确尺寸/较高画质/具体引擎的调用应选择具备相应已验证能力的服务；未知能力不宣称支持。

成功必须有真实图片；HTTP 200 的 SSE 错误/response.failed 仍是失败。生成图片的数量、尺寸及 usage 应按实际结果记录，费用按现行本站价格合同核对；超时/部分输出或未知结果不通过本修复新增自动重发。


## GPT Image 2.6（2026-11-05候选，尚未发布）

官方正式名为 `gpt-image-2.6-sunburst` 和 `gpt-image-2.6-flare`。没有已核实的裸 `gpt-image-2.6` 别名；不得把它映射到2.5或把两个引擎互相替换。本地模型目录/选择项和精确价格回退已补齐；账号实际可用模型仍由既有模型映射、白名单和上游权限决定，不修改原生账号模板或存量账号调度。

两个精确名使用原生 Images generation/edit。2.6 不自动回退到自建 Responses 图片工具：目前官方新能力合同明确针对 Image API，没有证明另一协议能够完整表达这些参数。用户真正的 Responses 请求继续保留原请求合同，但本轮未证明2.6工具调用可用。未知执行结果、空/截断/5xx及流中失败沿用已有终态、防重发和实际模型记录机制；上游没有回显模型时不补造确认。

`complexity=low/medium/high` 控制生成规划，缺省由上游选 high，与 `quality` 独立。原生适配保留该字段，以及 GIF 的 `frame_count` / `frame_duration_ms`。2.5裸别名的 OAuth 画质/尺寸能力门仍只用于2.5，未外推到2.6。参数保留和协议测试不能证明OAuth账号实际兑现这些参数。

API Key 路径保留官方 JSON / multipart 及 markup 附件。OAuth markup 等未核实的转换能力会明确拒绝，避免附件被忽略。SVG仅生成、n=1，不支持编辑、stream、partial_images和自定义压缩；GIF可生成/编辑，但不支持stream、partial_images、自定义压缩；帧参数只用于GIF。GIF帧数2–16，时长20–2000毫秒；实际帧时长圆整由上游处理。可选格式未做真实生成验收。

定价来源：官方模型页与 pricing，2026-11-05抓取。两者标准每百万 token：文本输入$5、缓存文本输入$1.25；图片输入$8、缓存图片输入$2；图片输出$30。沿用现有多模态usage拆分、缓存和倍率，不套旧图片模型费率，不改老模型定价。SVG还可能含作者模型文本用量，不应把每张图片计算器估值当总价；本站只按已有返回usage合同结算，SVG真实usage覆盖仍待确认。

只读现场：官方API Key账号185的 `/models` 返回两个精确2.6型号而无裸别名；OAuth账号113 Codex `/models` 仅列文本驱动，没有图片型号，该缺省不能证明不支持。本站现行映射未开放2.6，近七日没有2.6图片usage。本轮没有付费生成、编辑或流式发送，没有新增账号白名单，不声称真实出图或唯一账务验收完成。需要先经协调决定最少付费验收，再按结果开放精确供给。

官方资料：
- https://developers.openai.com/api/docs/models/gpt-image-2.6-sunburst
- https://developers.openai.com/api/docs/models/gpt-image-2.6-flare
- https://developers.openai.com/api/docs/guides/image-generation
- https://developers.openai.com/api/docs/pricing
