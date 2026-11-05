# OpenAI OAuth 图片接口

客户端使用 OpenAI SDK 的 Images generation/edit 接口访问本站。OpenAI OAuth 账号通过 ChatGPT Codex 后台认证，API Key 账号使用其原有 Platform/自定义上游；两者保留独立的模型权限与价格合同。

## OAuth 转发

`gpt-image-2.5` 使用既有原生 `/images/generations`、`/images/edits` 路径。编辑附件转换为原生 images/image_url 数据；纯图片调用不额外使用文本主模型。原生端点明确不支持且另一协议可完整表达原请求时才允许协议回退；不丢失图数、附件或编辑参数。未知发送结果不能视为没有执行。

网关自建的裸 2.5 Responses 图片请求不显式注入 reasoning.effort。2026-11-05 同账号单变量上游验证中，省略该字段成功，显式 medium 则返回 complexity 不支持。此规则不修改用户在真实文本 Responses 请求中明确选择的推理强度。

## 模型与输出边界

`gpt-image-2.5` 是 OAuth 上游接受的兼容调用名。当前无法独立证明它等同于 Platform API 的 `gpt-image-2.5-sunburst` 或 `gpt-image-2.5-flare`；请求模型、上游发送模型和实际返回模型应分别解释，上游未回显不能伪造确认。OAuth 账号不会因此自动取得其他模型权限或切换到 API Key 供给。

输出尺寸以实际图片和返回元数据为准。2026-11-05 上游实测中，1024x1024 请求返回 1254x1254，故不能承诺 OAuth 严格兑现每个请求尺寸，也不做隐式缩放或重复生成。需精确尺寸/具体引擎的调用应选择具备相应已验证能力的服务；未知能力不宣称支持。

成功必须有真实图片；HTTP 200 的 SSE 错误/response.failed 仍是失败。生成图片的数量、尺寸及 usage 应按实际结果记录，费用按现行本站价格合同核对；超时/部分输出或未知结果不通过本修复新增自动重发。
