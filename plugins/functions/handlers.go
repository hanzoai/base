package functions

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/pocketbase/dbx"
)

// handleDeployFunction handles POST /api/functions
func (p *plugin) handleDeployFunction(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	var input struct {
		Name    string            `json:"name"`
		Image   string            `json:"image"`
		Runtime string            `json:"runtime"`
		Handler string            `json:"handler"`
		EnvVars map[string]string `json:"envVars"`
		Memory  string            `json:"memory"`
		CPU     string            `json:"cpu"`
	}
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if input.Name == "" {
		return e.BadRequestError("function name is required", nil)
	}

	tenantSlug := e.Auth.GetString("tenantSlug")
	if tenantSlug == "" {
		tenantSlug = "default"
	}

	qualifiedName := fmt.Sprintf("t-%s-%s", tenantSlug, input.Name)

	// Build deploy request with default env vars
	envVars := map[string]string{
		"IAM_URL":     "https://hanzo.id",
		"STORAGE_URL": "http://s3.hanzo.svc:9000",
	}
	for k, v := range input.EnvVars {
		envVars[k] = v
	}

	req := DeployRequest{
		Service:   qualifiedName,
		Image:     input.Image,
		Namespace: p.config.FunctionNamespace,
		Labels: map[string]string{
			"hanzo.ai/tenant":   tenantSlug,
			"hanzo.ai/function": input.Name,
		},
		EnvVars: envVars,
	}
	if input.Memory != "" || input.CPU != "" {
		req.Limits = &FunctionLimits{Memory: input.Memory, CPU: input.CPU}
	}

	if err := p.deployFunction(req); err != nil {
		return e.InternalServerError("failed to deploy function", err)
	}

	// Persist to system collection
	col, err := p.app.FindCollectionByNameOrId(collectionFunctions)
	if err != nil {
		return e.InternalServerError("functions collection not found", err)
	}

	record := core.NewRecord(col)
	record.Set("name", input.Name)
	record.Set("qualifiedName", qualifiedName)
	record.Set("tenantId", tenantSlug)
	record.Set("image", input.Image)
	record.Set("runtime", input.Runtime)
	record.Set("handler", input.Handler)
	record.Set("status", "deployed")

	if err := p.app.Save(record); err != nil {
		return e.InternalServerError("failed to save function record", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"name":          input.Name,
		"qualifiedName": qualifiedName,
		"status":        "deployed",
	})
}

// handleListFunctions handles GET /api/functions
func (p *plugin) handleListFunctions(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	tenantSlug := e.Auth.GetString("tenantSlug")
	if tenantSlug == "" {
		tenantSlug = "default"
	}

	functions, err := p.listFunctions(p.config.FunctionNamespace)
	if err != nil {
		return e.InternalServerError("failed to list functions", err)
	}

	prefix := fmt.Sprintf("t-%s-", tenantSlug)
	result := make([]map[string]any, 0)
	for _, fn := range functions {
		if strings.HasPrefix(fn.Name, prefix) {
			result = append(result, map[string]any{
				"name":              strings.TrimPrefix(fn.Name, prefix),
				"qualifiedName":     fn.Name,
				"replicas":          fn.Replicas,
				"availableReplicas": fn.AvailableReplicas,
				"invocationCount":   fn.InvocationCount,
				"image":             fn.Image,
			})
		}
	}

	return e.JSON(http.StatusOK, result)
}

// handleGetFunction handles GET /api/functions/{name}
func (p *plugin) handleGetFunction(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	name := e.Request.PathValue("name")
	tenantSlug := e.Auth.GetString("tenantSlug")
	if tenantSlug == "" {
		tenantSlug = "default"
	}

	qualifiedName := fmt.Sprintf("t-%s-%s", tenantSlug, name)

	fn, err := p.getFunction(qualifiedName, p.config.FunctionNamespace)
	if err != nil {
		return e.InternalServerError("failed to get function", err)
	}
	if fn == nil {
		return e.NotFoundError("function not found", nil)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"name":              name,
		"qualifiedName":     fn.Name,
		"replicas":          fn.Replicas,
		"availableReplicas": fn.AvailableReplicas,
		"invocationCount":   fn.InvocationCount,
		"image":             fn.Image,
		"envVars":           fn.EnvVars,
		"labels":            fn.Labels,
	})
}

// handleDeleteFunction handles DELETE /api/functions/{name}
func (p *plugin) handleDeleteFunction(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	name := e.Request.PathValue("name")
	tenantSlug := e.Auth.GetString("tenantSlug")
	if tenantSlug == "" {
		tenantSlug = "default"
	}

	qualifiedName := fmt.Sprintf("t-%s-%s", tenantSlug, name)

	if err := p.deleteFunction(qualifiedName, p.config.FunctionNamespace); err != nil {
		return e.InternalServerError("failed to delete function", err)
	}

	// Remove from system collection
	records, err := p.app.FindRecordsByFilter(
		collectionFunctions,
		"qualifiedName = {:qname}",
		"",
		1,
		0,
		dbx.Params{"qname": qualifiedName},
	)
	if err == nil && len(records) > 0 {
		_ = p.app.Delete(records[0])
	}

	return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// handleInvokeFunction handles POST /api/functions/{name}/invoke
func (p *plugin) handleInvokeFunction(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	name := e.Request.PathValue("name")
	tenantSlug := e.Auth.GetString("tenantSlug")
	if tenantSlug == "" {
		tenantSlug = "default"
	}

	qualifiedName := fmt.Sprintf("t-%s-%s", tenantSlug, name)

	payload, err := io.ReadAll(e.Request.Body)
	if err != nil {
		return e.BadRequestError("failed to read request body", err)
	}

	async := e.Request.URL.Query().Get("async") == "true"

	body, statusCode, err := p.invokeFunction(qualifiedName, payload, async)
	if err != nil {
		return e.InternalServerError("failed to invoke function", err)
	}

	if async {
		return e.JSON(http.StatusAccepted, map[string]string{"status": "queued"})
	}

	e.Response.Header().Set("Content-Type", "application/json")
	e.Response.WriteHeader(statusCode)
	_, _ = e.Response.Write(body)
	return nil
}

// handleGetLogs handles GET /api/functions/{name}/logs
func (p *plugin) handleGetLogs(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	name := e.Request.PathValue("name")
	tenantSlug := e.Auth.GetString("tenantSlug")
	if tenantSlug == "" {
		tenantSlug = "default"
	}

	qualifiedName := fmt.Sprintf("t-%s-%s", tenantSlug, name)

	// OpenFaaS CE does not have a native logs endpoint.
	// Return kubectl command for now; wire to K8s API in future.
	return e.JSON(http.StatusOK, map[string]any{
		"name":    name,
		"message": "Logs available via: kubectl logs -l faas_function=" + qualifiedName + " -n " + p.config.FunctionNamespace,
	})
}
