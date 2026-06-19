package bootnode

import (
	"net/http"

	"github.com/hanzoai/base/core"
)

// chainNetwork describes one network of a chain.
type chainNetwork struct {
	Name           string `json:"name"`
	ChainID        *int   `json:"chainId"`
	IsTestnet      bool   `json:"isTestnet"`
	ExplorerURL    string `json:"explorerUrl"`
	NativeCurrency string `json:"nativeCurrency"`
	NativeDecimals int    `json:"nativeDecimals"`
}

// chainInfo describes a supported chain and its networks.
type chainInfo struct {
	Name     string                  `json:"name"`
	Slug     string                  `json:"slug"`
	Type     string                  `json:"type"`
	Networks map[string]chainNetwork `json:"networks"`
	LogoURL  string                  `json:"logoUrl"`
}

func intPtr(v int) *int { return &v }

// chainRegistry is the canonical set of chains bootnode serves. It is the Go
// equivalent of the Python ChainRegistry, trimmed to the Lux-family L1s plus
// the headline external chains. Reference data — not configuration.
var chainRegistry = map[string]chainInfo{
	"lux": {
		Name: "Lux Network", Slug: "lux", Type: "evm", LogoURL: "https://cdn.lux.network/logo.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Lux Mainnet", ChainID: intPtr(96369), ExplorerURL: "https://explore.lux.network", NativeCurrency: "LUX", NativeDecimals: 18},
			"testnet": {Name: "Lux Testnet", ChainID: intPtr(96368), IsTestnet: true, ExplorerURL: "https://explore-testnet.lux.network", NativeCurrency: "LUX", NativeDecimals: 18},
			"devnet":  {Name: "Lux Devnet", ChainID: intPtr(96370), IsTestnet: true, NativeCurrency: "LUX", NativeDecimals: 18},
		},
	},
	"zoo": {
		Name: "Zoo", Slug: "zoo", Type: "evm", LogoURL: "https://cdn.zoo.network/logo.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Zoo Mainnet", ChainID: intPtr(200200), ExplorerURL: "https://explore-zoo.lux.network", NativeCurrency: "ZOO", NativeDecimals: 18},
			"testnet": {Name: "Zoo Testnet", ChainID: intPtr(200201), IsTestnet: true, NativeCurrency: "ZOO", NativeDecimals: 18},
		},
	},
	"hanzo": {
		Name: "Hanzo", Slug: "hanzo", Type: "evm", LogoURL: "https://cdn.hanzo.ai/logo.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Hanzo Mainnet", ChainID: intPtr(36963), ExplorerURL: "https://explore-hanzo.lux.network", NativeCurrency: "AI", NativeDecimals: 18},
			"testnet": {Name: "Hanzo Testnet", ChainID: intPtr(36964), IsTestnet: true, NativeCurrency: "AI", NativeDecimals: 18},
		},
	},
	"pars": {
		Name: "Pars", Slug: "pars", Type: "evm", LogoURL: "https://cdn.pars.network/logo.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Pars Mainnet", ChainID: intPtr(494949), ExplorerURL: "https://explore-pars.lux.network", NativeCurrency: "PARS", NativeDecimals: 18},
			"testnet": {Name: "Pars Testnet", ChainID: intPtr(7071), IsTestnet: true, NativeCurrency: "PARS", NativeDecimals: 18},
		},
	},
	"ethereum": {
		Name: "Ethereum", Slug: "ethereum", Type: "evm", LogoURL: "https://cdn.lux.network/chains/ethereum.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Ethereum Mainnet", ChainID: intPtr(1), ExplorerURL: "https://etherscan.io", NativeCurrency: "ETH", NativeDecimals: 18},
			"sepolia": {Name: "Sepolia", ChainID: intPtr(11155111), IsTestnet: true, ExplorerURL: "https://sepolia.etherscan.io", NativeCurrency: "ETH", NativeDecimals: 18},
		},
	},
	"base": {
		Name: "Base", Slug: "base", Type: "evm", LogoURL: "https://cdn.lux.network/chains/base.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Base Mainnet", ChainID: intPtr(8453), ExplorerURL: "https://basescan.org", NativeCurrency: "ETH", NativeDecimals: 18},
		},
	},
	"bitcoin": {
		Name: "Bitcoin", Slug: "bitcoin", Type: "utxo", LogoURL: "https://cdn.lux.network/chains/bitcoin.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Bitcoin Mainnet", ExplorerURL: "https://mempool.space", NativeCurrency: "BTC", NativeDecimals: 8},
		},
	},
	"solana": {
		Name: "Solana", Slug: "solana", Type: "svm", LogoURL: "https://cdn.lux.network/chains/solana.svg",
		Networks: map[string]chainNetwork{
			"mainnet": {Name: "Solana Mainnet", ExplorerURL: "https://solscan.io", NativeCurrency: "SOL", NativeDecimals: 9},
			"devnet":  {Name: "Solana Devnet", IsTestnet: true, NativeCurrency: "SOL", NativeDecimals: 9},
		},
	},
}

// handleListChains lists all supported chains. Ports GET /chains. Public — no
// authentication required (matches the Python).
func (p *plugin) handleListChains(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"chains": chainRegistry})
}

// handleGetChain returns one chain. Ports GET /chains/{chain}.
func (p *plugin) handleGetChain(e *core.RequestEvent) error {
	slug := e.Request.PathValue("chain")
	chain, ok := chainRegistry[slug]
	if !ok {
		return e.NotFoundError("chain '"+slug+"' not found", nil)
	}
	return e.JSON(http.StatusOK, chain)
}
