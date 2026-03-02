package core

import (
	"fmt"
	"strings"
)

// PostgresDialect implements SQLDialect for PostgreSQL databases.
type PostgresDialect struct{}

func (d *PostgresDialect) DriverName() string {
	return "pgx"
}

func (d *PostgresDialect) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n)
}

func (d *PostgresDialect) AutoincrementType() string {
	return "BIGSERIAL PRIMARY KEY"
}

func (d *PostgresDialect) JSONExtract(column, path string) string {
	// Strip leading "$." or "$" prefix used by SQLite JSON paths.
	p := path
	if strings.HasPrefix(p, "$.") {
		p = p[2:]
	} else if strings.HasPrefix(p, "$") {
		p = p[1:]
	}

	// Handle nested paths like "a.b.c" -> col #>> '{a,b,c}'
	parts := strings.Split(p, ".")
	if len(parts) == 1 {
		return fmt.Sprintf("%s->>'%s'", column, parts[0])
	}
	return fmt.Sprintf("%s #>> '{%s}'", column, strings.Join(parts, ","))
}

func (d *PostgresDialect) JSONEach(column string) string {
	return fmt.Sprintf("jsonb_array_elements(%s)", column)
}

func (d *PostgresDialect) JSONValid(column string) string {
	return fmt.Sprintf("(%s)::jsonb IS NOT NULL", column)
}

func (d *PostgresDialect) JSONArray(values ...string) string {
	return fmt.Sprintf("jsonb_build_array(%s)", strings.Join(values, ", "))
}

// sqliteToPostgresFormat maps SQLite strftime format specifiers to PostgreSQL to_char patterns.
var sqliteToPostgresFormat = map[string]string{
	"%Y": "YYYY",
	"%m": "MM",
	"%d": "DD",
	"%H": "HH24",
	"%M": "MI",
	"%S": "SS",
	"%s": "epoch",
	"%w": "D",
	"%f": "SS.US",
}

func (d *PostgresDialect) Strftime(format, column string) string {
	// Handle epoch specially: extract epoch returns a number, not a formatted string.
	if format == "%s" {
		return fmt.Sprintf("EXTRACT(EPOCH FROM %s)", column)
	}

	pgFormat := format
	for sqliteFmt, pgFmt := range sqliteToPostgresFormat {
		pgFormat = strings.ReplaceAll(pgFormat, sqliteFmt, pgFmt)
	}

	// Handle the datetime format: %Y-%m-%d %H:%M:%fZ
	// After replacement this becomes: YYYY-MM-DD HH24:MI:SS.USZ
	return fmt.Sprintf("to_char(%s, '%s')", column, pgFormat)
}

func (d *PostgresDialect) Now() string {
	return "NOW()"
}

func (d *PostgresDialect) RandomID(byteLen int) string {
	return fmt.Sprintf("encode(gen_random_bytes(%d), 'hex')", byteLen)
}

func (d *PostgresDialect) Concat(parts ...string) string {
	return strings.Join(parts, " || ")
}

func (d *PostgresDialect) GroupConcat(column, separator string) string {
	return fmt.Sprintf("string_agg(%s, '%s')", column, separator)
}

func (d *PostgresDialect) TableColumns(table string) string {
	return fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_name = '%s' ORDER BY ordinal_position",
		table,
	)
}

func (d *PostgresDialect) HasTable(table string) string {
	return fmt.Sprintf(
		"SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = '%s' LIMIT 1",
		table,
	)
}

func (d *PostgresDialect) ListTables() string {
	return "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name"
}

func (d *PostgresDialect) ListViews() string {
	return "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'VIEW' ORDER BY table_name"
}

// pgTypeMap maps SQLite type names to PostgreSQL equivalents.
var pgTypeMap = map[string]string{
	"INTEGER": "BIGINT",
	"REAL":    "DOUBLE PRECISION",
	"BLOB":    "BYTEA",
	"JSON":    "JSONB",
	// These map to themselves but are listed for clarity.
	"TEXT":    "TEXT",
	"BOOLEAN": "BOOLEAN",
}

func (d *PostgresDialect) NormalizeColumnType(sqliteType string) string {
	upper := strings.ToUpper(strings.TrimSpace(sqliteType))
	if pgType, ok := pgTypeMap[upper]; ok {
		return pgType
	}
	return sqliteType
}

func (d *PostgresDialect) Vacuum() string {
	return "VACUUM"
}

func (d *PostgresDialect) BooleanValue(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}

func (d *PostgresDialect) SupportsReturning() bool {
	return true
}

func (d *PostgresDialect) CastToText(column string) string {
	return fmt.Sprintf("CAST(%s AS TEXT)", column)
}
