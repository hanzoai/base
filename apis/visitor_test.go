package apis_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
)

// The record ids Base gives the subjects below: a short lowercase sub, padded.
const (
	visitorID = "vic____________"
	memberID  = "mem____________"
)

// visitorBase is a Base its host opened for acme, holding a collection that
// admits visitors (comments) beside ones that do not (people, lure), and a
// visitor from zoo who owns zoo, a member of acme and a SuperAdmin to call it.
type visitorBase struct {
	app                    *tests.TestApp
	mux                    http.Handler
	iam                    *tests.IAM
	visitor, member, admin string
	person, comment        string
}

func newVisitorBase(t *testing.T) *visitorBase {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	iam := tests.NewIAM(t)
	app.Store().Set(apis.StoreKeyExternalAuthOnly, true)
	app.Store().Set(apis.StoreKeyJWKSURL, iam.URL)
	app.Store().Set(apis.StoreKeyTenant, "acme")

	signedIn := types.Pointer(`@request.auth.id != ""`)
	people := core.NewBaseCollection("people")
	people.ListRule, people.ViewRule, people.CreateRule, people.UpdateRule, people.DeleteRule = signedIn, signedIn, signedIn, signedIn, signedIn
	people.Fields.Add(&core.TextField{Name: "name"}, &core.FileField{Name: "doc", MaxSelect: 1, MaxSize: 1 << 20})
	if err := app.Save(people); err != nil {
		t.Fatal(err)
	}

	own := types.Pointer(`author = @request.auth.id`)
	comments := core.NewBaseCollection("comments")
	comments.Visitors = true
	comments.ListRule, comments.ViewRule = types.Pointer(""), types.Pointer("")
	comments.CreateRule = types.Pointer(`@request.auth.id != "" && author = @request.auth.id`)
	comments.UpdateRule, comments.DeleteRule = own, own
	comments.Fields.Add(
		&core.TextField{Name: "author"},
		&core.TextField{Name: "body", Max: 4000},
		&core.RelationField{Name: "ref", CollectionId: people.Id, MaxSelect: 1},
		&core.FileField{Name: "doc", MaxSelect: 1, MaxSize: 1 << 20},
	)
	if err := app.Save(comments); err != nil {
		t.Fatal(err)
	}

	// A rule that names visitors opens nothing on a collection that does not
	// admit them.
	lure := core.NewBaseCollection("lure")
	lure.ListRule = types.Pointer(`@request.auth.role = "visitor"`)
	if err := app.Save(lure); err != nil {
		t.Fatal(err)
	}

	person := core.NewRecord(people)
	person.Set("name", "Acme Corp")
	if err := app.Save(person); err != nil {
		t.Fatal(err)
	}
	comment := core.NewRecord(comments)
	comment.Set("author", memberID)
	comment.Set("body", "first")
	comment.Set("ref", person.Id)
	if err := app.Save(comment); err != nil {
		t.Fatal(err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	return &visitorBase{
		app: app,
		mux: mux,
		iam: iam,
		visitor: iam.Token(t, jwt.MapClaims{"sub": "vic", "name": "Vic", "email": "vic@zoo.test",
			"orgs": []any{map[string]any{"org": "zoo", "role": "owner"}}}),
		member: iam.Token(t, jwt.MapClaims{"sub": "mem", "name": "Mem", "email": "mem@acme.test",
			"orgs": []any{map[string]any{"org": "acme", "role": "member"}}}),
		admin: iam.Token(t, jwt.MapClaims{"sub": "root", "name": "root", "email": "root@admin.test",
			"orgs": []any{map[string]any{"org": "admin", "role": "owner"}}}),
		person:  person.Id,
		comment: comment.Id,
	}
}

func (b *visitorBase) do(t *testing.T, token, method, target, body string, headers ...string) (int, string) {
	t.Helper()
	return b.from(t, "192.0.2.1", token, method, target, body, headers...)
}

// from makes a request as do does, from the client address ip.
func (b *visitorBase) from(t *testing.T, ip, token, method, target, body string, headers ...string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = ip + ":1234"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	b.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A visitor is refused everything a collection that does not admit visitors
// offers, and everything that is not a record or a file: the schema, settings,
// batch, realtime, aggregates and history.
func TestAVisitorIsRefusedOutsideAVisitorsCollection(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	p, c := b.person, b.comment

	for _, r := range []struct{ method, target, body string }{
		{"GET", "/v1/collections/people/records", ""},
		{"GET", "/v1/collections/people/records/" + p, ""},
		{"POST", "/v1/collections/people/records", `{"name":"x"}`},
		{"PATCH", "/v1/collections/people/records/" + p, `{"name":"x"}`},
		{"DELETE", "/v1/collections/people/records/" + p, ""},
		{"GET", "/v1/files/people/" + p + "/doc.txt", ""},
		{"GET", "/v1/collections/lure/records", ""},
		{"GET", "/v1/collections/users/records", ""},
		{"GET", "/v1/collections", ""},
		{"GET", "/v1/collections/comments", ""},
		{"POST", "/v1/collections", `{"name":"mine","type":"base"}`},
		{"PATCH", "/v1/collections/comments", `{"listRule":null}`},
		{"GET", "/v1/settings", ""},
		{"POST", "/v1/batch", `{"requests":[{"method":"POST","url":"/v1/collections/people/records","body":{"name":"x"}}]}`},
		{"POST", "/v1/realtime/token", ""},
		{"POST", "/v1/realtime", `{"clientId":"x","subscriptions":["people"]}`},
		{"GET", "/v1/collections/comments/aggregate", ""},
		{"GET", "/v1/collections/comments/records/" + c + "/history", ""},
		{"GET", "/v1/nowhere", ""},
	} {
		if status, body := b.do(t, b.visitor, r.method, r.target, r.body); status != http.StatusForbidden {
			t.Errorf("%s %s: got %d %s, want 403", r.method, r.target, status, body)
		}
	}

	// Nothing above was written.
	if n, _ := b.app.CountRecords("people"); n != 1 {
		t.Fatalf("people holds %d records, want 1", n)
	}
	if _, err := b.app.FindCollectionByNameOrId("mine"); err == nil {
		t.Fatal("a visitor created a collection")
	}
}

// On a collection that admits visitors, a visitor reads what anyone may, writes
// as itself and changes only its own rows.
func TestAVisitorWritesItsOwnRows(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	const records = "/v1/collections/comments/records"

	status, body := b.do(t, b.visitor, "GET", records, "")
	if status != 200 || !strings.Contains(body, b.comment) {
		t.Fatalf("list: %d %s", status, body)
	}

	// Another's name is refused, its own admitted.
	if status, body := b.do(t, b.visitor, "POST", records, `{"author":"`+memberID+`","body":"as you"}`); status != 400 {
		t.Fatalf("create as another: %d %s", status, body)
	}
	status, body = b.do(t, b.visitor, "POST", records, `{"author":"`+visitorID+`","body":"hello"}`)
	if status != 200 {
		t.Fatalf("create: %d %s", status, body)
	}
	var made struct{ Id string }
	if err := json.Unmarshal([]byte(body), &made); err != nil {
		t.Fatal(err)
	}

	// Its own row it edits, and cannot hand to someone else.
	if status, body := b.do(t, b.visitor, "PATCH", records+"/"+made.Id, `{"body":"edited"}`); status != 200 || !strings.Contains(body, "edited") {
		t.Fatalf("update own: %d %s", status, body)
	}
	if status, body := b.do(t, b.visitor, "PATCH", records+"/"+made.Id, `{"author":"`+memberID+`"}`); status != 400 {
		t.Fatalf("give own away: %d %s", status, body)
	}

	// Another's row it neither edits nor deletes.
	if status, body := b.do(t, b.visitor, "PATCH", records+"/"+b.comment, `{"body":"mine now"}`); status != 404 {
		t.Fatalf("update another's: %d %s", status, body)
	}
	if status, body := b.do(t, b.visitor, "PATCH", records+"/"+b.comment, `{"author":"`+visitorID+`"}`); status != 404 {
		t.Fatalf("take another's: %d %s", status, body)
	}
	if status, body := b.do(t, b.visitor, "DELETE", records+"/"+b.comment, ""); status != 404 {
		t.Fatalf("delete another's: %d %s", status, body)
	}
	if got, err := b.app.FindRecordById("comments", b.comment); err != nil || got.GetString("body") != "first" || got.GetString("author") != memberID {
		t.Fatalf("another's row changed: %v %v", got, err)
	}

	// A file on the collection is past the door (there is none, so 404).
	if status, body := b.do(t, b.visitor, "GET", "/v1/files/comments/"+b.comment+"/doc.txt", ""); status != 404 {
		t.Fatalf("file: %d %s", status, body)
	}

	if status, body := b.do(t, b.visitor, "DELETE", records+"/"+made.Id, ""); status != 204 {
		t.Fatalf("delete own: %d %s", status, body)
	}
}

// A visitor reaches no row of another collection through one that admits it: a
// relation it expands or filters on is held to that collection's rules as a
// guest's would be. A member's request is unchanged.
func TestAVisitorIsAGuestToEveryOtherCollection(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	expand := "/v1/collections/comments/records?expand=ref"
	filter := "/v1/collections/comments/records?filter=" + url.QueryEscape(`ref.name = "Acme Corp"`)

	if _, body := b.do(t, b.member, "GET", expand, ""); !strings.Contains(body, "Acme Corp") {
		t.Fatalf("member expand: %s", body)
	}
	if status, body := b.do(t, b.visitor, "GET", expand, ""); status != 200 || strings.Contains(body, "Acme Corp") {
		t.Fatalf("visitor expand: %d %s", status, body)
	}
	if _, body := b.do(t, b.member, "GET", filter, ""); !strings.Contains(body, `"totalItems":1`) {
		t.Fatalf("member filter: %s", body)
	}
	if status, body := b.do(t, b.visitor, "GET", filter, ""); status != 200 || !strings.Contains(body, `"totalItems":0`) {
		t.Fatalf("visitor filter: %d %s", status, body)
	}

	// Nor is a visitor's relation to another collection's row allowed to
	// stand in for reading it.
	if status, body := b.do(t, b.member, "GET", "/v1/collections/people/records", ""); status != 200 || !strings.Contains(body, `"totalItems":1`) {
		t.Fatalf("member people: %d %s", status, body)
	}
	if status, body := b.do(t, b.member, "POST", "/v1/collections/comments/records", `{"author":"`+memberID+`","body":"member"}`); status != 200 {
		t.Fatalf("member create: %d %s", status, body)
	}
}

// Rules read a visitor as one: its role here is visitor, it administers
// nothing here whatever it is at home, and its org is its own.
func TestRulesReadAVisitor(t *testing.T) {
	t.Parallel()

	for _, s := range []struct {
		rule string
		want string
	}{
		{`@request.auth.role = "visitor"`, `"totalItems":1`},
		{`@request.auth.admin = true`, `"totalItems":0`},
		{`@request.auth.role = "owner"`, `"totalItems":0`},
		{`@request.auth.org_id = "zoo"`, `"totalItems":1`},
		{`@request.auth.name = "Vic"`, `"totalItems":1`},
		{`@request.auth.id = "` + visitorID + `"`, `"totalItems":1`},
	} {
		b := newVisitorBase(t)
		c, err := b.app.FindCollectionByNameOrId("comments")
		if err != nil {
			t.Fatal(err)
		}
		c.ListRule = types.Pointer(s.rule)
		if err := b.app.Save(c); err != nil {
			t.Fatal(err)
		}
		if status, body := b.do(t, b.visitor, "GET", "/v1/collections/comments/records", ""); status != 200 || !strings.Contains(body, s.want) {
			t.Errorf("%s: %d %s", s.rule, status, body)
		}
	}
}

// A SuperAdmin is never a visitor: it acts in a tenant's Base by naming it.
func TestASuperAdminIsNoVisitor(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	records := "/v1/collections/comments/records"

	if status, body := b.do(t, b.admin, "GET", records, ""); status != 403 {
		t.Fatalf("unnamed: %d %s", status, body)
	}
	if status, body := b.do(t, b.admin, "DELETE", records+"/"+b.comment, "", "X-Org-Id", "acme"); status != 204 {
		t.Fatalf("named: %d %s", status, body)
	}
}

// Naming the Base's org buys a visitor nothing: the org is the token's.
func TestAVisitorCannotNameTheBasesOrg(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	if status, body := b.do(t, b.visitor, "GET", "/v1/collections/people/records", "", "X-Org-Id", "acme"); status != 403 {
		t.Fatalf("%d %s", status, body)
	}
}

// A visitor's writes are held, whether or not the deployment limits anything,
// to ten a minute from one account, fifteen from one address whatever accounts
// sign in from it, and 120 from every visitor in the Base together.
func TestAVisitorsWritesAreLimited(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	if b.app.Settings().RateLimits.Enabled {
		t.Fatal("the fixture limits rates; this test is about a Base that does not")
	}
	visitor := func(n int) (token, id string) {
		sub := fmt.Sprintf("v%d", n)
		return b.iam.Token(t, jwt.MapClaims{"sub": sub, "name": sub,
			"orgs": []any{map[string]any{"org": "zoo" + sub, "role": "owner"}}}), sub + strings.Repeat("_", 15-len(sub))
	}
	write := func(ip, token, id string) int {
		status, _ := b.from(t, ip, token, "POST", "/v1/collections/comments/records", `{"author":"`+id+`","body":"again"}`)
		return status
	}

	// One account: ten, then refused.
	for i := range 10 {
		if status := write("198.51.100.1", b.visitor, visitorID); status != 200 {
			t.Fatalf("account write %d: %d", i+1, status)
		}
	}
	if status := write("198.51.100.1", b.visitor, visitorID); status != 429 {
		t.Fatalf("account write 11: %d", status)
	}

	// A second account on that address: five more fill the address's fifteen.
	second, secondID := visitor(2)
	for i := range 5 {
		if status := write("198.51.100.1", second, secondID); status != 200 {
			t.Fatalf("second account write %d: %d", i+1, status)
		}
	}
	if status := write("198.51.100.1", second, secondID); status != 429 {
		t.Fatalf("second account write 6, the address's 16th: %d", status)
	}
	// The same account from another address is held to its own count alone.
	if status := write("198.51.100.2", second, secondID); status != 200 {
		t.Fatalf("second account from another address: %d", status)
	}

	// Reads are not writes, and a member is not a visitor.
	if status, _ := b.do(t, b.visitor, "GET", "/v1/collections/comments/records", ""); status != 200 {
		t.Fatalf("read after the limit: %d", status)
	}
	if status, _ := b.from(t, "198.51.100.1", b.member, "POST", "/v1/collections/comments/records", `{"author":"`+memberID+`","body":"member"}`); status != 200 {
		t.Fatalf("member after the limit: %d", status)
	}

	// Every visitor together: 16 so far, then accounts of their own on
	// addresses of their own until the Base's 120.
	written := 16
	for n := 3; written < 120; n++ {
		token, id := visitor(n)
		for i := 0; i < 10 && written < 120; i++ {
			if status := write(fmt.Sprintf("203.0.113.%d", n), token, id); status != 200 {
				t.Fatalf("visitor write %d: %d", written+1, status)
			}
			written++
		}
	}
	fresh, freshID := visitor(99)
	if status := write("203.0.113.250", fresh, freshID); status != 429 {
		t.Fatalf("the Base's 121st visitor write: %d", status)
	}
}

// A Base whose auth collection cannot say a caller is a visitor admits none.
func TestNoRoleNoVisitor(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	users, err := b.app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	users.Fields.RemoveByName(core.FieldNameAuthRole)
	if err := b.app.SaveNoValidate(users); err != nil {
		t.Fatal(err)
	}

	if status, body := b.do(t, b.visitor, "GET", "/v1/collections/comments/records", ""); status != 403 {
		t.Fatalf("%d %s", status, body)
	}
}

// The collection's own option is what admits visitors, and it is set through
// the collections API like any other.
func TestTheOptionOpensACollection(t *testing.T) {
	t.Parallel()

	b := newVisitorBase(t)
	if status, _ := b.do(t, b.visitor, "GET", "/v1/collections/lure/records", ""); status != 403 {
		t.Fatalf("closed: %d", status)
	}
	if status, body := b.do(t, b.admin, "PATCH", "/v1/collections/lure", `{"visitors":true}`, "X-Org-Id", "acme"); status != 200 || !strings.Contains(body, `"visitors":true`) {
		t.Fatalf("open: %d %s", status, body)
	}
	if status, body := b.do(t, b.visitor, "GET", "/v1/collections/lure/records", ""); status != 200 {
		t.Fatalf("opened: %d %s", status, body)
	}
}
