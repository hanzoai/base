package graph

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/inflector"
)

// model is one Base's collections as the graph types them: the names every
// collection, field and root answers under, decided once per schema.
type model struct {
	colls    []*coll          // in name order
	byID     map[string]*coll // collection id → coll
	roots    map[string]root  // query and mutation root fields
	segments bool             // the Base has a segments collection, so filters take inSegment
	history  bool             // some collection keeps its history
	writable bool             // some collection takes writes
	used     map[string]bool  // the shared types the collections reach
	skipped  []string         // collections or fields left out, and why
}

// root is one root field: the collection it answers for and what it does.
type root struct {
	c  *coll
	op string // one, list, groupBy, create, update, upsert, delete
}

// coll is one collection in the graph.
type coll struct {
	c      *core.Collection
	typ    string // BasePerson
	single string // Person, as a mutation spells it
	list   string // people
	one    string // person
	fields []*field
	byName map[string]*field
	backs  []*back
	byBack map[string]*back
	write  bool // a base collection: records are written
	// history: the collection keeps its history, read as a field of each record
	history bool
	// target: some relation in the graph points here, so the RelationFilter is used
	target bool
}

// back is a relation from another collection onto this one, read from the
// record it points at: `opportunities_via_company` on a company.
type back struct {
	name  string
	from  *coll
	field *field
}

// kind is what a field's value is, which decides how it is typed, filtered,
// sorted and written.
type kind int

const (
	kText kind = iota // text, email, url
	kRich             // editor: HTML a person writes
	kNumber
	kRating
	kBool
	kDate
	kSelect
	kRelation
	kFile
	kJSON
	kComposite
	kOther // a type the graph does not know: answered as JSON, never filtered or sorted
)

// field is one field of a collection in the graph.
type field struct {
	f      core.Field
	name   string
	kind   kind
	multi  bool
	id     bool       // the primary key
	target *coll      // a relation's target, nil when it is not in the graph
	comp   *composite // a composite's shape
}

// The types every graph defines, which no collection's may take.
var reserved = []string{
	"BaseQuery", "BaseMutation", "BaseJSON", "BaseOrder", "BasePageInfo",
	"BaseAggregate", "BaseGroup", "BaseCollection",
	"BaseHistoryEntry", "BaseHistoryEdge", "BaseHistoryConnection",
	"BaseIDFilter", "BaseTextFilter", "BaseNumberFilter", "BaseBooleanFilter",
	"BaseDateFilter", "BaseRelativeDate", "BaseSelectFilter", "BaseContainsFilter",
	"BaseRelationFilter",
}

func init() {
	for _, c := range composites {
		reserved = append(reserved, prefix+c.typ, prefix+c.typ+"Filter", prefix+c.typ+"OrderBy", prefix+c.typ+"Input")
		for _, p := range c.parts {
			if p.elem != nil {
				reserved = append(reserved, prefix+p.elem.typ, prefix+p.elem.typ+"Input")
			}
		}
	}
}

// nameRegex is a GraphQL name; one starting with two underscores is the
// language's own.
var nameRegex = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

func validName(s string) bool {
	return nameRegex.MatchString(s) && !strings.HasPrefix(s, "__")
}

// filterWords are the keys a filter keeps for itself; a field so named is
// answered and written but not filtered.
var filterWords = []string{"and", "or", "not", "inSegment", "notInSegment"}

// build types the collections a graph serves: every base and view collection
// that is not one Base keeps for itself. An auth collection is identity, which
// IAM owns, so it is not a type here; a relation to one reads as the id it holds.
func build(collections []*core.Collection) *model {
	m := &model{byID: map[string]*coll{}, roots: map[string]root{}, used: map[string]bool{}}

	sorted := slices.Clone(collections)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	taken := map[string]bool{}
	for _, n := range reserved {
		taken[n] = true
	}
	roots := map[string]bool{"collections": true}

	for _, c := range sorted {
		if c.System || strings.HasPrefix(c.Name, "_") || !(c.IsBase() || c.IsView()) {
			continue
		}
		if c.Name == core.CollectionNameSegments {
			m.segments = true
		}

		single := inflector.Camelize(inflector.Singularize(c.Name))
		list := lcFirst(inflector.Camelize(c.Name))
		one := lcFirst(single)
		if one == list {
			one += "Record"
		}
		typ := prefix + single

		types := []string{typ, typ + "Edge", typ + "Connection", typ + "Filter", typ + "OrderBy", typ + "Input", typ + "RelationFilter"}
		names := []string{list, one, list + "GroupBy"}
		if c.IsBase() {
			names = append(names, "create"+single, "update"+single, "upsert"+single, "delete"+single)
		}

		ok := validName(single) && validName(list)
		for _, n := range types {
			ok = ok && !taken[n]
		}
		for _, n := range names {
			ok = ok && !roots[n]
		}
		if !ok {
			m.skipped = append(m.skipped, c.Name+": its names are another collection's or not GraphQL names")
			continue
		}
		for _, n := range types {
			taken[n] = true
		}
		for _, n := range names {
			roots[n] = true
		}

		x := &coll{
			c: c, typ: typ, single: single, list: list, one: one,
			byName: map[string]*field{}, byBack: map[string]*back{},
			write:   c.IsBase(),
			history: c.IsBase() && c.History,
		}
		m.colls = append(m.colls, x)
		m.byID[c.Id] = x

		m.roots[list] = root{x, "list"}
		m.roots[one] = root{x, "one"}
		m.roots[list+"GroupBy"] = root{x, "groupBy"}
		if x.write {
			m.writable = true
			m.roots["create"+single] = root{x, "create"}
			m.roots["update"+single] = root{x, "update"}
			m.roots["upsert"+single] = root{x, "upsert"}
			m.roots["delete"+single] = root{x, "delete"}
		}
	}

	for _, x := range m.colls {
		for _, f := range x.c.Fields {
			name := f.GetName()
			if f.GetHidden() {
				continue
			}
			if !validName(name) {
				m.skipped = append(m.skipped, x.c.Name+"."+name+": not a GraphQL name")
				continue
			}

			fd := &field{f: f, name: name, kind: kindOf(f)}
			if mv, ok := f.(core.MultiValuer); ok {
				fd.multi = mv.IsMultiple()
			}
			if t, ok := f.(*core.TextField); ok && t.PrimaryKey {
				fd.id = true
			}
			if fd.kind == kComposite {
				fd.comp = composites[f.Type()]
			}
			if rel, ok := f.(*core.RelationField); ok {
				fd.target = m.byID[rel.CollectionId]
				if fd.target != nil {
					fd.target.target = true
				}
			}
			x.fields = append(x.fields, fd)
			x.byName[name] = fd
		}
		if x.history && x.byName["history"] != nil {
			// the collection's own field keeps the name
			x.history = false
			m.skipped = append(m.skipped, x.c.Name+".history: a field of the collection is named so")
		}
	}

	m.history = false
	for _, x := range m.colls {
		m.history = m.history || x.history
	}

	// a relation is read back from the record it points at, under the name
	// Base's own expand gives it
	for _, x := range m.colls {
		for _, f := range x.fields {
			if f.kind != kRelation || f.target == nil {
				continue
			}
			name := x.c.Name + "_via_" + f.name
			t := f.target
			if !validName(name) || t.byName[name] != nil || t.byBack[name] != nil {
				m.skipped = append(m.skipped, t.c.Name+"."+name+": taken")
				continue
			}
			b := &back{name: name, from: x, field: f}
			t.backs = append(t.backs, b)
			t.byBack[name] = b
		}
	}

	return m
}

func kindOf(f core.Field) kind {
	switch f.Type() {
	case core.FieldTypeText, core.FieldTypeEmail, core.FieldTypeURL:
		return kText
	case core.FieldTypeEditor:
		return kRich
	case core.FieldTypeNumber:
		return kNumber
	case core.FieldTypeRating:
		return kRating
	case core.FieldTypeBool:
		return kBool
	case core.FieldTypeDate, core.FieldTypeAutodate:
		return kDate
	case core.FieldTypeSelect:
		return kSelect
	case core.FieldTypeRelation:
		return kRelation
	case core.FieldTypeFile:
		return kFile
	case core.FieldTypeJSON:
		return kJSON
	}
	if _, ok := composites[f.Type()]; ok {
		return kComposite
	}
	return kOther
}

// writable reports whether a field takes a value in an Input: never a value
// Base sets itself (a date it stamps, a writer it records) or a file, which is
// uploaded as a form rather than written as JSON.
func (f *field) writable() bool {
	switch {
	case f.f.Type() == core.FieldTypeAutodate, f.kind == kFile:
		return false
	case f.kind == kComposite:
		return f.comp.write
	}
	return true
}

// filterable reports whether a filter can name the field.
func (f *field) filterable() bool {
	return f.kind != kOther && !slices.Contains(filterWords, f.name) && f.f.Type() != core.FieldTypeGeoPoint
}

// sortable reports whether an orderBy can name the field.
func (f *field) sortable() bool {
	switch f.kind {
	case kText, kNumber, kRating, kBool, kDate:
		return true
	case kSelect:
		return !f.multi
	case kRelation:
		return !f.multi
	case kComposite:
		return f.comp.sortable()
	}
	return false
}

func lcFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
