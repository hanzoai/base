package core

import (
	"fmt"
	"strings"
)

// SQLiteDialect implements SQLDialect for SQLite databases.
type SQLiteDialect struct{}

func (d *SQLiteDialect) DriverName() string {
	return "sqlite3"
}

func (d *SQLiteDialect) Placeholder(_ int) string {
	return "?"
}

func (d *SQLiteDialect) AutoincrementType() string {
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

func (d *SQLiteDialect) JSONExtract(column, path string) string {
	// Ensure path starts with $.
	if !strings.HasPrefix(path, "$.") && !strings.HasPrefix(path, "$[") {
		path = "$." + path
	}
	return fmt.Sprintf("json_extract(%s, '%s')", column, path)
}

func (d *SQLiteDialect) JSONEach(column string) string {
	return fmt.Sprintf("json_each(%s)", column)
}

func (d *SQLiteDialect) JSONValid(column string) string {
	return fmt.Sprintf("json_valid(%s)", column)
}

func (d *SQLiteDialect) JSONArray(values ...string) string {
	return fmt.Sprintf("json_array(%s)", strings.Join(values, ", "))
}

func (d *SQLiteDialect) Strftime(format, column string) string {
	return fmt.Sprintf("strftime('%s', %s)", format, column)
}

func (d *SQLiteDialect) Now() string {
	return "datetime('now')"
}

func (d *SQLiteDialect) RandomID(byteLen int) string {
	return fmt.Sprintf("lower(hex(randomblob(%d)))", byteLen)
}

func (d *SQLiteDialect) Concat(parts ...string) string {
	return strings.Join(parts, " || ")
}

func (d *SQLiteDialect) GroupConcat(column, separator string) string {
	return fmt.Sprintf("group_concat(%s, '%s')", column, separator)
}

func (d *SQLiteDialect) TableColumns(table string) string {
	return fmt.Sprintf("PRAGMA table_info('%s')", table)
}

func (d *SQLiteDialect) HasTable(table string) string {
	return fmt.Sprintf(
		"SELECT 1 FROM sqlite_master WHERE type='table' AND name='%s' LIMIT 1",
		table,
	)
}

func (d *SQLiteDialect) ListTables() string {
	return "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
}

func (d *SQLiteDialect) ListViews() string {
	return "SELECT name FROM sqlite_master WHERE type='view' ORDER BY name"
}

func (d *SQLiteDialect) NormalizeColumnType(sqliteType string) string {
	return sqliteType
}

func (d *SQLiteDialect) Vacuum() string {
	return "VACUUM"
}

func (d *SQLiteDialect) BooleanValue(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (d *SQLiteDialect) SupportsReturning() bool {
	return false
}

func (d *SQLiteDialect) CastToText(column string) string {
	return fmt.Sprintf("CAST(%s AS TEXT)", column)
}
