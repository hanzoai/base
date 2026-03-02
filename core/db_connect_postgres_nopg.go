//go:build no_pg_driver

package core

import (
	"errors"

	"github.com/pocketbase/dbx"
)

// PostgresDBConnect is a stub when the pg driver is excluded via the no_pg_driver build tag.
func PostgresDBConnect(_ string) (*dbx.DB, error) {
	return nil, errors.New("PostgreSQL support is not available (built with no_pg_driver tag)")
}
