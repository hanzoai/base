package validators_test

import (
	"errors"
	"testing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
)

// The two engines word a unique violation completely differently, and there is
// no Postgres in CI — so the Postgres half can only be proven against its
// wording. The literal match this replaced was SQLite's alone, which is why a
// duplicate on Postgres surfaced as a 500 instead of a 400 field error.
func TestIsUniqueViolation(t *testing.T) {
	for _, s := range []struct {
		name string
		err  error
		want bool
	}{
		{"sqlite", errors.New("UNIQUE constraint failed: demo2.title"), true},
		{"postgres, by message", errors.New(`ERROR: duplicate key value violates unique constraint "idx_unique_demo2_title" (SQLSTATE 23505)`), true},
		{"postgres, by sqlstate alone", errors.New("pq: something (SQLSTATE 23505)"), true},
		{"an unrelated failure", errors.New("no such table: demo2"), false},
		{"a foreign key failure is not this", errors.New("FOREIGN KEY constraint failed"), false},
		{"nil", nil, false},
	} {
		t.Run(s.name, func(t *testing.T) {
			if got := validators.IsUniqueViolation(s.err); got != s.want {
				t.Errorf("IsUniqueViolation(%v) = %v, want %v", s.err, got, s.want)
			}
		})
	}
}

// Postgres names the INDEX, not the columns, so the field is recovered from the
// index name. Base builds those names out of the column, and the match is on a
// whole underscore-delimited token — a substring match would let a field named
// "e" claim every index containing the letter.
func TestNormalizeUniqueIndexErrorOnPostgresWording(t *testing.T) {
	err := errors.New(`ERROR: duplicate key value violates unique constraint "idx_unique_demo2_title" (SQLSTATE 23505)`)

	got := validators.NormalizeUniqueIndexError(err, "demo2", []string{"title", "active"})

	errs, ok := got.(validation.Errors)
	if !ok {
		t.Fatalf("want a validation.Errors, got %T (%v) — a duplicate on Postgres would surface as a 500", got, got)
	}
	if _, has := errs["title"]; !has {
		t.Errorf("want the violation attributed to title, got %v", errs)
	}
	if _, has := errs["active"]; has {
		t.Errorf("active is not in that index and must not be blamed: %v", errs)
	}
}

// A field whose name merely appears inside another token is not the culprit.
func TestNormalizeUniqueIndexErrorDoesNotMatchASubstring(t *testing.T) {
	err := errors.New(`duplicate key value violates unique constraint "idx_unique_demo2_title" (SQLSTATE 23505)`)

	got := validators.NormalizeUniqueIndexError(err, "demo2", []string{"tit", "e"})

	if _, ok := got.(validation.Errors); ok {
		t.Fatalf("a substring of the index name was blamed as the field: %v", got)
	}
}
