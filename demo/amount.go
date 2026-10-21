package main

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// parseAmount converts a human-readable amount such as "0.01" into the smallest
// unit used on chain. It only uses integers and strings, so no precision is lost
// to floating point.
func parseAmount(human string, decimals int) (*big.Int, error) {
	if decimals < 0 {
		return nil, fmt.Errorf("token decimals must be >= 0, got %d", decimals)
	}
	s := strings.TrimSpace(human)
	if s == "" {
		return nil, errors.New("amount must not be empty")
	}

	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > decimals {
		return nil, fmt.Errorf("amount %s has more decimal places than the token supports (%d)", human, decimals)
	}
	fracPart += strings.Repeat("0", decimals-len(fracPart))

	value, ok := new(big.Int).SetString(intPart+fracPart, 10)
	if !ok {
		return nil, fmt.Errorf("cannot parse amount %q", human)
	}
	if negative {
		value.Neg(value)
	}
	return value, nil
}

// formatAmount renders the smallest unit back into a human-readable string for logs.
func formatAmount(atomic *big.Int, decimals int) string {
	if atomic == nil {
		return ""
	}
	if decimals <= 0 {
		return atomic.String()
	}

	s := atomic.String()
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if len(s) <= decimals {
		s = strings.Repeat("0", decimals-len(s)+1) + s
	}

	intPart := s[:len(s)-decimals]
	fracPart := strings.TrimRight(s[len(s)-decimals:], "0")

	out := intPart
	if fracPart != "" {
		out += "." + fracPart
	}
	if negative {
		out = "-" + out
	}
	return out
}

// parseBig parses a base-10 string and returns nil for an empty or invalid value.
func parseBig(s string) *big.Int {
	if s == "" {
		return nil
	}
	value, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil
	}
	return value
}

// slippageToPercent converts a fraction such as "0.005" (0.5%) into the percent
// value expected by OKX, such as "0.5". big.Rat keeps the conversion exact.
func slippageToPercent(fraction string) string {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(fraction))
	if !ok {
		return fraction
	}
	r.Mul(r, big.NewRat(100, 1))
	s := r.FloatString(18)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}
