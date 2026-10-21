# OKX DEX 兑换 Demo 操作手册（单链 + 跨链）

面向执行者的一步步操作说明：照顺序做即可。
接口清单、字段说明与设计取舍见同目录的 [`README.md`](README.md)。

## 0. 准备

| 项目 | 要求 |
|---|---|
| Go | 1.27 或更高（用 `go version` 确认） |
| OKX 凭证 | 开发者平台创建的 API Key / Secret Key / Passphrase |
| 钱包 | **独立的测试钱包**，不要用主钱包 |
| RPC | 源链 RPC；跨链还需要目标链 RPC（通过 `--to-rpc-url` 传入） |

```bash
cd demo
```

不需要真实凭证就能先自检一遍：

```bash
gofmt -l .          # 无输出 = 格式正确
go vet ./...        # 静态检查
go test -v ./...    # 17 个测试：精度换算、签名、请求头、业务码、跨链参数与解析
go run . --help     # 查看全部参数
```

## 1. 配置

```bash
cp .env.example .env
```

按你要跑到哪一步，把对应变量填进 `.env`：

| 变量 | 阶段 1-2 | 阶段 3 | 阶段 4 |
|---|---|---|---|
| `OKX_API_KEY` / `OKX_SECRET_KEY` / `OKX_PASSPHRASE` | 必需 | 必需 | 必需 |
| `OKX_PROJECT_ID` | 可选 | 可选 | 可选 |
| `WALLET_ADDRESS` | 必需 | — | — |
| `WALLET_PRIVATE_KEY` | — | 必需 | 必需 |
| `RPC_URL` | — | 必需 | 必需 |

说明：

- 三个阶段都需要 OKX 凭证，因为询价本身也要鉴权。
- `WALLET_ADDRESS` 与 `WALLET_PRIVATE_KEY` 二选一即可；填了私钥会自动推导地址。
- 私钥只在本机使用，`.env` 已被 `.gitignore` 忽略。
- 跨链不新增 `.env` 变量：源链 RPC 用 `RPC_URL`，目标链 RPC 用 `--to-rpc-url`，
  目标收款地址用 `--receive-address`（默认当前钱包）。

## 2. 分四步执行

### 步骤 1：询价（默认阶段，不动资金）

```bash
go run . --chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xA0b86991c6218b36c1d19D4a2e9eb0ce3606eb48 \
  --amount 0.001
```

看这两行：

```console
level=INFO msg=quote fromAmount=... toAmount=... toAmountHuman=... toSymbol=USDC gasFeeUSD=... isHoneyPot=false
level=INFO msg=route protocols=...
```

**这一步不会签名、不会广播。**

### 步骤 2：获取未签名的交易数据

```bash
go run . --stage swap-data --chain-index 1 --from 0x... --to 0x... --amount 0.001
```

多出一行：

```console
level=INFO msg="unsigned swap transaction" to=0x3b3ae...6790 value=0 gas=... minReceiveAmount=... calldataBytes=...
```

`to` 是 OKX DEX router 合约、`value` 是随交易发送的原生币数量、`minReceiveAmount` 是滑点极限下的最少到账。
可以用区块浏览器或其他工具核对这份数据，仍然不广播。

### 步骤 3：本地签名（仍然不广播）

```bash
go run . --stage sign --chain-index 1 --from 0x... --to 0x... --amount 0.001
```

卖原生币时多出一行：

```console
level=INFO msg="locally signed (not broadcast)" txHash=0x... nonce=0 gas=... rawTx=0x02f8...
```

`rawTx` 就是已签名的原始交易，可以直接丢给任何广播接口。

卖 ERC-20 时，如果授权额度不足，会多出两行：先输出 `locally signed approval (not broadcast)`
（当前 nonce），再输出 `locally signed (not broadcast)`（下一个 nonce）。两笔都签好后才能广播。

### 步骤 4：小额实盘

先确认钱包在当前链上有原生币付 Gas，且余额足够卖出 `--amount`。然后：

```bash
go run . --stage execute --chain-index 1 --from 0x... --to 0x... --amount 0.001 --slippage 0.005
```

正常输出（ERC-20 作为卖出币且额度不足时，会先有两行授权）：

```console
level=WARN  msg="allowance is insufficient" current=0 required=1000
level=INFO  msg="sending approval transaction" spender=0x3b3ae...6790 amount=1000 txHash=0x...
level=INFO  msg="approval confirmed" txHash=0x...
level=INFO  msg="locally signed" txHash=0x... nonce=1 gas=... to=0x3b3ae...6790 value=0
level=INFO  msg="broadcast via own RPC" txHash=0x...
level=INFO  msg="transaction confirmed" txHash=0x... block=... gasUsed=...
level=INFO  msg="OKX transaction status" status=success txType=Swap height=... txTime=...
```

看到 `msg="transaction confirmed"` 即为链上成功。

> OKX 的 `/history` 二次核对失败只会打印 warning，不影响链上回执；以 txHash 的
> `transaction confirmed` 为准。

改走 OKX 广播通道（免自建 RPC 广播，带多播与 MEV 保护）：

```bash
go run . --stage execute --broadcast okx ...
```

## 3. 参数速查

| 参数 | 默认 | 说明 |
|---|---|---|
| `--stage` | `quote` | `quote` / `swap-data` / `sign` / `execute` |
| `--chain-index` | `1` | 1=Ethereum、56=BSC、137=Polygon、8453=Base |
| `--from` / `--to` | 必填 | 卖出/买入代币合约地址；原生币用 `0xeeee...eeee` |
| `--amount` | 必填 | 人类可读数量；`exactOut` 模式下表示买入数量 |
| `--slippage` | `0.005` | 0.005 即 0.5% |
| `--swap-mode` | `exactIn` | `exactIn` / `exactOut` |
| `--gas-level` | `average` | `slow` / `average` / `fast` |
| `--from-decimals` / `--to-decimals` | 自动 | 代币不在 OKX 主流列表时手动指定；跨链 quote 失败时建议显式指定 `--to-decimals` |
| `--broadcast` | `rpc` | `rpc` / `okx` |
| `--receipt-timeout` | `2m` | 等待回执超时 |
| `--watch-timeout` | `15m` | 跨链目标链余额轮询超时 |
| `--env-file` | `.env` | 环境变量文件；文件不存在则忽略 |
| `--wallet-address` | 读环境变量 | 只读阶段用的钱包地址，等价于 `WALLET_ADDRESS` |
| `--cross-chain` | 关闭 | 启用 OKX 跨链兑换 |
| `--to-chain-index` | 必填（跨链） | 目标链 OKX chainIndex |
| `--from-chain-id` / `--to-chain-id` | 等于对应 chainIndex | EVM chain ID |
| `--receive-address` | 当前钱包 | 目标链收款地址 |
| `--allow-bridge` / `--deny-bridge` | 空 | 按 bridgeId 允许/拒绝桥 |
| `--only-bridge` | 关闭 | 只桥接，不做目标链兑换 |
| `--watch-receive` / `--to-rpc-url` | 关闭 / 空 | 源链确认后轮询目标链余额 |

跨链只支持 `--swap-mode exactIn`；`--gas-level` 对跨链不生效，Gas 由 OKX 返回。

## 4. 出错了怎么办

| 现象 | 含义 | 处理 |
|---|---|---|
| `code=50103 ... OK-ACCESS-KEY can not be empty` | Key 没被识别 | 检查 `OKX_API_KEY`；把占位值写成全零 UUID 会被 OKX 当成空值 |
| `code=50111 Invalid OK-ACCESS-KEY` | Key 不存在 | 核对 Key 是否复制完整 |
| `code=50114 Invalid Authority` | Key 存在但鉴权不通过 | 核对 Secret Key 与 Passphrase |
| `code=50014 ... slippagePercent cannot be empty` | `/swap` 的滑点参数名不匹配 | 使用 `slippagePercent`；本 demo 已按线上行为修正 |
| `code=82000 Insufficient liquidity`（quote） | 报价侧流动性不足 | 改用 `--stage swap-data`；demo 在后续阶段会自动回退到 swap 数据 |
| `code=82114 The slippage too large` | 跨链滑点超过桥的限制 | 降低 `--slippage`，例如 `0.005`（0.5%） |
| `otherNativeFee` 较大 | 桥要求在源链额外支付目标链费用 | 确认源链余额覆盖 `tx.value + gas`，或换桥 |
| `insufficient source balance` | 源链余额不足（`value` 已包含 `otherNativeFee`） | 充值源链原生币，或换一条 `otherNativeFee=0` 的桥 |
| `RPC chain ID ... does not match` | RPC 与 `--chain-index` / `--to-chain-id` 不一致 | 修正 RPC 或链参数；不要用错误链签名 |
| `requires extra signing data` | 该路由需要非 EVM/额外签名数据 | 换桥或换路由；本 demo 只支持普通 EVM 签名 |
| `requires a zero-approval reset` | 路由要求先把授权额度重置为 0 | 先手动发一笔 approve(0)，再重新执行 |
| `destination not confirmed before timeout` | 目标链在超时内尚未到账 | **不是失败**；用目标链浏览器或桥方状态继续确认 |
| `missing required OKX credentials` | 没读到环境变量 | 确认在 `demo/` 目录下运行且 `.env` 存在 |
| `the sign/execute stages require RPC_URL` | 阶段 3/4 缺 RPC | 填 `RPC_URL` |
| `token ... is not in the OKX token list` | 代币不在主流列表 | 用 `--from-decimals` / `--to-decimals` 指定精度 |
| `go build .` 报 `error obtaining VCS status` | 仓库根目录 `.git` 不是有效仓库 | 加 `-buildvcs=false`，或直接用 `go run .` |
| 回执超时 | 交易可能只是还没打包 | **不要重跑**，用 txHash 到区块浏览器确认状态 |

## 5. 安全操作要点

- 默认阶段是 `quote`，只有显式 `--stage execute` 才会广播交易。
- 第一次实盘务必用最小金额，先跑通步骤 2、3 再做步骤 4。
- 授权按本次所需额度申请，不做无限授权。
- 交易哈希是唯一凭据：超时或报错时先用它查链上状态，确认前不要重复提交。
- 跨链源链成功不等于目标链成功；必须确认目标收款地址余额或桥方状态。
- 目标链收款地址默认是当前钱包；要换地址必须用 `--receive-address` 重新构造交易，
  不能手改 calldata。
- 广播前检查 `tx.value` 是否包含 `otherNativeFee`，并确认源链余额覆盖 `value + gas`。
- 需要立刻停手时直接 `Ctrl+C`，不会产生后续交易。

## 6. 跨链兑换（可选）

OKX 只返回**源链**交易，目标链由桥执行。以 Arbitrum → Ethereum 的 0.001 ETH 为例：

```bash
# 报价
go run . --cross-chain --chain-index 42161 --to-chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage quote
# 如果这里返回 82000，直接执行下面的 swap-data；后续阶段会自动回退到 /swap

# 源链未签名交易
go run . --cross-chain --chain-index 42161 --to-chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage swap-data --allow-bridge 636

# 本地签名（不广播）
go run . --cross-chain --chain-index 42161 --to-chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage sign --allow-bridge 636

# 广播源链交易并轮询目标链余额
go run . --cross-chain --chain-index 42161 --to-chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --stage execute \
  --allow-bridge 636 \
  --watch-receive --to-rpc-url https://ethereum-rpc.publicnode.com
```

要点：

- 只有源链交易需要签名；目标链不需要、也无法由你签名。
- `--receive-address` 默认等于当前钱包；目标链到账后查这个地址的余额。要换地址必须
  重新构造交易，不能手改 calldata。
- `--allow-bridge 636` 强制走 Across V3（本次实测约 10–20 秒到账、`otherNativeFee=0`）；
  不传则由 OKX 自动选桥。Stargate V2 等桥可能要求额外目标链费用。
- `quote` 加 `--allow-bridge 636` 可能返回 `82000 Insufficient liquidity`，这是 quote
  接口的限制；`swap-data` / `sign` / `execute` 会自动回退到 `/swap`，仍可继续。
- `needApprove=true` 时，先签/发 `/cross-chain/approve-tx` 返回的授权交易，再发源链交易。
- `otherNativeFee` 会被加到源链 `tx.value`，执行前确认余额足够。
- OKX 没有跨链状态查询接口；`--watch-receive` 只是目标链余额轮询，超时不代表失败。

正常执行会看到：

```console
msg="broadcast cross-chain source transaction via own RPC" txHash=0x...
msg="source transaction confirmed" txHash=0x... block=... gasUsed=...
msg="cross-chain submitted; source confirmation does not mean destination completion" bridge="ACROSS V3"
msg="watching destination balance" address=0x... initial=...
msg="destination balance updated" delta=... current=...
```

改跑 Arbitrum → Base 时，把 `--to-chain-index` 改为 `8453`，`--to-rpc-url` 改为
`https://mainnet.base.org` 即可；代币地址也要换成目标链上的地址。

完整的跨链端点与字段参考见
[`../docs/okx_cross_chain_api_reference.md`](../docs/okx_cross_chain_api_reference.md)。

## 7. 本 Demo 的边界

- 覆盖单链兑换和 EVM 跨链；跨链目标链完成状态依赖余额轮询，不是 OKX 状态接口。
- 不含报价过期重取、卡单替换、nonce 池、多账户与对账。
- 已用真实资金验证单链 10 USDC→ETH（2026-10-20）和跨链 0.001 ETH Arbitrum→Ethereum
  （Across V3，约 10 秒到账）。
- 未验证：其他桥、非 EVM 跨链、失败/退款/卡单恢复路径。
- OKX 侧不代签名、不代付 Gas：私钥签名与广播始终由本程序完成。
