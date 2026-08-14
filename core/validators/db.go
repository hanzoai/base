package validators

import (
	"database/sql"
	"errors"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/dbx"
)

// UniqueId checks whether a field string id already exists in the specified table.
//
// Example:
//
//	validation.Field(&form.RelId, validation.By(validators.UniqueId(form.app.DB(), "tbl_example"))
func UniqueId(db dbx.Builder, tableName string) validation.RuleFunc {
	return func(value any) error {
		v, _ := value.(string)
		if v == "" {
			return nil // nothing to check
		}

		var foundId string

		err := db.
			Select("id").
			From(tableName).
			Where(dbx.HashExp{"id": v}).
			Limit(1).
			Row(&foundId)

		if (err != nil && !errors.Is(err, sql.ErrNoRows)) || foundId != "" {
			return validation.NewError("validation_invalid_or_existing_id", "The model id is invalid or already exists.")
		}

		return nil
	}
}

// NormalizeUniqueIndexError attempts to convert a
// "unique constraint failed" error into a validation.Errors.
//
// The provided err is returned as it is without changes if:
// - err is nil
// - err is already validation.Errors
// - err is not "unique constraint failed" error
func NormalizeUniqueIndexError(err error, tableOrAlias string, fieldNames []string) error {
	if err == nil {
		return err
	}

	if _, ok := err.(validation.Errors); ok {
		return err
	}

	msg := strings.ToLower(err.Error())

	if !IsUniqueViolation(err) {
		return err
	}

	// Postgres names the INDEX rather than the columns
	// ("duplicate key value violates unique constraint \"idx_unique_demo2_title\""),
	// so the field is recovered from the index name instead. Base builds those
	// names out of the column, and matching a whole underscore-delimited token
	// rather than a substring is what keeps a field named "e" from matching
	// every index that happens to contain the letter.
	if !strings.Contains(msg, "unique constraint failed") {
		if idx := uniqueIndexName(msg); idx != "" {
			tokens := map[string]bool{}
			for _, t := range strings.Split(idx, "_") {
				tokens[t] = true
			}
			normalizedErrs := validation.Errors{}
			for _, name := range fieldNames {
				if tokens[strings.ToLower(name)] {
					normalizedErrs[name] = validation.NewError("validation_not_unique", "Value must be unique")
				}
			}
			if len(normalizedErrs) > 0 {
				return normalizedErrs
			}
		}
		return err
	}

	// check for unique constraint failure
	if strings.Contains(msg, "unique constraint failed") {
		// note: extra space to unify multi-columns lookup
		msg = strings.ReplaceAll(strings.TrimSpace(msg), ",", " ") + " "

		normalizedErrs := validation.Errors{}

		for _, name := range fieldNames {
			// note: extra spaces to exclude table name with suffix matching the current one
			// 		 OR other fields starting with the current field name
			if strings.Contains(msg, strings.ToLower(" "+tableOrAlias+"."+name+" ")) {
				normalizedErrs[name] = validation.NewError("validation_not_unique", "Value must be unique")
			}
		}

		if len(normalizedErrs) > 0 {
			return normalizedErrs
		}
	}

	return err
}

// IsUniqueViolation reports whether err is the engine's way of saying a unique
// index was violated.
//
// The two engines this store runs on word it completely differently, and the
// literal match this used to be ("unique constraint failed") is SQLite's alone —
// so on Postgres a duplicate never normalized into a field error and surfaced as
// a 500 instead of a 400. It is also what the table wire reads to answer with
// SQLSTATE 23505, which is the code REST clients branch on.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || // sqlite
		strings.Contains(msg, "duplicate key value violates unique constraint") || // postgres
		strings.Contains(msg, "sqlstate 23505") // postgres, when the driver reports the code
}

// uniqueIndexName pulls the quoted index name out of a Postgres duplicate-key
// message, or returns "" when there is not one to read.
func uniqueIndexName(msg string) string {
	const marker = `unique constraint "`
	i := strings.Index(msg, marker)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
