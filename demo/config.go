package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// stage mirrors the staged verification plan: get the read-only path working first,
// then move on to signing and broadcasting.
type stage int

const (
	stageQuote    stage = iota // quote only
	stageSwapData              // also fetch the unsigned swap transaction
	stageSign                  // also sign locally, without broadcasting
	stageExecute               // also broadcast and wait for the receipt
)

var stageNames = []string{"quote", "swap-data", "sign", "execute"}

func (s stage) String() string {
	if s < 0 || int(s) >= len(stageNames) {
		return "unknown"
	}
	return stageNames[s]
}

// needsChain reports whether the stage needs a node (nonce, allowance and receipt).
func (s stage) needsChain() bool { return s >= stageSign }

func parseStage(name string) (stage, error) {
	for i, n := range stageNames {
		if n == name {
			return stage(i), nil
		}
	}
	return 0, fmt.Errorf("unknown stage %q, valid values: %s", name, strings.Join(stageNames, " | "))
}

type config struct {
	// OKX API credentials
	apiKey     string
	secretKey  string
	passphrase string
	projectID  string

	// Trade parameters
	chainIndex string
	fromToken  string
	toToken    string
	amount     string
	slippage   string
	gasLevel   string
	swapMode   string

	// Cross-chain parameters
	crossChain     bool
	fromChainID    string
	toChainIndex   string
	toChainID      string
	receiveAddress string
	allowBridge    string
	denyBridge     string
	onlyBridge     bool
	toRPCURL       string
	watchReceive   bool
	watchTimeout   time.Duration

	// Token decimals; -1 means "look them up in the OKX token list"
	fromDecimals int
	toDecimals   int

	// Wallet and broadcast
	privateKey    string
	walletAddress string
	rpcURL        string
	broadcastVia  string

	stage          stage
	envFile        string
	receiptTimeout time.Duration
}

func loadConfig(args []string) (*config, error) {
	cfg := &config{}
	var stageName string

	fs := flag.NewFlagSet("okx-swap-demo", flag.ContinueOnError)
	fs.StringVar(&cfg.envFile, "env-file", ".env", "path to an env file (ignored when missing; existing variables win)")
	fs.StringVar(&cfg.chainIndex, "chain-index", "1", "OKX chainIndex, e.g. 1=Ethereum, 56=BSC, 137=Polygon, 8453=Base")
	fs.StringVar(&cfg.fromToken, "from", "", "contract address of the token to sell (use "+nativeTokenAddress+" for the native token)")
	fs.StringVar(&cfg.toToken, "to", "", "contract address of the token to buy")
	fs.StringVar(&cfg.amount, "amount", "", "amount to sell, human readable, e.g. 0.001")
	fs.StringVar(&cfg.slippage, "slippage", "0.005", "slippage limit, 0.005 means 0.5%")
	fs.StringVar(&cfg.gasLevel, "gas-level", "average", "gas level: slow | average | fast")
	fs.StringVar(&cfg.swapMode, "swap-mode", "exactIn", "swap mode: exactIn | exactOut")
	fs.BoolVar(&cfg.crossChain, "cross-chain", false, "use the OKX cross-chain API instead of a single-chain swap")
	fs.StringVar(&cfg.fromChainID, "from-chain-id", "", "source EVM chain ID (defaults to --chain-index)")
	fs.StringVar(&cfg.toChainIndex, "to-chain-index", "", "destination OKX chainIndex (required with --cross-chain)")
	fs.StringVar(&cfg.toChainID, "to-chain-id", "", "destination EVM chain ID (defaults to --to-chain-index)")
	fs.StringVar(&cfg.receiveAddress, "receive-address", "", "destination address (defaults to the wallet address)")
	fs.StringVar(&cfg.allowBridge, "allow-bridge", "", "comma-separated bridge IDs to allow")
	fs.StringVar(&cfg.denyBridge, "deny-bridge", "", "comma-separated bridge IDs to deny")
	fs.BoolVar(&cfg.onlyBridge, "only-bridge", false, "bridge only; do not perform a destination swap")
	fs.StringVar(&cfg.toRPCURL, "to-rpc-url", "", "destination RPC URL used by --watch-receive")
	fs.BoolVar(&cfg.watchReceive, "watch-receive", false, "poll the destination balance after the source transaction confirms")
	fs.DurationVar(&cfg.watchTimeout, "watch-timeout", 15*time.Minute, "timeout for --watch-receive")
	fs.IntVar(&cfg.fromDecimals, "from-decimals", -1, "decimals of the sold token (defaults to the OKX token list)")
	fs.IntVar(&cfg.toDecimals, "to-decimals", -1, "decimals of the bought token (defaults to the OKX token list)")
	fs.StringVar(&cfg.walletAddress, "wallet-address", "", "wallet address, used by read-only stages when no private key is set")
	fs.StringVar(&cfg.broadcastVia, "broadcast", "rpc", "broadcast channel: rpc | okx")
	fs.StringVar(&stageName, "stage", "quote", "stage: "+strings.Join(stageNames, " | "))
	fs.DurationVar(&cfg.receiptTimeout, "receipt-timeout", 2*time.Minute, "timeout for waiting on the transaction receipt")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if err := loadEnvFile(cfg.envFile); err != nil {
		return nil, err
	}
	cfg.applyEnv()

	st, err := parseStage(stageName)
	if err != nil {
		return nil, err
	}
	cfg.stage = st

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *config) applyEnv() {
	c.apiKey = os.Getenv("OKX_API_KEY")
	c.secretKey = os.Getenv("OKX_SECRET_KEY")
	c.passphrase = os.Getenv("OKX_PASSPHRASE")
	c.projectID = os.Getenv("OKX_PROJECT_ID")
	c.privateKey = os.Getenv("WALLET_PRIVATE_KEY")
	c.rpcURL = os.Getenv("RPC_URL")
	if c.walletAddress == "" {
		c.walletAddress = os.Getenv("WALLET_ADDRESS")
	}
}

func (c *config) validate() error {
	var missing []string
	for _, item := range []struct{ name, value string }{
		{"OKX_API_KEY", c.apiKey},
		{"OKX_SECRET_KEY", c.secretKey},
		{"OKX_PASSPHRASE", c.passphrase},
	} {
		if item.value == "" {
			missing = append(missing, item.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required OKX credentials: %s", strings.Join(missing, ", "))
	}

	if c.chainIndex == "" || c.fromToken == "" || c.toToken == "" || c.amount == "" {
		return errors.New("--chain-index, --from, --to and --amount are all required")
	}
	if !c.crossChain && strings.EqualFold(c.fromToken, c.toToken) {
		return errors.New("--from and --to must be different tokens")
	}

	slippage, err := strconv.ParseFloat(c.slippage, 64)
	if err != nil || slippage < 0 || slippage > 1 {
		return fmt.Errorf("--slippage must be a number between 0 and 1, got %q", c.slippage)
	}

	if c.stage.needsChain() && c.rpcURL == "" {
		return errors.New("the sign/execute stages require RPC_URL (nonce, allowance and receipt)")
	}
	if c.crossChain {
		if c.toChainIndex == "" {
			return errors.New("--to-chain-index is required with --cross-chain")
		}
		if c.fromChainID == "" {
			c.fromChainID = c.chainIndex
		}
		if c.toChainID == "" {
			c.toChainID = c.toChainIndex
		}
		if c.swapMode != "exactIn" {
			return errors.New("cross-chain swaps only support exactIn")
		}
		if c.watchReceive && c.toRPCURL == "" {
			return errors.New("--watch-receive requires --to-rpc-url")
		}
	}
	if c.swapMode != "exactIn" && c.swapMode != "exactOut" {
		return fmt.Errorf("--swap-mode supports exactIn or exactOut, got %q", c.swapMode)
	}
	if c.stage == stageExecute && c.privateKey == "" {
		return errors.New("the execute stage requires WALLET_PRIVATE_KEY")
	}
	if c.broadcastVia != "rpc" && c.broadcastVia != "okx" {
		return fmt.Errorf("--broadcast supports rpc or okx, got %q", c.broadcastVia)
	}
	return nil
}

// loadEnvFile reads a KEY=VALUE file. A missing file is skipped silently and variables
// already present in the environment are never overwritten.
func loadEnvFile(path string) error {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read env file %s: %w", path, err)
	}

	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set environment variable %s: %w", key, err)
		}
	}
	return nil
}
