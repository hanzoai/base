// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/dbx"
)

// award is the ONE place points change. Everything — a referral credit, a share
// click, an invite, a verified social follow, a hanzod run, an admin grant —
// flows through here, so the invariants hold in exactly one spot:
//
//   - dedup: at most one award per (entry, dedupKey). A replayed follow, a
//     double-submitted share, a re-run join: all no-ops. This is the anti-fraud
//     spine, enforced by a pre-check AND the UNIQUE(entry, dedupKey) index.
//   - the append-only event is the audit trail + the activity feed source.
//   - entry.points (the denormalized total position derives from) and
//     entry.breakdown (per-source subtotals) move together, atomically.
//
// It returns whether the award was newly applied (false = deduped) and must be
// called inside a transaction so the event and the totals commit together.
func (p *plugin) award(txApp core.App, entry *core.Record, source, dedupKey string, pts int, meta map[string]any) (bool, error) {
	waitlistID := entry.GetString("waitlist")

	if dedupKey != "" {
		_, err := txApp.FindFirstRecordByFilter(
			p.config.eventsCollection(),
			"entry = {:e} && dedupKey = {:k}",
			dbx.Params{"e": entry.Id, "k": dedupKey},
		)
		if err == nil {
			return false, nil // already awarded — idempotent
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}

	evCol, err := txApp.FindCollectionByNameOrId(p.config.eventsCollection())
	if err != nil {
		return false, err
	}
	ev := core.NewRecord(evCol)
	ev.Set("entry", entry.Id)
	ev.Set("waitlist", waitlistID)
	ev.Set("source", source)
	ev.Set("points", pts)
	ev.Set("dedupKey", dedupKey)
	if meta != nil {
		ev.Set("meta", meta)
	}
	if err := txApp.Save(ev); err != nil {
		if isUniqueViolation(err) {
			return false, nil // lost a race on the same dedupKey — treat as deduped
		}
		return false, err
	}

	// Move the denormalized total + the per-source subtotal together. Even a
	// zero-point event (a plain join) is recorded above for the feed; only a
	// non-zero award needs to touch the totals.
	if pts != 0 {
		entry.Set("points", entry.GetFloat("points")+float64(pts))
		bd := entryBreakdown(entry)
		bd[categoryOf(source)] += pts
		entry.Set("breakdown", bd)
		if err := txApp.Save(entry); err != nil {
			return false, err
		}
	}
	return true, nil
}

// competitionRank returns an entry's 1-based position and the list total.
// Position is COMPETITION rank: 1 + the number of entries with strictly more
// points (tied entries share a rank — standard leaderboard semantics). It is a
// single index-range COUNT on (waitlist, points DESC): cheap near the top,
// bounded by the number of entries genuinely ahead.
func (p *plugin) competitionRank(app core.App, waitlistID string, points float64) (rank int, total int, err error) {
	tot, err := app.CountRecords(
		p.config.entriesCollection(),
		dbx.NewExp("waitlist = {:wl}", dbx.Params{"wl": waitlistID}),
	)
	if err != nil {
		return 0, 0, err
	}
	ahead, err := app.CountRecords(
		p.config.entriesCollection(),
		dbx.NewExp("waitlist = {:wl} AND points > {:p}", dbx.Params{"wl": waitlistID, "p": points}),
	)
	if err != nil {
		return 0, 0, err
	}
	return int(ahead) + 1, int(tot), nil
}

// neighbors returns the entries immediately around a pivot — up to `window`
// ranked above (better) and `window` ranked below (worse), each NEAREST-FIRST.
// It is four pure index SEEKS (no OR): same-points peers by createdAt, then the
// adjacent points band. Every sub-query is `SEARCH ... USING INDEX idx_rank`,
// so latency is O(log n + window) — flat no matter how long the list is.
func (p *plugin) neighbors(app core.App, pivot *core.Record, window int) (above, below []*core.Record, err error) {
	wl := pivot.GetString("waitlist")
	pts := pivot.GetFloat("points")
	created := pivot.GetDateTime("createdAt").String()
	params := dbx.Params{"wl": wl, "p": pts, "c": created}
	ent := p.config.entriesCollection()

	// Above = better rank. Nearest first: same-points earlier joiners (latest
	// createdAt that still precedes the pivot), then the next-higher points.
	above, err = app.FindRecordsByFilter(ent,
		"waitlist = {:wl} && points = {:p} && createdAt < {:c}", "-createdAt", window, 0, params)
	if err != nil {
		return nil, nil, err
	}
	if len(above) < window {
		higher, err := app.FindRecordsByFilter(ent,
			"waitlist = {:wl} && points > {:p}", "points,-createdAt", window-len(above), 0, params)
		if err != nil {
			return nil, nil, err
		}
		above = append(above, higher...)
	}

	// Below = worse rank. Nearest first: same-points later joiners, then the
	// next-lower points.
	below, err = app.FindRecordsByFilter(ent,
		"waitlist = {:wl} && points = {:p} && createdAt > {:c}", "createdAt", window, 0, params)
	if err != nil {
		return nil, nil, err
	}
	if len(below) < window {
		lower, err := app.FindRecordsByFilter(ent,
			"waitlist = {:wl} && points < {:p}", "-points,createdAt", window-len(below), 0, params)
		if err != nil {
			return nil, nil, err
		}
		below = append(below, lower...)
	}
	return above, below, nil
}

// ── source taxonomy ───────────────────────────────────────────────────────

// categoryOf buckets an event source into the breakdown category the status
// view renders. New sources fold into an existing bucket (a new social network
// is still "social") or add their own key — never a schema change.
func categoryOf(source string) string {
	switch {
	case source == "referral":
		return "referrals"
	case strings.HasPrefix(source, "share:"):
		return "shares"
	case source == "invite_sent":
		return "invitesSent"
	case source == "invite_converted":
		return "invitesConverted"
	case strings.HasPrefix(source, "social:"):
		return "social"
	case strings.HasPrefix(source, "hanzod"):
		return "hanzod"
	case source == "join":
		return "signup"
	default:
		return "other"
	}
}

// sourcePoints is the server-side award value for a source — the business
// controls the currency, so a caller of /award cannot mint arbitrary points for
// a known source. Only an explicit "grant" carries a caller-supplied amount.
func (p *plugin) sourcePoints(source string) int {
	v := p.config.Points
	switch {
	case source == "referral":
		return v.Referral
	case strings.HasPrefix(source, "share:"):
		return v.Share
	case source == "invite_sent":
		return v.InviteSent
	case source == "invite_converted":
		return v.InviteConverted
	case strings.HasPrefix(source, "social:"):
		return v.Social
	case strings.HasPrefix(source, "hanzod"):
		return v.Hanzod
	case source == "join":
		return v.Signup
	default:
		return 0
	}
}

// ── breakdown helpers ─────────────────────────────────────────────────────

// entryBreakdown reads the per-source subtotal map off an entry, tolerating
// every representation the field takes across a set-vs-reload lifecycle
// (map, json.RawMessage, string, nil).
func entryBreakdown(e *core.Record) map[string]int {
	out := map[string]int{}
	raw := e.Get("breakdown")
	if raw == nil {
		return out
	}
	b, err := json.Marshal(raw)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return out
	}
	// Values may decode as float64 (JSON numbers); normalize to int.
	tmp := map[string]float64{}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return out
	}
	for k, v := range tmp {
		out[k] = int(v)
	}
	return out
}

// pointValues renders the award schedule for the wire (matches the widget's
// PointValues shape).
func (p *plugin) pointValues() map[string]int {
	v := p.config.Points
	return map[string]int{
		"REFERRAL":         v.Referral,
		"SHARE":            v.Share,
		"INVITE_SENT":      v.InviteSent,
		"INVITE_CONVERTED": v.InviteConverted,
		"SOCIAL":           v.Social,
		"HANZOD":           v.Hanzod,
	}
}

// breakdownWire returns the breakdown with the canonical keys always present
// (zero-filled) so the client never has to null-check a source it knows.
func breakdownWire(e *core.Record) map[string]int {
	bd := entryBreakdown(e)
	for _, k := range []string{"referrals", "shares", "invitesSent", "invitesConverted", "social", "hanzod"} {
		if _, ok := bd[k]; !ok {
			bd[k] = 0
		}
	}
	return bd
}
