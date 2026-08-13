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
	open map[string]*entry
}

// entry is one org's Base and the promise that it is open.
//
// The lock this registry needs is the one over its map. Opening a Base is a
// migration run on a fresh file, by far the longest thing here, so it happens
// outside that lock: the entry goes in first, the open follows, and everyone
// naming that org waits on ready while no other org waits at all.
type entry struct {
	app   core.App
	err   error
	ready chan struct{}
}

func newBases(p *plugin) *bases {
	return &bases{p: p, open: make(map[string]*entry)}
}

// base opens the Base that serves org, and is the one lookup that does.
func (b *bases) base(org string) (core.App, error) {
	if org == authz.AdminOrg {
		return b.p.app, nil
	}
	if err := validateSlug(org); err != nil {
		return nil, fmt.Errorf("org %q: %w", org, err)
	}

	e, mine := b.claim(org)
	if mine {
		b.fill(org, e)
	}

	<-e.ready
	if e.err != nil {
		return nil, e.err
	}
	return e.app, nil
}

// claim finds org's entry or puts a fresh one in, and reports whether this
// caller is the one that put it there and so owes it an open. Everyone else
// waits on the entry, which is what keeps one org's first request off every
// other org's path.
func (b *bases) claim(org string) (*entry, bool) {
	b.mu.RLock()
	e, ok := b.open[org]
	b.mu.RUnlock()
	if ok {
		return e, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if e, ok := b.open[org]; ok { // put there while we swapped the lock
		return e, false
	}
	e = &entry{ready: make(chan struct{})}
	b.open[org] = e

	return e, true
}

// fill opens org's Base and publishes the outcome to whoever is waiting on it.
//
// ready is never closed without one: err is set before the work so an open that
// unwinds still answers. An entry that did not end in a Base is dropped, so the
// next request opens it afresh rather than being told the same thing for the
// life of the process.
func (b *bases) fill(org string, e *entry) {
	e.err = fmt.Errorf("open the Base for %q stopped", org)

	defer func() {
		close(e.ready)
		if e.err == nil {
			return
		}
		b.mu.Lock()
		if b.open[org] == e {
			delete(b.open, org)
		}
		b.mu.Unlock()
	}()

	dir, err := b.p.orgDB.ProvisionOrg(org)
	if err != nil {
		e.err = err
		return
	}

	connect, err := encryptedConnect(b.p.orgDB, org)
	if err != nil {
		e.err = err
		return
	}

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       dir,
		EncryptionEnv: b.p.app.EncryptionEnv(),
		IsDev:         b.p.app.IsDev(),
		DBConnect:     connect,
	})
	if err := app.Bootstrap(); err != nil {
		e.err = fmt.Errorf("open the Base for %q: %w", org, err)
		return
	}
	b.p.declare(app)

	e.app, e.err = app, nil
	b.p.app.Logger().Info("base: opened", "org", org, "dir", dir)
}

// close releases every Base this process opened. A Base holds two SQLite
// handles, so leaving them to the exit means the last writes of a graceful
// shutdown land in a WAL nobody checkpoints.
//
// The entries come out of the map first: an open still in flight is waited for
// rather than skipped, and waiting under the lock would meet the open trying to
// take it.
func (b *bases) close() {
	b.mu.Lock()
	open := b.open
	b.open = make(map[string]*entry)
	b.mu.Unlock()

	for org, e := range open {
		<-e.ready
		if e.app == nil {
			continue
		}
		if err := e.app.ResetBootstrapState(); err != nil {
			b.p.app.Logger().Error("base: failed to close", "org", org, "error", err)
		}
	}
}
