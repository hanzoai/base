package apis

import (
	"database/sql"
	"errors"
	"os"

	"github.com/fatih/color"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/osutils"
	"github.com/hanzoai/dbx"
)

// DefaultInstallerFunc tells an operator how to create the first superuser.
//
// It only runs where IAM is NOT the auth source — with IAM on, loadInstaller
// returns before this and the superuser is whoever IAM issues an admin-claim
// token to.
//
// It used to open a browser at `{baseURL}/_/#/baseinstal/{token}` and print
// that address. There is no such page: the admin was rewritten and serves no
// `baseinstal` route, so the link a first-boot operator was handed went
// nowhere, and the CLI line printed under it as a fallback was the only thing
// that worked. Now it is the instruction rather than the fallback.
func DefaultInstallerFunc(app core.App, systemSuperuser *core.Record, baseURL string) error {
	color.Magenta("\n(!) No superuser exists yet. Create the first one with:")
	color.New(color.Bold).Add(color.FgCyan).Printf("    %s superuser upsert EMAIL PASS\n\n", executablePath())

	return nil
}

func loadInstaller(
	app core.App,
	baseURL string,
	installerFunc func(app core.App, systemSuperuser *core.Record, baseURL string) error,
) error {
	// IAM is the only auth source — once the platform plugin marks the
	// store as external-only, the local "first superuser" installer is
	// moot. The superuser is whoever IAM issues an admin-claim token to;
	// Base never seeds one locally.
	if externalOnly, _ := app.Store().Get(StoreKeyExternalAuthOnly).(bool); externalOnly {
		return nil
	}

	if installerFunc == nil || !needInstallerSuperuser(app) {
		return nil
	}

	superuser, err := findOrCreateInstallerSuperuser(app)
	if err != nil {
		return err
	}

	return installerFunc(app, superuser, baseURL)
}

func needInstallerSuperuser(app core.App) bool {
	total, err := app.CountRecords(core.CollectionNameSuperusers, dbx.Not(dbx.HashExp{
		"email": core.DefaultInstallerEmail,
	}))

	return err == nil && total == 0
}

func findOrCreateInstallerSuperuser(app core.App) (*core.Record, error) {
	col, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return nil, err
	}

	record, err := app.FindAuthRecordByEmail(col, core.DefaultInstallerEmail)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		record = core.NewRecord(col)
		record.SetEmail(core.DefaultInstallerEmail)
		record.SetVerified(true)

		err = app.Save(record)
		if err != nil {
			return nil, err
		}
	}

	return record, nil
}

func executablePath() string {
	if osutils.IsProbablyGoRun() {
		return "go run ."
	}

	return os.Args[0]
}
