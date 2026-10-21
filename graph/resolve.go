package graph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/zap-proto/zip"
)

// Bounds on what one field may ask.
const (
	// pageMost is the most records one page of a connection holds, and
	// pageDefault the size of a page nobody sized.
	pageMost    = 100
	pageDefault = 20
	// hopsMost is how far a selection may reach through relations, counting
	// each relation, back-relation and history it walks. It is checked on the
	// whole selection before any request is made.
	hopsMost = 4
)

// Resolve answers one root field of the graph, as zip's engine asks it.
func (g *Graph) Resolve(ctx context.Context, kind string, s zip.Selection) (any, error) {
	m, _, err := g.current(ctx)
	if err != nil {
		return nil, err
	}
	r := g.run(ctx, m)

	if s.Name == "collections" && kind == "query" {
		if len(s.Args) > 0 {
			return nil, bad("collections takes no arguments")
		}
		return r.collections(ctx)
	}

	rt, ok := m.roots[s.Name]
	if !ok {
		return nil, bad("no %s field %q", kind, s.Name)
	}
	write := slices.Contains([]string{"create", "update", "upsert", "delete"}, rt.op)
	if write != (kind == "mutation") {
		want := "query"
		if write {
			want = "mutation"
		}
		return nil, bad("%q is a %s field; this operation is a %s", s.Name, want, kind)
	}
	if err := takes(s, argsOf[rt.op]...); err != nil {
		return nil, err
	}

	c := rt.c
	var hops int
	switch rt.op {
	case "list":
		hops = m.connHops(c, s.Fields)
	case "groupBy", "delete":
	default:
		hops = m.hops(c, s.Fields)
	}
	if hops > hopsMost {
		return nil, bad("the selection reaches %d relations deep, and a field may reach %d", hops, hopsMost)
	}

	switch rt.op {
	case "one":
		id, err := str(s.Args, "id")
		if err != nil {
			return nil, err
		}
		return r.one(ctx, c, id, s.Fields)
	case "list":
		return r.connection(ctx, c, s.Args, s.Fields, nil)
	case "groupBy":
		return r.groups(ctx, c, s)
	case "create":
		data, err := m.input(c, s.Args["data"])
		if err != nil {
			return nil, err
		}
		return r.write(ctx, c, http.MethodPost, "", data, s.Fields)
	case "update":
		id, err := str(s.Args, "id")
		if err != nil {
			return nil, err
		}
		data, err := m.input(c, s.Args["data"])
		if err != nil {
			return nil, err
		}
		return r.write(ctx, c, http.MethodPatch, id, data, s.Fields)
	case "upsert":
		data, err := m.input(c, s.Args["data"])
		if err != nil {
			return nil, err
		}
		on, _ := s.Args["on"].(string)
		return r.upsert(ctx, c, data, on, s.Fields)
	case "delete":
		id, err := str(s.Args, "id")
		if err != nil {
			return nil, err
		}
		if _, err := r.call(ctx, http.MethodDelete, recordsPath(c)+"/"+url.PathEscape(id), nil, nil); err != nil {
			return nil, err
		}
		return id, nil
	}
	return nil, bad("no %s field %q", kind, s.Name)
}

// argsOf is the arguments each root takes.
var argsOf = map[string][]string{
	"one":     {"id"},
	"list":    {"filter", "orderBy", "first", "after", "last", "before"},
	"groupBy": {"field", "filter"},
	"create":  {"data"},
	"update":  {"id", "data"},
	"upsert":  {"data", "on"},
	"delete":  {"id"},
}

// takes refuses an argument the field does not take.
func takes(s zip.Selection, names ...string) error {
	for k := range s.Args {
		if !slices.Contains(names, k) {
			return bad("unknown argument %q on field %q", k, s.Name)
		}
	}
	return nil
}

func recordsPath(c *coll) string {
	return "/collections/" + url.PathEscape(c.c.Name) + "/records"
}

// ---------------------------------------------------------------------------
// planning

// hops is how far a record selection reaches through relations.
func (m *model) hops(c *coll, sel []zip.Selection) int {
	most := 0
	for _, s := range sel {
		d := 0
		switch f, b := c.byName[s.Name], c.byBack[s.Name]; {
		case f != nil && f.kind == kRelation && f.target != nil && !onlyID(s.Fields):
			d = 1 + m.hops(f.target, s.Fields)
		case b != nil:
			d = 1 + m.connHops(b.from, s.Fields)
		case s.Name == "history" && c.history:
			d = 1
		}
		most = max(most, d)
	}
	return most
}

// connHops is how far a connection's selection reaches.
func (m *model) connHops(c *coll, sel []zip.Selection) int {
	most := 0
	for _, s := range sel {
		if s.Name != "edges" {
			continue
		}
		for _, e := range s.Fields {
			if e.Name == "node" {
				most = max(most, m.hops(c, e.Fields))
			}
		}
	}
	return most
}

// expand is the relations a record selection reads beyond their ids, as Base's
// expand names them, so one request answers the record and what it points at.
func (m *model) expand(c *coll, sel []zip.Selection, at string, out *[]string) {
	for _, s := range sel {
		f := c.byName[s.Name]
		if f == nil || f.kind != kRelation || f.target == nil || onlyID(s.Fields) {
			continue
		}
		p := at + f.name
		if !slices.Contains(*out, p) {
			*out = append(*out, p)
		}
		m.expand(f.target, s.Fields, p+".", out)
	}
}

// onlyID reports whether a selection of a related record reads nothing the
// relation does not already hold.
func onlyID(sel []zip.Selection) bool {
	for _, s := range sel {
		if s.Name != "id" && s.Name != "__typename" {
			return false
		}
	}
	return true
}

func expandOf(m *model, c *coll, sels ...[]zip.Selection) string {
	var paths []string
	for _, sel := range sels {
		m.expand(c, sel, "", &paths)
	}
	return strings.Join(paths, ",")
}

// ---------------------------------------------------------------------------
// records

// one reads one record, or nil when there is none the caller may view.
func (r *run) one(ctx context.Context, c *coll, id string, sel []zip.Selection) (any, error) {
	q := url.Values{}
	if e := expandOf(r.m, c, sel); e != "" {
		q.Set("expand", e)
	}
	res, err := r.call(ctx, http.MethodGet, recordsPath(c)+"/"+url.PathEscape(id), q, nil)
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.record(c, res), nil
}

func (r *run) record(c *coll, v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return &record{r: r, c: c, m: m}
}

// record is one record as Base answered it, answering each selected field.
type record struct {
	r *run
	c *coll
	m map[string]any
}

func (o *record) Type() string { return o.c.typ }

func (o *record) Field(ctx context.Context, s zip.Selection) (any, error) {
	c := o.c
	id, _ := o.m["id"].(string)
	if f := c.byName[s.Name]; f != nil {
		if len(s.Args) > 0 {
			return nil, bad("%s.%s takes no arguments", c.typ, s.Name)
		}
		return o.value(f, s)
	}
	if b := c.byBack[s.Name]; b != nil {
		return o.r.connection(ctx, b.from, s.Args, s.Fields, map[string]any{"field": b.field.name, "op": "is", "value": id})
	}
	if s.Name == "history" && c.history {
		return o.r.history(ctx, c, id, s)
	}
	return nil, bad("%s has no field %q", c.typ, s.Name)
}

func (o *record) value(f *field, s zip.Selection) (any, error) {
	v := o.m[f.name]
	switch {
	case f.kind == kRelation && f.target != nil:
		expanded, _ := o.m["expand"].(map[string]any)
		if !f.multi {
			id, _ := v.(string)
			if id == "" {
				return nil, nil
			}
			if onlyID(s.Fields) {
				return &record{r: o.r, c: f.target, m: map[string]any{"id": id}}, nil
			}
			// a record the caller may not view is not expanded, and reads as none
			return o.r.record(f.target, expanded[f.name]), nil
		}
		var out []any
		if onlyID(s.Fields) {
			ids, _ := v.([]any)
			for _, id := range ids {
				out = append(out, &record{r: o.r, c: f.target, m: map[string]any{"id": id}})
			}
			return out, nil
		}
		items, _ := expanded[f.name].([]any)
		for _, x := range items {
			if rec := o.r.record(f.target, x); rec != nil {
				out = append(out, rec)
			}
		}
		return out, nil
	case f.kind == kComposite:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, nil
		}
		return &compositeValue{c: f.comp, m: m}, nil
	}
	return v, nil
}

// compositeValue is one composite value, answering its parts.
type compositeValue struct {
	c *composite
	m map[string]any
}

func (o *compositeValue) Type() string { return prefix + o.c.typ }

func (o *compositeValue) Field(_ context.Context, s zip.Selection) (any, error) {
	p := o.c.part(s.Name)
	if p == nil {
		return nil, bad("%s%s has no field %q", prefix, o.c.typ, s.Name)
	}
	v := o.m[p.name]
	if p.elem == nil {
		return v, nil
	}
	items, _ := v.([]any)
	out := make([]any, 0, len(items))
	for _, x := range items {
		if m, ok := x.(map[string]any); ok {
			out = append(out, &plain{typ: prefix + p.elem.typ, names: p.elem.fields, m: m})
		}
	}
	return out, nil
}

// plain is an object whose fields are a map's keys.
type plain struct {
	typ   string
	names []string
	m     map[string]any
}

func (o *plain) Type() string { return o.typ }

func (o *plain) Field(_ context.Context, s zip.Selection) (any, error) {
	if !slices.Contains(o.names, s.Name) {
		return nil, bad("%s has no field %q", o.typ, s.Name)
	}
	return o.m[s.Name], nil
}

// ---------------------------------------------------------------------------
// connections

// connection is one page of records and the facts about it, planned from the
// selection: rows only when edges or pageInfo are read, a count only when
// totalCount is, figures only when aggregate is.
type connection struct {
	r      *run
	c      *coll
	where  string // the where clause, as JSON
	rows   []any
	offset int
	next   bool
	prev   bool
	total  int
}

func (r *run) connection(ctx context.Context, c *coll, args map[string]any, sel []zip.Selection, scope map[string]any) (any, error) {
	for k := range args {
		if !slices.Contains(argsOf["list"], k) {
			return nil, bad("unknown argument %q on a %sConnection", k, c.typ)
		}
	}

	var where map[string]any
	if f, ok := args["filter"]; ok && f != nil {
		w, err := r.m.where(c, f, "")
		if err != nil {
			return nil, err
		}
		where = w
	}
	if scope != nil {
		if where == nil {
			where = scope
		} else {
			where = map[string]any{"and": []any{where, scope}}
		}
	}
	sortBy, err := r.m.sortOf(c, args["orderBy"])
	if err != nil {
		return nil, err
	}

	conn := &connection{r: r, c: c}
	if where != nil {
		raw, err := json.Marshal(where)
		if err != nil {
			return nil, err
		}
		conn.where = string(raw)
	}

	var rows, total bool
	var nodes [][]zip.Selection
	for _, s := range sel {
		switch s.Name {
		case "edges":
			rows = true
			for _, e := range s.Fields {
				if e.Name == "node" {
					nodes = append(nodes, e.Fields)
				}
			}
		case "pageInfo":
			rows = true
		case "totalCount":
			total = true
		case "aggregate", "__typename":
		default:
			return nil, bad("%sConnection has no field %q", c.typ, s.Name)
		}
	}
	expand := expandOf(r.m, c, nodes...)

	fetch := func(page, perPage int, counted bool) ([]any, int, error) {
		q := url.Values{"page": {strconv.Itoa(page)}, "perPage": {strconv.Itoa(perPage)}}
		if !counted {
			q.Set("skipTotal", "1")
		}
		if sortBy != "" {
			q.Set("sort", sortBy)
		}
		if expand != "" {
			q.Set("expand", expand)
		}
		if conn.where != "" {
			q.Set("where", conn.where)
		}
		res, err := r.call(ctx, http.MethodGet, recordsPath(c), q, nil)
		if err != nil {
			return nil, 0, err
		}
		obj, _ := res.(map[string]any)
		items, _ := obj["items"].([]any)
		n, _ := obj["totalItems"].(float64)
		return items, int(n), nil
	}

	if !rows && !total {
		return conn, nil
	}

	first, hasFirst, err := count(args, "first")
	if err != nil {
		return nil, err
	}
	last, hasLast, err := count(args, "last")
	if err != nil {
		return nil, err
	}
	after, hasAfter, err := position(args, "after")
	if err != nil {
		return nil, err
	}
	before, hasBefore, err := position(args, "before")
	if err != nil {
		return nil, err
	}
	switch {
	case hasFirst && hasLast:
		return nil, bad("a connection takes first or last, not both")
	case hasFirst && hasBefore, hasLast && hasAfter:
		return nil, bad("first reads on from after, last back from before")
	case !hasFirst && !hasLast:
		first, hasFirst = pageDefault, true
	}

	if !rows {
		_, n, err := fetch(1, 1, true)
		if err != nil {
			return nil, err
		}
		conn.total = n
		return conn, nil
	}

	var offset, n int
	switch {
	case hasFirst:
		n = first
		if hasAfter {
			offset = after + 1
		}
	case hasBefore:
		offset = max(0, before-last)
		n = before - offset
		conn.next = true
	default:
		// the last records: where they start is how many there are
		_, t, err := fetch(1, 1, true)
		if err != nil {
			return nil, err
		}
		conn.total, total = t, false
		offset = max(0, t-last)
		n = t - offset
	}

	items, more, t, err := window(fetch, offset, n, total)
	if err != nil {
		return nil, err
	}
	if total {
		conn.total = t
	}
	if hasFirst {
		conn.next = more
	}
	conn.offset, conn.prev = offset, offset > 0
	for _, x := range items {
		if rec := r.record(c, x); rec != nil {
			conn.rows = append(conn.rows, rec)
		}
	}
	return conn, nil
}

// window reads n records from offset, and whether more follow, through pages
// of n+1: one page when offset falls on one's start, else the two it spans.
func window(fetch func(page, perPage int, counted bool) ([]any, int, error), offset, n int, counted bool) ([]any, bool, int, error) {
	per := n + 1
	page := offset/per + 1
	skip := offset - (page-1)*per

	items, total, err := fetch(page, per, counted)
	if err != nil {
		return nil, false, 0, err
	}
	all := items[min(skip, len(items)):]
	if skip > 0 && len(items) == per {
		more, _, err := fetch(page+1, per, false)
		if err != nil {
			return nil, false, 0, err
		}
		all = append(all, more...)
	}
	if len(all) > n {
		return all[:n], true, total, nil
	}
	return all, false, total, nil
}

func (o *connection) Type() string { return o.c.typ + "Connection" }

func (o *connection) Field(ctx context.Context, s zip.Selection) (any, error) {
	switch s.Name {
	case "edges":
		edges := make([]any, len(o.rows))
		for i, rec := range o.rows {
			edges[i] = &plain{typ: o.c.typ + "Edge", names: []string{"node", "cursor"}, m: map[string]any{
				"node": rec, "cursor": cursor(o.offset + i),
			}}
		}
		return edges, nil
	case "pageInfo":
		info := map[string]any{"hasNextPage": o.next, "hasPreviousPage": o.prev, "startCursor": nil, "endCursor": nil}
		if len(o.rows) > 0 {
			info["startCursor"] = cursor(o.offset)
			info["endCursor"] = cursor(o.offset + len(o.rows) - 1)
		}
		return &plain{typ: "BasePageInfo", names: []string{"hasNextPage", "hasPreviousPage", "startCursor", "endCursor"}, m: info}, nil
	case "totalCount":
		return o.total, nil
	case "aggregate":
		return o.r.aggregate(ctx, o.c, o.where, "", s.Fields)
	}
	return nil, bad("%sConnection has no field %q", o.c.typ, s.Name)
}

// A cursor is a position in the order a connection was asked for.
func cursor(n int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(n)))
}

func position(args map[string]any, name string) (int, bool, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, false, nil
	}
	s, _ := v.(string)
	raw, err := base64.RawURLEncoding.DecodeString(s)
	n, aerr := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
	if err != nil || aerr != nil || !strings.HasPrefix(string(raw), "o:") || n < 0 || n > math.MaxInt32 {
		return 0, false, bad("%s is not a cursor this graph wrote", name)
	}
	return n, true, nil
}

func count(args map[string]any, name string) (int, bool, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, false, nil
	}
	n, err := integer(v)
	if err != nil || n < 0 || n > pageMost {
		return 0, false, bad("%s is a count from 0 to %d", name, pageMost)
	}
	return n, true, nil
}

func integer(v any) (int, error) {
	switch t := v.(type) {
	case int64:
		return int(t), nil
	case int:
		return t, nil
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < math.MaxInt32 {
			return int(t), nil
		}
	}
	return 0, fmt.Errorf("not an integer: %v", v)
}

func str(args map[string]any, name string) (string, error) {
	s, ok := args[name].(string)
	if !ok || s == "" {
		return "", bad("%s is required", name)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// aggregates

// aggregateOps are the figures BaseAggregate answers, each one of Base's
// aggregate operations.
var aggregateOps = []string{
	"count", "countEmpty", "countNotEmpty", "countUnique", "percentEmpty", "percentNotEmpty",
	"sum", "avg", "min", "max", "countTrue", "countFalse",
}

// opOf is the aggregate operation one selected figure is, as the aggregate
// route spells it.
func opOf(s zip.Selection) (string, error) {
	if !slices.Contains(aggregateOps, s.Name) {
		return "", bad("BaseAggregate has no field %q", s.Name)
	}
	if s.Name == "count" {
		if len(s.Args) > 0 {
			return "", bad("count takes no arguments")
		}
		return "count", nil
	}
	field, _ := s.Args["field"].(string)
	if field == "" || len(s.Args) != 1 {
		return "", bad("%s takes one argument, field", s.Name)
	}
	return s.Name + "(" + field + ")", nil
}

func opsOf(sels ...[]zip.Selection) (string, error) {
	var ops []string
	for _, sel := range sels {
		for _, s := range sel {
			if s.Name == "__typename" {
				continue
			}
			op, err := opOf(s)
			if err != nil {
				return "", err
			}
			if !slices.Contains(ops, op) {
				ops = append(ops, op)
			}
		}
	}
	if len(ops) == 0 {
		ops = []string{"count"}
	}
	return strings.Join(ops, ","), nil
}

// aggregate asks the aggregate route for the figures a selection reads, over
// what the where clause selects under the caller's list rule.
func (r *run) aggregate(ctx context.Context, c *coll, where, group string, sel ...[]zip.Selection) (any, error) {
	ops, err := opsOf(sel...)
	if err != nil {
		return nil, err
	}
	q := url.Values{"ops": {ops}}
	if where != "" {
		q.Set("where", where)
	}
	if group != "" {
		q.Set("group", group)
	}
	res, err := r.call(ctx, http.MethodGet, "/collections/"+url.PathEscape(c.c.Name)+"/aggregate", q, nil)
	if err != nil {
		return nil, err
	}
	obj, _ := res.(map[string]any)
	if group != "" {
		items, _ := obj["groups"].([]any)
		out := make([]any, 0, len(items))
		for _, x := range items {
			if m, ok := x.(map[string]any); ok {
				out = append(out, &groupValue{m: m})
			}
		}
		return out, nil
	}
	total, _ := obj["total"].(map[string]any)
	return &figures{m: total}, nil
}

// figures is one set of aggregate answers, keyed by operation.
type figures struct{ m map[string]any }

func (o *figures) Type() string { return "BaseAggregate" }

func (o *figures) Field(_ context.Context, s zip.Selection) (any, error) {
	op, err := opOf(s)
	if err != nil {
		return nil, err
	}
	v, ok := o.m[op]
	if !ok {
		// a figure asked of this value that its group was not asked for
		return nil, bad("%s was not asked of the group", op)
	}
	return v, nil
}

// groupValue is one group of a groupBy: the value and its figures.
type groupValue struct{ m map[string]any }

func (o *groupValue) Type() string { return "BaseGroup" }

func (o *groupValue) Field(_ context.Context, s zip.Selection) (any, error) {
	switch s.Name {
	case "value":
		return o.m["value"], nil
	case "aggregate":
		return &figures{m: o.m}, nil
	}
	return nil, bad("BaseGroup has no field %q", s.Name)
}

// groups answers a groupBy root, asking every figure its selection reads in
// one request.
func (r *run) groups(ctx context.Context, c *coll, s zip.Selection) (any, error) {
	field, err := str(s.Args, "field")
	if err != nil {
		return nil, err
	}
	var where string
	if f, ok := s.Args["filter"]; ok && f != nil {
		w, err := r.m.where(c, f, "")
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(w)
		if err != nil {
			return nil, err
		}
		where = string(raw)
	}
	var sels [][]zip.Selection
	for _, g := range s.Fields {
		switch g.Name {
		case "aggregate":
			sels = append(sels, g.Fields)
		case "value", "__typename":
		default:
			return nil, bad("BaseGroup has no field %q", g.Name)
		}
	}
	return r.aggregate(ctx, c, where, field, sels...)
}

// ---------------------------------------------------------------------------
// history

func (r *run) history(ctx context.Context, c *coll, id string, s zip.Selection) (any, error) {
	if err := takes(s, "first", "after"); err != nil {
		return nil, err
	}
	first, ok, err := count(s.Args, "first")
	if err != nil {
		return nil, err
	}
	if !ok {
		first = pageDefault
	}
	after, hasAfter, err := position(s.Args, "after")
	if err != nil {
		return nil, err
	}
	offset := 0
	if hasAfter {
		offset = after + 1
	}

	fetch := func(page, perPage int, _ bool) ([]any, int, error) {
		q := url.Values{"page": {strconv.Itoa(page)}, "perPage": {strconv.Itoa(perPage)}}
		res, err := r.call(ctx, http.MethodGet, recordsPath(c)+"/"+url.PathEscape(id)+"/history", q, nil)
		if err != nil {
			return nil, 0, err
		}
		obj, _ := res.(map[string]any)
		items, _ := obj["items"].([]any)
		n, _ := obj["totalItems"].(float64)
		return items, int(n), nil
	}
	items, more, total, err := window(fetch, offset, first, true)
	if err != nil {
		return nil, err
	}

	edges := make([]any, 0, len(items))
	for i, x := range items {
		m, _ := x.(map[string]any)
		entry := map[string]any{}
		for k, v := range m {
			entry[k] = v
		}
		if a, ok := m["actor"].(map[string]any); ok {
			entry["actor"] = &compositeValue{c: actorShape, m: a}
		}
		edges = append(edges, &plain{typ: "BaseHistoryEdge", names: []string{"node", "cursor"}, m: map[string]any{
			"node":   &plain{typ: "BaseHistoryEntry", names: []string{"id", "action", "actor", "changes", "created"}, m: entry},
			"cursor": cursor(offset + i),
		}})
	}
	info := map[string]any{"hasNextPage": more, "hasPreviousPage": offset > 0, "startCursor": nil, "endCursor": nil}
	if len(items) > 0 {
		info["startCursor"], info["endCursor"] = cursor(offset), cursor(offset+len(items)-1)
	}
	return &plain{typ: "BaseHistoryConnection", names: []string{"edges", "pageInfo", "totalCount"}, m: map[string]any{
		"edges":      edges,
		"pageInfo":   &plain{typ: "BasePageInfo", names: []string{"hasNextPage", "hasPreviousPage", "startCursor", "endCursor"}, m: info},
		"totalCount": total,
	}}, nil
}

var actorShape = composites["actor"]

// ---------------------------------------------------------------------------
// writes

// input checks a write's data against the collection's Input: a key it does not
// name is refused rather than dropped, as zip refuses an unknown argument.
func (m *model) input(c *coll, v any) (map[string]any, error) {
	data, ok := v.(map[string]any)
	if !ok {
		return nil, bad("data is a %sInput", c.typ)
	}
	for k, x := range data {
		f := c.byName[k]
		if f == nil || !f.writable() {
			return nil, bad("%sInput has no field %q", c.typ, k)
		}
		if f.kind != kComposite || x == nil {
			continue
		}
		parts, ok := x.(map[string]any)
		if !ok {
			return nil, bad("%s is a %s%sInput", k, prefix, f.comp.typ)
		}
		for pk, pv := range parts {
			p := f.comp.part(pk)
			if p == nil {
				return nil, bad("%s%sInput has no field %q", prefix, f.comp.typ, pk)
			}
			if p.elem == nil || pv == nil {
				continue
			}
			items, _ := pv.([]any)
			for _, item := range items {
				obj, _ := item.(map[string]any)
				for ek := range obj {
					if !slices.Contains(p.elem.fields, ek) {
						return nil, bad("%s%sInput has no field %q", prefix, p.elem.typ, ek)
					}
				}
			}
		}
	}
	return data, nil
}

// write creates a record (id "") or updates one, and answers it as the caller
// may view it.
func (r *run) write(ctx context.Context, c *coll, method, id string, data map[string]any, sel []zip.Selection) (any, error) {
	q := url.Values{}
	if e := expandOf(r.m, c, sel); e != "" {
		q.Set("expand", e)
	}
	path := recordsPath(c)
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	res, err := r.call(ctx, method, path, q, data)
	if err != nil {
		return nil, err
	}
	return r.record(c, res), nil
}

// upsert updates the record whose `on` holds the value data gives it, or
// creates one. The read and the write are two requests, each under the
// caller's rules, so two callers upserting one value at once can both find
// nothing; a unique index on `on` makes the second create fail rather than
// write a twin.
func (r *run) upsert(ctx context.Context, c *coll, data map[string]any, on string, sel []zip.Selection) (any, error) {
	if on == "" {
		on = "id"
	}
	value := lookup(data, on)
	switch value.(type) {
	case string, float64, int64, bool:
	default:
		return nil, bad("upsert on %q needs data to give it a value", on)
	}
	if value == "" {
		return nil, bad("upsert on %q needs data to give it a value", on)
	}

	if on == "id" {
		id := value.(string)
		_, err := r.call(ctx, http.MethodGet, recordsPath(c)+"/"+url.PathEscape(id), nil, nil)
		switch {
		case isNotFound(err):
			return r.write(ctx, c, http.MethodPost, "", data, sel)
		case err != nil:
			return nil, err
		}
		patch := map[string]any{}
		for k, v := range data {
			if k != "id" {
				patch[k] = v
			}
		}
		return r.write(ctx, c, http.MethodPatch, id, patch, sel)
	}

	head, _, _ := strings.Cut(on, ".")
	if f := c.byName[head]; f == nil || !f.filterable() {
		return nil, bad("%s has no field %q to upsert on", c.typ, on)
	}
	where, err := json.Marshal(map[string]any{"field": on, "op": "is", "value": value})
	if err != nil {
		return nil, err
	}
	res, err := r.call(ctx, http.MethodGet, recordsPath(c), url.Values{
		"page": {"1"}, "perPage": {"2"}, "skipTotal": {"1"}, "where": {string(where)},
	}, nil)
	if err != nil {
		return nil, err
	}
	obj, _ := res.(map[string]any)
	items, _ := obj["items"].([]any)
	switch len(items) {
	case 0:
		return r.write(ctx, c, http.MethodPost, "", data, sel)
	case 1:
		found, _ := items[0].(map[string]any)
		id, _ := found["id"].(string)
		return r.write(ctx, c, http.MethodPatch, id, data, sel)
	}
	return nil, bad("more than one %s has %s %v; upsert on a field that names one record", label(c), on, value)
}

// lookup reads a dotted path out of data.
func lookup(data map[string]any, path string) any {
	var v any = data
	for _, seg := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[seg]
	}
	return v
}

// ---------------------------------------------------------------------------
// collections

// collections answers this Base's collections as its own collections route
// lists them for the caller, the ones the graph types.
func (r *run) collections(ctx context.Context) (any, error) {
	res, err := r.call(ctx, http.MethodGet, "/collections", url.Values{
		"page": {"1"}, "perPage": {"500"}, "skipTotal": {"1"}, "sort": {"name"},
	}, nil)
	if err != nil {
		return nil, err
	}
	obj, _ := res.(map[string]any)
	items, _ := obj["items"].([]any)
	out := []any{}
	for _, x := range items {
		m, _ := x.(map[string]any)
		id, _ := m["id"].(string)
		if _, ok := r.m.byID[id]; !ok {
			continue
		}
		out = append(out, &plain{typ: "BaseCollection", names: []string{"id", "name", "type", "label", "plural", "icon", "description", "fields"}, m: m})
	}
	return out, nil
}
