# 共用微信小程序商品的来源充值

本渠道沿用合作方现有小程序、商品和付款入口，不新增本站 API 入口。本站仅处理自己创建的来源订单；共用商品下其他来源的付款不进入本站余额。

当前为开发候选，默认没有新增生产配置。2026-10-15 收到正式接入包后，已只读核验独立来源 `ritelsi`、登记名称 `RitelSI`、登记网站 `https://api-us.ritel.si`、共用商品 `"3"`–`"8"` 和服务端渠道字段。正式签名事件/查单可用，现有安卓测试单已全退，不可用于补发本站额度。尚需本站部署、启用和新的用户主动付款验收；iOS 真机付款仍未验证。

## 金额与实际付款渠道

按用户确定的费率，安卓虚拟支付 1%，iOS Apple IAP 12%。六档人民币现金商品保持不变：

| 实付人民币 | 安卓到账额度 | iOS 到账额度 |
|---:|---:|---:|
| 10 | 9.90 | 8.80 |
| 30 | 29.70 | 26.40 |
| 50 | 49.50 | 44.00 |
| 100 | 99.00 | 88.00 |
| 200 | 198.00 | 176.00 |
| 500 | 495.00 | 440.00 |

网页在下单前同时展示两种到账结果，并冻结到签名报价。报价内 `credited_amount_usd` 暂存较低的 iOS 结果作为档位标识，不是已经确定的到账额；`credit_by_channel` 保存双方结果。付款确认时按可信渠道，在同一个数据库条件更新中写入 `PAID`、最终 `amount` 与渠道快照，再调用原幂等充值/返佣路径。订单原始报价保持不变，已结算金额和渠道不可被重试改写。

不以浏览器 User-Agent、本站设备、小程序客户端上报的系统信息或用户自选系统作为计费证明。电脑下单、换手机扫码仍按实际支付渠道结算。未知渠道不按低费率猜测。

## 合作接口

沿用附件约定的后端接口，API origin 固定 `https://alar1c.cn`：

- `POST /api/partner/v1/orders`：`{partnerOrderId, amount}`，`amount` 为整数分。
- `POST /api/partner/v1/order`：`{id}`，查询来源订单。
- 请求采用原四个 `X-Gelv-*` 头与 HMAC-SHA256；重试保持来源单号及金额，重新生成 nonce/时间戳。

配置包含 `partnerId`、`partnerSecret`、`productIds`（逗号分隔，填写共用的现有商品 ID）。不需要本站持有微信 AppKey、商户私钥或 OpenID。密钥经原配置加密存储，管理响应隐藏该字段。一次仅启用一个 Gelv 实例，支付类型为 `gelv`，展示为“微信小程序”，仅用于余额充值。

### 正式合作协议字段

下列字段已在本次正式接入包说明，并通过独立来源的真实只读查单核对。它们是合作服务协议，**不是微信官方原生字段**。完整正式文档为 `RitelSI-integration.zip/API.md`。

```json
{
  "order": {
    "id": "0123456789abcdef01234567",
    "partnerOrderId": "sub2_example_source_order",
    "amount": 10000,
    "currency": "CNY",
    "productId": "6",
    "status": "paid",
    "refunded": 0,
    "wxOrderId": "example-only-platform-payment-id",
    "paymentChannel": "apple_iap",
    "channelVerified": true,
    "channelVerificationStatus": "verified",
    "feeRateBps": 1200
  }
}
```

`paymentChannel` 只允许 `wechat_virtual`（适用 1% 的已核验虚拟支付）或 `apple_iap`（适用 12% 的已核验 Apple IAP）。合作方必须依据微信官方服务端查单/已验证通知、与同笔订单匹配的支付凭证确认渠道，不能把客户端的 `platform` 原样改名返回。`channelVerified=true` 表示已完成该服务端核验。付款完成后渠道应保持不变；纠正错误走人工对账，不让重试改变已发额度。

待付款可无渠道或为 false；已付款必须同时满足 `channelVerified=true`、`channelVerificationStatus=verified`，且 `feeRateBps` 与冻结规则（100 或 1200）一致。渠道冲突/缺失或费率不符时保留待核对并记录 `GELV_CHANNEL_REVIEW_REQUIRED`，不加余额。不提供完整用户凭证、OpenID、Apple 原始收据或格律激活码给本站。`refunded` 已明确为累计退款分，不能把布尔值、单次金额和累计金额混用。productId 按正式字符串解析，并按对应档位固定到单一商品。

正式包称 iOS IAP 后台开通由用户确认、appleAvailable 已启用；本轮没有 iPhone 付款证据。开关可用与真机实付成功分开验收。

结算字段 `settlementState/platformFeeFen/cpsFeeFen/settledAt` 为平台返回的原值；缺失保留未知，数值零保留为已知零。付款后补到的结算数据采用 JSON 状态比较更新原订单证据，不修改余额或重发返佣。平台服务费字段不视为全部 Apple 佣金，不能用其空/零来判断渠道。

## 后台恢复和退款

首版采用持久本地订单主动查单，复用现有支付周期的单实例锁；不依赖用户保持网页打开，不虚构 webhook。活跃订单每轮最多 15 笔、历史已绑定订单最多 5 笔，按上次处理时间轮转；所有历史来源单持续可审计。相应周期频率、合作方限流和真实流量需联调核定。

创建响应丢失时，在订单有效期内用同一个来源号和现金金额重试，恢复上游 ID 与原码；没有新码不会清掉已经保存的码。已过期且从未持久化上游 ID 的空意向不重新创建。已绑定来源单即使网页取消或过期，仍能核对并按原报价恢复到账。

数据库部分唯一索引限制 `(provider_instance_id, payment_trade_no)` 在 `provider_key='gelv'` 下不可重复；另一个部分索引约束同实例的 `gelv_wx_order_id`，防止同一微信平台支付被两个不同来源订单重复认领。平台交易号与最终额度、渠道一起绑定到 PAID。现有其他渠道和空交易号不受影响。

充值沿用原固定兑换码，兑换码核销与余额增加同事务；支付状态、最终完成及返佣分步恢复。原始报价冻结两种额度，已核销的兑换码不会因重启再次加款。退款状态不会触发充值；首版记录 `GELV_REFUND_REVIEW_REQUIRED` 供管理员核账，不自动退款或扣回余额。既有规则不自动追回已发返佣，需一起人工核对。Apple IAP 的退款由对应平台流程处理，不冒充支持合作方自动退款。

## 配置及上线

1. 将本分支合入实际发布基线前重新核对并发变更及迁移编号；本分支已同步至 `faef78969`（2.11.0.5），保留邀请注册、首充奖励与观测修补，不代表已经部署。
2. 备份实际数据库和支付配置，执行 `288_gelv_external_order_identity.sql`。该迁移只加两个 Gelv 部分唯一索引，不改旧财务行；平台交易号的 JSON 表达式索引由 SQL 迁移管理。
3. 后台保存关闭的 Gelv 实例，填写独立来源/私密密钥/现有商品白名单；不复用开发者测试来源。交付文件用 `secret`，Admin 配置对应 `partnerSecret`，应在内存映射，不原样输出配置或写入源码。本轮已准备 Windows DPAPI 加密的关闭状态请求候选，尚未提交。
4. 确认双方渠道字段已就绪、当前 iOS 可用范围和退款对账责任，再按用户授权启用渠道/支付类型。全局报价和其他已有渠道不因本次接入重定价。
5. 分别以安卓/iOS 做主动确认金额的小额真实支付，核对支付单、渠道、最终额度、一次兑换和返佣；关网页、重启、重复查单及退款各自验收。自动化测试不得冒充真实支付。
6. 关闭新入口时保留已接单查询、核账、补账能力；回滚不能丢弃新订单字段或停掉所有到账确认。

## 本地验证

后端运行 `go test -tags=unit` 的支付/履约/渠道回归和 `-race`；前端运行支付组件、支付页、套餐购买及 locale 检查，并做类型检查、ESLint 与 Vite build。

`TestGelvPostgresLedgerAndConcurrentRecovery` 使用 `integration` tag 和显式 `GELV_TEST_DSN`；只接受 localhost/127.0.0.1 的 `gelv_test` 数据库。必须先创建可丢弃的隔离 PostgreSQL，绝不能使用生产库。该测试使用真实仓储，覆盖余额更新失败回滚、并发确认、两类渠道额度、已充值后恢复及实际迁移执行。

## 资料与判断边界

腾讯云开发官方参考明确区分 `wx.requestVirtualPayment` 与普通 `wx.requestPayment`，列出了 Android 1% / iOS 12%，要求服务端通知/查单确认，且 iOS 需开通 IAP。该参考支持按真实支付渠道区分的设计；本轮微信开发者文档直链无法读取，所以没有把未核实的原始渠道字段写死到本站代码。合作方负责用其实际微信接入核定原字段，本站只消费确认后的合作协议。

[腾讯云开发官方虚拟支付参考](https://github.com/TencentCloudBase/skills/blob/main/skills/cloudbase-wechat-integration/references/virtual-payment.md)
