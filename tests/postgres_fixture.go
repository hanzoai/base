package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/dbx"
	"github.com/hanzoai/sqlite"
)

// loadFixture brings the fixture's rows (tests/data, the cloned data.db and
// auxiliary.db) into a Base that runs on PostgreSQL, so a test finds on either
// engine the superusers, collections and records it was written against.
//
// The collections go first, through the Base's own import, because saving a
// collection is what makes the table its records live in, in PostgreSQL's
// spelling; then every table the fixture holds is copied, row for row, over
// whatever the migrations seeded.
func loadFixture(app core.App, fixture string) error {
	// A Base of its own reads the fixture's collections, and a Base watches its
	// data directory and reloads the app it belongs to when a marker file
	// appears there; so it reads a copy, not the directory the app was given.
	dir, err := TempDirClone(fixture)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if os.Getenv("HZ_TEST_SQLLOG") != "" {
		if db, ok := app.NonconcurrentDB().(*dbx.DB); ok {
			db.ExecLogFunc = func(ctx context.Context, t time.Duration, q string, r sql.Result, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "SQLERR %v :: %s\n", err, q)
				}
			}
		}
	}
	for _, f := range []struct {
		file string
		dst  dbx.Builder
		cols bool
	}{
		{"data.db", app.NonconcurrentDB(), true},
		{"auxiliary.db", app.AuxNonconcurrentDB(), false},
	} {
		src, err := sql.Open("sqlite", sqlite.PragmaDSN(dir+"/"+f.file, sqlite.DefaultPragmas))
		if err != nil {
			return err
		}
		err = func() error {
			defer src.Close()
			if f.cols {
				if err := importCollections(app, dir); err != nil {
					return err
				}
			}
			to, ok := f.dst.(*dbx.DB)
			if !ok {
				return fmt.Errorf("tests: no database handle for %s", f.file)
			}
			return copyAll(src, to.DB())
		}()
		if err != nil {
			return fmt.Errorf("tests: load %s on PostgreSQL: %w", f.file, err)
		}
	}
	return app.ReloadCachedCollections()
}

// importCollections creates, on the app, the collections the fixture holds. They are
// read through a Base of their own on the fixture's files, which is what knows the
// form a collection is exported in (its options are flattened into it).
func importCollections(app core.App, dir string) error {
	src := core.NewBaseApp(core.BaseAppConfig{DataDir: dir, EncryptionEnv: "hz_test_env"})
	if err := src.Bootstrap(); err != nil {
		return err
	}
	defer func() { _ = src.ResetBootstrapState() }()
	cols, err := src.FindAllCollections()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(cols)
	if err != nil {
		return err
	}
	// A collection is exported without the secrets of its OAuth2 providers, and an
	// import validates that they are there. The rows are copied over afterwards,
	// secrets included, so any value does to get the table made.
	var docs []map[string]any
	if err := json.Unmarshal(raw, &docs); err != nil {
		return err
	}
	for _, c := range docs {
		if o, ok := c["oauth2"].(map[string]any); ok {
			if ps, ok := o["providers"].([]any); ok {
				for _, p := range ps {
					if pm, ok := p.(map[string]any); ok && pm["clientSecret"] == nil || pm["clientSecret"] == "" {
						pm["clientSecret"] = "placeholder"
					}
				}
			}
		}
	}
	if raw, err = json.Marshal(docs); err != nil {
		return err
	}
	return app.ImportCollectionsByMarshaledJSON(raw, false)
}

func copyAll(from, to *sql.DB) error {
	rows, err := from.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, n)
	}
	rows.Close()
	for _, t := range tables {
		if err := copyOne(from, to, t); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return nil
}

func copyOne(from, to *sql.DB, table string) error {
	types, err := serverColumns(to, table)
	if err != nil {
		return err
	}
	if len(types) == 0 {
		// a table of the fixture the collections did not make (a view's, a
		// legacy one): nothing to hold it
		return nil
	}
	cr, err := from.Query(`SELECT name FROM pragma_table_info('` + table + `') ORDER BY cid`)
	if err != nil {
		return err
	}
	var cols []string
	for cr.Next() {
		var n string
		if err := cr.Scan(&n); err != nil {
			cr.Close()
			return err
		}
		if _, ok := types[n]; ok {
			cols = append(cols, n)
		}
	}
	cr.Close()
	if len(cols) == 0 {
		return nil
	}
	// the fixture's rows replace what the migrations seeded
	if _, err := to.Exec(`DELETE FROM "` + table + `"`); err != nil {
		return err
	}
	rows, err := from.Query(`SELECT ` + quotedList(cols) + ` FROM "` + table + `"`)
	if err != nil {
		return err
	}
	defer rows.Close()
	size := 200
	if n := 65535 / len(cols); n < size {
		size = n
	}
	var values []any
	count := 0
	flush := func() error {
		if count == 0 {
			return nil
		}
		var b strings.Builder
		b.WriteString(`INSERT INTO "` + table + `" (` + quotedList(cols) + `) VALUES `)
		n := 1
		for r := 0; r < count; r++ {
			if r > 0 {
				b.WriteString(",")
			}
			b.WriteString("(")
			for c := range cols {
				if c > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, "$%d", n)
				n++
			}
			b.WriteString(")")
		}
		b.WriteString(" ON CONFLICT DO NOTHING")
		if _, err := to.Exec(b.String(), values...); err != nil {
			return err
		}
		values, count = values[:0], 0
		return nil
	}
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, c := range cols {
			values = append(values, adapt(cells[i], types[c]))
		}
		count++
		if count == size {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func serverColumns(to *sql.DB, table string) (map[string]string, error) {
	rows, err := to.Query(`SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var n, t string
		if err := rows.Scan(&n, &t); err != nil {
			return nil, err
		}
		out[n] = strings.ToLower(t)
	}
	return out, rows.Err()
}

// adapt turns a value SQLite handed back into one the server's column takes.
func adapt(v any, typ string) any {
	if v == nil {
		return nil
	}
	str := func() string {
		switch t := v.(type) {
		case string:
			return t
		case []byte:
			return string(t)
		}
		return fmt.Sprint(v)
	}
	switch typ {
	case "boolean":
		switch t := v.(type) {
		case bool:
			return t
		case int64:
			return t != 0
		}
		s := str()
		return s == "true" || s == "1"
	case "json", "jsonb":
		if s := str(); s != "" {
			return s
		}
		return nil
	case "numeric", "integer", "bigint", "smallint", "real", "double precision":
		if s := str(); s == "" {
			return nil
		}
		if b, ok := v.([]byte); ok {
			return string(b)
		}
		return v
	case "bytea":
		return v
	default:
		if b, ok := v.([]byte); ok {
			return string(b)
		}
		return v
	}
}

func quotedList(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = `"` + c + `"`
	}
	return strings.Join(out, ",")
}
