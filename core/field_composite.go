package core

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/tools/dbutils"
	"github.com/hanzoai/base/tools/inflector"
	"github.com/hanzoai/dbx"
)

// CompositeField is a field whose value is one JSON object of named parts: a
// currency's amount and code, a person's first and last name, an address.
//
// The column holds the whole object, NULL when the value is empty. A filter, a
// sort or an aggregate reaches one part with a dotted name (`name.lastName`),
// exactly as it reaches into a JSON field, and the parts are named here so a
// path that names no part is refused rather than read as NULL.
type CompositeField interface {
	Field

	// Parts names the value's parts, in the order a person reads them.
	Parts() []string
}

// TextParts names the parts of a composite value that hold text a person
// searches: the ones a "contains" over the whole value reads.
type TextParts interface {
	TextParts() []string
}

// IsComposite reports whether field holds a composite value.
func IsComposite(field Field) bool {
	_, ok := field.(CompositeField)
	return ok
}

// HasPart reports whether a composite field names part.
func HasPart(field Field, part string) bool {
	c, ok := field.(CompositeField)
	return ok && slices.Contains(c.Parts(), part)
}

// compositeColumnType is the column every composite value is stored in: a JSON
// document, NULL when empty.
func compositeColumnType(app App) string {
	return app.Dialect().Json() + " DEFAULT NULL"
}

// defaultMaxValues is how many items a multi-item composite (emails, phones,
// links) holds when its field sets no limit; maxMaxValues is the most a field may
// allow.
const (
	defaultMaxValues = 10
	maxMaxValues     = 100
)

// maxValuesOf reads a field's MaxValues setting.
func maxValuesOf(n int) int {
	if n <= 0 {
		return defaultMaxValues
	}
	return n
}

func validateMaxValues(n *int) *validation.FieldRules {
	return validation.Field(n, validation.Min(0), validation.Max(maxMaxValues))
}

var errValueNotUnique = validation.NewError("validation_not_unique", "Value must be unique.")

// checkUniquePart refuses a value whose part is already held by another record
// of the same collection. It is how a composite's primary part (an email, a
// phone number, a URL) is kept unique: the part lives inside a JSON document, so
// no column index can say it, and an expression index is spelled differently on
// every engine.
//
// It compares case-insensitively, since every such part is matched that way by
// the people who type it.
//
// It is a read before the write, so it holds where the engine admits one writer
// (SQLite). On PostgreSQL two writers of one value at once can both pass it; a
// unique expression index from the dialect is what would close that.
func checkUniquePart(ctx context.Context, app App, record *Record, field, part, value string) error {
	if value == "" {
		return nil
	}

	table := inflector.Columnify(record.Collection().Name)
	expr := dbutils.JSONExtract(app.Dialect(), inflector.Columnify(field), part)

	var exists int
	err := app.ConcurrentDB().
		Select("(1)").
		From(table).
		Where(dbx.NewExp("LOWER("+expr+") = LOWER({:value})", dbx.Params{"value": value})).
		AndWhere(dbx.Not(dbx.HashExp{"id": record.Id})).
		Limit(1).
		WithContext(ctx).
		Row(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if exists == 1 {
		return errValueNotUnique
	}

	return nil
}
