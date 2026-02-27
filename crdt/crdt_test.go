package crdt

import (
	"encoding/json"
	"sync"
	"testing"
)

// -------------------------------------------------------------------
// GCounter tests
// -------------------------------------------------------------------

func TestGCounter_IncrementAndValue(t *testing.T) {
	g := NewGCounter()
	g.Increment("A", 3)
	g.Increment("B", 5)
	g.Increment("A", 2)

	if v := g.Value(); v != 10 {
		t.Fatalf("expected 10, got %d", v)
	}
}

func TestGCounter_Merge(t *testing.T) {
	g1 := NewGCounter()
	g1.Increment("A", 3)
	g1.Increment("B", 2)

	g2 := NewGCounter()
	g2.Increment("A", 1)
	g2.Increment("B", 5)
	g2.Increment("C", 4)

	g1.Merge(g2)

	// A: max(3,1)=3, B: max(2,5)=5, C: max(0,4)=4 => 12
	if v := g1.Value(); v != 12 {
		t.Fatalf("expected 12, got %d", v)
	}
}

func TestGCounter_MergeConverges(t *testing.T) {
	g1 := NewGCounter()
	g2 := NewGCounter()

	g1.Increment("A", 10)
	g2.Increment("B", 20)

	// merge both ways
	g1copy := NewGCounter()
	g1copy.Merge(g1)
	g1copy.Merge(g2)

	g2copy := NewGCounter()
	g2copy.Merge(g2)
	g2copy.Merge(g1)

	if g1copy.Value() != g2copy.Value() {
		t.Fatalf("convergence failed: %d != %d", g1copy.Value(), g2copy.Value())
	}
}

// -------------------------------------------------------------------
// PNCounter tests
// -------------------------------------------------------------------

func TestPNCounter_IncrementDecrement(t *testing.T) {
	pn := NewPNCounter()
	pn.Increment("A", 10)
	pn.Decrement("B", 3)

	if v := pn.Value(); v != 7 {
		t.Fatalf("expected 7, got %d", v)
	}
}

func TestPNCounter_MergeConverges(t *testing.T) {
	pn1 := NewPNCounter()
	pn2 := NewPNCounter()

	pn1.Increment("A", 10)
	pn1.Decrement("A", 2)
	pn2.Increment("B", 5)
	pn2.Decrement("B", 1)

	r1 := NewPNCounter()
	r1.Merge(pn1)
	r1.Merge(pn2)

	r2 := NewPNCounter()
	r2.Merge(pn2)
	r2.Merge(pn1)

	if r1.Value() != r2.Value() {
		t.Fatalf("convergence failed: %d != %d", r1.Value(), r2.Value())
	}
	// (10-2) + (5-1) = 12
	if r1.Value() != 12 {
		t.Fatalf("expected 12, got %d", r1.Value())
	}
}

// -------------------------------------------------------------------
// LWWRegister tests
// -------------------------------------------------------------------

func TestLWWRegister_SetGet(t *testing.T) {
	r := NewLWWRegister()
	r.Set("first", Timestamp{Time: 1, NodeID: "A"})
	r.Set("second", Timestamp{Time: 2, NodeID: "A"})

	val, ts := r.Get()
	if val != "second" || ts.Time != 2 {
		t.Fatalf("expected 'second' at time 2, got %v at %v", val, ts)
	}
}

func TestLWWRegister_ConcurrentWritesBiasHigherTimestamp(t *testing.T) {
	r := NewLWWRegister()
	r.Set("old", Timestamp{Time: 5, NodeID: "A"})
	r.Set("new-but-older", Timestamp{Time: 3, NodeID: "B"})

	val, _ := r.Get()
	if val != "old" {
		t.Fatalf("expected 'old' to win (higher timestamp), got %v", val)
	}
}

func TestLWWRegister_MergeConverges(t *testing.T) {
	r1 := NewLWWRegister()
	r2 := NewLWWRegister()

	r1.Set("from-A", Timestamp{Time: 5, NodeID: "A"})
	r2.Set("from-B", Timestamp{Time: 3, NodeID: "B"})

	c1 := NewLWWRegister()
	c1.Merge(r1)
	c1.Merge(r2)

	c2 := NewLWWRegister()
	c2.Merge(r2)
	c2.Merge(r1)

	v1, _ := c1.Get()
	v2, _ := c2.Get()
	if v1 != v2 {
		t.Fatalf("convergence failed: %v != %v", v1, v2)
	}
	if v1 != "from-A" {
		t.Fatalf("expected 'from-A', got %v", v1)
	}
}

// -------------------------------------------------------------------
// ORSet tests
// -------------------------------------------------------------------

func TestORSet_AddContainsRemove(t *testing.T) {
	s := NewORSet("A")
	s.Add("x", 1)
	s.Add("y", 2)

	if !s.Contains("x") {
		t.Fatal("expected x to be present")
	}
	if !s.Contains("y") {
		t.Fatal("expected y to be present")
	}

	s.Remove("x")
	if s.Contains("x") {
		t.Fatal("expected x to be removed")
	}
	if !s.Contains("y") {
		t.Fatal("expected y to still be present")
	}
}

func TestORSet_MergeConverges(t *testing.T) {
	s1 := NewORSet("A")
	s2 := NewORSet("B")

	s1.Add("x", 1)
	s1.Add("y", 2)
	s2.Add("y", 3)
	s2.Add("z", 4)

	// remove y from s1 before merge
	s1.Remove("y")

	// merge s2 into s1: s2's "y" tags should re-add it (add-wins)
	s1.Merge(s2)

	if !s1.Contains("x") {
		t.Fatal("expected x after merge")
	}
	if !s1.Contains("y") {
		t.Fatal("expected y after merge (add-wins)")
	}
	if !s1.Contains("z") {
		t.Fatal("expected z after merge")
	}
}

func TestORSet_MergeSymmetric(t *testing.T) {
	s1 := NewORSet("A")
	s2 := NewORSet("B")

	s1.Add("a", nil)
	s2.Add("b", nil)

	r1 := NewORSet("R1")
	r1.Merge(s1)
	r1.Merge(s2)

	r2 := NewORSet("R2")
	r2.Merge(s2)
	r2.Merge(s1)

	e1 := r1.Elements()
	e2 := r2.Elements()

	if len(e1) != len(e2) {
		t.Fatalf("convergence failed: sizes differ %d vs %d", len(e1), len(e2))
	}
	for k := range e1 {
		if _, ok := e2[k]; !ok {
			t.Fatalf("convergence failed: key %q missing from r2", k)
		}
	}
}

// -------------------------------------------------------------------
// MVRegister tests
// -------------------------------------------------------------------

func TestMVRegister_DominatedWrite(t *testing.T) {
	mv := NewMVRegister()
	mv.Set("first", Timestamp{Time: 1, NodeID: "A"})
	mv.Set("second", Timestamp{Time: 2, NodeID: "A"})

	entries := mv.Get()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Value != "second" {
		t.Fatalf("expected 'second', got %v", entries[0].Value)
	}
}

func TestMVRegister_MergeConverges(t *testing.T) {
	mv1 := NewMVRegister()
	mv2 := NewMVRegister()

	mv1.Set("from-1", Timestamp{Time: 5, NodeID: "A"})
	mv2.Set("from-2", Timestamp{Time: 3, NodeID: "B"})

	r1 := NewMVRegister()
	r1.Merge(mv1)
	r1.Merge(mv2)

	r2 := NewMVRegister()
	r2.Merge(mv2)
	r2.Merge(mv1)

	e1 := r1.Get()
	e2 := r2.Get()
	if len(e1) != len(e2) {
		t.Fatalf("convergence failed: %d vs %d entries", len(e1), len(e2))
	}
}

// -------------------------------------------------------------------
// RGA (text) tests
// -------------------------------------------------------------------

func TestRGA_InsertAndToString(t *testing.T) {
	rga := NewRGA("A")
	rga.Insert(-1, 'H')
	rga.Insert(0, 'i')

	if s := rga.ToString(); s != "Hi" {
		t.Fatalf("expected 'Hi', got %q", s)
	}
}

func TestRGA_InsertText(t *testing.T) {
	rga := NewRGA("A")
	rga.InsertText(-1, "Hello")

	if s := rga.ToString(); s != "Hello" {
		t.Fatalf("expected 'Hello', got %q", s)
	}
}

func TestRGA_Delete(t *testing.T) {
	rga := NewRGA("A")
	rga.InsertText(-1, "abc")
	if _, err := rga.Delete(1); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if s := rga.ToString(); s != "ac" {
		t.Fatalf("expected 'ac', got %q", s)
	}
}

func TestRGA_MergeConverges(t *testing.T) {
	rga1 := NewRGA("A")
	rga2 := NewRGA("B")

	rga1.InsertText(-1, "Hello")
	rga2.InsertText(-1, "World")

	r1 := NewRGA("R1")
	if err := r1.Merge(rga1); err != nil {
		t.Fatalf("merge rga1 into r1: %v", err)
	}
	if err := r1.Merge(rga2); err != nil {
		t.Fatalf("merge rga2 into r1: %v", err)
	}

	r2 := NewRGA("R2")
	if err := r2.Merge(rga2); err != nil {
		t.Fatalf("merge rga2 into r2: %v", err)
	}
	if err := r2.Merge(rga1); err != nil {
		t.Fatalf("merge rga1 into r2: %v", err)
	}

	s1 := r1.ToString()
	s2 := r2.ToString()
	if s1 != s2 {
		t.Fatalf("convergence failed: %q != %q", s1, s2)
	}

	if r1.Length() != 10 {
		t.Fatalf("expected length 10, got %d (text: %q)", r1.Length(), s1)
	}
}

func TestRGA_ConcurrentInsertSamePosition(t *testing.T) {
	rga1 := NewRGA("A")
	rga2 := NewRGA("B")

	op1 := rga1.Insert(-1, 'x')
	op2 := rga2.Insert(-1, 'y')

	if err := rga1.ApplyOp(RGAOp{
		Type:     OpInsert,
		ID:       op2.ID,
		ParentID: op2.ParentID,
		Char:     op2.Char,
	}); err != nil {
		t.Fatalf("apply op2 to rga1: %v", err)
	}
	if err := rga2.ApplyOp(RGAOp{
		Type:     OpInsert,
		ID:       op1.ID,
		ParentID: op1.ParentID,
		Char:     op1.Char,
	}); err != nil {
		t.Fatalf("apply op1 to rga2: %v", err)
	}

	s1 := rga1.ToString()
	s2 := rga2.ToString()
	if s1 != s2 {
		t.Fatalf("convergence failed: %q != %q", s1, s2)
	}
	if len(s1) != 2 {
		t.Fatalf("expected 2 chars, got %d: %q", len(s1), s1)
	}
}

func TestRGA_ApplyOpIdempotent(t *testing.T) {
	rga := NewRGA("A")
	op := rga.Insert(-1, 'x')

	err := rga.ApplyOp(op)
	if err != nil {
		t.Fatalf("idempotent apply should not fail: %v", err)
	}

	if s := rga.ToString(); s != "x" {
		t.Fatalf("expected 'x', got %q", s)
	}
}

func TestRGA_StateVectorAndOpsSince(t *testing.T) {
	rga := NewRGA("A")
	rga.InsertText(-1, "abc")

	sv := rga.StateVector()
	if sv["A"] != 3 {
		t.Fatalf("expected sv[A]=3, got %d", sv["A"])
	}

	rga.Insert(2, 'd')

	ops := rga.OpsSince(sv)
	if len(ops) != 1 {
		t.Fatalf("expected 1 op since sv, got %d", len(ops))
	}
}

// -------------------------------------------------------------------
// Document tests
// -------------------------------------------------------------------

func TestDocument_TextFieldMerge(t *testing.T) {
	doc1 := NewDocument("test", "A")
	doc2 := NewDocument("test", "B")

	doc1.GetText("title").InsertText(-1, "Hello")
	doc2.GetText("title").InsertText(-1, "World")

	doc1.Merge(doc2)
	doc2.Merge(doc1)

	s1 := doc1.GetText("title").ToString()
	s2 := doc2.GetText("title").ToString()
	if s1 != s2 {
		t.Fatalf("document text convergence failed: %q != %q", s1, s2)
	}
}

func TestDocument_CounterFieldMerge(t *testing.T) {
	doc1 := NewDocument("test", "A")
	doc2 := NewDocument("test", "B")

	doc1.GetCounter("votes").Increment("A", 5)
	doc2.GetCounter("votes").Increment("B", 3)

	doc1.Merge(doc2)
	if v := doc1.GetCounter("votes").Value(); v != 8 {
		t.Fatalf("expected counter=8, got %d", v)
	}
}

func TestDocument_SetFieldMerge(t *testing.T) {
	doc1 := NewDocument("test", "A")
	doc2 := NewDocument("test", "B")

	doc1.GetSet("tags").Add("go", nil)
	doc2.GetSet("tags").Add("rust", nil)

	doc1.Merge(doc2)
	elems := doc1.GetSet("tags").Elements()
	if len(elems) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(elems))
	}
}

func TestDocument_RegisterFieldMerge(t *testing.T) {
	doc1 := NewDocument("test", "A")
	doc2 := NewDocument("test", "B")

	doc1.GetRegister("status").Set("draft", Timestamp{Time: 1, NodeID: "A"})
	doc2.GetRegister("status").Set("published", Timestamp{Time: 2, NodeID: "B"})

	doc1.Merge(doc2)
	val, _ := doc1.GetRegister("status").Get()
	if val != "published" {
		t.Fatalf("expected 'published', got %v", val)
	}
}

func TestDocument_EncodeDecodeRoundTrip(t *testing.T) {
	doc := NewDocument("test-doc", "nodeA")
	doc.GetText("body").InsertText(-1, "Hello World")
	doc.GetCounter("views").Increment("nodeA", 42)
	doc.GetSet("tags").Add("crdt", nil)
	doc.GetRegister("title").Set("My Doc", Timestamp{Time: 1, NodeID: "nodeA"})

	data, err := doc.Encode()
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	doc2, err := Decode(data, "nodeB")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if doc2.ID() != "test-doc" {
		t.Fatalf("expected ID 'test-doc', got %q", doc2.ID())
	}

	if s := doc2.GetText("body").ToString(); s != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", s)
	}

	if v := doc2.GetCounter("views").Value(); v != 42 {
		t.Fatalf("expected counter=42, got %d", v)
	}
}

// -------------------------------------------------------------------
// SyncManager tests
// -------------------------------------------------------------------

func TestSyncManager_SyncProtocol(t *testing.T) {
	var broadcastMu sync.Mutex
	var broadcasts [][]byte

	sm := NewSyncManager(func(docID, excludeClient string, msg []byte) {
		broadcastMu.Lock()
		broadcasts = append(broadcasts, msg)
		broadcastMu.Unlock()
	})

	doc := NewDocument("doc1", "server")
	doc.GetText("content").InsertText(-1, "server-text")
	sm.RegisterDocument("doc1", doc)

	step1 := `{"type":"sync_step1","docId":"doc1","stateVector":{}}`
	resp, err := sm.HandleSync("client-1", []byte(step1))
	if err != nil {
		t.Fatalf("sync step 1 failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response for sync step 1")
	}

	var msg SyncMessage
	if err := json.Unmarshal(resp, &msg); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if msg.Type != SyncStep2 {
		t.Fatalf("expected sync_step2, got %s", msg.Type)
	}
	if len(msg.StateVector) == 0 {
		t.Fatal("expected non-empty state vector in response")
	}
}

func TestSyncManager_GetOrCreateDocument(t *testing.T) {
	sm := NewSyncManager(nil)

	doc1 := sm.GetOrCreateDocument("new-doc", "node1")
	if doc1 == nil {
		t.Fatal("expected non-nil document")
	}

	doc2 := sm.GetOrCreateDocument("new-doc", "node2")
	if doc1 != doc2 {
		t.Fatal("expected same document instance")
	}
}

// -------------------------------------------------------------------
// StateVersion tests
// -------------------------------------------------------------------

func TestStateVersion_Dominates(t *testing.T) {
	v1 := StateVersion{"A": 5, "B": 3}
	v2 := StateVersion{"A": 3, "B": 2}

	if !v1.Dominates(v2) {
		t.Fatal("expected v1 to dominate v2")
	}
	if v2.Dominates(v1) {
		t.Fatal("expected v2 to NOT dominate v1")
	}
}

func TestStateVersion_Merge(t *testing.T) {
	v1 := StateVersion{"A": 5, "B": 3}
	v2 := StateVersion{"A": 3, "C": 7}

	merged := v1.Merge(v2)
	if merged["A"] != 5 || merged["B"] != 3 || merged["C"] != 7 {
		t.Fatalf("unexpected merge result: %v", merged)
	}
}

// -------------------------------------------------------------------
// Concurrent safety tests
// -------------------------------------------------------------------

func TestGCounter_ConcurrentAccess(t *testing.T) {
	g := NewGCounter()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Increment("node", 1)
		}()
	}

	wg.Wait()
	if v := g.Value(); v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}
}

func TestORSet_ConcurrentAccess(t *testing.T) {
	s := NewORSet("A")
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := string(rune('a' + n%26))
			s.Add(key, n)
		}(i)
	}

	wg.Wait()
	elems := s.Elements()
	if len(elems) == 0 {
		t.Fatal("expected non-empty set")
	}
}
