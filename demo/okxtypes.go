package main

import (
	"context"
	"net/http"
	"net/url"
)

// chainInfo comes from /supported/chain. dexTokenApproveAddress is the spender
// used for ERC-20 approvals.
type chainInfo struct {
	ChainIndex             flexString `json:"chainIndex"`
	ChainID                flexString `json:"chainId"`
	ChainName              string     `json:"chainName"`
	DexTokenApproveAddress string     `json:"dexTokenApproveAddress"`
}

// tokenInfo comes from /all-tokens. Note the field is named decimals here, while
// quote/swap responses name the same concept decimal.
type tokenInfo struct {
	Decimals             flexString `json:"decimals"`
	TokenContractAddress string     `json:"tokenContractAddress"`
	TokenSymbol          string     `json:"tokenSymbol"`
	TokenName            string     `json:"tokenName"`
}

type tokenMeta struct {
	TokenContractAddress string     `json:"tokenContractAddress"`
	TokenSymbol          string     `json:"tokenSymbol"`
	Decimal              flexString `json:"decimal"`
	IsHoneyPot           bool       `json:"isHoneyPot"`
	TaxRate              flexString `json:"taxRate"`
}

type dexProtocol struct {
	DexName string     `json:"dexName"`
	Percent flexString `json:"percent"`
}

type dexRouter struct {
	Router        string          `json:"router"`
	RouterPercent flexString      `json:"routerPercent"`
	DexProtocol   dexProtocolList `json:"dexProtocol"`
}

type quoteData struct {
	ChainIndex      flexString  `json:"chainIndex"`
	FromTokenAmount flexString  `json:"fromTokenAmount"`
	ToTokenAmount   flexString  `json:"toTokenAmount"`
	TradeFee        flexString  `json:"tradeFee"`
	EstimateGasFee  flexString  `json:"estimateGasFee"`
	DexRouterList   []dexRouter `json:"dexRouterList"`
	FromToken       tokenMeta   `json:"fromToken"`
	ToToken         tokenMeta   `json:"toToken"`
}

// swapTx is the unsigned transaction data returned by OKX.
type swapTx struct {
	From                 string     `json:"from"`
	To                   string     `json:"to"`
	Data                 string     `json:"data"`
	Value                flexString `json:"value"`
	Gas                  flexString `json:"gas"`
	GasPrice             flexString `json:"gasPrice"`
	MaxPriorityFeePerGas flexString `json:"maxPriorityFeePerGas"`
	MinReceiveAmount     flexString `json:"minReceiveAmount"`
	MaxSpendAmount       flexString `json:"maxSpendAmount"`
	Slippage             flexString `json:"slippage"`
}

type swapData struct {
	RouterResult quoteData `json:"routerResult"`
	Tx           swapTx    `json:"tx"`
}

type approveData struct {
	Data               string     `json:"data"`
	DexContractAddress string     `json:"dexContractAddress"`
	GasLimit           flexString `json:"gasLimit"`
	GasPrice           flexString `json:"gasPrice"`
}

type txHistory struct {
	ChainIndex  flexString `json:"chainIndex"`
	TxHash      string     `json:"txHash"`
	Height      flexString `json:"height"`
	TxTime      flexString `json:"txTime"`
	Status      string     `json:"status"`
	TxType      string     `json:"txType"`
	FromAddress string     `json:"fromAddress"`
	ToAddress   string     `json:"toAddress"`
	DexRouter   string     `json:"dexrouter"`
}

type broadcastData struct {
	OrderID string `json:"orderId"`
	TxHash  string `json:"txHash"`
}

type quoteParams struct {
	chainIndex       string
	fromTokenAddress string
	toTokenAddress   string
	amount           string
	swapMode         string
}

func (p quoteParams) values() url.Values {
	return url.Values{
		"chainIndex":       {p.chainIndex},
		"fromTokenAddress": {p.fromTokenAddress},
		"toTokenAddress":   {p.toTokenAddress},
		"amount":           {p.amount},
		"swapMode":         {p.swapMode},
	}
}

// swapParams maps to the /swap query parameters. The live v6 endpoint requires
// slippagePercent; sending only slippage fails with code 50014.
type swapParams struct {
	chainIndex        string
	fromTokenAddress  string
	toTokenAddress    string
	amount            string
	swapMode          string
	slippage          string
	userWalletAddress string
	gasLevel          string
}

func (p swapParams) values() url.Values {
	v := url.Values{
		"chainIndex":        {p.chainIndex},
		"fromTokenAddress":  {p.fromTokenAddress},
		"toTokenAddress":    {p.toTokenAddress},
		"amount":            {p.amount},
		"swapMode":          {p.swapMode},
		"slippagePercent":   {slippageToPercent(p.slippage)},
		"userWalletAddress": {p.userWalletAddress},
	}
	if p.gasLevel != "" {
		v.Set("gasLevel", p.gasLevel)
	}
	return v
}

func (c *okxClient) supportedChains(ctx context.Context, chainIndex string) ([]chainInfo, error) {
	query := url.Values{"chainIndex": {chainIndex}}
	return c.fetch[chainInfo](ctx, http.MethodGet, aggregatorPath+"/supported/chain", query, nil)
}

func (c *okxClient) tokens(ctx context.Context, chainIndex string) ([]tokenInfo, error) {
	query := url.Values{"chainIndex": {chainIndex}}
	return c.fetch[tokenInfo](ctx, http.MethodGet, aggregatorPath+"/all-tokens", query, nil)
}

func (c *okxClient) quote(ctx context.Context, p quoteParams) ([]quoteData, error) {
	return c.fetch[quoteData](ctx, http.MethodGet, aggregatorPath+"/quote", p.values(), nil)
}

// approveTransaction returns the calldata and target contract of an unsigned approval.
func (c *okxClient) approveTransaction(ctx context.Context, chainIndex, tokenAddress, amount string) ([]approveData, error) {
	query := url.Values{
		"chainIndex":           {chainIndex},
		"tokenContractAddress": {tokenAddress},
		"approveAmount":        {amount},
	}
	return c.fetch[approveData](ctx, http.MethodGet, aggregatorPath+"/approve-transaction", query, nil)
}

func (c *okxClient) swap(ctx context.Context, p swapParams) ([]swapData, error) {
	return c.fetch[swapData](ctx, http.MethodGet, aggregatorPath+"/swap", p.values(), nil)
}

// history looks up the final status of a single-chain swap by txHash. It only covers
// transactions that went through the OKX DEX API. The endpoint takes both chainIndex
// and chainId; OKX uses the same value for EVM chains, so we reuse it here.
func (c *okxClient) history(ctx context.Context, chainIndex, chainID, txHash string) ([]txHistory, error) {
	query := url.Values{
		"chainIndex": {chainIndex},
		"chainId":    {chainID},
		"txHash":     {txHash},
	}
	return c.fetch[txHistory](ctx, http.MethodGet, aggregatorPath+"/history", query, nil)
}

// broadcastTransaction submits an already signed raw transaction through OKX, so no
// own RPC broadcast endpoint is needed.
func (c *okxClient) broadcastTransaction(ctx context.Context, signedTx, chainIndex, address string) ([]broadcastData, error) {
	body := map[string]string{
		"signedTx":   signedTx,
		"chainIndex": chainIndex,
		"address":    address,
	}
	return c.fetch[broadcastData](ctx, http.MethodPost, gatewayPath+"/broadcast-transaction", nil, body)
}
