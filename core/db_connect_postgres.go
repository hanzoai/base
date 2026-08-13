//go:build !no_pg_driver

package core

import (
	"fmt"

	"github.com/hanzoai/dbx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// PostgresDBConnect creates a new PostgreSQL database connection.
// The dsn should be a PostgreSQL connection string like:
// "postgres://user:pass@host:5432/dbname?sslmode=disable"
//
// Sizing the pool is the caller's, the same as for a SQLite connection.
func PostgresDBConnect(dsn string) (*dbx.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL DSN: %w", err)
	}

	// a statement is described where it is executed, so a connection already
	// in the pool serves the schema as it stands rather than as it stood
	config.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec

	db := dbx.NewFromDB(stdlib.OpenDB(*config), "pgx")

	// Verify connection
	if err := db.DB().Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	return db, nil
}
