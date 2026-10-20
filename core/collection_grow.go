package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hanzoai/base/tools/dbutils"
)

// Growth is what a grow changed.
type Growth struct {
	// Created names the collections the grow created.
	Created []string `json:"created"`

	// Grown names, per collection that already existed, what the grow added to
	// it: a field by its name, a select value as "field:value", an index as
	// "index:name".
	Grown map[string][]string `json:"grown"`

	// Kept names, per collection, the fields a document describes with another
	// type than the one the collection already has. They are left as they are.
	// A collection that exists with another type than the document's is kept
	// whole, as "type".
	Kept map[string][]string `json:"kept"`
}

// GrowCollections applies collection documents — the shape an import takes —
// to app additively, in one transaction, and reports what changed.
//
// A collection the documents name and app lacks is created as the document
// describes it: fields, rules, options and indexes. A collection that exists
// gains the fields it lacks (matched by name), the select values it lacks (with
// the document's options for them) and the indexes whose names it lacks.
// Nothing that exists is changed or dropped: not a field, a field's settings, a
// rule, an option or an index. A field the document describes with another type
// than the collection's is reported as kept.
//
// So growing the same documents twice changes nothing the second time, and an
// org that added fields of its own, renamed a label or tightened a rule keeps
// what it did.
//
// The one exception is a rule the document says it replaces. A document may
// carry `replaces`, naming for each rule ("listRule", …) the rules an earlier
// version of the same document gave it: an existing collection still holding
// one of those exactly takes the document's rule, and one holding anything
// else keeps it. That is how a module tightens a rule it shipped without
// overwriting a rule an org wrote.
//
// A relation field's collectionId may name its collection by name. It resolves
// against the documents and app alike, so a set of collections that relate to
// each other — or to themselves — is described without ids.
func GrowCollections(app App, docs []map[string]any) (*Growth, error) {
	if len(docs) == 0 {
		return nil, errors.New("no collections to grow")
	}

	wanted := make([]*Collection, len(docs))
	replaced := make([]map[string][]string, len(docs))
	for i, doc := range docs {
		r, err := rulesReplaced(doc["replaces"])
		if err != nil {
			return nil, fmt.Errorf("collection %d: %w", i, err)
		}
		replaced[i] = r

		raw, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		c := &Collection{}
		if err := json.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("collection %d: %w", i, err)
		}
		if c.Name == "" {
			return nil, fmt.Errorf("collection %d names no collection", i)
		}
		wanted[i] = c
	}

	growth := &Growth{Created: []string{}, Grown: map[string][]string{}, Kept: map[string][]string{}}

	err := app.RunInTransaction(func(txApp App) error {
		created := make(map[string]bool, len(wanted))

		// First, the base collections that do not exist, created bare: no
		// relation, no rule and no index, since each of those can name a
		// collection or a column the next step is still to make.
		for _, doc := range wanted {
			if !doc.IsBase() {
				continue
			}
			if _, err := findCollectionByName(txApp, doc.Name); err == nil {
				continue
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			shell := NewBaseCollection(doc.Name, freeCollectionId(txApp, doc.Id))
			shell.collectionBaseOptions = doc.collectionBaseOptions
			for _, f := range doc.Fields {
				if f.Type() != FieldTypeRelation && f.GetName() != FieldNameId {
					shell.Fields.Add(f)
				}
			}
			if err := txApp.Save(shell); err != nil {
				return fmt.Errorf("create %q: %w", doc.Name, err)
			}

			created[doc.Name] = true
			growth.Created = append(growth.Created, doc.Name)
		}

		// Then every collection is grown to what its document describes.
		for i, doc := range wanted {
			if err := growCollection(txApp, doc, replaced[i], created[doc.Name], growth); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return growth, nil
}

func growCollection(txApp App, doc *Collection, replaced map[string][]string, created bool, growth *Growth) error {
	cur, err := findCollectionByName(txApp, doc.Name)
	if errors.Is(err, sql.ErrNoRows) {
		// an auth or a view collection nobody has yet, created as described
		for _, f := range doc.Fields {
			if err := resolveRelation(txApp, f); err != nil {
				return fmt.Errorf("create %q: %w", doc.Name, err)
			}
		}
		doc.Id = freeCollectionId(txApp, doc.Id)
		if err := txApp.Save(doc); err != nil {
			return fmt.Errorf("create %q: %w", doc.Name, err)
		}
		growth.Created = append(growth.Created, doc.Name)
		return nil
	}
	if err != nil {
		return err
	}

	if cur.Type != doc.Type {
		growth.Kept[doc.Name] = append(growth.Kept[doc.Name], "type")
		return nil
	}

	var added, kept []string
	note := func(s string) {
		if !created {
			added = append(added, s)
		}
	}

	// a view's fields are its query's, so only its own document makes them
	if !cur.IsView() {
		for _, f := range doc.Fields {
			name := f.GetName()
			if name == FieldNameId {
				continue
			}

			have := cur.Fields.GetByName(name)
			switch {
			case have == nil:
				if err := resolveRelation(txApp, f); err != nil {
					return fmt.Errorf("grow %q: %w", doc.Name, err)
				}
				f.SetId("") // a fresh id, never one that could replace another field
				cur.Fields.Add(f)
				note(name)
			case have.Type() != f.Type():
				kept = append(kept, name)
			default:
				if hs, ok := have.(*SelectField); ok {
					for _, v := range growSelect(hs, f.(*SelectField)) {
						note(name + ":" + v)
					}
				}
			}
		}
	}

	if created {
		cur.ListRule, cur.ViewRule = doc.ListRule, doc.ViewRule
		cur.CreateRule, cur.UpdateRule, cur.DeleteRule = doc.CreateRule, doc.UpdateRule, doc.DeleteRule
		cur.Indexes = slices.Clone(doc.Indexes)
	} else if !cur.IsView() {
		for _, name := range ruleNames {
			have, want := ruleOf(cur, name), ruleOf(doc, name)
			if *have != nil && slices.Contains(replaced[name], **have) && !sameRule(*have, *want) {
				*have = cloneRule(*want)
				note("rule:" + name)
			}
		}
		for _, idx := range doc.Indexes {
			name := dbutils.ParseIndex(idx).IndexName
			if name == "" || cur.GetIndex(name) != "" || hasIndexDefinition(txApp, cur.Indexes, idx) {
				continue // already there, by name or under another one
			}
			cur.Indexes = append(cur.Indexes, idx)
			note("index:" + name)
		}
	}

	if len(kept) > 0 {
		growth.Kept[doc.Name] = append(growth.Kept[doc.Name], kept...)
	}

	if !created && len(added) == 0 {
		return nil // nothing to write
	}

	if err := txApp.Save(cur); err != nil {
		return fmt.Errorf("grow %q: %w", doc.Name, err)
	}

	if len(added) > 0 {
		growth.Grown[doc.Name] = append(growth.Grown[doc.Name], added...)
	}

	return nil
}

// ruleNames are a collection's rules, as a document spells them.
var ruleNames = []string{"listRule", "viewRule", "createRule", "updateRule", "deleteRule"}

func ruleOf(c *Collection, name string) **string {
	switch name {
	case "listRule":
		return &c.ListRule
	case "viewRule":
		return &c.ViewRule
	case "createRule":
		return &c.CreateRule
	case "updateRule":
		return &c.UpdateRule
	}
	return &c.DeleteRule
}

func sameRule(a, b *string) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func cloneRule(r *string) *string {
	if r == nil {
		return nil
	}
	v := *r
	return &v
}

// rulesReplaced reads a document's `replaces`: per rule, the earlier rules the
// document's own rule replaces.
func rulesReplaced(v any) (map[string][]string, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string][]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("replaces names, per rule, the rules it replaces: %w", err)
	}
	for name := range out {
		if !slices.Contains(ruleNames, name) {
			return nil, fmt.Errorf("replaces names %q, which is not a rule", name)
		}
	}
	return out, nil
}

// hasIndexDefinition reports whether indexes already holds idx's definition under
// some name: the same columns, uniqueness and condition on the same table.
func hasIndexDefinition(app App, indexes []string, idx string) bool {
	want := indexDefinition(app, idx)
	for _, have := range indexes {
		if indexDefinition(app, have) == want {
			return true
		}
	}
	return false
}

// indexDefinition is an index with its names set aside, as the collection
// validator compares two of them.
func indexDefinition(app App, idx string) string {
	parsed := dbutils.ParseIndex(idx)
	parsed.SchemaName = ""
	parsed.IndexName = "definition"
	return parsed.Build(app.Dialect())
}

// growSelect adds to have the values of want it lacks, with want's options for
// them, and returns the values it added.
func growSelect(have, want *SelectField) []string {
	var added []string
	for _, v := range want.Values {
		if slices.Contains(have.Values, v) {
			continue
		}
		have.Values = append(have.Values, v)
		added = append(added, v)
		for _, o := range want.Options {
			if o.Value == v {
				have.Options = append(have.Options, o)
			}
		}
	}
	return added
}

// resolveRelation points a relation field at its collection's id when the
// document named the collection.
func resolveRelation(app App, f Field) error {
	rel, ok := f.(*RelationField)
	if !ok {
		return nil
	}

	target, err := app.FindCollectionByNameOrId(rel.CollectionId)
	if err != nil {
		return fmt.Errorf("relation %q names no collection %q", rel.Name, rel.CollectionId)
	}
	rel.CollectionId = target.Id

	return nil
}

// findCollectionByName finds a collection by its name, never by an id that
// happens to read like the name.
func findCollectionByName(app App, name string) (*Collection, error) {
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(c.Name, name) {
		return nil, sql.ErrNoRows
	}
	return c, nil
}

// freeCollectionId keeps a document's id when no collection holds it, and
// otherwise lets the save generate one.
func freeCollectionId(app App, id string) string {
	if id == "" {
		return ""
	}
	if _, err := app.FindCollectionByNameOrId(id); err == nil {
		return ""
	}
	return id
}
