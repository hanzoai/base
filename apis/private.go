package apis

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/dbx"
)

// bindPrivateApi exposes the /private endpoint — a blind, per-user encrypted
// blob store. The server never inspects the ciphertext and never derives keys.
//
//	PUT    /private/{tag}   upsert ciphertext (body = raw bytes)
//	GET    /private/{tag}   fetch ciphertext
//	GET    /private         list tags (user_id, tag, size, updated_at)
//	DELETE /private/{tag}   hard delete
//
// The client is responsible for encryption (recommended: luxfi/age, X25519 or
// X-Wing PQ-hybrid). The server has no key material and cannot decrypt under
// any circumstances — that is the point.
//
// Per-tag cap (default 1 MiB) and per-user cap (default 100 MiB) are
// configurable via BASE_PRIVATE_MAX_TAG_BYTES and BASE_PRIVATE_MAX_USER_BYTES.
func bindPrivateApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/private").Bind(RequireAuth())
	sub.GET("", privateList)
	sub.GET("/{tag}", privateGet)
	sub.PUT("/{tag}", privatePut).Unbind(DefaultBodyLimitMiddlewareId)
	sub.DELETE("/{tag}", privateDelete)
}

const (
	maxTagBytesDefault  int64 = 1 << 20   // 1 MiB
	maxUserBytesDefault int64 = 100 << 20 // 100 MiB
)

// privateTable is a plain table rather than a Base collection, so its rows stay
// invisible to the admin UI and the CRUD endpoints. Named once because six
// queries reference it.
const privateTable = "_private"

func ensurePrivateTable(app core.App) error {
	// Idempotent. Stored in a plain table (not a Base collection) so the rows
	// stay invisible to the admin UI and the CRUD endpoints.
	_, err := app.DB().NewQuery(`
		CREATE TABLE IF NOT EXISTS _private (
			user_id    TEXT NOT NULL,
			tag        TEXT NOT NULL,
			ct         BLOB NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, tag)
		) WITHOUT ROWID
	`).Execute()
	return err
}

func validPrivateTag(tag string) bool {
	if tag == "" || len(tag) > 128 || strings.Contains(tag, "..") {
		return false
	}
	// Tags are client-chosen labels. Restrict to a safe charset so we never
	// have to worry about path-encoding bugs in logs or exports.
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

func privateEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func privateUserBytes(db dbx.Builder, userID string) (int64, error) {
	var total int64
	err := db.Select("COALESCE(SUM(LENGTH(ct)), 0)").
		From(privateTable).
		Where(dbx.HashExp{"user_id": userID}).
		Row(&total)
	return total, err
}

func privatePut(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("Authentication required.", nil)
	}
	tag := e.Request.PathValue("tag")
	if !validPrivateTag(tag) {
		return e.BadRequestError("Invalid tag.", nil)
	}
	if err := ensurePrivateTable(e.App); err != nil {
		return e.InternalServerError("Private store unavailable.", err)
	}

	maxTag := privateEnvInt64("BASE_PRIVATE_MAX_TAG_BYTES", maxTagBytesDefault)
	maxUser := privateEnvInt64("BASE_PRIVATE_MAX_USER_BYTES", maxUserBytesDefault)

	// Read body with a hard cap. Anything beyond maxTag is 413 — we never
	// buffer past the limit so a hostile client can't exhaust memory.
	body, err := io.ReadAll(io.LimitReader(e.Request.Body, maxTag+1))
	if err != nil {
		return e.BadRequestError("Failed to read body.", err)
	}
	if int64(len(body)) > maxTag {
		return e.Error(http.StatusRequestEntityTooLarge, "Ciphertext exceeds per-tag limit.", nil)
	}
	if len(body) == 0 {
		return e.BadRequestError("Empty ciphertext not allowed — use DELETE to remove a tag.", nil)
	}

	// Quota check — subtract the old row size so overwrites don't double count.
	userID := e.Auth.Id
	var oldSize int64
	_ = e.App.DB().Select("LENGTH(ct)").
		From(privateTable).
		Where(dbx.HashExp{"user_id": userID, "tag": tag}).
		Row(&oldSize)

	total, err := privateUserBytes(e.App.DB(), userID)
	if err != nil {
		return e.InternalServerError("Quota check failed.", err)
	}
	if total-oldSize+int64(len(body)) > maxUser {
		return e.Error(http.StatusInsufficientStorage, "Per-user quota exceeded.", nil)
	}

	now := time.Now().UnixMilli()
	// Upsert, keyed on the (user_id, tag) primary key. This was hand-written
	// ON CONFLICT SQL because SqliteBuilder.Upsert used to answer "not
	// supported" — it was simply an unimplemented override, and dbx v1.17.2
	// implements it. The builder emits the same statement.
	_, err = e.App.DB().Upsert(privateTable, dbx.Params{
		"user_id":    userID,
		"tag":        tag,
		"ct":         body,
		"updated_at": now,
	}, "user_id", "tag").Execute()
	if err != nil {
		return e.InternalServerError("Write failed.", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"tag":        tag,
		"size":       len(body),
		"updated_at": now,
	})
}

func privateGet(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("Authentication required.", nil)
	}
	tag := e.Request.PathValue("tag")
	if !validPrivateTag(tag) {
		return e.BadRequestError("Invalid tag.", nil)
	}
	if err := ensurePrivateTable(e.App); err != nil {
		return e.InternalServerError("Private store unavailable.", err)
	}

	var row struct {
		CT        []byte `db:"ct"`
		UpdatedAt int64  `db:"updated_at"`
	}
	err := e.App.DB().Select("ct", "updated_at").
		From(privateTable).
		Where(dbx.HashExp{"user_id": e.Auth.Id, "tag": tag}).
		One(&row)
	if err != nil {
		// Indistinguishable from not-authorized — don't leak existence.
		return e.NotFoundError("", nil)
	}

	// Raw ciphertext. Client decrypts. Server can't.
	e.Response.Header().Set("Content-Type", "application/octet-stream")
	e.Response.Header().Set("Cache-Control", "no-store")
	e.Response.Header().Set("X-Content-Type-Options", "nosniff")
	e.Response.Header().Set("X-Private-Updated-At", strconv.FormatInt(row.UpdatedAt, 10))
	_, err = e.Response.Write(row.CT)
	return err
}

// privateListItem is the per-tag metadata row returned by GET /private.
type privateListItem struct {
	Tag       string `db:"tag" json:"tag"`
	Size      int64  `db:"size" json:"size"`
	UpdatedAt int64  `db:"updated_at" json:"updated_at"`
}

func privateList(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("Authentication required.", nil)
	}
	if err := ensurePrivateTable(e.App); err != nil {
		return e.InternalServerError("Private store unavailable.", err)
	}

	rows := []privateListItem{}
	err := e.App.DB().Select("tag", "LENGTH(ct) AS size", "updated_at").
		From(privateTable).
		Where(dbx.HashExp{"user_id": e.Auth.Id}).
		OrderBy("tag").
		All(&rows)
	if err != nil {
		return e.InternalServerError("List failed.", err)
	}

	return e.JSON(http.StatusOK, map[string]any{"items": rows})
}

func privateDelete(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("Authentication required.", nil)
	}
	tag := e.Request.PathValue("tag")
	if !validPrivateTag(tag) {
		return e.BadRequestError("Invalid tag.", nil)
	}
	_, err := e.App.DB().Delete(privateTable,
		dbx.HashExp{"user_id": e.Auth.Id, "tag": tag},
	).Execute()
	if err != nil {
		return e.InternalServerError("Delete failed.", err)
	}
	return e.NoContent(http.StatusNoContent)
}
