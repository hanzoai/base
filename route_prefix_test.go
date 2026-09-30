package base

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// banned is every route prefix this module may not address, with the reason
// each is refused. One walker reads them all: a second test would be a second
// place for the rule to be stated, and they would drift.
var banned = []struct {
	re  *regexp.Regexp
	why string
}{
	// The `/api/` PATH segment. Deliberately NOT the `api.hanzo.ai` HOSTNAME —
	// the standard bans the path segment, not the api.* subdomain.
	{regexp.MustCompile(`(^|[^.\w])/api/`), "Hanzo services answer under /v1/<service>/, never /api/"},

	// The same segment reached through an ABSOLUTE URL at one of our own hosts.
	// The rule above cannot see it: a word character precedes the segment there,
	// so `https://hanzo.id/api/login/...` reads exactly like the third-party URLs
	// the exclusion exists to permit. Our hosts are the ones the standard binds,
	// so they are named.
	{regexp.MustCompile(`hanzo\.(ai|id|bot|network)\S*/api/`), "our own hosts answer under /v1/<service>/, never /api/"},

	// /v1/platform belongs to the PaaS at platform.hanzo.ai. Base published its
	// own resource there once, and "where are my Bases?" had no answer a person
	// could act on: two products wore one name. A Base is Base's noun — /v1/bases.
	{regexp.MustCompile(`^/v1/platform(/|$)`), "that prefix is the PaaS; a Base is addressed at /v1/bases"},
}

// TestNoBannedRouteLiterals walks every Go file in the module. Two rules do the
// work, because one cannot: a bare path is caught by requiring a non-word
// character before the segment, which deliberately lets a third-party absolute
// URL like gitlab.com/api/v4 through, and our own hosts are named outright,
// since to the first rule they read the same way. Base serves only under /v1,
// IAM answers only under /v1/iam, and Tasks only under /v1/tasks.
//
// It inspects STRING LITERALS via go/ast, not lines of text: only a literal can
// BE a route. Prose that names the prefix — a comment recording which dead
// upstream path a rewrite replaced — is documentation, not a route, and the
// parser tells the two apart exactly.
func TestNoBannedRouteLiterals(t *testing.T) {
	self := "route_prefix_test.go" // names the banned prefix in order to ban it

	fset := token.NewFileSet()
	var offenders []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch name := d.Name(); {
			case path == ".":
			case name == "node_modules" || name == "testdata" || strings.HasPrefix(name, "."):
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || path == self {
			return nil
		}
		// ParseFile without ParseComments: comments are prose, not routes.
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
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
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("found %d banned route literal(s):\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}

// clientPrefix is a quoted /api path in TypeScript: a default base URL or a
// fetch target. A template that puts a caller's host first, like Kubo's
// `${api}/api/v0/add`, has no quote before the segment and is not ours.
var clientPrefix = regexp.MustCompile("['\"`(]/api(['\"`/]|$)")

// TestNoApiPrefixInClients reads the TypeScript this repo ships — the admin SPA
// and the SDKs — for a quoted /api path. The SPA and the private-store client
// both call Base, which answers only under /v1.
func TestNoApiPrefixInClients(t *testing.T) {
	var offenders []string
	for _, root := range []string{"ui-react/src", "sdk"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if n := d.Name(); n == "node_modules" || n == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".ts", ".tsx", ".js", ".mjs":
			default:
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(b), "\n") {
				if clientPrefix.MatchString(line) {
					offenders = append(offenders, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("found %d /api path(s) in client code; Base answers under /v1:\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}
