package core

import (
	"fmt"
	"os"
	"strings"
)

// StorageTier names the data backend for a Base instance. SQLite is the
// zero-config default; sql and datastore are per-instance upgrades along one
// axis — the /v1 data plane (collections/records/auth/files/SQL/realtime) is
// identical across tiers, so apps built on Base don't change when a tier moves.
type StorageTier string

const (
	// TierSQLite is the default: embedded SQLite (or :memory:). Zero config —
	// the out-of-box SaaS default, used for everything until an instance grows.
	TierSQLite StorageTier = "sqlite"

	// TierSQL is hanzoai/sql (PostgreSQL) — relational scale, multi-writer.
	// Upgrade in place by setting BASE_DB_TIER=sql + BASE_DB_URL; no app rewrite.
	TierSQL StorageTier = "sql"

	// TierDatastore is hanzoai/datastore (horizontal OLAP analytics). Reserved:
	// the backend adapter is not wired yet, so selecting it errors honestly
	// rather than silently falling back.
	TierDatastore StorageTier = "datastore"
)

// ResolveStorageTier reads the declared storage tier and its DSNs from the
// environment. One knob, default sqlite, upgrade in place — no app rewrite.
//
//	BASE_DB_TIER   sqlite (default) | sql | datastore
//	BASE_DB_URL    PostgreSQL DSN for tier=sql (the main data DB); BASE_DATA_DSN
//	               is accepted as an alias
//	BASE_AUX_DSN   optional separate aux-DB DSN (defaults to the data DSN)
//
// It returns the resolved tier plus the data/aux DSNs to apply to
// BaseAppConfig. Empty DSNs mean SQLite (file or :memory:). A misconfigured
// tier (sql without a URL, the not-yet-wired datastore, or an unknown value)
// returns an error so the daemon fails loudly at startup instead of running on
// the wrong backend.
func ResolveStorageTier() (tier StorageTier, dataDSN, auxDSN string, err error) {
	switch t := StorageTier(strings.ToLower(strings.TrimSpace(os.Getenv("BASE_DB_TIER")))); t {
	case "", TierSQLite:
		return TierSQLite, "", "", nil
	case TierSQL:
		dsn := firstNonEmptyEnv("BASE_DB_URL", "BASE_DATA_DSN")
		if dsn == "" {
			return TierSQL, "", "", fmt.Errorf("BASE_DB_TIER=sql requires BASE_DB_URL (a hanzoai/sql PostgreSQL DSN)")
		}
		aux := firstNonEmptyEnv("BASE_AUX_DSN")
		if aux == "" {
			aux = dsn
		}
		return TierSQL, dsn, aux, nil
	case TierDatastore:
		return TierDatastore, "", "", fmt.Errorf("BASE_DB_TIER=datastore (hanzoai/datastore) is not yet available — use sqlite (default) or sql")
	default:
		return t, "", "", fmt.Errorf("unknown BASE_DB_TIER %q (want sqlite, sql, or datastore)", t)
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
