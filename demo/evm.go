package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// nativeTokenAddress is the placeholder OKX uses for the native token (ETH, BNB, POL, ...).
const nativeTokenAddress = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

// allowanceSelector is keccak256("allowance(address,address)")[:4]. Hand-encoding it
// avoids pulling in ABI binding code for a single read-only call.
var allowanceSelector = []byte{0xdd, 0x62, 0xed, 0x3e}

// evmClient talks to the chain over plain JSON-RPC and avoids pulling in the full
// geth node stack.
type evmClient struct {
	rpc     *rpc.Client
	chainID *big.Int
	address common.Address
	key     *ecdsa.PrivateKey
}

func newEVMClient(ctx context.Context, rpcURL string, key *ecdsa.PrivateKey, address common.Address) (*evmClient, error) {
	if rpcURL == "" {
		return nil, errors.New("RPC_URL is required")
	}
	client, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect to RPC node: %w", err)
	}
	chainID, err := callHexBig(ctx, client, "eth_chainId")
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("read chainID: %w", err)
	}
	return &evmClient{rpc: client, chainID: chainID, address: address, key: key}, nil
}

func (e *evmClient) close() {
	e.rpc.Close()
}

// checkRPCChain verifies that the configured RPC serves the same chain as the
// OKX chainIndex used to build the transaction. Signing with the wrong RPC chain
// ID could execute calldata on an unintended network.
func checkRPCChain(chain chainInfo, evm *evmClient) error {
	if chain.ChainID == "" {
		return nil
	}
	want, ok := new(big.Int).SetString(string(chain.ChainID), 10)
	if !ok {
		return fmt.Errorf("invalid chainId %q returned by OKX", chain.ChainID)
	}
	if evm.chainID == nil || evm.chainID.Cmp(want) != 0 {
		return fmt.Errorf("RPC chain ID %v does not match OKX chain ID %s for chainIndex %s; check RPC_URL and --chain-index", evm.chainID, chain.ChainID, chain.ChainIndex)
	}
	return nil
}

// resolveWallet returns the wallet address and an optional private key. With a private
// key the address is derived, otherwise an explicit address is required.
func resolveWallet(cfg *config) (common.Address, *ecdsa.PrivateKey, error) {
	if cfg.privateKey != "" {
		key, err := parsePrivateKey(cfg.privateKey)
		if err != nil {
			return common.Address{}, nil, err
		}
		return crypto.PubkeyToAddress(key.PublicKey), key, nil
	}
	if cfg.walletAddress == "" {
		return common.Address{}, nil, errors.New("WALLET_PRIVATE_KEY or WALLET_ADDRESS must be set")
	}
	if !common.IsHexAddress(cfg.walletAddress) {
		return common.Address{}, nil, fmt.Errorf("invalid wallet address: %s", cfg.walletAddress)
	}
	return common.HexToAddress(cfg.walletAddress), nil, nil
}

func parsePrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return key, nil
}

// rpcCall is the transaction object used by eth_call and eth_estimateGas.
type rpcCall struct {
	From  *common.Address `json:"from,omitempty"`
	To    *common.Address `json:"to"`
	Value string          `json:"value,omitempty"`
	Data  string          `json:"data,omitempty"`
}

// callHexBig calls a JSON-RPC method that returns a hex quantity
// (eth_chainId, eth_call, eth_gasPrice, ...).
func callHexBig(ctx context.Context, client *rpc.Client, method string, args ...any) (*big.Int, error) {
	var raw string
	if err := client.CallContext(ctx, &raw, method, args...); err != nil {
		return nil, err
	}
	digits := strings.TrimPrefix(raw, "0x")
	if digits == "" {
		return new(big.Int), nil
	}
	value, ok := new(big.Int).SetString(digits, 16)
	if !ok {
		return nil, fmt.Errorf("cannot parse the %s response %q", method, raw)
	}
	return value, nil
}

// allowance reads the ERC-20 allowance via eth_call.
func (e *evmClient) allowance(ctx context.Context, token, spender common.Address) (*big.Int, error) {
	data := make([]byte, 0, 4+32+32)
	data = append(data, allowanceSelector...)
	data = append(data, common.LeftPadBytes(e.address.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(spender.Bytes(), 32)...)

	call := rpcCall{To: &token, Data: hexutil.Encode(data)}
	amount, err := callHexBig(ctx, e.rpc, "eth_call", call, "latest")
	if err != nil {
		return nil, fmt.Errorf("read allowance: %w", err)
	}
	return amount, nil
}

func (e *evmClient) nonce(ctx context.Context) (uint64, error) {
	var raw string
	if err := e.rpc.CallContext(ctx, &raw, "eth_getTransactionCount", e.address, "pending"); err != nil {
		return 0, fmt.Errorf("read nonce: %w", err)
	}
	nonce, err := strconv.ParseUint(strings.TrimPrefix(raw, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse nonce %q: %w", raw, err)
	}
	return nonce, nil
}

func (e *evmClient) estimateGas(ctx context.Context, call rpcCall) (uint64, error) {
	var raw string
	if err := e.rpc.CallContext(ctx, &raw, "eth_estimateGas", call); err != nil {
		return 0, err
	}
	limit, err := strconv.ParseUint(strings.TrimPrefix(raw, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse gasLimit %q: %w", raw, err)
	}
	return limit, nil
}

type txRequest struct {
	to        common.Address
	data      []byte
	value     *big.Int
	gasLimit  uint64
	gasPrice  *big.Int
	gasTipCap *big.Int // when set and non-zero, an EIP-1559 dynamic-fee transaction is used
	nonce     *uint64  // optional override; used when signing several transactions in a row
}

// newTxRequest turns the transaction fields returned by OKX into signing parameters.
// Missing gas fields fall back to RPC estimation.
func (e *evmClient) newTxRequest(ctx context.Context, to, data, value, gasLimit, gasPrice, gasTip string) (txRequest, error) {
	req := txRequest{
		to:        common.HexToAddress(to),
		data:      common.FromHex(data),
		value:     parseBig(value),
		gasPrice:  parseBig(gasPrice),
		gasTipCap: parseBig(gasTip),
	}
	if req.value == nil {
		req.value = new(big.Int)
	}
	if req.gasTipCap != nil && req.gasTipCap.Sign() == 0 {
		req.gasTipCap = nil
	}
	if limit, err := strconv.ParseUint(gasLimit, 10, 64); err == nil {
		req.gasLimit = limit
	}

	if req.gasPrice == nil || req.gasPrice.Sign() == 0 {
		price, err := callHexBig(ctx, e.rpc, "eth_gasPrice")
		if err != nil {
			return txRequest{}, fmt.Errorf("estimate gasPrice: %w", err)
		}
		req.gasPrice = price
	}
	if req.gasLimit == 0 {
		limit, err := e.estimateGas(ctx, rpcCall{
			From:  &e.address,
			To:    &req.to,
			Value: hexutil.EncodeBig(req.value),
			Data:  hexutil.Encode(req.data),
		})
		if err != nil {
			return txRequest{}, fmt.Errorf("estimate gasLimit: %w", err)
		}
		req.gasLimit = limit
	}
	return req, nil
}

func (e *evmClient) signTx(ctx context.Context, req txRequest) (*types.Transaction, error) {
	if e.key == nil {
		return nil, errors.New("a private key is required to sign")
	}
	nonce, err := e.nonce(ctx)
	if err != nil {
		return nil, err
	}
	if req.nonce != nil {
		nonce = *req.nonce
	}

	var tx *types.Transaction
	if req.gasTipCap != nil {
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   e.chainID,
			Nonce:     nonce,
			GasTipCap: req.gasTipCap,
			GasFeeCap: req.gasPrice,
			Gas:       req.gasLimit,
			To:        &req.to,
			Value:     req.value,
			Data:      req.data,
		})
	} else {
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: req.gasPrice,
			Gas:      req.gasLimit,
			To:       &req.to,
			Value:    req.value,
			Data:     req.data,
		})
	}

	signed, err := types.SignTx(tx, types.LatestSignerForChainID(e.chainID), e.key)
	if err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}
	return signed, nil
}

// encodeSignedTx returns the RLP-encoded raw transaction (0x-prefixed) for the OKX
// broadcast endpoint.
func encodeSignedTx(tx *types.Transaction) (string, error) {
	raw, err := tx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("RLP-encode transaction: %w", err)
	}
	return "0x" + hex.EncodeToString(raw), nil
}

func (e *evmClient) send(ctx context.Context, tx *types.Transaction) (common.Hash, error) {
	raw, err := tx.MarshalBinary()
	if err != nil {
		return common.Hash{}, fmt.Errorf("RLP-encode transaction: %w", err)
	}
	var hash common.Hash
	if err := e.rpc.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Encode(raw)); err != nil {
		return common.Hash{}, fmt.Errorf("broadcast transaction: %w", err)
	}
	return hash, nil
}

// waitReceipt polls for the receipt. A timeout does not mean the transaction failed:
// the caller must check the on-chain status by txHash before deciding to retry.
func (e *evmClient) waitReceipt(ctx context.Context, hash common.Hash, timeout time.Duration) (*types.Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		var receipt *types.Receipt
		if err := e.rpc.CallContext(ctx, &receipt, "eth_getTransactionReceipt", hash); err != nil {
			return nil, fmt.Errorf("query receipt: %w", err)
		}
		if receipt != nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for the receipt of %s: %w", hash.Hex(), ctx.Err())
		case <-ticker.C:
		}
	}
}
