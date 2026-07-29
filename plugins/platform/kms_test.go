package platform

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeSecrets is a KMS stand-in that records the exact coordinate every call
// asks for. The coordinate is the whole point of these tests: the KMS store
// shards its tenant boundary on the "orgs/{org}" segment of the PATH, so a
// bridge that drops the org writes every tenant's material into one shared
// deployment-wide record.
type fakeSecrets struct {
	mu     sync.Mutex
	store  map[string]string
	reads  []string
	closed bool
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{store: map[string]string{}} }

func key(path, name, env string) string { return path + "|" + name + "|" + env }

func (f *fakeSecrets) GetAt(_ context.Context, path, name, env string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key(path, name, env)
	f.reads = append(f.reads, k)
	v, ok := f.store[k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (f *fakeSecrets) PutAt(_ context.Context, path, name, env, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[key(path, name, env)] = value
	return nil
}

func (f *fakeSecrets) DeleteAt(_ context.Context, path, name, env string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.store, key(path, name, env))
	return nil
}

func (f *fakeSecrets) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

// withFakeKMS points the dial seam at a fake for the duration of a test.
func withFakeKMS(t *testing.T) *fakeSecrets {
	t.Helper()
	f := newFakeSecrets()
	prev := dialKMS
	dialKMS = func(context.Context, string) (secrets, error) { return f, nil }
	t.Cleanup(func() { dialKMS = prev })
	return f
}

func TestKMSRefFoldsOrgIntoPath(t *testing.T) {
	cases := []struct {
		org, secret string
		wantPath    string
		wantName    string
	}{
		{"acme", "db-password", "orgs/acme", "db-password"},
		{"acme", "providers/stripe/api_key", "orgs/acme/providers/stripe", "api_key"},
		{"globex", "providers/stripe/api_key", "orgs/globex/providers/stripe", "api_key"},
		{"acme", "/leading/slash", "orgs/acme/leading", "slash"},
	}
	for _, c := range cases {
		path, name := ref(c.org, c.secret)
		if path != c.wantPath || name != c.wantName {
			t.Errorf("ref(%q,%q) = (%q,%q), want (%q,%q)",
				c.org, c.secret, path, name, c.wantPath, c.wantName)
		}
	}
}

// TestKMSSecretsAreTenantScoped is the regression guard. Two orgs storing the
// same logical secret must never collide on one KMS record.
func TestKMSSecretsAreTenantScoped(t *testing.T) {
	f := withFakeKMS(t)
	c := mustKMS(t, "kms.test:9999")

	if err := c.SetSecret("acme", "providers/stripe/api_key", "acme-key"); err != nil {
		t.Fatalf("SetSecret(acme): %v", err)
	}
	if err := c.SetSecret("globex", "providers/stripe/api_key", "globex-key"); err != nil {
		t.Fatalf("SetSecret(globex): %v", err)
	}

	if len(f.store) != 2 {
		t.Fatalf("two orgs wrote %d KMS record(s), want 2 — the org is missing from the coordinate: %v", len(f.store), f.store)
	}

	got, err := c.GetSecret("acme", "providers/stripe/api_key")
	if err != nil {
		t.Fatalf("GetSecret(acme): %v", err)
	}
	if got != "acme-key" {
		t.Errorf("acme read %q, want acme-key", got)
	}
	got, err = c.GetSecret("globex", "providers/stripe/api_key")
	if err != nil {
		t.Fatalf("GetSecret(globex): %v", err)
	}
	if got != "globex-key" {
		t.Errorf("globex read %q, want globex-key", got)
	}
}

func TestKMSGetSetDeleteRoundTrip(t *testing.T) {
	f := withFakeKMS(t)
	c := mustKMS(t, "zap://kms.test:9999")

	if err := c.SetSecret("org1", "db-password", "s3cret"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	v, err := c.GetSecret("org1", "db-password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if v != "s3cret" {
		t.Errorf("got %q, want s3cret", v)
	}

	// Second read is served from cache — no second hop to KMS.
	reads := len(f.reads)
	if _, err := c.GetSecret("org1", "db-password"); err != nil {
		t.Fatalf("cached GetSecret: %v", err)
	}
	if len(f.reads) != reads {
		t.Errorf("cached read hit KMS %d extra time(s)", len(f.reads)-reads)
	}

	if err := c.DeleteSecret("org1", "db-password"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if _, err := c.GetSecret("org1", "db-password"); err == nil {
		t.Error("read after delete succeeded")
	}
}

func TestKMSInvalidateCacheIsPerOrg(t *testing.T) {
	f := withFakeKMS(t)
	c := mustKMS(t, "kms.test:9999")

	for _, org := range []string{"acme", "globex"} {
		if err := c.SetSecret(org, "k", org); err != nil {
			t.Fatalf("SetSecret(%s): %v", org, err)
		}
		if _, err := c.GetSecret(org, "k"); err != nil {
			t.Fatalf("GetSecret(%s): %v", org, err)
		}
	}

	c.InvalidateCache("acme")
	reads := len(f.reads)
	if _, err := c.GetSecret("globex", "k"); err != nil {
		t.Fatalf("GetSecret(globex): %v", err)
	}
	if len(f.reads) != reads {
		t.Error("invalidating acme also dropped globex's cache entry")
	}
	if _, err := c.GetSecret("acme", "k"); err != nil {
		t.Fatalf("GetSecret(acme): %v", err)
	}
	if len(f.reads) == reads {
		t.Error("invalidating acme did not force a re-read")
	}
}

func TestKMSNotConfigured(t *testing.T) {
	c, err := NewKMSClient("")
	if err != nil {
		t.Fatalf("NewKMSClient(\"\"): %v", err)
	}
	if _, err := c.GetSecret("t1", "key"); !errors.Is(err, ErrKMSNotConfigured) {
		t.Errorf("GetSecret: %v, want ErrKMSNotConfigured", err)
	}
	if err := c.SetSecret("t1", "key", "val"); !errors.Is(err, ErrKMSNotConfigured) {
		t.Errorf("SetSecret: %v, want ErrKMSNotConfigured", err)
	}
	if err := c.DeleteSecret("t1", "key"); !errors.Is(err, ErrKMSNotConfigured) {
		t.Errorf("DeleteSecret: %v, want ErrKMSNotConfigured", err)
	}
}

// An http(s) endpoint is a misconfiguration, not a fallback: base speaks native
// ZAP to KMS and nothing else. It must fail at construction, where an operator
// sees it, rather than degrade every secret read into the env fallback.
func TestKMSRejectsHTTPEndpoint(t *testing.T) {
	for _, ep := range []string{"https://kms.hanzo.ai", "http://kms.hanzo.svc.cluster.local:8443"} {
		if _, err := NewKMSClient(ep); err == nil {
			t.Errorf("NewKMSClient(%q) accepted an HTTP endpoint", ep)
		}
	}
}
