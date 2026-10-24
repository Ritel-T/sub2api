# 开发与验收

## 只用三个层级

| 时机 | 命令（frontend目录） | 范围 |
|---|---|---|
| 编辑中 | `pnpm test <spec路径>`；单次用`pnpm test:run <spec路径>` | 明确受影响测试，watch复用进程 |
| 阶段收口 | `pnpm verify:local` | 全源码设计门＋内容缓存ESLint＋类型；另附本批行为测试 |
| 最终候选 | `pnpm verify:release` | 设计门、规则自测、无缓存ESLint、全量Vitest、项目类型、正式打包各一次 |

文档只查内容/链接。单测试改动只跑该测试。页面改动补其接线与业务边界；共享组件补消费者。全局setup、依赖、构建配置及广泛改动在最终候选跑完整入口。通过后没有相关输入变化就停止重跑；失败先修并定向复验。完整入口通过后不另跑lint/type/build。

## 环境与隔离

默认jsdom，真正纯逻辑/源码文本/模拟API测试在文件首行声明`// @vitest-environment node`。核运行时导入闭包并运行该spec后才迁移；不为省环境而mock掉被测行为。Node环境没有storage、observer、idle等浏览器补丁。DOM、真实渲染、浏览器持久化继续用jsdom；保留文件隔离和10秒超时。

时间逻辑推进假时钟并断言边界前后，每例恢复timer/global/mock。共享setup不导入Vue测试工具，也不维护无内容的全局stub。相同模块、同mock边界的小spec并回主spec复用fixture；不同隔离边界不强并。新缺陷放到最接近的主spec，不按轮次新建review/audit文件。

## 覆盖职责

- 共享组件/状态机：防重、身份失效、ABA、晚包、失败解锁等边界。
- 消费者：真实对象、载荷、权限和金额接线；共享helper已测不等于接线正确。
- AST门：全量源码禁用写法；规则自身正反例不能用业务测试替代。
- 浏览器：实际CSS/字体/焦点/滚动，选择本批受影响的语言、主题、尺寸、状态，不做机械全排列。

同层同输入同断言且有明确替代才删除重复用例；不以减少测试数为目标，不关闭隔离、放宽断言或增加skip。

## 入口边界

`lint:cached`使用ESLint content策略，缓存仅在node_modules/.cache，工具配置变化由ESLint失效。插件/依赖变动用无缓存完整入口；缓存不是验收证据。

独立`build`保留设计、locale和vue-tsc门；`lint:check`保留设计门。Vite checker保留dev反馈，build不再重复检查；裸`vite build`仅打包。全量已含locale，完整入口不嵌套build/lint:check。coverage仅在覆盖审计时运行。

当前Vitest related在本树依赖图分析PARSE_ERROR，故使用明确spec路径；也不能靠它覆盖readFileSync/CSS/动态入口。检查命中数量，不加passWithNoTests；累积未提交改动不能用HEAD充当本轮范围。

每批保存基线/任务差异、依赖锁身份、命令/退出码和未验项；一份最终日志、一份必要恢复记录，失败日志仅保留诊断价值。普通本机改动不复制整树/反复恢复演练，迁移和生产遵守独立合同。工程通过不代表全站视觉或生产业务通过；候选必须与最新生产整合后才可发布。
