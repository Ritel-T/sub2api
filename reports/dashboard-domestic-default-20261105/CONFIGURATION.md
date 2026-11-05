# 第9项：按ID配置首页默认组（候选交付）

只提交候选，未部署/改生产设置/分组/已有Key，未创建真实Key。后台设置保存测试使用既有内存repository stub；视觉预览使用实际组件及合成组数据，不冒充线上保存验收。

## 基线与整合

分支 `fix/dashboard-domestic-default-20261105` 延续 ff706bcdbc022d5928e4d99c5e47d00d60129aa6，祖先是实测线上 .30 / 1e779f6ba99d02eca5fb40c1fabf51fa6449de79。保留ff历史及回归成果，最终按ID替代全部国模/Grok名称例外。统一候选9a0a614e7057f8647ecae61c2a39aeea226f53ed同样包含.30；只合本分支相对.30的两个提交，勿覆盖整份旧设置文件。无需数据库迁移、不改VERSION/Worker映射，版本和canonical构建由统一整合承担。

共享路径主要是 backend/internal/service 的 setting_public.go、setting_parse.go、settings_view.go，handler/dto/settings.go，以及 frontend/src/views/admin/SettingsView.vue。保留统一候选既有上游 account_performance / openai_response_header_timeout 字段、图片.28/.29、支付宝.30和性能改动。

## 设置合同

一个现有settings条目 `quick_key_default_groups`，JSON对象 `{平台bucket: 正整数分组ID}`。例如 `{"domestic":17,"grok":13}`。domestic沿用原国模聚合定义；其他bucket沿用现有平台目录。默认空对象，留空条目删除该覆盖。后台“站点设置”API Base URL之后展示实际组名与ID下拉框，随现有页面保存；保存用现有Admin API，不引入新权限或平台。

GET /admin/settings、更新响应、公共settings及HTML注入均传递该对象。公共仅传bucket→ID，不附加管理组列表、权限或凭据；后台单独用原GET /admin/groups/all获取可选启用组。局部更新省略该项则保留原配置，{}显式清空；null、非正数和非整数拒绝。

首页只匹配GET /groups/available原有权限过滤后的active组，而且须属于相同bucket。无配置、组删除/禁用、用户无权限或跨平台目标时都取该bucket接口顺序中第一个active组；没有可用组则不增加按钮。不会跨权限补组，不改列表排序或已有Key。创建接口仍执行既有鉴权，不依赖前端授权。

## 统一发布配置清单（待发布执行，本轮未写）

2026-11-05 20:52:39 UTC重新只读核实vm-la仍为.30、image5f150642a800…；证据groups-readonly.safe.json。当前目标：国模060=17/deepseek/active，Grok060=13/grok/active。统一发布前重读实际组列表并核名称、平台、启用状态后，在后台设domestic→当时国模060 ID、grok→当时Grok060 ID；当前核对值的最小对象是 `{"domestic":17,"grok":13}`。这是延续期望国模060和原Grok060行为的必要发布配置；不要仅发布空默认后宣称行为延续。若届时已有配置，合并其他平台条目，不以本例覆盖。

当前其余普通用户默认由现有排序给出：OpenAI ChatGPT060=12，Anthropic Kiro060=11，Gemini070=21；可在同一表单另设，非本轮强制写入。用户权限可能不同，均须遵守available结果。

统一上线时保留该settings行最小同机原件，保存后GET读回，并核公共settings字段；验证首页以用户当前available组交集选择，不需要代创建真实Key。代码回退不回灌业务库；本配置行按实际需要恢复。没有名称兼容逻辑，改组名不会改变已配ID。

## 必要验证

- 原26项首页回归延续为27项，按ID配置替换原名称例外断言，补无权限目标/错误平台不扩按钮检查；27/27通过。
- 后台实际组件下拉框展示名称及ID、发出数字ID和清空配置，1/1通过。
- Go现有Admin settings harness：保存→持久化映射→读回→公共/HTML注入一致，省略字段保留、{}清空、非法输入拒绝；1项通过（GOTOOLCHAIN=go1.27.3）。
- 定向ESLint、vue-tsc --noEmit、现行check:design及diff --check通过。复用既有其余验证，不重跑全套/真实PG或构建；统一候选合入后由整合任务完成必要合并检查和canonical构建。
- 实际组件1280px合成视觉预览settings-preview.png已检查，名称和选中ID可见、两列无截断；表单交互由上述组件测试覆盖。不等同生产管理员保存。

设计依据锁定现有SettingsView站点区块及CcsImportSettings的AppHeading/包装label/input下拉框，复用原间距、两列断点和页面保存方式，无新视觉系统。
