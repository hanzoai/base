package apis

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/dbutils"
	"github.com/hanzoai/base/tools/inflector"
	"github.com/hanzoai/base/tools/search"
	"github.com/hanzoai/dbx"
)

// whereQueryParam carries a filter written as data (see [core.CompileWhere]).
const whereQueryParam = "where"

// whereExpr compiles the request's ?where= against collection and binds it with
// resolver, or answers nil when the request sends none.
func whereExpr(e *core.RequestEvent, collection *core.Collection, info *core.RequestInfo, resolver *core.RecordFieldResolver) (dbx.Expression, error) {
	raw := e.Request.URL.Query().Get(whereQueryParam)
	if raw == "" {
		return nil, nil
	}

	filter, params, err := core.CompileWhere(e.App, collection, info, []byte(raw), time.Now())
	if err != nil {
		return nil, err
	}

	return filter.BuildExprWithLimit(resolver, search.DefaultFilterExprLimit*4, params)
}

// aggregateMaxOps and aggregateMaxGroups bound one aggregate request.
const (
	aggregateMaxOps    = 20
	aggregateMaxGroups = 200
)

// The aggregate operations.
var aggregateOps = []string{
	"count", "countEmpty", "countNotEmpty", "countUnique", "percentEmpty", "percentNotEmpty",
	"sum", "avg", "min", "max", "countTrue", "countFalse",
}

// aggregateOp is one requested operation, such as sum(amount.amountMicros).
type aggregateOp struct {
	key   string // as the caller spelled it, the key of its answer
	name  string
	field string // "" for count
}

// aggregateField is a field an aggregate reads: on the collection itself, a
// whole value or one part of a composite or JSON value.
type aggregateField struct {
	path  string
	field core.Field
	part  string
	expr  string // the SQL that reads it off the collection's own row
}

// recordsAggregate answers the figures a list shows beside its rows: a count,
// sums, averages, extremes, how many are empty — over the records the caller's
// list rule admits, narrowed by the same filter and where a list takes, and
// optionally once per value of one field (a board's columns).
//
//	GET /collections/{collection}/aggregate?ops=count,sum(amount.amountMicros)&group=stage
//
// Records are counted once however many related rows a rule or a filter joins,
// because the figures are taken over the ids the filter selects rather than over
// the joined rows.
func recordsAggregate(e *core.RequestEvent) error {
	collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue("collection"))
	if err != nil || collection == nil {
		return e.NotFoundError("Missing collection context.", err)
	}

	if err := checkCollectionRateLimit(e, collection, "list"); err != nil {
		return err
	}

	info, err := e.RequestInfo()
	if err != nil {
		return firstApiError(err, e.BadRequestError("", err))
	}

	superuser := info.HasSuperuserAuth()

	if collection.ListRule == nil && !superuser {
		return e.ForbiddenError("Only superusers can perform this action.", nil)
	}

	if err := checkForSuperuserOnlyRuleFields(info); err != nil {
		return err
	}

	q := e.Request.URL.Query()

	ops, err := parseAggregateOps(q.Get("ops"))
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	fields := map[string]*aggregateField{}
	for _, op := range ops {
		if op.field == "" {
			continue
		}
		f, err := resolveAggregateField(e.App, collection, op.field, superuser)
		if err != nil {
			return e.BadRequestError(err.Error(), err)
		}
		if err := checkAggregateOp(op, f); err != nil {
			return e.BadRequestError(err.Error(), err)
		}
		fields[op.field] = f
	}

	var group *aggregateField
	if g := q.Get("group"); g != "" {
		group, err = resolveAggregateField(e.App, collection, g, superuser)
		if err != nil {
			return e.BadRequestError(err.Error(), err)
		}
		if m, ok := group.field.(core.MultiValuer); ok && m.IsMultiple() && group.part == "" {
			return e.BadRequestError("A field holding several values cannot group.", nil)
		}
	}

	ids, err := aggregateIds(e, collection, info)
	if err != nil {
		return e.BadRequestError(err.Error(), err)
	}

	total, err := aggregateRows(e, collection, info, ids, ops, fields, nil)
	if err != nil {
		return firstApiError(err, e.BadRequestError("Failed to aggregate.", err))
	}

	out := map[string]any{"total": map[string]any{}, "groups": []any{}}
	if len(total) == 1 {
		out["total"] = total[0].values
	}

	if group != nil {
		rows, err := aggregateRows(e, collection, info, ids, ops, fields, group)
		if err != nil {
			return firstApiError(err, e.BadRequestError("Failed to aggregate.", err))
		}
		groups := make([]any, 0, len(rows))
		for _, r := range rows {
			v := map[string]any{"value": r.value}
			for k, x := range r.values {
				v[k] = x
			}
			groups = append(groups, v)
		}
		out["groups"] = groups
	}

	return e.JSON(http.StatusOK, out)
}

func parseAggregateOps(raw string) ([]aggregateOp, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "count"
	}

	var ops []aggregateOp
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		op := aggregateOp{key: item, name: item}
		if i := strings.IndexByte(item, '('); i >= 0 {
			if !strings.HasSuffix(item, ")") {
				return nil, fmt.Errorf("%q is not an operation", item)
			}
			op.name = item[:i]
			op.field = strings.TrimSpace(item[i+1 : len(item)-1])
		}

		if !slices.Contains(aggregateOps, op.name) {
			return nil, fmt.Errorf("unknown operation %q", op.name)
		}
		if (op.name == "count") != (op.field == "") {
			return nil, fmt.Errorf("%q: count takes no field and every other operation takes one", item)
		}

		if !slices.ContainsFunc(ops, func(o aggregateOp) bool { return o.key == op.key }) {
			ops = append(ops, op)
		}
	}

	if len(ops) > aggregateMaxOps {
		return nil, fmt.Errorf("more than %d operations", aggregateMaxOps)
	}

	return ops, nil
}

func resolveAggregateField(app core.App, collection *core.Collection, path string, superuser bool) (*aggregateField, error) {
	segs := strings.Split(path, ".")

	field := collection.Fields.GetByName(segs[0])
	if field == nil {
		return nil, fmt.Errorf("%q has no field %q", collection.Name, segs[0])
	}
	if field.GetHidden() && !superuser {
		return nil, fmt.Errorf("%q is not filterable", segs[0])
	}

	table := inflector.Columnify(collection.Name)
	column := table + "." + inflector.Columnify(field.GetName())

	f := &aggregateField{path: path, field: field, expr: "[[" + column + "]]"}

	if len(segs) == 1 {
		return f, nil
	}

	switch {
	case core.IsComposite(field) && len(segs) == 2 && core.HasPart(field, segs[1]):
		f.part = segs[1]
	case field.Type() == core.FieldTypeJSON:
		for _, s := range segs[1:] {
			if !regexAggregatePart(s) {
				return nil, fmt.Errorf("%q is not a field path", path)
			}
		}
		f.part = strings.Join(segs[1:], ".")
	default:
		// a figure is over the collection's own rows, never through a relation
		return nil, fmt.Errorf("%q: an aggregate reads a field of %q itself", path, collection.Name)
	}

	f.expr = dbutils.JSONExtract(app.Dialect(), column, f.part)

	return f, nil
}

func regexAggregatePart(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// numericPart reports whether a part holds numbers.
func numericPart(f *aggregateField) bool {
	return f.field.Type() == core.FieldTypeJSON ||
		slices.Contains([]string{"amountMicros", "addressLat", "addressLng"}, f.part)
}

func checkAggregateOp(op aggregateOp, f *aggregateField) error {
	t := f.field.Type()

	numeric := (f.part == "" && (t == core.FieldTypeNumber || t == core.FieldTypeRating)) ||
		(f.part != "" && numericPart(f))
	dated := f.part == "" && (t == core.FieldTypeDate || t == core.FieldTypeAutodate)

	switch op.name {
	case "sum", "avg":
		if !numeric {
			return fmt.Errorf("%s needs a number, and %q is not one", op.name, f.path)
		}
	case "min", "max":
		if !numeric && !dated {
			return fmt.Errorf("%s needs a number or a date, and %q is neither", op.name, f.path)
		}
	case "countTrue", "countFalse":
		if t != core.FieldTypeBool || f.part != "" {
			return fmt.Errorf("%s needs a bool, and %q is not one", op.name, f.path)
		}
	case "countEmpty", "countNotEmpty", "percentEmpty", "percentNotEmpty":
		if t == core.FieldTypeBool {
			return fmt.Errorf("a bool is never empty")
		}
	}

	return nil
}

// aggregateIds is the subquery of the ids the caller may list and asked for:
// the list rule, then the filter, then the where.
func aggregateIds(e *core.RequestEvent, collection *core.Collection, info *core.RequestInfo) (*dbx.Query, error) {
	table := inflector.Columnify(collection.Name)
	query := e.App.RecordQuery(collection)

	resolver := core.NewRecordFieldResolver(e.App, collection, info, true)

	if !info.HasSuperuserAuth() && collection.ListRule != nil && *collection.ListRule != "" {
		expr, err := search.FilterData(*collection.ListRule).BuildExpr(resolver)
		if err != nil {
			return nil, err
		}
		query.AndWhere(expr)
	}

	resolver.SetAllowHiddenFields(info.HasSuperuserAuth())

	if raw := e.Request.URL.Query().Get(search.FilterQueryParam); raw != "" {
		if len(raw) > search.MaxFilterLength {
			return nil, search.ErrFilterLengthLimit
		}
		expr, err := search.FilterData(raw).BuildExpr(resolver)
		if err != nil {
			return nil, err
		}
		query.AndWhere(expr)
	}

	if expr, err := whereExpr(e, collection, info, resolver); err != nil {
		return nil, err
	} else if expr != nil {
		query.AndWhere(expr)
	}

	if err := resolver.UpdateQuery(query); err != nil {
		return nil, err
	}

	return query.Select("[[" + table + ".id]]").Build(), nil
}

// aggregateRow is one row of figures: the whole set, or one group of it.
type aggregateRow struct {
	value  any
	values map[string]any
}

// aggregateRows takes the figures over ids, once, or once per value of group.
//
// The plain figures are one query. Each emptiness question is a count of its
// own over the same ids, narrowed by the field's empty predicate, which is the
// where clause's own `empty` so that "empty" means one thing in a filter and a
// figure.
func aggregateRows(
	e *core.RequestEvent,
	collection *core.Collection,
	info *core.RequestInfo,
	ids *dbx.Query,
	ops []aggregateOp,
	fields map[string]*aggregateField,
	group *aggregateField,
) ([]*aggregateRow, error) {
	d := e.App.Dialect()
	table := inflector.Columnify(collection.Name)
	inIds := dbx.NewExp("[["+table+".id]] IN ("+ids.SQL()+")", ids.Params())

	base := func() *dbx.SelectQuery {
		q := e.App.ConcurrentDB().Select().From(table).AndWhere(inIds)
		if group != nil {
			q.AndSelect(group.expr+" AS [[__group]]").GroupBy(group.expr).
				OrderBy("[[__count]] DESC", "[[__group]] ASC").
				Limit(aggregateMaxGroups)
		}
		return q.AndSelect("COUNT(*) AS [[__count]]")
	}

	// the plain figures, one column each
	main := base()
	cols := map[string]string{}
	for i, op := range ops {
		col := "__a" + strconv.Itoa(i)
		var expr string
		switch op.name {
		case "count", "countEmpty", "countNotEmpty", "percentEmpty", "percentNotEmpty":
			continue
		case "countUnique":
			expr = "COUNT(DISTINCT " + fields[op.field].expr + ")"
		case "sum", "avg", "min", "max":
			f := fields[op.field]
			x := f.expr
			if f.part != "" {
				x = d.Number(x)
			}
			if op.name == "min" || op.name == "max" {
				if t := f.field.Type(); f.part == "" && (t == core.FieldTypeDate || t == core.FieldTypeAutodate) {
					// an unset date is the empty string, which no extreme is
					x = "NULLIF(" + f.expr + ", '')"
				}
			}
			expr = strings.ToUpper(op.name) + "(" + x + ")"
		case "countTrue":
			expr = "SUM(CASE WHEN " + fields[op.field].expr + " = " + d.Bool(true) + " THEN 1 ELSE 0 END)"
		case "countFalse":
			expr = "SUM(CASE WHEN " + fields[op.field].expr + " = " + d.Bool(true) + " THEN 0 ELSE 1 END)"
		}
		cols[op.key] = col
		main.AndSelect(expr + " AS [[" + col + "]]")
	}

	// the emptiness figures, one column per field, in the same query: the
	// field's empty predicate is the where clause's own `empty`, so "empty"
	// means one thing in a filter and a figure, and every group a figure
	// reports is counted by the one query that chose the groups
	db, ok := e.App.ConcurrentDB().(*dbx.DB)
	if !ok {
		return nil, errors.New("aggregate: the figures are read outside a transaction")
	}
	emptyCols := map[string]string{}
	for _, op := range ops {
		switch op.name {
		case "countEmpty", "countNotEmpty", "percentEmpty", "percentNotEmpty":
		default:
			continue
		}
		if _, done := emptyCols[op.field]; done {
			continue
		}

		filter, params, err := core.CompileWhere(e.App, collection, info, []byte(`{"field":`+strconv.Quote(op.field)+`,"op":"empty"}`), time.Now())
		if err != nil {
			return nil, err
		}
		resolver := core.NewRecordFieldResolver(e.App, collection, info, true)
		expr, err := filter.BuildExpr(resolver, params)
		if err != nil {
			return nil, err
		}

		bound := dbx.Params{}
		col := "__e" + strconv.Itoa(len(emptyCols))
		main.AndSelect("SUM(CASE WHEN " + expr.Build(db, bound) + " THEN 1 ELSE 0 END) AS [[" + col + "]]").AndBind(bound)
		emptyCols[op.field] = col
	}

	rows := []dbx.NullStringMap{}
	if err := main.All(&rows); err != nil {
		return nil, err
	}

	out := make([]*aggregateRow, 0, len(rows))
	for _, r := range rows {
		row := &aggregateRow{values: map[string]any{}}
		if group != nil {
			row.value = groupValue(group, r["__group"])
		}
		total := parseFloat(r["__count"])
		for _, op := range ops {
			col, ok := cols[op.key]
			empty := parseFloat(r[emptyCols[op.field]])
			switch {
			case op.name == "count":
				row.values[op.key] = int64(total)
			case op.name == "countEmpty":
				row.values[op.key] = int64(empty)
			case op.name == "countNotEmpty":
				row.values[op.key] = int64(total - empty)
			case op.name == "percentEmpty":
				row.values[op.key] = percent(empty, total)
			case op.name == "percentNotEmpty":
				row.values[op.key] = percent(total-empty, total)
			case ok:
				row.values[op.key] = figureValue(op, fields[op.field], r[col])
			}
		}
		out = append(out, row)
	}

	return out, nil
}

func percent(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(part/total*100*100) / 100
}

func parseFloat(v sql.NullString) float64 {
	if !v.Valid {
		return 0
	}
	f, _ := strconv.ParseFloat(v.String, 64)
	return f
}

// figureValue reads one figure as what it is: a count, a number, or — for the
// extremes of a date — the date.
func figureValue(op aggregateOp, f *aggregateField, v sql.NullString) any {
	if !v.Valid {
		if op.name == "countUnique" || op.name == "countTrue" || op.name == "countFalse" || op.name == "sum" {
			return int64(0)
		}
		return nil
	}

	switch op.name {
	case "countUnique", "countTrue", "countFalse":
		return int64(parseFloat(v))
	case "min", "max":
		if t := f.field.Type(); f.part == "" && (t == core.FieldTypeDate || t == core.FieldTypeAutodate) {
			return v.String
		}
	}

	return parseFloat(v)
}

// groupValue reads a group's value as the field holds it.
func groupValue(f *aggregateField, v sql.NullString) any {
	if !v.Valid {
		return nil
	}

	if f.part == "" {
		switch f.field.Type() {
		case core.FieldTypeBool:
			b, _ := strconv.ParseBool(v.String)
			return b || v.String == "1"
		case core.FieldTypeNumber, core.FieldTypeRating:
			return parseFloat(v)
		}
	} else if numericPart(f) {
		if n, err := strconv.ParseFloat(v.String, 64); err == nil {
			return n
		}
	}

	return v.String
}
