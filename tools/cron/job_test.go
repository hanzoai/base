package cron

import (
	"encoding/json"
	"testing"
)

func TestJobId(t *testing.T) {
	j := Job{id: "test"}
	if j.Id() != "test" {
		t.Fatalf("expected id=test, got %q", j.Id())
	}
}

func TestJobExpression(t *testing.T) {
	j := Job{expression: "1 2 3 4 5"}
	if j.Expression() != "1 2 3 4 5" {
		t.Fatalf("expected expression=1 2 3 4 5, got %q", j.Expression())
	}
}

func TestJobRunNoClient(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Run on zero-value Job must not panic: %v", r)
		}
	}()
	(&Job{}).Run()
}

func TestJobRunThroughClient(t *testing.T) {
	c := New()
	defer c.Stop()

	calls := 0
	if err := c.Add("probe", "24h", func() { calls++ }); err != nil {
		t.Fatal(err)
	}
	jobs := c.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	jobs[0].Run()
	if calls != 1 {
		t.Fatalf("expected Run to invoke callback once, got %d", calls)
	}
}

func TestJobMarshalJSON(t *testing.T) {
	j := Job{id: "test_id", expression: "1 2 3 4 5"}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"test_id","expression":"1 2 3 4 5"}`
	if string(raw) != want {
		t.Fatalf("expected %s, got %s", want, string(raw))
	}
}
