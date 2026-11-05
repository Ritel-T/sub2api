# 首页一键创建 Key 的国模默认分组修正

2026-11-05：只读核实 vm-la 正式版本 2.18.0.30-ritel，revision 1e779f6ba99d02eca5fb40c1fabf51fa6449de79，image 5f150642a800dfcb3a4b843bc605fc766c13d4437810dd56868a04e8d0d604cb，healthy。本分支基于该线上提交；本次未发布、未创建真实 Key、未更改分组或既有 Key。

## 当前行为与原因

`groups/available` 按用户权限与有效订阅过滤，按 sort_order / ID 升序返回。首页每个平台取第一个 active 分组，国内平台合并到 domestic；只有 Grok 优先选择名称 Grok 060。

本次 Admin API GET /admin/groups/all 的公开标准组顺序与首页逻辑推导：OpenAI → ChatGPT 060 (12)，Anthropic → Kiro 060 (11)，国模 → DeepSeek 无限 (23)，Grok → Grok 060 (13)，Gemini → Gemini 070 (21)。国模 060 (17) active，但 sort_order 90，排在 DeepSeek 无限的 0 后。具体用户仍受其授权与订阅过滤影响；没有冒充读取每位用户会话。

## 修正

在用户已经可绑定的 active 国内组中优先选择名称为“国模 060”的组，沿用 Grok 的现有模式，不固定 ID。该组不可用时仍取接口返回的第一个 eligible 国内组。其余平台与全局排序保持原逻辑；已有 Key 不回写。

## 验证

原代码下新增代表回归失败：期望 817，实际选 23；修复后首页既有套件 26/26 通过。覆盖无限组在前、060 使用变化后的 ID、只显示一个国模入口、060 inactive 回退，以及原有创建中防重复与未知状态逻辑。定向 ESLint、vue-tsc --noEmit、git diff --check 通过。没有发送真实 Key 创建或模型请求。


## 后续第9项实施

按ID配置已在同分支追加实现，名称例外被替代；完整合同、检查及统一发布配置清单见 [CONFIGURATION.md](CONFIGURATION.md)。上文为首轮历史，不能把原名称逻辑当最终候选。
