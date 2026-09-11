package org

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hanzoai/authz"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/sqlite"
)

// testMasterKey is a master key the way KMS holds one: 32 bytes, base64.
var testMasterKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

// kmsHolding answers as KMS's HTTP door does for a deployment that stores
// masterKey at base/MASTER_KEY_B64, or no key when masterKey is empty. Every
// other secret is missing, and every answer comes at once.
func kmsHolding(t *testing.T, masterKey string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/kms/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "kms-test", "expiresIn": 3600})
		case r.URL.Path == "/v1/kms/secrets/"+masterKeyPath && masterKey != "":
			_ = json.NewEncoder(w).Encode(map[string]string{"value": masterKey})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

func testApp(t *testing.T) core.App {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	return app
}

// TestOrgBaseIsWrittenUnderTheMasterKey reads an org's Base off the disk after
// the API wrote to it, with the master key read from KMS the way the base
// binary reads it.
//
// Only a build with SQLCipher linked can keep an encrypted Base open. Any other
// build refuses the key rather than open org Bases it cannot encrypt, and there
// that refusal is the assertion.
func TestOrgBaseIsWrittenUnderTheMasterKey(t *testing.T) {
	app := testApp(t)
	iam := newIssuer(t)
	err := Register(app, Config{IAMEndpoint: iam.url, KMSEndpoint: kmsHolding(t, testMasterKey),
		IAMClientID: "svc", IAMClientSecret: "shh", IAMOrg: "hanzo"})
	if !sqlite.CodecLinked() {
		if err == nil || !strings.Contains(err.Error(), "SQLCipher") {
			t.Fatalf("a build without SQLCipher took a master key: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}

	// Written before the deployment had a key.
	db := NewOrgDB(app, "")
	if _, err := db.ProvisionOrg("globex"); err != nil {
		t.Fatal(err)
	}
	seed(t, db.OrgDir("globex"), "globex_notes")

	srv := httptest.NewServer(serve(t, app))
	defer srv.Close()

	// A platform admin whose home org is acme acts in acme's Base.
	root := iam.token(t, "acme/root", "acme", authz.AdminOrg)
	if code, body := send(t, srv, http.MethodPost, "/v1/collections", root,
		`{"name":"notes","type":"base","listRule":"","createRule":"","fields":[{"name":"title","type":"text"}]}`); code != http.StatusOK {
		t.Fatalf("creating a collection in acme's Base answered %d %s", code, body)
	}

	ann := iam.token(t, "acme/ann", "acme")
	if code, body := send(t, srv, http.MethodPost, "/v1/collections/notes/records", ann,
		`{"title":"CANARY_PLAINTEXT"}`); code != http.StatusOK {
		t.Fatalf("writing to acme's Base answered %d %s", code, body)
	}
	if code, body := send(t, srv, http.MethodGet, "/v1/collections/notes/records", ann, ""); code != http.StatusOK ||
		!strings.Contains(body, "CANARY_PLAINTEXT") {
		t.Fatalf("acme's Base did not read the record back: %d %s", code, body)
	}

	bases, _ := app.Store().Get(apis.StoreKeyBases).(apis.Bases)
	if _, err := bases("globex"); err == nil || !strings.Contains(err.Error(), "unencrypted") {
		t.Fatalf("opening a plaintext org Base under a master key answered %v", err)
	}

	// Shutting down closes each org Base.
	terminate := new(core.TerminateEvent)
	terminate.App = app
	if err := app.OnTerminate().Trigger(terminate, func(*core.TerminateEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"data.db", "auxiliary.db"} {
		raw, err := os.ReadFile(filepath.Join(app.DataDir(), orgsDirName, "acme", name))
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) == 0 {
			t.Fatalf("orgs/acme/%s is empty", name)
		}
		if bytes.HasPrefix(raw, []byte("SQLite format 3")) {
			t.Fatalf("orgs/acme/%s is a plaintext SQLite file", name)
		}
		if bytes.Contains(raw, []byte("CANARY_PLAINTEXT")) {
			t.Fatalf("orgs/acme/%s carries the record in the clear", name)
		}
	}
}

// TestMasterKeyFailsClosed pins what Base does when it cannot tell whether it
// has a master key.
func TestMasterKeyFailsClosed(t *testing.T) {
	iam := newIssuer(t)

	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()

	for _, c := range []struct {
		name   string
		config Config
	}{
		{"KMS with no org to read the key beneath", Config{KMSEndpoint: kmsHolding(t, testMasterKey)}},
		{"KMS that does not answer", Config{KMSEndpoint: gone.URL, IAMOrg: "hanzo"}},
		{"a key that is not 32 bytes", Config{
			KMSEndpoint: kmsHolding(t, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))),
			IAMOrg:      "hanzo",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.config.IAMEndpoint = iam.url
			c.config.IAMClientID, c.config.IAMClientSecret = "svc", "shh"
			if err := Register(testApp(t), c.config); err == nil {
				t.Fatal("Register started a Base that cannot read its master key")
			}
		})
	}

	t.Run("no key stored", func(t *testing.T) {
		if err := Register(testApp(t), Config{IAMEndpoint: iam.url, KMSEndpoint: kmsHolding(t, ""),
			IAMClientID: "svc", IAMClientSecret: "shh", IAMOrg: "hanzo"}); err != nil {
			t.Fatalf("a KMS that stores no master key stopped Register: %v", err)
		}
	})
}
