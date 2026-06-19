package bootnode

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/auth"
	"github.com/hanzoai/base/plugins/bootnode/models"
)

// handleCreateProject creates a project owned by the authenticated IAM user and
// scoped to their org. Ports POST /projects from bootnode/api/auth_keys.py,
// minus the Python's client-supplied owner_id (owner is the authenticated
// user, never the request body).
func (p *plugin) handleCreateProject(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		return e.BadRequestError("name is required", nil)
	}

	col, err := p.app.FindCollectionByNameOrId(models.Projects)
	if err != nil {
		return e.InternalServerError("projects collection not found", err)
	}
	org := id.Org
	if org == "" {
		org = "hanzo"
	}
	rec := core.NewRecord(col)
	rec.Set("name", body.Name)
	rec.Set("description", body.Description)
	rec.Set("ownerId", id.UserID)
	rec.Set("orgId", org)
	rec.Set("settings", map[string]any{})
	if err := p.app.Save(rec); err != nil {
		return e.InternalServerError("failed to create project", err)
	}

	return e.JSON(http.StatusCreated, projectJSON(rec))
}

// handleGetProject returns a project by id, scoped to the caller's org.
func (p *plugin) handleGetProject(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	rec, err := p.app.FindRecordById(models.Projects, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("project not found", err)
	}
	if !ownsProject(id, rec) {
		return e.ForbiddenError("access denied", nil)
	}
	return e.JSON(http.StatusOK, projectJSON(rec))
}

// handleCreateAPIKey mints a bootnode project key (bn_…). The raw key is
// returned exactly once; only its salted hash is stored. Ports POST /keys.
func (p *plugin) handleCreateAPIKey(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}

	var body struct {
		ProjectID         string   `json:"projectId"`
		Name              string   `json:"name"`
		RateLimit         int      `json:"rateLimit"`
		ComputeUnitsLimit int      `json:"computeUnitsLimit"`
		AllowedOrigins    []string `json:"allowedOrigins"`
		AllowedChains     []string `json:"allowedChains"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if strings.TrimSpace(body.Name) == "" || body.ProjectID == "" {
		return e.BadRequestError("projectId and name are required", nil)
	}

	project, err := p.app.FindRecordById(models.Projects, body.ProjectID)
	if err != nil {
		return e.NotFoundError("project not found", err)
	}
	if !ownsProject(id, project) {
		return e.ForbiddenError("access denied", nil)
	}

	rawKey, hash, prefix, err := auth.GenerateKey(p.config.APIKeySalt)
	if err != nil {
		return e.InternalServerError("failed to generate key", err)
	}

	col, err := p.app.FindCollectionByNameOrId(models.APIKeys)
	if err != nil {
		return e.InternalServerError("api keys collection not found", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", project.Id)
	rec.Set("name", body.Name)
	rec.Set("keyHash", hash)
	rec.Set("keyPrefix", prefix)
	rec.Set("rateLimit", defaultInt(body.RateLimit, 100))
	rec.Set("computeUnitsLimit", defaultInt(body.ComputeUnitsLimit, 1000))
	rec.Set("allowedOrigins", body.AllowedOrigins)
	rec.Set("allowedChains", body.AllowedChains)
	rec.Set("isActive", true)
	if err := p.app.Save(rec); err != nil {
		return e.InternalServerError("failed to save key", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"id":                rec.Id,
		"name":              rec.GetString("name"),
		"key":               rawKey, // shown once
		"keyPrefix":         rec.GetString("keyPrefix"),
		"rateLimit":         rec.GetInt("rateLimit"),
		"computeUnitsLimit": rec.GetInt("computeUnitsLimit"),
		"created":           rec.GetString("created"),
	})
}

// handleListAPIKeys lists a project's keys (never the raw key). Ports GET /keys.
func (p *plugin) handleListAPIKeys(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	projectID := e.Request.URL.Query().Get("projectId")
	if projectID == "" {
		return e.BadRequestError("projectId query parameter is required", nil)
	}
	project, err := p.app.FindRecordById(models.Projects, projectID)
	if err != nil {
		return e.NotFoundError("project not found", err)
	}
	if !ownsProject(id, project) {
		return e.ForbiddenError("access denied", nil)
	}

	keys, err := p.app.FindRecordsByFilter(
		models.APIKeys,
		"project = {:project}",
		"-created", 0, 0,
		map[string]any{"project": project.Id},
	)
	if err != nil {
		return e.InternalServerError("failed to list keys", err)
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"id":                k.Id,
			"name":              k.GetString("name"),
			"keyPrefix":         k.GetString("keyPrefix"),
			"rateLimit":         k.GetInt("rateLimit"),
			"computeUnitsLimit": k.GetInt("computeUnitsLimit"),
			"isActive":          k.GetBool("isActive"),
			"lastUsedAt":        k.GetString("lastUsedAt"),
			"created":           k.GetString("created"),
		})
	}
	return e.JSON(http.StatusOK, out)
}

// handleDeleteAPIKey deactivates a key (soft delete, matching the Python).
func (p *plugin) handleDeleteAPIKey(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	key, err := p.app.FindRecordById(models.APIKeys, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("API key not found", err)
	}
	project, err := p.app.FindRecordById(models.Projects, key.GetString("project"))
	if err != nil || !ownsProject(id, project) {
		return e.ForbiddenError("access denied", nil)
	}
	key.Set("isActive", false)
	if err := p.app.Save(key); err != nil {
		return e.InternalServerError("failed to deactivate key", err)
	}
	return e.JSON(http.StatusOK, map[string]any{"status": "deleted", "id": key.Id})
}

// requireProject resolves the project a request acts within. It accepts a
// bootnode bn_ key (resolved by hash → owning project) OR an authenticated
// user (whose first project is used). This is the Go equivalent of the
// Python deps.py get_project_from_key. Returns a 404 when no project is found.
func (p *plugin) requireProject(e *core.RequestEvent) (*core.Record, error) {
	cred, _ := bearerToken(e.Request)
	if auth.ClassifyCredential(cred, false) == auth.KeyBootnode {
		hash := auth.HashKey(cred, p.config.APIKeySalt)
		key, err := p.app.FindFirstRecordByData(models.APIKeys, "keyHash", hash)
		if err != nil {
			return nil, e.UnauthorizedError("invalid API key", nil)
		}
		if !key.GetBool("isActive") {
			return nil, e.UnauthorizedError("API key is inactive", nil)
		}
		project, err := p.app.FindRecordById(models.Projects, key.GetString("project"))
		if err != nil {
			return nil, e.NotFoundError("project not found for key", err)
		}
		return project, nil
	}

	// Fall back to the authenticated user's first project.
	id, err := p.requireUser(e)
	if err != nil {
		return nil, err
	}
	projects, err := p.app.FindRecordsByFilter(
		models.Projects,
		"ownerId = {:owner}",
		"created", 1, 0,
		map[string]any{"owner": id.UserID},
	)
	if err != nil || len(projects) == 0 {
		return nil, e.NotFoundError("No project found. Create a project first.", nil)
	}
	return projects[0], nil
}

// ownsProject reports whether the identity may act on the project. Ownership
// is by IAM user id; org-scoped access is granted when the project's org
// matches the caller's org (covers org-shared projects).
func ownsProject(id *Identity, project *core.Record) bool {
	if project == nil {
		return false
	}
	if project.GetString("ownerId") == id.UserID {
		return true
	}
	return id.Org != "" && project.GetString("orgId") == id.Org
}

func projectJSON(rec *core.Record) map[string]any {
	return map[string]any{
		"id":          rec.Id,
		"name":        rec.GetString("name"),
		"description": rec.GetString("description"),
		"orgId":       rec.GetString("orgId"),
		"ownerId":     rec.GetString("ownerId"),
		"created":     rec.GetString("created"),
	}
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
