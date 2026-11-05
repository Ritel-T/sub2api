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
