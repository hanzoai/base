package bootnode

import (
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/kube"
)

// handleCreateKMSKey provisions a managed key by applying a bootno.de/v1
// KMSSecret custom resource. Ports the key-provisioning surface of
// bootnode/api/keys.
//
// CRITICAL: no plaintext key material ever touches this service. The KMSSecret
// CR only NAMES a secret and the KMS path it should be synced from; the
// bootno.de operator (with KMS credentials) materializes the secret into the
// cluster. The request body carries no private key, and the response never
// returns one.
func (p *plugin) handleCreateKMSKey(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	if !p.kube.Available() {
		return e.JSON(http.StatusServiceUnavailable, map[string]any{
			"error": "no Kubernetes cluster configured for key provisioning",
		})
	}

	var body struct {
		Name    string `json:"name"`
		Chain   string `json:"chain"`
		KMSPath string `json:"kmsPath"`
		KeyType string `json:"keyType"` // secp256k1 | ed25519 | bls
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	// Defensive: reject any attempt to smuggle key material through this API.
	if rejectPlaintextKeyFields(e) {
		return e.BadRequestError("private key material must never be sent — keys are sourced from KMS by path", nil)
	}
	body.Name = strings.TrimSpace(strings.ToLower(body.Name))
	if !isCRName(body.Name) {
		return e.BadRequestError("name must be a lowercase DNS-1123 label", nil)
	}
	if body.KMSPath == "" {
		return e.BadRequestError("kmsPath is required (the KMS path to sync the secret from)", nil)
	}
	keyType := body.KeyType
	if keyType == "" {
		keyType = "secp256k1"
	}

	spec := map[string]any{
		// kmsPath is a reference, not a secret. The operator reads it with its
		// own KMS credentials and syncs the material into a k8s Secret.
		"kmsPath":   body.KMSPath,
		"chain":     strings.ToLower(body.Chain),
		"keyType":   keyType,
		"createdBy": id.UserID,
		"org":       id.Org,
	}

	if _, err := p.kube.Apply(e.Request.Context(), kube.KMSSecretGVR, body.Name, orgLabels(id.Org), spec); err != nil {
		return e.InternalServerError("failed to apply KMSSecret resource", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"id":      body.Name,
		"name":    body.Name,
		"chain":   spec["chain"],
		"keyType": keyType,
		"status":  "syncing",
		// No key material in the response — by design.
	})
}

// handleGetKMSKey returns a KMSSecret CR's status (never its material). Ports
// the key-read surface of bootnode/api/keys.
func (p *plugin) handleGetKMSKey(e *core.RequestEvent) error {
	if _, err := p.requireUser(e); err != nil {
		return err
	}
	name := e.Request.PathValue("name")
	if !p.kube.Available() {
		return e.NotFoundError("key not found", nil)
	}
	var obj map[string]any
	found, err := p.kube.Get(e.Request.Context(), kube.KMSSecretGVR, name, &obj)
	if err != nil {
		return e.InternalServerError("failed to fetch key", err)
	}
	if !found {
		return e.NotFoundError("key not found", nil)
	}
	// Return only metadata + status, never spec material references in full.
	return e.JSON(http.StatusOK, crSummary(obj))
}

// handleKeyStatus reports whether the caller's project has a managed key
// configured. Ports GET /keys/status from bootnode/api/keys/__init__.py, which
// the staking UI calls to decide whether an MPC wallet exists.
func (p *plugin) handleKeyStatus(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}

	configured := false
	if p.kube.Available() {
		var list struct {
			Items []map[string]any `json:"items"`
		}
		if found, err := p.kube.Get(e.Request.Context(), kube.KMSSecretGVR, "", &list); err == nil && found {
			for _, item := range list.Items {
				if spec, _ := item["spec"].(map[string]any); spec != nil {
					if id.Org == "" || spec["org"] == id.Org {
						configured = true
						break
					}
				}
			}
		}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"configured":  configured,
		"mpcEnabled":  true,
		"tfheEnabled": true,
	})
}

// rejectPlaintextKeyFields returns true if the raw request body contains any
// field name that looks like it carries private key material. A defense in
// depth against a misbehaving client: this API only accepts KMS path
// references, never secrets.
func rejectPlaintextKeyFields(e *core.RequestEvent) bool {
	info, err := e.RequestInfo()
	if err != nil || info == nil {
		return false
	}
	for k := range info.Body {
		lk := strings.ToLower(k)
		switch lk {
		case "privatekey", "private_key", "secret", "secretkey", "secret_key", "mnemonic", "seed", "privkey":
			return true
		}
	}
	return false
}
