// Copyright © 2026 Hanzo AI. MIT License.

package core_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/tasks/pkg/sdk/client"
)

// TestTasksEmbed boots a BaseApp with TASKS_EMBED=true and verifies the
// in-process Hanzo Tasks daemon is reachable over native ZAP.
//
// One Go binary, full workflow engine inside. No gRPC. No proxy. No
// upstream temporal.io. The same path Base apps take when they want
// durable workflows without running a sidecar tasksd container.
func TestTasksEmbed(t *testing.T) {
	port := freeTCPPort(t)
	t.Setenv("TASKS_EMBED", "true")
	t.Setenv("TASKS_EMBED_ZAP_PORT", strconv.Itoa(port))
	t.Setenv("TASKS_NAMESPACE", "default")

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if app == nil {
		t.Fatal("NewBaseApp returned nil")
	}

	c, err := client.Dial(client.Options{
		HostPort:    "127.0.0.1:" + strconv.Itoa(port),
		Namespace:   "default",
		DialTimeout: 3 * time.Second,
		CallTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("client.Dial: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	svc, status, err := c.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if svc != "tasks" || status != "ok" {
		t.Fatalf("Health = %q/%q, want tasks/ok", svc, status)
	}

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        "embed-smoke",
		TaskQueue: "default",
	}, "EmbedSmoke")
	if err != nil {
		t.Fatalf("ExecuteWorkflow: %v", err)
	}
	if run.GetID() == "" || run.GetRunID() == "" {
		t.Fatalf("missing IDs: %+v", run)
	}

	list, err := c.ListWorkflows(ctx, "", 10, nil)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	found := false
	for _, e := range list.Executions {
		if e.WorkflowID == run.GetID() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("workflow %s not in list", run.GetID())
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
