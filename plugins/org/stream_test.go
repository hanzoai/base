package org

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/base/core"
)

// posts puts a collection into an org's Base that anyone may read and write, so
// that what a subscriber is or is not sent turns on which Base it is subscribed
// to and on nothing else.
func posts(t *testing.T, dir string) {
	t.Helper()

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	anyone := ""
	c := core.NewBaseCollection("posts")
	c.ListRule, c.ViewRule, c.CreateRule = &anyone, &anyone, &anyone
	c.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
}

// send issues one ordinary request, carrying the caller's credential in a header
// the way everything but the stream carries it.
func send(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	out, _ := io.ReadAll(res.Body)

	return res.StatusCode, string(out)
}

// mint asks for the grant a stream is opened with, which is an ordinary
// authenticated request and the only place the caller's own credential appears.
func mint(t *testing.T, srv *httptest.Server, token string) string {
	t.Helper()

	code, body := send(t, srv, http.MethodPost, "/v1/realtime/token", token, "")
	if code != http.StatusOK {
		t.Fatalf("minting a grant answered %d %s", code, body)
	}

	var out struct{ Token string }
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Token == "" {
		t.Fatal("minting a grant answered with no grant")
	}

	return out.Token
}

// stream opens a realtime stream the way a browser does — a GET carrying no
// headers at all — and returns the reader the rest of it arrives on together
// with the client id the server handed back.
func stream(t *testing.T, srv *httptest.Server, query string) (*bufio.Reader, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/realtime"+query, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })

	if res.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(res.Body)
		t.Fatalf("opening the stream answered %d %s", res.StatusCode, out)
	}

	r := bufio.NewReader(res.Body)

	name, data := frame(t, r)
	if name != "CONNECT" {
		t.Fatalf("the stream opened with %q, want CONNECT", name)
	}

	var hello struct{ ClientId string }
	if err := json.Unmarshal([]byte(data), &hello); err != nil {
		t.Fatal(err)
	}

	return r, hello.ClientId
}

// frame reads one event off a stream: its name and its data.
func frame(t *testing.T, r *bufio.Reader) (string, string) {
	t.Helper()

	var name, data string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}

		switch line = strings.TrimRight(line, "\r\n"); {
		case line == "":
			if name != "" {
				return name, data
			}
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimPrefix(line, "event:")
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimPrefix(line, "data:")
		}
	}
}

func subscribe(clientId string) string {
	return `{"clientId":"` + clientId + `","subscriptions":["posts/*"]}`
}

// TestAStreamReachesTheOrgItsCallerIsIn is the defect stated as a pair of
// requests.
//
// A stream is opened by EventSource, which sends no headers, so a stream opened
// with nothing in its query carries no credential, resolves to no org and
// registers on the Base the process runs on. The POST that names that client
// does carry the token, resolves the org, and looks for the client on the org's
// broker — where it is not. Same page, same second, two Bases.
func TestAStreamReachesTheOrgItsCallerIsIn(t *testing.T) {
	_, iam, mux, db := twoOrgs(t)
	posts(t, db.OrgDir("alpha"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ann := iam.token(t, "alpha/ann", "alpha")

	// Opened with a grant, the stream is alpha's, and the subscription lands on
	// the same broker the org's writes are published to.
	_, granted := stream(t, srv, "?token="+mint(t, srv, ann))
	if code, body := send(t, srv, http.MethodPost, "/v1/realtime", ann, subscribe(granted)); code != http.StatusNoContent {
		t.Fatalf("subscribing a granted stream answered %d %s, want 204", code, body)
	}

	// Opened with nothing, it is a stream on the process's own Base, and an
	// alpha token looking for it there does not find it.
	_, ungranted := stream(t, srv, "")
	if code, body := send(t, srv, http.MethodPost, "/v1/realtime", ann, subscribe(ungranted)); code != http.StatusNotFound {
		t.Fatalf("subscribing an ungranted stream answered %d %s, want 404", code, body)
	}
}

// TestAStreamIsSentTheOrgsWrites is the whole path through the doors a browser
// uses: a grant minted on an authenticated POST, spent by a GET that can carry
// nothing, and a record written to that org's Base arriving on the stream.
func TestAStreamIsSentTheOrgsWrites(t *testing.T) {
	_, iam, mux, db := twoOrgs(t)
	posts(t, db.OrgDir("alpha"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ann := iam.token(t, "alpha/ann", "alpha")

	r, id := stream(t, srv, "?token="+mint(t, srv, ann))
	if code, body := send(t, srv, http.MethodPost, "/v1/realtime", ann, subscribe(id)); code != http.StatusNoContent {
		t.Fatalf("subscribing answered %d %s, want 204", code, body)
	}

	if code, body := send(t, srv, http.MethodPost, "/v1/collections/posts/records", ann, `{"title":"hello"}`); code != http.StatusOK {
		t.Fatalf("writing to alpha's Base answered %d %s", code, body)
	}

	name, data := frame(t, r)
	if name != "posts/*" || !strings.Contains(data, `"action":"create"`) || !strings.Contains(data, "hello") {
		t.Fatalf("the stream was sent %q %s, want the create it subscribed to", name, data)
	}
}

// TestAGrantOpensOneStream pins what a grant is worth once it has been used, and
// what an unmintable caller gets.
func TestAGrantOpensOneStream(t *testing.T) {
	_, iam, mux, _ := twoOrgs(t)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ann := iam.token(t, "alpha/ann", "alpha")

	spent := mint(t, srv, ann)
	stream(t, srv, "?token="+spent)

	if code, body := send(t, srv, http.MethodGet, "/v1/realtime?token="+spent, "", ""); code != http.StatusUnauthorized {
		t.Fatalf("a second stream on a spent grant answered %d %s, want 401", code, body)
	}
	if code, body := send(t, srv, http.MethodGet, "/v1/realtime?token=never-minted", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("a stream on a grant nobody minted answered %d %s, want 401", code, body)
	}
	if code, body := send(t, srv, http.MethodPost, "/v1/realtime/token", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("minting a grant with no credential answered %d %s, want 401", code, body)
	}
}
