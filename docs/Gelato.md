# Gelato Relay 竞品调研

我会按你给的四个维度来分析，并把 Gelato Relay 本身 与 Gelato 的其他产品（例如智能账户、Paymaster、Bundler、自动化执行）区分开。

先说最关键的结论：Gelato Relay 的核心业务是交易中继基础设施，而不是一个主动在各条链上寻找套利或交易机会的机器人。 它主要替 DApp 提交交易、处理交易签名和 nonce、估算 Gas、广播交易，并通过不同支付机制实现 Gas 代付或由用户承担交易费用。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



## 1. 业务逻辑机制：Gelato 到底替用户做了什么？

### 1.1 典型业务流程

① 用户发起操作

连接钱包、签署交易请求或授权消息

② DApp 调用 Gelato Relay API

提交目标链、目标合约、调用数据、费用支付方式

③ Gelato 处理交易请求

验证、模拟、估算费用、管理 nonce 和签名、检查资金

④ Relayer 向目标链广播

使用 Relayer 的链上账户提交交易并支付原生 Gas

⑤ 链上合约执行并返回结果

执行成功或失败，返回任务 ID 和交易状态

官方文档中的 API 请求包含 `chainId`、`target`、`data` 等字段；Relay SDK 返回 `taskId`，用于追踪任务。开发者还可以设置 Gas Limit 和重试次数。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



这里需要区分两个角色：

- 用户或 DApp 决定做什么。 例如兑换、领取奖励、提交订单。
- Relayer 负责把请求提交上链。 它不天然负责决定交易策略、寻找套利机会或选择最优 DEX 路径。

如果 DApp 想做跨链兑换，通常还需要路由器、聚合器、桥或其他执行逻辑。Relayer 可以参与交易流程，但不能仅凭 Relayer 这个身份就推断它具备跨链寻价能力。

### 1.2 它会主动寻找不同链上的交易机会吗？

通常不会。Gelato 有不同的产品，不能把整个 Gelato 平台的能力都归到 Relay 上。

| 能力                    | 是否属于 Relay 的核心职责            |
| --------------------- | --------------------------- |
| 接收 DApp 的交易请求         | 是                           |
| 管理交易签名、nonce 和 Gas 参数 | 是                           |
| 广播交易、处理重试             | 是                           |
| 替 DApp 支付 Gas         | 支持，取决于支付方式                  |
| 主动寻找跨链套利机会            | 不是核心职责                      |
| 计算 DEX 最优兑换路径         | 不是核心职责                      |
| 按链上或链下条件自动触发任务        | 更接近 Gelato Functions 等自动化产品 |

Gelato 的跨链示例会组合 Relay 与 Functions：Functions 负责监听或获取条件数据，Relay 负责将最终交易提交到目标链。

[image](https://www.google.com/s2/favicons?domain=https://blog.gelato.cloud\&sz=32)

blog.gelato.cloud

+1



这对你们团队的产品定位很重要：如果你们做的是 Relayer，首要竞争力通常是可靠的交易执行、低延迟、费用控制和开发者体验，而不是交易策略本身。

## 2. 用户账户控制：Gelato 会不会掌握用户钱包和资金？

这部分是 Relayer 产品最需要认真研究的安全边界。

### 2.1 先区分两种完全不同的账户

模式 A：用户使用自己的 EOA 钱包

例如 MetaMask 钱包

- 用户持有自己的私钥，自己签署授权或交易请求。
- Gelato 使用自己控制的 Relayer 账户支付原生 Gas，并把请求提交到目标合约。
- 用户的 EOA 私钥不会因为使用 Relayer 就自动交给 Gelato。
- 用户的代币通常仍由用户钱包或目标协议合约持有，具体取决于交易内容。

模式 B：用户使用智能合约账户

例如 ERC-4337 Smart Account

- 用户通过智能账户规定的验证机制授权操作。
- 智能账户合约控制其链上资产和权限。
- Bundler 负责提交 UserOperation，Paymaster 可以负责 Gas 赞助。
- Gelato 可以提供这些基础设施，但它们不等于普通 Relayer API 本身。

Gelato 的 ERC-2771 实现允许目标合约从转发请求中恢复原始签名用户的地址。官方文档还说明，`trustedForwarder` 可以被设置为不可变地址，使合约能够识别可信的转发者。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



### 2.2 用户如何防止项目方挪用资金？

这里要区分两件事：Relayer 有没有用户私钥，以及Relayer 交易最终被授权执行什么操作。

如果使用 EOA + 可信的签名请求机制，Relayer 通常不需要用户私钥，也不应因此获得任意转移用户资产的权限。但如果用户已经给某个合约无限额 Token Approval，或者签署了权限过大的授权，风险就会发生在授权机制或目标合约层面。

建议从以下几个方面审查 Gelato：

| 安全边界       | 需要验证的问题                          |
| ---------- | -------------------------------- |
| 用户签名       | 签名是否绑定链 ID、目标合约、调用数据、nonce 和有效期？ |
| 转发者验证      | 目标合约是否正确校验可信的 Forwarder？         |
| 资产授权       | 是否要求用户授权特定合约？授权额度能否限制或撤销？        |
| Relayer 密钥 | 服务商的交易签名密钥如何托管、隔离和轮换？            |
| 智能账户权限     | 是否有 Session Key、限额、白名单等机制？       |
| 资金隔离       | 用户资产、DApp 赞助资金和服务商运营资金是否分离？      |

还有一个容易忽视的细节：`ERC-2771` 只解决可信转发与原始调用者识别问题，并不会自动保证整个目标合约安全。Gelato 的 `callWithSyncFee` 文档特别警告，如果开发者没有正确限制费用收取逻辑，合约内资金可能被恶意调用者转走。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



竞品调研结论： 不应只问“Gelato 会不会拿到用户私钥”，还要问“它的执行权限由谁限制、目标合约如何校验、用户授权的资产范围有多大”。这是三个不同的问题。

## 3. 费用与计费体系

Gelato 的费用不能简单理解为“每笔交易收固定手续费”。官方文档区分了链上 Gas 成本和服务费，并提供不同的支付路径。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



### 3.1 三种主要费用承担模式

| 模式                        | 谁承担链上 Gas？          | Gelato 如何收费？                        |
| ------------------------- | ------------------- | ----------------------------------- |
| Sponsored Call + Gas Tank | DApp / 集成方          | 从 Gas Tank 资金中支付交易成本和适用的服务费         |
| SyncFee                   | 由目标合约按调用逻辑支付        | 在交易执行时向 Gelato 的 fee collector 支付费用 |
| Smart Account + Paymaster | 取决于 Paymaster 的赞助策略 | 按对应 Paymaster / Bundler 产品的计费规则收取   |

第一种模式下，Gelato 的 Gas Tank（历史上也称 1Balance）支持用统一余额覆盖多个网络上的服务支出。第二种模式则可以让用户或协议通过支持的代币支付费用，而不必单独持有原生 Gas Token。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



### 3.2 费用由哪些部分组成？

A. 实际链上 Gas

由交易消耗的 Gas 与执行时的 Gas 价格决定。不同链、合约复杂度和网络拥堵程度都会影响费用。

B. 服务费或 Gas 加价

Gelato 的文档说明，服务费可能采用订阅费或按交易 Gas 成本计算的百分比加价。具体方案取决于所使用的产品和账户套餐。

C. 跨链充值成本

使用跨链 USDC 充值 Gas Tank 时，可能产生跨链转移与链上 Gas 成本。官方文档说明，相关充值费用用于覆盖 CCTP 和 Gas 成本，而非额外收取一笔 Gas Tank 服务费。

### 3.3 具体价格是多少？

这里需要谨慎：我查到的公开文档可以确认收费结构，但不足以确认一个适用于所有链、所有 Relayer API 用户的统一固定价格。 Gelato 当前文档提供动态费用报价接口，价格可能随链、Gas 价格、支付 Token 和账户方案变化。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+2



例如，当前 Relayer API 文档提供：

- `relayer_getFeeData`：获取支付代币汇率和当前 Gas 价格。
- `relayer_getFeeQuote`：获取特定 Gas 用量的费用报价。
- `relayer_sendTransaction`：提交交易。
- `relayer_getStatus`：查询交易状态。

  [image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

  Gelato

  +1



可以从 [Gelato 官方定价入口](https://docs.gelato.cloud/pricing/pricing-plans) 和 [Relay 文档](https://docs.gelato.cloud/) 继续核实具体套餐。公开文档链接可能随产品迁移而调整；如需精确比较价格，建议获取实际账户下的报价，或向 Gelato 销售团队询价。

对你们的竞品表，建议记录以下字段，而不是只记一个手续费率：

- Gas 成本是否原价转嫁，还是存在加价；
- 是否有最低单笔费用；
- 是否有月费、最低消费或调用量套餐；
- 失败交易是否收费；
- 赞助额度是否有上限；
- 支持哪些支付 Token；
- 是否支持跨链统一余额；
- 是否有企业级 SLA 或专属报价。

## 4. Gelato 有哪些值得关注的特殊机制？

我认为下面这些机制，比单纯的“支持多少条链”更值得作为竞品差异化研究的重点。

### 4.1 多链统一 Gas 余额：Gas Tank / 1Balance

传统 Relayer 往往需要在每条链分别充值运营资金，或者要求每个 DApp 自己管理不同链的 Gas 余额。Gelato 的 Gas Tank 可以把多个网络的费用整合到统一余额中。

例如，DApp 用 Polygon 上的 USDC 充值，之后可以用这笔余额支付符合条件的 Ethereum 主网交易成本。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



这项能力的产品价值很直接：

- 降低开发团队管理多链运营资金的复杂度。
- 避免某条链的 Gas 余额耗尽导致服务中断。
- 把 Gas 支付抽象成一个统一的资金管理产品。
- 为 DApp 提供更简单的财务对账方式。

需要注意：统一余额不代表不同链上的资产在技术上天然互通，跨链充值仍然涉及相应的转移机制、费用和确认时间。

### 4.2 SyncFee：在交易执行过程中收取费用

传统赞助交易通常由 DApp 预先承担 Gas；SyncFee 则允许目标合约在执行过程中，把费用支付给 Gelato 的费用接收地址。

Gelato 还提供 `_transferRelayFeeCapped(maxFee)` 这样的机制，让开发者设定最高费用限制。如果实际费用超过上限，交易就不会按该路径继续执行。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato



这对 DeFi 特别有意义：

假设用户通过 DEX 兑换代币，DApp 希望直接从交易涉及的代币中收取服务费，而不要求用户额外准备 ETH。只要交易合约与支付机制正确设计，就可以把交易执行和费用结算放在同一个链上流程里。

但它并不意味着所有 DEX 都可以不经修改直接接入，具体取决于合约架构、授权和费用支付逻辑。

### 4.3 ERC-2771：让目标合约识别真正的用户

使用普通交易转发时，目标合约看到的 `msg.sender` 可能是 Gelato 的转发合约，而不是原始用户。

通过 ERC-2771，目标合约可以使用 `_msgSender()` 恢复原始签名用户地址，进而执行权限检查。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato



这让开发者能够保留基于用户地址的授权逻辑，同时把交易广播和 Gas 支付交给 Relayer。

不过，这也意味着目标合约必须正确实现可信转发者校验。若开发者错误地信任了不可信的 Forwarder，可能造成权限绕过。

### 4.4 交易重试、状态跟踪与费用预估

Gelato 提供交易状态查询、费用预估和重试配置等能力。当前 Relayer API 还提供同步提交和等待交易确认的接口。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



这类能力对基础设施产品很关键：

- 重试机制： 应对状态变化导致的暂时性失败。
- 状态跟踪： 让 DApp 查询交易是否已提交、确认、拒绝或回滚。
- 费用预估： 提前计算支付代币需要覆盖的费用。
- 同步接口： 在特定调用流程中等待交易确认后再返回。

但要注意，重试不等于保证成功。链上状态、合约条件、Gas 价格以及请求是否可重复执行，都会影响最终结果。

### 4.5 新一代 Turbo Relayer 与智能账户能力

Gelato 当前文档已经单独列出 Turbo Relayer、Gasless ERC-4337、EIP-7702 智能账户支持、交易批处理及费用聚合等功能。

[image](https://www.google.com/s2/favicons?domain=https://docs.gelato.cloud\&sz=32)

Gelato

+1



这说明它的产品路线不只是“替别人发交易”，而是在向更完整的交易执行基础设施发展：

- 统一交易提交 API；
- 支持不同类型的签名和授权；
- 让交易费用使用不同的 Token 支付；
- 为智能账户提供 Gas 赞助和批量执行能力；
- 将交易基础设施与账户抽象能力整合。

这些功能应该分别核实其当前支持的链、部署方式和可用套餐，不要仅凭文档目录就假定所有功能都已对所有客户开放。

## 五、最终总结：Gelato 的竞品画像

## Gelato Relay

产品定位：面向 Web3 开发者的交易中继与 Gasless 基础设施

核心能力

交易提交、签名与 nonce 管理、Gas 估算、交易状态跟踪、重试及 Gas 代付。

产品差异化

多链统一资金管理、SyncFee、ERC-2771，以及与账户抽象产品的整合。

商业模式

实际链上 Gas 成本，加上适用的服务费、Gas 加价或订阅费用；具体费率需按产品和账户方案核实。

主要安全边界

用户签名、目标合约权限、可信转发者、Relayer 自身签名密钥，以及赞助资金的管理。

我的建议是，你们下一步可以把竞品分析重点放在三个问题上：

1. 交易执行能力： Gelato 如何保证交易提交的可靠性、吞吐量、失败处理与状态可观测性？
2. 资金和权限模型： 如何做到用户无需交出私钥，同时又能让 DApp 安全地代付 Gas、收取费用？
3. 商业模式： 如何在 Gas 成本、服务费、统一余额和企业级服务之间形成可持续的收入结构？

这三个维度能够帮助你们判断：应该直接复刻 Gelato 的核心 Relayer 能力，还是进一步发展成包含 Gas 代付、账户抽象和多链资金管理的综合基础设施产品。