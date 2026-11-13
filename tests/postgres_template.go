package tests

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hanzoai/base/core"
)

// A test app on PostgreSQL starts as the fixture does on SQLite: the migrations
// applied and the fixture's collections and records in place. Building that takes
// seconds (the migrations, then a table per collection), which a suite that makes
// an app per test cannot pay each time. So it is built ONCE per fixture directory
// into a template database, and each app is a copy of the template, which the
// server makes in tens of milliseconds.
type template struct {
	once sync.Once
	name string
	err  error
}

var (
	templates   sync.Map // fixture directory + encryption env -> *template
	clones      atomic.Int64
	templateSeq atomic.Int64
)

// postgresClone returns a DSN for a database that holds a fresh copy of the
// fixture, and the release for it. Without a PostgreSQL server it answers "".
func postgresClone(fixture, env string) (data, aux string, drop func(), err error) {
	if postgres == "" {
		return "", "", nil, nil
	}
	v, _ := templates.LoadOrStore(fixture+"\x00"+env, &template{})
	t := v.(*template)
	t.once.Do(func() { t.name, t.err = buildTemplate(fixture, env) })
	if t.err != nil {
		return "", "", nil, t.err
	}

	name := fmt.Sprintf("hz_t_%d_%d", os.Getpid(), clones.Add(1))
	if err := exec(fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, name, t.name)); err != nil {
		return "", "", nil, fmt.Errorf("tests: copying the template database: %w", err)
	}
	data, err = databaseDSN(name)
	if err != nil {
		return "", "", nil, err
	}
	return data, auxiliary(data), func() { _ = exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`) }, nil
}

func buildTemplate(fixture, env string) (string, error) {
	name := fmt.Sprintf("hz_tpl_%d_%d", os.Getpid(), templateSeq.Add(1))
	if err := exec(`CREATE DATABASE ` + name); err != nil {
		return "", fmt.Errorf("tests: creating the template database: %w", err)
	}
	dsn, err := databaseDSN(name)
	if err != nil {
		return "", err
	}
	// the auxiliary database is a schema of its own, as a host that places a Base
	// gives it one; the data is in public
	if err := execOn(dsn, `CREATE SCHEMA aux`); err != nil {
		return "", fmt.Errorf("tests: creating the auxiliary schema: %w", err)
	}
	dir, err := TempDirClone(fixture)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir, EncryptionEnv: env, DataDSN: dsn, AuxDSN: auxiliary(dsn)})
	if err := app.Bootstrap(); err != nil {
		return "", fmt.Errorf("tests: bootstrap the template: %w", err)
	}
	// closing the app is what lets the server copy the database
	defer func() { _ = app.ResetBootstrapState() }()
	if err := app.RunAllMigrations(); err != nil {
		return "", fmt.Errorf("tests: migrate the template: %w", err)
	}
	if err := loadFixture(app, fixture); err != nil {
		return "", err
	}
	return name, nil
}

// databaseDSN names a database on the server the suite was pointed at.
func databaseDSN(name string) (string, error) {
	u, err := url.Parse(postgres)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// auxiliary is the DSN of the auxiliary database for a data DSN.
func auxiliary(dsn string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&search_path=aux"
	}
	return dsn + "?search_path=aux"
}

func execOn(dsn, query string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(query)
	return err
}
