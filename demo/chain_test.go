package main

import (
	"math/big"
	"testing"
)

func TestCheckRPCChain(t *testing.T) {
	evm := &evmClient{chainID: big.NewInt(42161)}

	tests := []struct {
		name    string
		chain   chainInfo
		wantErr bool
	}{
		{
			name:  "matching chain",
			chain: chainInfo{ChainIndex: "42161", ChainID: "42161"},
		},
		{
			name:    "mismatched chain",
			chain:   chainInfo{ChainIndex: "1", ChainID: "1"},
			wantErr: true,
		},
		{
			name:  "missing chain id from OKX",
			chain: chainInfo{ChainIndex: "42161"},
		},
		{
			name:    "invalid chain id from OKX",
			chain:   chainInfo{ChainIndex: "42161", ChainID: "abc"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRPCChain(tt.chain, evm)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkRPCChain() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
