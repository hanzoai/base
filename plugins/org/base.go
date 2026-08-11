package org

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/hanzoai/authz"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/dbx"
	"github.com/hanzoai/sqlite"
)

// encrypted returns how to open an org's Base with the org's own key, or nil to
// open it plaintext.
//
// The key is derived per org (OrgDEK), so one org's file is unreadable with
// another's key and a leaked key is worth one tenant rather than all of them.
// sqlite.OpenDB is the whole mechanism and it means the same thing under both
// builds: SQLCipher when Base is linked with cgo, and the pure-Go codec envelope
// otherwise. The two are byte-compatible, so a file written by one opens under
// the other — which matters because CI ships CGO_ENABLED=0 and an operator may
// not.
//
// An empty DEK is dev mode: no master key was configured, and the Base opens
// plaintext exactly as it did before. A master key of the wrong length is an
// error rather than a silent downgrade, because a key that quietly becomes no
// key is how data ends up in the clear while the deployment believes otherwise.
func encryptedConnect(orgDB *OrgDB, org string) (core.DBConnectFunc, error) {
	dek, err := orgDB.OrgDEK(org)
	if err != nil {
		return nil, err
	}
	if dek == "" {
		return nil, nil
	}

	key, err := hex.DecodeString(dek)
	if err != nil {
		return nil, fmt.Errorf("platform: decode the %q key: %w", org, err)
	}

	return func(dbPath string) (*dbx.DB, error) {
		// Same refusal DefaultDBConnect makes: a build whose SQLite cannot
		// answer geoDistance dies here rather than at a failed search.
		if err := core.VerifySQLiteMathFunctions(); err != nil {
			return nil, err
		}

		db, err := sqlite.OpenDB(dbPath, key)
		if err != nil {
			return nil, err
		}

		return dbx.NewFromDB(db, "sqlite"), nil
	}, nil
}

// bases maps an org to the Base that serves it: {DataDir}/orgs/{org}, opened the
// first time a request arrives carrying that org. There is no create verb —
// using an org opens its Base.
//
// The reserved admin org's Base is the process's own. Schema, settings, backups
// and logs are process-wide and _superusers is unscoped inside the process, so a
// platform session belongs to the Base the process runs on rather than to some
// tenant's file. That Base is reached by naming the org, not by falling into it
// when nothing else matched — falling into it is what this replaces.
type bases struct {
	p *plugin

	mu   sync.RWMutex
	open map[string]core.App
}

func newBases(p *plugin) *bases {
	return &bases{p: p, open: make(map[string]core.App)}
}

// base opens the Base that serves org, and is the one lookup that does.
func (b *bases) base(org string) (core.App, error) {
	if org == authz.AdminOrg {
		return b.p.app, nil
	}
	if err := validateSlug(org); err != nil {
		return nil, fmt.Errorf("org %q: %w", org, err)
	}

	b.mu.RLock()
	app, ok := b.open[org]
	b.mu.RUnlock()
	if ok {
		return app, nil
	}

	// Cold open is serialized across orgs. Opening one Base is a migration run
	// on a fresh file and happens once per org per process; splitting the lock
	// per org buys a few milliseconds and costs a second way to hold it.
	b.mu.Lock()
	defer b.mu.Unlock()

	if app, ok := b.open[org]; ok {
		return app, nil
	}

	dir, err := b.p.orgDB.ProvisionOrg(org)
	if err != nil {
		return nil, err
	}

	connect, err := encryptedConnect(b.p.orgDB, org)
	if err != nil {
		return nil, err
	}

	app = core.NewBaseApp(core.BaseAppConfig{
		DataDir:       dir,
		EncryptionEnv: b.p.app.EncryptionEnv(),
		IsDev:         b.p.app.IsDev(),
		DBConnect:     connect,
	})
	if err := app.Bootstrap(); err != nil {
		return nil, fmt.Errorf("open the Base for %q: %w", org, err)
	}
	b.p.declare(app)

	b.open[org] = app
	b.p.app.Logger().Info("base: opened", "org", org, "dir", dir)

	return app, nil
}

// close releases every Base this process opened. A Base holds two SQLite
// handles, so leaving them to the exit means the last writes of a graceful
// shutdown land in a WAL nobody checkpoints.
func (b *bases) close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for org, app := range b.open {
		if err := app.ResetBootstrapState(); err != nil {
			b.p.app.Logger().Error("base: failed to close", "org", org, "error", err)
		}
		delete(b.open, org)
	}
}
