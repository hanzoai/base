package dbutils

import (
	"github.com/hanzoai/orm/dialect"
)

// The helpers here take a column name and hand the engine's spelling back with
// the identifier bracketed the way dbx expects it. The spelling itself belongs
// to the dialect — a column declared to hold JSON may equally hold a bare
// scalar, and normalizing that is the same problem on every engine.

// JSONEach is a relation with one row per element of the column, in a column
// named value.
func JSONEach(d dialect.Dialect, column string) string {
	return d.Each(bracket(column))
}

// JSONArrayLength counts the elements of the column. An empty or null column
// is 0.
func JSONArrayLength(d dialect.Dialect, column string) string {
	return d.Length(bracket(column))
}

// JSONExtract reads path out of the column as text. The path is relative to
// the value: "a.b", "[0]" or "" for the value itself.
func JSONExtract(d dialect.Dialect, column string, path string) string {
	// a property path reads as a member unless it already opens with an index
	if path != "" && path[0] != '[' {
		path = "." + path
	}

	return d.Extract(bracket(column), path)
}

func bracket(column string) string {
	return "[[" + column + "]]"
}
