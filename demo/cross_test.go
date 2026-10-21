package main

import (
	"encoding/json/v2"
	"testing"
)

func TestCrossQuoteParams(t *testing.T) {
	params := crossQuoteParams{
		fromChainIndex:   "42161",
		toChainIndex:     "8453",
		fromChainID:      "42161",
		toChainID:        "8453",
		fromTokenAddress: "0xfrom",
		toTokenAddress:   "0xto",
		amount:           "1000000000000000",
		slippage:         "0.005",
		allowBridge:      "636",
		denyBridge:       "1004",
	}

	values := params.values()
	if got := values.Get("slippage"); got != "0.5" {
		t.Errorf("slippage = %q, want 0.5", got)
	}
	if got := values.Get("slippagePercent"); got != "" {
		t.Errorf("slippagePercent = %q, want it to be omitted", got)
	}
	if got := values.Get("fromChainId"); got != "42161" {
		t.Errorf("fromChainId = %q", got)
	}
	if got := values.Get("allowBridge"); got != "636" {
		t.Errorf("allowBridge = %q", got)
	}
}

func TestCrossSwapParams(t *testing.T) {
	params := crossSwapParams{
		crossQuoteParams: crossQuoteParams{
			fromChainIndex: "42161",
			toChainIndex:   "8453",
			fromChainID:    "42161",
			toChainID:      "8453",
			amount:         "1000",
			slippage:       "0.005",
		},
		userWalletAddress: "0xwallet",
		receiveAddress:    "0xreceive",
		bridgeID:          "636",
		onlyBridge:        true,
	}

	values := params.values()
	for key, want := range map[string]string{
		"userWalletAddress": "0xwallet",
		"receiveAddress":    "0xreceive",
		"bridgeId":          "636",
		"onlyBridge":        "true",
		"slippage":          "0.5",
	} {
		if got := values.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestCrossApproveParams(t *testing.T) {
	params := crossApproveParams{
		chainIndex:           "42161",
		tokenContractAddress: "0xusdc",
		approveAmount:        "1000000",
		userWalletAddress:    "0xwallet",
		bridgeID:             "636",
	}
	values := params.values()
	for key, want := range map[string]string{
		"chainIndex":           "42161",
		"tokenContractAddress": "0xusdc",
		"approveAmount":        "1000000",
		"userWalletAddress":    "0xwallet",
		"bridgeId":             "636",
	} {
		if got := values.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestCrossQuoteDecode(t *testing.T) {
	const body = `{
		"fromChainIndex":"42161",
		"fromToken":{"decimals":18,"tokenContractAddress":"0xeeee","tokenSymbol":"ETH"},
		"fromTokenAmount":"1000000000000000",
		"routerList":[{
			"bridgeId":636,
			"bridgeName":"ACROSS V3",
			"estimateTime":"12",
			"minimumReceived":"997869068138731",
			"needApprove":false,
			"otherNativeFee":"0",
			"toTokenAmount":"997869068138731"
		}],
		"toChainIndex":"8453",
		"toToken":{"decimals":18,"tokenContractAddress":"0xeeee","tokenSymbol":"ETH"}
	}`
	var quote crossQuoteData
	if err := json.Unmarshal([]byte(body), &quote); err != nil {
		t.Fatalf("Unmarshal() returned an error: %v", err)
	}
	if len(quote.RouterList) != 1 {
		t.Fatalf("routerList length = %d, want 1", len(quote.RouterList))
	}
	if quote.RouterList[0].BridgeID != "636" {
		t.Errorf("bridgeId = %q, want 636", quote.RouterList[0].BridgeID)
	}
	if quote.ToToken.Decimals != "18" {
		t.Errorf("toToken.decimals = %q, want 18", quote.ToToken.Decimals)
	}
}

func TestCrossSwapDecode(t *testing.T) {
	const body = `{
		"fromTokenAmount":"1000000",
		"minimumReceived":"412966252623501",
		"router":{"bridgeId":636,"bridgeName":"ACROSS V3","needApprove":true,"otherNativeFee":"0"},
		"toTokenAmount":"416063428787395",
		"tx":{
			"data":"0x110560ad",
			"from":"0xwallet",
			"gasLimit":"135000",
			"gasPrice":"46856256",
			"maxPriorityFeePerGas":"19856256",
			"randomKeyAccount":[],
			"signatureData":[],
			"to":"0xrouter",
			"value":"0"
		}
	}`
	var swap crossSwapData
	if err := json.Unmarshal([]byte(body), &swap); err != nil {
		t.Fatalf("Unmarshal() returned an error: %v", err)
	}
	if !swap.Router.NeedApprove {
		t.Error("needApprove = false, want true")
	}
	if swap.Tx.GasLimit != "135000" || swap.Tx.Value != "0" {
		t.Errorf("tx = %+v, want gasLimit 135000 and value 0", swap.Tx)
	}
}

func TestIsLiquidityError(t *testing.T) {
	if !isLiquidityError(errString("OKX business error: code=82000 msg=Insufficient liquidity")) {
		t.Error("expected a liquidity error")
	}
	if isLiquidityError(errString("OKX business error: code=50014")) {
		t.Error("unexpected liquidity error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
