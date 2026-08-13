package org

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/subscriptions"
	"github.com/hanzoai/base/tools/types"
)

// spy is a realtime subscriber on one Base, and what that Base sent it.
type spy struct {
	client *subscriptions.DefaultClient

	mu   sync.Mutex
	got  []string
	done chan struct{}
	wg   sync.WaitGroup
}

// watch subscribes a guest client to sub on app's broker and starts reading its
// channel. The reader has to be running before the write: a broadcast is
// delivered by a send on an unbuffered channel.
func watch(t *testing.T, app core.App, sub string) *spy {
	t.Helper()

	s := &spy{client: subscriptions.NewDefaultClient(), done: make(chan struct{})}
	s.client.Subscribe(sub)
	app.SubscriptionsBroker().Register(s.client)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case msg, ok := <-s.client.Channel():
				if !ok {
					return
				}
				var data struct{ Action string }
				_ = json.Unmarshal(msg.Data, &data)
				s.mu.Lock()
				s.got = append(s.got, data.Action)
				s.mu.Unlock()
			case <-s.done:
				return
			}
		}
	}()

	t.Cleanup(func() {
		close(s.done)
		s.wg.Wait()
	})

	return s
}

func (s *spy) actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

// await returns once the subscriber has been sent n messages, or reports what it
// has after waiting. Sends are fired and forgotten, so there is nothing to join.
func (s *spy) await(n int) []string {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.actions(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s.actions()
}

// quiet returns what the subscriber was sent once the sends have had time to
// arrive. It is how a count of zero, or a count of exactly one, is asserted:
// waiting for a message that should not come is the only way to know it did not.
func (s *spy) quiet() []string {
	time.Sleep(300 * time.Millisecond)
	return s.actions()
}

// tenant opens org's Base through the registry and gives it one collection,
// which is what a subscriber can be sent a record from. rule is the collection's
// list rule, so a caller can choose between a rule that admits everyone without
// asking the database and one that has to query it.
func tenant(t *testing.T, b *bases, org string, rule string) (core.App, *core.Collection) {
	t.Helper()

	app, err := b.base(org)
	if err != nil {
		t.Fatal(err)
	}

	c := core.NewBaseCollection("posts")
	c.ListRule = types.Pointer(rule)
	c.ViewRule = types.Pointer(rule)
	c.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}

	return app, c
}

func write(t *testing.T, app core.App, c *core.Collection, title string) {
	t.Helper()

	r := core.NewRecord(c)
	r.Set("title", title)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
}

func registry(t *testing.T) *bases {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	b := newBases(&plugin{app: app, orgDB: NewOrgDB(app, "")})
	t.Cleanup(b.close)

	return b
}

// A subscriber on an org's Base is sent what is written to that Base.
//
// A subscriber registers on the broker of the Base its request resolved to, and
// for anyone in an org that is the org's own Base. The write that reaches it is
// a write to the same file.
func TestOrgBasePublishes(t *testing.T) {
	b := registry(t)

	app, c := tenant(t, b, "acme", "")
	s := watch(t, app, "posts/*")

	write(t, app, c, "hello")

	if got := s.await(1); len(got) != 1 || got[0] != "create" {
		t.Fatalf("the org's subscriber was sent %v, want one create", got)
	}
}

// One org's subscriber is sent nothing of another org's.
//
// Two orgs are two Bases and so two brokers, which is what keeps a topic name
// they both use from being one topic.
func TestOrgBasesDoNotCross(t *testing.T) {
	b := registry(t)

	acme, acmeposts := tenant(t, b, "acme", "")
	globex, _ := tenant(t, b, "globex", "")

	mine := watch(t, acme, "posts/*")
	theirs := watch(t, globex, "posts/*")

	write(t, acme, acmeposts, "hello")

	if got := mine.await(1); len(got) != 1 {
		t.Fatalf("acme's subscriber was sent %v, want one message", got)
	}
	if got := theirs.quiet(); len(got) != 0 {
		t.Fatalf("globex's subscriber was sent acme's writes: %v", got)
	}
}

// A rule is evaluated against the org's own Base.
//
// A rule that is not the empty string is a query, run against the rows of some
// Base to decide whether a subscriber may be sent this record. A delete runs
// that query outside the delete transaction, against a Base named separately
// from the one the record was written to, which is what this pins: the org's
// collection exists on the org's Base and nowhere else, so a check made
// anywhere else answers no and the subscriber is sent nothing.
func TestOrgBaseChecksRulesAgainstItself(t *testing.T) {
	b := registry(t)

	app, c := tenant(t, b, "acme", "title != ''")
	write(t, app, c, "hello")

	record, err := app.FindFirstRecordByData(c, "title", "hello")
	if err != nil {
		t.Fatal(err)
	}

	s := watch(t, app, "posts/*")
	if err := app.Delete(record); err != nil {
		t.Fatal(err)
	}

	if got := s.await(1); len(got) != 1 || got[0] != "delete" {
		t.Fatalf("a delete under a rule that queries sent %v, want one delete", got)
	}
}
