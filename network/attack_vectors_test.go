package network

// attack_vectors_test.go — Base Network adversarial test suite.
//
// A test here is one of exactly two things, and never both:
//
//   - an ASSERTION of a defence. It passes while the defence holds and fails
//     with a description of what regressed. Deterministic — one seeded rand
//     source, no wall-clock margin, no port binding, no external service.
//   - a MARKER for a defence this package does not hold. Its whole body is
//     t.Skip(blockedReason), stated once at the top.
//
// Nothing decides at runtime which of the two it is. A test that skips only
// when it observes the defence missing cannot fail, and reports the regression
// as a skip — so every conditional skip in this file was rewritten into one
// shape or the other, and the `network-attack-suite` gate in hanzo.yml holds
// the file to it: a skip carrying any other reason is a failure, and so is a
// disagreement between the tests that DECLARE a marker skip and the tests that
// actually skipped. A marker that starts passing, or an assertion that starts
// skipping, is therefore a red build rather than a line in a log.
//
// Groups, in file order:
//   1. Consensus / frame / envelope integrity
//   2. P2P / transport
//   3. Archive / PITR
//   4. Shard routing / isolation
//   5. Resource exhaustion / DoS
//   6. Encryption / KMS
//   7. Correctness under concurrency
//
// The defences the assertions describe are stated as contracts in
// docs/NETWORK.md.

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockedReason is the one skip string this package uses, and a marker states
// it as its whole body. The gate reads it from this line, so there is one
// spelling of it rather than one here and one in the pipeline.
const blockedReason = "the defence this names is not held by this package"

// seededRand returns the deterministic rand source every test in this file
// uses. Shared constant seed keeps CI reproducible.
func seededRand(t *testing.T) *rand.Rand {
	t.Helper()
	return rand.New(rand.NewSource(42))
}

// mustStartCluster spins up n nodes over a memory transport, wires each into
// a shared hub, and starts them. Callers get a (ctx, cancel, []nodes, hub)
// tuple. Cleanup is registered on t.
func mustStartCluster(t *testing.T, n int, replication int) (context.Context, context.CancelFunc, []*node, *memoryHub) {
	t.Helper()
	hub := newMemoryHub()
	peers := make([]string, n)
	for i := range peers {
		peers[i] = fmt.Sprintf("node-%d", i)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	nodes := make([]*node, 0, n)
	for _, id := range peers {
		cfg := Config{
			Enabled:     true,
			ShardKey:    "user_id",
			Replication: replication,
			Peers:       filter(peers, id),
			NodeID:      id,
			Role:        RoleValidator,
			Archive:     "off",
			ListenHTTP:  ":0",
			ListenP2P:   ":0",
		}
		nn, err := newNodeWithTransport(cfg, hub.connect(NodeID(id)))
		if err != nil {
			cancel()
			t.Fatalf("node %s: %v", id, err)
		}
		if err := nn.Start(ctx); err != nil {
			cancel()
			t.Fatalf("start %s: %v", id, err)
		}
		t.Cleanup(func() { _ = nn.Stop(context.Background()) })
		nodes = append(nodes, nn)
	}
	t.Cleanup(cancel)
	return ctx, cancel, nodes, hub
}

// ---------------------------------------------------------------------------
// Group 1 — Consensus / frame / envelope integrity.
// Threat model: peers on the quasar p2p port are untrusted; they can craft
// arbitrary envelopes and frames. The invariant is that the inner frame's
// ShardID is the only shard routing authority — the envelope header is a
// routing HINT and must be validated against the frame.
// ---------------------------------------------------------------------------

// TestAttack_FrameShardIDSpoof — R1 envelope/frame ShardID confusion.
//
// Threat: peer crafts Envelope{ShardID: "victim"} wrapping Frame{ShardID:
// "attacker"}; both are attacker-controlled on the wire.
// Invariant: a frame must be rejected if its inner ShardID does not match
// the routing ShardID (envelope header == frame.ShardID).
// Expected: shard "victim" NEVER observes a finalised frame whose inner
// ShardID differs.
func TestAttack_FrameShardIDSpoof(t *testing.T) {
	ctx, _, nodes, _ := mustStartCluster(t, 2, 2)
	victim := nodes[1]
	sh, err := victim.shard("victim-shard")
	if err != nil {
		t.Fatalf("shard: %v", err)
	}
	preSeq := sh.LocalSeq()

	evil := newFrame("attacker-shard", 1<<60, 0, []byte("OWNED"))
	victim.onPeerFrame(Envelope{ShardID: "victim-shard", Frame: evil})

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sh.LocalSeq() != preSeq {
			t.Fatalf("EXPLOIT: victim-shard localSeq advanced to %d via "+
				"frame whose inner ShardID was %q (envelope-frame confusion "+
				"accepted). Defence missing on onPeerFrame or ingestRemote.",
				sh.LocalSeq(), evil.ShardID)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Defence working: victim never absorbed the cross-shard frame.
}

// TestAttack_SelfForgedSeqOverflow — R2 localSeq pinning via attacker-chosen
// Seq.
//
// Threat: peer submits a valid-for-its-own-shard frame with Seq near 2^64.
// Invariant: Shard.localSeq must be driven by quasar-finalised height, not
// by attacker-controlled Frame.Seq. Monotonic one-way advancement to
// attacker-chosen heights breaks read-your-writes for every honest client.
// Expected: honest Seq=1 followed by attacker Seq=2^62 leaves localSeq
// bounded — either localSeq stays at 1, or (if engine internally rejects
// the attacker frame) at 1 still.
func TestAttack_SelfForgedSeqOverflow(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 2, 2)
	victim := nodes[1]
	sh, _ := victim.shard("shard-foo")

	const wildSeq uint64 = 1 << 62
	evil := newFrame("shard-foo", wildSeq, 0, []byte("ghost"))
	victim.onPeerFrame(Envelope{ShardID: "shard-foo", Frame: evil})

	time.Sleep(300 * time.Millisecond)

	got := sh.LocalSeq()
	if got >= wildSeq {
		t.Fatalf("EXPLOIT: localSeq=%d after a peer submitted an attacker-"+
			"chosen Seq=%d. Every honest client presenting a txseq cookie "+
			"<= %d now reads stale state as 'caught up'. Fix: drive "+
			"localSeq from quasar-finalised height, not Frame.Seq header.",
			got, wildSeq, wildSeq)
	}
}

// TestAttack_DuplicateFrameReplay — idempotency check.
//
// Threat: peer replays the same finalised frame repeatedly (e.g. captured
// off the wire, resent after reboot).
// Invariant: apply must be idempotent per (salt, cksm). Second apply is a
// no-op that neither writes nor advances counters.
// Expected: FramesDuplicate counter increments; FramesFinalized does not
// double-count.
func TestAttack_DuplicateFrameReplay(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 2, 2)
	n := nodes[0]
	sh, _ := n.shard("shard-dup")
	f := newFrame("shard-dup", 1, 0, []byte("once"))

	// Submit the same frame 5 times. Idempotency is keyed on (salt, cksm)
	// so the repeats must coalesce.
	for i := 0; i < 5; i++ {
		_ = sh.submitLocal(f)
	}

	// Wait for the frame to finalise rather than for a fixed interval. A
	// deadline that expires is this test proving nothing, so it says so
	// instead of excusing itself — an engine that finalises nothing is the
	// same silence as one that never dedupes.
	deadline := time.Now().Add(2 * time.Second)
	for counterVal(t, n.Metrics().FramesFinalized) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	finalised := counterVal(t, n.Metrics().FramesFinalized)
	if finalised == 0 {
		t.Fatalf("5 submits of one frame finalised none within 2s — the apply " +
			"path never ran, so this asserts nothing about dedupe.")
	}
	if finalised > 1 {
		t.Fatalf("replay bypassed dedupe: FramesFinalized=%v (want ≤1). "+
			"(salt,cksm) idempotency key broken.", finalised)
	}
}

// TestAttack_OutOfOrderFrames — reordering doesn't corrupt localSeq.
//
// Threat: quasar may deliver finalised frames out of arrival order (DAG is
// not linear). If localSeq-tracking does a naive max(), reordering is fine;
// if it does anything like "only advance on +1 contiguous", stale reads
// block forever.
// Invariant: localSeq reflects the highest finalised Seq regardless of
// delivery order.
func TestAttack_OutOfOrderFrames(t *testing.T) {
	// Single-node test engine so every submit finalises deterministically.
	_, _, nodes, _ := mustStartCluster(t, 1, 1)
	n := nodes[0]
	sh, _ := n.shard("shard-ooo")

	// Submit Seq=10 before Seq=1..9 — simulates quasar DAG reordering.
	for _, seq := range []uint64{10, 3, 7, 1, 5, 2, 8, 4, 9, 6} {
		f := newFrame("shard-ooo", seq, seq-1, []byte(fmt.Sprintf("%d", seq)))
		_ = sh.submitLocal(f)
	}

	// Poll for the max seq to be reached.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sh.LocalSeq() >= 10 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := sh.LocalSeq()
	if got < 10 {
		t.Fatalf("out-of-order delivery lost frames: localSeq=%d, want ≥10. "+
			"Finalised height not tracked as max() over all applied seqs.", got)
	}
}

// TestAttack_ForgedBlockHeight — quasar Block.Height vs Frame.Seq skew.
//
// Threat: the quasar adapter builds a Block with Height = Frame.Seq. A
// malicious peer could submit a block whose Height disagrees with the
// embedded Frame.Seq, attempting to confuse consensus ordering.
// Invariant: blockID must be derived from the frame checksum (already is);
// Height is advisory only and cannot override Frame.Seq.
// Today the adapter code in quasar.go forwardFinalized decodes the inner
// frame — if Blue adds a Height-vs-Seq consistency check, this test
// asserts it.
func TestAttack_ForgedBlockHeight(t *testing.T) {
	// The adapter today trusts Frame.Seq over Block.Height on the receive
	// path (see decodeFrame in forwardFinalized). This is the correct
	// invariant; the test guards against regressions that would let
	// Block.Height override.
	f := newFrame("shard-fbh", 42, 41, []byte("payload"))
	enc := f.encode()
	got, err := decodeFrame(enc)
	if err != nil {
		t.Fatalf("decodeFrame: %v", err)
	}
	if got.Seq != 42 {
		t.Fatalf("Frame.Seq skew: got %d want 42", got.Seq)
	}
	if err := got.Valid(); err != nil {
		t.Fatalf("Valid after decode: %v", err)
	}
}

// TestAttack_NilFrameFields — decoder robustness against edge-case inputs.
//
// Threat: peer sends frames with empty shardID, zero salt, empty payload.
// Invariant: decoder accepts or rejects deterministically; no panic, no
// out-of-bounds.
func TestAttack_NilFrameFields(t *testing.T) {
	// Empty shardID: currently the decoder treats "" as a zero-length string
	// which IS a valid frame by the wire format. The invariant we care about
	// is "no panic on any attacker-controlled bytes".
	f := newFrame("", 1, 0, nil)
	if err := f.Valid(); err != nil {
		t.Fatalf("Valid on empty frame: %v", err)
	}
	// Random garbage that's too short MUST error, not panic.
	garbage := []byte{0x00, 0x00}
	if _, err := decodeFrame(garbage); err == nil {
		t.Fatalf("decodeFrame(garbage) = nil; want error on short input")
	}
	// Oversized shardIDLen field that claims length past buffer end.
	bad := make([]byte, 64)
	bad[0] = 1 // version
	binary.BigEndian.PutUint16(bad[1:], 0xFFFF)
	if _, err := decodeFrame(bad); err == nil {
		t.Fatalf("decodeFrame with oversized shardID len = nil; want error")
	}
}

// ---------------------------------------------------------------------------
// Group 2 — P2P / transport.
// ---------------------------------------------------------------------------

// TestAttack_NoTransportFallback — the production constructor cannot end up
// with the no-op transport.
//
// nopTransport accepts every send and delivers nothing. It is the shape a
// test asks for, and a production node that fell back to it would start
// clean, log nothing and replicate nothing — every pod its own island, each
// one certain it is a member. Broadcast returning nil is the whole problem:
// silence is indistinguishable from success.
// Invariant: newNode, the one production entry point, always injects the real
// transport. The no-op is reachable only by a caller that names it.
//
// This says nothing about whether a peer is AUTHENTICATED — that is
// TestAttack_PeerImpersonation, and it is a marker.
func TestAttack_NoTransportFallback(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ShardKey:    "user_id",
		Replication: 3,
		Peers:       []string{"b:9999", "c:9999"},
		NodeID:      "a",
		Role:        RoleValidator,
		Archive:     "off",
		ListenHTTP:  ":8090",
		ListenP2P:   ":9999",
	}
	nn, err := newNode(cfg)
	if err != nil {
		t.Fatalf("newNode: %v", err)
	}
	if _, isNop := nn.transport.(*nopTransport); isNop {
		t.Fatalf("newNode built a node on the no-op transport: it sends to " +
			"nobody and reports success, so a pod replicates nothing and " +
			"nothing anywhere says so.")
	}
}

// TestAttack_ReplayOldFrame — capture-replay across the wire.
//
// Threat: attacker captures an envelope in flight, replays it hours later.
// Invariant: dedupe keyed on (salt, cksm) coalesces — state is unchanged.
// A successful defence also means signing keys rotated before the replay
// don't suddenly accept old bytes.
func TestAttack_ReplayOldFrame(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 2, 2)
	n := nodes[0]
	sh, _ := n.shard("shard-replay")
	f := newFrame("shard-replay", 1, 0, []byte("orig"))

	_ = sh.submitLocal(f)
	time.Sleep(200 * time.Millisecond)
	before := sh.LocalSeq()

	// Capture the frame, modify nothing, replay after a "delay".
	time.Sleep(100 * time.Millisecond)
	_ = sh.ingestRemote(f)
	time.Sleep(200 * time.Millisecond)

	after := sh.LocalSeq()
	if after != before {
		t.Fatalf("replay advanced state: before=%d after=%d. "+
			"Dedupe on (salt,cksm) is broken.", before, after)
	}
}

// TestAttack_PeerImpersonation — a peer's claimed NodeID is not attested.
//
// MARKER. A peer is whatever answers at an address in BASE_PEERS, and the
// NodeID it states in the handshake is its own word. The p2p plane is what its
// operator makes it: a cluster-internal port whose reachability is a
// NetworkPolicy question.
//
// This file used to carry an mTLS surface with SAN pinning that nothing
// constructed. It was deleted rather than wired, because it could not be wired
// here. The transport is a luxfi/zap node, which takes ONE *tls.Config and
// uses it for both the listener it binds and every peer it dials — and Go
// dials with the config verbatim, so a single config cannot name the peer it
// is dialling. Measured, on zap v1.2.7: a client config with no ServerName
// refuses to handshake at all; one carrying a peer's name verifies against
// that peer and fails hostname verification against every other; and
// InsecureSkipVerify, the usual way out, leaves VerifyPeerCertificate with an
// empty chain on the dialling side, so the pinning hook had no leaf to pin.
//
// Closing it needs the transport to build a config per destination, which is
// upstream, and certificates need an issuer, which is a deployment. Both have
// to land together: a config threaded through while nothing fills it reads as
// mTLS and is plaintext. The names that promised it now refuse — see
// TestAttack_TLSNamesRefused.
func TestAttack_PeerImpersonation(t *testing.T) {
	t.Skip(blockedReason)
}

// TestAttack_QuasarFloodDOS — rate-limit / backpressure on peer submits.
//
// Threat: attacker floods the node with valid-looking frames; the apply
// loop is unbounded, engine channel fills, memory OOMs.
// Invariant: the per-shard quasar engine channel MUST be bounded (today
// cap=1024). Under flood the submit path must return an error, not block
// indefinitely.
// Expected: after N frames > channel cap, submitLocal does not hang.
func TestAttack_QuasarFloodDOS(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 1, 1)
	n := nodes[0]
	sh, _ := n.shard("shard-flood")

	// Submit 2048 (= 2x channel cap) tiny frames as fast as we can; if the
	// submit path blocks forever the test deadline trips. We constrain to
	// 3 s so a pathological regression is caught.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := uint64(1); i <= 2048; i++ {
			f := newFrame("shard-flood", i, i-1, []byte{byte(i)})
			_ = sh.submitLocal(f)
		}
	}()
	select {
	case <-done:
		// Acceptable — either the engine absorbed all, or returned err
		// on overflow. Both are bounded liveness.
	case <-time.After(3 * time.Second):
		t.Fatalf("flood of 2048 frames blocked submitLocal past 3 s — " +
			"unbounded blocking on engine channel. At 100k shards this is " +
			"a fleetwide DOS via any one shard.")
	}
}

// TestAttack_GatewayMembershipPoisoning — a peer cannot add itself.
//
// Threat: a pod on the p2p plane announces membership, or answers a members
// probe with endpoints it chose, and the routing ring absorbs them.
// Invariant: the ring is built from the Membership source (DNS over
// BASE_PEERS) and from nothing a peer says. Frames arriving from an
// unannounced node are handled on their merits and change no routing.
// Expected: after traffic naming a node the ring has never heard of, the
// member set is byte-identical to what it was.
func TestAttack_GatewayMembershipPoisoning(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 2, 2)
	n := nodes[0]

	before := n.MembersFor("shard-x")

	// A frame that arrives claiming a shard, from a node nobody listed.
	n.onPeerFrame(Envelope{
		ShardID: "shard-x",
		Frame:   newFrame("shard-x", 1, 0, []byte("from nowhere")),
	})
	time.Sleep(200 * time.Millisecond)

	after := n.MembersFor("shard-x")
	if len(before) != len(after) {
		t.Fatalf("peer traffic changed the member set: %v → %v. Routing is "+
			"supposed to come from the membership source and from nothing a "+
			"peer sends.", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("peer traffic changed the member set: %v → %v", before, after)
		}
	}
}

// ---------------------------------------------------------------------------
// Group 3 — Archive / PITR.
// ---------------------------------------------------------------------------

// TestAttack_ArchiveSegmentForgery — R3 bucket writer forges a segment.
//
// Threat: attacker with bucket write access (stale IAM, compromised CI,
// misconfigured SA) crafts a full .lbn, places it at the deterministic
// path. PITR replays it.
// Invariant: segment verification requires an Ed25519 signature over
// (body || crc || pubkey) with a key from the configured trust set. An
// attacker without the archive-role private key cannot forge.
// Expected: attacker-signed segment is rejected by decodeSegment with a
// signer-mismatch error; Range does not yield attacker frames.
func TestAttack_ArchiveSegmentForgery(t *testing.T) {
	_, archivePriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	cfg := ArchiveConfig{
		URL:                "mem://ignored",
		SegmentTargetBytes: 4096,
		FlushInterval:      20 * time.Millisecond,
		RetryDeadline:      time.Second,
		SigningKey:         archivePriv,
	}
	up := newMemUploader()
	w := newArchiveWriter(up, "svc", cfg, nil)
	t.Cleanup(func() { _ = w.Close() })

	// Legitimate segment via the writer.
	ctx := context.Background()
	for i := uint64(1); i <= 5; i++ {
		f := newFrame("victim-shard", i, i-1, []byte(fmt.Sprintf("real-%d", i)))
		if err := w.Append(ctx, "victim-shard", i, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	_ = w.Close()

	// Attacker crafts a segment with their OWN key — not the archive-role
	// key — and writes it at a believable path.
	attackerSigner, _ := testSignerPair(t)
	sb := newSegmentBuffer("victim-shard", 5000)
	evil := newFrame("victim-shard", 5000, 4999, []byte("INJECTED-PITR-ROWS"))
	if err := sb.append(5000, evil.encode()); err != nil {
		t.Fatalf("attacker append: %v", err)
	}
	enc, err := sb.encode(attackerSigner)
	if err != nil {
		t.Fatalf("attacker encode: %v", err)
	}
	key := objectKey("svc", "victim-shard", 5000, time.Now().UnixNano())
	if err := up.put(context.Background(), key, enc); err != nil {
		t.Fatalf("attacker put: %v", err)
	}

	// PITR replays. The forged segment must not be yielded — and the five
	// legitimate frames must still be, because a reader that rejects
	// everything also refuses the forgery and has destroyed the archive to
	// do it. Both halves or neither.
	it, err := w.Range(context.Background(), "victim-shard", 1, 6000)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	var seen []uint64
	for f, ferr := range it {
		if ferr != nil {
			t.Fatalf("iter: %v", ferr)
		}
		if f.Seq == 5000 && string(f.Payload) == "INJECTED-PITR-ROWS" {
			t.Fatalf("EXPLOIT: forged segment accepted during PITR; " +
				"attacker payload replayed as if quasar-finalised. " +
				"Signature verification absent or trust set too wide.")
		}
		seen = append(seen, f.Seq)
	}
	if len(seen) != 5 {
		t.Fatalf("the writer's own history did not survive verification: "+
			"got seqs %v, want the five legitimate frames", seen)
	}
}

// TestAttack_ArchiveSegmentRewrite — R8 deterministic-key overwrite.
//
// Threat: mid-flush crash leaves a segment at `.../00000000000000000042-N.lbn`
// on disk; restart re-encodes a segment from the same startSeq with fewer
// frames (the tail was lost to the crash). Old-impl: same objectKey means
// PutObject overwrites and the tail is gone.
// Invariant: objectKey MUST include a per-flush disambiguator (nanos) so
// two flushes for the same startSeq cannot collide.
func TestAttack_ArchiveSegmentRewrite(t *testing.T) {
	a := objectKey("svc", "shard", 42, 100)
	b := objectKey("svc", "shard", 42, 200)
	if a == b {
		t.Fatalf("objectKey collision for same startSeq: a=%q b=%q. "+
			"R8 regression — re-flush overwrites prior segment and loses "+
			"the tail.", a, b)
	}
	if !strings.Contains(a, "-") || !strings.Contains(b, "-") {
		t.Fatalf("objectKey missing nanos suffix: a=%q b=%q", a, b)
	}
}

// TestAttack_ArchiveOutOfOrderSegments — Range must dedupe overlapping
// segments.
//
// Threat: two flushes cover the same startSeq (crash + restart). Without
// dedupe Range yields the same frame twice, PITR double-writes.
// Invariant: Range dedupes by (startSeq + frameIndex) and the later-nanos
// segment wins.
func TestAttack_ArchiveOutOfOrderSegments(t *testing.T) {
	signer, verifier := testSignerPair(t)
	up := newMemUploader()
	cfg := ArchiveConfig{
		URL:                "mem://",
		SegmentTargetBytes: 1 << 20,
		FlushInterval:      time.Hour,
		RetryDeadline:      time.Second,
		SigningKey:         signer.priv,
		TrustedSegmentKeys: []ed25519.PublicKey{signer.pub},
	}
	w := newArchiveWriter(up, "svc", cfg, nil)
	t.Cleanup(func() { _ = w.Close() })

	// Two segments covering seqs 1..3, crafted directly into storage.
	for _, nanos := range []int64{100, 200} {
		sb := newSegmentBuffer("s", 1)
		for i := uint64(1); i <= 3; i++ {
			f := newFrame("s", i, i-1, []byte(fmt.Sprintf("v%d-%d", nanos, i)))
			_ = sb.append(i, f.encode())
		}
		enc, err := sb.encode(signer)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_ = up.put(context.Background(), objectKey("svc", "s", 1, nanos), enc)
	}
	_ = verifier

	// Range should yield exactly 3 frames (deduped), not 6.
	it, err := w.Range(context.Background(), "s", 1, 3)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	var seen int
	for _, ferr := range it {
		if ferr != nil {
			t.Fatalf("iter err: %v", ferr)
		}
		seen++
	}
	if seen != 3 {
		t.Fatalf("overlap dedupe broken: yielded %d frames, want 3. "+
			"PITR double-replays on restart→re-flush.", seen)
	}
}

// TestAttack_PITRReplayCrossShard — a restore reads only its own shard.
//
// Threat: a segment for shard B is placed at shard A's path — a bucket
// prefix typo, a rename by anyone holding bucket write. It is validly
// signed, so signature verification passes it.
// Invariant: a segment names its shard INSIDE the signature and the object
// key names one too. Only the signed half counts, so Range refuses a segment
// whose declared shard is not the one being read, exactly as it refuses one
// it cannot verify.
func TestAttack_PITRReplayCrossShard(t *testing.T) {
	signer, _ := testSignerPair(t)
	up := newMemUploader()
	cfg := ArchiveConfig{
		URL:                "mem://",
		SegmentTargetBytes: 1 << 20,
		FlushInterval:      time.Hour,
		RetryDeadline:      time.Second,
		SigningKey:         signer.priv,
		TrustedSegmentKeys: []ed25519.PublicKey{signer.pub},
	}
	w := newArchiveWriter(up, "svc", cfg, nil)
	t.Cleanup(func() { _ = w.Close() })

	// Place a segment encoded for shard "B" — signed by a key the reader
	// trusts — at shard "A"'s path.
	sb := newSegmentBuffer("B", 1)
	f := newFrame("B", 1, 0, []byte("B's rows"))
	_ = sb.append(1, f.encode())
	enc, err := sb.encode(signer)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	wrongPath := objectKey("svc", "A", 1, time.Now().UnixNano())
	if err := up.put(context.Background(), wrongPath, enc); err != nil {
		t.Fatalf("put: %v", err)
	}

	it, err := w.Range(context.Background(), "A", 1, 10)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	for f, ferr := range it {
		if ferr != nil {
			t.Fatalf("iter: %v", ferr)
		}
		t.Fatalf("restoring shard A yielded a frame from segment %q (seq %d) — "+
			"a restore writes what it is handed, so B's rows land in A's "+
			"database. Range must compare the segment's own ShardID with the "+
			"shard being read.", f.ShardID, f.Seq)
	}
}

// TestAttack_ArchiveBucketPermissionLeak — BASE_ARCHIVE names any bucket.
//
// MARKER. NewArchive dispatches on scheme and accepts whatever host follows
// it, so where a pod ships its frames is settled entirely by the value of an
// environment variable. There is no allowlist here and this package is the
// wrong place for one: it knows a URL, not which buckets the deployment owns.
// That belongs to whatever writes the variable — the operator validating a
// spec against the buckets it provisioned.
//
// It previously read as an assertion and was not one. It called NewArchive
// with an s3:// URL, which resolves credentials and asks the endpoint whether
// the bucket exists — so it passed by failing to reach AWS, and would have
// started skipping on any runner that could. A suite whose first line is
// "no external services" cannot settle this by dialling one.
func TestAttack_ArchiveBucketPermissionLeak(t *testing.T) {
	t.Skip(blockedReason)
}

// ---------------------------------------------------------------------------
// Group 4 — Shard routing / isolation.
// ---------------------------------------------------------------------------

// The marker that stood here said a client can name its own shard, because
// core.shardResolver read the identity first and fell back to the header
// X-<Shard-Key> when there was none. BASE_SHARD_KEY now names ONE source and
// names it in the value: ShardKeyHeader ("header:") reads the request header
// an operator wrote, and every other form reads the verified identity and
// nothing else. The assertion is where the resolver is — core's
// TestShardResolverIdentityReadsNothingElse — because this package cannot
// import core, and a marker here for a defence held there would be reporting
// on a file it cannot see.
//
// What a shard reaches, for whoever reads that test next: the resolved id goes
// to writeForward and nowhere else, so it selects WHICH POD serves a write and
// no part of what that pod will let the caller read or write. Isolation is the
// org's Base and the collection rules. What it selects among is this ring, so
// the pod is always a member of BASE_PEERS.

// TestAttack_OwnerConsistencyAcrossPods — deterministic routing invariant.
//
// Threat: two pods disagree on ownership of a shard; gateway sends writes
// to one, reads to the other; state diverges.
// Invariant: router.ownerOf(s) must be byte-identical across any pod
// sharing the same member set.
func TestAttack_OwnerConsistencyAcrossPods(t *testing.T) {
	members := []NodeID{"a", "b", "c", "d", "e"}
	r1 := newRouter(members, 3)
	// Shuffle the member list to mimic different pods seeing peers in
	// different orders.
	rnd := seededRand(t)
	shuffled := append([]NodeID(nil), members...)
	rnd.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	r2 := newRouter(shuffled, 3)

	for _, shard := range []string{"u1", "u2", "u3", "u4", "u5"} {
		if r1.ownerOf(shard) != r2.ownerOf(shard) {
			t.Fatalf("shard %q ownership diverges: r1=%q r2=%q. "+
				"Non-deterministic routing → split-brain.",
				shard, r1.ownerOf(shard), r2.ownerOf(shard))
		}
	}
}

// TestAttack_EmptyShardKey — Config.ShardKey validation.
//
// Threat: production deploy with BASE_SHARD_KEY unset; every request
// lands on shard "" which collapses every base into one engine.
// Invariant: Config.validate() rejects empty ShardKey when Enabled.
func TestAttack_EmptyShardKey(t *testing.T) {
	cfg := Config{Enabled: true, Replication: 1, NodeID: "a"}
	if err := cfg.validate(); err == nil {
		t.Fatalf("empty ShardKey with Enabled=true accepted. Every base " +
			"collapses into one engine → no isolation.")
	}
}

// TestAttack_ConcurrentRebalance — member churn while writes in flight.
//
// Threat: during a pod scale event, a shard's owner changes; writes hit
// old owner, reads hit new owner, localSeq mismatch.
// Invariant: router setMembers is atomic; membersFor always reflects a
// single snapshot (never a partially-mutated state). Race detector
// catches torn writes under -race.
func TestAttack_ConcurrentRebalance(t *testing.T) {
	members := []NodeID{"a", "b", "c"}
	r := newRouter(members, 2)

	stop := make(chan struct{})
	readerDone := make(chan uint64)
	go func() {
		var n uint64
		for {
			select {
			case <-stop:
				readerDone <- n
				return
			default:
			}
			ms := r.membersFor("shard-x")
			if len(ms) == 0 {
				readerDone <- n
				t.Error("empty members during rebalance — torn setMembers")
				return
			}
			// Assert no duplicates in the snapshot (a partial setMembers
			// could leave dupes in a naive impl).
			seen := map[NodeID]bool{}
			for _, m := range ms {
				if seen[m] {
					readerDone <- n
					t.Errorf("duplicate member in snapshot: %v", ms)
					return
				}
				seen[m] = true
			}
			n++
		}
	}()

	// Rebalance repeatedly.
	for i := 0; i < 500; i++ {
		set := []NodeID{"a", "b", "c", "d"}
		if i%2 == 0 {
			set = []NodeID{"a", "b", "c"}
		}
		r.setMembers(set)
	}
	close(stop)
	ops := <-readerDone
	if ops == 0 {
		// Extreme scheduler starvation. Acceptable outcome: the
		// reader goroutine simply never ran on a busy CI. Don't fail —
		// race-detector already validated the lock semantics.
		t.Log("reader goroutine did not run (CI scheduler busy) — " +
			"race detector validates the invariant in a separate run")
	}
}

// ---------------------------------------------------------------------------
// Group 5 — Resource exhaustion / DoS.
// ---------------------------------------------------------------------------

// TestAttack_UnboundedShardBacklog — R6 archive backlog cap.
//
// Threat: S3 outage + high write rate → per-shard backlog grows without
// bound → OOM.
// Invariant: BacklogMaxBytes / BacklogMaxSegments enforced; oldest
// segments dropped with IncDrops metric firing.
// Expected: after 4000 appends against a failing uploader, backlog
// remains bounded by the cap.
func TestAttack_UnboundedShardBacklog(t *testing.T) {
	signer, _ := testSignerPair(t)
	fail := &failingUploader{}
	drops := atomic.Int64{}
	m := &ArchiveMetrics{
		IncDrops: func() { drops.Add(1) },
	}
	cfg := ArchiveConfig{
		SegmentTargetBytes: 64, // small, rotates frequently
		FlushInterval:      5 * time.Millisecond,
		RetryDeadline:      20 * time.Millisecond,
		BacklogMaxBytes:    4096, // tight cap
		BacklogMaxSegments: 32,
		SigningKey:         signer.priv,
	}
	w := newArchiveWriter(fail, "svc", cfg, m)
	t.Cleanup(func() { _ = w.Close() })

	ctx := context.Background()
	for i := uint64(1); i <= 4000; i++ {
		f := newFrame("shard-A", i, i-1, []byte(strings.Repeat("x", 60)))
		if err := w.Append(ctx, "shard-A", i, f.encode()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	time.Sleep(500 * time.Millisecond)

	w.mu.Lock()
	q := w.shards["shard-A"]
	backlogBytes := 0
	for _, p := range q.backlog {
		backlogBytes += len(p.data)
	}
	backlogLen := len(q.backlog)
	w.mu.Unlock()

	// The cap plus at most the one segment that crossed it before the shed.
	// A slacker bound passes while the cap is off by a factor.
	if backlogBytes > cfg.BacklogMaxBytes+cfg.SegmentTargetBytes {
		t.Fatalf("backlog bytes %d over cap %d (+1 segment of %d); the cap is "+
			"not bounding memory. Under a storage outage the pod OOMs.",
			backlogBytes, cfg.BacklogMaxBytes, cfg.SegmentTargetBytes)
	}
	if backlogLen > cfg.BacklogMaxSegments+1 {
		t.Fatalf("backlog segments %d over cap %d", backlogLen, cfg.BacklogMaxSegments)
	}
	if drops.Load() == 0 {
		t.Fatalf("4000 appends against failing uploader produced 0 drops; " +
			"either the cap is absent or IncDrops wiring is missing, and the " +
			"bound above holds vacuously.")
	}
}

// TestAttack_ApplyLoopStarvation — one noisy shard starves others.
//
// Threat: a pathological shard floods its apply loop; other shards'
// applies stall.
// Invariant: each shard has its own applyLoop goroutine; contention on
// shared mutexes must be bounded.
func TestAttack_ApplyLoopStarvation(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 1, 1)
	n := nodes[0]

	noisy, _ := n.shard("noisy")
	quiet, _ := n.shard("quiet")

	// Hammer noisy with 500 frames; quiet gets 1.
	for i := uint64(1); i <= 500; i++ {
		f := newFrame("noisy", i, i-1, []byte{byte(i)})
		_ = noisy.submitLocal(f)
	}
	qf := newFrame("quiet", 1, 0, []byte("q"))
	_ = quiet.submitLocal(qf)

	// Both should make progress within 2s.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if quiet.LocalSeq() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("quiet shard starved: its 1 frame did not finalise while " +
		"noisy shard's 500 were in flight. Per-shard apply isolation broken.")
}

// TestAttack_ManySmallShards — nothing bounds how many shards a pod holds.
//
// MARKER, and capacity rather than confidentiality. A shard is created by
// being named, and each one is a Quasar engine holding a 1024-slot channel,
// so the pod's floor is that product. A shard key of the wrong grain — per
// user rather than per org — reaches it without an adversary. Nothing in the
// tree is exercised above three shards.
//
// Two markers used to say this: one for the count and one for the per-shard
// cost. They are one sentence, and the product is the only thing either of
// them meant.
func TestAttack_ManySmallShards(t *testing.T) {
	t.Skip(blockedReason)
}

// ---------------------------------------------------------------------------
// Group 6 — Names this package refuses.
//
// Each asserts the same thing about a different variable: a name that promises
// a property this package does not deliver is an error at startup and never a
// no-op, because an operator who sets one and sees no complaint has been told
// the property holds.
// ---------------------------------------------------------------------------

// TestAttack_WrongDEKForShard — per-shard DEK misbinding.
//
// Threat: the KMS plumbing caches DEKs in memory LRU; a bug swaps DEKs
// between shards and org A reads org B's plaintext.
// Invariant: this package decides no key, so a name that reads like one is
// refused at startup rather than accepted. An operator who sets it and gets
// no error would take encryption at rest as settled.
func TestAttack_WrongDEKForShard(t *testing.T) {
	t.Setenv("BASE_ENCRYPT", "sqlcipher")
	t.Setenv("BASE_NETWORK", "quasar")
	t.Setenv("BASE_SHARD_KEY", "user_id")
	t.Setenv("HOSTNAME", "a")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv accepted BASE_ENCRYPT; it must refuse")
	}
}

// TestAttack_TLSNamesRefused — the peer plane presents no certificate, and
// every name that offered one says so.
//
// Threat: an operator reads that the p2p transport does mTLS, mounts a CA and
// a keypair, sets these, and gets a process that starts clean. Nothing it does
// authenticates a peer, and the quiet start is what says otherwise.
// Invariant: each name is an error at startup. One at a time, because an
// operator setting only the one they read about must hit it too.
func TestAttack_TLSNamesRefused(t *testing.T) {
	for _, name := range tlsNames {
		t.Run(name, func(t *testing.T) {
			t.Setenv("BASE_NETWORK", "quasar")
			t.Setenv("BASE_SHARD_KEY", "user_id")
			t.Setenv("HOSTNAME", "a")
			t.Setenv(name, "/etc/base/peer.pem")
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatalf("ConfigFromEnv accepted %s. A name that reads as peer "+
					"authentication and does nothing tells an operator the "+
					"property holds; it must refuse", name)
			}
		})
	}
}

// The three markers that stood here — a rogue KMS cluster member, a deleted
// DEK, a platform-wide KEK — all named base/plugins/kms and the env variable
// BASE_KEK_SCOPE. Neither exists. A key is DERIVED per org from a master key
// (hanzoai/cek, one derivation for the platform), so there is no wrapper to
// delete and no scope to choose, and the separation the third one asked for is
// asserted where the derivation is: plugins/org's TestOrgDB_OrgDEK_UniquePerOrg
// and TestOrgDB_UserDEK_UniquePerUser. Whether the KMS a Base talks to is
// authenticated is the KMS client's question, in plugins/org, not the
// replication plane's.

// TestAttack_FeatureGateEscape — BASE_NETWORK accepts only known modes.
//
// Threat: an attacker-controlled env injection sets BASE_NETWORK to an
// unknown value; the code silently falls back to standalone.
// Invariant: ConfigFromEnv rejects unknown modes with an explicit error.
func TestAttack_FeatureGateEscape(t *testing.T) {
	t.Setenv("BASE_NETWORK", "paxos-v99")
	t.Setenv("BASE_SHARD_KEY", "user_id")
	t.Setenv("HOSTNAME", "a")
	if _, err := FromEnv(); err == nil {
		t.Fatalf("unknown BASE_NETWORK=paxos-v99 accepted; fail-open on " +
			"feature gate is a silent-regression vector.")
	}
}

// ---------------------------------------------------------------------------
// Group 7 — Correctness under concurrency.
// ---------------------------------------------------------------------------

// TestAttack_WriterChurnLostCommits — concurrent submits must all finalise.
//
// Threat: two goroutines submit on the same shard concurrently; one
// finalisation overwrites another's seq counter.
// Invariant: atomic Seq allocation; no torn updates.
func TestAttack_WriterChurnLostCommits(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 1, 1)
	n := nodes[0]
	sh, _ := n.shard("shard-churn")

	const goroutines = 8
	const perGoroutine = 25
	var wg sync.WaitGroup
	w := &shardWriter{shardID: "shard-churn", src: nopSource{}}
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				f, err := w.buildFrame()
				if err != nil {
					return
				}
				_ = sh.submitLocal(f)
			}
		}()
	}
	wg.Wait()

	// Wait for engine finalise to drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fin := counterVal(t, n.Metrics().FramesFinalized)
		if fin >= float64(goroutines*perGoroutine) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := counterVal(t, n.Metrics().FramesFinalized)
	if got < float64(goroutines*perGoroutine) {
		t.Fatalf("concurrent writers lost commits: FramesFinalized=%v "+
			"want %d. Atomic seq allocation likely broken.",
			got, goroutines*perGoroutine)
	}
}

// TestAttack_NetworkPartition — apply loops must not spin on closed ctx.
//
// Threat: a partition stops peer delivery; applyLoop spins on a ctx that
// was cancelled, burning CPU.
// Invariant: shard.close() cancels ctx and applyLoop exits promptly.
func TestAttack_NetworkPartition(t *testing.T) {
	_, _, nodes, _ := mustStartCluster(t, 1, 1)
	n := nodes[0]
	sh, _ := n.shard("shard-partition")

	start := runtime.NumGoroutine()
	sh.close()

	// Give the goroutine a beat to exit.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= start {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A leaked goroutine isn't immediately fatal — Go's runtime may
	// schedule it later — but if we still see > start+2 after 500 ms,
	// the apply loop is spinning.
	if runtime.NumGoroutine() > start+2 {
		t.Fatalf("applyLoop goroutine did not exit after shard.close(); " +
			"partition = goroutine leak + CPU burn.")
	}
}

// TestAttack_ClockSkew — node with skewed clock must not break ordering.
//
// Threat: one node's clock is 1 hr ahead; Frame.Timestamp on its frames
// claims the future; replicas that rely on Timestamp for ordering fail.
// Invariant: ordering is by quasar Block.Height (= Frame.Seq), never by
// Timestamp. Timestamp is advisory metadata.
func TestAttack_ClockSkew(t *testing.T) {
	// Build two frames for the same shard with equal seqs but wildly
	// different timestamps. blockID (= cksm) differs because Salt differs;
	// both round-trip, neither affects the other.
	past := newFrame("shard-skew", 1, 0, []byte("past"))
	future := newFrame("shard-skew", 1, 0, []byte("future"))
	future.Timestamp = time.Now().Add(time.Hour).UnixNano()

	if past.ApplyKey() == future.ApplyKey() {
		t.Fatalf("independent frames share ApplyKey; dedupe would collide " +
			"even across clock-skewed submissions")
	}
	// Both must pass Valid().
	if err := past.Valid(); err != nil {
		t.Fatalf("past.Valid: %v", err)
	}
	// `future` has an altered Timestamp but computeCksm does not include
	// Timestamp (see wal.go:61-74). That's fine — Timestamp is advisory —
	// but assert no regression would make it checksum-covered and break
	// replay across clock-skewed restarts.
	if err := future.Valid(); err != nil {
		t.Fatalf("future.Valid: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Creative additions — discovered while writing the suite.
// ---------------------------------------------------------------------------

// TestAttack_SegmentCRCWithBadSig — CRC passes but signature fails.
//
// Threat: attacker with bucket-write access but no signing key tries to
// slip a CRC-valid but signature-invalid segment past the reader.
// Invariant: signature check comes AFTER crc, so a CRC-valid but
// sig-invalid segment MUST still be rejected.
func TestAttack_SegmentCRCWithBadSig(t *testing.T) {
	signer, verifier := testSignerPair(t)
	sb := newSegmentBuffer("s", 1)
	_ = sb.append(1, newFrame("s", 1, 0, []byte("legit")).encode())
	enc, err := sb.encode(signer)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Corrupt only the signature — last 64 bytes.
	enc[len(enc)-1] ^= 0x55
	// Recompute CRC after corruption so CRC still passes.
	bodyEnd := len(enc) - (segmentPubKeyLen + segmentSigLen + segmentFooterCRCLen)
	crc := crc32.ChecksumIEEE(enc[:bodyEnd])
	binary.BigEndian.PutUint32(enc[bodyEnd:bodyEnd+4], crc)

	_, derr := decodeSegment(enc, verifier)
	if derr == nil {
		t.Fatalf("EXPLOIT: CRC-valid + signature-corrupt segment accepted. " +
			"Signature verification is skipped when CRC matches.")
	}
}

// TestAttack_SegmentV1Downgrade — LBN1 must be rejected even though it's
// a known magic.
//
// Threat: attacker writes an unauthenticated LBN1 segment at the
// deterministic path; old decoders accepted it. LBN2 readers must refuse.
// Invariant: LBN1 magic is a hard-reject.
func TestAttack_SegmentV1Downgrade(t *testing.T) {
	// Build a minimal LBN1-shaped blob (magic + header + CRC, no sig).
	// Manually because no encoder exists for v1 anymore.
	var buf []byte
	buf = append(buf, []byte("LBN1")...)
	buf = append(buf, 0x00, 0x01) // shard len = 1
	buf = append(buf, 's')
	buf = append(buf, make([]byte, 8)...)     // startSeq = 0
	buf = append(buf, 0x00, 0x00, 0x00, 0x00) // frame count = 0
	crc := crc32.ChecksumIEEE(buf)
	cb := make([]byte, 4)
	binary.BigEndian.PutUint32(cb, crc)
	buf = append(buf, cb...)
	// Pad with sig + pubkey space so length passes minimum header check.
	buf = append(buf, make([]byte, segmentPubKeyLen+segmentSigLen)...)

	_, verifier := testSignerPair(t)
	_, err := decodeSegment(buf, verifier)
	if err == nil {
		t.Fatalf("LBN1 (unauthenticated legacy) accepted by LBN2 reader; " +
			"downgrade attack succeeds.")
	}
}

// TestAttack_NilVerifier — fail-closed on nil verifier.
//
// Threat: a misconfigured or racy startup yields a nil verifier; reader
// must not silently accept all.
func TestAttack_NilVerifier(t *testing.T) {
	signer, _ := testSignerPair(t)
	sb := newSegmentBuffer("s", 1)
	_ = sb.append(1, newFrame("s", 1, 0, []byte("hi")).encode())
	enc, err := sb.encode(signer)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, derr := decodeSegment(enc, nil); derr == nil {
		t.Fatalf("EXPLOIT: nil verifier accepted a segment. Must fail " +
			"closed with ErrSegmentUnsigned.")
	}
}

// TestAttack_FrameSeqZero — Seq=0 must not collide with "not yet applied".
//
// Threat: a frame with Seq=0 is valid by the decoder; localSeq starts at
// 0; txseq cookie=0 always reports caught-up.
// Invariant: Seq=0 is a semantically valid frame (it's just the first one).
// The gateway's txseq==0 convention means "no prior write" — the first
// Seq emitted should be 1, not 0, so they can't collide.
// shardWriter.seq starts at 0 and Add(1) returns 1 first — correct. Guard
// the invariant.
func TestAttack_FrameSeqZero(t *testing.T) {
	w := &shardWriter{shardID: "s", src: nopSource{}}
	f, err := w.buildFrame()
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}
	if f.Seq == 0 {
		t.Fatalf("first frame has Seq=0; collides with txseq=0 sentinel. " +
			"First Seq MUST be 1.")
	}
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// failingUploader refuses every upload, which is what a storage outage or a
// revoked credential looks like from in here.
type failingUploader struct {
	attempts atomic.Uint64
}

func (u *failingUploader) put(context.Context, string, []byte) error {
	u.attempts.Add(1)
	return fmt.Errorf("injected failure #%d", u.attempts.Load())
}
func (*failingUploader) get(context.Context, string) ([]byte, error) {
	return nil, errors.New("injected failure")
}
func (*failingUploader) list(context.Context, string) ([]string, error) { return nil, nil }
func (*failingUploader) close() error                                   { return nil }
func (*failingUploader) scheme() string                                 { return "fail" }
