package crdt

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Anchorer submits and verifies CRDT state anchors on a Lux chain.
type Anchorer interface {
	// Submit anchors the given Merkle root at the next logical height.
	Submit(ctx context.Context, root [32]byte) error

	// Verify checks whether the on-chain anchor at height matches root.
	Verify(ctx context.Context, height uint64, root [32]byte) (bool, error)

	// LatestHeight returns the highest anchored height for the configured appID.
	LatestHeight(ctx context.Context) (uint64, error)
}

// AnchorConfig configures the background anchoring goroutine.
type AnchorConfig struct {
	// AppID identifies the application (32-byte hash of app name).
	AppID [32]byte

	// Interval is the maximum time between anchors. Default: 5 minutes.
	Interval time.Duration

	// OpThreshold is the number of operations that trigger an anchor.
	// Default: 1000. Set to 0 to disable op-based anchoring.
	OpThreshold int

	// RPCEndpoint is the JSON-RPC URL of the Lux node.
	RPCEndpoint string

	// From is the sender address (hex-encoded, with 0x prefix).
	From string

	// ContractAddress is the anchor precompile address.
	// Default: 0x0700000000000000000000000000000000000010
	ContractAddress string
}

// AppIDFromName computes the 32-byte appID as SHA-256 of the given name.
func AppIDFromName(name string) [32]byte {
	return sha256.Sum256([]byte(name))
}

// RPCAnchorer implements Anchorer via JSON-RPC calls to a Lux node.
type RPCAnchorer struct {
	cfg     AnchorConfig
	mu      sync.Mutex
	height  uint64
	rpcCall func(ctx context.Context, method string, params []any) (json.RawMessage, error)
}

// NewRPCAnchorer creates an Anchorer that talks to a Lux node via JSON-RPC.
// The rpcCall function is injected for testability. Pass nil to use the default
// HTTP JSON-RPC client targeting cfg.RPCEndpoint.
func NewRPCAnchorer(cfg AnchorConfig, rpcCall func(ctx context.Context, method string, params []any) (json.RawMessage, error)) *RPCAnchorer {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.OpThreshold == 0 {
		cfg.OpThreshold = 1000
	}
	if cfg.ContractAddress == "" {
		cfg.ContractAddress = "0x0700000000000000000000000000000000000010"
	}
	return &RPCAnchorer{cfg: cfg, rpcCall: rpcCall}
}

// Submit sends an anchor transaction via eth_sendTransaction.
func (a *RPCAnchorer) Submit(ctx context.Context, root [32]byte) error {
	a.mu.Lock()
	a.height++
	h := a.height
	a.mu.Unlock()

	calldata := encodeSubmitCalldata(a.cfg.AppID, h, root)

	params := []any{map[string]string{
		"from": a.cfg.From,
		"to":   a.cfg.ContractAddress,
		"data": "0x" + hex.EncodeToString(calldata),
	}}

	_, err := a.rpcCall(ctx, "eth_sendTransaction", params)
	if err != nil {
		return fmt.Errorf("anchor submit: %w", err)
	}
	return nil
}

// Verify queries the anchor precompile via eth_call and compares roots.
func (a *RPCAnchorer) Verify(ctx context.Context, height uint64, root [32]byte) (bool, error) {
	calldata := encodeGetCalldata(a.cfg.AppID, height)

	params := []any{map[string]string{
		"to":   a.cfg.ContractAddress,
		"data": "0x" + hex.EncodeToString(calldata),
	}, "latest"}

	result, err := a.rpcCall(ctx, "eth_call", params)
	if err != nil {
		return false, fmt.Errorf("anchor verify: %w", err)
	}

	var hexResult string
	if err := json.Unmarshal(result, &hexResult); err != nil {
		return false, fmt.Errorf("anchor verify: unmarshal result: %w", err)
	}

	// Strip 0x prefix
	if len(hexResult) >= 2 && hexResult[:2] == "0x" {
		hexResult = hexResult[2:]
	}

	chainRoot, err := hex.DecodeString(hexResult)
	if err != nil {
		return false, fmt.Errorf("anchor verify: decode hex: %w", err)
	}

	if len(chainRoot) < 32 {
		return false, errors.New("anchor verify: chain returned < 32 bytes")
	}

	var onChain [32]byte
	copy(onChain[:], chainRoot[:32])
	return onChain == root, nil
}

// LatestHeight queries the latest anchored height via eth_call.
func (a *RPCAnchorer) LatestHeight(ctx context.Context) (uint64, error) {
	calldata := encodeGetLatestCalldata(a.cfg.AppID)

	params := []any{map[string]string{
		"to":   a.cfg.ContractAddress,
		"data": "0x" + hex.EncodeToString(calldata),
	}, "latest"}

	result, err := a.rpcCall(ctx, "eth_call", params)
	if err != nil {
		return 0, fmt.Errorf("anchor latestHeight: %w", err)
	}

	var hexResult string
	if err := json.Unmarshal(result, &hexResult); err != nil {
		return 0, fmt.Errorf("anchor latestHeight: unmarshal: %w", err)
	}

	if len(hexResult) >= 2 && hexResult[:2] == "0x" {
		hexResult = hexResult[2:]
	}

	b, err := hex.DecodeString(hexResult)
	if err != nil {
		return 0, fmt.Errorf("anchor latestHeight: decode: %w", err)
	}

	if len(b) < 32 {
		return 0, nil
	}
	return binary.BigEndian.Uint64(b[24:32]), nil
}

// AnchorBackground runs a background goroutine that periodically anchors
// the document's state. Cancel the context to stop it.
func AnchorBackground(ctx context.Context, doc *Document, anchorer Anchorer, cfg AnchorConfig) {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Minute
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			root, err := DocumentMerkleRoot(doc)
			if err != nil {
				log.Printf("anchor: compute root: %v", err)
				continue
			}
			if err := anchorer.Submit(ctx, root); err != nil {
				log.Printf("anchor: submit: %v", err)
			}
		}
	}
}

// DocumentMerkleRoot computes SHA-256 of the gob-encoded document snapshot.
func DocumentMerkleRoot(doc *Document) ([32]byte, error) {
	data, err := doc.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("merkle root: encode: %w", err)
	}
	return sha256.Sum256(data), nil
}

// --- ABI encoding helpers ---
// These produce minimal ABI-compatible calldata without importing the full ABI library.

// Function selector: keccak256("submit(bytes32,uint64,bytes32)")[:4]
// Precomputed to avoid runtime dependency on crypto/keccak.
var (
	// submit(bytes32,uint64,bytes32) selector
	selectorSubmit = [4]byte{0x3c, 0x21, 0xe8, 0xa1}
	// get(bytes32,uint64) selector
	selectorGet = [4]byte{0x8e, 0xaa, 0x6a, 0xc0}
	// getLatest(bytes32) selector
	selectorGetLatest = [4]byte{0xba, 0x41, 0xd8, 0x47}
)

func encodeSubmitCalldata(appID [32]byte, height uint64, root [32]byte) []byte {
	buf := make([]byte, 4+96)
	copy(buf[:4], selectorSubmit[:])
	copy(buf[4:36], appID[:])
	binary.BigEndian.PutUint64(buf[60:68], height)
	copy(buf[68:100], root[:])
	return buf
}

func encodeGetCalldata(appID [32]byte, height uint64) []byte {
	buf := make([]byte, 4+64)
	copy(buf[:4], selectorGet[:])
	copy(buf[4:36], appID[:])
	binary.BigEndian.PutUint64(buf[60:68], height)
	return buf
}

func encodeGetLatestCalldata(appID [32]byte) []byte {
	buf := make([]byte, 4+32)
	copy(buf[:4], selectorGetLatest[:])
	copy(buf[4:36], appID[:])
	return buf
}
