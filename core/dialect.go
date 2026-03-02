package core

// SQLDialect abstracts database-specific SQL syntax.
type SQLDialect interface {
	// DriverName returns the driver name ("sqlite3" or "pgx").
	DriverName() string

	// Placeholder returns the param placeholder for position n (1-indexed).
	// SQLite: "?", PostgreSQL: "$1", "$2", etc.
	Placeholder(n int) string

	// AutoincrementType returns the column type for auto-increment primary keys.
	AutoincrementType() string

	// JSONExtract returns SQL to extract a JSON field.
	// SQLite: json_extract(col, '$.field')
	// PostgreSQL: col->>'field' or col #>> '{path}'
	JSONExtract(column, path string) string

	// JSONEach returns a table-valued function call for iterating JSON arrays.
	// SQLite: json_each(col)
	// PostgreSQL: jsonb_array_elements(col)
	JSONEach(column string) string

	// JSONValid returns SQL to check if a value is valid JSON.
	JSONValid(column string) string

	// JSONArray returns SQL to create a JSON array from values.
	JSONArray(values ...string) string

	// Strftime returns SQL for date formatting.
	// SQLite: strftime(format, col)
	// PostgreSQL: to_char(col, format)
	Strftime(format, column string) string

	// Now returns SQL for current timestamp.
	Now() string

	// RandomID returns SQL to generate a random hex ID of given byte length.
	// SQLite: lower(hex(randomblob(n)))
	// PostgreSQL: encode(gen_random_bytes(n), 'hex')
	RandomID(byteLen int) string

	// Concat returns SQL for string concatenation.
	Concat(parts ...string) string

	// GroupConcat returns SQL for group concatenation.
	GroupConcat(column, separator string) string

	// TableColumns returns SQL to get column info for a table.
	TableColumns(table string) string

	// HasTable returns SQL to check if a table exists.
	HasTable(table string) string

	// ListTables returns SQL to list all user tables.
	ListTables() string

	// ListViews returns SQL to list all views.
	ListViews() string

	// NormalizeColumnType converts a SQLite column type to the dialect equivalent.
	// Pass through for SQLite, convert types for PostgreSQL.
	NormalizeColumnType(sqliteType string) string

	// Vacuum returns the SQL command to reclaim space.
	Vacuum() string

	// BooleanValue returns the SQL literal for a boolean.
	BooleanValue(b bool) string

	// SupportsReturning returns true if INSERT ... RETURNING is supported.
	SupportsReturning() bool

	// CastToText returns SQL to cast a column to text type.
	CastToText(column string) string
}

// DialectFromDriver returns the appropriate dialect for the given driver name.
func DialectFromDriver(driver string) SQLDialect {
	switch driver {
	case "pgx", "postgres", "postgresql":
		return &PostgresDialect{}
	default:
		return &SQLiteDialect{}
	}
}
