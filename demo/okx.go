package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	okxBaseURL = "https://web3.okx.com"

	// Aggregator endpoints (quote/swap/approve/history) moved to v6; the v5 paths still work today.
	aggregatorPath = "/api/v6/dex/aggregator"
	// Onchain gateway endpoints (simulate/broadcast) are still on v5.
	gatewayPath = "/api/v5/dex/pre-transaction"
)

type okxClient struct {
	http       *http.Client
	baseURL    string
	apiKey     string
	secretKey  string
	passphrase string
	projectID  string
}

func newOKXClient(cfg *config) *okxClient {
	return &okxClient{
		http:       &http.Client{Timeout: 30 * time.Second},
		baseURL:    okxBaseURL,
		apiKey:     cfg.apiKey,
		secretKey:  cfg.secretKey,
		passphrase: cfg.passphrase,
		projectID:  cfg.projectID,
	}
}

// do performs a single OKX request. The GET query string is part of the signature,
// so the signed requestPath and the URL actually sent must come from the same value.
func (c *okxClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	requestPath := path
	if len(query) > 0 {
		requestPath += "?" + query.Encode()
	}

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
	}

	// Signature payload = timestamp + HTTP_METHOD + requestPath + body,
	// signed with HMAC-SHA256 and Base64-encoded.
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	signature := c.sign(timestamp, method, requestPath, string(payload))

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+requestPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)
	req.Header.Set("Content-Type", "application/json")
	if c.projectID != "" {
		req.Header.Set("OK-ACCESS-PROJECT", c.projectID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s returned HTTP %d: %s", method, path, resp.StatusCode, truncate(string(raw), 300))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s response: %w (body=%s)", path, err, truncate(string(raw), 300))
	}
	return nil
}

func (c *okxClient) sign(timestamp, method, requestPath, body string) string {
	mac := hmac.New(sha256.New, []byte(c.secretKey))
	mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// fetch calls an OKX endpoint, validates the business code and returns the data array.
func (c *okxClient) fetch[T any](ctx context.Context, method, path string, query url.Values, body any) ([]T, error) {
	var envelope struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []T    `json:"data"`
	}
	if err := c.do(ctx, method, path, query, body, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != "0" {
		return nil, fmt.Errorf("OKX business error (%s): code=%s msg=%s", path, envelope.Code, envelope.Msg)
	}
	return envelope.Data, nil
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
