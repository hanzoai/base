package apis

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// The Supabase data wire, served natively at /rest/v1/{table}.
//
// We run their Studio front end, and its whole vocabulary is table/row/column.
// Base speaks collection/record. Something has to translate, and the only
// question is where: a client-side adapter is a shim every caller carries and
// every new caller reimplements, so the translation belongs here, once, on the
// wire the clients already know.
//
// It is mounted OUTSIDE the api prefix, at the root, because supabase-js builds
// its own URL: given a base host it calls /rest/v1/{table} unprompted. Serving
// exactly that path is what lets a Supabase client point at Base by changing a
// hostname and nothing else — no adapter, no fork, no per-call rewrite.
//
// This is a rendering of the SAME read, not a second one. The door rewrites the
// query into Base's own dialect and then runs recordsList — the same collection
// lookup, the same rate limit, the same list rule, the same field resolver, the
// same enrichment and the same timing throttle. Only the final write differs:
// PostgREST answers a bare array with the count in Content-Range, where Base
// answers the {items,page,perPage,totalItems,totalPages} envelope. Duplicating
// the handler to change one line is how two doors come to disagree about who may
// read a row, which is the one disagreement that matters.
func bindRestApi(app core.App, rg *router.Router[*core.RequestEvent]) {
	sub := rg.Group("/rest/v1")
	sub.GET("/{collection}", restList)
}

// restWire marks a request as arriving on the PostgREST door. It is a context
// value rather than a path check because the renderer is deep inside
// recordsList's event handler, and a path comparison there would be a second
// copy of the routing decision this file already made.
type restWireKey struct{}

// wantsRest reports whether this request arrived on the PostgREST door.
func wantsRest(e *core.RequestEvent) bool {
	return e.Request.Context().Value(restWireKey{}) != nil
}

// restCount reads the Prefer header. PostgREST computes a total only when asked,
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
	q, err := restQuery(e.Request.URL.Query(), restCount(e))
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	e.Request.URL.RawQuery = q.Encode()
	e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), restWireKey{}, true))

	return recordsList(e)
}

// restRender writes the PostgREST shape: the rows as a bare array, and the count
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

// restQuery translates a PostgREST query string into Base's own.
//
// Every value a caller supplies crosses as a dbx placeholder ({:pN}) with the
// value in the params map, never spliced into the expression text — the filter
// DSL is an expression language, so concatenating a caller's string into it is
// injection into the WHERE clause, not merely a quoting bug.
func restQuery(in url.Values, counted bool) (url.Values, error) {
	out := url.Values{}

	if sel := in.Get("select"); sel != "" && sel != "*" {
		out.Set("fields", sel)
	}

	if order := in.Get("order"); order != "" {
		sort, err := restSort(order)
		if err != nil {
			return nil, err
		}
		out.Set("sort", sort)
	}

	limit, offset := 0, 0
	if raw := in.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("limit must be a non-negative integer, got %q", raw)
		}
		limit = n
	}
	if raw := in.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("offset must be a non-negative integer, got %q", raw)
		}
		offset = n
	}
	// Base pages; PostgREST takes an arbitrary offset. Where the two agree the
	// translation is exact. Where they do not, this REFUSES rather than rounding
	// to the nearest page — a silently shifted window returns real rows for the
	// wrong range, which reads as data corruption and not as a paging bug.
	if limit > 0 {
		out.Set("perPage", strconv.Itoa(limit))
		if offset%limit != 0 {
			return nil, fmt.Errorf(
				"offset %d is not a multiple of limit %d; this store pages, so it can only start a window on a page boundary",
				offset, limit)
		}
		out.Set("page", strconv.Itoa(offset/limit+1))
	} else if offset > 0 {
		return nil, fmt.Errorf("offset needs a limit, otherwise there is no window to move")
	}

	if !counted {
		out.Set("skipTotal", "1")
	}

	filter, params, err := restFilter(in)
	if err != nil {
		return nil, err
	}
	if filter != "" {
		out.Set("filter", filter)
		for k, v := range params {
			out.Set(k, v)
		}
	}

	return out, nil
}

// restSort maps `order=col.asc,other.desc` onto Base's `sort=+col,-other`.
// A bare column is ascending, which is PostgREST's default and SQL's.
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
			// nullsfirst/nullslast ride along in PostgREST; this store has no
			// way to express them, and honouring the direction while dropping
			// the null placement would answer a different question silently.
			return "", fmt.Errorf("order %q: only asc and desc are supported", raw)
		}
	}
	return strings.Join(terms, ","), nil
}

// restOps maps PostgREST's operator names onto the filter DSL's symbols. `in`
// and `is` are absent because neither is a binary symbol — they are expanded by
// restFilter into a disjunction and a null/bool comparison respectively.
var restOps = map[string]string{
	"eq":    "=",
	"neq":   "!=",
	"gt":    ">",
	"gte":   ">=",
	"lt":    "<",
	"lte":   "<=",
	"like":  "~",
	"ilike": "~", // this store's ~ is already case-insensitive
	"not":   "!=",
}

// restReserved are the query params that steer the read rather than filter it.
// Anything else is a column predicate.
var restReserved = map[string]bool{
	"select": true, "order": true, "limit": true, "offset": true,
	"on_conflict": true, "columns": true,
}

func restFilter(in url.Values) (string, map[string]string, error) {
	params := map[string]string{}
	terms := []string{}
	n := 0

	// url.Values is a map, so its iteration order is random — sort the columns
	// so one request always produces one filter string. An expression that
	// varies run to run defeats the parsed-filter cache and makes a failure
	// impossible to reproduce from the logs.
	cols := make([]string, 0, len(in))
	for col := range in {
		if !restReserved[col] {
			cols = append(cols, col)
		}
	}
	sortStrings(cols)

	for _, col := range cols {
		for _, raw := range in[col] {
			op, val, has := strings.Cut(raw, ".")
			if !has {
				return "", nil, fmt.Errorf("filter %s=%q needs an operator, e.g. %s=eq.%s", col, raw, col, raw)
			}

			switch op {
			case "is":
				// is.null / is.true / is.false are literals in the DSL, never
				// placeholders: a quoted "null" is the STRING null and would
				// match nothing while looking like it worked.
				switch val {
				case "null", "true", "false":
					terms = append(terms, col+"="+val)
				default:
					return "", nil, fmt.Errorf("filter %s=is.%s: is takes null, true or false", col, val)
				}
			case "in":
				items := restList_(val)
				if len(items) == 0 {
					return "", nil, fmt.Errorf("filter %s=in.%s names no values", col, val)
				}
				alts := make([]string, 0, len(items))
				for _, item := range items {
					key := "p" + strconv.Itoa(n)
					n++
					params[key] = item
					alts = append(alts, col+"={:"+key+"}")
				}
				terms = append(terms, "("+strings.Join(alts, " || ")+")")
			default:
				sym, ok := restOps[op]
				if !ok {
					return "", nil, fmt.Errorf("filter %s=%s.%s: unknown operator %q", col, op, val, op)
				}
				key := "p" + strconv.Itoa(n)
				n++
				if op == "like" || op == "ilike" {
					// PostgREST spells the wildcard *, SQL spells it %.
					val = strings.ReplaceAll(val, "*", "%")
				}
				params[key] = val
				terms = append(terms, col+sym+"{:"+key+"}")
			}
		}
	}

	return strings.Join(terms, " && "), params, nil
}

// restList_ splits PostgREST's `in.(a,b,c)` value list, tolerating the
// parentheses being present or absent and stripping the double quotes it uses
// around values that contain a comma.
func restList_(val string) []string {
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
