// Command baselocal is a disposable LOCAL Base server for manual admin-UI
// testing. It deliberately does NOT register the platform/IAM plugin, so the
// store stays in "standard" (non-external-only) mode and locally-minted
// _superusers tokens authenticate the /v1 admin API.
//
// Base has removed the PocketBase local-password surface (no `superuser` CLI,
// no password field on _superusers, no auth-with-password route) in favor of
// Hanzo IAM. For local testing we therefore seed a superuser record and mint a
// long-lived static auth token via the core API, writing it to
// <DATA_DIR>/superuser.token for the operator to use as `Authorization: Bearer`.
package main

import (
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/hanzoai/base"
	"github.com/hanzoai/base/core"
)

const (
	superuserEmail = "z@base.test"
	tokenTTL       = 720 * time.Hour // 30 days — stable for a manual test session
)

func main() {
	app := base.New()

	// Seed the superuser + mint a token once the DB/migrations are ready.
	// OnServe fires after apis.Serve runs RunAllMigrations, so _superusers exists.
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		rec, err := upsertSuperuser(e.App, superuserEmail)
		if err != nil {
			return err
		}

		token, err := rec.NewStaticAuthToken(tokenTTL)
		if err != nil {
			return err
		}

		tokenPath := filepath.Join(e.App.DataDir(), "superuser.token")
		if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
			return err
		}

		log.Printf("baselocal: superuser %q ready (id=%s); static token (30d) written to %s", superuserEmail, rec.Id, tokenPath)
		log.Printf("baselocal: SUPERUSER_TOKEN=%s", token)

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// upsertSuperuser finds or creates the _superusers record for email.
// _superusers has no password field in Base (IAM-native), so there is no
// password to set — the record's presence is enough to mint auth tokens.
func upsertSuperuser(app core.App, email string) (*core.Record, error) {
	col, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return nil, err
	}

	rec, err := app.FindAuthRecordByEmail(col, email)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		rec = core.NewRecord(col)
		rec.SetEmail(email)
		rec.SetVerified(true)
		if err := app.Save(rec); err != nil {
			return nil, err
		}
	}

	return rec, nil
}
