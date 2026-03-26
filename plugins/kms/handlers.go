// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kms

import (
	"encoding/json"
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// registerRoutes registers the KMS REST API routes.
func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	api := r.Group("/api/kms")

	api.POST("/secrets", p.handleCreateSecret)
	api.GET("/secrets/{key}", p.handleGetSecret)
	api.DELETE("/secrets/{key}", p.handleDeleteSecret)
	api.GET("/secrets", p.handleListSecrets)
	api.POST("/unlock", p.handleUnlock)
	api.POST("/lock", p.handleLock)
	api.POST("/invite", p.handleInvite)
	api.POST("/sync", p.handleSync)
	api.GET("/status", p.handleStatus)
}

// requireSuperuser checks that the request has superuser authentication.
func (p *plugin) requireSuperuser(e *core.RequestEvent) error {
	if !e.HasSuperuserAuth() {
		return e.UnauthorizedError("superuser auth required", nil)
	}
	return nil
}

// --- Secret CRUD ---

type createSecretRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// handleCreateSecret encrypts and stores a secret via the MPC cluster.
// POST /api/kms/secrets
func (p *plugin) handleCreateSecret(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	var req createSecretRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if req.Key == "" || req.Value == "" {
		return e.BadRequestError("key and value are required", nil)
	}

	if err := p.client.Set(req.Key, []byte(req.Value)); err != nil {
		p.logger.Error("kms: set secret failed", "key", req.Key, "error", err)
		return e.BadRequestError("failed to store secret", err)
	}

	p.logger.Info("kms: secret created", "key", req.Key)
	return e.JSON(http.StatusCreated, map[string]string{
		"key":    req.Key,
		"status": "created",
	})
}

// handleGetSecret retrieves and decrypts a secret from the MPC cluster.
// GET /api/kms/secrets/{key}
func (p *plugin) handleGetSecret(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	key := e.Request.PathValue("key")
	if key == "" {
		return e.BadRequestError("key is required", nil)
	}

	value, err := p.client.Get(key)
	if err != nil {
		return e.NotFoundError("secret not found or decrypt failed", err)
	}

	return e.JSON(http.StatusOK, map[string]string{
		"key":   key,
		"value": string(value),
	})
}

// handleDeleteSecret removes a secret from all MPC nodes.
// DELETE /api/kms/secrets/{key}
func (p *plugin) handleDeleteSecret(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	key := e.Request.PathValue("key")
	if key == "" {
		return e.BadRequestError("key is required", nil)
	}

	if err := p.client.Delete(key); err != nil {
		p.logger.Error("kms: delete secret failed", "key", key, "error", err)
		return e.BadRequestError("failed to delete secret", err)
	}

	p.logger.Info("kms: secret deleted", "key", key)
	return e.JSON(http.StatusOK, map[string]string{
		"key":    key,
		"status": "deleted",
	})
}

// handleListSecrets returns the decrypted names of all secrets.
// GET /api/kms/secrets
func (p *plugin) handleListSecrets(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	names, err := p.client.List()
	if err != nil {
		p.logger.Error("kms: list secrets failed", "error", err)
		return e.BadRequestError("failed to list secrets", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"secrets": names,
		"total":   len(names),
	})
}

// --- Lock/Unlock ---

type unlockRequest struct {
	Passphrase string `json:"passphrase"`
}

// handleUnlock derives the CEK from a passphrase and stores it in memory.
// POST /api/kms/unlock
func (p *plugin) handleUnlock(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	var req unlockRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if req.Passphrase == "" {
		return e.BadRequestError("passphrase is required", nil)
	}

	if err := p.client.Unlock(req.Passphrase); err != nil {
		return e.BadRequestError("unlock failed", err)
	}

	p.logger.Info("kms: unlocked")
	return e.JSON(http.StatusOK, map[string]string{"status": "unlocked"})
}

// handleLock zeros the CEK from memory.
// POST /api/kms/lock
func (p *plugin) handleLock(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	p.client.Lock()

	p.logger.Info("kms: locked")
	return e.JSON(http.StatusOK, map[string]string{"status": "locked"})
}

// --- Member Management ---

type inviteRequest struct {
	MemberPubKey []byte `json:"member_pub_key"`
}

// handleInvite wraps the CEK for a new member's HPKE public key.
// POST /api/kms/invite
func (p *plugin) handleInvite(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	var req inviteRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if len(req.MemberPubKey) == 0 {
		return e.BadRequestError("member_pub_key is required", nil)
	}

	wrapped, err := p.client.InviteMember(req.MemberPubKey)
	if err != nil {
		return e.BadRequestError("invite failed", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"status":      "invited",
		"wrapped_cek": json.RawMessage(wrapped),
	})
}

// --- Sync ---

// handleSync triggers CRDT sync across all MPC nodes.
// POST /api/kms/sync
func (p *plugin) handleSync(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	if err := p.client.Sync(); err != nil {
		return e.BadRequestError("sync failed", err)
	}

	return e.JSON(http.StatusOK, map[string]string{"status": "synced"})
}

// --- Status ---

// handleStatus returns the health status of all MPC nodes.
// GET /api/kms/status
func (p *plugin) handleStatus(e *core.RequestEvent) error {
	if err := p.requireSuperuser(e); err != nil {
		return err
	}

	statuses, err := p.client.Status()
	if err != nil {
		return e.BadRequestError("status check failed", err)
	}

	healthy := 0
	for _, s := range statuses {
		if s.Healthy {
			healthy++
		}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"nodes":     statuses,
		"healthy":   healthy,
		"total":     len(statuses),
		"threshold": p.config.Threshold,
		"unlocked":  p.client.IsUnlocked(),
	})
}
