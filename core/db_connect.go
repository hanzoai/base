//go:build !no_default_driver

package core

import (
	"github.com/hanzoai/dbx"
	"github.com/hanzoai/sqlite"
)

// DefaultDBConnect opens an embedded SQLite database on the canonical Hanzo
// driver (github.com/hanzoai/sqlite). That package registers the "sqlite"
// database/sql driver name under BOTH build configs — modernc (pure Go, !cgo)
// and mattn/SQLCipher (cgo) — so exactly ONE package registers "sqlite" no
// matter how Base is linked (standalone !cgo, or embedded into a cgo binary such
// as commerce). Base MUST NOT import modernc.org/sqlite directly, or a cgo build
// double-registers "sqlite" and panics at init.
//
// sqlite.PragmaDSN encodes sqlite.DefaultPragmas (busy_timeout leads — a
// connection must block on a busy DB before journal_mode=WAL is set) in the
// ACTIVE backend's DSN syntax, so the pragmas apply on both backends; a
// single-form DSN would be silently dropped by the other backend.
func DefaultDBConnect(dbPath string) (*dbx.DB, error) {
	return dbx.Open("sqlite", sqlite.PragmaDSN(dbPath, sqlite.DefaultPragmas))
}
