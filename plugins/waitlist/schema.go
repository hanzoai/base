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

// ensureSchema idempotently creates the two collections that back the
// waitlist plugin AND the default waitlist row, so /join works out of the
// box. It is safe to call on every boot.
func (p *plugin) ensureSchema() error {
	if _, err := p.ensureWaitlistsCollection(); err != nil {
		return fmt.Errorf("waitlist: ensure waitlists collection: %w", err)
	}
	if _, err := p.ensureEntriesCollection(); err != nil {
		return fmt.Errorf("waitlist: ensure entries collection: %w", err)
	}
	if err := p.ensureDefaultWaitlist(); err != nil {
		return fmt.Errorf("waitlist: ensure default waitlist: %w", err)
	}
	return nil
}

// ensureDefaultWaitlist idempotently seeds the configured default waitlist
// row (slug = Config.DefaultSlug, default "launch"). The join endpoint 404s
// on an unknown slug, so a fresh deployment is useless without at least one
// list — the plugin owns its default. An existing row with the slug is left
// untouched (a superuser may rename it via the dashboard).
func (p *plugin) ensureDefaultWaitlist() error {
	name := p.config.waitlistsCollection()
	if _, err := p.app.FindFirstRecordByData(name, "slug", p.config.DefaultSlug); err == nil {
		return nil // already present
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	col, err := p.app.FindCollectionByNameOrId(name)
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("slug", p.config.DefaultSlug)
	rec.Set("name", p.config.DefaultName)
	if err := p.app.Save(rec); err != nil {
		return err
	}
	p.logger.Info("waitlist: seeded default waitlist", "slug", p.config.DefaultSlug)
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
		&core.TextField{
			Name:        "slug",
			Required:    true,
			Min:         1,
			Max:         100,
			Pattern:     `^[a-z0-9][a-z0-9-]*$`,
			Presentable: true,
		},
		&core.TextField{
			Name:     "name",
			Required: true,
			Min:      1,
			Max:      200,
		},
		&core.AutodateField{Name: "createdAt", OnCreate: true},
		&core.AutodateField{Name: "updatedAt", OnCreate: true, OnUpdate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_slug` ON `%s` (`slug`)", name, name),
	}
	// No public CRUD — the plugin owns this collection. Superusers (admin)
	// still have full access via the dashboard.
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

	if c, err := p.app.FindCollectionByNameOrId(name); err == nil {
		return c, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	c := core.NewBaseCollection(name)
	c.Fields.Add(
		&core.RelationField{
			Name:          "waitlist",
			CollectionId:  parent.Id,
			Required:      true,
			CascadeDelete: true,
			MaxSelect:     1,
		},
		&core.EmailField{
			Name:     "email",
			Required: true,
		},
		&core.TextField{
			Name:        "refCode",
			Required:    true,
			Min:         4,
			Max:         32,
			Pattern:     `^[A-Za-z0-9]+$`,
			Presentable: true,
		},
		&core.TextField{
			Name:    "referredBy",
			Max:     32,
			Pattern: `^[A-Za-z0-9]*$`,
		},
		&core.NumberField{
			Name:     "referralCount",
			Required: false,
		},
		&core.AutodateField{Name: "createdAt", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_waitlist_email` ON `%s` (`waitlist`, `email` COLLATE NOCASE)", name, name),
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_waitlist_refCode` ON `%s` (`waitlist`, `refCode` COLLATE NOCASE)", name, name),
		fmt.Sprintf("CREATE INDEX `idx_%s_rank` ON `%s` (`waitlist`, `referralCount` DESC, `createdAt` ASC)", name, name),
	}
	if err := p.app.Save(c); err != nil {
		return nil, err
	}
	p.logger.Info("waitlist: created collection", "name", name)
	return c, nil
}
