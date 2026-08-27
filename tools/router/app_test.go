package router_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hanzoai/base/tools/router"
	"github.com/zap-proto/zip"
)

// answered is one reply, as a caller sees it.
type answered struct {
	status int
	header http.Header
	body   string
}

func take(res *http.Response, err error) answered {
	if err != nil {
		return answered{body: "transport: " + err.Error()}
	}
	defer res.Body.Close()

	body, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		return answered{body: "body: " + readErr.Error()}
	}

	return answered{status: res.StatusCode, header: res.Header, body: string(body)}
}

// whole renders every field of a reply but Date, which is the instant it was
// written and so the one field no two replies can agree on.
func (a answered) whole() string {
	var lines []string
	for name, values := range a.header {
		if name == "Date" {
			continue
		}
		lines = append(lines, name+": "+strings.Join(values, ","))
	}
	sort.Strings(lines)

	return fmt.Sprintf("%d\n%s\n%q", a.status, strings.Join(lines, "\n"), a.body)
}

// stated renders the fields the ROUTER decides: the status, the body, and the
// headers its routes and their middleware set. What is left out is the
// transport's — the framing, the server name, and the content type a server
// stamps on a reply that carries no body.
func (a answered) stated(names ...string) string {
	var lines []string
	for _, name := range names {
		lines = append(lines, name+": "+a.header.Get(name))
	}

	return fmt.Sprintf("%d\n%s\n%q", a.status, strings.Join(lines, "\n"), a.body)
}

// wire builds one router carrying the shapes Base registers — a literal, a
// single-segment wildcard nested two deep, several methods at one address, a
// HEAD beside its GET, a trailing wildcard, a route that answers by returning
// an error, a redirect, and a stream that flushes.
func wire(t *testing.T) *router.Router[*router.Event] {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*router.Event, router.EventCleanupFunc) {
		return &router.Event{Response: w, Request: req}, nil
	})

	r.BindFunc(func(e *router.Event) error {
		e.Response.Header().Set("X-Chain", "root")
		return e.Next()
	})

	say := func(name string, values ...string) func(*router.Event) error {
		return func(e *router.Event) error {
			e.Response.Header().Set("X-Route", name)
			read := make([]string, 0, len(values))
			for _, v := range values {
				read = append(read, v+"="+e.Request.PathValue(v))
			}
			body, _ := io.ReadAll(e.Request.Body)
			return e.String(http.StatusOK, fmt.Sprintf("%s %s %s q=%s body=%s",
				name, e.Request.Method, strings.Join(read, ","), e.Request.URL.RawQuery, body))
		}
	}

	v1 := r.Group("/v1").BindFunc(func(e *router.Event) error {
		e.Response.Header().Set("X-Chain", e.Response.Header().Get("X-Chain")+":v1")
		return e.Next()
	})

	v1.GET("/collections", say("list"))
	v1.POST("/collections", say("create"))
	v1.GET("/collections/{collection}", say("view", "collection"))
	v1.PATCH("/collections/{collection}", say("update", "collection"))
	v1.DELETE("/collections/{collection}", say("delete", "collection"))
	v1.GET("/collections/{collection}/records/{id}", say("record", "collection", "id"))
	v1.GET("/files/{collection}/{recordId}/{filename}", say("file", "collection", "recordId", "filename"))
	v1.GET("/rest/{collection}", say("rest", "collection"))
	v1.HEAD("/rest/{collection}", say("head", "collection"))

	v1.GET("/gone/{collection}", func(e *router.Event) error {
		return router.NewNotFoundError("no such thing", nil)
	})
	v1.GET("/moved", func(e *router.Event) error {
		return e.Redirect(http.StatusMovedPermanently, "/v1/collections")
	})
	v1.GET("/stream", func(e *router.Event) error {
		e.Response.Header().Set("Content-Type", "text/event-stream")
		for i := range 3 {
			fmt.Fprintf(e.Response, "data: %d\n\n", i)
			_ = e.Flush()
		}
		return nil
	})

	r.GET("/healthz", say("healthz"))
	r.GET("/{path...}", say("spa", "path"))

	return r
}

// asked is the request table both builds answer. It covers every route, the
// methods no route names, and the decisions that belong to the matcher rather
// than to any one route.
var asked = []struct{ method, target, body string }{
	{"GET", "/v1/collections", ""},
	{"POST", "/v1/collections", "hello"},
	{"GET", "/v1/collections?page=2&perPage=5", ""},
	{"GET", "/v1/collections/posts", ""},
	{"PATCH", "/v1/collections/posts", "{}"},
	{"DELETE", "/v1/collections/posts", ""},
	{"GET", "/v1/collections/posts/records/abc123", ""},
	{"GET", "/v1/files/posts/abc123/photo.png", ""},
	{"GET", "/v1/rest/posts", ""},
	{"HEAD", "/v1/rest/posts", ""},
	{"HEAD", "/v1/collections", ""},
	{"GET", "/v1/gone/posts", ""},
	{"GET", "/v1/moved", ""},
	{"GET", "/v1/stream", ""},
	{"GET", "/healthz", ""},
	{"GET", "/", ""},
	{"GET", "/assets/app.js", ""},

	// the matcher's own decisions, not any one route's
	{"PUT", "/v1/collections", ""},          // no route names PUT here
	{"OPTIONS", "/v1/collections", ""},      // nor OPTIONS
	{"DELETE", "/healthz", ""},              // nor a method on a literal
	{"GET", "/v1/collections/", ""},         // a trailing slash is a different address
	{"GET", "/V1/Collections", ""},          // and so is a different case
	{"GET", "/v1//collections", ""},         // a path that needs cleaning
	{"GET", "/v1/./collections", ""},        // and another
	{"GET", "/v1/collections/../moved", ""}, // and another, leaving a segment
	{"GET", "/v1//collections?page=2", ""},  // the query survives the cleaning
	{"GET", "/v1/collections/a%2Fb", ""},    // an encoded separator stays one segment
	{"GET", "/v1/collections/%C3%A9", ""},   // and a segment reaches the handler decoded
	{"POST", "/v1/rest/posts", "x"},         // a method named at a sibling address
	{"GET", "/v1/files/posts/abc123", ""},   // one segment short of a route
	{"GET", "/v1", ""},                      // a group prefix is not an address
}

// TestAppAnswersWhatTheAdaptedMuxAnswers is the conversion's own measurement.
// What BuildApp replaces is the mux behind one wildcard, so both builds are
// driven over one transport and one request table, and every field of every
// reply has to agree — status, headers and body bytes.
func TestAppAnswersWhatTheAdaptedMuxAnswers(t *testing.T) {
	r := wire(t)

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	adapted := zip.New(zip.Config{DisableStartupMessage: true})
	adapted.All("/*", zip.AdaptNetHTTP(mux))

	app, err := r.BuildApp()
	if err != nil {
		t.Fatalf("BuildApp: %v", err)
	}

	for _, want := range asked {
		name := want.method + " " + want.target

		req, err := http.NewRequest(want.method, "http://base"+want.target, strings.NewReader(want.body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		byWildcard := take(adapted.Test(req))

		req, err = http.NewRequest(want.method, "http://base"+want.target, strings.NewReader(want.body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		byApp := take(app.Test(req))

		if byWildcard.whole() != byApp.whole() {
			t.Errorf("%s\n  wildcard %s\n  app      %s", name, byWildcard.whole(), byApp.whole())
		}
	}
}

// TestAppAnswersWhatTheMuxAnswers asks which route answered and what it wrote,
// of the mux served by net/http — the router's own answer with no transport of
// ours in front of it. The route names itself in the body it writes.
//
// It compares what the router states rather than every header, because the two
// servers frame a reply differently and neither framing is the router's: net/http
// counts a small reply and sends it whole, while the adapter streams whatever the
// handler writes, so a handler that flushes reaches the client while it is still
// writing. The total comparison is the test above, where both sides share one
// transport and there is nothing to excuse.
func TestAppAnswersWhatTheMuxAnswers(t *testing.T) {
	r := wire(t)

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	app, err := r.BuildApp()
	if err != nil {
		t.Fatalf("BuildApp: %v", err)
	}

	stated := []string{"Location", "X-Route", "X-Chain"}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	for _, want := range asked {
		name := want.method + " " + want.target

		req, err := http.NewRequest(want.method, srv.URL+want.target, strings.NewReader(want.body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		byMux := take(client.Do(req))

		req, err = http.NewRequest(want.method, "http://base"+want.target, strings.NewReader(want.body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		byApp := take(app.Test(req))

		if byMux.stated(stated...) != byApp.stated(stated...) {
			t.Errorf("%s\n  mux %s\n  app %s", name, byMux.stated(stated...), byApp.stated(stated...))
		}
	}
}

// TestAppIsAddressedRouteByRoute requires the built app to carry every route at
// its own address. It is the whole reason for the sibling: mounted as one
// wildcard, a composer sees one path, so it can neither route around Base nor
// publish what Base serves.
func TestAppIsAddressedRouteByRoute(t *testing.T) {
	app, err := wire(t).BuildApp()
	if err != nil {
		t.Fatalf("BuildApp: %v", err)
	}

	var got []string
	for _, route := range app.Fiber().GetRoutes(true) {
		got = append(got, route.Method+" "+route.Path)
	}

	for _, want := range []string{
		"GET /v1/collections",
		"POST /v1/collections",
		"GET /v1/collections/:collection",
		"PATCH /v1/collections/:collection",
		"DELETE /v1/collections/:collection",
		"GET /v1/collections/:collection/records/:id",
		"GET /v1/files/:collection/:recordId/:filename",
		"GET /v1/rest/:collection",
		"HEAD /v1/rest/:collection",
		"GET /healthz",
		"GET /*",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("no route at %q; the app carries %v", want, got)
		}
	}
}

// TestAppRefusesWhatItCannotAddress requires a pattern with no zip address to
// be named rather than approximated.
func TestAppRefusesWhatItCannotAddress(t *testing.T) {
	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*router.Event, router.EventCleanupFunc) {
		return &router.Event{Response: w, Request: req}, nil
	})
	r.GET("/v1/exact/{$}", func(e *router.Event) error { return nil })

	if _, err := r.BuildApp(); err == nil {
		t.Fatal("expected {$} to be refused")
	} else if !strings.Contains(err.Error(), "{$}") {
		t.Fatalf("expected the refusal to name {$}, got %q", err)
	}
}
