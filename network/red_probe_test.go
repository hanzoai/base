package network

// Red team probes. Each test demonstrates a concrete defect in the code under
// review; all tests live in the _test.go file so they are never compiled into
// a production binary.
//
// Run with:
//
//	go test -run TestRedProbe ./...

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestRedProbe_EnvelopeShardConfusion exercises the following attack:
//
// The transport carries an Envelope { ShardID string; Frame Frame }. Node
// code routes by Envelope.ShardID:
//
//	onPeerFrame(env) → n.shard(env.ShardID) → s.ingestRemote(env.Frame)
//
// Frame.Valid() only verifies the checksum, which is over the Frame's
// *internal* ShardID — an attacker can set:
//
//	Envelope.ShardID  = "victim-shard"       (routes into victim Shard{})
//	Frame.ShardID     = "attacker-shard"     (cksm binds to this)
//
// Both values are under attacker control on the wire. The frame validates,
// enters the victim Shard's Quasar engine, finalises, and is handed to the
// victim's apply callback with the attacker's payload. Once apply wires a
// real SQLite write, that write lands in the victim's database.
//
// Today's Shard.applyLoop also bumps Shard.localSeq = f.Seq unconditionally.
// An attacker-chosen f.Seq lets the attacker advance the victim shard's
// local sequence number to any uint64, corrupting read-your-writes state.
func TestRedProbe_EnvelopeShardConfusion(t *testing.T) {
	hub := newMemoryHub()
	peers := []string{"a", "b"}
	var victim *node

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, id := range peers {
		cfg := Config{
			Enabled:     true,
			ShardKey:    "user_id",
			Replication: 2,
			Peers:       filter(peers, id),
			NodeID:      id,
			Role:        RoleValidator,
			Archive:     "off",
			ListenHTTP:  ":0",
			ListenP2P:   ":0",
		}
		nn, err := newNodeWithTransport(cfg, hub.connect(NodeID(id)))
		if err != nil {
			t.Fatalf("node %s: %v", id, err)
		}
		if err := nn.Start(ctx); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		t.Cleanup(func() { _ = nn.Stop(context.Background()) })
		if id == "b" {
			victim = nn
		}
	}

	// Force creation of the victim shard so we can observe its localSeq.
	victimShard, err := victim.shard("victim-shard")
	if err != nil {
		t.Fatalf("shard: %v", err)
	}
	preSeq := victimShard.LocalSeq()

	// Build a frame whose *internal* shardID is "attacker-shard", but
	// publish it under ShardID=victim-shard. The checksum binds the inner
	// shardID so Valid() passes.
	const wildSeq uint64 = 0x7FFF_FFFF_FFFF_FFF0
	evil := newFrame("attacker-shard", wildSeq, 0, []byte("OWNED"))

	// Simulate the transport delivering this envelope to the victim.
	victim.onPeerFrame(Envelope{ShardID: "victim-shard", Frame: evil})

	// Wait for the apply loop to process the finalised frame.
	deadline := time.Now().Add(2 * time.Second)
	for victimShard.LocalSeq() == preSeq && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}

	post := victimShard.LocalSeq()
	if post == preSeq {
		t.Log("note: attacker frame has NOT reached apply within 2s; the " +
			"quasar engine may reject submission when Block.Data's decoded " +
			"shardID diverges from the engine's. If the fix ensures strict " +
			"binding, this test is the proof it's working.")
		t.Skip("apply did not observe the frame; fix may already be in place")
	}

	if post != wildSeq {
		t.Fatalf("victim localSeq = %d, want attacker-chosen %d (confusion confirmed but less severe)",
			post, wildSeq)
	}
	// If we got here, the victim shard's localSeq has been corrupted to
	// the attacker-chosen value using a frame whose internal shardID was
	// a different shard entirely. This is a cross-shard state-injection
	// vulnerability. The same code path, once apply is wired to sqlite,
	// will write the attacker's payload to the victim's database.
	t.Fatalf("EXPLOIT CONFIRMED: victim 'victim-shard' localSeq advanced to %d "+
		"via a frame whose internal ShardID was 'attacker-shard'. Once "+
		"apply is wired to SQLite this is full cross-shard data injection.",
		post)
}

// TestRedProbe_SelfForgedLocalSeqOverflow shows that an attacker who can
// submit a frame for their own shard can still jump the local sequence
// counter past 2^63 via frame.Seq being attacker-chosen on ingestRemote.
//
// Even without shard confusion (legit use-case: "attacker.submit(own
// shard, seq=2^63)"), the victim's local writer's w.seq is advanced on
// the NEXT write via atomic.Uint64.Add — no, wait — the LOCAL writer's
// w.seq is independent of Shard.localSeq. But Shard.localSeq is the
// signal the gateway uses for read-your-writes (`txseq`). Once this is
// pushed to 2^63-1, subsequent real finalisations (with small seqs) no
// longer bump localSeq. Clients whose cookie carries a real txseq (say
// 42) will see localSeq ≥ 42 and get served whatever stale state the
// victim has — reads don't block for catch-up because the shard already
// claims to be arbitrarily far ahead.
func TestRedProbe_SelfForgedLocalSeqOverflow(t *testing.T) {
	hub := newMemoryHub()
	peers := []string{"a", "b"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var target *node
	for _, id := range peers {
		cfg := Config{
			Enabled: true, ShardKey: "user_id", Replication: 2,
			Peers: filter(peers, id), NodeID: id,
			Role: RoleValidator, Archive: "off",
			ListenHTTP: ":0", ListenP2P: ":0",
		}
		nn, err := newNodeWithTransport(cfg, hub.connect(NodeID(id)))
		if err != nil {
			t.Fatalf("node %s: %v", id, err)
		}
		if err := nn.Start(ctx); err != nil {
			t.Fatalf("start: %v", err)
		}
		t.Cleanup(func() { _ = nn.Stop(context.Background()) })
		if id == "b" {
			target = nn
		}
	}

	sh, _ := target.shard("shard-foo")
	wildSeq := uint64(1) << 62
	evil := newFrame("shard-foo", wildSeq, 0, []byte("ghost"))
	target.onPeerFrame(Envelope{ShardID: "shard-foo", Frame: evil})

	deadline := time.Now().Add(2 * time.Second)
	for sh.LocalSeq() < wildSeq && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if got := sh.LocalSeq(); got < wildSeq {
		t.Skipf("frame did not propagate into localSeq (got %d); the "+
			"quasar engine or the apply path may ignore it", got)
	}
	// Honest writer now submits seq=1 — will localSeq get pulled back? No;
	// `if f.Seq > s.localSeq { s.localSeq = f.Seq }` is one-way.
	honest := newFrame("shard-foo", 1, 0, []byte("honest"))
	_ = sh.submitLocal(honest)

	time.Sleep(200 * time.Millisecond)

	if sh.LocalSeq() < wildSeq {
		t.Fatalf("localSeq %d < attacker-chosen %d — unexpected pullback",
			sh.LocalSeq(), wildSeq)
	}
	// localSeq is stuck at 2^62. A gateway serving reads with txseq=1 for
	// an honest client will report "caught up" without ever applying the
	// honest frame.
	t.Fatalf("EXPLOIT: localSeq=%d after attacker seq=%d. Read-your-writes "+
		"invariant violated: shard reports caught-up for arbitrarily high "+
		"txseq cookies while holding stale state.", sh.LocalSeq(), wildSeq)
}

// TestRedProbe_UnboundedBacklog drives the archive writer with a uploader
// that always fails. The writer re-buffers every segment forever. Memory
// grows without bound — no cap on backlog size or lag bytes. A hostile
// backend (S3 outage, bucket deleted, creds revoked) turns into an OOM.
func TestRedProbe_UnboundedBacklog(t *testing.T) {
	fail := &failingUploader{}
	w := newArchiveWriter(fail, "svc", ArchiveConfig{
		SegmentTargetBytes: 64,
		FlushInterval:      5 * time.Millisecond,
		RetryDeadline:      50 * time.Millisecond, // short so we don't wait forever
	}, nil)
	t.Cleanup(func() { _ = w.Close() })

	ctx := context.Background()
	for i := uint64(1); i <= 2000; i++ {
		f := newFrame("shard-A", i, i-1,
			[]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		if err := w.Append(ctx, "shard-A", i, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// Let the flush loop churn.
	time.Sleep(1200 * time.Millisecond)

	w.mu.Lock()
	q := w.shards["shard-A"]
	backlogLen := len(q.backlog)
	totalBytes := 0
	for _, b := range q.backlog {
		totalBytes += len(b.data)
	}
	w.mu.Unlock()

	t.Logf("after 2000 appends against failing uploader: "+
		"backlog=%d segments, bytes=%d", backlogLen, totalBytes)
	if backlogLen < 10 {
		t.Fatalf("backlog did not accumulate as expected (%d) — "+
			"test needs revisiting", backlogLen)
	}
	// Demonstrate no cap: even under persistent failure, we can just keep
	// pushing until RAM runs out.
	for i := uint64(2001); i <= 4000; i++ {
		f := newFrame("shard-A", i, i-1,
			[]byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
		if err := w.Append(ctx, "shard-A", i, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	w.mu.Lock()
	q = w.shards["shard-A"]
	newLen := len(q.backlog)
	newBytes := 0
	for _, b := range q.backlog {
		newBytes += len(b.data)
	}
	w.mu.Unlock()
	if newBytes <= totalBytes {
		t.Fatalf("backlog bytes did not grow under continued input: was %d, now %d",
			totalBytes, newBytes)
	}
	t.Logf("UNBOUNDED: after 4000 appends: backlog=%d segments, bytes=%d (no cap enforced)",
		newLen, newBytes)
	// Fail loudly so this shows up in the suite output.
	if newBytes > 10_000 {
		t.Fatalf("unbounded per-shard backlog (%d bytes). Under a real "+
			"hostile backend this grows until OOM", newBytes)
	}
}

type failingUploader struct {
	attempts atomic.Uint64
}

func (u *failingUploader) put(context.Context, string, []byte) error {
	u.attempts.Add(1)
	return fmt.Errorf("injected failure #%d", u.attempts.Load())
}
func (*failingUploader) get(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("no")
}
func (*failingUploader) list(context.Context, string) ([]string, error) { return nil, nil }
func (*failingUploader) close() error                                   { return nil }
func (*failingUploader) scheme() string                                 { return "fail" }

// TestRedProbe_ArchiveForgedSegment: an attacker with bucket write access
// forges an entire segment object with a chosen shardID / startSeq. The
// CRC-32 footer is not a MAC — the attacker trivially computes it. On
// PITR restore the tool reads the forged frames into the target DB.
//
// This test exercises the in-memory uploader, which is a fair model for
// "what happens if someone with bucket write access creates an arbitrary
// .lbn at the expected path". The PITR replay path runs the same decode
// logic against the same bytes.
func TestRedProbe_ArchiveForgedSegment(t *testing.T) {
	up := newMemUploader()
	w := newArchiveWriter(up, "svc", ArchiveConfig{
		SegmentTargetBytes: 8 << 20,
		FlushInterval:      time.Hour,
		RetryDeadline:      time.Second,
	}, nil)
	t.Cleanup(func() { _ = w.Close() })

	// Legitimate history: seq 1..10.
	ctx := context.Background()
	for i := uint64(1); i <= 10; i++ {
		f := newFrame("victim-shard", i, i-1, []byte(fmt.Sprintf("real-%d", i)))
		if err := w.Append(ctx, "victim-shard", i, f.encode()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	_ = w.Close()

	// Attacker forges a segment at seq 5000 using their own signing key.
	// Post-R3, the writer's verifier only trusts the archive-role key, so
	// the forged segment should fail signature verification on read.
	attackerSigner, _ := testSignerPair(t)
	sb := newSegmentBuffer("victim-shard", 5000)
	evil := newFrame("victim-shard", 5000, 4999, []byte("INJECTED-PITR-ROWS"))
	if err := sb.append(5000, evil.encode()); err != nil {
		t.Fatalf("append evil: %v", err)
	}
	enc, err := sb.encode(attackerSigner)
	if err != nil {
		t.Fatalf("encode evil segment: %v", err)
	}
	key := objectKey("svc", "victim-shard", 5000, time.Now().UnixNano())
	if err := up.put(ctx, key, enc); err != nil {
		t.Fatalf("put evil: %v", err)
	}

	// PITR driver reads frames back and replays them. The .Range iterator
	// yields the injected frame because the CRC passes (attacker wrote a
	// well-formed segment) and Frame.Valid() passes (attacker computed
	// their own payload hash). There is nothing the reader can check
	// against — the archive has no author-signed manifest.
	it, err := w.Range(ctx, "victim-shard", 1, 6000)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	var seen []uint64
	for f, ferr := range it {
		if ferr != nil {
			t.Fatalf("iter: %v", ferr)
		}
		if err := f.Valid(); err != nil {
			t.Fatalf("frame invalid: %v", err)
		}
		seen = append(seen, f.Seq)
	}
	found5000 := false
	for _, s := range seen {
		if s == 5000 {
			found5000 = true
			break
		}
	}
	if !found5000 {
		t.Fatalf("expected injected seq 5000 in Range output (seen %v)", seen)
	}
	t.Fatalf("EXPLOIT: forged segment (seq=5000, payload='INJECTED-PITR-ROWS') "+
		"accepted by Range; PITR restore will write attacker bytes into "+
		"the reconstructed SQLite. All seqs returned: %v", seen)
}
