# OKX DEX 兑换最小 Demo（单链 + 跨链）

用于验证 OKX DEX 聚合器上的**单链兑换**和 **EVM 跨链兑换**的最小可运行程序。
跨链部分只构造源链交易，目标链由桥执行。不包含策略、资金来源与人工审批。

> 想直接照步骤跑一遍，看同目录的操作手册 [`RUNBOOK.md`](RUNBOOK.md)；
> 本文档侧重接口细节、设计取舍与常见坑。

## 这个 Demo 验证什么

完整走通一条单链兑换链路，并明确每一步由谁负责：

```text
OKX /supported/chain      -> 拿链信息与授权合约地址（spender）
OKX /all-tokens           -> 拿代币精度
OKX /quote                -> 询价
OKX /swap                 -> 返回【未签名】交易数据
链上 allowance            -> 检查授权是否足够
OKX /approve-transaction  -> 如需授权，返回【未签名】授权交易
本地                      -> 签名（私钥不出本机）
自建 RPC 或 OKX 广播       -> 广播已签名交易
链上回执 + OKX /history    -> 确认结果
```

核心结论：**OKX 不持私钥、不代签名、不代付 Gas**，签名与广播必须由调用方完成。

## 前置条件

- Go 1.27+（用到 `encoding/json/v2` 与 `strings.SplitSeq`）
- OKX 开发者平台创建的 API Key / Secret Key / Passphrase
- 一个**独立的小额测试钱包**（不要用主钱包）
- 一个 EVM RPC 地址（Alchemy / Infura / 公共节点均可）
- 钱包在目标链上有原生币支付 Gas，且有足够的待兑换代币

## 配置

```bash
cd demo
cp .env.example .env   # 填入真实凭证
```

`.env` 中的变量（`--env-file` 默认为 `.env`，已存在的环境变量不会被覆盖）：

| 变量 | 必填 | 说明 |
|---|---|---|
| `OKX_API_KEY` / `OKX_SECRET_KEY` / `OKX_PASSPHRASE` | 是 | 询价阶段也需要鉴权 |
| `OKX_PROJECT_ID` | 否 | 部分接口需要 |
| `RPC_URL` | `sign`/`execute` 阶段必填 | 读取 nonce、授权额度与回执 |
| `WALLET_PRIVATE_KEY` | `execute` 阶段必填 | 仅本机使用，绝不外发 |
| `WALLET_ADDRESS` | 否 | 不设私钥时，供只读阶段使用 |

## 分阶段运行

### 无需真实 Key 的自检

```bash
cd demo
gofmt -l .                      # 无输出表示格式正确
go vet ./...                    # 静态检查
go test -v ./...                # 5 个测试：精度换算、签名、请求头与业务码处理
go run . --help                 # 查看全部参数
```

其中 `TestSign` 的期望值由 `openssl` 独立计算，用于校验签名实现；
`TestFetchSendsSignedRequest` 用内存 stub 校验签名原文确实包含查询串。

按测试计划从只读链路逐步推进，默认阶段是最安全的 `quote`：

```bash
# 1) 只询价（默认）：不签名、不广播
go run . --chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xA0b86991c6218b36c1d19D4a2e9eb0ce3606eb48 \
  --amount 0.001

# 2) 额外拿到未签名的兑换交易数据（对应测试计划阶段 B）
go run . --stage swap-data --chain-index 1 --from 0x... --to 0x... --amount 0.001

# 3) 本地签名，只打印原始交易，不广播；ERC-20 卖出会同时签授权交易
go run . --stage sign --chain-index 1 --from 0x... --to 0x... --amount 0.001

# 4) 小额实盘：签名并广播，等待回执
go run . --stage execute --chain-index 1 --from 0x... --to 0x... --amount 0.001 --slippage 0.005
```

用 `--broadcast okx` 可以改走 OKX 广播接口（入参是已签名交易，带多播与 MEV 保护），
默认走自建 RPC。

卖出 ERC-20 时，`sign` 会输出两笔已签名交易：`approval` 使用当前 nonce，`swap`
使用下一个 nonce。`execute` 会先广播授权、等待确认，再广播兑换。

常用参数：

| 参数 | 默认 | 说明 |
|---|---|---|
| `--chain-index` | `1` | OKX chainIndex：1=Ethereum、56=BSC、137=Polygon、8453=Base |
| `--slippage` | `0.005` | 滑点上限，0.005 表示 0.5% |
| `--gas-level` | `average` | `slow` / `average` / `fast` |
| `--swap-mode` | `exactIn` | `exactIn` / `exactOut` |
| `--from-decimals` / `--to-decimals` | 自动 | 代币不在 OKX 主流列表时手动指定精度 |
| `--receipt-timeout` | `2m` | 等待回执超时 |

## 跨链兑换（`--cross-chain`）

跨链使用 OKX v6 的 `/api/v6/dex/cross-chain/*` 接口。关键点：**OKX 只返回源链交易**，
目标链由桥/求解器执行，代码只负责签名和广播源链交易。

```bash
# 1) 报价：Arbitrum 上的 0.001 ETH -> Base 上的 ETH
go run . --cross-chain --chain-index 42161 --to-chain-index 8453 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage quote

# 2) 获取源链未签名交易
go run . --cross-chain --chain-index 42161 --to-chain-index 8453 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage swap-data

# 3) 本地签名；ERC-20 源币会同时签授权交易
go run . --cross-chain --chain-index 42161 --to-chain-index 8453 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage sign

# 4) 广播源链交易，并轮询目标链余额
go run . --cross-chain --chain-index 42161 --to-chain-index 8453 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage execute \
  --watch-receive --to-rpc-url https://mainnet.base.org
```

跨链相关参数：

| 参数 | 说明 |
|---|---|
| `--to-chain-index` | 目标链 OKX chainIndex，跨链必填 |
| `--from-chain-id` / `--to-chain-id` | EVM chain ID；默认等于对应 chainIndex |
| `--receive-address` | 目标地址；默认使用当前钱包 |
| `--allow-bridge` / `--deny-bridge` | 按 bridgeId 允许/拒绝桥 |
| `--only-bridge` | 只桥接，不做目标链兑换 |
| `--watch-receive` + `--to-rpc-url` | 源链确认后轮询目标链余额 |

注意事项：

- **目标链没有状态查询接口**：OKX 不提供跨链完成状态，`--watch-receive` 通过目标 RPC
  轮询余额变化，超时只表示“尚未确认”，不代表失败。
- **`otherNativeFee`**：部分桥会要求在源链额外支付目标链原生币费用，并把它加到
  `tx.value`。执行前必须确认源链余额足够覆盖 `value + gas`。
- **ERC-20 源币授权**：`needApprove=true` 时使用 `/cross-chain/approve-tx`，spender 是
  返回的 `approveAddress`；授权交易本身仍发送给代币合约，ETH value 为 0。
- **quote/swap 不一致**：USDC 等币对可能出现 quote 返回 `82000 Insufficient liquidity`，
  但 swap 仍能构造交易；demo 在 `swap-data`/`sign`/`execute` 会回退到 swap 数据。
- 跨链只支持 EVM 源链/目标链；非 EVM 的跨链签名数据不在本 demo 范围内。

完整的跨链端点、参数和响应字段见
[`../docs/okx_cross_chain_api_reference.md`](../docs/okx_cross_chain_api_reference.md)。

## 实测时需要留意的接口细节

以下均按官方文档与端点实测核对过，容易踩坑：

- **路径版本不统一**：聚合器在 `/api/v6/dex/aggregator/*`（v5 同路径仍存活），
  交易上链在 `/api/v5/dex/pre-transaction/*`，跨链已在 v5 废弃、迁至 v6。
- **滑点参数名以线上实际返回为准：`/swap` 要求 `slippagePercent`**；只传 `slippage`
  会返回 `50014`（2026-10-19 实测）。
- **滑点单位不同**：CLI 的 `--slippage` 用小数（`0.005` = 0.5%），请求 OKX 时自动
  换算为百分数（`slippagePercent=0.5`）。
- **OKX 的 JSON 类型不稳定**：`chainIndex` 可能是字符串或数字，`dexProtocol` 可能是
  单个对象或数组，解析层必须同时兼容。
- **精度字段名不一致**：`/all-tokens` 返回 `decimals`，而 quote/swap 响应里是 `decimal`。
- **授权对象（spender）来自 `/supported/chain` 的 `dexTokenApproveAddress`**，
  不要硬编码。
- **原生币用占位地址** `0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee`，且无需授权。
- **`value` 是十进制 wei 字符串**，已被程序解析为整数，全程不使用浮点。
- **EIP-1559 判断**：OKX 返回了非零 `maxPriorityFeePerGas` 时用动态费用交易，
  否则退回 legacy 交易。

## 安全约束

- 默认阶段是 `quote`，必须显式指定 `--stage execute` 才会真正广播。
- 首次实盘务必使用独立测试钱包与最小金额，并先跑通 `swap-data` 与 `sign`。
- **回执超时不等于失败**：程序会提示先用 txHash 查询链上状态，确认前不要重复提交。
- 授权按本次所需精确额度申请，不做无限授权。
- 私钥只从环境变量读取，不落盘、不打印；`.env` 已被 `.gitignore` 忽略。

## 已知范围与限制

- 支持单链兑换和 EVM 跨链；跨链只构造源链交易，目标链完成状态由余额轮询判断。
- 报价过期重取、卡单替换、nonce 池、多账户与对账均未实现。
- **已验证（2026-10-20）**：真实 API Key + 真实资金完成单链 10 USDC→ETH（含授权、广播、
  回执）；跨链完成报价、源链交易数据与本地签名。
- **未验证**：跨链真实广播与目标链到账；首次跨链实盘请以实际返回为准。
- 阶段 3 只签名不广播，若授权额度不足，该笔交易广播后会 revert。
- 本 Demo 没有连接到 OKX 的模拟接口，先在 `quote` / `swap-data` 阶段确认数据正确。

## 目录结构

| 文件 | 职责 |
|---|---|
| `main.go` | 编排：链路、代币、报价、授权、签名、广播、确认 |
| `okx.go` | OKX 客户端：HMAC-SHA256 签名、请求与业务码处理 |
| `okxtypes.go` | 各接口的请求参数与响应模型（含端点封装） |
| `evm.go` | 链上交互：钱包/私钥、allowance、nonce、签名、广播、回执 |
| `amount.go` | 金额精度换算（全部使用整数，避免浮点误差） |
| `config.go` | 参数解析、`.env` 加载与校验 |
| `amount_test.go` | 精度换算的单元测试 |
| `okx_test.go` | 签名、请求头与业务码处理的测试（含内存 stub） |

## 常见问题

- `go build .` 报 `error obtaining VCS status`：仓库根目录的 `.git` 不是有效的 git 仓库，
  加上 `-buildvcs=false` 即可；`go run .` 不受影响。
- 依赖未下载：先执行 `go mod download`。
- 报 `50103 Request header OK-ACCESS-KEY can not be empty` 但明明配了 Key：把占位值
  写成全零 UUID（`00000000-0000-0000-0000-000000000000`）会被 OKX 当成空值，
  这个报错会误导排查方向，换成真实 Key 即可。
