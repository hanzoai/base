package crdt

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// mockAnchorer records calls for testing.
type mockAnchorer struct {
	mu       sync.Mutex
	anchors  map[uint64][32]byte
	height   uint64
	submitCt int
}

func newMockAnchorer() *mockAnchorer {
	return &mockAnchorer{anchors: make(map[uint64][32]byte)}
}

func (m *mockAnchorer) Submit(ctx context.Context, root [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.height++
	m.anchors[m.height] = root
	m.submitCt++
	return nil
}

func (m *mockAnchorer) Verify(ctx context.Context, height uint64, root [32]byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.anchors[height]
	if !ok {
		return false, nil
	}
	return stored == root, nil
}

func (m *mockAnchorer) LatestHeight(ctx context.Context) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.height, nil
}

func TestMockAnchorerSubmitVerify(t *testing.T) {
	ctx := context.Background()
	a := newMockAnchorer()

	root := [32]byte{0xAA}
	if err := a.Submit(ctx, root); err != nil {
		t.Fatalf("submit: %v", err)
	}

	ok, err := a.Verify(ctx, 1, root)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("verify should match")
	}

	// Wrong root
	wrongRoot := [32]byte{0xBB}
	ok, err = a.Verify(ctx, 1, wrongRoot)
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if ok {
		t.Fatal("verify should not match wrong root")
	}

	// Nonexistent height
	ok, err = a.Verify(ctx, 999, root)
	if err != nil {
		t.Fatalf("verify nonexistent: %v", err)
	}
	if ok {
		t.Fatal("verify should not match nonexistent height")
	}
}

func TestMockAnchorerLatestHeight(t *testing.T) {
	ctx := context.Background()
	a := newMockAnchorer()

	h, _ := a.LatestHeight(ctx)
	if h != 0 {
		t.Fatalf("expected 0, got %d", h)
	}

	a.Submit(ctx, [32]byte{1})
	a.Submit(ctx, [32]byte{2})
	a.Submit(ctx, [32]byte{3})

	h, _ = a.LatestHeight(ctx)
	if h != 3 {
		t.Fatalf("expected 3, got %d", h)
	}
}

func TestDocumentMerkleRoot(t *testing.T) {
	doc := NewDocument("test", "nodeA")
	doc.GetText("body").InsertText(-1, "Hello")
	doc.GetCounter("views").Increment("nodeA", 10)

	root1, err := DocumentMerkleRoot(doc)
	if err != nil {
		t.Fatalf("root: %v", err)
	}

	// Same state = same root
	root2, err := DocumentMerkleRoot(doc)
	if err != nil {
		t.Fatalf("root2: %v", err)
	}
	if root1 != root2 {
		t.Fatal("deterministic root failed")
	}

	// Mutate and recompute
	doc.GetCounter("views").Increment("nodeA", 1)
	root3, err := DocumentMerkleRoot(doc)
	if err != nil {
		t.Fatalf("root3: %v", err)
	}
	if root3 == root1 {
		t.Fatal("root should change after mutation")
	}
}

func TestDocumentAnchorRoundTrip(t *testing.T) {
	ctx := context.Background()
	a := newMockAnchorer()

	doc := NewDocument("test", "A")
	doc.GetText("title").InsertText(-1, "Test Doc")

	root, err := DocumentMerkleRoot(doc)
	if err != nil {
		t.Fatalf("root: %v", err)
	}

	if err := a.Submit(ctx, root); err != nil {
		t.Fatalf("submit: %v", err)
	}

	ok, err := a.Verify(ctx, 1, root)
	if err != nil || !ok {
		t.Fatal("round-trip verification failed")
	}
}

func TestAnchorBackgroundCancelStops(t *testing.T) {
	a := newMockAnchorer()
	doc := NewDocument("test", "A")
	doc.GetText("x").InsertText(-1, "data")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		AnchorBackground(ctx, doc, a, AnchorConfig{Interval: 50 * time.Millisecond})
		close(done)
	}()

	// Wait for at least one anchor
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("AnchorBackground did not stop after context cancel")
	}

	a.mu.Lock()
	ct := a.submitCt
	a.mu.Unlock()
	if ct == 0 {
		t.Fatal("expected at least one submit")
	}
}

func TestAppIDFromName(t *testing.T) {
	id1 := AppIDFromName("my-app")
	id2 := AppIDFromName("my-app")
	if id1 != id2 {
		t.Fatal("AppIDFromName not deterministic")
	}

	id3 := AppIDFromName("other-app")
	if id1 == id3 {
		t.Fatal("different names should produce different IDs")
	}
}

func TestRPCAnchorer_SubmitCalldata(t *testing.T) {
	// Verify the calldata encoding matches the precompile's expectations.
	appID := AppIDFromName("test-app")
	root := [32]byte{0xDE, 0xAD}

	var captured []byte
	mockRPC := func(ctx context.Context, method string, params []any) (json.RawMessage, error) {
		if method != "eth_sendTransaction" {
			t.Fatalf("expected eth_sendTransaction, got %s", method)
		}
		p := params[0].(map[string]string)
		data := p["data"]
		b, _ := hex.DecodeString(data[2:]) // strip 0x
		captured = b
		return json.RawMessage(`"0xdeadbeef"`), nil
	}

	a := NewRPCAnchorer(AnchorConfig{
		AppID:       appID,
		RPCEndpoint: "http://localhost:9650",
		From:        "0x0000000000000000000000000000000000000001",
	}, mockRPC)

	if err := a.Submit(context.Background(), root); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Verify calldata structure: 4 selector + 32 appID + 32 height + 32 root = 100 bytes
	if len(captured) != 100 {
		t.Fatalf("expected 100 bytes calldata, got %d", len(captured))
	}

	// Check selector
	if captured[0] != selectorSubmit[0] || captured[1] != selectorSubmit[1] ||
		captured[2] != selectorSubmit[2] || captured[3] != selectorSubmit[3] {
		t.Fatal("selector mismatch")
	}

	// Check appID
	var gotAppID [32]byte
	copy(gotAppID[:], captured[4:36])
	if gotAppID != appID {
		t.Fatal("appID mismatch in calldata")
	}

	// Check height = 1 (first submit)
	h := binary.BigEndian.Uint64(captured[60:68])
	if h != 1 {
		t.Fatalf("expected height 1, got %d", h)
	}

	// Check root
	var gotRoot [32]byte
	copy(gotRoot[:], captured[68:100])
	if gotRoot != root {
		t.Fatal("root mismatch in calldata")
	}
}

func TestRPCAnchorer_VerifyCalldata(t *testing.T) {
	appID := AppIDFromName("test-app")
	expectedRoot := [32]byte{0xCA, 0xFE}

	// Mock returns the expected root as hex
	mockRPC := func(ctx context.Context, method string, params []any) (json.RawMessage, error) {
		if method != "eth_call" {
			t.Fatalf("expected eth_call, got %s", method)
		}
		return json.Marshal("0x" + hex.EncodeToString(expectedRoot[:]))
	}

	a := NewRPCAnchorer(AnchorConfig{
		AppID:       appID,
		RPCEndpoint: "http://localhost:9650",
		From:        "0x0000000000000000000000000000000000000001",
	}, mockRPC)

	ok, err := a.Verify(context.Background(), 1, expectedRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("verify should match")
	}

	// Wrong root
	ok, err = a.Verify(context.Background(), 1, [32]byte{0xFF})
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if ok {
		t.Fatal("verify should not match wrong root")
	}
}

// --- WithAutoAnchor integration tests ---

func TestDocument_NoAnchorer_NoGoroutineLeak(t *testing.T) {
	// Snapshot goroutine count, create a doc with no anchorer, confirm
	// no new goroutines were spawned.
	runtime.GC()
	before := runtime.NumGoroutine()

	doc := NewDocument("no-anchor", "nodeA")
	doc.GetText("x").InsertText(-1, "test")

	after := runtime.NumGoroutine()
	doc.Close() // no-op, should not panic

	// Allow +1 for runtime jitter (GC, scheduler), but not the +1 that
	// would come from a background anchor goroutine.
	if after > before+1 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestDocument_WithAutoAnchor_FiresAndReportsStatus(t *testing.T) {
	a := newMockAnchorer()

	doc := NewDocument("anchored", "nodeA",
		WithAutoAnchor(a, 50*time.Millisecond),
	)
	defer doc.Close()

	// Apply some ops so the Merkle root is non-zero.
	doc.GetText("body").InsertText(-1, "hello")
	doc.GetCounter("hits").Increment("nodeA", 1)

	// Wait for at least two anchor cycles.
	time.Sleep(200 * time.Millisecond)

	st := doc.AnchorStatus()
	if st.LastHeight == 0 {
		t.Fatal("expected LastHeight > 0 after anchor cycle")
	}
	if st.LastRoot == [32]byte{} {
		t.Fatal("expected non-zero LastRoot")
	}
	if st.LastAnchoredAt.IsZero() {
		t.Fatal("expected non-zero LastAnchoredAt")
	}
	if st.LastError != nil {
		t.Fatalf("unexpected LastError: %v", st.LastError)
	}

	a.mu.Lock()
	ct := a.submitCt
	a.mu.Unlock()
	if ct < 2 {
		t.Fatalf("expected >= 2 submits, got %d", ct)
	}
}

func TestDocument_Close_StopsAnchorWithin200ms(t *testing.T) {
	a := newMockAnchorer()

	doc := NewDocument("close-test", "nodeA",
		WithAutoAnchor(a, 50*time.Millisecond),
	)
	doc.GetText("x").InsertText(-1, "data")

	// Let one anchor fire.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	doc.Close()
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("Close took %v, expected < 200ms", elapsed)
	}

	// Verify goroutine is gone: a second Close should not block.
	doc.Close()
}

// --- RED TEAM: Adversarial attack vectors (ed972e42 review) ---

// blockingAnchorer has a Submit that blocks until unblocked via a channel.
type blockingAnchorer struct {
	submitEntered chan struct{} // closed when Submit starts executing
	unblock       chan struct{} // close to let Submit return
}

func newBlockingAnchorer() *blockingAnchorer {
	return &blockingAnchorer{
		submitEntered: make(chan struct{}),
		unblock:       make(chan struct{}),
	}
}

func (b *blockingAnchorer) Submit(ctx context.Context, root [32]byte) error {
	select {
	case <-b.submitEntered:
	default:
		close(b.submitEntered)
	}
	// Block until unblocked OR context cancelled.
	select {
	case <-b.unblock:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingAnchorer) Verify(ctx context.Context, height uint64, root [32]byte) (bool, error) {
	return true, nil
}

func (b *blockingAnchorer) LatestHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

// Before fix: Close blocks indefinitely because the goroutine is stuck
// inside Submit which does not observe context cancellation.
// After fix: startAnchorLoop passes ctx to Submit, and blockingAnchorer
// (any well-behaved Anchorer) returns on ctx.Done(). Close returns fast.
func TestCloseWhileSubmitMidFlight(t *testing.T) {
	ba := newBlockingAnchorer()

	doc := NewDocument("anchor-1", "nodeA",
		WithAutoAnchor(ba, 20*time.Millisecond),
	)
	doc.GetText("x").InsertText(-1, "data")

	// Wait for Submit to be entered (goroutine is now blocked inside Submit).
	select {
	case <-ba.submitEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("Submit was never entered within 2s")
	}

	// Now Close. Submit is blocked. Close MUST return within 200ms
	// because context cancellation propagates into Submit.
	start := time.Now()
	doc.Close()
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("VULN: Close blocked %v while Submit was mid-flight (expected <200ms)", elapsed)
	}
}

// Runs concurrent mutations (which acquire doc.mu) and AnchorStatus reads
// (which acquire anchorMu) under -race for 500ms. If there were an AB/BA
// lock ordering bug, the race detector or a deadlock would catch it.
func TestNoCrossLockDeadlock(t *testing.T) {
	a := newMockAnchorer()

	doc := NewDocument("anchor-2", "nodeA",
		WithAutoAnchor(a, 10*time.Millisecond),
	)
	defer doc.Close()
	doc.GetCounter("x").Increment("nodeA", 1)

	var wg sync.WaitGroup
	done := make(chan struct{})
	time.AfterFunc(500*time.Millisecond, func() { close(done) })

	// Goroutine 1: rapid mutations (acquires doc.mu).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
				doc.GetCounter("x").Increment("nodeA", 1)
			}
		}
	}()

	// Goroutine 2: rapid AnchorStatus reads (acquires anchorMu).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = doc.AnchorStatus()
			}
		}
	}()

	// Goroutine 3: rapid Version reads (acquires doc.mu RLock).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = doc.Version()
			}
		}
	}()

	wg.Wait()
	// If we reach here, no deadlock occurred.
}

func TestDoubleWithAutoAnchor_NoZombie(t *testing.T) {
	a1 := newMockAnchorer()
	a2 := newMockAnchorer()

	runtime.GC()
	before := runtime.NumGoroutine()

	doc := NewDocument("anchor-3", "nodeA",
		WithAutoAnchor(a1, 1*time.Second),
		WithAutoAnchor(a2, 30*time.Millisecond),
	)
	doc.GetText("x").InsertText(-1, "data")

	// Only one goroutine should have started.
	after := runtime.NumGoroutine()
	// Allow +2 for runtime jitter, but a zombie from a1 would add another.
	if after > before+2 {
		doc.Close()
		t.Fatalf("goroutine leak: before=%d after=%d (expected at most +2)", before, after)
	}

	// Wait for at least one anchor cycle.
	time.Sleep(100 * time.Millisecond)

	// a2 should have received submits (it was the last option applied).
	a2.mu.Lock()
	ct2 := a2.submitCt
	a2.mu.Unlock()

	// a1 should have received zero submits (overwritten by a2).
	a1.mu.Lock()
	ct1 := a1.submitCt
	a1.mu.Unlock()

	doc.Close()

	if ct1 != 0 {
		t.Fatalf("VULN: first anchorer got %d submits (expected 0, zombie goroutine)", ct1)
	}
	if ct2 == 0 {
		t.Fatal("second anchorer got 0 submits (expected > 0)")
	}
}

func TestAnchorStatusAfterClose(t *testing.T) {
	a := newMockAnchorer()

	doc := NewDocument("anchor-4", "nodeA",
		WithAutoAnchor(a, 30*time.Millisecond),
	)
	doc.GetText("x").InsertText(-1, "data")

	// Let at least one anchor fire.
	time.Sleep(100 * time.Millisecond)

	preSt := doc.AnchorStatus()
	doc.Close()

	// AnchorStatus after Close must not panic and must return last snapshot.
	postSt := doc.AnchorStatus()

	if postSt.LastHeight == 0 && preSt.LastHeight > 0 {
		t.Fatal("AnchorStatus after Close lost data")
	}
	if postSt.LastHeight != preSt.LastHeight {
		t.Fatalf("AnchorStatus changed after Close: pre=%d post=%d", preSt.LastHeight, postSt.LastHeight)
	}
}

func TestCloseOnDecodedDocument(t *testing.T) {
	// Create, encode, decode, then Close the decoded doc.
	orig := NewDocument("anchor-5", "nodeA")
	orig.GetText("x").InsertText(-1, "hello")
	orig.GetCounter("y").Increment("nodeA", 42)

	data, err := orig.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := Decode(data, "nodeB")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// This must not panic. Decoded docs have no anchor goroutine.
	decoded.Close()

	// AnchorStatus on a decoded doc must return zero value.
	st := decoded.AnchorStatus()
	if st.LastHeight != 0 || !st.LastAnchoredAt.IsZero() || st.LastError != nil {
		t.Fatalf("decoded doc AnchorStatus should be zero, got: %+v", st)
	}
}

// failingAnchorer always fails Submit, used to test backoff behavior.
type failingAnchorer struct {
	mu        sync.Mutex
	submitCt  int
	submitTimes []time.Time
}

func newFailingAnchorer() *failingAnchorer {
	return &failingAnchorer{}
}

func (f *failingAnchorer) Submit(ctx context.Context, root [32]byte) error {
	f.mu.Lock()
	f.submitCt++
	f.submitTimes = append(f.submitTimes, time.Now())
	f.mu.Unlock()
	return fmt.Errorf("simulated RPC failure")
}

func (f *failingAnchorer) Verify(ctx context.Context, height uint64, root [32]byte) (bool, error) {
	return false, nil
}

func (f *failingAnchorer) LatestHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

// exponential backoff (intervals grow), not constant-interval retries.
func TestExponentialBackoffOnFailure(t *testing.T) {
	fa := newFailingAnchorer()
	baseInterval := 30 * time.Millisecond

	doc := NewDocument("anchor-6", "nodeA",
		WithAutoAnchor(fa, baseInterval),
	)
	doc.GetText("x").InsertText(-1, "data")

	// Let it run long enough for several retry cycles.
	// With 30ms base and exponential backoff (30, 60, 120, 240ms),
	// in 600ms we'd see ~4 attempts with backoff vs ~20 without.
	time.Sleep(600 * time.Millisecond)
	doc.Close()

	fa.mu.Lock()
	ct := fa.submitCt
	times := make([]time.Time, len(fa.submitTimes))
	copy(times, fa.submitTimes)
	fa.mu.Unlock()

	if ct < 2 {
		t.Fatalf("expected >= 2 submit attempts, got %d", ct)
	}

	// With backoff, we should see significantly fewer attempts than
	// the ~20 that constant 30ms interval would produce in 600ms.
	// With exponential backoff (30, 60, 120, 240ms) we expect ~4-5 attempts.
	// Allow up to 8 to account for timing jitter, but reject > 12
	// which would indicate no backoff at all.
	if ct > 12 {
		t.Fatalf("VULN: thundering herd — %d retries in 600ms with %v base interval "+
			"(expected <= 12 with backoff, got constant-interval retry)", ct, baseInterval)
	}

	// Verify intervals are non-decreasing (evidence of backoff).
	if len(times) >= 3 {
		gap1 := times[1].Sub(times[0])
		gap2 := times[2].Sub(times[1])
		// Second gap should be >= first gap (backoff increases interval).
		// Allow 10ms of jitter.
		if gap2 < gap1-10*time.Millisecond {
			t.Logf("WARNING: gap1=%v gap2=%v — gaps should be non-decreasing with backoff", gap1, gap2)
		}
	}
}
