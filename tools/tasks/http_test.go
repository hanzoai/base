package tasks

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	server "github.com/hanzoai/tasks/pkg/tasks"
)

// TestHTTPSpeaksTasks runs the client's HTTP transport against the Hanzo Tasks
// server's own HTTP handler, so the paths and bodies it sends are the ones the
// server answers, not a copy of them. Each call is then read back from the
// server's store: a schedule the server did not record would have left the
// client ticking locally with no error anywhere.
func TestHTTPSpeaksTasks(t *testing.T) {
	// A unix socket path is capped at 107 bytes; t.TempDir() can exceed it.
	dir, err := os.MkdirTemp("", "tk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	srv, err := server.Embed(context.Background(), server.EmbedConfig{
		Address: filepath.Join(dir, "s.sock"),
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	t.Cleanup(func() { srv.Stop(context.Background()) })
	hs := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(hs.Close)
	view := srv.View(server.Principal{})

	c := New(hs.URL, "", nil)
	t.Cleanup(c.Stop)
	if err := c.Add("every", "30s", nil); err != nil {
		t.Fatalf("add interval: %v", err)
	}
	if err := c.Add("nightly", "0 3 * * *", nil); err != nil {
		t.Fatalf("add cron: %v", err)
	}

	got := map[string]server.Schedule{}
	rows, err := view.ListSchedules("default")
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	for _, s := range rows {
		got[s.ScheduleId] = s
	}
	if s := got["every"]; len(s.Spec.Interval) != 1 || s.Spec.Interval[0].Interval != "30s" {
		t.Errorf("every: spec %+v, want one 30s interval", s.Spec)
	}
	if s := got["nightly"]; len(s.Spec.CronString) != 1 || s.Spec.CronString[0] != "0 3 * * *" {
		t.Errorf("nightly: spec %+v, want cron 0 3 * * *", s.Spec)
	}
	for _, id := range []string{"every", "nightly"} {
		if a := got[id].Action; a.WorkflowType.Name != id || a.TaskQueue != taskQueue {
			t.Errorf("%s: action %+v, want workflow %q on queue %q", id, a, id, taskQueue)
		}
	}

	c.Remove("every")
	rows, err = view.ListSchedules("default")
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	var ids []string
	for _, s := range rows {
		ids = append(ids, s.ScheduleId)
	}
	sort.Strings(ids)
	if len(ids) != 1 || ids[0] != "nightly" {
		t.Errorf("after Remove(every) the server holds %v, want [nightly]", ids)
	}

	if err := c.Now("webhook.deliver", map[string]any{"id": "w1"}); err != nil {
		t.Fatalf("now: %v", err)
	}
	wfs, err := view.ListWorkflows("default")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	if len(wfs) != 1 || wfs[0].Type.Name != "webhook.deliver" || wfs[0].TaskQueue != taskQueue {
		t.Fatalf("server started %+v, want one webhook.deliver on queue %q", wfs, taskQueue)
	}
}
