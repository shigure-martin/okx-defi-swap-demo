package main

import (
	"math/big"
	"testing"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		name     string
		human    string
		decimals int
		want     string
		wantErr  bool
	}{
		{name: "integer", human: "1", decimals: 18, want: "1000000000000000000"},
		{name: "decimal", human: "0.01", decimals: 18, want: "10000000000000000"},
		{name: "six decimals", human: "1.5", decimals: 6, want: "1500000"},
		{name: "zero", human: "0", decimals: 6, want: "0"},
		{name: "leading dot", human: ".5", decimals: 2, want: "50"},
		{name: "zero decimals", human: "12", decimals: 0, want: "12"},
		{name: "too many decimals", human: "1.234", decimals: 2, wantErr: true},
		{name: "invalid input", human: "abc", decimals: 6, wantErr: true},
		{name: "empty input", human: "", decimals: 6, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmount(tt.human, tt.decimals)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseAmount(%q, %d): expected an error, got %v", tt.human, tt.decimals, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAmount(%q, %d) returned an error: %v", tt.human, tt.decimals, err)
			}
			if got.String() != tt.want {
				t.Errorf("parseAmount(%q, %d) = %s, want %s", tt.human, tt.decimals, got, tt.want)
			}
		})
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		atomic   string
		decimals int
		want     string
	}{
		{atomic: "1000000000000000000", decimals: 18, want: "1"},
		{atomic: "10000000000000000", decimals: 18, want: "0.01"},
		{atomic: "1500000", decimals: 6, want: "1.5"},
		{atomic: "0", decimals: 6, want: "0"},
		{atomic: "1", decimals: 18, want: "0.000000000000000001"},
		{atomic: "12345", decimals: 0, want: "12345"},
	}

	for _, tt := range tests {
		value, ok := new(big.Int).SetString(tt.atomic, 10)
		if !ok {
			t.Fatalf("bad test data, cannot parse: %s", tt.atomic)
		}
		if got := formatAmount(value, tt.decimals); got != tt.want {
			t.Errorf("formatAmount(%s, %d) = %s, want %s", tt.atomic, tt.decimals, got, tt.want)
		}
	}
}
