package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // a where clause names its time zone, and an image may carry no zoneinfo

	"github.com/hanzoai/base/tools/search"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

// A where clause is a filter written as data rather than as text: what a saved
// view, a segment or a client that never composes filter strings sends.
//
//	{"and": [node, ...]}  {"or": [node, ...]}  {"not": node}
//	{"field": "a.b", "op": "is", "value": ..., "tz": "Europe/Paris", "weekStart": 1}
//	{"op": "inSegment", "value": "<segment id>"}
//
// [CompileWhere] turns one into Base's own filter language, with every value
// bound as a parameter, so it is applied by the same resolver, under the same
// rules, as a `filter` the caller wrote by hand. There is no second query engine:
// a where clause is a way to SAY a filter, checked against the collection's
// fields and the operators each field's type admits.
//
// The operator a leaf may use depends on the type its path ends on:
//
//	text, email, url, editor  contains notContains is isNot empty notEmpty
//	number, rating            is isNot gt gte lt lte empty notEmpty
//	bool                      is
//	date, autodate            is isNot gt gte lt lte today past future relative empty notEmpty
//	select, relation          is isNot (any of / none of) contains notContains (multi) empty notEmpty
//	json, file                contains notContains empty notEmpty
//	a composite value         contains notContains (over its text parts) empty notEmpty
//	a composite part          the operators of its kind (amountMicros, addressLat and addressLng are numbers)
//
// A path may walk a relation (`company.name`) or a back-relation
// (`list_members_via_person.list`, which is how "in list" is asked). The value
// `@me` is the caller (`@request.auth.id`). Base's filter language has no NOT, so
// a negation is pushed down to the leaves (De Morgan), each of which knows its
// own opposite — an empty value counts as not matching "does not contain".

// The operators of a where clause.
const (
	WhereIs           = "is"
	WhereIsNot        = "isNot"
	WhereContains     = "contains"
	WhereNotContains  = "notContains"
	WhereGt           = "gt"
	WhereGte          = "gte"
	WhereLt           = "lt"
	WhereLte          = "lte"
	WhereEmpty        = "empty"
	WhereNotEmpty     = "notEmpty"
	WhereToday        = "today"
	WherePast         = "past"
	WhereFuture       = "future"
	WhereRelative     = "relative"
	WhereInSegment    = "inSegment"
	WhereNotInSegment = "notInSegment"
)

// The value a where clause names the caller by.
const whereMe = "@me"

// Bounds on what one where clause may ask.
const (
	whereMaxLeaves       = 100
	whereMaxValues       = 100
	whereMaxDepth        = 8
	whereMaxSegmentDepth = 4
)

// The two constants, spelled in the filter language. Every record has an id.
const (
	whereTrue  = `id != ""`
	whereFalse = `id = ""`
)

// CompileWhere turns a where clause over collection into Base filter text and
// the parameters it binds.
//
// requestInfo is the caller's, which decides whether a hidden field may be named
// and whether a segment a clause names may be read. now is the instant relative
// dates are computed from.
//
// An empty clause (null, {} or an empty group) constrains nothing and compiles
// to an always-true filter.
func CompileWhere(app App, collection *Collection, requestInfo *RequestInfo, raw []byte, now time.Time) (search.FilterData, dbx.Params, error) {
	c := &whereCompiler{
		app:         app,
		requestInfo: requestInfo,
		params:      dbx.Params{},
		now:         now,
	}

	x, err := c.compile(collection, raw, 0)
	if err != nil {
		return "", nil, err
	}

	return search.FilterData(x.render(false)), c.params, nil
}

type whereCompiler struct {
	app         App
	requestInfo *RequestInfo
	params      dbx.Params
	now         time.Time
	leaves      int
	segments    []string // the segments being expanded, outermost first
}

// whereNode is one node of a where clause, as it arrives.
type whereNode struct {
	And       []json.RawMessage `json:"and"`
	Or        []json.RawMessage `json:"or"`
	Not       json.RawMessage   `json:"not"`
	Field     string            `json:"field"`
	Op        string            `json:"op"`
	Value     json.RawMessage   `json:"value"`
	Tz        string            `json:"tz"`
	WeekStart *int              `json:"weekStart"`
}

func (c *whereCompiler) compile(collection *Collection, raw []byte, depth int) (*wexpr, error) {
	if depth > whereMaxDepth {
		return nil, fmt.Errorf("where: nested deeper than %d", whereMaxDepth)
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return wconst(true), nil
	}

	var n whereNode
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&n); err != nil {
		return nil, fmt.Errorf("where: %w", err)
	}

	kinds := 0
	for _, set := range []bool{n.And != nil, n.Or != nil, n.Not != nil, n.Op != ""} {
		if set {
			kinds++
		}
	}
	if kinds > 1 {
		return nil, errors.New("where: a node is one of and, or, not or a leaf")
	}

	switch {
	case n.And != nil, n.Or != nil:
		items := n.And
		if n.Or != nil {
			items = n.Or
		}
		kids := make([]*wexpr, 0, len(items))
		for _, item := range items {
			kid, err := c.compile(collection, item, depth+1)
			if err != nil {
				return nil, err
			}
			kids = append(kids, kid)
		}
		if len(kids) == 0 {
			// an empty group constrains nothing, as an empty view filter does
			return wconst(true), nil
		}
		if n.Or != nil {
			return wor(kids...), nil
		}
		return wand(kids...), nil
	case n.Not != nil:
		kid, err := c.compile(collection, n.Not, depth+1)
		if err != nil {
			return nil, err
		}
		return wnot(kid), nil
	case n.Op != "":
		c.leaves++
		if c.leaves > whereMaxLeaves {
			return nil, fmt.Errorf("where: more than %d conditions", whereMaxLeaves)
		}
		return c.leaf(collection, &n)
	}

	return wconst(true), nil
}

// ---------------------------------------------------------------------------
// paths

// wherePath is what a leaf's field path reaches.
type wherePath struct {
	path     string // the path in the filter language
	field    Field  // the field the path ends on
	part     string // the part of a composite or JSON field, "" for the whole value
	multi    bool   // the value is a set somewhere along the path: compare with any-of
	nullable bool   // the value may be NULL: a part, or a value through a relation
}

func (c *whereCompiler) superuser() bool {
	return c.requestInfo != nil && c.requestInfo.HasSuperuserAuth()
}

func (c *whereCompiler) resolve(collection *Collection, path string) (*wherePath, error) {
	segs := strings.Split(path, ".")
	wp := &wherePath{path: path}
	coll := collection

	for i, seg := range segs {
		if !fieldNameRegex.MatchString(seg) {
			return nil, fmt.Errorf("where: %q is not a field path", path)
		}

		last := i == len(segs)-1
		field := coll.Fields.GetByName(seg)

		if field == nil {
			m := viaRegex.FindStringSubmatch(seg)
			if m == nil {
				return nil, fmt.Errorf("where: %q has no field %q", coll.Name, seg)
			}
			back, err := c.app.FindCachedCollectionByNameOrId(m[1])
			if err != nil {
				return nil, fmt.Errorf("where: no collection %q", m[1])
			}
			rel, ok := back.Fields.GetByName(m[2]).(*RelationField)
			if !ok || rel.CollectionId != coll.Id {
				return nil, fmt.Errorf("where: %q.%q does not relate to %q", back.Name, m[2], coll.Name)
			}
			if last {
				return nil, fmt.Errorf("where: %q names a back-relation and no field of %q", path, back.Name)
			}
			coll = back
			wp.multi = true
			wp.nullable = true
			continue
		}

		if field.GetHidden() && !c.superuser() {
			return nil, fmt.Errorf("where: %q is not filterable", seg)
		}

		if last {
			wp.field = field
			// a JSON document may be NULL, which matches no comparison
			if field.Type() == FieldTypeJSON {
				wp.nullable = true
			}
			return wp, nil
		}

		switch {
		case field.Type() == FieldTypeRelation:
			rel := field.(*RelationField)
			target, err := c.app.FindCachedCollectionByNameOrId(rel.CollectionId)
			if err != nil {
				return nil, fmt.Errorf("where: %q relates to a missing collection", seg)
			}
			if rel.IsMultiple() {
				wp.multi = true
			}
			wp.nullable = true
			coll = target
		case IsComposite(field):
			part := segs[i+1]
			if i+2 != len(segs) || !HasPart(field, part) {
				return nil, fmt.Errorf("where: %q has no part %q", seg, strings.Join(segs[i+1:], "."))
			}
			wp.field, wp.part, wp.nullable = field, part, true
			return wp, nil
		case field.Type() == FieldTypeJSON:
			wp.field, wp.part, wp.nullable = field, strings.Join(segs[i+1:], "."), true
			return wp, nil
		default:
			return nil, fmt.Errorf("where: %q is not a relation, a composite or a JSON field", seg)
		}
	}

	return nil, fmt.Errorf("where: %q is not a field path", path)
}

// The kinds a leaf compares, which decide its operators.
const (
	kindText      = "text"
	kindNumber    = "number"
	kindBool      = "bool"
	kindDate      = "date"
	kindSet       = "select" // select and relation: values named one by one
	kindJSON      = "json"
	kindFile      = "file"
	kindComposite = "composite"
	kindJSONPart  = "jsonPart"
)

// numericParts are the composite parts that hold numbers.
var numericParts = []string{"amountMicros", "addressLat", "addressLng"}

func kindOf(wp *wherePath) (string, error) {
	if wp.part != "" {
		switch {
		case wp.field.Type() == FieldTypeJSON:
			return kindJSONPart, nil
		case slices.Contains(numericParts, wp.part):
			return kindNumber, nil
		default:
			return kindText, nil
		}
	}

	switch wp.field.Type() {
	case FieldTypeText, FieldTypeEmail, FieldTypeURL, FieldTypeEditor:
		return kindText, nil
	case FieldTypeNumber, FieldTypeRating:
		return kindNumber, nil
	case FieldTypeBool:
		return kindBool, nil
	case FieldTypeDate, FieldTypeAutodate:
		return kindDate, nil
	case FieldTypeSelect, FieldTypeRelation:
		return kindSet, nil
	case FieldTypeJSON:
		return kindJSON, nil
	case FieldTypeFile:
		return kindFile, nil
	}

	if IsComposite(wp.field) {
		return kindComposite, nil
	}

	return "", fmt.Errorf("where: a %s field cannot be filtered", wp.field.Type())
}

// isMultiField reports whether the field the path ends on holds a set.
func isMultiField(f Field) bool {
	m, ok := f.(MultiValuer)
	return ok && m.IsMultiple()
}

// elemPath is how the filter language names each element of the value a path
// ends on: a multi-relation's ids are `rel.id` and a multi-select's or a multi-
// file's values are `field:each`, while a single value is the path itself.
func elemPath(wp *wherePath) string {
	switch {
	case wp.part != "" || !isMultiField(wp.field):
		return wp.path
	case wp.field.Type() == FieldTypeRelation:
		return wp.path + ".id"
	default:
		return wp.path + ":each"
	}
}

// ---------------------------------------------------------------------------
// leaves

func (c *whereCompiler) leaf(collection *Collection, n *whereNode) (*wexpr, error) {
	if n.Op == WhereInSegment || n.Op == WhereNotInSegment {
		if n.Field != "" {
			return nil, fmt.Errorf("where: %s names no field", n.Op)
		}
		x, err := c.segment(collection, n.Value)
		if err != nil {
			return nil, err
		}
		if n.Op == WhereNotInSegment {
			return wnot(x), nil
		}
		return x, nil
	}

	if n.Field == "" {
		return nil, fmt.Errorf("where: %q needs a field", n.Op)
	}

	wp, err := c.resolve(collection, n.Field)
	if err != nil {
		return nil, err
	}

	kind, err := kindOf(wp)
	if err != nil {
		return nil, err
	}

	// the negative operators are their positive twins, negated
	switch n.Op {
	case WhereIsNot:
		x, err := c.leafOp(wp, kind, WhereIs, n)
		return wnot(x), err
	case WhereNotContains:
		x, err := c.leafOp(wp, kind, WhereContains, n)
		return wnot(x), err
	case WhereNotEmpty:
		x, err := c.leafOp(wp, kind, WhereEmpty, n)
		return wnot(x), err
	}

	return c.leafOp(wp, kind, n.Op, n)
}

func (c *whereCompiler) leafOp(wp *wherePath, kind, op string, n *whereNode) (*wexpr, error) {
	refuse := func() (*wexpr, error) {
		return nil, fmt.Errorf("where: %q cannot be asked of %q", n.Op, n.Field)
	}

	if op == WhereEmpty {
		if kind == kindBool {
			return refuse()
		}
		return c.empty(wp, kind), nil
	}

	switch kind {
	case kindText:
		switch op {
		case WhereIs:
			return c.anyOf(wp, n.Value, true)
		case WhereContains:
			return c.contains(wp, n.Value)
		}
	case kindNumber:
		switch op {
		case WhereIs:
			return c.numbers(wp, n.Value)
		case WhereGt, WhereGte, WhereLt, WhereLte:
			v, err := c.number(n.Value)
			if err != nil {
				return nil, err
			}
			return c.compare(wp, op, c.bind(v)), nil
		}
	case kindBool:
		if op == WhereIs {
			return c.boolean(wp, n.Value)
		}
	case kindDate:
		return c.date(wp, op, n)
	case kindSet:
		switch op {
		case WhereIs:
			return c.anyOf(wp, n.Value, true)
		case WhereContains:
			// a set contains any of the values; a single value is one of them
			return c.anyOf(wp, n.Value, true)
		}
	case kindJSON, kindFile:
		if op == WhereContains {
			return c.contains(wp, n.Value)
		}
	case kindJSONPart:
		switch op {
		case WhereIs:
			return c.anyOf(wp, n.Value, false)
		case WhereContains:
			return c.contains(wp, n.Value)
		case WhereGt, WhereGte, WhereLt, WhereLte:
			v, err := c.scalar(n.Value)
			if err != nil {
				return nil, err
			}
			return c.compare(wp, op, c.bind(v)), nil
		}
	case kindComposite:
		if op == WhereContains {
			return c.compositeContains(wp, n.Value)
		}
	}

	return refuse()
}

// empty is the leaf a field's "no value" is, per type.
func (c *whereCompiler) empty(wp *wherePath, kind string) *wexpr {
	p := wp.path

	switch {
	case kind == kindComposite:
		return watom(p+` = null`, p+` != null`)
	case kind == kindJSON:
		return watom(
			`(`+p+` = null || `+p+` = "" || `+p+` = "null" || `+p+` = "[]" || `+p+` = "{}")`,
			`(`+p+` != null && `+p+` != "" && `+p+` != "null" && `+p+` != "[]" && `+p+` != "{}")`,
		)
	case wp.part != "":
		return watom(`(`+p+` = "" || `+p+` = null)`, `(`+p+` != "" && `+p+` != null)`)
	case isMultiField(wp.field):
		return watom(p+`:length = 0`, p+`:length > 0`)
	case kind == kindNumber:
		return watom(p+` = 0`, p+` != 0`)
	default:
		return watom(p+` = ""`, p+` != ""`)
	}
}

// anyOf is "the value is one of these", for text, a select, a relation and a
// JSON part. me admits @me.
func (c *whereCompiler) anyOf(wp *wherePath, raw json.RawMessage, me bool) (*wexpr, error) {
	vals, err := c.values(raw)
	if err != nil {
		return nil, err
	}

	many := wp.multi || isMultiField(wp.field)
	path := elemPath(wp)

	kids := make([]*wexpr, 0, len(vals))
	for _, v := range vals {
		var operand string
		if s, ok := v.(string); ok && s == whereMe && me {
			operand = "@request.auth.id"
		} else {
			s, err := c.text(v)
			if err != nil {
				return nil, err
			}
			operand = c.bind(s)
		}

		if many {
			kids = append(kids, watom(path+` ?= `+operand, path+` != `+operand))
		} else {
			kids = append(kids, watom(path+` = `+operand, path+` != `+operand))
		}
	}

	return wor(kids...), nil
}

// contains is a case-insensitive substring of the value, any of several when
// several are given.
func (c *whereCompiler) contains(wp *wherePath, raw json.RawMessage) (*wexpr, error) {
	vals, err := c.values(raw)
	if err != nil {
		return nil, err
	}

	many := wp.multi || isMultiField(wp.field)

	kids := make([]*wexpr, 0, len(vals))
	for _, v := range vals {
		s, err := c.text(v)
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, fmt.Errorf("where: %q contains nothing", wp.path)
		}
		kids = append(kids, c.like(elemPath(wp), s, many, wp.nullable))
	}

	return wor(kids...), nil
}

// like is one substring match. Its opposite counts a NULL, or a set with
// nothing in it, as not containing the text, which a plain NOT LIKE would leave
// out.
func (c *whereCompiler) like(path, s string, many, nullable bool) *wexpr {
	operand := c.bind(likeLiteral(s))

	pos := path + ` ~ ` + operand
	if many {
		pos = path + ` ?~ ` + operand
	}

	neg := path + ` !~ ` + operand
	if many || nullable {
		neg = `(` + neg + ` || ` + path + ` = null)`
	}

	return watom(pos, neg)
}

// likeLiteral escapes the pattern characters in s, so a contains matches what
// was typed and nothing it happens to spell.
func likeLiteral(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// compositeContains matches the text in any of a composite value's text parts.
// A phone number is matched by its digits, and text with no digit in it
// matches no number.
func (c *whereCompiler) compositeContains(wp *wherePath, raw json.RawMessage) (*wexpr, error) {
	vals, err := c.values(raw)
	if err != nil {
		return nil, err
	}

	tp, ok := wp.field.(TextParts)
	if !ok {
		return nil, fmt.Errorf("where: %q has no text to search", wp.path)
	}

	kids := []*wexpr{}
	for _, v := range vals {
		s, err := c.text(v)
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, fmt.Errorf("where: %q contains nothing", wp.path)
		}
		if wp.field.Type() == FieldTypePhones {
			s = strings.TrimPrefix(PhoneDigits(s), "+")
			if s == "" {
				kids = append(kids, wconst(false))
				continue
			}
		}
		for _, part := range tp.TextParts() {
			kids = append(kids, c.like(wp.path+"."+part, s, wp.multi, true))
		}
	}

	return wor(kids...), nil
}

func (c *whereCompiler) numbers(wp *wherePath, raw json.RawMessage) (*wexpr, error) {
	vals, err := c.values(raw)
	if err != nil {
		return nil, err
	}

	kids := make([]*wexpr, 0, len(vals))
	for _, v := range vals {
		f, err := toNumber(v)
		if err != nil {
			return nil, fmt.Errorf("where: %q: %w", wp.path, err)
		}
		operand := c.bind(f)
		if wp.multi {
			kids = append(kids, watom(wp.path+` ?= `+operand, wp.path+` != `+operand))
		} else {
			kids = append(kids, watom(wp.path+` = `+operand, wp.path+` != `+operand))
		}
	}

	return wor(kids...), nil
}

// whereSigns are the ordering operators and their opposites.
var whereSigns = map[string][2]string{
	WhereGt:  {">", "<="},
	WhereGte: {">=", "<"},
	WhereLt:  {"<", ">="},
	WhereLte: {"<=", ">"},
}

// compare is one ordering comparison against an operand already bound. Its
// opposite counts a NULL, or a set with nothing in it, as on the other side.
func (c *whereCompiler) compare(wp *wherePath, op, operand string) *wexpr {
	sign, opposite := whereSigns[op][0], whereSigns[op][1]

	pos := wp.path + ` ` + sign + ` ` + operand
	if wp.multi {
		pos = wp.path + ` ?` + sign + ` ` + operand
	}

	neg := wp.path + ` ` + opposite + ` ` + operand
	if wp.multi || wp.nullable {
		neg = `(` + neg + ` || ` + wp.path + ` = null)`
	}

	return watom(pos, neg)
}

func (c *whereCompiler) boolean(wp *wherePath, raw json.RawMessage) (*wexpr, error) {
	v, err := c.scalar(raw)
	if err != nil {
		return nil, err
	}

	var b bool
	switch x := v.(type) {
	case bool:
		b = x
	case string:
		b, err = strconv.ParseBool(x)
		if err != nil {
			return nil, fmt.Errorf("where: %q is true or false", wp.path)
		}
	default:
		return nil, fmt.Errorf("where: %q is true or false", wp.path)
	}

	lit := strconv.FormatBool(b)
	return watom(wp.path+` = `+lit, wp.path+` != `+lit), nil
}

// ---------------------------------------------------------------------------
// dates

// whereRelative is the value of a relative date leaf.
type whereRelative struct {
	Direction string `json:"direction"` // past, next or this
	Amount    int    `json:"amount"`
	Unit      string `json:"unit"` // second minute hour day week month quarter year
}

func (c *whereCompiler) date(wp *wherePath, op string, n *whereNode) (*wexpr, error) {
	loc := time.UTC
	if n.Tz != "" {
		l, err := time.LoadLocation(n.Tz)
		if err != nil {
			return nil, fmt.Errorf("where: unknown time zone %q", n.Tz)
		}
		loc = l
	}

	weekStart := time.Monday
	if n.WeekStart != nil {
		if *n.WeekStart < 0 || *n.WeekStart > 6 {
			return nil, errors.New("where: weekStart is 0 (Sunday) to 6 (Saturday)")
		}
		weekStart = time.Weekday(*n.WeekStart)
	}

	now := c.now.In(loc)
	p := wp.path
	set := watom(p+` != ""`, p+` = ""`)

	between := func(from, to time.Time) *wexpr {
		a, b := c.bind(formatInstant(from)), c.bind(formatInstant(to))
		return wand(set, watom(p+` >= `+a, p+` < `+a), watom(p+` < `+b, p+` >= `+b))
	}
	cmp := func(sign, opposite string, t time.Time) *wexpr {
		v := c.bind(formatInstant(t))
		return wand(set, watom(p+` `+sign+` `+v, p+` `+opposite+` `+v))
	}

	switch op {
	case WhereToday:
		start := startOf(now, "day", weekStart)
		return between(start, addUnits(start, "day", 1)), nil
	case WherePast:
		return cmp("<", ">=", c.now), nil
	case WhereFuture:
		return cmp(">", "<=", c.now), nil
	case WhereRelative:
		var r whereRelative
		if err := json.Unmarshal(n.Value, &r); err != nil {
			return nil, fmt.Errorf("where: a relative date is {direction, amount, unit}: %w", err)
		}
		if !slices.Contains([]string{"second", "minute", "hour", "day", "week", "month", "quarter", "year"}, r.Unit) {
			return nil, fmt.Errorf("where: unknown unit %q", r.Unit)
		}
		start := startOf(now, r.Unit, weekStart)
		switch r.Direction {
		case "past":
			if r.Amount < 1 {
				return nil, errors.New("where: a relative date counts at least 1 unit")
			}
			return between(addUnits(start, r.Unit, -r.Amount), start), nil
		case "next":
			if r.Amount < 1 {
				return nil, errors.New("where: a relative date counts at least 1 unit")
			}
			from := addUnits(start, r.Unit, 1)
			return between(from, addUnits(from, r.Unit, r.Amount)), nil
		case "this":
			return between(start, addUnits(start, r.Unit, 1)), nil
		}
		return nil, fmt.Errorf("where: unknown direction %q", r.Direction)
	}

	// the rest compare against a value: a day (YYYY-MM-DD, in the leaf's time
	// zone) or an instant
	v, err := c.scalar(n.Value)
	if err != nil {
		return nil, err
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil, fmt.Errorf("where: %q compares to a date", p)
	}

	if day, err := time.ParseInLocation(time.DateOnly, s, loc); err == nil {
		next := day.AddDate(0, 0, 1)
		switch op {
		case WhereIs:
			return between(day, next), nil
		case WhereGt:
			return cmp(">=", "<", next), nil
		case WhereGte:
			return cmp(">=", "<", day), nil
		case WhereLt:
			return cmp("<", ">=", day), nil
		case WhereLte:
			return cmp("<", ">=", next), nil
		}
	} else {
		dt, err := types.ParseDateTime(s)
		if err != nil || dt.IsZero() {
			return nil, fmt.Errorf("where: %q is not a date", s)
		}
		t := dt.Time()
		switch op {
		case WhereIs:
			value := c.bind(formatInstant(t))
			return wand(set, watom(p+` = `+value, p+` != `+value)), nil
		case WhereGt:
			return cmp(">", "<=", t), nil
		case WhereGte:
			return cmp(">=", "<", t), nil
		case WhereLt:
			return cmp("<", ">=", t), nil
		case WhereLte:
			return cmp("<=", ">", t), nil
		}
	}

	return nil, fmt.Errorf("where: %q cannot be asked of %q", op, p)
}

func formatInstant(t time.Time) string {
	return t.UTC().Format(types.DefaultDateLayout)
}

// startOf is the start of the unit t falls in, in t's own location.
func startOf(t time.Time, unit string, weekStart time.Weekday) time.Time {
	y, m, d := t.Date()
	loc := t.Location()
	switch unit {
	case "second":
		return t.Truncate(time.Second)
	case "minute":
		return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, loc)
	case "hour":
		return time.Date(y, m, d, t.Hour(), 0, 0, 0, loc)
	case "day":
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	case "week":
		back := (int(t.Weekday()) - int(weekStart) + 7) % 7
		return time.Date(y, m, d-back, 0, 0, 0, 0, loc)
	case "month":
		return time.Date(y, m, 1, 0, 0, 0, 0, loc)
	case "quarter":
		return time.Date(y, m-(m-1)%3, 1, 0, 0, 0, 0, loc)
	default: // year
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	}
}

// addUnits moves t by n units, keeping the wall clock where the unit is a
// calendar one.
func addUnits(t time.Time, unit string, n int) time.Time {
	switch unit {
	case "second":
		return t.Add(time.Duration(n) * time.Second)
	case "minute":
		return t.Add(time.Duration(n) * time.Minute)
	case "hour":
		return t.Add(time.Duration(n) * time.Hour)
	case "day":
		return t.AddDate(0, 0, n)
	case "week":
		return t.AddDate(0, 0, 7*n)
	case "month":
		return t.AddDate(0, n, 0)
	case "quarter":
		return t.AddDate(0, 3*n, 0)
	default: // year
		return t.AddDate(n, 0, 0)
	}
}

// ---------------------------------------------------------------------------
// segments

// segment compiles the where clause a saved segment holds, for the records of
// collection it may filter.
func (c *whereCompiler) segment(collection *Collection, raw json.RawMessage) (*wexpr, error) {
	v, err := c.scalar(raw)
	if err != nil {
		return nil, err
	}
	id, ok := v.(string)
	if !ok || id == "" {
		return nil, errors.New("where: a segment is named by its id")
	}

	if slices.Contains(c.segments, id) {
		return nil, fmt.Errorf("where: segment %q names itself", id)
	}
	if len(c.segments) >= whereMaxSegmentDepth {
		return nil, fmt.Errorf("where: segments nested deeper than %d", whereMaxSegmentDepth)
	}

	segments, err := c.app.FindCachedCollectionByNameOrId(CollectionNameSegments)
	if err != nil {
		return nil, errors.New("where: this Base keeps no segments")
	}

	record, err := c.app.FindRecordById(segments, id)
	if err != nil {
		return nil, fmt.Errorf("where: no segment %q", id)
	}

	info := c.requestInfo
	if info == nil {
		info = &RequestInfo{}
	}
	canView, err := c.app.CanAccessRecord(record, info, segments.ViewRule)
	if err != nil || !canView {
		// a segment the caller cannot read is one it cannot name
		return nil, fmt.Errorf("where: no segment %q", id)
	}

	if object := record.GetString("object"); object != collection.Name && object != collection.Id {
		return nil, fmt.Errorf("where: segment %q filters %q, not %q", id, object, collection.Name)
	}

	var where json.RawMessage
	switch w := record.Get("where").(type) {
	case types.JSONRaw:
		where = json.RawMessage(w)
	default:
		where, err = json.Marshal(w)
		if err != nil {
			return nil, fmt.Errorf("where: segment %q: %w", id, err)
		}
	}

	c.segments = append(c.segments, id)
	defer func() { c.segments = c.segments[:len(c.segments)-1] }()

	return c.compile(collection, where, 0)
}

// ---------------------------------------------------------------------------
// values

// values reads a leaf's value as a list: a single value is a list of one.
func (c *whereCompiler) values(raw json.RawMessage) ([]any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil || v == nil {
		return nil, errors.New("where: the condition needs a value")
	}

	list, ok := v.([]any)
	if !ok {
		list = []any{v}
	}
	if len(list) == 0 {
		return nil, errors.New("where: the condition needs a value")
	}
	if len(list) > whereMaxValues {
		return nil, fmt.Errorf("where: more than %d values", whereMaxValues)
	}

	return list, nil
}

// scalar reads a leaf's value as one value.
func (c *whereCompiler) scalar(raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil || v == nil {
		return nil, errors.New("where: the condition needs a value")
	}
	switch v.(type) {
	case []any, map[string]any:
		return nil, errors.New("where: the condition takes one value")
	}
	return v, nil
}

func (c *whereCompiler) number(raw json.RawMessage) (float64, error) {
	v, err := c.scalar(raw)
	if err != nil {
		return 0, err
	}
	return toNumber(v)
}

func toNumber(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", x)
		}
		return f, nil
	}
	return 0, fmt.Errorf("%v is not a number", v)
}

// text reads one value as text: a string, or a number or a bool written out.
func (c *whereCompiler) text(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(x), nil
	}
	return "", fmt.Errorf("where: %v is not a value to compare", v)
}

// bind binds v as a parameter and returns its placeholder. An empty string is
// written as the literal, which the filter language reads as the empty value.
func (c *whereCompiler) bind(v any) string {
	if s, ok := v.(string); ok && s == "" {
		return `""`
	}
	name := "w" + strconv.Itoa(len(c.params))
	c.params[name] = v
	return "{:" + name + "}"
}

// ---------------------------------------------------------------------------
// the compiled tree

// wexpr is a compiled where clause: a tree of and/or/not over leaves, each of
// which carries its own filter text and that text's opposite, so rendering a
// negation pushes it down to the leaves.
type wexpr struct {
	op       byte // 'l' leaf, 'a' and, 'o' or, 'n' not, 't' true, 'f' false
	pos, neg string
	kids     []*wexpr
}

func watom(pos, neg string) *wexpr { return &wexpr{op: 'l', pos: pos, neg: neg} }

func wconst(v bool) *wexpr {
	if v {
		return &wexpr{op: 't'}
	}
	return &wexpr{op: 'f'}
}

func wand(kids ...*wexpr) *wexpr {
	if len(kids) == 0 {
		return wconst(true)
	}
	if len(kids) == 1 {
		return kids[0]
	}
	return &wexpr{op: 'a', kids: kids}
}

func wor(kids ...*wexpr) *wexpr {
	if len(kids) == 0 {
		return wconst(false)
	}
	if len(kids) == 1 {
		return kids[0]
	}
	return &wexpr{op: 'o', kids: kids}
}

func wnot(kid *wexpr) *wexpr { return &wexpr{op: 'n', kids: []*wexpr{kid}} }

func (x *wexpr) render(negated bool) string {
	switch x.op {
	case 'l':
		if negated {
			return x.neg
		}
		return x.pos
	case 't', 'f':
		if (x.op == 't') != negated {
			return whereTrue
		}
		return whereFalse
	case 'n':
		return x.kids[0].render(!negated)
	}

	isAnd := x.op == 'a'
	if negated {
		isAnd = !isAnd
	}
	sep := " || "
	if isAnd {
		sep = " && "
	}

	parts := make([]string, len(x.kids))
	for i, kid := range x.kids {
		parts[i] = kid.render(negated)
	}

	return "(" + strings.Join(parts, sep) + ")"
}
