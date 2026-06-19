package bootnode

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/kube"
)

// Network tiers. Resource defaults are resolved server-side; the request only
// names the tier.
const (
	tierStarter    = "starter"
	tierPro        = "pro"
	tierEnterprise = "enterprise"
)

// tierDefaults returns the replica/validator allocation for a tier. This is the
// Go port of the Python _tier_defaults — but it feeds a Network CR spec for the
// operator to reconcile, rather than templating raw Kubernetes manifests.
func tierDefaults(tier string) (web, api, validators int) {
	switch tier {
	case tierPro:
		return 2, 3, 3
	case tierEnterprise:
		return 3, 5, 5
	default: // starter
		return 2, 0, 0
	}
}

// handleCreateNetwork applies a bootno.de/v1 Network custom resource for a
// 1-click white-labeled network. Ports POST /networks. Unlike the Python (which
// shelled out to kubectl with nginx-annotated Ingress manifests), this submits
// a declarative Network CR; the bootno.de operator owns the actual rollout
// (ingress, TLS, CORS, IAM app, validator fleet).
func (p *plugin) handleCreateNetwork(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	if !p.kube.Available() {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "no Kubernetes cluster configured for network provisioning",
		})
	}

	var body struct {
		Name             string `json:"name"`
		Tier             string `json:"tier"`
		Region           string `json:"region"`
		ChainID          *int   `json:"chainId"`
		DeployValidators bool   `json:"deployValidators"`
		ValidatorCount   int    `json:"validatorCount"`
		Brand            struct {
			Name         string `json:"name"`
			Domain       string `json:"domain"`
			LogoURL      string `json:"logoUrl"`
			PrimaryColor string `json:"primaryColor"`
			AccentColor  string `json:"accentColor"`
		} `json:"brand"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	body.Name = strings.TrimSpace(strings.ToLower(body.Name))
	if !isCRName(body.Name) {
		return e.BadRequestError("name must be a lowercase DNS-1123 label (a-z, 0-9, -)", nil)
	}
	if body.Brand.Domain == "" {
		return e.BadRequestError("brand.domain is required", nil)
	}
	tier := body.Tier
	if tier == "" {
		tier = tierStarter
	}
	web, api, validators := tierDefaults(tier)
	if !body.DeployValidators {
		validators = 0
	} else if body.ValidatorCount > 0 {
		validators = body.ValidatorCount
	}
	region := body.Region
	if region == "" {
		region = "sfo3"
	}

	spec := map[string]any{
		"tier":   tier,
		"region": region,
		"brand": map[string]any{
			"name":         orDefault(body.Brand.Name, body.Name),
			"domain":       body.Brand.Domain,
			"logoUrl":      body.Brand.LogoURL,
			"primaryColor": orDefault(body.Brand.PrimaryColor, "#000000"),
			"accentColor":  orDefault(body.Brand.AccentColor, "#fd4444"),
		},
		"iam": map[string]any{
			"org":      body.Name,
			"domain":   body.Name + ".id",
			"clientId": body.Name + "-cloud",
		},
		"replicas": map[string]any{
			"web": web,
			"api": api,
		},
		"validators": map[string]any{
			"enabled": body.DeployValidators,
			"count":   validators,
		},
		"urls": map[string]any{
			"cloud": "https://cloud." + body.Brand.Domain,
			"api":   "https://api.cloud." + body.Brand.Domain,
			"ws":    "wss://ws.cloud." + body.Brand.Domain,
			"rpc":   "https://api.cloud." + body.Brand.Domain + "/v1/rpc/" + body.Name,
		},
		"createdBy": id.UserID,
		"org":       id.Org,
	}
	if body.ChainID != nil {
		spec["chainId"] = *body.ChainID
	}

	if _, err := p.kube.Apply(e.Request.Context(), kube.NetworkGVR, body.Name, orgLabels(id.Org), spec); err != nil {
		return e.InternalServerError("failed to apply Network resource", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"id":        body.Name,
		"name":      body.Name,
		"tier":      tier,
		"region":    region,
		"status":    "provisioning",
		"namespace": p.kube.Namespace(),
		"cloudUrl":  spec["urls"].(map[string]any)["cloud"],
		"apiUrl":    spec["urls"].(map[string]any)["api"],
	})
}

// handleListNetworks lists Network CRs scoped to the caller's org. Ports
// GET /networks. The cluster is the source of truth.
func (p *plugin) handleListNetworks(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	return p.listCRs(e, kube.NetworkGVR, id.Org)
}

// handleGetNetwork returns one Network CR. Ports GET /networks/{id}.
func (p *plugin) handleGetNetwork(e *core.RequestEvent) error {
	if _, err := p.requireUser(e); err != nil {
		return err
	}
	return p.getCR(e, kube.NetworkGVR, e.Request.PathValue("id"))
}

// handleDeleteNetwork tears down a Network CR. Ports DELETE /networks/{id}.
func (p *plugin) handleDeleteNetwork(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	name := e.Request.PathValue("id")
	if err := p.kube.Delete(e.Request.Context(), kube.NetworkGVR, name); err != nil {
		return e.InternalServerError("failed to delete Network resource", err)
	}
	return e.JSON(http.StatusOK, map[string]any{"status": "deleted", "id": name})
}

// --- shared CR list/get helpers (used by networks and nodes) ---

// listCRs lists custom resources of a kind, filtered to org via label selector.
func (p *plugin) listCRs(e *core.RequestEvent, gvr kube.GVR, org string) error {
	if !p.kube.Available() {
		return e.JSON(http.StatusOK, []any{}) // no cluster → empty list, not an error
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	// A list is a GET on the plural with no name; reuse Get with empty name by
	// fetching the collection endpoint.
	found, err := p.kube.Get(e.Request.Context(), gvr, "", &list)
	if err != nil {
		return e.InternalServerError(fmt.Sprintf("failed to list %s", gvr.Kind), err)
	}
	if !found {
		return e.JSON(http.StatusOK, []any{})
	}
	out := make([]map[string]any, 0, len(list.Items))
	for _, item := range list.Items {
		summary := crSummary(item)
		if org == "" || summary["org"] == org || summary["org"] == nil {
			out = append(out, summary)
		}
	}
	return e.JSON(http.StatusOK, out)
}

// getCR fetches one custom resource by name.
func (p *plugin) getCR(e *core.RequestEvent, gvr kube.GVR, name string) error {
	if !p.kube.Available() {
		return e.NotFoundError(gvr.Kind+" not found", nil)
	}
	var obj map[string]any
	found, err := p.kube.Get(e.Request.Context(), gvr, name, &obj)
	if err != nil {
		return e.InternalServerError("failed to fetch "+gvr.Kind, err)
	}
	if !found {
		return e.NotFoundError(gvr.Kind+" not found", nil)
	}
	return e.JSON(http.StatusOK, obj)
}

// crSummary flattens a CR into a compact API response.
func crSummary(item map[string]any) map[string]any {
	meta, _ := item["metadata"].(map[string]any)
	spec, _ := item["spec"].(map[string]any)
	status, _ := item["status"].(map[string]any)
	out := map[string]any{}
	if meta != nil {
		out["id"] = meta["name"]
		out["name"] = meta["name"]
	}
	if spec != nil {
		out["org"] = spec["org"]
		out["tier"] = spec["tier"]
		out["region"] = spec["region"]
		out["chain"] = spec["chain"]
	}
	if status != nil {
		out["status"] = status["phase"]
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// isCRName validates a DNS-1123 label suitable for a Kubernetes object name.
func isCRName(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
