package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

const crossChainPath = "/api/v6/dex/cross-chain"

type crossToken struct {
	Decimals             flexString `json:"decimals"`
	TokenContractAddress string     `json:"tokenContractAddress"`
	TokenSymbol          string     `json:"tokenSymbol"`
}

type crossRouter struct {
	BridgeID              flexString `json:"bridgeId"`
	BridgeName            string     `json:"bridgeName"`
	CrossChainFee         flexString `json:"crossChainFee"`
	EstimateGasFee        flexString `json:"estimateGasFee"`
	EstimateTime          flexString `json:"estimateTime"`
	MinimumReceived       flexString `json:"minimumReceived"`
	NeedApprove           bool       `json:"needApprove"`
	NeedCancelApprove     bool       `json:"needCancelApprove"`
	OtherNativeFee        flexString `json:"otherNativeFee"`
	PriceImpactPercentage flexString `json:"priceImpactPercentage"`
	ToTokenAmount         flexString `json:"toTokenAmount"`
	WarningMessage        string     `json:"warningMessage"`
}

type crossQuoteData struct {
	FromChainIndex  flexString    `json:"fromChainIndex"`
	FromToken       crossToken    `json:"fromToken"`
	FromTokenAmount flexString    `json:"fromTokenAmount"`
	RouterList      []crossRouter `json:"routerList"`
	ToChainIndex    flexString    `json:"toChainIndex"`
	ToToken         crossToken    `json:"toToken"`
}

// crossTx is the source-chain transaction returned by the cross-chain swap API.
// It uses gasLimit instead of the single-chain swap's gas field.
type crossTx struct {
	Data                 string     `json:"data"`
	From                 string     `json:"from"`
	GasLimit             flexString `json:"gasLimit"`
	GasPrice             flexString `json:"gasPrice"`
	MaxPriorityFeePerGas flexString `json:"maxPriorityFeePerGas"`
	RandomKeyAccount     []any      `json:"randomKeyAccount"`
	SignatureData        []any      `json:"signatureData"`
	To                   string     `json:"to"`
	Value                flexString `json:"value"`
}

type crossSwapData struct {
	FromTokenAmount flexString  `json:"fromTokenAmount"`
	MinimumReceived flexString  `json:"minimumReceived"`
	Router          crossRouter `json:"router"`
	ToTokenAmount   flexString  `json:"toTokenAmount"`
	Tx              crossTx     `json:"tx"`
}

type crossApproveTx struct {
	Data string `json:"data"`
	From string `json:"from"`
	To   string `json:"to"`
	// Value is the approval amount in OKX's response, not the ETH value.
	// The on-chain approve transaction must send 0 ETH.
	Value flexString `json:"value"`
}

type crossApproveData struct {
	ApproveAddress       string         `json:"approveAddress"`
	ChainIndex           flexString     `json:"chainIndex"`
	NeedApprove          bool           `json:"needApprove"`
	TokenContractAddress string         `json:"tokenContractAddress"`
	Tx                   crossApproveTx `json:"tx"`
}

type crossQuoteParams struct {
	fromChainIndex   string
	toChainIndex     string
	fromChainID      string
	toChainID        string
	fromTokenAddress string
	toTokenAddress   string
	amount           string
	slippage         string
	allowBridge      string
	denyBridge       string
}

func (p crossQuoteParams) values() url.Values {
	v := url.Values{
		"fromChainIndex":   {p.fromChainIndex},
		"toChainIndex":     {p.toChainIndex},
		"fromChainId":      {p.fromChainID},
		"toChainId":        {p.toChainID},
		"fromTokenAddress": {p.fromTokenAddress},
		"toTokenAddress":   {p.toTokenAddress},
		"amount":           {p.amount},
		"slippage":         {slippageToPercent(p.slippage)},
	}
	if p.allowBridge != "" {
		v.Set("allowBridge", p.allowBridge)
	}
	if p.denyBridge != "" {
		v.Set("denyBridge", p.denyBridge)
	}
	return v
}

type crossSwapParams struct {
	crossQuoteParams
	userWalletAddress string
	receiveAddress    string
	bridgeID          string
	onlyBridge        bool
}

func (p crossSwapParams) values() url.Values {
	v := p.crossQuoteParams.values()
	v.Set("userWalletAddress", p.userWalletAddress)
	if p.receiveAddress != "" {
		v.Set("receiveAddress", p.receiveAddress)
	}
	if p.bridgeID != "" {
		v.Set("bridgeId", p.bridgeID)
	}
	if p.onlyBridge {
		v.Set("onlyBridge", "true")
	}
	return v
}

type crossApproveParams struct {
	chainIndex           string
	tokenContractAddress string
	approveAmount        string
	userWalletAddress    string
	bridgeID             string
}

func (p crossApproveParams) values() url.Values {
	return url.Values{
		"chainIndex":           {p.chainIndex},
		"tokenContractAddress": {p.tokenContractAddress},
		"approveAmount":        {p.approveAmount},
		"userWalletAddress":    {p.userWalletAddress},
		"bridgeId":             {p.bridgeID},
	}
}

func (c *okxClient) crossQuote(ctx context.Context, p crossQuoteParams) ([]crossQuoteData, error) {
	return c.fetch[crossQuoteData](ctx, http.MethodGet, crossChainPath+"/quote", p.values(), nil)
}

func (c *okxClient) crossSwap(ctx context.Context, p crossSwapParams) ([]crossSwapData, error) {
	return c.fetch[crossSwapData](ctx, http.MethodGet, crossChainPath+"/swap", p.values(), nil)
}

func (c *okxClient) crossApprove(ctx context.Context, p crossApproveParams) ([]crossApproveData, error) {
	return c.fetch[crossApproveData](ctx, http.MethodGet, crossChainPath+"/approve-tx", p.values(), nil)
}

func crossDecimals(t crossToken, fallback int) int {
	if t.Decimals != "" {
		if n, err := strconv.Atoi(string(t.Decimals)); err == nil {
			return n
		}
	}
	return fallback
}
