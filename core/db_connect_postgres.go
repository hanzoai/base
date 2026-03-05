//go:build !no_pg_driver

package core

import (
	"fmt"

	"github.com/pocketbase/dbx"
	_ "github.com/jackc/pgx/v5/stdlib" // register "pgx" driver
)

// PostgresDBConnect creates a new PostgreSQL database connection.
// The dsn should be a PostgreSQL connection string like:
// "postgres://user:pass@host:5432/dbname?sslmode=disable"
func PostgresDBConnect(dsn string) (*dbx.DB, error) {
	db, err := dbx.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Connection pool settings
	db.DB().SetMaxOpenConns(50)
	db.DB().SetMaxIdleConns(10)

	// Verify connection
	if err := db.DB().Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	return db, nil
}
