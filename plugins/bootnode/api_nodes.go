package bootnode

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/kube"
)

// nodePresets maps a simple-mode preset to a fleet configuration. The Go port
// targets bootno.de/v1 NodeFleet CRs (operator-reconciled) rather than the
// Python's direct Docker container orchestration — fleets run in the cluster,
// not on the API host.
var nodePresets = map[string]map[string]any{
	"rpc": {
		"executionClient": "geth", "consensusClient": "", "syncMode": "light",
		"enableMEV": false, "enableValidator": false,
	},
	"full": {
		"executionClient": "geth", "consensusClient": "lighthouse", "syncMode": "snap",
		"enableMEV": false, "enableValidator": false,
	},
	"staking": {
		"executionClient": "geth", "consensusClient": "lighthouse", "syncMode": "snap",
		"enableMEV": true, "enableValidator": true,
	},
	"archive": {
		"executionClient": "erigon", "consensusClient": "lighthouse", "syncMode": "archive",
		"enableMEV": false, "enableValidator": false,
	},
}

// handleCreateNodeFleet applies a bootno.de/v1 NodeFleet custom resource. Ports
// POST /nodes — the CRD-driven, production path (the Python's docker provider
// is a local-dev concern outside Base's remit; cloud provisioning was always
// the intended production target).
func (p *plugin) handleCreateNodeFleet(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	if !p.kube.Available() {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "no Kubernetes cluster configured for node provisioning",
		})
	}

	var body struct {
		Name            string `json:"name"`
		Chain           string `json:"chain"`
		Network         string `json:"network"`
		Mode            string `json:"mode"`
		Preset          string `json:"preset"`
		Replicas        int    `json:"replicas"`
		ExecutionClient string `json:"executionClient"`
		ConsensusClient string `json:"consensusClient"`
		SyncMode        string `json:"syncMode"`
		EnableMEV       bool   `json:"enableMev"`
		EnableValidator bool   `json:"enableValidator"`
		FeeRecipient    string `json:"feeRecipient"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	body.Name = strings.TrimSpace(strings.ToLower(body.Name))
	if !isCRName(body.Name) {
		return e.BadRequestError("name must be a lowercase DNS-1123 label", nil)
	}
	if body.Chain == "" {
		return e.BadRequestError("chain is required", nil)
	}
	network := body.Network
	if network == "" {
		network = "mainnet"
	}

	// Resolve preset for simple mode (Go port of the Python preset expansion).
	execClient, consClient, syncMode := body.ExecutionClient, body.ConsensusClient, body.SyncMode
	enableMEV, enableValidator := body.EnableMEV, body.EnableValidator
	if body.Mode == "simple" && body.Preset != "" {
		preset, ok := nodePresets[body.Preset]
		if !ok {
			return e.BadRequestError("unknown preset (rpc, full, staking, archive)", nil)
		}
		execClient, _ = preset["executionClient"].(string)
		consClient, _ = preset["consensusClient"].(string)
		syncMode, _ = preset["syncMode"].(string)
		enableMEV, _ = preset["enableMEV"].(bool)
		enableValidator, _ = preset["enableValidator"].(bool)
	}
	if execClient == "" {
		execClient = "geth"
	}
	if syncMode == "" {
		syncMode = "snap"
	}
	replicas := body.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	spec := map[string]any{
		"chain":           strings.ToLower(body.Chain),
		"network":         strings.ToLower(network),
		"replicas":        replicas,
		"executionClient": execClient,
		"consensusClient": consClient,
		"syncMode":        syncMode,
		"enableMev":       enableMEV,
		"enableValidator": enableValidator,
		"createdBy":       id.UserID,
		"org":             id.Org,
	}
	if body.FeeRecipient != "" {
		spec["feeRecipient"] = body.FeeRecipient
	}

	if _, err := p.kube.Apply(e.Request.Context(), kube.NodeFleetGVR, body.Name, orgLabels(id.Org), spec); err != nil {
		return e.InternalServerError("failed to apply NodeFleet resource", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"id":              body.Name,
		"name":            body.Name,
		"chain":           spec["chain"],
		"network":         spec["network"],
		"replicas":        replicas,
		"executionClient": execClient,
		"status":          "provisioning",
		"namespace":       p.kube.Namespace(),
	})
}

// handleListNodeFleets lists NodeFleet CRs for the caller's org. Ports
// GET /nodes.
func (p *plugin) handleListNodeFleets(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	return p.listCRs(e, kube.NodeFleetGVR, id.Org)
}

// handleGetNodeFleet returns one NodeFleet CR. Ports GET /nodes/{id}.
func (p *plugin) handleGetNodeFleet(e *core.RequestEvent) error {
	if _, err := p.requireUser(e); err != nil {
		return err
	}
	return p.getCR(e, kube.NodeFleetGVR, e.Request.PathValue("id"))
}

// handleDeleteNodeFleet tears down a NodeFleet CR. Ports DELETE /nodes/{id}.
func (p *plugin) handleDeleteNodeFleet(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	name := e.Request.PathValue("id")
	if err := p.kube.Delete(e.Request.Context(), kube.NodeFleetGVR, name); err != nil {
		return e.InternalServerError("failed to delete NodeFleet resource", err)
	}
	return e.JSON(http.StatusOK, map[string]any{"status": "deleted", "id": name})
}
