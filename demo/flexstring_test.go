package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"
)

func TestFlexString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "string", input: `"123"`, want: "123"},
		{name: "number", input: `123`, want: "123"},
		{name: "negative decimal", input: `-1.5`, want: "-1.5"},
		{name: "large integer", input: `123456789012345678901234567890`, want: "123456789012345678901234567890"},
		{name: "null", input: `null`, want: ""},
		{name: "boolean", input: `true`, wantErr: true},
		{name: "object", input: `{}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got flexString
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("Unmarshal(%s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestChainInfoAcceptsNumericFields(t *testing.T) {
	const body = `{"code":"0","msg":"","data":[{"chainId":1,"chainIndex":1,"chainName":"Ethereum","dexTokenApproveAddress":"0x40aA958dd87FC8305b97f2BA922CDdCa374bcD7f"}]}`

	client := stubClient(func(*http.Request) *http.Response {
		return jsonResponse(body)
	})

	chains, err := client.supportedChains(t.Context(), "1")
	if err != nil {
		t.Fatalf("supportedChains() returned an error: %v", err)
	}
	if len(chains) != 1 || chains[0].ChainIndex != "1" {
		t.Fatalf("supportedChains() = %+v, want one chain with index 1", chains)
	}
}

func TestDexProtocolListAcceptsObjectOrArray(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "object", input: `{"dexName":"Uniswap V3","percent":"100"}`, want: 1},
		{name: "array", input: `[{"dexName":"Uniswap V3","percent":"60"},{"dexName":"Curve","percent":"40"}]`, want: 2},
		{name: "null", input: `null`, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got dexProtocolList
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) returned an error: %v", tt.input, err)
			}
			if len(got) != tt.want {
				t.Errorf("len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestSwapParamsUseSlippagePercent(t *testing.T) {
	params := swapParams{
		chainIndex:        "1",
		fromTokenAddress:  "0xfrom",
		toTokenAddress:    "0xto",
		amount:            "1000",
		swapMode:          "exactIn",
		slippage:          "0.005",
		userWalletAddress: "0xwallet",
	}

	values := params.values()
	if got := values.Get("slippagePercent"); got != "0.5" {
		t.Errorf("slippagePercent = %q, want 0.5", got)
	}
	if got := values.Get("slippage"); got != "" {
		t.Errorf("slippage = %q, want it to be omitted", got)
	}
}

func TestSlippageToPercent(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "0.005", want: "0.5"},
		{in: "0.5", want: "50"},
		{in: "0.0005", want: "0.05"},
		{in: "1", want: "100"},
		{in: "0", want: "0"},
	}
	for _, tt := range tests {
		if got := slippageToPercent(tt.in); got != tt.want {
			t.Errorf("slippageToPercent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
