// Package cloudsql implements Hanzo Cloud SQL — a serverless PostgreSQL
// integration plugin for Hanzo Base. It manages per-base database
// provisioning, connection routing, and proxies schema management requests
// to postgres-meta.
//
// Hanzo Cloud SQL is the scalable, per-org PostgreSQL layer backed by
// Neon's open-source storage/compute separation. "Hanzo SQL" (sql.hanzo.svc)
// is the shared single-instance PostgreSQL; "Hanzo Cloud SQL" is the
// auto-scaling, per-base serverless variant.
//
// Example:
//
//	cloudsql.MustRegister(app, cloudsql.Config{
//		MetaURL:        "http://base-meta.hanzo.svc:8080",
//		ComputeHost:    "cloud-sql.hanzo.svc",
//		ComputePort:    5432,
//		DefaultPGUser:  "hanzo",
//		DefaultPGPass:  "from-kms",
//	})
package cloudsql

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/hanzoai/base/apis"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/dbx"
)

// Config defines the configuration for the Cloud SQL plugin.
type Config struct {
	// MetaURL is the URL of the postgres-meta service.
	MetaURL string

	// ComputeHost is the hostname of the Cloud SQL compute endpoint.
	ComputeHost string

	// ComputePort is the port for Cloud SQL compute (default: 5432).
	ComputePort int

	// DefaultPGUser is the PostgreSQL admin user (default: "hanzo").
	DefaultPGUser string

	// DefaultPGPass is the PostgreSQL admin password.
	DefaultPGPass string

	// ProxyHost is the Cloud SQL proxy hostname for client connections.
	ProxyHost string

	// ProxyPort is the Cloud SQL proxy port for client connections.
	ProxyPort int
}

// MustRegister registers the Cloud SQL plugin and panics on failure.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the Cloud SQL plugin to the provided app instance.
func Register(app core.App, config Config) error {
	if config.MetaURL == "" {
		config.MetaURL = "http://base-meta.hanzo.svc:8080"
	}
	if config.ComputeHost == "" {
		config.ComputeHost = "cloud-sql.hanzo.svc"
	}
	if config.ComputePort == 0 {
		config.ComputePort = 5432
	}
	if config.DefaultPGUser == "" {
		config.DefaultPGUser = "hanzo"
	}
	if config.ProxyHost == "" {
		config.ProxyHost = config.ComputeHost
	}
	if config.ProxyPort == 0 {
		config.ProxyPort = config.ComputePort
	}

	metaURL, err := url.Parse(config.MetaURL)
	if err != nil {
		return fmt.Errorf("cloudsql: invalid meta URL: %w", err)
	}

	p := &plugin{
		app:       app,
		config:    config,
		metaProxy: httputil.NewSingleHostReverseProxy(metaURL),
		orgDB: &orgDBMap{
			databases: make(map[string]*OrgDatabase),
		},
	}

	p.metaProxy.Director = func(req *http.Request) {
		req.URL.Scheme = metaURL.Scheme
		req.URL.Host = metaURL.Host
		req.Host = metaURL.Host
	}

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return p.ensureCollections()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		p.registerRoutes(e.Router)
		return e.Next()
	})

	return nil
}

type plugin struct {
	app       core.App
	config    Config
	metaProxy *httputil.ReverseProxy
	orgDB     *orgDBMap
}

// OrgDatabase holds Cloud SQL database info for a base.
type OrgDatabase struct {
	OrgID        string `json:"orgId"`
	DatabaseName string `json:"databaseName"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Password     string `json:"password"`
	SSLMode      string `json:"sslMode"`
}

// ConnectionString returns a PostgreSQL connection string.
func (t *OrgDatabase) ConnectionString() string {
	sslMode := t.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		t.User, t.Password, t.Host, t.Port, t.DatabaseName, sslMode)
}

type orgDBMap struct {
	mu        sync.RWMutex
	databases map[string]*OrgDatabase
}

func (m *orgDBMap) Get(orgID string) (*OrgDatabase, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	db, ok := m.databases[orgID]
	return db, ok
}

func (m *orgDBMap) Set(orgID string, db *OrgDatabase) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.databases[orgID] = db
}

func (m *orgDBMap) Delete(orgID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.databases, orgID)
}

// --------------------------------------------------------------------------
// Bootstrap
// --------------------------------------------------------------------------

const collectionCloudSQLDBs = "_cloud_sql_databases"

func (p *plugin) ensureCollections() error {
	_, err := p.app.FindCollectionByNameOrId(collectionCloudSQLDBs)
	if err == nil {
		return nil
	}

	c := core.NewBaseCollection(collectionCloudSQLDBs)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "orgId", Required: true},
		&core.TextField{Name: "databaseName", Required: true},
		&core.TextField{Name: "host", Required: true},
		&core.NumberField{Name: "port", Required: true},
		&core.TextField{Name: "pgUser", Required: true},
		&core.TextField{Name: "pgPassword"},
		&core.TextField{Name: "sslMode"},
		&core.TextField{Name: "cloudSqlProjectId"},
		&core.TextField{Name: "cloudSqlBranchId"},
		&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"provisioning", "ready", "error", "deleting"},
		},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	p.app.Logger().Info("creating cloud sql system collection", "name", collectionCloudSQLDBs)
	return p.app.Save(c)
}

// --------------------------------------------------------------------------
// Routes
// --------------------------------------------------------------------------

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	// Every address below answers about one org's database, and a database is
	// reached with the password that opens it — so every request has to name
	// an org, and the only org it may name is the one its credential carries.
	// The rule is stated once per group, so a route added to either group
	// inherits it, and each handler asks the same question again for the value.
	api := r.Group("/v1/cloud-sql")
	api.BindFunc(actsInAnOrg)

	api.POST("/databases", p.handleCreateDatabase)
	api.GET("/databases", p.handleListDatabases)
	api.GET("/databases/{id}", p.handleGetDatabase)
	api.DELETE("/databases/{id}", p.handleDeleteDatabase)
	api.GET("/databases/{id}/connection", p.handleGetConnection)

	api.POST("/databases/{id}/branches", p.handleCreateBranch)
	api.GET("/databases/{id}/branches", p.handleListBranches)

	// Register specific methods to avoid conflict with the static catch-all GET /{path...}
	meta := r.Group("/v1/meta")
	meta.BindFunc(actsInAnOrg)
	meta.GET("/{path...}", p.handleMetaProxy)
	meta.POST("/{path...}", p.handleMetaProxy)
	meta.PUT("/{path...}", p.handleMetaProxy)
	meta.PATCH("/{path...}", p.handleMetaProxy)
	meta.DELETE("/{path...}", p.handleMetaProxy)
}

// orgOf is the organization a request acts in: the one the door resolved from
// the verified credential and published on the event, whichever door read it —
// an IAM token over JWKS, or an IAM key. A request carrying no credential
// resolves no org, and no org is not every org.
//
// It is never read off the request. X-Org-Id states what a caller MEANT; the
// door has already answered with what the credential SAYS, and that is the
// answer a database is handed out on.
func orgOf(e *core.RequestEvent) (string, error) {
	if org, _ := e.Get(apis.RequestEventKeyOrg).(string); org != "" {
		return org, nil
	}

	return "", e.UnauthorizedError("The request requires a credential that carries an organization.", nil)
}

// actsInAnOrg refuses a request that carries no organization, before it reaches
// a handler.
func actsInAnOrg(e *core.RequestEvent) error {
	if _, err := orgOf(e); err != nil {
		return err
	}

	return e.Next()
}

// database is the row {id} names, read for the org the credential acts in. A
// row another org owns is refused rather than described, so an id is worth
// nothing to a caller who is not in the org that holds it.
func (p *plugin) database(e *core.RequestEvent) (*core.Record, error) {
	org, err := orgOf(e)
	if err != nil {
		return nil, err
	}

	record, err := p.app.FindRecordById(collectionCloudSQLDBs, e.Request.PathValue("id"))
	if err != nil {
		return nil, e.NotFoundError("database not found", err)
	}

	if record.GetString("orgId") != org {
		return nil, e.ForbiddenError("The credential does not act in the requested organization.", nil)
	}

	return record, nil
}

// --------------------------------------------------------------------------
// Database provisioning
// --------------------------------------------------------------------------

func (p *plugin) handleCreateDatabase(e *core.RequestEvent) error {
	org, err := orgOf(e)
	if err != nil {
		return err
	}

	var body struct {
		OrgID        string `json:"orgId"`
		DatabaseName string `json:"databaseName"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	// A request may state the org it means, and stating another one is refused
	// rather than quietly filed under the caller's own.
	if body.OrgID != "" && body.OrgID != org {
		return e.ForbiddenError("The credential does not act in the requested organization.", nil)
	}
	if body.DatabaseName == "" {
		return e.BadRequestError("databaseName is required", nil)
	}

	existing, _ := p.app.FindFirstRecordByData(collectionCloudSQLDBs, "orgId", org)
	if existing != nil {
		return e.BadRequestError("base already has a database", nil)
	}

	dbName := "t_" + sanitizeDBName(body.DatabaseName)
	if err := p.createDatabase(dbName); err != nil {
		p.app.Logger().Error("failed to create Cloud SQL database",
			"orgId", org,
			"error", err.Error(),
		)
		return e.InternalServerError("failed to provision database", err)
	}

	col, err := p.app.FindCollectionByNameOrId(collectionCloudSQLDBs)
	if err != nil {
		return e.InternalServerError("_cloud_sql_databases collection not found", err)
	}

	record := core.NewRecord(col)
	record.Set("orgId", org)
	record.Set("databaseName", dbName)
	record.Set("host", p.config.ComputeHost)
	record.Set("port", p.config.ComputePort)
	record.Set("pgUser", p.config.DefaultPGUser)
	record.Set("pgPassword", p.config.DefaultPGPass)
	record.Set("sslMode", "disable")
	record.Set("status", "ready")

	if err := p.app.Save(record); err != nil {
		return e.InternalServerError("failed to save database record", err)
	}

	tdb := &OrgDatabase{
		OrgID:        org,
		DatabaseName: dbName,
		Host:         p.config.ComputeHost,
		Port:         p.config.ComputePort,
		User:         p.config.DefaultPGUser,
		Password:     p.config.DefaultPGPass,
		SSLMode:      "disable",
	}
	p.orgDB.Set(org, tdb)

	return e.JSON(http.StatusCreated, map[string]any{
		"id":               record.Id,
		"orgId":            org,
		"databaseName":     dbName,
		"host":             p.config.ProxyHost,
		"port":             p.config.ProxyPort,
		"status":           "ready",
		"connectionString": tdb.ConnectionString(),
	})
}

func (p *plugin) handleListDatabases(e *core.RequestEvent) error {
	org, err := orgOf(e)
	if err != nil {
		return err
	}

	records, err := p.app.FindRecordsByFilter(
		collectionCloudSQLDBs, "orgId = {:org}", "", 0, 0, dbx.Params{"org": org})
	if err != nil {
		return e.InternalServerError("failed to list databases", err)
	}

	result := make([]map[string]any, 0, len(records))
	for _, r := range records {
		result = append(result, map[string]any{
			"id":           r.Id,
			"orgId":        r.GetString("orgId"),
			"databaseName": r.GetString("databaseName"),
			"host":         r.GetString("host"),
			"port":         r.Get("port"),
			"status":       r.GetString("status"),
		})
	}
	return e.JSON(http.StatusOK, result)
}

func (p *plugin) handleGetDatabase(e *core.RequestEvent) error {
	record, err := p.database(e)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{
		"id":           record.Id,
		"orgId":        record.GetString("orgId"),
		"databaseName": record.GetString("databaseName"),
		"host":         record.GetString("host"),
		"port":         record.Get("port"),
		"status":       record.GetString("status"),
	})
}

func (p *plugin) handleDeleteDatabase(e *core.RequestEvent) error {
	record, err := p.database(e)
	if err != nil {
		return err
	}

	dbName := record.GetString("databaseName")
	orgID := record.GetString("orgId")

	if err := p.dropDatabase(dbName); err != nil {
		p.app.Logger().Warn("failed to drop Cloud SQL database",
			"databaseName", dbName,
			"error", err.Error(),
		)
	}

	if err := p.app.Delete(record); err != nil {
		return e.InternalServerError("failed to delete database record", err)
	}

	p.orgDB.Delete(orgID)
	return e.JSON(http.StatusOK, map[string]bool{"deleted": true})
}

// handleGetConnection hands back the string that opens the database, password
// and all. It reaches a caller the org owning that database resolved to, and
// nobody else.
func (p *plugin) handleGetConnection(e *core.RequestEvent) error {
	record, err := p.database(e)
	if err != nil {
		return err
	}

	tdb := &OrgDatabase{
		OrgID:        record.GetString("orgId"),
		DatabaseName: record.GetString("databaseName"),
		Host:         record.GetString("host"),
		Port:         int(record.GetFloat("port")),
		User:         record.GetString("pgUser"),
		Password:     record.GetString("pgPassword"),
		SSLMode:      record.GetString("sslMode"),
	}

	return e.JSON(http.StatusOK, map[string]any{
		"connectionString": tdb.ConnectionString(),
		"host":             tdb.Host,
		"port":             tdb.Port,
		"database":         tdb.DatabaseName,
		"user":             tdb.User,
	})
}

// --------------------------------------------------------------------------
// Branches
// --------------------------------------------------------------------------

func (p *plugin) handleCreateBranch(e *core.RequestEvent) error {
	if _, err := p.database(e); err != nil {
		return err
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if body.Name == "" {
		body.Name = "preview"
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"branch":  body.Name,
		"status":  "created",
		"message": "Branch provisioning coming in Phase 4",
	})
}

func (p *plugin) handleListBranches(e *core.RequestEvent) error {
	if _, err := p.database(e); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, []map[string]any{
		{"name": "main", "status": "ready", "primary": true},
	})
}

// --------------------------------------------------------------------------
// postgres-meta proxy
// --------------------------------------------------------------------------

// handleMetaProxy forwards a schema request to postgres-meta on the connection
// that opens ONE database: the one belonging to the org the credential carries.
// Every statement postgres-meta runs therefore runs inside that database.
//
// An org with no database of its own gets that answer. There is no wider
// connection to fall back to — the admin connection reaches every database on
// the server, so lending it to a request is lending the whole server.
func (p *plugin) handleMetaProxy(e *core.RequestEvent) error {
	org, err := orgOf(e)
	if err != nil {
		return err
	}

	tdb, ok := p.orgDB.Get(org)
	if !ok {
		record, err := p.app.FindFirstRecordByData(collectionCloudSQLDBs, "orgId", org)
		if err != nil {
			return e.NotFoundError("no database found for this organization", err)
		}
		tdb = &OrgDatabase{
			OrgID:        org,
			DatabaseName: record.GetString("databaseName"),
			Host:         record.GetString("host"),
			Port:         int(record.GetFloat("port")),
			User:         record.GetString("pgUser"),
			Password:     record.GetString("pgPassword"),
			SSLMode:      record.GetString("sslMode"),
		}
		p.orgDB.Set(org, tdb)
	}

	e.Request.Header.Set("X-Connection-Encrypted", tdb.ConnectionString())

	path := e.Request.PathValue("path")
	e.Request.URL.Path = "/" + path

	p.metaProxy.ServeHTTP(e.Response, e.Request)
	return nil
}

// --------------------------------------------------------------------------
// Cloud SQL database operations
// --------------------------------------------------------------------------

func (p *plugin) createDatabase(dbName string) error {
	query := fmt.Sprintf("CREATE DATABASE %q", dbName)
	return p.executeMetaQuery(query)
}

func (p *plugin) dropDatabase(dbName string) error {
	query := fmt.Sprintf("DROP DATABASE IF EXISTS %q", dbName)
	return p.executeMetaQuery(query)
}

func (p *plugin) executeMetaQuery(sql string) error {
	metaURL := strings.TrimRight(p.config.MetaURL, "/")
	reqBody := fmt.Sprintf(`{"query":"%s"}`, strings.ReplaceAll(sql, `"`, `\"`))

	req, err := http.NewRequest("POST", metaURL+"/query", strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("cloudsql: create meta request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudsql: meta request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("cloudsql: meta query returned %d", resp.StatusCode)
	}
	return nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func sanitizeDBName(name string) string {
	name = strings.ToLower(name)
	var sb strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			sb.WriteRune(ch)
		} else if ch == '-' {
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	if result == "" {
		result = "default"
	}
	return result
}
