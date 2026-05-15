package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hanzoai/base/cmd"
	"github.com/hanzoai/base/plugins/platform"
	"github.com/hanzoai/base/tests"
)

func TestIAMUserCreateCommand(t *testing.T) {
	t.Setenv("IAM_MODE", "embedded")
	t.Setenv("IAM_USER_PASSWORD", "test-password-123")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Cleanup()

	// Bring the _iam_users collection into existence the way the
	// platform plugin would on first boot.
	if err := platform.EnsureIAMUsersCollection(app); err != nil {
		t.Fatalf("ensure collection: %v", err)
	}

	root := cmd.NewIAMUserCommand(app)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"create", "z@example.com", "--name", "Z"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v; output=%s", err, out.String())
	}
	if got := out.String(); !strings.Contains(got, "created user") || !strings.Contains(got, "z@example.com") {
		t.Errorf("unexpected output: %q", got)
	}

	// User should now authenticate.
	if _, err := platform.AuthenticateEmbeddedIAMUser(app, "z@example.com", "test-password-123"); err != nil {
		t.Fatalf("authenticate after create: %v", err)
	}
}

func TestIAMUserCreateCommand_MissingPasswordEnv(t *testing.T) {
	// No IAM_USER_PASSWORD and no stdin → should fail cleanly, not panic.
	t.Setenv("IAM_USER_PASSWORD", "")
	t.Setenv("IAM_MODE", "embedded")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Cleanup()
	if err := platform.EnsureIAMUsersCollection(app); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	root := cmd.NewIAMUserCommand(app)
	root.SetIn(strings.NewReader("\n")) // empty password line
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"create", "z@example.com"})

	// We don't assert on the *exact* error string — we just want this
	// branch to error rather than panic or silently create a user.
	if err := root.Execute(); err == nil {
		// If somehow it succeeded, the test must fail loudly.
		t.Fatalf("expected create to fail with empty password, got success: %s", out.String())
	}
}
