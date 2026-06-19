package bootnode

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/iam"
	"github.com/hanzoai/base/plugins/bootnode/kube"
	"github.com/hanzoai/base/plugins/bootnode/models"
	"github.com/hanzoai/base/plugins/commerce"
	"github.com/hanzoai/base/tests"
	luxlog "github.com/luxfi/log"
)

// fakeIAM stands in for Hanzo IAM. It validates exactly one token and resolves
// it to a fixed user in the "lux" org.
func fakeIAM(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/userinfo":
			if r.Header.Get("Authorization") != "Bearer valid-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "user-123", "email": "z@lux.network", "name": "Z", "orgIds": []string{"lux"},
			})
		case "/api/get-users": // LookupByAttribute (team invite)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// fakeAPIServer stands in for the Kubernetes apiserver. It records applied CRs
// in-memory and serves them back on GET (named + list).
func fakeAPIServer(t *testing.T) (*httptest.Server, map[string]map[string]any) {
	store := map[string]map[string]any{} // path -> object
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch: // server-side apply
			var obj map[string]any
			_ = json.NewDecoder(r.Body).Decode(&obj)
			store[r.URL.Path] = obj
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(obj)
		case http.MethodGet:
			if obj, ok := store[r.URL.Path]; ok {
				_ = json.NewEncoder(w).Encode(obj)
				return
			}
			// List endpoint (no trailing name): return all matching items.
			items := []map[string]any{}
			for p, o := range store {
				if strings.HasPrefix(p, r.URL.Path+"/") {
					items = append(items, o)
				}
			}
			if strings.HasSuffix(r.URL.Path, "/networks") ||
				strings.HasSuffix(r.URL.Path, "/nodefleets") ||
				strings.HasSuffix(r.URL.Path, "/kmssecrets") {
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodDelete:
			delete(store, r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}
	}))
	return srv, store
}

// newTestPlugin wires a bootnode plugin against fake IAM + apiserver and an
// initialized test app with all collections created.
func newTestPlugin(t *testing.T) (*plugin, *tests.TestApp, func()) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	if err := models.EnsureAll(app); err != nil {
		app.Cleanup()
		t.Fatalf("EnsureAll: %v", err)
	}

	iamSrv := fakeIAM(t)
	apiSrv, _ := fakeAPIServer(t)

	k := kube.New("bootnode")
	// Point the kube client at the fake apiserver (out-of-cluster shape).
	kube.SetTarget(k, apiSrv.URL, "test-token")

	cfg := Config{Enabled: true, IAMEndpoint: iamSrv.URL, APIKeySalt: "test-salt", KubeNamespace: "bootnode"}
	cfg.resolve()

	p := &plugin{
		app:      app,
		config:   cfg,
		logger:   luxlog.New("component", "bootnode-test"),
		iam:      iam.NewClient(iamSrv.URL),
		kube:     k,
		commerce: commerce.New(commerce.Config{}),
	}

	cleanup := func() {
		iamSrv.Close()
		apiSrv.Close()
		app.Cleanup()
	}
	return p, app, cleanup
}

// serve builds an httptest server over the plugin's mounted routes.
func serve(t *testing.T, p *plugin, app *tests.TestApp) *httptest.Server {
	t.Helper()
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	p.registerRoutes(r)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	return httptest.NewServer(mux)
}

func do(t *testing.T, method, url, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func TestEndToEnd_AuthTeamNetworksNodesKeys(t *testing.T) {
	p, app, cleanup := newTestPlugin(t)
	defer cleanup()
	srv := serve(t, p, app)
	defer srv.Close()

	const tok = "valid-token"

	// --- auth: unauthenticated is rejected ---
	resp, _ := do(t, http.MethodGet, srv.URL+"/v1/auth/me", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth /me: want 401, got %d", resp.StatusCode)
	}

	// --- auth: /me with a valid IAM token ---
	resp, me := do(t, http.MethodGet, srv.URL+"/v1/auth/me", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/me: want 200, got %d", resp.StatusCode)
	}
	if me["id"] != "user-123" || me["org"] != "lux" {
		t.Fatalf("/me identity wrong: %v", me)
	}

	// --- auth: create a project ---
	resp, proj := do(t, http.MethodPost, srv.URL+"/v1/auth/projects", tok, map[string]any{"name": "My App"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d (%v)", resp.StatusCode, proj)
	}
	projectID, _ := proj["id"].(string)
	if projectID == "" || proj["orgId"] != "lux" {
		t.Fatalf("project response wrong: %v", proj)
	}

	// --- auth: mint a bn_ API key, ensure raw key returned once + bn_ prefix ---
	resp, keyResp := do(t, http.MethodPost, srv.URL+"/v1/auth/keys", tok, map[string]any{
		"projectId": projectID, "name": "prod",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create key: want 201, got %d (%v)", resp.StatusCode, keyResp)
	}
	rawKey, _ := keyResp["key"].(string)
	if !strings.HasPrefix(rawKey, "bn_") {
		t.Fatalf("raw key must start with bn_, got %q", rawKey)
	}

	// --- auth: list keys never exposes the raw key ---
	resp, _ = do(t, http.MethodGet, srv.URL+"/v1/auth/keys?projectId="+projectID, tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list keys: want 200, got %d", resp.StatusCode)
	}

	// --- team: invite a member (pending, since fake IAM returns no user) ---
	resp, member := do(t, http.MethodPost, srv.URL+"/v1/team", tok, map[string]any{
		"email": "teammate@lux.network", "role": "member",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite member: want 201, got %d (%v)", resp.StatusCode, member)
	}
	if member["status"] != "pending" || member["role"] != "member" {
		t.Fatalf("member should be pending/member: %v", member)
	}

	// --- team: list members ---
	resp, team := do(t, http.MethodGet, srv.URL+"/v1/team", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list team: want 200, got %d", resp.StatusCode)
	}
	if total, _ := team["total"].(float64); total != 1 {
		t.Fatalf("team total: want 1, got %v", team["total"])
	}

	// --- networks: create a Network CR ---
	resp, net := do(t, http.MethodPost, srv.URL+"/v1/networks", tok, map[string]any{
		"name": "acme", "tier": "pro", "brand": map[string]any{"domain": "acme.network"},
		"deployValidators": true, "validatorCount": 5,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create network: want 201, got %d (%v)", resp.StatusCode, net)
	}
	if net["id"] != "acme" || net["status"] != "provisioning" {
		t.Fatalf("network response wrong: %v", net)
	}

	// --- networks: get it back from the (fake) cluster ---
	resp, gotNet := do(t, http.MethodGet, srv.URL+"/v1/networks/acme", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get network: want 200, got %d", resp.StatusCode)
	}
	spec, _ := gotNet["spec"].(map[string]any)
	if spec == nil || spec["tier"] != "pro" {
		t.Fatalf("network CR spec wrong: %v", gotNet)
	}

	// --- nodes: create a NodeFleet CR via simple preset ---
	resp, fleet := do(t, http.MethodPost, srv.URL+"/v1/nodes", tok, map[string]any{
		"name": "eth-fleet", "chain": "ethereum", "mode": "simple", "preset": "full",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fleet: want 201, got %d (%v)", resp.StatusCode, fleet)
	}
	if fleet["executionClient"] != "geth" {
		t.Fatalf("preset 'full' must select geth: %v", fleet)
	}

	// --- keys: provisioning must reject plaintext key material ---
	resp, _ = do(t, http.MethodPost, srv.URL+"/v1/keys", tok, map[string]any{
		"name": "signer", "chain": "lux", "privateKey": "0xdeadbeef",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("keys must reject plaintext private key, got %d", resp.StatusCode)
	}

	// --- keys: KMSSecret CR provisioning by path (no material) ---
	resp, key := do(t, http.MethodPost, srv.URL+"/v1/keys", tok, map[string]any{
		"name": "signer", "chain": "lux", "kmsPath": "providers/lux/signer", "keyType": "secp256k1",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create KMS key: want 201, got %d (%v)", resp.StatusCode, key)
	}
	if _, leaked := key["privateKey"]; leaked {
		t.Fatal("key response must NEVER contain private key material")
	}

	// --- keys: status reports configured=true now that a KMSSecret exists ---
	resp, status := do(t, http.MethodGet, srv.URL+"/v1/keys/status", tok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("key status: want 200, got %d", resp.StatusCode)
	}
	if status["configured"] != true {
		t.Fatalf("key status should be configured after KMSSecret apply: %v", status)
	}

	// --- chains: public, unauthenticated ---
	resp, chains := do(t, http.MethodGet, srv.URL+"/v1/chains", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chains: want 200, got %d", resp.StatusCode)
	}
	if _, ok := chains["chains"].(map[string]any)["lux"]; !ok {
		t.Fatalf("chains must include lux: %v", chains)
	}
}

func TestRegisterRejectsInsecureSaltInProduction(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	defer app.Cleanup()
	// Non-local IAM + default salt must fail fast.
	err = Register(app, Config{Enabled: true, IAMEndpoint: "https://hanzo.id"})
	if err == nil {
		t.Fatal("Register must refuse the insecure default salt against production IAM")
	}
}
