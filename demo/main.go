package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func main() {
	err := run(os.Args[1:])
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	slog.Error("run failed", "err", err)
	os.Exit(1)
}

// run walks one single-chain DEX swap end to end:
// quote -> unsigned swap tx -> local signing -> broadcast -> receipt -> cross-check.
func run(args []string) error {
	cfg, err := loadConfig(args)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if cfg.crossChain {
		return runCross(ctx, cfg, logger)
	}

	wallet, key, err := resolveWallet(cfg)
	if err != nil {
		return err
	}
	okx := newOKXClient(cfg)
	logger.Info("starting", "stage", cfg.stage.String(), "chainIndex", cfg.chainIndex, "wallet", wallet.Hex())

	chain, err := fetchChain(ctx, okx, cfg.chainIndex)
	if err != nil {
		return err
	}
	logger.Info("chain info", "chainName", chain.ChainName, "approveSpender", chain.DexTokenApproveAddress)

	from, to, err := resolveTokens(ctx, okx, cfg)
	if err != nil {
		return err
	}

	// With exactIn the amount is what we sell (sell-token decimals).
	// With exactOut the amount is what we buy (buy-token decimals).
	amountDecimals := from.decimals
	if cfg.swapMode == "exactOut" {
		amountDecimals = to.decimals
	}
	amount, err := parseAmount(cfg.amount, amountDecimals)
	if err != nil {
		return err
	}
	logger.Info("sell amount",
		"human", cfg.amount,
		"atomic", amount.String(),
		"symbol", from.symbol,
		"decimals", amountDecimals,
		"swapMode", cfg.swapMode,
	)

	quotes, err := okx.quote(ctx, quoteParams{
		chainIndex:       cfg.chainIndex,
		fromTokenAddress: cfg.fromToken,
		toTokenAddress:   cfg.toToken,
		amount:           amount.String(),
		swapMode:         cfg.swapMode,
	})
	if err != nil {
		return fmt.Errorf("get quote: %w", err)
	}
	if len(quotes) == 0 {
		return errors.New("OKX returned no quote")
	}
	logQuote(logger, quotes[0], to.decimals)
	if cfg.stage == stageQuote {
		return nil
	}

	swaps, err := okx.swap(ctx, swapParams{
		chainIndex:        cfg.chainIndex,
		fromTokenAddress:  cfg.fromToken,
		toTokenAddress:    cfg.toToken,
		amount:            amount.String(),
		swapMode:          cfg.swapMode,
		slippage:          cfg.slippage,
		userWalletAddress: wallet.Hex(),
		gasLevel:          cfg.gasLevel,
	})
	if err != nil {
		return fmt.Errorf("get swap data: %w", err)
	}
	if len(swaps) == 0 {
		return errors.New("OKX returned no swap data")
	}
	swap := swaps[0]
	logSwap(logger, swap)
	if cfg.stage == stageSwapData {
		return nil
	}

	// From here on a node is required: allowance, nonce and receipts.
	evm, err := newEVMClient(ctx, cfg.rpcURL, key, wallet)
	if err != nil {
		return err
	}
	defer evm.close()
	if err := checkRPCChain(chain, evm); err != nil {
		return err
	}

	needsApprove, allowance, err := checkAllowance(ctx, evm, chain, from, amount)
	if err != nil {
		return err
	}
	if needsApprove {
		logger.Warn("allowance is insufficient", "current", allowance.String(), "required", amount.String())
	}

	if cfg.stage == stageSign {
		if needsApprove {
			return signApprovalAndSwap(ctx, cfg, okx, evm, from, amount, swap.Tx, logger)
		}
		return signAndPrint(ctx, cfg, evm, swap.Tx, nil, logger)
	}

	if needsApprove {
		if err := sendApprove(ctx, cfg, okx, evm, from, amount, logger); err != nil {
			return err
		}
	}

	hash, err := signAndBroadcast(ctx, cfg, okx, evm, swap.Tx, logger)
	if err != nil {
		return err
	}

	receipt, err := evm.waitReceipt(ctx, hash, cfg.receiptTimeout)
	if err != nil {
		return fmt.Errorf("%w; check the on-chain status by txHash first and do not resubmit before confirming", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("transaction was mined but reverted, txHash=%s", hash.Hex())
	}
	logger.Info("transaction confirmed",
		"txHash", hash.Hex(),
		"block", receipt.BlockNumber.String(),
		"gasUsed", receipt.GasUsed,
	)

	confirmViaOKX(ctx, okx, cfg, hash, logger)
	return nil
}

type tokenRef struct {
	address  string
	symbol   string
	decimals int
}

func fetchChain(ctx context.Context, okx *okxClient, chainIndex string) (chainInfo, error) {
	chains, err := okx.supportedChains(ctx, chainIndex)
	if err != nil {
		return chainInfo{}, fmt.Errorf("get supported chains: %w", err)
	}
	if len(chains) == 0 {
		return chainInfo{}, fmt.Errorf("OKX returned no chain info for chainIndex=%s", chainIndex)
	}
	return chains[0], nil
}

// resolveTokens fills in symbols and decimals. Explicit flags win, otherwise the
// OKX token list is used.
func resolveTokens(ctx context.Context, okx *okxClient, cfg *config) (tokenRef, tokenRef, error) {
	from := tokenRef{address: cfg.fromToken, symbol: cfg.fromToken, decimals: cfg.fromDecimals}
	to := tokenRef{address: cfg.toToken, symbol: cfg.toToken, decimals: cfg.toDecimals}
	if from.decimals >= 0 && to.decimals >= 0 {
		return from, to, nil
	}

	tokens, err := okx.tokens(ctx, cfg.chainIndex)
	if err != nil {
		return from, to, fmt.Errorf("get token list: %w", err)
	}
	for _, ref := range []*tokenRef{&from, &to} {
		info, found := findToken(tokens, ref.address)
		if !found {
			if ref.decimals < 0 {
				return from, to, fmt.Errorf("token %s is not in the OKX token list; pass --from-decimals / --to-decimals", ref.address)
			}
			continue
		}
		ref.symbol = info.TokenSymbol
		if ref.decimals >= 0 {
			continue
		}
		decimals, err := strconv.Atoi(string(info.Decimals))
		if err != nil {
			return from, to, fmt.Errorf("cannot parse decimals %q of token %s", info.Decimals, ref.address)
		}
		ref.decimals = decimals
	}
	return from, to, nil
}

func findToken(tokens []tokenInfo, address string) (tokenInfo, bool) {
	index := slices.IndexFunc(tokens, func(t tokenInfo) bool {
		return strings.EqualFold(t.TokenContractAddress, address)
	})
	if index < 0 {
		return tokenInfo{}, false
	}
	return tokens[index], true
}

func isNative(address string) bool {
	return strings.EqualFold(address, nativeTokenAddress)
}

// checkAllowance inspects the ERC-20 allowance; selling the native token needs no approval.
func checkAllowance(ctx context.Context, evm *evmClient, chain chainInfo, from tokenRef, amount *big.Int) (bool, *big.Int, error) {
	if isNative(from.address) {
		return false, nil, nil
	}
	if chain.DexTokenApproveAddress == "" {
		return false, nil, errors.New("OKX returned no approve contract for this chain, cannot check the allowance")
	}
	current, err := evm.allowance(ctx, common.HexToAddress(from.address), common.HexToAddress(chain.DexTokenApproveAddress))
	if err != nil {
		return false, nil, err
	}
	return current.Cmp(amount) < 0, current, nil
}

func sendApprove(ctx context.Context, cfg *config, okx *okxClient, evm *evmClient, from tokenRef, amount *big.Int, logger *slog.Logger) error {
	approves, err := okx.approveTransaction(ctx, cfg.chainIndex, from.address, amount.String())
	if err != nil {
		return fmt.Errorf("get approval transaction: %w", err)
	}
	if len(approves) == 0 {
		return errors.New("OKX returned no approval transaction")
	}
	approve := approves[0]

	// The approval calldata must be sent to the ERC-20 token contract. The
	// dexContractAddress field is the spender encoded inside that calldata.
	req, err := evm.newTxRequest(ctx, from.address, approve.Data, "0", string(approve.GasLimit), string(approve.GasPrice), "")
	if err != nil {
		return err
	}
	signed, err := evm.signTx(ctx, req)
	if err != nil {
		return err
	}

	logger.Info("sending approval transaction", "spender", approve.DexContractAddress, "amount", amount.String(), "txHash", signed.Hash().Hex())
	hash, err := evm.send(ctx, signed)
	if err != nil {
		return err
	}
	receipt, err := evm.waitReceipt(ctx, hash, cfg.receiptTimeout)
	if err != nil {
		return fmt.Errorf("wait for the approval receipt: %w; check the on-chain status by txHash first", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("approval transaction reverted, txHash=%s", hash.Hex())
	}
	logger.Info("approval confirmed", "txHash", hash.Hex())
	return nil
}

func signAndPrint(ctx context.Context, cfg *config, evm *evmClient, data swapTx, nonce *uint64, logger *slog.Logger) error {
	signed, err := signSwapTx(ctx, evm, data, nonce)
	if err != nil {
		return err
	}
	raw, err := encodeSignedTx(signed)
	if err != nil {
		return err
	}
	logger.Info("locally signed (not broadcast)",
		"txHash", signed.Hash().Hex(),
		"nonce", signed.Nonce(),
		"gas", signed.Gas(),
		"rawTx", raw,
	)
	return nil
}

// signApprovalAndSwap builds and signs both transactions needed for an ERC-20
// sell: the exact-amount approval first, then the swap with the next nonce.
// Nothing is broadcast.
func signApprovalAndSwap(ctx context.Context, cfg *config, okx *okxClient, evm *evmClient, from tokenRef, amount *big.Int, data swapTx, logger *slog.Logger) error {
	approves, err := okx.approveTransaction(ctx, cfg.chainIndex, from.address, amount.String())
	if err != nil {
		return fmt.Errorf("get approval transaction: %w", err)
	}
	if len(approves) == 0 {
		return errors.New("OKX returned no approval transaction")
	}
	approve := approves[0]

	req, err := evm.newTxRequest(ctx, from.address, approve.Data, "0", string(approve.GasLimit), string(approve.GasPrice), "")
	if err != nil {
		return err
	}
	nonce, err := evm.nonce(ctx)
	if err != nil {
		return err
	}
	req.nonce = &nonce

	signed, err := evm.signTx(ctx, req)
	if err != nil {
		return fmt.Errorf("sign approval: %w", err)
	}
	raw, err := encodeSignedTx(signed)
	if err != nil {
		return err
	}
	logger.Info("locally signed approval (not broadcast)",
		"txHash", signed.Hash().Hex(),
		"nonce", signed.Nonce(),
		"gas", signed.Gas(),
		"spender", approve.DexContractAddress,
		"amount", amount.String(),
		"rawTx", raw,
	)

	next := nonce + 1
	return signAndPrint(ctx, cfg, evm, data, &next, logger)
}

func signAndBroadcast(ctx context.Context, cfg *config, okx *okxClient, evm *evmClient, data swapTx, logger *slog.Logger) (common.Hash, error) {
	signed, err := signSwapTx(ctx, evm, data, nil)
	if err != nil {
		return common.Hash{}, err
	}
	raw, err := encodeSignedTx(signed)
	if err != nil {
		return common.Hash{}, err
	}
	logger.Info("locally signed",
		"txHash", signed.Hash().Hex(),
		"nonce", signed.Nonce(),
		"gas", signed.Gas(),
		"to", data.To,
		"value", data.Value,
	)

	if cfg.broadcastVia != "okx" {
		hash, err := evm.send(ctx, signed)
		if err != nil {
			return common.Hash{}, err
		}
		logger.Info("broadcast via own RPC", "txHash", hash.Hex())
		return hash, nil
	}

	results, err := okx.broadcastTransaction(ctx, raw, cfg.chainIndex, evm.address.Hex())
	if err != nil {
		return common.Hash{}, fmt.Errorf("OKX broadcast: %w", err)
	}
	if len(results) == 0 {
		return common.Hash{}, errors.New("OKX broadcast returned no result")
	}
	logger.Info("broadcast via OKX", "orderId", results[0].OrderID, "txHash", results[0].TxHash)
	return signed.Hash(), nil
}

func signSwapTx(ctx context.Context, evm *evmClient, data swapTx, nonce *uint64) (*types.Transaction, error) {
	req, err := evm.newTxRequest(ctx, data.To, data.Data, string(data.Value), string(data.Gas), string(data.GasPrice), string(data.MaxPriorityFeePerGas))
	if err != nil {
		return nil, err
	}
	req.nonce = nonce
	return evm.signTx(ctx, req)
}

// confirmViaOKX cross-checks the status through OKX. A failure here is not fatal.
func confirmViaOKX(ctx context.Context, okx *okxClient, cfg *config, hash common.Hash, logger *slog.Logger) {
	history, err := okx.history(ctx, cfg.chainIndex, cfg.chainIndex, hash.Hex())
	if err != nil {
		logger.Warn("failed to query the OKX transaction status (ignored)", "err", err)
		return
	}
	if len(history) == 0 {
		logger.Info("OKX has not indexed this transaction yet, try again later")
		return
	}
	logger.Info("OKX transaction status",
		"status", history[0].Status,
		"txType", history[0].TxType,
		"height", history[0].Height,
		"txTime", history[0].TxTime,
	)
}

func logQuote(logger *slog.Logger, quote quoteData, toDecimals int) {
	logger.Info("quote",
		"fromAmount", quote.FromTokenAmount,
		"toAmount", quote.ToTokenAmount,
		"toAmountHuman", formatAmount(parseBig(string(quote.ToTokenAmount)), toDecimals),
		"toSymbol", quote.ToToken.TokenSymbol,
		"gasFeeUSD", quote.TradeFee,
		"isHoneyPot", quote.ToToken.IsHoneyPot,
		"taxRate", quote.ToToken.TaxRate,
	)
	for _, router := range quote.DexRouterList {
		attrs := []any{"protocols", formatProtocols(router.DexProtocol)}
		if router.Router != "" {
			attrs = append(attrs, "router", router.Router)
		}
		if router.RouterPercent != "" {
			attrs = append(attrs, "percent", router.RouterPercent)
		}
		logger.Info("route", attrs...)
	}
}

func logSwap(logger *slog.Logger, data swapData) {
	logger.Info("unsigned swap transaction",
		"to", data.Tx.To,
		"value", data.Tx.Value,
		"gas", data.Tx.Gas,
		"gasPrice", data.Tx.GasPrice,
		"maxPriorityFeePerGas", data.Tx.MaxPriorityFeePerGas,
		"minReceiveAmount", data.Tx.MinReceiveAmount,
		"slippage", data.Tx.Slippage,
		"calldataBytes", len(data.Tx.Data)/2,
	)
}

func formatProtocols(protocols []dexProtocol) string {
	parts := make([]string, 0, len(protocols))
	for _, p := range protocols {
		parts = append(parts, fmt.Sprintf("%s(%s%%)", p.DexName, p.Percent))
	}
	return strings.Join(parts, ", ")
}
