//go:build !no_default_driver

package core

import (
	"github.com/hanzoai/dbx"
	"github.com/hanzoai/sqlite"
)

// DefaultDBConnect opens an embedded SQLite database on the canonical Hanzo
// driver (github.com/hanzoai/sqlite). That package registers the "sqlite"
// database/sql driver name under BOTH build configs — its pure-Go backend
// (!cgo) and its hanzoai/sqlcipher (cgo) backend — so exactly ONE package
// registers "sqlite" no matter how Base is linked (standalone !cgo, or embedded
// into a cgo binary such as commerce). Base MUST NOT import a low-level SQLite
// driver (modernc.org/sqlite or hanzoai/csqlite) directly, or a build
// double-registers "sqlite" and panics at init.
//
// sqlite.PragmaDSN encodes sqlite.DefaultPragmas (busy_timeout leads — a
// connection must block on a busy DB before journal_mode=WAL is set) in the
// ACTIVE backend's DSN syntax, so the pragmas apply on both backends; a
// single-form DSN would be silently dropped by the other backend.
//
// VerifySQLiteMathFunctions runs once per process and refuses a build whose
// linked SQLite cannot answer the search layer's geoDistance filter, so a
// deficient binary dies here rather than at a customer's failed search. See
// sqlite_math.go.
func DefaultDBConnect(dbPath string) (*dbx.DB, error) {
	if err := VerifySQLiteMathFunctions(); err != nil {
		return nil, err
	}

	return dbx.Open("sqlite", sqlite.PragmaDSN(dbPath, sqlite.DefaultPragmas))
}
