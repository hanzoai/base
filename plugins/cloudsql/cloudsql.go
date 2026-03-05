// Package cloudsql implements Hanzo Cloud SQL — a serverless PostgreSQL
// integration plugin for Hanzo Base. It manages per-tenant database
// provisioning, connection routing, and proxies schema management requests
// to postgres-meta.
//
// Hanzo Cloud SQL is the scalable, multi-tenant PostgreSQL layer backed by
// Neon's open-source storage/compute separation. "Hanzo SQL" (sql.hanzo.svc)
// is the shared single-instance PostgreSQL; "Hanzo Cloud SQL" is the
// auto-scaling, per-tenant serverless variant.
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
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
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
		app:    app,
		config: config,
		metaProxy: httputil.NewSingleHostReverseProxy(metaURL),
		tenantDB: &tenantDBMap{
			databases: make(map[string]*TenantDatabase),
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
	tenantDB  *tenantDBMap
}

// TenantDatabase holds Cloud SQL database info for a tenant.
type TenantDatabase struct {
	TenantID     string `json:"tenantId"`
	DatabaseName string `json:"databaseName"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Password     string `json:"password"`
	SSLMode      string `json:"sslMode"`
}

// ConnectionString returns a PostgreSQL connection string.
func (t *TenantDatabase) ConnectionString() string {
	sslMode := t.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		t.User, t.Password, t.Host, t.Port, t.DatabaseName, sslMode)
}

type tenantDBMap struct {
	mu        sync.RWMutex
	databases map[string]*TenantDatabase
}

func (m *tenantDBMap) Get(tenantID string) (*TenantDatabase, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	db, ok := m.databases[tenantID]
	return db, ok
}

func (m *tenantDBMap) Set(tenantID string, db *TenantDatabase) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.databases[tenantID] = db
}

func (m *tenantDBMap) Delete(tenantID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.databases, tenantID)
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
		&core.TextField{Name: "tenantId", Required: true},
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

	p.app.Logger().Info("creating cloud sql system collection", slog.String("name", collectionCloudSQLDBs))
	return p.app.Save(c)
}

// --------------------------------------------------------------------------
// Routes
// --------------------------------------------------------------------------

func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	api := r.Group("/api/cloud-sql")

	api.POST("/databases", p.handleCreateDatabase)
	api.GET("/databases", p.handleListDatabases)
	api.GET("/databases/{id}", p.handleGetDatabase)
	api.DELETE("/databases/{id}", p.handleDeleteDatabase)
	api.GET("/databases/{id}/connection", p.handleGetConnection)

	api.POST("/databases/{id}/branches", p.handleCreateBranch)
	api.GET("/databases/{id}/branches", p.handleListBranches)

	// Register specific methods to avoid conflict with the static catch-all GET /{path...}
	meta := r.Group("/api/meta")
	meta.GET("/{path...}", p.handleMetaProxy)
	meta.POST("/{path...}", p.handleMetaProxy)
	meta.PUT("/{path...}", p.handleMetaProxy)
	meta.PATCH("/{path...}", p.handleMetaProxy)
	meta.DELETE("/{path...}", p.handleMetaProxy)
}

// --------------------------------------------------------------------------
// Database provisioning
// --------------------------------------------------------------------------

func (p *plugin) handleCreateDatabase(e *core.RequestEvent) error {
	authHeader := e.Request.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return e.UnauthorizedError("missing authorization", nil)
	}

	var body struct {
		TenantID     string `json:"tenantId"`
		DatabaseName string `json:"databaseName"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if body.TenantID == "" || body.DatabaseName == "" {
		return e.BadRequestError("tenantId and databaseName are required", nil)
	}

	existing, _ := p.app.FindFirstRecordByData(collectionCloudSQLDBs, "tenantId", body.TenantID)
	if existing != nil {
		return e.BadRequestError("tenant already has a database", nil)
	}

	dbName := "t_" + sanitizeDBName(body.DatabaseName)
	if err := p.createDatabase(dbName); err != nil {
		p.app.Logger().Error("failed to create Cloud SQL database",
			slog.String("tenantId", body.TenantID),
			slog.String("error", err.Error()),
		)
		return e.InternalServerError("failed to provision database", err)
	}

	col, err := p.app.FindCollectionByNameOrId(collectionCloudSQLDBs)
	if err != nil {
		return e.InternalServerError("_cloud_sql_databases collection not found", err)
	}

	record := core.NewRecord(col)
	record.Set("tenantId", body.TenantID)
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

	tdb := &TenantDatabase{
		TenantID:     body.TenantID,
		DatabaseName: dbName,
		Host:         p.config.ComputeHost,
		Port:         p.config.ComputePort,
		User:         p.config.DefaultPGUser,
		Password:     p.config.DefaultPGPass,
		SSLMode:      "disable",
	}
	p.tenantDB.Set(body.TenantID, tdb)

	return e.JSON(http.StatusCreated, map[string]any{
		"id":               record.Id,
		"tenantId":         body.TenantID,
		"databaseName":     dbName,
		"host":             p.config.ProxyHost,
		"port":             p.config.ProxyPort,
		"status":           "ready",
		"connectionString": tdb.ConnectionString(),
	})
}

func (p *plugin) handleListDatabases(e *core.RequestEvent) error {
	records, err := p.app.FindRecordsByFilter(collectionCloudSQLDBs, "", "", 0, 0, nil)
	if err != nil {
		return e.InternalServerError("failed to list databases", err)
	}

	result := make([]map[string]any, 0, len(records))
	for _, r := range records {
		result = append(result, map[string]any{
			"id":           r.Id,
			"tenantId":     r.GetString("tenantId"),
			"databaseName": r.GetString("databaseName"),
			"host":         r.GetString("host"),
			"port":         r.Get("port"),
			"status":       r.GetString("status"),
		})
	}
	return e.JSON(http.StatusOK, result)
}

func (p *plugin) handleGetDatabase(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	record, err := p.app.FindRecordById(collectionCloudSQLDBs, id)
	if err != nil {
		return e.NotFoundError("database not found", err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"id":           record.Id,
		"tenantId":     record.GetString("tenantId"),
		"databaseName": record.GetString("databaseName"),
		"host":         record.GetString("host"),
		"port":         record.Get("port"),
		"status":       record.GetString("status"),
	})
}

func (p *plugin) handleDeleteDatabase(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	record, err := p.app.FindRecordById(collectionCloudSQLDBs, id)
	if err != nil {
		return e.NotFoundError("database not found", err)
	}

	dbName := record.GetString("databaseName")
	tenantID := record.GetString("tenantId")

	if err := p.dropDatabase(dbName); err != nil {
		p.app.Logger().Warn("failed to drop Cloud SQL database",
			slog.String("databaseName", dbName),
			slog.String("error", err.Error()),
		)
	}

	if err := p.app.Delete(record); err != nil {
		return e.InternalServerError("failed to delete database record", err)
	}

	p.tenantDB.Delete(tenantID)
	return e.JSON(http.StatusOK, map[string]bool{"deleted": true})
}

func (p *plugin) handleGetConnection(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	record, err := p.app.FindRecordById(collectionCloudSQLDBs, id)
	if err != nil {
		return e.NotFoundError("database not found", err)
	}

	tdb := &TenantDatabase{
		TenantID:     record.GetString("tenantId"),
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
	id := e.Request.PathValue("id")
	if _, err := p.app.FindRecordById(collectionCloudSQLDBs, id); err != nil {
		return e.NotFoundError("database not found", err)
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
	id := e.Request.PathValue("id")
	if _, err := p.app.FindRecordById(collectionCloudSQLDBs, id); err != nil {
		return e.NotFoundError("database not found", err)
	}
	return e.JSON(http.StatusOK, []map[string]any{
		{"name": "main", "status": "ready", "primary": true},
	})
}

// --------------------------------------------------------------------------
// postgres-meta proxy
// --------------------------------------------------------------------------

func (p *plugin) handleMetaProxy(e *core.RequestEvent) error {
	tenantID := e.Request.Header.Get("X-Tenant-ID")

	var connStr string
	if tenantID != "" {
		tdb, ok := p.tenantDB.Get(tenantID)
		if !ok {
			record, err := p.app.FindFirstRecordByData(collectionCloudSQLDBs, "tenantId", tenantID)
			if err != nil {
				return e.NotFoundError("no database found for tenant", err)
			}
			tdb = &TenantDatabase{
				TenantID:     tenantID,
				DatabaseName: record.GetString("databaseName"),
				Host:         record.GetString("host"),
				Port:         int(record.GetFloat("port")),
				User:         record.GetString("pgUser"),
				Password:     record.GetString("pgPassword"),
				SSLMode:      record.GetString("sslMode"),
			}
			p.tenantDB.Set(tenantID, tdb)
		}
		connStr = tdb.ConnectionString()
	} else {
		connStr = fmt.Sprintf("postgres://%s:%s@%s:%d/postgres?sslmode=disable",
			p.config.DefaultPGUser, p.config.DefaultPGPass,
			p.config.ComputeHost, p.config.ComputePort)
	}

	e.Request.Header.Set("X-Connection-Encrypted", connStr)

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
