package apis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/search"
	"github.com/hanzoai/orm/query"
)

// The REST data wire: one table per path, filters in the query string.
//
// Our console speaks table/row/column. Base speaks collection/record. Something has to translate, and the only
// question is where: a client-side adapter is a shim every caller carries and
// every new caller reimplements, so the translation belongs here, once, on the
// wire the clients already know.
//
// It is mounted UNDER the api prefix, so its address is `<prefix>/rest/{collection}`
// — `/v1/rest/...` on a Base that has its origin to itself, `/v1/base/rest/...`
// where a host mounts several apps. One address scheme, `/v1` first, the same one
// every other wire here answers on.
//
// It used to sit at the root, at `/rest/v1/{collection}`, so that a REST client
// pointed at a bare hostname would find it with no configuration. That bought
// exactly one thing — a client that hardcodes its own path — and cost the rule
// that every address this fleet serves begins `/v1`. The clients are ours; they
// can be told where the API is rooted, and now are. A path that spells a prefix
// before its version is a second scheme, and there is one.
//
// This is a rendering of the SAME read, not a second one. The door rewrites the
// query into Base's own dialect and then runs recordsList — the same collection
// lookup, the same rate limit, the same list rule, the same field resolver, the
// same enrichment and the same timing throttle. Only the final write differs:
// This wire answers a bare array with the count in Content-Range, where Base's
// own answers the {items,page,perPage,totalItems,totalPages} envelope. Duplicating
// the handler to change one line is how two doors come to disagree about who may
// read a row, which is the one disagreement that matters.
func bindRestApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/rest")
	sub.GET("/{collection}", restList)
	// A count query is a HEAD: a REST client's .select('*', {count, head: true})
	// issues one and reads the total out of Content-Range, because the rows are
	// exactly what it does not want. Without this the count path 404s while every
	// other read works, which reads as "counting is broken" rather than "that
	// verb is unrouted".
	sub.HEAD("/{collection}", restList)
	sub.POST("/{collection}", restCreate)
	sub.PATCH("/{collection}", restUpdate)
	sub.DELETE("/{collection}", restDelete)
}

// restWire marks a request as arriving on the this wire door. It is a context
// value rather than a path check because the renderer is deep inside
// recordsList's event handler, and a path comparison there would be a second
// copy of the routing decision this file already made.
type restWireKey struct{}

// restCall is what the door parsed out of the request: the predicates it could
// not express as query params, carried as DATA so recordsList can bind them
// against the collection's own field resolver.
type restCall struct {
	preds []restPredicate

	// hold suppresses the per-record response write so a filtered write that
	// touches N rows answers ONCE. Base's write handlers are by-id and each
	// writes its own response; This wire's PATCH and DELETE are filtered, so the
	// door drives those handlers per row and renders the whole set itself.
	hold bool

	// rows are what those per-record writes would have rendered, in order.
	rows []*core.Record
}

// wantsRest reports whether this request arrived on the this wire door.
func wantsRest(e *core.RequestEvent) bool {
	return restCallOf(e) != nil
}

func restCallOf(e *core.RequestEvent) *restCall {
	c, _ := e.Request.Context().Value(restWireKey{}).(*restCall)
	return c
}

// restCount reads the Prefer header. The wire computes a total only when asked,
// and Base's search provider likewise skips the count unless it has to — so
// "count not requested" maps onto skipTotal rather than onto a discarded number.
func restCount(e *core.RequestEvent) bool {
	for _, raw := range e.Request.Header.Values("Prefer") {
		for _, part := range strings.Split(raw, ",") {
			if strings.HasPrefix(strings.TrimSpace(part), "count=") {
				return true
			}
		}
	}
	return false
}

func restList(e *core.RequestEvent) error {
	q, preds, err := restQuery(e.Request.URL.Query(), restCount(e))
	if err != nil {
		return restError(e, http.StatusBadRequest, "bad_query", err.Error(), "")
	}

	e.Request.URL.RawQuery = q.Encode()
	e.Request = e.Request.WithContext(
		context.WithValue(e.Request.Context(), restWireKey{}, &restCall{preds: preds}))

	return restFail(e, recordsList(e))
}

// restRender writes the this wire shape: the rows as a bare array, and the count
// in Content-Range rather than in the body. A client that asked for no count
// gets `*/*` — the honest answer, since the total was never computed. Ranges are
// inclusive and zero-based, so an empty page is `*/N` and not `0--1/N`.
func restRender(e *core.RequestEvent, records []*core.Record, total int, counted bool) error {
	from := 0
	if p, per := restPaging(e); per > 0 {
		from = per * (p - 1)
	}

	span := "*"
	if len(records) > 0 {
		span = strconv.Itoa(from) + "-" + strconv.Itoa(from+len(records)-1)
	}
	size := "*"
	if counted {
		size = strconv.Itoa(total)
	}
	e.Response.Header().Set("Content-Range", span+"/"+size)

	// HEAD asked for the headers. Writing the rows anyway would be answering a
	// question nobody asked and paying to serialise it.
	if e.Request.Method == http.MethodHead {
		return e.NoContent(http.StatusOK)
	}

	if records == nil {
		records = []*core.Record{}
	}
	return e.JSON(http.StatusOK, records)
}

// restPaging recovers the page/perPage this request was translated into, so the
// Content-Range offset is derived from what actually ran rather than from a
// second reading of the original query.
func restPaging(e *core.RequestEvent) (page int, perPage int) {
	q := e.Request.URL.Query()
	page, _ = strconv.Atoi(q.Get("page"))
	perPage, _ = strconv.Atoi(q.Get("perPage"))
	if page < 1 {
		page = 1
	}
	return page, perPage
}

// restQuery translates a wire query string into Base's own.
//
// Every value a caller supplies crosses as a dbx placeholder ({:pN}) with the
// value in the params map, never spliced into the expression text — the filter
// DSL is an expression language, so concatenating a caller's string into it is
// injection into the WHERE clause, not merely a quoting bug.
func restQuery(in url.Values, counted bool) (url.Values, []restPredicate, error) {
	out := url.Values{}

	if sel := in.Get("select"); sel != "" && sel != "*" {
		out.Set("fields", sel)
	}

	if order := in.Get("order"); order != "" {
		sort, err := restSort(order)
		if err != nil {
			return nil, nil, err
		}
		out.Set("sort", sort)
	}

	limit, offset := 0, 0
	if raw := in.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, nil, fmt.Errorf("limit must be a non-negative integer, got %q", raw)
		}
		limit = n
	}
	if raw := in.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, nil, fmt.Errorf("offset must be a non-negative integer, got %q", raw)
		}
		offset = n
	}
	// Base pages; the wire takes an arbitrary offset. Where the two agree the
	// translation is exact. Where they do not, this REFUSES rather than rounding
	// to the nearest page — a silently shifted window returns real rows for the
	// wrong range, which reads as data corruption and not as a paging bug.
	if limit > 0 {
		out.Set("perPage", strconv.Itoa(limit))
		if offset%limit != 0 {
			return nil, nil, fmt.Errorf(
				"offset %d is not a multiple of limit %d; this store pages, so it can only start a window on a page boundary",
				offset, limit)
		}
		out.Set("page", strconv.Itoa(offset/limit+1))
	} else if offset > 0 {
		return nil, nil, fmt.Errorf("offset needs a limit, otherwise there is no window to move")
	}

	if !counted {
		out.Set("skipTotal", "1")
	}

	preds, err := restPredicates(in)
	if err != nil {
		return nil, nil, err
	}

	return out, preds, nil
}

// restPredicate is one column comparison, still as the caller wrote it. The
// value is NOT rendered into any expression here — it travels as data until
// restWhere binds it, which is the whole point of the type existing.
type restPredicate struct {
	Col  string
	Op   string
	Vals []string

	// Negate carries the wire's `not.` PREFIX, which is not an operator: it
	// wraps whatever follows. `not.is.null` is how the wire spells IS NOT NULL,
	// and the table editor offers exactly that, so rejecting the prefix would
	// leave a filter the grid can build and the server cannot answer.
	Negate bool
}

// restReserved are the query params that steer the read rather than filter it.
// Anything else is a column predicate.
var restReserved = map[string]bool{
	"select": true, "order": true, "limit": true, "offset": true,
	"on_conflict": true, "columns": true,
}

// restOps maps the wire's operator names onto SQL. `in` and `is` are absent
// because neither is a binary operator — restWhere expands them.
//
// neq is `<>` and not this store's null-safe `IS DISTINCT FROM`. The two return
// DIFFERENT ROWS against a nullable column: `<>` drops NULLs, the null-safe form
// keeps them. We serve the wire's wire, so a query written against another store has
// to return what another store returns; quietly improving on it is how a migrated app
// starts showing rows the developer filtered out.
var restOps = map[string]string{
	"eq":  "=",
	"neq": "<>",
	"gt":  ">",
	"gte": ">=",
	"lt":  "<",
	"lte": "<=",
}

// restPredicates reads the column comparisons out of a wire query.
//
// Column order is sorted because url.Values iterates randomly, and an expression
// that varies run to run cannot be reproduced from a log.
func restPredicates(in url.Values) ([]restPredicate, error) {
	cols := make([]string, 0, len(in))
	for col := range in {
		if !restReserved[col] {
			cols = append(cols, col)
		}
	}
	sortStrings(cols)

	preds := []restPredicate{}
	for _, col := range cols {
		for _, raw := range in[col] {
			negate := false
			if rest, ok := strings.CutPrefix(raw, "not."); ok {
				negate, raw = true, rest
			}
			op, val, has := strings.Cut(raw, ".")
			if !has {
				return nil, fmt.Errorf("filter %s=%q needs an operator, e.g. %s=eq.%s", col, raw, col, raw)
			}
			switch op {
			case "is":
				if val != "null" && val != "true" && val != "false" {
					return nil, fmt.Errorf("filter %s=is.%s: is takes null, true or false", col, val)
				}
				preds = append(preds, restPredicate{Col: col, Op: op, Vals: []string{val}, Negate: negate})
			case "in":
				vals := restValues(val)
				if len(vals) == 0 {
					return nil, fmt.Errorf("filter %s=in.%s names no values", col, val)
				}
				preds = append(preds, restPredicate{Col: col, Op: op, Vals: vals, Negate: negate})
			case "like", "ilike":
				// This wire spells the wildcard *, SQL spells it %.
				preds = append(preds, restPredicate{Col: col, Op: op, Vals: []string{strings.ReplaceAll(val, "*", "%")}, Negate: negate})
			default:
				if _, ok := restOps[op]; !ok {
					return nil, fmt.Errorf("filter %s=%s.%s: unknown operator %q", col, op, val, op)
				}
				preds = append(preds, restPredicate{Col: col, Op: op, Vals: []string{val}, Negate: negate})
			}
		}
	}
	return preds, nil
}

// restWhere turns the predicates into ONE bound expression.
//
// It deliberately does not go through FilterData. That mechanism substitutes
// placeholders with strings.ReplaceAll + strconv.Quote and then re-parses the
// result as an expression — server-side templating for API rules, where the
// template is ours. Feeding a caller's value through it would make quoting the
// only thing between a query string and the WHERE clause, which is not a
// boundary anything should rest on. Here the column becomes a resolved
// identifier and the value becomes a bound parameter, so neither is ever text
// in an expression.
func restWhere(resolver search.FieldResolver, preds []restPredicate) (query.Expression, error) {
	if len(preds) == 0 {
		return nil, nil
	}

	exprs := make([]query.Expression, 0, len(preds))
	n := 0

	for _, p := range preds {
		// The COLUMN is caller-supplied too. Resolving it is what proves it names
		// a real field on this collection and yields an identifier safe to place
		// in SQL; splicing p.Col directly would be injection through the other
		// half of the predicate.
		left, err := resolver.Resolve(p.Col)
		if err != nil {
			return nil, fmt.Errorf("filter %s: %w", p.Col, err)
		}

		switch p.Op {
		case "is":
			switch p.Vals[0] {
			case "null":
				exprs = append(exprs, query.NewExp(left.Identifier+" IS NULL", left.Params))
			default:
				key := "rp" + strconv.Itoa(n)
				n++
				params := query.Params{key: p.Vals[0] == "true"}
				exprs = append(exprs, query.NewExp(left.Identifier+" = {:"+key+"}", mergeRestParams(left.Params, params)))
			}
		case "in":
			slots := make([]string, 0, len(p.Vals))
			params := query.Params{}
			for _, v := range p.Vals {
				key := "rp" + strconv.Itoa(n)
				n++
				params[key] = v
				slots = append(slots, "{:"+key+"}")
			}
			exprs = append(exprs, query.NewExp(
				left.Identifier+" IN ("+strings.Join(slots, ", ")+")",
				mergeRestParams(left.Params, params)))
		case "like", "ilike":
			key := "rp" + strconv.Itoa(n)
			n++
			params := query.Params{key: p.Vals[0]}
			sql := left.Identifier + " LIKE {:" + key + "} ESCAPE '\\'"
			if p.Op == "ilike" {
				// Case-insensitivity stated in the query rather than inherited
				// from the engine's collation, which differs between the two
				// this store runs on.
				sql = "LOWER(" + left.Identifier + ") LIKE LOWER({:" + key + "}) ESCAPE '\\'"
			}
			exprs = append(exprs, query.NewExp(sql, mergeRestParams(left.Params, params)))
		default:
			key := "rp" + strconv.Itoa(n)
			n++
			params := query.Params{key: p.Vals[0]}
			exprs = append(exprs, query.NewExp(
				left.Identifier+" "+restOps[p.Op]+" {:"+key+"}",
				mergeRestParams(left.Params, params)))
		}

		if p.Negate {
			exprs[len(exprs)-1] = query.Not(exprs[len(exprs)-1])
		}

		if left.AfterBuild != nil {
			exprs[len(exprs)-1] = left.AfterBuild(exprs[len(exprs)-1])
		}
	}

	return query.And(exprs...), nil
}

func mergeRestParams(sets ...query.Params) query.Params {
	out := query.Params{}
	for _, set := range sets {
		for k, v := range set {
			out[k] = v
		}
	}
	return out
}

// restValues splits the wire's `in.(a,b,c)` list, tolerating the parentheses
// being present or absent and stripping the quotes it uses around a value that
// contains a comma.
func restValues(val string) []string {
	val = strings.TrimPrefix(strings.TrimSuffix(val, ")"), "(")
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.TrimPrefix(strings.TrimSuffix(p, `"`), `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// restSort maps `order=col.asc,other.desc` onto Base's `sort=+col,-other`.
// A bare column is ascending, which is this wire's default and SQL's.
func restSort(order string) (string, error) {
	parts := strings.Split(order, ",")
	terms := make([]string, 0, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		col, dir, has := strings.Cut(raw, ".")
		if col == "" {
			return "", fmt.Errorf("order term %q names no column", raw)
		}
		switch {
		case !has, dir == "asc":
			terms = append(terms, "+"+col)
		case dir == "desc":
			terms = append(terms, "-"+col)
		default:
			// nullsfirst/nullslast ride along on this wire; this store has no way
			// to express them, and honouring the direction while dropping the
			// null placement would answer a different question silently.
			return "", fmt.Errorf("order %q: only asc and desc are supported", raw)
		}
	}
	return strings.Join(terms, ","), nil
}

// --- writes -----------------------------------------------------------------
//
// The wire's writes are FILTERED where Base's are by-id: PATCH /posts?id=eq.7
// updates every row the filter selects, and Base's handler updates the one row
// its path names. So the door resolves the filter to rows first and then drives
// Base's own handler once per row, which is what keeps the create/update/delete
// RULES doing the deciding — a write that reimplemented them would be a second
// answer to who may change a row.
//
// Rows are resolved through the same list read a GET does, so a caller can only
// touch what they can already see; the per-row handler then applies the write
// rule on top. Visibility and mutation stay two separate questions.

// restBodyLimit bounds how many rows one filtered write may touch. The wire states no such bound and no client applies one, which is how an unfiltered DELETE
// empties a table. Refusing past a ceiling turns the catastrophic case into an
// error someone reads.
const restWriteCeiling = 500

// restCreate inserts one row. A bulk insert (a JSON array body) is refused
// rather than looped: The wire inserts an array in ONE statement, so a partial
// failure rolls the whole thing back, and looping would half-apply it and report
// success for the rows that landed. Base's own batch plane is the honest home
// for that, and it ships disabled.
func restCreate(e *core.RequestEvent) error {
	body, err := io.ReadAll(io.LimitReader(e.Request.Body, 1<<20))
	if err != nil {
		return restError(e, http.StatusBadRequest, "bad_body", "unreadable request body", err.Error())
	}
	if bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), []byte("[")) {
		return restError(e, http.StatusNotImplemented, "bad_query",
			"inserting several rows in one request is not supported here",
			"send one object per request; a true bulk insert has to be one statement to roll back as one")
	}
	e.Request.Body = io.NopCloser(bytes.NewReader(body))

	call := &restCall{hold: true}
	e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), restWireKey{}, call))

	if err := recordCreate(true, nil)(e); err != nil {
		return restFail(e, err)
	}

	if restWantsObject(e) && len(call.rows) != 1 {
		return restError(e, http.StatusNotAcceptable, "not_one_row",
			"JSON object requested, multiple (or no) rows returned",
			fmt.Sprintf("Results contain %d rows, application/vnd.hanzo.object+json requires 1 row", len(call.rows)))
	}
	if !restReturns(e) {
		return e.NoContent(http.StatusCreated)
	}
	if restWantsObject(e) {
		return e.JSON(http.StatusCreated, call.rows[0])
	}
	return e.JSON(http.StatusCreated, call.rows)
}

// restUpdate applies one patch to every row a filter selects.
func restUpdate(e *core.RequestEvent) error {
	return restWriteOverRows(e, func(e *core.RequestEvent, id string) error {
		e.Request.SetPathValue("id", id)
		return recordUpdate(true, nil)(e)
	})
}

// restDelete removes every row a filter selects.
func restDelete(e *core.RequestEvent) error {
	return restWriteOverRows(e, func(e *core.RequestEvent, id string) error {
		e.Request.SetPathValue("id", id)
		return recordDelete(true, nil)(e)
	})
}

// restWriteOverRows is the shared body of the two filtered writes: resolve the
// filter to ids, refuse the shapes that are too dangerous to guess at, then run
// the per-row handler with its own response held back so the door answers once.
func restWriteOverRows(e *core.RequestEvent, apply func(*core.RequestEvent, string) error) error {
	collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue("collection"))
	if err != nil || collection == nil {
		return e.NotFoundError("Missing collection context.", err)
	}

	preds, err := restPredicates(e.Request.URL.Query())
	if err != nil {
		return restError(e, http.StatusBadRequest, "bad_query", err.Error(), "")
	}
	// An unfiltered PATCH or DELETE means every row. this wire allows it and the
	// client sends it without comment, so the first time anyone discovers it is
	// after the table is empty. This refuses instead, and says how to mean it.
	if len(preds) == 0 {
		return restError(e, http.StatusBadRequest, "bad_query",
			"a filtered write needs a filter; add one (e.g. ?id=eq.<id>) to say which rows you mean",
			"without a filter this would touch every row in the table")
	}

	ids, err := restMatchingIDs(e, collection, preds)
	if err != nil {
		return err
	}

	call := &restCall{preds: preds, hold: true}
	e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), restWireKey{}, call))

	for _, id := range ids {
		if err := apply(e, id); err != nil {
			return restFail(e, err)
		}
	}

	return restWriteRender(e, call.rows, len(ids))
}

// restMatchingIDs resolves a filter to the ids of the rows it selects, through
// the same list rule a GET obeys.
func restMatchingIDs(e *core.RequestEvent, collection *core.Collection, preds []restPredicate) ([]string, error) {
	requestInfo, err := e.RequestInfo()
	if err != nil {
		return nil, firstApiError(err, e.BadRequestError("", err))
	}

	if collection.ListRule == nil && !requestInfo.HasSuperuserAuth() {
		return nil, e.ForbiddenError("Only superusers can perform this action.", nil)
	}
	if err := checkForSuperuserOnlyRuleFields(requestInfo); err != nil {
		return nil, err
	}

	q := e.App.RecordQuery(collection)
	resolver := core.NewRecordFieldResolver(e.App, collection, requestInfo, true)
	resolver.SetAllowHiddenFields(requestInfo.HasSuperuserAuth())

	if !requestInfo.HasSuperuserAuth() && collection.ListRule != nil && *collection.ListRule != "" {
		expr, err := search.FilterData(*collection.ListRule).BuildExpr(resolver)
		if err != nil {
			return nil, err
		}
		q.AndWhere(expr)
	}

	where, err := restWhere(resolver, preds)
	if err != nil {
		return nil, restError(e, http.StatusBadRequest, "bad_query", err.Error(), "")
	}
	if where != nil {
		q.AndWhere(where)
	}
	if err := resolver.UpdateQuery(q); err != nil {
		return nil, err
	}

	records := []*core.Record{}
	if err := q.Limit(restWriteCeiling + 1).All(&records); err != nil {
		return nil, firstApiError(err, e.InternalServerError("Failed to resolve the affected rows.", err))
	}
	if len(records) > restWriteCeiling {
		return nil, restError(e, http.StatusBadRequest, "bad_query",
			fmt.Sprintf("this filter selects more than %d rows", restWriteCeiling),
			"narrow the filter, or make the change in batches")
	}

	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.Id)
	}
	return ids, nil
}

// restFault is the wire's error body, and its field names are not decoration: a
// client BRANCHES on `code`.
//
// Two vocabularies meet there, and the split is the point. A constraint the
// STORE refused answers with the store's own SQLSTATE — `23505` for a duplicate
// — because that is a fact about the data and every SQL engine already agrees
// on how to say it. A request the WIRE would not accept answers with a word of
// ours (`bad_query`, `bad_body`, `not_one_row`, `refused`, `not_found`,
// `store_failed`), because that is a fact about this API and nobody else gets
// to name it. `details` carries the specifics a person acts on.
//
// Base's own {data,message,status} body carries neither, which is why this is a
// second rendering rather than the same body under another name.
type restFault struct {
	Message string `json:"message"`
	Details string `json:"details"`
	Hint    string `json:"hint"`
	Code    string `json:"code"`
}

func restError(e *core.RequestEvent, status int, code, message, details string) error {
	return e.JSON(status, restFault{Message: message, Details: details, Code: code})
}

// restWantsObject reports whether the caller asked for a bare object rather than
// an array — .single(), and .maybeSingle() on anything but a GET.
func restWantsObject(e *core.RequestEvent) bool {
	return strings.Contains(e.Request.Header.Get("Accept"), "application/vnd.hanzo.object+json")
}

// restReturns reports what the caller wants back. The wire returns nothing unless asked, and a client reads an empty body as
// exactly that.
func restReturns(e *core.RequestEvent) bool {
	for _, raw := range e.Request.Header.Values("Prefer") {
		if strings.Contains(raw, "return=representation") {
			return true
		}
	}
	return false
}

// restWriteRender answers a filtered write once, whatever it touched.
//
// Cardinality is the CALLER's claim: an Accept of `application/vnd.hanzo.object
// +json` says the write hits exactly one row, and anything else is 406
// not_one_row carrying the count in `details`, so a caller that meant to touch
// one row is told how many it actually would have.
func restWriteRender(e *core.RequestEvent, rows []*core.Record, affected int) error {
	if restWantsObject(e) && len(rows) != 1 {
		return restError(e, http.StatusNotAcceptable, "not_one_row",
			"JSON object requested, multiple (or no) rows returned",
			fmt.Sprintf("Results contain %d rows, application/vnd.hanzo.object+json requires 1 row", len(rows)))
	}

	if !restReturns(e) {
		e.Response.Header().Set("Content-Range", "*/"+strconv.Itoa(affected))
		return e.NoContent(http.StatusNoContent)
	}

	e.Response.Header().Set("Content-Range", "*/"+strconv.Itoa(affected))
	if restWantsObject(e) {
		return e.JSON(http.StatusOK, rows[0])
	}
	if rows == nil {
		rows = []*core.Record{}
	}
	return e.JSON(http.StatusOK, rows)
}

// restFail renders any failure on this door in the wire's error shape.
//
// A client reads `code` and branches on it, so an error that arrives as Base's
// {data,message,status} is an error the caller cannot act on: every
// `error.code === '23505'` branch in a migrated app is dead against that body.
// A unique violation is the one that matters most, because handling a duplicate
// is ordinary application logic rather than an exceptional case — it becomes 409
// with the SQLSTATE, which is what the wire states and what clients test for.
func restFail(e *core.RequestEvent, err error) error {
	if err == nil {
		return nil
	}

	var apiErr *router.ApiError
	if !errors.As(err, &apiErr) {
		return restError(e, http.StatusInternalServerError, "store_failed", err.Error(), "")
	}

	if field, ok := restNotUnique(apiErr); ok {
		return restError(e, http.StatusConflict, "23505",
			"duplicate key value violates unique constraint",
			fmt.Sprintf("Key (%s) already exists.", field))
	}

	details := ""
	if len(apiErr.Data) > 0 {
		if raw, mErr := json.Marshal(apiErr.Data); mErr == nil {
			details = string(raw)
		}
	}
	return restError(e, apiErr.Status, restCodeFor(apiErr.Status), apiErr.Message, details)
}

// restNotUnique reports the first field a validation error rejected as
// non-unique. The normalizer upstream turns both engines' wording into this one
// code, so the door reads a code rather than matching an engine's prose.
func restNotUnique(apiErr *router.ApiError) (string, bool) {
	for field, raw := range apiErr.Data {
		if v, ok := raw.(validation.Error); ok && v.Code() == "validation_not_unique" {
			return field, true
		}
		if m, ok := raw.(map[string]any); ok && m["code"] == "validation_not_unique" {
			return field, true
		}
	}
	return "", false
}

// restCodeFor gives a failure the code its status implies, for the errors that
// arrive already carrying a status and nothing else. It is deliberately coarse:
// a wrong specific code is worse than an honest general one, because a client
// branching on it would take the wrong path with confidence.
func restCodeFor(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_query"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "refused"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusNotAcceptable:
		return "not_one_row"
	default:
		return "store_failed"
	}
}
