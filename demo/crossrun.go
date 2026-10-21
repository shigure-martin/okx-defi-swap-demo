package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// runCross implements the cross-chain path. OKX only builds the source-chain
// transaction; the destination leg is executed by the bridge and must be
// monitored separately.
func runCross(ctx context.Context, cfg *config, logger *slog.Logger) error {
	wallet, key, err := resolveWallet(cfg)
	if err != nil {
		return err
	}
	if cfg.receiveAddress == "" {
		cfg.receiveAddress = wallet.Hex()
	}

	okx := newOKXClient(cfg)
	logger.Info("starting cross-chain",
		"stage", cfg.stage.String(),
		"fromChainIndex", cfg.chainIndex,
		"toChainIndex", cfg.toChainIndex,
		"wallet", wallet.Hex(),
		"receiveAddress", cfg.receiveAddress,
	)

	srcChain, err := fetchChain(ctx, okx, cfg.chainIndex)
	if err != nil {
		return err
	}
	logger.Info("source chain", "chainName", srcChain.ChainName, "chainId", srcChain.ChainID)

	from, err := resolveSourceToken(ctx, okx, cfg)
	if err != nil {
		return err
	}
	amount, err := parseAmount(cfg.amount, from.decimals)
	if err != nil {
		return err
	}
	logger.Info("sell amount",
		"human", cfg.amount,
		"atomic", amount.String(),
		"symbol", from.symbol,
		"decimals", from.decimals,
	)

	quoteParams := crossQuoteParams{
		fromChainIndex:   cfg.chainIndex,
		toChainIndex:     cfg.toChainIndex,
		fromChainID:      cfg.fromChainID,
		toChainID:        cfg.toChainID,
		fromTokenAddress: cfg.fromToken,
		toTokenAddress:   cfg.toToken,
		amount:           amount.String(),
		slippage:         cfg.slippage,
		allowBridge:      cfg.allowBridge,
		denyBridge:       cfg.denyBridge,
	}

	var (
		quote     crossQuoteData
		haveQuote bool
		router    crossRouter
	)
	toDecimals := cfg.toDecimals
	if toDecimals < 0 {
		toDecimals = 18
	}

	quotes, err := okx.crossQuote(ctx, quoteParams)
	switch {
	case err != nil:
		if !isLiquidityError(err) || cfg.stage == stageQuote {
			return fmt.Errorf("get cross-chain quote: %w", err)
		}
		logger.Warn("cross-chain quote unavailable; falling back to swap data", "err", err)
	case len(quotes) == 0 || len(quotes[0].RouterList) == 0:
		if cfg.stage == stageQuote {
			return errors.New("OKX returned no cross-chain quote")
		}
		logger.Warn("cross-chain quote contains no bridge route; falling back to swap data")
	default:
		quote = quotes[0]
		haveQuote = true
		router = quote.RouterList[0]
		toDecimals = crossDecimals(quote.ToToken, toDecimals)
		logCrossQuote(logger, quote, router, toDecimals)
	}

	if cfg.stage == stageQuote {
		if !haveQuote {
			return errors.New("OKX returned no cross-chain quote")
		}
		return nil
	}

	bridgeID := ""
	if haveQuote {
		bridgeID = string(router.BridgeID)
	}
	swapParams := crossSwapParams{
		crossQuoteParams:  quoteParams,
		userWalletAddress: wallet.Hex(),
		receiveAddress:    cfg.receiveAddress,
		bridgeID:          bridgeID,
		onlyBridge:        cfg.onlyBridge,
	}
	swaps, err := okx.crossSwap(ctx, swapParams)
	if err != nil && bridgeID != "" && isLiquidityError(err) {
		logger.Warn("quoted bridge has insufficient liquidity; retrying with automatic route selection", "bridge", router.BridgeID, "bridgeName", router.BridgeName)
		swapParams.bridgeID = ""
		swaps, err = okx.crossSwap(ctx, swapParams)
	}
	if err != nil {
		return fmt.Errorf("get cross-chain swap data: %w", err)
	}
	if len(swaps) == 0 {
		return errors.New("OKX returned no cross-chain swap data")
	}
	swap := swaps[0]
	if len(swap.Tx.RandomKeyAccount) > 0 || len(swap.Tx.SignatureData) > 0 {
		return errors.New("this cross-chain route requires extra signing data; not supported by this demo")
	}
	if swap.Tx.From != "" && !strings.EqualFold(swap.Tx.From, wallet.Hex()) {
		return fmt.Errorf("OKX returned a transaction for %s, expected %s", swap.Tx.From, wallet.Hex())
	}
	if !haveQuote {
		logger.Warn("destination token metadata unavailable from quote; using --to-decimals", "decimals", toDecimals)
	}
	logCrossSwap(logger, swap, toDecimals)

	if cfg.stage == stageSwapData {
		return nil
	}

	evm, err := newEVMClient(ctx, cfg.rpcURL, key, wallet)
	if err != nil {
		return err
	}
	defer evm.close()
	if err := checkRPCChain(srcChain, evm); err != nil {
		return err
	}
	if err := ensureSourceBalance(ctx, evm, swap.Tx); err != nil {
		if cfg.stage == stageExecute {
			return err
		}
		logger.Warn("source balance may be insufficient", "err", err)
	}

	var approval *crossApproveData
	if swap.Router.NeedApprove {
		if swap.Router.NeedCancelApprove {
			return errors.New("this route requires a zero-approval reset first; not supported by this demo")
		}
		approvals, err := okx.crossApprove(ctx, crossApproveParams{
			chainIndex:           cfg.chainIndex,
			tokenContractAddress: cfg.fromToken,
			approveAmount:        string(swap.FromTokenAmount),
			userWalletAddress:    wallet.Hex(),
			bridgeID:             string(swap.Router.BridgeID),
		})
		if err != nil {
			return fmt.Errorf("get cross-chain approval: %w", err)
		}
		if len(approvals) == 0 {
			return errors.New("OKX returned no cross-chain approval")
		}
		approval = &approvals[0]
		if !approval.NeedApprove {
			approval = nil
		}
	}

	if cfg.stage == stageSign {
		return signCross(ctx, evm, approval, swap, logger)
	}

	return executeCross(ctx, cfg, okx, evm, approval, swap, logger)
}

// resolveSourceToken resolves only the source token. The destination token may
// not exist on the source chain, so its metadata comes from the cross-chain quote.
func resolveSourceToken(ctx context.Context, okx *okxClient, cfg *config) (tokenRef, error) {
	ref := tokenRef{address: cfg.fromToken, symbol: cfg.fromToken, decimals: cfg.fromDecimals}
	if ref.decimals >= 0 {
		return ref, nil
	}

	tokens, err := okx.tokens(ctx, cfg.chainIndex)
	if err != nil {
		return ref, fmt.Errorf("get source token list: %w", err)
	}
	info, found := findToken(tokens, ref.address)
	if !found {
		return ref, fmt.Errorf("token %s is not in the OKX token list; pass --from-decimals", ref.address)
	}
	ref.symbol = info.TokenSymbol
	decimals, err := strconv.Atoi(string(info.Decimals))
	if err != nil {
		return ref, fmt.Errorf("cannot parse decimals %q of token %s", info.Decimals, ref.address)
	}
	ref.decimals = decimals
	return ref, nil
}

func logCrossQuote(logger *slog.Logger, quote crossQuoteData, router crossRouter, toDecimals int) {
	logger.Info("cross-chain quote",
		"fromChainIndex", quote.FromChainIndex,
		"toChainIndex", quote.ToChainIndex,
		"fromAmount", quote.FromTokenAmount,
		"toAmount", router.ToTokenAmount,
		"toAmountHuman", formatAmount(parseBig(string(router.ToTokenAmount)), toDecimals),
		"minimumReceived", router.MinimumReceived,
		"bridge", router.BridgeName,
		"bridgeId", router.BridgeID,
		"estimateTimeSeconds", router.EstimateTime,
		"otherNativeFee", router.OtherNativeFee,
		"needApprove", router.NeedApprove,
		"warning", router.WarningMessage,
	)
}

func logCrossSwap(logger *slog.Logger, swap crossSwapData, toDecimals int) {
	logger.Info("unsigned cross-chain source transaction",
		"to", swap.Tx.To,
		"value", swap.Tx.Value,
		"gasLimit", swap.Tx.GasLimit,
		"gasPrice", swap.Tx.GasPrice,
		"maxPriorityFeePerGas", swap.Tx.MaxPriorityFeePerGas,
		"minimumReceived", swap.Router.MinimumReceived,
		"toAmountHuman", formatAmount(parseBig(string(swap.ToTokenAmount)), toDecimals),
		"bridge", swap.Router.BridgeName,
		"bridgeId", swap.Router.BridgeID,
		"otherNativeFee", swap.Router.OtherNativeFee,
	)
}

func ensureSourceBalance(ctx context.Context, evm *evmClient, tx crossTx) error {
	balance, err := callHexBig(ctx, evm.rpc, "eth_getBalance", evm.address, "latest")
	if err != nil {
		return fmt.Errorf("read source balance: %w", err)
	}
	value := parseBig(string(tx.Value))
	if value == nil {
		value = new(big.Int)
	}
	gasLimit, err := strconv.ParseUint(string(tx.GasLimit), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid gasLimit %q", tx.GasLimit)
	}
	gasPrice := parseBig(string(tx.GasPrice))
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), gasPrice)
	required := new(big.Int).Add(value, gasCost)
	if balance.Cmp(required) < 0 {
		return fmt.Errorf("insufficient source balance: need %s wei (value %s + max gas %s), have %s wei", required, value, gasCost, balance)
	}
	return nil
}

func isLiquidityError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "82000") || strings.Contains(err.Error(), "Insufficient liquidity"))
}

func signCross(ctx context.Context, evm *evmClient, approval *crossApproveData, swap crossSwapData, logger *slog.Logger) error {
	if approval != nil {
		nonce, err := evm.nonce(ctx)
		if err != nil {
			return err
		}
		req, err := evm.newTxRequest(ctx, approval.Tx.To, approval.Tx.Data, "0", "0", "", "")
		if err != nil {
			return err
		}
		req.nonce = &nonce
		signed, err := evm.signTx(ctx, req)
		if err != nil {
			return fmt.Errorf("sign cross-chain approval: %w", err)
		}
		raw, err := encodeSignedTx(signed)
		if err != nil {
			return err
		}
		logger.Info("locally signed cross-chain approval (not broadcast)",
			"txHash", signed.Hash().Hex(),
			"nonce", signed.Nonce(),
			"gas", signed.Gas(),
			"spender", approval.ApproveAddress,
			"rawTx", raw,
		)

		next := nonce + 1
		return signCrossSwap(ctx, evm, swap, &next, logger)
	}
	return signCrossSwap(ctx, evm, swap, nil, logger)
}

func signCrossSwap(ctx context.Context, evm *evmClient, swap crossSwapData, nonce *uint64, logger *slog.Logger) error {
	req, err := evm.newTxRequest(ctx,
		swap.Tx.To,
		swap.Tx.Data,
		string(swap.Tx.Value),
		string(swap.Tx.GasLimit),
		string(swap.Tx.GasPrice),
		string(swap.Tx.MaxPriorityFeePerGas),
	)
	if err != nil {
		return err
	}
	req.nonce = nonce
	signed, err := evm.signTx(ctx, req)
	if err != nil {
		return fmt.Errorf("sign cross-chain swap: %w", err)
	}
	raw, err := encodeSignedTx(signed)
	if err != nil {
		return err
	}
	logger.Info("locally signed cross-chain swap (not broadcast)",
		"txHash", signed.Hash().Hex(),
		"nonce", signed.Nonce(),
		"gas", signed.Gas(),
		"to", swap.Tx.To,
		"value", swap.Tx.Value,
		"rawTx", raw,
	)
	return nil
}

func executeCross(ctx context.Context, cfg *config, okx *okxClient, evm *evmClient, approval *crossApproveData, swap crossSwapData, logger *slog.Logger) error {
	if approval != nil {
		if err := sendCrossApprove(ctx, cfg, evm, approval, logger); err != nil {
			return err
		}
	}

	hash, err := signAndBroadcastCross(ctx, cfg, okx, evm, swap, logger)
	if err != nil {
		return err
	}
	receipt, err := evm.waitReceipt(ctx, hash, cfg.receiptTimeout)
	if err != nil {
		return fmt.Errorf("%w; check the source-chain status by txHash before resubmitting", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("source transaction was mined but reverted, txHash=%s", hash.Hex())
	}
	logger.Info("source transaction confirmed",
		"txHash", hash.Hex(),
		"block", receipt.BlockNumber.String(),
		"gasUsed", receipt.GasUsed,
	)
	logger.Info("cross-chain submitted; source confirmation does not mean destination completion",
		"bridge", swap.Router.BridgeName,
		"estimateTimeSeconds", swap.Router.EstimateTime,
	)

	if cfg.watchReceive {
		return watchDestination(ctx, cfg, swap, logger)
	}
	return nil
}

func sendCrossApprove(ctx context.Context, cfg *config, evm *evmClient, approval *crossApproveData, logger *slog.Logger) error {
	req, err := evm.newTxRequest(ctx, approval.Tx.To, approval.Tx.Data, "0", "0", "", "")
	if err != nil {
		return err
	}
	signed, err := evm.signTx(ctx, req)
	if err != nil {
		return fmt.Errorf("sign cross-chain approval: %w", err)
	}
	logger.Info("sending cross-chain approval", "spender", approval.ApproveAddress, "txHash", signed.Hash().Hex())
	hash, err := evm.send(ctx, signed)
	if err != nil {
		return err
	}
	receipt, err := evm.waitReceipt(ctx, hash, cfg.receiptTimeout)
	if err != nil {
		return fmt.Errorf("wait for the approval receipt: %w; check the status by txHash first", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("cross-chain approval reverted, txHash=%s", hash.Hex())
	}
	logger.Info("cross-chain approval confirmed", "txHash", hash.Hex())
	return nil
}

func signAndBroadcastCross(ctx context.Context, cfg *config, okx *okxClient, evm *evmClient, swap crossSwapData, logger *slog.Logger) (common.Hash, error) {
	req, err := evm.newTxRequest(ctx,
		swap.Tx.To,
		swap.Tx.Data,
		string(swap.Tx.Value),
		string(swap.Tx.GasLimit),
		string(swap.Tx.GasPrice),
		string(swap.Tx.MaxPriorityFeePerGas),
	)
	if err != nil {
		return common.Hash{}, err
	}
	signed, err := evm.signTx(ctx, req)
	if err != nil {
		return common.Hash{}, err
	}
	raw, err := encodeSignedTx(signed)
	if err != nil {
		return common.Hash{}, err
	}
	logger.Info("locally signed cross-chain swap",
		"txHash", signed.Hash().Hex(),
		"nonce", signed.Nonce(),
		"gas", signed.Gas(),
		"to", swap.Tx.To,
		"value", swap.Tx.Value,
	)

	if cfg.broadcastVia != "okx" {
		hash, err := evm.send(ctx, signed)
		if err != nil {
			return common.Hash{}, err
		}
		logger.Info("broadcast cross-chain source transaction via own RPC", "txHash", hash.Hex())
		return hash, nil
	}

	results, err := okx.broadcastTransaction(ctx, raw, cfg.chainIndex, evm.address.Hex())
	if err != nil {
		return common.Hash{}, fmt.Errorf("OKX broadcast: %w", err)
	}
	if len(results) == 0 {
		return common.Hash{}, errors.New("OKX broadcast returned no result")
	}
	logger.Info("broadcast cross-chain source transaction via OKX", "orderId", results[0].OrderID, "txHash", results[0].TxHash)
	return signed.Hash(), nil
}

// watchDestination polls the destination wallet balance because OKX does not
// expose a cross-chain status endpoint. A timeout is a warning, not a failure.
func watchDestination(ctx context.Context, cfg *config, swap crossSwapData, logger *slog.Logger) error {
	client, err := rpc.DialContext(ctx, cfg.toRPCURL)
	if err != nil {
		return fmt.Errorf("connect to destination RPC: %w", err)
	}
	defer client.Close()

	wantID, ok := new(big.Int).SetString(cfg.toChainID, 10)
	if !ok {
		return fmt.Errorf("invalid --to-chain-id %q", cfg.toChainID)
	}
	gotID, err := callHexBig(ctx, client, "eth_chainId")
	if err != nil {
		return fmt.Errorf("read destination chain ID: %w", err)
	}
	if gotID.Cmp(wantID) != 0 {
		return fmt.Errorf("destination RPC chain ID %s does not match --to-chain-id %s", gotID, wantID)
	}

	start, err := destinationBalance(ctx, client, cfg.toToken, cfg.receiveAddress)
	if err != nil {
		return err
	}
	minimum := parseBig(string(swap.Router.MinimumReceived))
	if minimum == nil {
		minimum = parseBig(string(swap.MinimumReceived))
	}
	if minimum == nil {
		minimum = big.NewInt(1)
	}
	logger.Info("watching destination balance",
		"rpc", cfg.toRPCURL,
		"token", cfg.toToken,
		"address", cfg.receiveAddress,
		"initial", start.String(),
		"minimumExpectedDelta", minimum.String(),
	)

	deadline := time.Now().Add(cfg.watchTimeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastLog := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			current, err := destinationBalance(ctx, client, cfg.toToken, cfg.receiveAddress)
			if err != nil {
				logger.Warn("destination balance query failed", "err", err)
				continue
			}
			delta := new(big.Int).Sub(current, start)
			if minimum != nil && delta.Cmp(minimum) >= 0 {
				logger.Info("destination balance updated", "delta", delta.String(), "current", current.String())
				return nil
			}
			if time.Since(lastLog) >= 30*time.Second {
				logger.Info("waiting for destination balance", "delta", delta.String())
				lastLog = time.Now()
			}
			if time.Now().After(deadline) {
				logger.Warn("destination not confirmed before timeout", "delta", delta.String(), "timeout", cfg.watchTimeout)
				return nil
			}
		}
	}
}

func destinationBalance(ctx context.Context, client *rpc.Client, token, address string) (*big.Int, error) {
	addr := common.HexToAddress(address)
	if isNative(token) {
		return callHexBig(ctx, client, "eth_getBalance", addr, "latest")
	}
	tokenAddr := common.HexToAddress(token)
	data := "0x70a08231" + hexutil.Encode(common.LeftPadBytes(addr.Bytes(), 32))[2:]
	return callHexBig(ctx, client, "eth_call", rpcCall{To: &tokenAddr, Data: data}, "latest")
}
