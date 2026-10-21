# OKX DEX API 开发接入与测试参考

- **文档版本：** v0.6
- **调研日期：** 2026-10-20
- **适用场景：** 多链 DEX 统一交易终端；首期优先 EVM，计划支持跨链兑换
- **v0.6 变更：** 实测跨链 v6 接口可用：`quote`/`swap` 使用 `slippage`（百分数），新增 `/approve-tx`；确认 `otherNativeFee` 会计入源链 `tx.value`，且跨链没有目标链状态接口。记录 quote 返回 `82000` 但 swap 仍可构造的差异。
- **v0.5 变更：** 使用真实 API Key 完成报价、`swap` 数据与本地签名联调；确认 `/swap` 必须使用 `slippagePercent`，且该参数按百分数解释（`0.5` = 0.5%，比小数表示大 100 倍），并记录 OKX 响应中 `chainIndex`、`dexProtocol` 的混合 JSON 类型问题。
- **v0.4 变更：** 通过端点探测实测各接口存活状态：跨链 API 已迁移至 v6（v5 整组废弃）、`build-tx` 更名为 `swap`、`bridge-tokens-pairs` 移除；补充状态查询与监控能力边界（含 OKX 不提供的部分）。
- **v0.3 变更：** 依据 OKX 官方 DEX SDK 源码核实供应商能力边界与真实端点，修正聚合器 API 版本路径，补充非托管账户与地址模型。

## 1. 执行摘要

### 测试策略决定

本项目不把 OKX Sandbox/Testnet 作为开发前置条件。当前公开文档中未找到明确公开、独立的 DEX Sandbox/Testnet API Base URL 或完整测试网交易环境说明；因此，**采用“先做无资金 API 联调与交易数据校验，再用受控的小额主网交易验证端到端流程”的策略**。

公开接入指南使用 `https://web3.okx.com/api/v5/` 等地址，并要求创建开发者项目、申请 API Key、按要求签名请求。交易模拟 API 也不是所有开发者默认可用：官方文档注明该接口目前仅向白名单客户开放。

实盘测试不是零风险测试。即使金额较小，也可能发生 Gas 损耗、交易失败、滑点、代币合约风险、授权风险、错误链上广播或跨链延迟。必须先完成交易数据检查、资金上限、钱包隔离和人工确认，再广播主网交易。

### 供应商能力边界（非托管，重要）

OKX DEX API 是**非托管**服务：它不持有资金、不保管私钥、不代付 Gas，也不替用户签名。

- **它提供：** 链与代币元数据、最优路由与报价、未签名的交易 calldata、桥聚合（跨链路由）、链上模拟、Gas 估算，以及可选的交易广播服务（智能多播、MEV 保护）。
- **它不提供：** 资金托管、代付 Gas、代签名、跨链资金搬运（源链仍需自有资金）。

因此“产品维护一个资金账户并在 OKX DEX 上交易”，意味着**产品自身就是那个钱包**：必须自己持有密钥、自己签名、自己保证源链余额与 Gas、自己广播并跟踪结果。广播接口入参为 `signedTx`（已签名原始交易），这一事实本身就说明 OKX 不持私钥。

## 2. 官方文档入口

| 主题 | 官方链接 | 用途 |
|---|---|---|
| DEX API 总览 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-what-is-dex-api | 产品能力和 API 分类 |
| API 访问与鉴权 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-api-access-and-usage | API Key、签名、请求头 |
| 开发者平台 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-developer-portal | 创建项目和 API Key |
| API 费用 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-api-fees | 核对当前等级、费用和额度 |
| 支持的单链兑换网络 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-get-aggregator-supported-chains | 支持链与相关授权地址 |
| 代币列表 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-get-tokens | 查询代币元数据 |
| 兑换报价 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-get-quote | 获取单链报价 |
| EVM 兑换快速开始 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-use-swap-quick-start | EVM 授权、报价、构造交易和广播 |
| DEX SDK 介绍 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-sdk-introduction | TypeScript SDK |
| 官方 DEX SDK | https://github.com/okx/okx-dex-sdk | SDK 源码和示例 |
| 交易模拟 API | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-onchain-gateway-api-simulate-transaction | 模拟交易；当前仅对白名单客户开放 |
| 更新日志 | https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-change-log | 关注版本、费用和接口变更 |

> 官方文档可能同时存在不同 API 版本或历史路径。实施时应以对应接口的当前文档为准，不要混用 `/api/v5/`、`/api/v6/` 的路径、参数和签名规则。

## 3. 环境策略：无资金联调 + 小额主网验证

本项目不等待 Sandbox/Testnet 环境。开发按以下顺序推进：

1. **无资金 API 联调**：验证鉴权、支持链、代币列表、报价、错误码和限流处理。
2. **交易数据校验**：请求授权与 Swap 交易数据，但不签名、不广播；检查链 ID、交易目标、value、calldata、金额和滑点。
3. **本地模拟与 mock**：使用固定响应、单元测试和本地 fork（如适用）验证状态机、失败分支和 UI。
4. **小额主网实测**：先选一条链、一个高流动性交易对和单链 Swap；使用独立测试钱包、严格限额和人工逐笔确认。
5. **跨链实测**：单链流程稳定后，再用极小金额验证跨链报价、源链提交、目标链完成状态和异常恢复。
6. **扩大覆盖面**：只有前一阶段验收通过，才增加链、代币、交易金额或自动化程度。

### 实盘测试前必须确认

- 使用的 API Key、API 版本和请求签名正确；
- 用户钱包链 ID 与报价链一致；
- 交易目标地址、金额、代币地址、value 和 calldata 均经过校验；
- 明确最大滑点、最低到账量、预估 Gas 和总成本；
- 钱包有足够的原生币支付 Gas；
- 授权目标（spender）与 OKX 对应链的官方文档一致；
- 交易由测试负责人逐笔审核并明确批准；
- 交易广播后能够通过交易哈希和供应商/链上状态查询跟踪结果；
- 已定义失败、超时和重复提交的处理方法。

### 关于模拟接口

官方交易模拟 API 当前注明仅向白名单客户开放。若账户没有该权限，不应把模拟接口作为 PoC 的必要条件；可用本地校验、mock 和小额实测替代，但要清楚这些方式覆盖的风险不同。

> v0.3 补充：官方 DEX SDK 已提供模拟调用（`/api/v5/dex/pre-transaction/simulate`），该接口可能已对普通开发者开放，联调时以实际返回为准。

API 等级、调用额度、费用和权限仍需在开发者平台核实。

## 4. API Key 与鉴权

### 创建项目

1. 打开 OKX 官方开发者平台并连接钱包登录。
2. 按页面提示完成验证。
3. 在项目中创建 API Key。
4. 安全保存 API Key、Secret Key 和 Passphrase。
5. 按具体接口文档确认是否还需 Project ID 或项目级请求头。

### 签名规则

官方文档描述的签名原文通常由以下内容组成：

```text
timestamp + HTTP_METHOD + requestPath + body
```

随后使用 Secret Key 通过 HMAC-SHA256 签名，并对结果进行 Base64 编码。

注意：

- `timestamp` 使用 UTC ISO 时间，并与 `OK-ACCESS-TIMESTAMP` 一致。
- HTTP 方法使用大写。
- GET 请求的签名应包含实际路径和查询字符串。
- POST 请求必须将实际发送的原始请求体纳入签名。
- 官方文档指出，客户端与服务端时间偏差不得超过 30 秒。
- 不要将 Secret Key 暴露给浏览器、移动端、日志或代码仓库。

常见请求头：

```text
OK-ACCESS-KEY
OK-ACCESS-SIGN
OK-ACCESS-TIMESTAMP
OK-ACCESS-PASSPHRASE
```

部分接口还可能要求项目相关请求头；以该接口当前文档为准。

### 密钥管理

- 开发和生产使用不同 API Key。
- Secret Key 仅存放在可信后端或 Secret Manager 中。
- 前端调用自有后端，不直接持有 OKX Secret Key。
- 不要将 OKX API Key 与用户钱包私钥混为一谈。
- 日志屏蔽 API Key、签名、Secret Key、Passphrase 等敏感数据。

## 5. 首期应验证的 API 能力

### 基础查询

先验证：

- 查询支持的链；
- 查询代币列表；
- 获取指定代币对报价；
- 验证金额精度、代币地址和链 ID；
- 处理无报价、参数错误、鉴权失败、限流和服务异常。

首期需要联调的三个 API 分组如下。路径已通过**端点探测**（未授权请求的错误码）核实，注意**版本号并不统一**：

```text
# 1) 兑换 API（聚合器，v6）
GET  /api/v6/dex/aggregator/supported/chain       获取支持的链
GET  /api/v6/dex/aggregator/all-tokens            获取币种列表
GET  /api/v6/dex/aggregator/get-liquidity         获取流动性列表
GET  /api/v6/dex/aggregator/quote                 获取最优报价
GET  /api/v6/dex/aggregator/swap                  获取兑换所需交易数据（未签名）
GET  /api/v6/dex/aggregator/swap-instruction      获取 Solana 兑换指令
GET  /api/v6/dex/aggregator/history               按 txHash 查单链兑换最终状态

# 2) 交易上链 API（v5）
GET  /api/v5/dex/pre-transaction/gas-limit              估算 Gas 上限
GET  /api/v5/dex/pre-transaction/gas-price              查询 Gas 价格
GET  /api/v5/dex/pre-transaction/simulate               交易模拟
POST /api/v5/dex/pre-transaction/broadcast-transaction  广播交易（入参 signedTx）
GET  /api/v5/dex/post-transaction/orders                按 address/chainIndex/orderId 回查状态

# 3) 跨链 API（v6）
GET  /api/v6/dex/cross-chain/supported/bridges   支持的桥
GET  /api/v6/dex/cross-chain/supported/tokens    支持跨链的代币
GET  /api/v6/dex/cross-chain/quote               跨链报价
GET  /api/v6/dex/cross-chain/swap                构造源链交易（含 userWalletAddress）
GET  /api/v6/dex/cross-chain/approve-tx          ERC-20 源币授权交易
```

> **版本与废弃情况（已实测）：**
> - 聚合器与跨链现已统一到 `/api/v6/`；跨链在 v5 下**整组返回 `API_DEPRECATED`（permanently removed）**。
> - 跨链构造交易的接口由 `build-tx` 改名为 `swap`；`supported/bridge-tokens-pairs` 已 404 移除。
> - 交易上链（`pre-transaction` / `post-transaction`）在 v5 与 v6 下均存活。
> - **官方 DEX SDK 的 bridge 模块仍指向已废弃的 v5 跨链路径，不可直接照搬。**
> - 适配器应集中管理版本路径，不要在业务代码里混用。
> - **滑点参数名与单位（2026-10-19 实测）：** 聚合器 `/swap` 要求 `slippagePercent`，只传 `slippage` 会返回 `50014 Parameter slippagePercent cannot be empty`；该参数按百分数解释，`0.5` 表示 0.5%（不是 50%）。如果内部用小数表示滑点，必须在请求前乘以 100。
> - **JSON 类型不稳定：** `/supported/chain` 的 `chainIndex` 可能返回数字，`/quote` 中 `dexProtocol` 可能是单个对象或数组；解析层需同时兼容字符串/数字与对象/数组。
> - **跨链滑点参数名不同（2026-10-20 实测）：** `/cross-chain/quote` 与 `/cross-chain/swap` 使用 `slippage`，不是单链 `/swap` 的 `slippagePercent`；`slippage=0.5` 表示 0.5%，`slippage=5` 会返回 `82114 The slippage too large`。
> - **跨链授权：** `GET /cross-chain/approve-tx` 需要 `chainIndex`、`tokenContractAddress`、`approveAmount`、`userWalletAddress`、`bridgeId`；返回 `approveAddress`（spender）和 `tx`（`to` 为代币合约、`data` 为 approve calldata）。响应里的 `tx.value` 是授权数量，不是 ETH value，链上交易 value 必须为 0。
> - **目标链 Gas：** `/supported/bridges` 返回 `requireOtherNativeFee`；为 true 的桥（如 Stargate V2）会在 quote 中返回 `otherNativeFee`，并把它加到源链 `tx.value`。调用方必须保证源链余额覆盖 `value + gas`。
> - **quote/swap 不一致：** 部分 ERC-20 币对可能 quote 返回 `82000 Insufficient liquidity`，但 `/cross-chain/swap` 仍能构造交易；实现应允许回退到 swap 数据。

### 状态查询与监控能力（核实）

OKX 提供的是**订单级状态回查**，不是链上执行监控服务：

```text
GET /api/v6/dex/aggregator/history       根据 txHash 查单链兑换最终状态（官方文档页仍写作 v5，两版均存活）
    入参: chainIndex, chainId(即将废弃), txHash, isFromMyProject
    返回: status(pending|success|fail), height, txTime, txType(Approve|Wrap|Unwrap|Swap),
          fromAddress, toAddress, dexrouter
    限制: 仅能查"经 OKX DEX API 发出"的交易

GET /api/v5/dex/post-transaction/orders  查询广播订单列表（按时间倒序）
    入参: address, chainIndex, txStatus(1排队中|2成功|3失败), orderId, cursor, limit(默认20,最大100)
    返回: orders[] { chainIndex, address, orderId, txStatus, failReason, txHash }
    限制: 仅覆盖"通过 OKX 广播接口发出"的订单
```

**明确不提供的能力（必须自建）：**

- 跨链目标链监控：无任何跨链状态查询端点，跨链文档也未公开；目标链是否到账必须自己跟踪；
- 链上确认数、最终性与 reorg 处理；
- 卡单/pending 交易的 Gas 替换与 nonce 管理；
- 推送/WebSocket 订阅（只有轮询）；
- 余额与持仓监控（需自行通过 RPC 查询）。

### EVM 单链 Swap

按官方 EVM 快速开始指南，典型流程是：

1. 获取链和代币信息；
2. 查询 ERC-20 allowance；
3. 如有需要，获取授权交易数据；
4. 获取兑换报价；
5. 获取 Swap 交易数据；
6. 校验链 ID、交易目标、金额、滑点和 calldata；
7. 估算 Gas；
8. 由持有资金的签名方签名（本项目为非托管模式，签名方即产品自身的密钥）；
9. 广播交易并查询链上回执；
10. 将最终状态反馈给产品层。

**报价成功不等于交易成功。** 报价只是某一时刻的估算；实际交易可能因价格变化、滑点、余额、Gas、授权或路由状态而失败。

其中第 2~4 步（allowance、授权交易、报价）与第 6~7 步（字段校验、Gas 估算）可由 OKX 与自有后端完成，**第 8~9 步的签名与广播必须由产品侧负责**：可以广播到自建 RPC，也可以调用 OKX 的 `broadcast-transaction`（入参为已签名交易，附带多播与 MEV 保护）。

### 跨链兑换

跨链必须作为单独 PoC 验收，重点确认：

- 跨链报价参数与返回字段；
- 支持的源链、目标链和代币组合；
- 源链交易构造与提交方式；
- 目标链执行状态查询；
- 最低到账量、费用、预计时间和报价有效期；
- 目标链延迟、执行失败、退款或恢复机制；
- 订单唯一标识、状态回查和幂等处理。

至少覆盖正常完成、源链失败、源链成功但目标链延迟、目标链执行失败、报价过期、重复轮询和服务重启后的状态恢复。

**只有达到定义好的目标链完成条件，跨链订单才能显示最终成功。源链交易成功不代表跨链兑换已完成。**

跨链调用顺序（端点见第 5 节，均在 `/api/v6/dex/cross-chain/` 下）：

1. `GET /supported/bridges`、`GET /supported/tokens` 确认源链、目标链与代币组合可用；
2. `GET /quote` 获取跨链报价（`fromChainIndex`/`toChainIndex`、`fromChainId`/`toChainId`、`amount`、`slippage`，可传 `allowBridge`/`denyBridge` 限定桥）；
3. 若 `needApprove=true`，调用 `GET /approve-tx`（带 `chainIndex`、`tokenContractAddress`、`approveAmount`、`userWalletAddress`、`bridgeId`），先签/发授权交易；
4. `GET /swap` 构造源链交易（须传 `userWalletAddress`，可传 `receiveAddress`、`onlyBridge`）；
5. 产品侧签名并广播源链交易，目标链由 OKX/桥方执行；
6. 跟踪目标链到账（无官方状态接口，需轮询目标链余额或对接桥方），不能仅凭源链成功判定完成。

注意：`/swap` 只返回**源链**的未签名交易，目标链那一笔不需要（也无法）由产品签名。第 6 步没有官方接口可用，必须自行对接桥方或目标链。

## 6. 分层测试计划

### 阶段 A：API 联调

- [ ] 创建项目和 API Key
- [ ] 正确签名的 GET 请求成功
- [ ] 错误签名或过期时间戳会被正确识别
- [ ] 支持链、代币列表和报价请求成功
- [ ] 无效链、无效代币、无效金额和限流均有明确错误处理
- [ ] 日志没有泄露凭证

### 阶段 B：交易数据构造，不广播

- [ ] 获得报价和授权交易数据（如需要）
- [ ] 获得 Swap 交易数据
- [ ] 校验链 ID、`to`、`value`、`data`、代币地址、金额和滑点
- [ ] 验证原生币与 ERC-20 的差异
- [ ] 验证报价过期和参数变化
- [ ] 未经批准不签名、不广播

### 阶段 C：小额主网单链 Swap（本项目首个链上验证阶段）

通过阶段 A、B 后，按以下顺序执行。测试金额由团队根据资产价值、Gas 成本和风险承受能力设定；不要把固定金额当成普遍安全阈值。

- [ ] 使用独立测试钱包，不使用团队金库或个人主要资产钱包
- [ ] 选择一条链和流动性较高、合约地址已核验的代币对
- [ ] 首笔交易金额设为团队批准的最低可行金额
- [ ] 在广播前人工检查链 ID、`to`、`value`、`data`、输入金额、最低输出、滑点和 Gas
- [ ] 确认钱包有足够的原生币支付 Gas，并将 Gas 计入测试成本
- [ ] 仅在必要时授权；检查 spender，并优先避免无必要的无限授权
- [ ] 记录报价时间、预估输出、最低输出、预估 Gas、实际 Gas、交易哈希和实际到账
- [ ] 验证成功、失败、超时、回执查询和应用状态更新
- [ ] 测试结束后检查并处理不再需要的授权
- [ ] 每笔交易后复盘，未通过验收前不自动连续执行或扩大金额

### 阶段 D：小额主网跨链兑换

单链 Swap 流程稳定后才进行跨链测试：

- [ ] 选择已确认受支持的源链、目标链和代币组合
- [ ] 首笔跨链测试使用团队批准的最低可行金额
- [ ] 广播前确认预估到账、最低到账、费用、预计时间和报价有效期
- [ ] 保存源链交易哈希及供应商订单/跟踪标识（若有）
- [ ] 持续跟踪目标链状态，不能仅凭源链成功就判定完成
- [ ] 记录目标链实际到账、总耗时、各项费用和异常信息
- [ ] 验证目标链延迟、执行失败、退款/恢复及人工介入流程
- [ ] 在状态机和恢复逻辑通过验收前，不进行自动化批量实测

### 实盘资金与操作护栏

- **独立资金池：** 仅向测试钱包转入本轮所需的少量资金，不在钱包中保留不必要的资产。
- **单笔上限：** 设置明确的单笔金额上限；每轮测试开始前确认，不因失败而自动加码。
- **累计上限：** 同时设置单日累计损失上限和 Gas 预算；达到任一上限立即停止。
- **人工批准：** 初期每笔交易都由人检查并批准；报价和构造交易可以自动化，广播先保持人工门控。
- **交易白名单：** 限制链、代币、交易目标和 spender；未在白名单内的交易直接拒绝。
- **停止开关：** 鉴权异常、交易参数不一致、异常滑点、重复订单、状态无法确认或连续失败时暂停测试。
- **不盲目重试：** 广播超时后先查询交易哈希、nonce 和订单状态；确认原交易状态前不得直接重复提交。
- **结果审计：** 每笔记录报价、签名前交易数据摘要、人工批准人、交易哈希、实际成本和结果；日志不得保存私钥或 Secret Key。

## 7. 建议的系统边界

建议将 OKX API 封装在自有后端适配器中，避免前端直接依赖供应商字段：

```text
上游交易指令来源（策略/上游系统/API）
    |
    v
交易应用后端
    |
    +-- Token / Chain Registry        (chainIndex 与 chainId 双向映射)
    +-- OKX DEX Adapter
    |      +-- Auth & Signing         (OKX API 鉴权，与钱包签名是两回事)
    |      +-- Quote
    |      +-- Approval Transaction
    |      +-- Swap Transaction
    |      +-- Status / Error Mapping
    +-- Wallet / Signer               (产品自持密钥，本地签名)
    +-- Nonce & Gas Manager           (逐链 nonce、Gas 预算与补给)
    +-- Order State Machine
    +-- Risk & Policy Checks
    +-- Ledger & Reconciliation
    +-- Observability / Audit Logs
    |
    v
已签名交易 -> 自建 RPC 或 OKX 广播服务 -> 链上
```

### 资金账户与地址模型

- **EVM 各链共用一个地址：** 同一私钥派生出的地址可用于所有 EVM 链，EVM 侧的资金账户是“一个密钥、多条链”。
- **余额与 Gas 逐链独立：** 同一地址在不同链上的资产互不相通，每条链都要有原生币支付 Gas。
- **非 EVM 链地址不同：** Solana、Sui、TON、TRON 需要各自独立的密钥对与地址。
- **实际模型：** 产品维护的是一个“多链密钥集合”（1 个 EVM 密钥 + N 个非 EVM 密钥），而不是单一账户；账本、Gas 与风控都必须按链维度记账。
- **链标识：** OKX 使用 `chainIndex` 作为内部链标识，跨链接口同时需要 `chainIndex` 与 `chainId`；Registry 必须维护双向映射，避免混用。

建议统一内部数据结构：

- `QuoteRequest`：源链、目标链、输入/输出代币、金额模式、滑点；
- `QuoteResult`：预估输入、预估输出、最低输出、费用、路由和有效期（若提供）；
- `UnsignedTransaction`：链 ID、交易目标、value、calldata、Gas 字段；
- `ExecutionStatus`：待签名、待广播、源链待确认、源链成功、目标链处理中、完成、失败、恢复中；
- `ProviderError`：供应商业务码、HTTP 状态、可重试性和用户可读信息。

具体字段必须依据实际 API 响应映射，不要假设不同链或接口的响应结构完全一致。

## 8. 主要风险与注意事项

1. **API 版本漂移：** 集中管理 endpoint 和签名逻辑，避免业务代码中散落不同版本路径。
2. **签名错误：** 签名所用 query string / body 必须与实际请求完全一致。
3. **密钥泄漏：** Secret Key 只能保存在可信后端。
4. **报价过期：** 设置应用侧有效期，过期后重新报价。
5. **金额精度：** 使用最小单位整数或字符串，不使用浮点数处理链上金额。
6. **滑点与最低到账：** 向用户展示预估到账、最低到账和费用。
7. **授权风险：** 仅在需要时授权；检查 spender 是否匹配当前链和官方文档。
8. **路由支持：** 支持某条链不代表任意代币对和金额都有可执行路由。
9. **模拟接口权限：** 交易模拟 API 当前注明仅向白名单客户开放。
10. **跨链最终性：** 源链确认不能代表目标链完成。
11. **费用与配额：** PoC 前核实 API 等级、调用量、服务费和商业条款。
12. **重试与恢复：** 超时后先查询状态，不要盲目重复提交可能产生重复交易的操作。
13. **托管边界误解：** OKX 不持有资金、不代签名、不代付 Gas。不要把“接入 OKX”理解为资金托管；密钥、签名、Gas 与广播始终是产品自身职责。

## 9. PoC 验收清单

### API 与环境

- [ ] 已创建独立开发项目/API Key，并确认权限、等级、额度和费用
- [ ] 已成功完成鉴权、支持链、代币和报价请求
- [ ] 已记录 API 版本和所用文档版本
- [ ] 已完成 mock、错误响应和限流测试

### 单链交易

- [ ] 至少一条目标 EVM 链的报价与交易构造可用
- [ ] 已通过人工审核的小额主网交易完成端到端验证
- [ ] 验证授权、签名、广播、回执和实际到账
- [ ] 覆盖失败、超时、报价过期和重试
- [ ] 所有金额计算使用整数/最小单位
- [ ] 单笔上限、累计上限、Gas 预算和停止开关已生效
- [ ] 交易目标、spender、链 ID 和关键交易字段有校验记录

### 跨链交易

- [ ] 明确支持的源链、目标链和代币组合
- [ ] 验证跨链报价和完整执行流程
- [ ] 能识别目标链处理中、成功、失败和恢复状态
- [ ] 已定义超时、重试、退款/恢复和用户通知策略
- [ ] 不把源链成功误报为跨链最终成功

### 安全与运维

- [ ] 前端不持有 OKX Secret Key
- [ ] 已建立限额、监控、日志脱敏和暂停机制
- [ ] 已校验交易目标、链 ID、金额、滑点和授权目标
- [ ] 已明确供应商故障时的降级策略

## 10. 结论

**本项目采用“无资金联调 + 小额主网验证”，不等待 OKX 提供 Sandbox/Testnet。**

执行顺序：

1. 完成鉴权、支持链、代币和报价接口联调；
2. 获取交易数据但不广播，完成字段与安全校验；
3. 用 mock / 单元测试验证异常路径和订单状态机；
4. 在独立测试钱包中完成受控的小额主网单链 Swap；
5. 单链流程稳定后，再验证小额跨链兑换及异常恢复。

主网测试仍然是真实资金操作。所有交易都应有明确资金上限、Gas 预算、交易白名单、人工批准和停止开关。若交易数据异常、状态无法确认或发生非预期损失，应立即暂停，而不是自动重试或增加金额。
