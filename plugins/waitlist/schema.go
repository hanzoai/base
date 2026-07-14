// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

// ensureSchema idempotently creates (and upgrades) the collections that back
// the plugin: `waitlists`, `waitlist_entries`, `waitlist_events`, then seeds
// any configured default waitlists. Safe on every boot — an existing
// pre-points list gains the points/breakdown fields and the points rank index
// without losing data.
func (p *plugin) ensureSchema() error {
	if _, err := p.ensureWaitlistsCollection(); err != nil {
		return fmt.Errorf("waitlist: ensure waitlists collection: %w", err)
	}
	entries, err := p.ensureEntriesCollection()
	if err != nil {
		return fmt.Errorf("waitlist: ensure entries collection: %w", err)
	}
	if _, err := p.ensureEventsCollection(entries); err != nil {
		return fmt.Errorf("waitlist: ensure events collection: %w", err)
	}
	if err := p.ensureDefaultWaitlists(); err != nil {
		return fmt.Errorf("waitlist: seed default waitlists: %w", err)
	}
	return nil
}

func (p *plugin) ensureWaitlistsCollection() (*core.Collection, error) {
	name := p.config.waitlistsCollection()
	if c, err := p.app.FindCollectionByNameOrId(name); err == nil {
		return c, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	c := core.NewBaseCollection(name)
	c.Fields.Add(
		&core.TextField{Name: "slug", Required: true, Min: 1, Max: 100, Pattern: `^[a-z0-9][a-z0-9-]*$`, Presentable: true},
		&core.TextField{Name: "name", Required: true, Min: 1, Max: 200},
		&core.AutodateField{Name: "createdAt", OnCreate: true},
		&core.AutodateField{Name: "updatedAt", OnCreate: true, OnUpdate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_slug` ON `%s` (`slug`)", name, name),
	}
	if err := p.app.Save(c); err != nil {
		return nil, err
	}
	p.logger.Info("waitlist: created collection", "name", name)
	return c, nil
}

func (p *plugin) ensureEntriesCollection() (*core.Collection, error) {
	name := p.config.entriesCollection()
	parent, err := p.ensureWaitlistsCollection()
	if err != nil {
		return nil, err
	}

	c, findErr := p.app.FindCollectionByNameOrId(name)
	if findErr != nil && !errors.Is(findErr, sql.ErrNoRows) {
		return nil, findErr
	}
	if c == nil {
		c = core.NewBaseCollection(name)
		c.Fields.Add(
			&core.RelationField{Name: "waitlist", CollectionId: parent.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
			&core.EmailField{Name: "email", Required: true},
			&core.TextField{Name: "refCode", Required: true, Min: 4, Max: 32, Pattern: `^[A-Za-z0-9]+$`, Presentable: true},
			&core.TextField{Name: "referredBy", Max: 32, Pattern: `^[A-Za-z0-9]*$`},
			&core.NumberField{Name: "referralCount"},
		)
	}
	// Reconcile the position + access fields (present on new collections; added
	// in place on an existing pre-points list). points is the single number
	// position derives from; breakdown holds the per-source subtotals the
	// status view renders; accessGranted is the sticky access grant (once true,
	// never revoked by rank drift).
	if c.Fields.GetByName("points") == nil {
		c.Fields.Add(&core.NumberField{Name: "points"})
	}
	if c.Fields.GetByName("breakdown") == nil {
		c.Fields.Add(&core.JSONField{Name: "breakdown", MaxSize: 100_000})
	}
	if c.Fields.GetByName("accessGranted") == nil {
		c.Fields.Add(&core.BoolField{Name: "accessGranted"})
	}
	if c.Fields.GetByName("createdAt") == nil {
		c.Fields.Add(&core.AutodateField{Name: "createdAt", OnCreate: true})
	}
	// The one index that makes rank a bounded COUNT and the neighborhood a
	// keyset seek: points DESC (leaderboard order is a forward scan), createdAt
	// ASC (earlier joiners win ties). waitlist leads so every query is scoped.
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_wl_email` ON `%s` (`waitlist`, `email` COLLATE NOCASE)", name, name),
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_wl_refCode` ON `%s` (`waitlist`, `refCode` COLLATE NOCASE)", name, name),
		fmt.Sprintf("CREATE INDEX `idx_%s_rank` ON `%s` (`waitlist`, `points` DESC, `createdAt` ASC)", name, name),
	}
	if err := p.app.Save(c); err != nil {
		return nil, err
	}
	if findErr != nil {
		p.logger.Info("waitlist: created collection", "name", name)
	}
	return c, nil
}

// ensureEventsCollection creates the append-only points ledger. Every award is
// a row here; UNIQUE(entry, dedupKey) is the dedup / anti-fraud guarantee, and
// the (waitlist, createdAt) index powers the activity feed.
func (p *plugin) ensureEventsCollection(entries *core.Collection) (*core.Collection, error) {
	name := p.config.eventsCollection()
	if c, err := p.app.FindCollectionByNameOrId(name); err == nil {
		return c, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	parent, err := p.ensureWaitlistsCollection()
	if err != nil {
		return nil, err
	}

	c := core.NewBaseCollection(name)
	c.Fields.Add(
		&core.RelationField{Name: "entry", CollectionId: entries.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.RelationField{Name: "waitlist", CollectionId: parent.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		// source is the open-ended event kind: "referral", "share:x",
		// "invite_sent", "invite_converted", "social:x:follow",
		// "social:discord:join", "hanzod:run", "join", "grant". A new earn
		// channel is a new source string — never a schema change.
		&core.TextField{Name: "source", Required: true, Min: 1, Max: 100, Presentable: true},
		&core.NumberField{Name: "points"},
		// dedupKey scopes idempotency to (entry, key): "referral:<email>",
		// "share:x:2026-07-08", "social:x:follow", "invite:<email>". Empty key
		// = never deduped (e.g. an explicit admin grant or accumulator boost).
		&core.TextField{Name: "dedupKey", Max: 200},
		&core.JSONField{Name: "meta", MaxSize: 20_000},
		&core.AutodateField{Name: "createdAt", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		// The anti-fraud spine: one (entry, dedupKey) can be awarded once. A
		// partial index so empty-key events (grants) are never blocked.
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_dedup` ON `%s` (`entry`, `dedupKey`) WHERE `dedupKey` != ''", name, name),
		fmt.Sprintf("CREATE INDEX `idx_%s_feed` ON `%s` (`waitlist`, `createdAt` DESC)", name, name),
		fmt.Sprintf("CREATE INDEX `idx_%s_entry` ON `%s` (`entry`)", name, name),
	}
	if err := p.app.Save(c); err != nil {
		return nil, err
	}
	p.logger.Info("waitlist: created collection", "name", name)
	return c, nil
}

// ensureDefaultWaitlists seeds cfg.DefaultSlugs as waitlist rows. Each slug
// that has no row yet gets one (slug + Title-cased name). Idempotent.
func (p *plugin) ensureDefaultWaitlists() error {
	if len(p.config.DefaultSlugs) == 0 {
		return nil
	}
	col, err := p.app.FindCollectionByNameOrId(p.config.waitlistsCollection())
	if err != nil {
		return err
	}
	for _, slug := range p.config.DefaultSlugs {
		if _, err := p.app.FindFirstRecordByData(p.config.waitlistsCollection(), "slug", slug); err == nil {
			continue // already exists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rec := core.NewRecord(col)
		rec.Set("slug", slug)
		rec.Set("name", titleSlug(slug))
		if err := p.app.Save(rec); err != nil {
			return err
		}
		p.logger.Info("waitlist: seeded default waitlist", "slug", slug)
	}
	return nil
}
