package org

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// banned is every route prefix this package may not address, with the reason
// each is refused. One walker reads them all: a second test would be a second
// place for the rule to be stated, and they would drift.
var banned = []struct {
	re  *regexp.Regexp
	why string
}{
	// The `/api/` PATH segment. Deliberately NOT the `api.hanzo.ai` HOSTNAME —
	// the standard bans the path segment, not the api.* subdomain.
	{regexp.MustCompile(`(^|[^.\w])/api/`), "Hanzo services answer under /v1/<service>/, never /api/"},

	// /v1/platform belongs to the PaaS at platform.hanzo.ai. Base published its
	// own resource there once, and "where are my Bases?" had no answer a person
	// could act on: two products wore one name. A Base is Base's noun — /v1/bases.
	{regexp.MustCompile(`^/v1/platform(/|$)`), "that prefix is the PaaS; a Base is addressed at /v1/bases"},
}

// TestNoBannedRouteLiterals guards the ONE package in this repo that calls
// OUT to Hanzo services. Base's own record store legitimately serves `/api/*`
// (the native surface every Base client speaks), so the ban cannot be repo-wide
// — but no route here may address a Hanzo service that way: IAM answers only
// under /v1/iam, and the edge refuses the old prefix outright.
//
// It inspects STRING LITERALS via go/ast, not lines of text: only a literal can
// BE a route. Prose that names the prefix — a comment recording which dead
// upstream path a rewrite replaced — is documentation, not a route, and the
// parser tells the two apart exactly.
func TestNoBannedRouteLiterals(t *testing.T) {
	self := "route_prefix_test.go" // names the banned prefix in order to ban it

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var offenders []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" || e.Name() == self {
			continue
		}
		// ParseFile without ParseComments: comments are prose, not routes.
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, b := range banned {
				if b.re.MatchString(v) {
					offenders = append(offenders,
						fset.Position(lit.Pos()).String()+": "+lit.Value+" — "+b.why)
				}
			}
			return true
		})
	}
	if len(offenders) > 0 {
		t.Fatalf("found %d banned route literal(s):\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}
