package router

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/zap-proto/zip"
)

// BuildApp constructs a zip [zip.App] from the current router configurations,
// with one zip route per registered route, so a composer mounts this router by
// calling Use on it and every route this router serves is an address in the
// composed router rather than one wildcard.
//
//	app.Use(baseApp)
//
// It reads the same configuration [Router.BuildMux] reads, through the same
// walk, and answers with the same handler: the event factory, the route's hook
// chain, and the error handler.
//
// WHICH route answers is still decided by [http.ServeMux], one pattern at a
// time — see [mount]. zip's router matches a superset: it folds case, it reads
// "/x" and "/x/" as one address, and it performs none of ServeMux's path
// cleaning. So a request zip carries to a route is offered to that route's own
// pattern first, and a pattern that does not name it hands the request on to
// the next route zip matched, which is the walk ServeMux performs internally.
func (r *Router[T]) BuildApp() (*zip.App, error) {
	r.bindCatchAll()

	app := zip.New(zip.Config{DisableStartupMessage: true})

	// Routes gathered by the address zip spells them at, in registration order.
	// A route that names no method matches every method, so it cannot be
	// registered beside a route that names one: two claims on GET /* is a
	// composition zip refuses. ServeMux settles that pair by specificity, so
	// such an address takes every method and its handler offers the request to
	// the named method first.
	var addresses []string
	routes := map[string][]leaf{}

	err := r.walk(r.RouterGroup, nil, func(method, path string, serve http.HandlerFunc) error {
		address, err := spell(path)
		if err != nil {
			return err
		}
		if _, seen := routes[address]; !seen {
			addresses = append(addresses, address)
		}
		routes[address] = append(routes[address], mount(method, path, serve))
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, address := range addresses {
		at := routes[address]

		// A named method is more specific than none, which is the order the
		// offer below has to follow. Stable, so two named methods keep the
		// order they were registered in.
		sort.SliceStable(at, func(i, j int) bool { return at[i].method != "" && at[j].method == "" })

		if at[len(at)-1].method == "" {
			app.All(address, offer(at))
			continue
		}
		for _, l := range at {
			verb, ok := verbs[l.method]
			if !ok {
				return nil, fmt.Errorf("cannot register %s %s: zip has no %s route", l.method, address, l.method)
			}
			verb(app, address, offer([]leaf{l}))
		}
	}

	return app, nil
}

// leaf is one registered route as zip serves it: whether a request belongs to
// the route, and the handler that answers when it does.
type leaf struct {
	method string
	claims func(*zip.Ctx) bool
	serve  zip.Handler
}

// mount turns one registered route into that pair, and both halves are one
// [http.ServeMux] holding exactly this route's pattern.
//
// So nothing here restates ServeMux's rules. The matcher [Router.BuildMux]
// installs decides the method, the case, the trailing slash, the path values a
// handler reads back with [http.Request.PathValue], and the redirect a path
// that needs cleaning earns — one pattern at a time, from the same request the
// handler will be given. A pattern that names the request answers it; a pattern
// that names nothing leaves the request for the next route.
func mount(method, path string, serve http.HandlerFunc) leaf {
	pattern := path
	if method != "" {
		pattern = method + " " + pattern
	}

	one := http.NewServeMux()
	one.Handle(pattern, serve)

	return leaf{
		method: method,
		claims: func(c *zip.Ctx) bool {
			u, err := url.ParseRequestURI(string(c.Fiber().RequestCtx().RequestURI()))
			if err != nil {
				return false
			}
			// The same three fields ServeMux reads, off the same raw request
			// URI the adapter parses the handler's own request from.
			_, named := one.Handler(&http.Request{Method: c.Method(), Host: c.Host(), URL: u})
			return named != ""
		},
		serve: zip.AdaptNetHTTP(one),
	}
}

// offer hands a request to the first route at an address whose pattern names
// it, and to the next route zip matched when none of them does.
func offer(at []leaf) zip.Handler {
	return func(c *zip.Ctx) error {
		for _, l := range at {
			if l.claims(c) {
				return l.serve(c)
			}
		}
		return c.Next()
	}
}

// verbs is the method each zip route registrar answers for. A method zip has no
// registrar for is refused rather than widened into All, which would claim
// every other method at that address as well.
var verbs = map[string]func(zip.Router, string, zip.Handler){
	http.MethodGet:     func(r zip.Router, p string, h zip.Handler) { r.Get(p, h) },
	http.MethodHead:    func(r zip.Router, p string, h zip.Handler) { r.Head(p, h) },
	http.MethodPost:    func(r zip.Router, p string, h zip.Handler) { r.Post(p, h) },
	http.MethodPut:     func(r zip.Router, p string, h zip.Handler) { r.Put(p, h) },
	http.MethodPatch:   func(r zip.Router, p string, h zip.Handler) { r.Patch(p, h) },
	http.MethodDelete:  func(r zip.Router, p string, h zip.Handler) { r.Delete(p, h) },
	http.MethodOptions: func(r zip.Router, p string, h zip.Handler) { r.Options(p, h) },
}

// spell writes a ServeMux path the way zip's router writes it: "{name}" is one
// segment, "{name...}" is the rest of the path, and a path ending in "/" is the
// subtree it stands for in a ServeMux pattern.
//
// The address zip matches on is therefore wider than the pattern in every case
// where the two spellings differ, which is what makes the offer above sound:
// zip carries a request to the route, and the pattern decides.
func spell(path string) (string, error) {
	if path == "" || path[0] != '/' {
		return "", fmt.Errorf("cannot register %q: zip addresses start at /, so a pattern naming a host has no address", path)
	}

	segments := strings.Split(path, "/")
	for i, s := range segments {
		if len(s) < 3 || s[0] != '{' || s[len(s)-1] != '}' {
			continue
		}
		name := s[1 : len(s)-1]
		switch {
		case name == "$":
			// "{$}" ends a ServeMux pattern at that exact path, and zip reads
			// "/x" and "/x/" as one address, so no address expresses it.
			return "", fmt.Errorf("cannot register %q: zip has no address that ends where {$} ends", path)
		case strings.HasSuffix(name, "..."):
			segments[i] = "*"
		default:
			segments[i] = ":" + name
		}
	}

	address := strings.Join(segments, "/")
	if strings.HasSuffix(address, "/") {
		address += "*"
	}

	return address, nil
}
