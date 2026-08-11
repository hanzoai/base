package dbutils_test

import (
	"strings"
	"testing"

	"github.com/hanzoai/base/tools/dbutils"
	"github.com/hanzoai/orm/dialect"
)

// The engine's spelling is the dialect's to get right, and it is tested there
// by executing it. What this layer owes its callers is the bracketing dbx
// expects around a column name, and a path that reads as a member unless it
// already opens with an index.

func TestJSONColumnIsBracketed(t *testing.T) {
	d := dialect.For("sqlite")

	for name, got := range map[string]string{
		"each":    dbutils.JSONEach(d, "a.b"),
		"length":  dbutils.JSONArrayLength(d, "a.b"),
		"extract": dbutils.JSONExtract(d, "a.b", ""),
	} {
		if strings.Contains(got, "[[a.b]]") {
			continue
		}
		t.Errorf("%s did not bracket the column: %s", name, got)
	}
}

func TestJSONExtractPath(t *testing.T) {
	d := dialect.For("sqlite")

	for path, want := range map[string]string{
		"":      "'$'",
		"a":     "'$.a'",
		"a.b":   "'$.a.b'",
		"[0]":   "'$[0]'",
		"[0].a": "'$[0].a'",
	} {
		if got := dbutils.JSONExtract(d, "col", path); !strings.Contains(got, want) {
			t.Errorf("path %q produced %s, wanted it to carry %s", path, got, want)
		}
	}
}
