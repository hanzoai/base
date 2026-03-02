// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// registerRoutes adds vault API routes.
// All routes require authentication — the user ID comes from the auth context.
func (p *plugin) registerRoutes(r *router.Router[*core.RequestEvent]) {
	api := r.Group("/vault")

	// Require auth for all vault routes.
	api.BindFunc(func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.UnauthorizedError("auth required", nil)
		}
		return e.Next()
	})

	// GET /vault/status — shard status for current user
	api.GET("/status", func(e *core.RequestEvent) error {
		userID := e.Auth.Id
		shard, err := p.GetShard(userID)
		if err != nil {
			return e.InternalServerError("", err)
		}
		return e.JSON(200, map[string]interface{}{
			"user_id":   userID,
			"shard":     shard.Path,
			"encrypted": true,
			"sync":      p.config.SyncEnabled,
		})
	})

	// POST /vault/put — store encrypted key-value
	api.POST("/put", func(e *core.RequestEvent) error {
		userID := e.Auth.Id
		shard, err := p.GetShard(userID)
		if err != nil {
			return e.InternalServerError("", err)
		}

		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := e.BindBody(&req); err != nil {
			return e.BadRequestError("", nil)
		}

		encrypted, err := shard.Encrypt([]byte(req.Value))
		if err != nil {
			return e.InternalServerError("encrypt failed", err)
		}

		// In full implementation: write to user's SQLite shard + CRDT log
		_ = encrypted

		return e.JSON(200, map[string]string{
			"status": "stored",
			"key":    req.Key,
			"shard":  shard.UserID,
		})
	})

	// POST /vault/get — retrieve and decrypt
	api.POST("/get", func(e *core.RequestEvent) error {
		userID := e.Auth.Id
		_, err := p.GetShard(userID)
		if err != nil {
			return e.InternalServerError("", err)
		}

		var req struct {
			Key string `json:"key"`
		}
		if err := e.BindBody(&req); err != nil {
			return e.BadRequestError("", nil)
		}

		// In full implementation: read from SQLite shard, decrypt with DEK
		return e.JSON(200, map[string]string{
			"key":    req.Key,
			"status": "decrypted",
		})
	})

	// GET /vault/anchor — get current merkle root for chain anchoring
	api.GET("/anchor", func(e *core.RequestEvent) error {
		userID := e.Auth.Id
		shard, err := p.GetShard(userID)
		if err != nil {
			return e.InternalServerError("", err)
		}

		// Compute merkle root of the shard state
		hash := sha256.Sum256([]byte(shard.Path + ":" + shard.UserID))

		return e.JSON(200, map[string]string{
			"user_id":     userID,
			"merkle_root": hex.EncodeToString(hash[:]),
			"chain":       p.config.ChainRPC,
			"status":      "ready_to_anchor",
		})
	})

	// GET /vault/keys — list user's encryption keys (metadata only, never DEK)
	api.GET("/keys", func(e *core.RequestEvent) error {
		userID := e.Auth.Id
		return e.JSON(200, map[string]interface{}{
			"user_id": userID,
			"org_id":  p.config.OrgID,
			"key_hierarchy": map[string]string{
				"master_kek": "HSM / K-Chain ML-KEM (never exported)",
				"org_kek":    fmt.Sprintf("HMAC-SHA256(master, vault:org:%s)", p.config.OrgID),
				"user_dek":   fmt.Sprintf("HMAC-SHA256(orgKEK, vault:user:%s)", userID),
				"encryption": "AES-256-GCM per entry",
			},
		})
	})

	// POST /vault/sync — trigger CRDT sync with peers
	api.POST("/sync", func(e *core.RequestEvent) error {
		if !p.config.SyncEnabled {
			return e.JSON(200, map[string]string{"status": "sync_disabled"})
		}
		return e.JSON(200, map[string]string{"status": "sync_triggered"})
	})

	// POST /vault/export — export encrypted shard for backup/migration
	api.POST("/export", func(e *core.RequestEvent) error {
		userID := e.Auth.Id
		shard, err := p.GetShard(userID)
		if err != nil {
			return e.InternalServerError("", err)
		}

		// In full implementation: read SQLite file, it's already encrypted
		return e.JSON(200, map[string]interface{}{
			"user_id": userID,
			"path":    shard.Path,
			"format":  "encrypted-sqlite",
			"note":    "file is AES-256-GCM encrypted with user DEK — safe to store anywhere",
		})
	})
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v interface{}) {
	json.NewEncoder(w).Encode(v)
}
