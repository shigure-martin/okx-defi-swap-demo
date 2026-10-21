# OKX DEX 跨链 API 参考（实测版）

- **整理日期：** 2026-10-20
- **状态：** 已用真实 API Key 验证；未发现可访问的官方公开跨链文档页
- **Base URL：** `https://web3.okx.com`
- **鉴权：** 与单链 DEX API 相同，使用 `OK-ACCESS-KEY` / `OK-ACCESS-SIGN` / `OK-ACCESS-TIMESTAMP` / `OK-ACCESS-PASSPHRASE`

> 说明：OKX 当前公开的 DEX 文档主要覆盖单链聚合器。旧版跨链文档路径
> （如 `/zh-hans/onchainos/dev-docs-v5/dex-api/dex-cross-chain-api-introduction`）
> 目前返回 404 或回退到 Onchain OS 首页。因此本文件以线上接口实测结果为准。

## 1. 端点

全部位于 `/api/v6/dex/cross-chain/`：

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/supported/bridges` | 查询源链/目标链支持的桥 |
| GET | `/supported/tokens` | 查询支持跨链的代币 |
| GET | `/quote` | 获取跨链报价 |
| GET | `/approve-tx` | 构造 ERC-20 源币授权交易 |
| GET | `/swap` | 构造**源链**未签名交易 |

## 2. 请求参数

### `/quote`

| 参数 | 必填 | 说明 |
|---|---|---|
| `fromChainIndex` | 是 | 源链 OKX chainIndex，如 `42161` |
| `toChainIndex` | 是 | 目标链 OKX chainIndex，如 `1` |
| `fromChainId` | 是 | 源链 EVM chainId |
| `toChainId` | 是 | 目标链 EVM chainId |
| `fromTokenAddress` | 是 | 源币地址；原生币用 `0xeeee...eeee` |
| `toTokenAddress` | 是 | 目标币地址 |
| `amount` | 是 | 卖出数量，含精度 |
| `slippage` | 是 | **百分数**，`0.5` 表示 0.5%；不是单链的 `slippagePercent` |
| `allowBridge` / `denyBridge` | 否 | 按 bridgeId 允许/拒绝桥 |

### `/swap`

继承 `/quote` 的全部参数，另外：

| 参数 | 必填 | 说明 |
|---|---|---|
| `userWalletAddress` | 是 | 源链签名钱包 |
| `receiveAddress` | 否 | 目标链收款地址；默认等于钱包地址 |
| `bridgeId` | 否 | 指定桥；不传则由 OKX 自动选路 |
| `onlyBridge` | 否 | 只桥接，不做目标链兑换 |

### `/approve-tx`

| 参数 | 必填 | 说明 |
|---|---|---|
| `chainIndex` | 是 | 源链 chainIndex |
| `tokenContractAddress` | 是 | 源 ERC-20 代币 |
| `approveAmount` | 是 | 授权数量，含精度 |
| `userWalletAddress` | 是 | 钱包地址 |
| `bridgeId` | 是 | 与 `/swap` 使用同一个 bridgeId |

## 3. 响应要点

### `/quote`

```json
{
  "code": "0",
  "data": [{
    "fromTokenAmount": "1000000000000000",
    "toTokenAmount": "993260692088278",
    "fromChainIndex": "42161",
    "toChainIndex": "1",
    "routerList": [{
      "bridgeId": 636,
      "bridgeName": "ACROSS V3",
      "minimumReceived": "993260692088278",
      "estimateTime": "18",
      "otherNativeFee": "0",
      "needApprove": false,
      "needCancelApprove": false,
      "warningMessage": ""
    }]
  }]
}
```

- `minimumReceived`：目标链最低到账数量。
- `estimateTime`：预计耗时，单位秒。
- `otherNativeFee`：桥要求的额外目标链原生币费用；会被加到源链 `tx.value`。
- `needApprove`：ERC-20 源币是否需要先授权。
- `needCancelApprove`：是否需要先把额度重置为 0。

### `/swap`

```json
{
  "code": "0",
  "data": [{
    "fromTokenAmount": "1000000000000000",
    "minimumReceived": "993260692088278",
    "toTokenAmount": "993260692088278",
    "router": {
      "bridgeId": 636,
      "bridgeName": "ACROSS V3",
      "otherNativeFee": "0",
      "needApprove": false
    },
    "tx": {
      "from": "0x...",
      "to": "0x97CCDBea4632140639aD5eA9b944aa034eb15fD4",
      "data": "0x...",
      "value": "1000000000000000",
      "gasLimit": "197874",
      "gasPrice": "47010776",
      "maxPriorityFeePerGas": "19978376",
      "randomKeyAccount": [],
      "signatureData": []
    }
  }]
}
```

- **`/swap` 只返回源链交易**，目标链不需要、也无法由调用方签名。
- `tx.value` 已包含 `amount + otherNativeFee`（如果桥要求）。
- `randomKeyAccount` / `signatureData` 非空通常表示非 EVM 或特殊签名流程；本 demo 会拒绝。

### `/approve-tx`

```json
{
  "code": "0",
  "data": [{
    "approveAddress": "0x97CCDBea4632140639aD5eA9b944aa034eb15fD4",
    "chainIndex": "42161",
    "needApprove": true,
    "tokenContractAddress": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
    "tx": {
      "from": "0x...",
      "to": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
      "data": "0x095ea7b3...",
      "value": "1000000"
    }
  }]
}
```

- 授权交易的 `to` 是**代币合约**。
- `approveAddress` 才是 spender。
- 响应里的 `tx.value` 表示授权数量，**不是** ETH value；链上授权交易必须发送 0 ETH。

## 4. 推荐调用顺序

```text
supported/bridges & supported/tokens
  -> quote
  -> needApprove ? approve-tx -> 签/发授权交易
  -> swap（源链未签名交易）
  -> 本地签名 -> 广播源链交易
  -> 轮询目标链余额或对接桥方
```

## 5. 实测注意事项

1. **滑点参数名与单位不同**：跨链用 `slippage`，`0.5` = 0.5%；`5` 会返回 `82114 slippage too large`。
2. **quote/swap 可能不一致**：部分 ERC-20 币对 quote 返回 `82000 Insufficient liquidity`，但 `/swap` 仍可构造交易；实现应允许回退。
3. **目标链 Gas**：`supported/bridges` 返回 `requireOtherNativeFee`；为 true 的桥会在 quote 中返回 `otherNativeFee` 并加到 `tx.value`。
4. **没有目标链状态接口**：源链成功不等于目标链完成；需要轮询目标链余额或对接桥方。
5. **跨链源交易仍需自己签名/广播**：OKX 不持私钥、不代签、不代付 Gas。
6. **官方 SDK 的 bridge 模块已过时**：仍指向 v5 `build-tx`，不要直接照搬。

## 6. 官方入口

- DEX API 文档首页（主要覆盖单链）：
  https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-what-is-dex-api
- DEX API 参考（主要覆盖单链）：
  https://web3.okx.com/zh-hans/onchainos/dev-docs-v5/dex-api/dex-api-reference
- 开发者平台：
  https://web3.okx.com/onchainos/dev-portal
- 官方 SDK（bridge 模块过时）：
  https://github.com/okx/okx-dex-sdk

## 7. Demo 对应参数

```bash
go run . --cross-chain \
  --chain-index 42161 --to-chain-index 1 \
  --from 0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --to   0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --amount 0.001 --slippage 0.005 \
  --stage quote
```

- `--slippage 0.005` 会转换成 OKX 的 `slippage=0.5`。
- `--to-chain-id` / `--from-chain-id` 默认等于对应 chainIndex。
- `--watch-receive --to-rpc-url ...` 用于轮询目标链余额。
