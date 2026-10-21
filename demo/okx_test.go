package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSign checks the signature implementation against an independently computed
// expected value. The expected value was produced with openssl:
//
//	printf '%s' '<timestamp><METHOD><path>' | openssl dgst -sha256 -hmac 'test-secret' -binary | base64
func TestSign(t *testing.T) {
	client := &okxClient{secretKey: "test-secret"}

	const (
		timestamp   = "2026-01-01T00:00:00.000Z"
		method      = http.MethodGet
		requestPath = "/api/v6/dex/aggregator/supported/chain?chainIndex=1"
		want        = "OuZ5YMSjM3ubl/1hoP7R+dCN8/NacOQn7wwL/ntvK7s="
	)

	if got := client.sign(timestamp, method, requestPath, ""); got != want {
		t.Errorf("sign() = %q, want %q", got, want)
	}
}

// stubTransport returns canned responses without listening on a port, so the tests
// also run in restricted environments.
type stubTransport struct {
	handle func(*http.Request) *http.Response
}

func (s stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.handle(req), nil
}

func stubClient(handle func(*http.Request) *http.Response) *okxClient {
	return &okxClient{
		http:       &http.Client{Transport: stubTransport{handle: handle}},
		baseURL:    "https://stub.invalid",
		apiKey:     "test-key",
		secretKey:  "test-secret",
		passphrase: "test-passphrase",
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestFetchSendsSignedRequest verifies that the headers are present, that the signature
// covers the request actually sent, and that data is decoded when the business code is 0.
func TestFetchSendsSignedRequest(t *testing.T) {
	var (
		gotPath   string
		gotQuery  string
		gotHeader http.Header
	)

	client := stubClient(func(r *http.Request) *http.Response {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Clone()
		return jsonResponse(`{"code":"0","msg":"","data":[{"chainIndex":"1","chainName":"Ethereum","dexTokenApproveAddress":"0x3b3ae790Df4F312e745D270119c6052904FB6790"}]}`)
	})

	chains, err := client.supportedChains(t.Context(), "1")
	if err != nil {
		t.Fatalf("supportedChains() returned an error: %v", err)
	}
	if len(chains) != 1 || chains[0].ChainName != "Ethereum" {
		t.Fatalf("supportedChains() = %+v, want one Ethereum entry", chains)
	}
	if want := "/api/v6/dex/aggregator/supported/chain"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
	if want := "chainIndex=1"; gotQuery != want {
		t.Errorf("query string = %q, want %q", gotQuery, want)
	}

	for _, header := range []string{"OK-ACCESS-KEY", "OK-ACCESS-SIGN", "OK-ACCESS-TIMESTAMP", "OK-ACCESS-PASSPHRASE"} {
		if gotHeader.Get(header) == "" {
			t.Errorf("request is missing the %s header", header)
		}
	}

	// The signature must cover the path actually sent, including the query string,
	// otherwise OKX rejects it as a bad signature.
	want := client.sign(gotHeader.Get("OK-ACCESS-TIMESTAMP"), http.MethodGet, gotPath+"?"+gotQuery, "")
	if got := gotHeader.Get("OK-ACCESS-SIGN"); got != want {
		t.Errorf("signature = %q, want %q (the signed payload must include the query string)", got, want)
	}
}

// TestFetchBusinessError verifies that a non-zero business code returns an error
// instead of silently succeeding.
func TestFetchBusinessError(t *testing.T) {
	client := stubClient(func(*http.Request) *http.Response {
		return jsonResponse(`{"code":"50114","msg":"Invalid Authority","data":[]}`)
	})

	_, err := client.fetch[chainInfo](t.Context(), http.MethodGet, "/api/v6/dex/aggregator/supported/chain", url.Values{}, nil)
	if err == nil {
		t.Fatal("expected a business error, got nil")
	}
	if !strings.Contains(err.Error(), "50114") {
		t.Errorf("error = %q, want it to contain the business code 50114", err)
	}
}
