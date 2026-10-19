package graph

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/zap-proto/zip"
)

// where turns a filter argument into a where clause over c (core.CompileWhere's
// language), every value kept as the data it arrived as, so it is compiled,
// bound and checked against the caller's rules by the same code a REST `where`
// is. at is the path the filter is reached by: "" at the collection, or a
// relation's path ending in a dot.
func (m *model) where(c *coll, v any, at string) (map[string]any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, bad("%sFilter takes an object", c.typ)
	}

	var all []any
	for _, k := range keys(obj) {
		val := obj[k]
		if val == nil {
			continue
		}
		switch k {
		case "and", "or":
			items, ok := val.([]any)
			if !ok {
				items = []any{val}
			}
			kids := make([]any, 0, len(items))
			for _, item := range items {
				kid, err := m.where(c, item, at)
				if err != nil {
					return nil, err
				}
				kids = append(kids, kid)
			}
			all = append(all, map[string]any{k: kids})
		case "not":
			kid, err := m.where(c, val, at)
			if err != nil {
				return nil, err
			}
			all = append(all, map[string]any{"not": kid})
		case "inSegment", "notInSegment":
			if !m.segments {
				return nil, bad("%sFilter has no field %q", c.typ, k)
			}
			if at != "" {
				return nil, bad("%s selects %s records, not records reached through %s", k, c.c.Name, strings.TrimSuffix(at, "."))
			}
			all = append(all, map[string]any{"op": k, "value": val})
		default:
			if f := c.byName[k]; f != nil && f.filterable() {
				leaves, err := m.leaves(f.filterOf(), f, nil, val, at+k)
				if err != nil {
					return nil, err
				}
				all = append(all, leaves...)
				continue
			}
			if b := c.byBack[k]; b != nil {
				kid, err := m.where(b.from, val, at+k+".")
				if err != nil {
					return nil, err
				}
				all = append(all, kid)
				continue
			}
			return nil, bad("%sFilter has no field %q", c.typ, k)
		}
	}

	switch len(all) {
	case 0:
		return map[string]any{}, nil
	case 1:
		return all[0].(map[string]any), nil
	}
	return map[string]any{"and": all}, nil
}

// leaves turns one field's operators into where leaves on path. f is the field
// when the filter is a field's own, p the part when it is a composite part's.
func (m *model) leaves(typ string, f *field, p *part, v any, path string) ([]any, error) {
	ops, ok := v.(map[string]any)
	if !ok {
		return nil, bad("%s on %q takes an object of operators", typ, path)
	}

	allowed := m.opsOf(typ, f)

	// a date's time zone and week start belong to every leaf it makes
	mods := map[string]any{}
	for _, k := range []string{"tz", "weekStart"} {
		if x, ok := ops[k]; ok && x != nil && typ == "BaseDateFilter" {
			mods[k] = x
		}
	}

	leaf := func(op string, value any) map[string]any {
		l := map[string]any{"field": path, "op": op}
		if value != nil {
			l["value"] = value
		}
		for k, x := range mods {
			l[k] = x
		}
		return l
	}

	var out []any
	for _, k := range keys(ops) {
		val := ops[k]
		if val == nil {
			continue
		}
		if !slices.Contains(allowed, k) {
			return nil, bad("%s on %q has no operator %q", typ, path, k)
		}
		switch k {
		case "tz", "weekStart":
			// read above, with every leaf
		case "empty":
			b, ok := val.(bool)
			if !ok {
				return nil, bad("empty on %q is true or false", path)
			}
			if b {
				out = append(out, leaf("empty", nil))
			} else {
				out = append(out, leaf("notEmpty", nil))
			}
		case "today", "past", "future":
			b, ok := val.(bool)
			if !ok {
				return nil, bad("%s on %q is true or false", k, path)
			}
			if b {
				out = append(out, leaf(k, nil))
			} else {
				out = append(out, map[string]any{"not": leaf(k, nil)})
			}
		case "where":
			kid, err := m.where(f.target, val, path+".")
			if err != nil {
				return nil, err
			}
			out = append(out, kid)
		default:
			if f != nil && f.kind == kComposite && p == nil {
				if part := f.comp.part(k); part != nil {
					sub, err := m.leaves(partFilter(part), nil, part, val, path+"."+k)
					if err != nil {
						return nil, err
					}
					out = append(out, sub...)
					continue
				}
			}
			out = append(out, leaf(k, val))
		}
	}
	return out, nil
}

// opsOf is every key a filter input takes.
func (m *model) opsOf(typ string, f *field) []string {
	if ops, ok := sharedOps[typ]; ok {
		out := make([]string, len(ops))
		for i, o := range ops {
			out[i] = o.name
		}
		return out
	}
	if f != nil && f.kind == kRelation && f.target != nil {
		return []string{"is", "isNot", "empty", "where"}
	}
	if f != nil && f.kind == kComposite {
		var out []string
		if f.comp.text {
			out = append(out, "contains", "notContains")
		}
		out = append(out, "empty")
		for _, p := range f.comp.parts {
			out = append(out, p.name)
		}
		return out
	}
	return nil
}

// partFilter is the filter a composite part takes.
func partFilter(p *part) string {
	switch {
	case p.list:
		return "BaseContainsFilter"
	case p.kind == kNumber:
		return "BaseNumberFilter"
	}
	return "BaseTextFilter"
}

// sortOf turns an orderBy argument into Base's sort, each term a field path
// with its direction, and the record id last so the order is total.
func (m *model) sortOf(c *coll, v any) (string, error) {
	if v == nil {
		return "", nil
	}
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	var terms []string
	byID := false
	for _, item := range items {
		path, dir, err := m.sortTerm(c, item, "", 0)
		if err != nil {
			return "", err
		}
		if path == "id" {
			byID = true
		}
		terms = append(terms, dir+path)
	}
	if len(terms) > 0 && !byID {
		terms = append(terms, "id")
	}
	return strings.Join(terms, ","), nil
}

// sortTerm reads one orderBy entry: one field, and one of its parts or a
// related record's field below it.
func (m *model) sortTerm(c *coll, v any, at string, hops int) (string, string, error) {
	obj, ok := v.(map[string]any)
	if !ok || len(obj) != 1 {
		return "", "", bad("each %sOrderBy names one field, so the order is the list's", c.typ)
	}
	for k, val := range obj {
		f := c.byName[k]
		if f == nil || !f.sortable() {
			return "", "", bad("%sOrderBy has no field %q", c.typ, k)
		}
		switch f.kind {
		case kComposite:
			sub, ok := val.(map[string]any)
			if !ok || len(sub) != 1 {
				return "", "", bad("%s orders by one of its parts", k)
			}
			for pk, pv := range sub {
				p := f.comp.part(pk)
				if p == nil || p.list {
					return "", "", bad("%s has no part %q to order by", k, pk)
				}
				dir, err := direction(pv)
				return at + k + "." + pk, dir, err
			}
		case kRelation:
			if s, ok := val.(string); ok {
				dir, err := direction(s)
				return at + k, dir, err
			}
			if f.target == nil {
				return "", "", bad("%s orders by its id: Asc or Desc", k)
			}
			if hops >= 1 {
				return "", "", bad("an order reaches one relation deep")
			}
			return m.sortTerm(f.target, val, at+k+".", hops+1)
		default:
			dir, err := direction(val)
			return at + k, dir, err
		}
	}
	return "", "", bad("each %sOrderBy names one field", c.typ)
}

func direction(v any) (string, error) {
	switch v {
	case "Asc":
		return "", nil
	case "Desc":
		return "-", nil
	}
	return "", bad("an order is Asc or Desc, not %v", v)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// bad is a refusal the caller reads.
func bad(format string, args ...any) error {
	return zip.ErrBadRequest(fmt.Sprintf(format, args...))
}
