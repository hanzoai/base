package graph

import (
	"fmt"
	"strings"

	"github.com/hanzoai/base/core"
)

// op is one operator of a filter input, with the GraphQL type it takes.
type op struct{ name, typ string }

// The filter inputs every graph shares, by name, and what each operator does.
// A field's filter is one of these, its target's RelationFilter or its
// composite's Filter; each operator is one leaf of a where clause
// (core.CompileWhere), so a filter here means what the same where means over
// REST.
var shared = []struct {
	name string
	desc string
	ops  []op
}{
	{"BaseIDFilter", "A record id is one of `is` and none of `isNot`.", []op{
		{"is", "[ID!]"}, {"isNot", "[ID!]"},
	}},
	{"BaseTextFilter", "Text is one of `is` (exactly) and none of `isNot`; contains any of `contains` (ignoring case) and none of `notContains`; `empty: true` has no text, `empty: false` has some. `@me` in `is` is the caller's id.", []op{
		{"is", "[String!]"}, {"isNot", "[String!]"}, {"contains", "[String!]"}, {"notContains", "[String!]"}, {"empty", "Boolean"},
	}},
	{"BaseNumberFilter", "A number is one of `is`, none of `isNot`, and above or below the bounds given. Base stores no missing number, so `empty: true` is zero.", []op{
		{"is", "[Float!]"}, {"isNot", "[Float!]"}, {"gt", "Float"}, {"gte", "Float"}, {"lt", "Float"}, {"lte", "Float"}, {"empty", "Boolean"},
	}},
	{"BaseBooleanFilter", "A truth value is `is`.", []op{
		{"is", "Boolean"},
	}},
	{"BaseDateFilter", "A date or an instant. `is` and the bounds take a day (`2026-10-19`, in `tz`) or an instant (`2026-10-19 15:30:00.000Z`); `today`, `past` and `future` are relative to now (false negates); `relative` is a span counted from the start of the current unit. `tz` names the IANA time zone days are counted in (UTC when absent) and `weekStart` the first day of a week (0 Sunday to 6 Saturday, Monday when absent).", []op{
		{"is", "String"}, {"isNot", "String"}, {"gt", "String"}, {"gte", "String"}, {"lt", "String"}, {"lte", "String"},
		{"today", "Boolean"}, {"past", "Boolean"}, {"future", "Boolean"}, {"relative", "BaseRelativeDate"},
		{"empty", "Boolean"}, {"tz", "String"}, {"weekStart", "Int"},
	}},
	{"BaseSelectFilter", "A choice is one of `is` and none of `isNot`; a field holding several choices holds any of `is` and none of `isNot`.", []op{
		{"is", "[String!]"}, {"isNot", "[String!]"}, {"empty", "Boolean"},
	}},
	{"BaseContainsFilter", "A value's text contains any of `contains` (ignoring case) and none of `notContains`.", []op{
		{"contains", "[String!]"}, {"notContains", "[String!]"}, {"empty", "Boolean"},
	}},
	{"BaseRelationFilter", "A relation points at one of `is` and none of `isNot`; `@me` is the caller's id.", []op{
		{"is", "[ID!]"}, {"isNot", "[ID!]"}, {"empty", "Boolean"},
	}},
}

// sharedOps is each shared filter's operators, by its name.
var sharedOps = func() map[string][]op {
	out := map[string][]op{}
	for _, s := range shared {
		out[s.name] = s.ops
	}
	return out
}()

// filterOf is the filter input a field takes.
func (f *field) filterOf() string {
	switch {
	case f.id:
		return "BaseIDFilter"
	case f.kind == kText, f.kind == kRich:
		return "BaseTextFilter"
	case f.kind == kNumber, f.kind == kRating:
		return "BaseNumberFilter"
	case f.kind == kBool:
		return "BaseBooleanFilter"
	case f.kind == kDate:
		return "BaseDateFilter"
	case f.kind == kSelect:
		return "BaseSelectFilter"
	case f.kind == kRelation && f.target != nil:
		return f.target.typ + "RelationFilter"
	case f.kind == kRelation:
		return "BaseRelationFilter"
	case f.kind == kComposite:
		return prefix + f.comp.typ + "Filter"
	}
	return "BaseContainsFilter" // file, json
}

// outOf is the GraphQL type a field answers.
func (f *field) outOf() string {
	list := func(t string) string {
		if f.multi {
			return "[" + t + "!]"
		}
		return t
	}
	switch f.kind {
	case kText, kRich, kDate:
		if f.id {
			return "ID!"
		}
		return "String"
	case kNumber:
		return "Float"
	case kRating:
		return "Int"
	case kBool:
		return "Boolean"
	case kSelect, kFile:
		return list("String")
	case kRelation:
		if f.target == nil {
			return list("ID")
		}
		return list(f.target.typ)
	case kComposite:
		return prefix + f.comp.typ
	}
	return "BaseJSON"
}

// inOf is the GraphQL type a field takes in an Input.
func (f *field) inOf() string {
	switch f.kind {
	case kText, kRich, kDate:
		if f.id {
			return "ID"
		}
		return "String"
	case kNumber:
		return "Float"
	case kRating:
		return "Int"
	case kBool:
		return "Boolean"
	case kSelect:
		if f.multi {
			return "[String!]"
		}
		return "String"
	case kRelation:
		if f.multi {
			return "[ID!]"
		}
		return "ID"
	case kComposite:
		return prefix + f.comp.typ + "Input"
	}
	return "BaseJSON"
}

// orderOf is the type an orderBy takes for a field.
func (f *field) orderOf() string {
	switch f.kind {
	case kComposite:
		return prefix + f.comp.typ + "OrderBy"
	case kRelation:
		if f.target != nil {
			return f.target.typ + "OrderBy"
		}
	}
	return "BaseOrder"
}

// connectionArgs are the arguments every connection takes.
func connectionArgs(c *coll) string {
	return fmt.Sprintf("filter: %sFilter, orderBy: [%sOrderBy!], first: Int, after: String, last: Int, before: String", c.typ, c.typ)
}

// render writes the schema of m.
func (m *model) render() string {
	w := &writer{}

	if m.writable {
		w.line("schema { query: BaseQuery mutation: BaseMutation }")
	} else {
		w.line("schema { query: BaseQuery }")
	}
	w.line("")

	w.desc("", "Any JSON value: a json field, a geo point, a minimum or a maximum.")
	w.line("scalar BaseJSON")
	w.line("")
	w.desc("", "A direction to sort in. Base places no nulls: an empty text sorts first ascending, and a missing part of a composite sorts where the database engine puts NULL.")
	w.line("enum BaseOrder { Asc Desc }")
	w.line("")

	w.block("type BasePageInfo", "Where a page sits in its connection.", []string{
		"hasNextPage: Boolean!", "hasPreviousPage: Boolean!", "startCursor: String", "endCursor: String",
	})
	w.desc("", "Figures over every record a connection's filter selects, whatever page is read. `field` names a field, or a part of a composite or JSON field (`amount.amountMicros`), of the collection itself.")
	w.line("type BaseAggregate {")
	w.line("  count: Int!")
	for _, a := range []string{"countEmpty: Int!", "countNotEmpty: Int!", "countUnique: Int!", "percentEmpty: Float!", "percentNotEmpty: Float!",
		"sum: Float", "avg: Float", "min: BaseJSON", "max: BaseJSON", "countTrue: Int!", "countFalse: Int!"} {
		name, typ, _ := strings.Cut(a, ": ")
		w.line("  " + name + "(field: String!): " + typ)
	}
	w.line("}")
	w.line("")
	w.block("type BaseGroup", "The records that hold one value of the field grouped by.", []string{
		"value: BaseJSON", "aggregate: BaseAggregate!",
	})
	w.block("type BaseCollection", "A collection of this Base, as its schema describes it.", []string{
		"id: ID!", "name: String!", "type: String!", "label: String", "plural: String", "icon: String", "description: String",
		`"""The collection's fields, as Base's own schema writes them."""`, "fields: BaseJSON!",
	})
	w.block("input BaseRelativeDate", "A span counted from the start of the current unit: `{direction: past, amount: 7, unit: day}` is the seven days before today; `this` is the current unit itself.", []string{
		`"""past, next or this"""`, "direction: String!", "amount: Int", `"""second, minute, hour, day, week, month, quarter or year"""`, "unit: String!",
	})
	for _, s := range shared {
		ops := make([]string, len(s.ops))
		for i, o := range s.ops {
			ops[i] = o.name + ": " + o.typ
		}
		w.block("input "+s.name, s.desc, ops)
	}

	if m.history {
		w.block("type BaseHistoryEntry", "One write to a record: who made it, when, and what changed from what to what.", []string{
			`"""create, update or delete"""`, "action: String!", "id: ID!", "actor: BaseActor", `"""Each changed field's {from, to}."""`, "changes: BaseJSON!", "created: String!",
		})
		w.block("type BaseHistoryEdge", "", []string{"node: BaseHistoryEntry!", "cursor: String!"})
		w.block("type BaseHistoryConnection", "A record's history, newest first.", []string{
			"edges: [BaseHistoryEdge!]!", "pageInfo: BasePageInfo!", "totalCount: Int!",
		})
	}

	m.renderComposites(w)

	for _, c := range m.colls {
		m.renderCollection(w, c)
	}

	// roots
	w.line("type BaseQuery {")
	w.desc("  ", "This Base's collections, as their schemas describe them.")
	w.line("  collections: [BaseCollection!]!")
	for _, c := range m.colls {
		w.desc("  ", "One "+label(c)+" by id, or null when there is none the caller may see.")
		w.line(fmt.Sprintf("  %s(id: ID!): %s", c.one, c.typ))
		w.desc("  ", "The "+plural(c)+" the caller may list, as a connection. A cursor is a position in the order asked for.")
		w.line(fmt.Sprintf("  %s(%s): %sConnection!", c.list, connectionArgs(c), c.typ))
		w.desc("  ", "The "+plural(c)+" a filter selects, grouped by one field of the collection, with figures for each group.")
		w.line(fmt.Sprintf("  %sGroupBy(field: String!, filter: %sFilter): [BaseGroup!]!", c.list, c.typ))
	}
	w.line("}")
	w.line("")

	if m.writable {
		w.line("type BaseMutation {")
		for _, c := range m.colls {
			if !c.write {
				continue
			}
			w.line(fmt.Sprintf("  create%s(data: %sInput!): %s", c.single, c.typ, c.typ))
			w.line(fmt.Sprintf("  update%s(id: ID!, data: %sInput!): %s", c.single, c.typ, c.typ))
			w.desc("  ", "Updates the "+label(c)+" whose `on` (a field, or a part of a composite, `id` when absent) holds the value data gives it, or creates one when none does.")
			w.line(fmt.Sprintf("  upsert%s(data: %sInput!, on: String): %s", c.single, c.typ, c.typ))
			w.desc("  ", "Deletes the "+label(c)+" and answers its id.")
			w.line(fmt.Sprintf("  delete%s(id: ID!): ID!", c.single))
		}
		w.line("}")
		w.line("")
	}

	return w.String()
}

func (m *model) renderComposites(w *writer) {
	for _, ft := range compositeOrder {
		c := composites[ft]
		if !m.used[ft] {
			continue
		}
		var out, in, filter, order []string
		if c.text {
			filter = append(filter, "contains: [String!]", "notContains: [String!]")
		}
		filter = append(filter, "empty: Boolean")
		for _, p := range c.parts {
			switch {
			case p.list && p.elem != nil:
				out = append(out, p.name+": ["+prefix+p.elem.typ+"!]")
				in = append(in, p.name+": ["+prefix+p.elem.typ+"Input!]")
				filter = append(filter, p.name+": BaseContainsFilter")
			case p.list:
				out = append(out, p.name+": [String!]")
				in = append(in, p.name+": [String!]")
				filter = append(filter, p.name+": BaseContainsFilter")
			case p.kind == kNumber:
				out = append(out, p.name+": Float")
				in = append(in, p.name+": Float")
				filter = append(filter, p.name+": BaseNumberFilter")
				order = append(order, p.name+": BaseOrder")
			default:
				out = append(out, p.name+": String")
				in = append(in, p.name+": String")
				filter = append(filter, p.name+": BaseTextFilter")
				order = append(order, p.name+": BaseOrder")
			}
			if p.elem != nil {
				fields := make([]string, len(p.elem.fields))
				for i, f := range p.elem.fields {
					fields[i] = f + ": String"
				}
				w.block("type "+prefix+p.elem.typ, "", fields)
				if c.write {
					w.block("input "+prefix+p.elem.typ+"Input", "", fields)
				}
			}
		}
		w.block("type "+prefix+c.typ, "", out)
		if c.write {
			w.block("input "+prefix+c.typ+"Input", "", in)
		}
		w.block("input "+prefix+c.typ+"Filter", "`contains` reads every part a person searches; each part filters on its own.", filter)
		w.block("input "+prefix+c.typ+"OrderBy", "One part, the one to sort by.", order)
	}
}

// compositeOrder is the order composite types are written in.
var compositeOrder = []string{
	core.FieldTypeActor, core.FieldTypeAddress, core.FieldTypeCurrency, core.FieldTypeEmails,
	core.FieldTypeFullName, core.FieldTypeLinks, core.FieldTypePhones,
}

func (m *model) renderCollection(w *writer, c *coll) {
	var out []string
	for _, f := range c.fields {
		if d := describe(f.f); d != "" {
			out = append(out, `"""`+escape(d)+`"""`)
		}
		out = append(out, f.name+": "+f.outOf())
	}
	for _, b := range c.backs {
		out = append(out, fmt.Sprintf(`"""The %s whose %s is this %s."""`, plural(b.from), b.field.name, label(c)))
		out = append(out, fmt.Sprintf("%s(%s): %sConnection!", b.name, connectionArgs(b.from), b.from.typ))
	}
	if c.history {
		out = append(out, `"""Every write to this `+label(c)+`, newest first."""`, "history(first: Int, after: String): BaseHistoryConnection!")
	}
	w.block("type "+c.typ, c.c.Description, out)
	w.block("type "+c.typ+"Edge", "", []string{"node: " + c.typ + "!", "cursor: String!"})
	w.block("type "+c.typ+"Connection", "", []string{
		"edges: [" + c.typ + "Edge!]!", "pageInfo: BasePageInfo!",
		`"""How many records the filter selects, counted only when asked for."""`, "totalCount: Int!",
		"aggregate: BaseAggregate!",
	})

	filter := []string{
		"and: [" + c.typ + "Filter!]", "or: [" + c.typ + "Filter!]", "not: " + c.typ + "Filter",
	}
	if m.segments {
		filter = append(filter, `"""The records the saved segment with this id selects."""`, "inSegment: ID", "notInSegment: ID")
	}
	var order, in []string
	for _, f := range c.fields {
		if f.filterable() {
			filter = append(filter, f.name+": "+f.filterOf())
		}
		if f.sortable() {
			order = append(order, f.name+": "+f.orderOf())
		}
		if c.write && f.writable() {
			in = append(in, f.name+": "+f.inOf())
		}
	}
	for _, b := range c.backs {
		filter = append(filter, fmt.Sprintf(`"""Some %s whose %s is this %s matches."""`, label(b.from), b.field.name, label(c)))
		filter = append(filter, b.name+": "+b.from.typ+"Filter")
	}
	w.block("input "+c.typ+"Filter", "Every key given holds (and), and an empty filter selects everything.", filter)
	w.block("input "+c.typ+"OrderBy", "One field, the one to sort by; a list of these sorts by each in turn.", order)
	if c.write {
		w.block("input "+c.typ+"Input", "", in)
	}
	if c.target {
		w.block("input "+c.typ+"RelationFilter", "A relation to a "+label(c)+": one of `is`, none of `isNot`, or one that `where` selects.", []string{
			"is: [ID!]", "isNot: [ID!]", "empty: Boolean", "where: " + c.typ + "Filter",
		})
	}
}

// describe is what a person reads about a field.
func describe(f core.Field) string {
	h, ok := f.(core.FieldMetaHolder)
	if !ok {
		return ""
	}
	meta := h.Meta()
	switch {
	case meta.Label != "" && meta.Description != "":
		return meta.Label + ": " + meta.Description
	case meta.Description != "":
		return meta.Description
	}
	return meta.Label
}

func label(c *coll) string {
	if c.c.Label != "" {
		return strings.ToLower(c.c.Label)
	}
	return c.c.Name
}

func plural(c *coll) string {
	if c.c.Plural != "" {
		return strings.ToLower(c.c.Plural)
	}
	return c.c.Name
}

// writer writes SDL.
type writer struct{ strings.Builder }

func (w *writer) line(s string) {
	w.WriteString(s)
	w.WriteByte('\n')
}

func (w *writer) desc(indent, s string) {
	if s != "" {
		w.line(indent + `"""` + escape(s) + `"""`)
	}
}

// block writes one type with its fields, or nothing when it has none: a type
// with no fields is not GraphQL.
func (w *writer) block(head, desc string, fields []string) {
	n := 0
	for _, f := range fields {
		if !strings.HasPrefix(f, `"""`) {
			n++
		}
	}
	if n == 0 {
		return
	}
	w.desc("", desc)
	w.line(head + " {")
	for _, f := range fields {
		w.line("  " + f)
	}
	w.line("}")
	w.line("")
}

func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"""`, `\"""`)
}
