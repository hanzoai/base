package platform

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

// bannedPrefix matches an `/api/` PATH segment. It deliberately does NOT match
// the `api.hanzo.ai` HOSTNAME — the standard bans the path segment, not the
// api.* subdomain.
var bannedPrefix = regexp.MustCompile(`(^|[^.\w])/api/`)

// TestNoApiPrefixInRouteLiterals guards the ONE package in this repo that calls
// OUT to Hanzo services. Base's own record store legitimately serves `/api/*`
// (the native surface every Base client speaks), so the ban cannot be repo-wide
// — but no route here may address a Hanzo service that way: IAM answers only
// under /v1/iam, and the edge refuses the old prefix outright.
//
// It inspects STRING LITERALS via go/ast, not lines of text: only a literal can
// BE a route. Prose that names the prefix — a comment recording which dead
// upstream path a rewrite replaced — is documentation, not a route, and the
// parser tells the two apart exactly.
func TestNoApiPrefixInRouteLiterals(t *testing.T) {
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
			if v, err := strconv.Unquote(lit.Value); err == nil && bannedPrefix.MatchString(v) {
				offenders = append(offenders, fset.Position(lit.Pos()).String()+": "+lit.Value)
			}
			return true
		})
	}
	if len(offenders) > 0 {
		t.Fatalf("this package calls Hanzo services; use /v1/<service>/. Found %d banned route literal(s):\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}
