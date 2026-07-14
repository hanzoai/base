// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"crypto/subtle"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/dbx"
)

// sharePlatforms bounds /track-share to real share targets (anti-garbage).
var sharePlatforms = map[string]struct{}{
	"webshare": {}, "email": {}, "x": {}, "twitter": {}, "linkedin": {},
	"facebook": {}, "reddit": {}, "telegram": {}, "whatsapp": {}, "sms": {},
	"copy": {}, "mastodon": {}, "bluesky": {}, "threads": {},
}

// ── POST /v1/waitlist/join ─────────────────────────────────────────────────

type joinRequest struct {
	Waitlist       string `json:"waitlist"`
	Email          string `json:"email"`
	ReferrerCode   string `json:"referrerCode,omitempty"`
	TurnstileToken string `json:"turnstileToken,omitempty"`
}

func (p *plugin) handleJoin(e *core.RequestEvent) error {
	var req joinRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	email := normalizeEmail(req.Email)
	slug := strings.TrimSpace(req.Waitlist)
	if email == "" || slug == "" {
		return e.BadRequestError("waitlist and email are required", nil)
	}
	if !isValidEmail(email) {
		return e.BadRequestError("invalid email", nil)
	}

	// A trusted server (superuser or admin secret) has already verified the
	// signup, so it bypasses the public widget gates. The public widget path
	// keeps ALL protections: disposable-domain, rate-limit and captcha.
	if !p.isAdmin(e) {
		if _, blocked := p.disposable[emailDomain(email)]; blocked {
			return e.BadRequestError("disposable email not allowed", nil)
		}
		ip := e.RealIP()
		if !p.limiter.allow("join:" + ip) {
			return e.TooManyRequestsError("rate limit exceeded", nil)
		}
		if err := p.turnstile.verify(e.Request.Context(), req.TurnstileToken, ip); err != nil {
			return e.BadRequestError("captcha verification failed", map[string]string{"reason": err.Error()})
		}
	}

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return p.wlLookupError(e, err)
	}
	entriesCol, err := p.app.FindCollectionByNameOrId(p.config.entriesCollection())
	if err != nil {
		return e.InternalServerError("entries collection missing", err)
	}

	var entry *core.Record
	var alreadyJoined bool

	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		if existing, lookupErr := txApp.FindFirstRecordByFilter(
			p.config.entriesCollection(),
			"waitlist = {:wl} && email = {:email}",
			dbx.Params{"wl": wl.Id, "email": email},
		); lookupErr == nil {
			entry, alreadyJoined = existing, true
			return nil
		} else if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}

		refCode, codeErr := p.allocRefCode(txApp, wl.Id)
		if codeErr != nil {
			return codeErr
		}
		fresh := core.NewRecord(entriesCol)
		fresh.Set("waitlist", wl.Id)
		fresh.Set("email", email)
		fresh.Set("refCode", refCode)
		fresh.Set("referredBy", "")
		fresh.Set("referralCount", 0)
		fresh.Set("points", 0)
		fresh.Set("breakdown", map[string]int{})
		fresh.Set("accessGranted", false)
		if err := txApp.Save(fresh); err != nil {
			return err
		}
		entry = fresh

		via := "direct"
		if strings.TrimSpace(req.ReferrerCode) != "" {
			via = "referral"
		}
		// Record the join (signup bonus may be 0 — still logged for the feed).
		if _, err := p.award(txApp, entry, "join", "join", p.config.Points.Signup,
			map[string]any{"who": maskEmail(email), "source": via}); err != nil {
			return err
		}

		return p.creditReferrer(txApp, wl.Id, entry, strings.TrimSpace(req.ReferrerCode), email)
	})
	if txErr != nil {
		p.logger.Error("waitlist: join failed", "error", txErr)
		return e.InternalServerError("join failed", txErr)
	}

	return e.JSON(http.StatusOK, p.entryView(p.app, wl, entry, alreadyJoined, false))
}

// creditReferrer credits a referral (and, if the joiner was invited by the
// referrer, the conversion bonus) — all deduped through award().
func (p *plugin) creditReferrer(txApp core.App, waitlistID string, joiner *core.Record, referrerCode, joinerEmail string) error {
	if referrerCode == "" {
		return nil
	}
	referrer, err := txApp.FindFirstRecordByFilter(
		p.config.entriesCollection(),
		"waitlist = {:wl} && refCode = {:rc}",
		dbx.Params{"wl": waitlistID, "rc": referrerCode},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // unknown code is ignored, not an error
		}
		return err
	}
	if referrer.GetString("email") == joinerEmail {
		return nil // no self-referral
	}
	joiner.Set("referredBy", referrer.GetString("refCode"))
	if err := txApp.Save(joiner); err != nil {
		return err
	}

	referrer.Set("referralCount", referrer.GetFloat("referralCount")+1)
	meta := map[string]any{"who": maskEmail(referrer.GetString("email")), "invited": maskEmail(joinerEmail)}
	if _, err := p.award(txApp, referrer, "referral", "referral:"+joinerEmail, p.config.Points.Referral, meta); err != nil {
		return err
	}

	// Conversion: did this referrer previously invite the joiner's email?
	if _, err := txApp.FindFirstRecordByFilter(
		p.config.eventsCollection(),
		"entry = {:e} && dedupKey = {:k}",
		dbx.Params{"e": referrer.Id, "k": "invite:" + joinerEmail},
	); err == nil {
		if _, err := p.award(txApp, referrer, "invite_converted", "invite_converted:"+joinerEmail,
			p.config.Points.InviteConverted, meta); err != nil {
			return err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

// ── GET /v1/waitlist/status ────────────────────────────────────────────────

func (p *plugin) handleStatus(e *core.RequestEvent) error {
	slug := strings.TrimSpace(e.Request.URL.Query().Get("waitlist"))
	email := normalizeEmail(e.Request.URL.Query().Get("email"))
	if slug == "" || email == "" {
		return e.BadRequestError("waitlist and email are required", nil)
	}
	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return p.wlLookupError(e, err)
	}
	entry, err := p.app.FindFirstRecordByFilter(
		p.config.entriesCollection(),
		"waitlist = {:wl} && email = {:email}",
		dbx.Params{"wl": wl.Id, "email": email},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("entry not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
	}
	return e.JSON(http.StatusOK, p.entryView(p.app, wl, entry, false, true))
}

// ── GET /v1/waitlist/neighborhood ──────────────────────────────────────────
//
// The scalable leaderboard view: the caller's absolute rank plus the `window`
// entries just above and below — never the whole list. Keyset seeks make it
// O(log n + window), flat regardless of list size (benchmarked at ~0.1ms to
// 10M rows).

func (p *plugin) handleNeighborhood(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	slug := strings.TrimSpace(q.Get("waitlist"))
	email := normalizeEmail(q.Get("email"))
	if slug == "" || email == "" {
		return e.BadRequestError("waitlist and email are required", nil)
	}
	window := clampInt(atoiOr(q.Get("window"), 25), 1, 100)

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return p.wlLookupError(e, err)
	}
	pivot, err := p.app.FindFirstRecordByFilter(
		p.config.entriesCollection(),
		"waitlist = {:wl} && email = {:email}",
		dbx.Params{"wl": wl.Id, "email": email},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("entry not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
	}

	rank, total, err := p.competitionRank(p.app, wl.Id, pivot.GetFloat("points"))
	if err != nil {
		return e.InternalServerError("rank failed", err)
	}
	above, below, err := p.neighbors(p.app, pivot, window)
	if err != nil {
		return e.InternalServerError("neighborhood failed", err)
	}

	rows := make([]neighborRow, 0, len(above)+len(below)+1)
	for i := len(above) - 1; i >= 0; i-- { // farthest-above first, in rank order
		rows = append(rows, mkNeighbor(above[i], rank-(i+1), false))
	}
	rows = append(rows, mkNeighbor(pivot, rank, true))
	for j, r := range below {
		rows = append(rows, mkNeighbor(r, rank+(j+1), false))
	}

	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "waitlist": wl.GetString("slug"), "email": email,
		"rank": rank, "total": total, "points": int(pivot.GetFloat("points")),
		"window": window, "entries": rows,
	})
}

type neighborRow struct {
	Rank          int    `json:"rank"`
	Email         string `json:"email"`
	Points        int    `json:"points"`
	ReferralCount int    `json:"referralCount"`
	IsMe          bool   `json:"isMe,omitempty"`
}

func mkNeighbor(r *core.Record, rank int, isMe bool) neighborRow {
	return neighborRow{
		Rank: rank, Email: maskEmail(r.GetString("email")),
		Points: int(r.GetFloat("points")), ReferralCount: int(r.GetFloat("referralCount")), IsMe: isMe,
	}
}

// ── GET /v1/waitlist/list ──────────────────────────────────────────────────
//
// Leaderboard page (top-N). Page 1 is the head of the list and is cheap; deep
// offsets pay for the skip — for "around me" use /neighborhood instead.

func (p *plugin) handleList(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	slug := strings.TrimSpace(q.Get("waitlist"))
	if slug == "" {
		return e.BadRequestError("waitlist is required", nil)
	}
	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.JSON(http.StatusOK, map[string]any{"ok": true, "waitlist": slug, "page": 1, "pageSize": 0, "total": 0, "totalPages": 1, "entries": []any{}})
		}
		return e.InternalServerError("lookup failed", err)
	}
	page := clampInt(atoiOr(q.Get("page"), 1), 1, 1<<30)
	pageSize := clampInt(atoiOr(q.Get("pageSize"), 100), 1, 500)
	isAdmin := p.isAdmin(e)

	total, err := p.app.CountRecords(p.config.entriesCollection(), dbx.NewExp("waitlist = {:wl}", dbx.Params{"wl": wl.Id}))
	if err != nil {
		return e.InternalServerError("count failed", err)
	}
	offset := (page - 1) * pageSize
	recs, err := p.app.FindRecordsByFilter(
		p.config.entriesCollection(),
		"waitlist = {:wl}", "-points,createdAt", pageSize, offset,
		dbx.Params{"wl": wl.Id},
	)
	if err != nil {
		return e.InternalServerError("query failed", err)
	}
	entries := make([]map[string]any, 0, len(recs))
	for i, r := range recs {
		row := map[string]any{
			"rank":          offset + i + 1,
			"email":         emailFor(r, isAdmin),
			"refCode":       refCodeFor(r, isAdmin),
			"points":        int(r.GetFloat("points")),
			"referralCount": int(r.GetFloat("referralCount")),
			"createdAt":     r.GetDateTime("createdAt").String(),
		}
		entries = append(entries, row)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "waitlist": wl.GetString("slug"), "page": page, "pageSize": pageSize,
		"total": int(total), "totalPages": max0(int((total + int64(pageSize) - 1) / int64(pageSize))), "entries": entries,
	})
}

// ── GET /v1/waitlist/activity ──────────────────────────────────────────────

func (p *plugin) handleActivity(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	slug := strings.TrimSpace(q.Get("waitlist"))
	if slug == "" {
		return e.BadRequestError("waitlist is required", nil)
	}
	limit := clampInt(atoiOr(q.Get("limit"), 20), 1, 100)
	typeFilter := parseTypes(q.Get("types"))

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.JSON(http.StatusOK, map[string]any{"ok": true, "waitlist": slug, "now": time.Now().UnixMilli(), "entries": []any{}})
		}
		return e.InternalServerError("lookup failed", err)
	}
	// Over-fetch when filtering (a type may be sparse), then filter + truncate.
	fetch := limit
	if len(typeFilter) > 0 {
		fetch = 100
	}
	recs, err := p.app.FindRecordsByFilter(
		p.config.eventsCollection(),
		"waitlist = {:wl}", "-createdAt", fetch, 0, dbx.Params{"wl": wl.Id},
	)
	if err != nil {
		return e.InternalServerError("query failed", err)
	}
	out := make([]map[string]any, 0, limit)
	for _, ev := range recs {
		a := activityFromEvent(ev)
		if len(typeFilter) > 0 {
			if _, ok := typeFilter[a["type"].(string)]; !ok {
				continue
			}
		}
		out = append(out, a)
		if len(out) >= limit {
			break
		}
	}
	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "waitlist": wl.GetString("slug"), "now": time.Now().UnixMilli(), "entries": out,
	})
}

// ── POST /v1/waitlist/track-share ──────────────────────────────────────────

type trackShareRequest struct {
	Waitlist string `json:"waitlist"`
	RefCode  string `json:"refCode"`
	Platform string `json:"platform"`
}

func (p *plugin) handleTrackShare(e *core.RequestEvent) error {
	var req trackShareRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	slug := strings.TrimSpace(req.Waitlist)
	refCode := strings.TrimSpace(req.RefCode)
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if slug == "" || refCode == "" || platform == "" {
		return e.BadRequestError("waitlist, refCode, platform are required", nil)
	}
	if _, ok := sharePlatforms[platform]; !ok {
		return e.BadRequestError("unknown platform: "+platform, nil)
	}
	if !p.limiter.allow("share:" + e.RealIP()) {
		return e.TooManyRequestsError("rate limit exceeded", nil)
	}

	wl, entry, err := p.resolveEntryByRefCode(slug, refCode)
	if err != nil {
		return p.entryLookupError(e, err)
	}

	var awarded bool
	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		fresh, _ := txApp.FindFirstRecordByFilter(p.config.entriesCollection(),
			"id = {:id}", dbx.Params{"id": entry.Id})
		if fresh == nil {
			fresh = entry
		}
		var e2 error
		awarded, e2 = p.award(txApp, fresh, "share:"+platform, "share:"+platform+":"+todayUTC(),
			p.config.Points.Share, map[string]any{"who": maskEmail(fresh.GetString("email")), "platform": platform})
		entry = fresh
		return e2
	})
	if txErr != nil {
		return e.InternalServerError("share failed", txErr)
	}
	fresh, _ := p.app.FindRecordById(p.config.entriesCollection(), entry.Id)
	awardedPts := 0
	if awarded {
		awardedPts = p.config.Points.Share
	}
	_ = wl
	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "awarded": awardedPts, "alreadyClaimed": !awarded,
		"points": int(fresh.GetFloat("points")), "pointBreakdown": breakdownWire(fresh),
	})
}

// ── POST /v1/waitlist/invite ───────────────────────────────────────────────

type inviteRequest struct {
	Waitlist string   `json:"waitlist"`
	RefCode  string   `json:"refCode"`
	Emails   []string `json:"emails"`
	Message  string   `json:"message,omitempty"`
}

func (p *plugin) handleInvite(e *core.RequestEvent) error {
	var req inviteRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	slug := strings.TrimSpace(req.Waitlist)
	refCode := strings.TrimSpace(req.RefCode)
	if slug == "" || refCode == "" || len(req.Emails) == 0 {
		return e.BadRequestError("waitlist, refCode, emails are required", nil)
	}
	if len(req.Emails) > p.config.InviteMaxBatch {
		return e.BadRequestError(fmt.Sprintf("max %d emails per batch", p.config.InviteMaxBatch), nil)
	}
	if !p.limiter.allow("invite:" + e.RealIP()) {
		return e.TooManyRequestsError("rate limit exceeded", nil)
	}

	wl, entry, err := p.resolveEntryByRefCode(slug, refCode)
	if err != nil {
		return p.entryLookupError(e, err)
	}

	var sent, skipped, invalid, dupes int
	seen := map[string]struct{}{}
	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		fresh, _ := txApp.FindFirstRecordByFilter(p.config.entriesCollection(), "id = {:id}", dbx.Params{"id": entry.Id})
		if fresh == nil {
			fresh = entry
		}
		who := maskEmail(fresh.GetString("email"))
		for _, raw := range req.Emails {
			inv := normalizeEmail(raw)
			if inv == "" {
				continue
			}
			if _, dup := seen[inv]; dup {
				dupes++
				continue
			}
			seen[inv] = struct{}{}
			if !isValidEmail(inv) {
				invalid++
				continue
			}
			if _, blocked := p.disposable[emailDomain(inv)]; blocked {
				invalid++
				continue
			}
			if inv == fresh.GetString("email") {
				invalid++
				continue
			}
			// Already on the list -> nothing to invite.
			if _, e2 := txApp.FindFirstRecordByFilter(p.config.entriesCollection(),
				"waitlist = {:wl} && email = {:email}", dbx.Params{"wl": wl.Id, "email": inv}); e2 == nil {
				skipped++
				continue
			} else if !errors.Is(e2, sql.ErrNoRows) {
				return e2
			}
			ok, e2 := p.award(txApp, fresh, "invite_sent", "invite:"+inv, p.config.Points.InviteSent,
				map[string]any{"who": who, "invited": maskEmail(inv)})
			if e2 != nil {
				return e2
			}
			if ok {
				sent++
			} else {
				dupes++ // already invited this address before
			}
		}
		entry = fresh
		return nil
	})
	if txErr != nil {
		return e.InternalServerError("invite failed", txErr)
	}
	// NOTE: delivery of the invite email is a host concern (SMTP/provider). The
	// plugin records the invite so the conversion bonus fires when they join.
	fresh, _ := p.app.FindRecordById(p.config.entriesCollection(), entry.Id)
	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "sent": sent, "skipped": skipped, "invalid": invalid, "duplicates": dupes,
		"pointsAwarded": sent * p.config.Points.InviteSent,
		"points":        int(fresh.GetFloat("points")), "pointBreakdown": breakdownWire(fresh),
	})
}

// ── POST /v1/waitlist/boost ────────────────────────────────────────────────
//
// Service-authed position boost. A trusted server (a superuser session, or the
// shared AdminSecret) credits a caller-supplied amount of points to an entry
// — e.g. hanzod crediting a verified node run. It is the SAME walk as /award
// (the one award() choke point, one points number); it differs only in auth
// (superuser/AdminSecret vs the connector's AwardSecret) and in that the
// caller sets the amount. An empty dedupKey is a plain accumulator; a non-empty
// one makes the boost replay-safe (node-nonce keyed).

type boostRequest struct {
	Waitlist    string `json:"waitlist"`
	Email       string `json:"email,omitempty"`
	RefCode     string `json:"refCode,omitempty"`
	Source      string `json:"source"`
	Amount      int    `json:"amount,omitempty"`
	DedupKey    string `json:"dedupKey,omitempty"`
	GrantAccess bool   `json:"grantAccess,omitempty"`
}

func (p *plugin) handleBoost(e *core.RequestEvent) error {
	// Service-authed ONLY: superuser or admin secret. Mirrors /export — no
	// secret configured and no superuser makes the route look absent (404).
	if !p.isAdmin(e) {
		if p.config.AdminSecret == "" {
			return e.NotFoundError("boost disabled", nil)
		}
		return e.UnauthorizedError("admin auth required", nil)
	}

	var req boostRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	slug := strings.TrimSpace(req.Waitlist)
	source := strings.TrimSpace(req.Source)
	if slug == "" || source == "" {
		return e.BadRequestError("waitlist and source are required", nil)
	}
	if !validAwardSource(source) {
		return e.BadRequestError("unknown source: "+source, nil)
	}
	amount := req.Amount
	if amount == 0 {
		amount = 1
	}

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return p.wlLookupError(e, err)
	}
	entry, err := p.lookupEntry(wl.Id, normalizeEmail(req.Email), strings.TrimSpace(req.RefCode))
	if err != nil {
		return p.entryLookupError(e, err)
	}

	meta := map[string]any{"who": maskEmail(entry.GetString("email"))}
	var awarded bool
	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		fresh, e2 := txApp.FindRecordById(p.config.entriesCollection(), entry.Id)
		if e2 != nil {
			return e2
		}
		var e3 error
		if awarded, e3 = p.award(txApp, fresh, source, strings.TrimSpace(req.DedupKey), amount, meta); e3 != nil {
			return e3
		}
		if req.GrantAccess {
			fresh.Set("accessGranted", true)
			if e4 := txApp.Save(fresh); e4 != nil {
				return e4
			}
		}
		entry = fresh
		return nil
	})
	if txErr != nil {
		p.logger.Error("waitlist: boost failed", "error", txErr)
		return e.InternalServerError("boost failed", txErr)
	}

	fresh, _ := p.app.FindRecordById(p.config.entriesCollection(), entry.Id)
	rank, total, err := p.competitionRank(p.app, wl.Id, fresh.GetFloat("points"))
	if err != nil {
		return e.InternalServerError("rank failed", err)
	}
	awardedPts := 0
	if awarded {
		awardedPts = amount
	}
	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "awarded": awardedPts, "alreadyAwarded": !awarded,
		"source": source, "email": entry.GetString("email"),
		"points": int(fresh.GetFloat("points")), "pointBreakdown": breakdownWire(fresh),
		"rank": rank, "total": total, "hasAccess": p.hasAccess(fresh, rank),
	})
}

// ── POST /v1/waitlist/award ────────────────────────────────────────────────
//
// Server-to-server award for a VERIFIED event. This is the extensibility seam:
// a cloud automations connector verifies a social follow/join or a hanzod run,
// then calls this endpoint (Bearer AwardSecret) to credit points. It is never
// public — a client cannot self-award. Idempotent via (entry, dedupKey).

type awardRequest struct {
	Waitlist string         `json:"waitlist"`
	Email    string         `json:"email,omitempty"`
	RefCode  string         `json:"refCode,omitempty"`
	Source   string         `json:"source"`
	DedupKey string         `json:"dedupKey,omitempty"`
	Points   *int           `json:"points,omitempty"` // honored only for "grant"
	Meta     map[string]any `json:"meta,omitempty"`
}

func (p *plugin) handleAward(e *core.RequestEvent) error {
	if p.config.AwardSecret == "" {
		return e.NotFoundError("award disabled", nil)
	}
	header := e.Request.Header.Get("Authorization")
	expected := "Bearer " + p.config.AwardSecret
	if subtle.ConstantTimeCompare([]byte(header), []byte(expected)) != 1 {
		return e.UnauthorizedError("award auth required", nil)
	}

	var req awardRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	slug := strings.TrimSpace(req.Waitlist)
	source := strings.TrimSpace(req.Source)
	if slug == "" || source == "" {
		return e.BadRequestError("waitlist and source are required", nil)
	}
	if !validAwardSource(source) {
		return e.BadRequestError("unknown source: "+source, nil)
	}

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return p.wlLookupError(e, err)
	}
	entry, err := p.lookupEntry(wl.Id, normalizeEmail(req.Email), strings.TrimSpace(req.RefCode))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("entry not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
	}

	pts := p.sourcePoints(source)
	if source == "grant" && req.Points != nil {
		pts = *req.Points // the one caller-controlled amount (admin bonus)
	}
	dedupKey := strings.TrimSpace(req.DedupKey)
	if dedupKey == "" && source != "grant" {
		dedupKey = source // a bare social:x:follow dedups to once per entry
	}
	meta := req.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	if _, ok := meta["who"]; !ok {
		meta["who"] = maskEmail(entry.GetString("email"))
	}

	var awarded bool
	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		fresh, e2 := txApp.FindRecordById(p.config.entriesCollection(), entry.Id)
		if e2 != nil {
			return e2
		}
		var e3 error
		awarded, e3 = p.award(txApp, fresh, source, dedupKey, pts, meta)
		entry = fresh
		return e3
	})
	if txErr != nil {
		return e.InternalServerError("award failed", txErr)
	}
	fresh, _ := p.app.FindRecordById(p.config.entriesCollection(), entry.Id)
	rank, total, err := p.competitionRank(p.app, wl.Id, fresh.GetFloat("points"))
	if err != nil {
		return e.InternalServerError("rank failed", err)
	}
	awardedPts := 0
	if awarded {
		awardedPts = pts
	}
	return e.JSON(http.StatusOK, map[string]any{
		"ok": true, "awarded": awardedPts, "alreadyAwarded": !awarded,
		"source": source, "email": entry.GetString("email"),
		"points": int(fresh.GetFloat("points")), "pointBreakdown": breakdownWire(fresh),
		"rank": rank, "total": total,
	})
}

// ── GET /v1/waitlist/export ────────────────────────────────────────────────

func (p *plugin) handleExport(e *core.RequestEvent) error {
	if !p.isAdmin(e) {
		if p.config.AdminSecret == "" {
			return e.NotFoundError("export disabled", nil)
		}
		return e.UnauthorizedError("admin auth required", nil)
	}
	slug := strings.TrimSpace(e.Request.URL.Query().Get("waitlist"))
	if slug == "" {
		return e.BadRequestError("waitlist is required", nil)
	}
	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return p.wlLookupError(e, err)
	}
	entries, err := p.app.FindRecordsByFilter(
		p.config.entriesCollection(), "waitlist = {:wl}", "-points,createdAt", 0, 0, dbx.Params{"wl": wl.Id},
	)
	if err != nil {
		return e.InternalServerError("query failed", err)
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"rank", "email", "refCode", "referredBy", "points", "referralCount", "accessGranted", "createdAt"})
	for i, r := range entries {
		_ = w.Write([]string{
			strconv.Itoa(i + 1), r.GetString("email"), r.GetString("refCode"), r.GetString("referredBy"),
			strconv.Itoa(int(r.GetFloat("points"))), strconv.Itoa(int(r.GetFloat("referralCount"))),
			strconv.FormatBool(r.GetBool("accessGranted")),
			r.GetDateTime("createdAt").String(),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return e.InternalServerError("csv encode failed", err)
	}
	e.Response.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="waitlist-%s-%d.csv"`, wl.GetString("slug"), time.Now().Unix()))
	return e.Blob(http.StatusOK, "text/csv; charset=utf-8", []byte(buf.String()))
}

// ── shared helpers ─────────────────────────────────────────────────────────

// entryView builds the join/status wire object for a single entry: its
// points-derived rank, the per-source breakdown, and the access decision.
func (p *plugin) entryView(app core.App, wl, entry *core.Record, alreadyJoined, withAheadOf bool) map[string]any {
	rank, total, err := p.competitionRank(app, wl.Id, entry.GetFloat("points"))
	if err != nil {
		p.logger.Error("waitlist: rank failed", "error", err)
	}
	out := map[string]any{
		"ok": true, "waitlist": wl.GetString("slug"), "email": entry.GetString("email"),
		"refCode": entry.GetString("refCode"), "rank": rank, "total": total,
		"points": int(entry.GetFloat("points")), "pointBreakdown": breakdownWire(entry),
		"pointValues": p.pointValues(), "referralCount": int(entry.GetFloat("referralCount")),
		"hasAccess": p.hasAccess(entry, rank),
		"capacity":  p.config.AccessCapacity, "open": p.config.Open,
		"shareUrl": "?ref=" + entry.GetString("refCode"),
	}
	if withAheadOf {
		out["aheadOf"] = max0(total - rank)
	}
	if alreadyJoined {
		out["alreadyJoined"] = true
	}
	return out
}

// grantsAccess is the pure access policy: the master open switch, a sticky
// per-entry grant, or a rank within the auto-grant capacity window.
func grantsAccess(open, accessGranted bool, capacity, rank int) bool {
	return open || accessGranted || (capacity > 0 && rank <= capacity)
}

// hasAccess reports whether the entry may access the gated product at the
// given (points-derived) rank. When access is earned via the capacity
// threshold it is persisted (sticky) so later rank drift never revokes it.
// Persist errors are swallowed — access is a read concern and must never fail
// the request.
func (p *plugin) hasAccess(entry *core.Record, rank int) bool {
	granted := entry.GetBool("accessGranted")
	if !grantsAccess(p.config.Open, granted, p.config.AccessCapacity, rank) {
		return false
	}
	// Only a capacity-earned grant is made sticky; the open switch stays a
	// live global toggle and an already-granted entry needs no write.
	if !granted && !p.config.Open {
		entry.Set("accessGranted", true)
		if err := p.app.Save(entry); err != nil {
			p.logger.Warn("waitlist: sticky access persist failed", "error", err)
		}
	}
	return true
}

func (p *plugin) allocRefCode(txApp core.App, waitlistID string) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		cand := generateRefCode()
		_, err := txApp.FindFirstRecordByFilter(p.config.entriesCollection(),
			"waitlist = {:wl} && refCode = {:rc}", dbx.Params{"wl": waitlistID, "rc": cand})
		if errors.Is(err, sql.ErrNoRows) {
			return cand, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("waitlist: failed to allocate unique refCode")
}

func (p *plugin) resolveEntryByRefCode(slug, refCode string) (*core.Record, *core.Record, error) {
	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		return nil, nil, err
	}
	entry, err := p.app.FindFirstRecordByFilter(p.config.entriesCollection(),
		"waitlist = {:wl} && refCode = {:rc}", dbx.Params{"wl": wl.Id, "rc": refCode})
	if err != nil {
		return wl, nil, err
	}
	return wl, entry, nil
}

func (p *plugin) lookupEntry(waitlistID, email, refCode string) (*core.Record, error) {
	if email != "" {
		return p.app.FindFirstRecordByFilter(p.config.entriesCollection(),
			"waitlist = {:wl} && email = {:email}", dbx.Params{"wl": waitlistID, "email": email})
	}
	if refCode != "" {
		return p.app.FindFirstRecordByFilter(p.config.entriesCollection(),
			"waitlist = {:wl} && refCode = {:rc}", dbx.Params{"wl": waitlistID, "rc": refCode})
	}
	return nil, sql.ErrNoRows
}

func (p *plugin) findWaitlistBySlugOrID(slugOrID string) (*core.Record, error) {
	rec, err := p.app.FindFirstRecordByData(p.config.waitlistsCollection(), "slug", slugOrID)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return p.app.FindRecordById(p.config.waitlistsCollection(), slugOrID)
}

// isAdmin is true for a superuser session or a matching admin Bearer secret.
// It is the single service-authed predicate the plugin gates /boost and
// /export on, and the flag that lets a trusted server bypass the public /join
// widget protections.
func (p *plugin) isAdmin(e *core.RequestEvent) bool {
	if e.HasSuperuserAuth() {
		return true
	}
	if p.config.AdminSecret == "" {
		return false
	}
	header := e.Request.Header.Get("Authorization")
	return subtle.ConstantTimeCompare([]byte(header), []byte("Bearer "+p.config.AdminSecret)) == 1
}

func (p *plugin) wlLookupError(e *core.RequestEvent, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return e.NotFoundError("waitlist not found", nil)
	}
	return e.InternalServerError("lookup failed", err)
}

func (p *plugin) entryLookupError(e *core.RequestEvent, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return e.NotFoundError("entry not found", nil)
	}
	return e.InternalServerError("lookup failed", err)
}

// activityFromEvent projects a ledger row onto the widget's activity shape
// (type + who + platform/invited), reading the denormalized meta so the feed
// needs no join back to entries.
func activityFromEvent(ev *core.Record) map[string]any {
	src := ev.GetString("source")
	meta := eventMeta(ev)
	a := map[string]any{"ts": ev.GetDateTime("createdAt").Time().UnixMilli(), "type": activityType(src)}
	if who, ok := meta["who"]; ok {
		a["who"] = who
	}
	if inv, ok := meta["invited"]; ok {
		a["invited"] = inv
	}
	switch {
	case src == "join":
		if s, ok := meta["source"]; ok {
			a["source"] = s
		}
	case strings.HasPrefix(src, "share:"):
		a["platform"] = strings.TrimPrefix(src, "share:")
	case strings.HasPrefix(src, "social:"):
		a["platform"] = socialPlatform(src)
	}
	return a
}

func activityType(source string) string {
	switch {
	case source == "join":
		return "join"
	case source == "referral", source == "invite_converted":
		return "referral"
	case strings.HasPrefix(source, "share:"):
		return "share"
	case source == "invite_sent":
		return "invite"
	case strings.HasPrefix(source, "social:"):
		return "social"
	case strings.HasPrefix(source, "hanzod"):
		return "hanzod"
	default:
		return "other"
	}
}

// socialPlatform pulls the network out of "social:<platform>:<action>".
func socialPlatform(source string) string {
	parts := strings.Split(source, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// validAwardSource gates /award and /boost to the known earn channels
// (open-ended within social:/hanzod so a new network needs no core change).
func validAwardSource(source string) bool {
	switch {
	case source == "referral", source == "invite_sent", source == "invite_converted", source == "grant":
		return true
	case strings.HasPrefix(source, "share:"), strings.HasPrefix(source, "social:"), strings.HasPrefix(source, "hanzod"):
		return true
	default:
		return false
	}
}

func eventMeta(ev *core.Record) map[string]any {
	out := map[string]any{}
	raw := ev.Get("meta")
	if raw == nil {
		return out
	}
	b, err := json.Marshal(raw)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func emailFor(r *core.Record, isAdmin bool) string {
	if isAdmin {
		return r.GetString("email")
	}
	return maskEmail(r.GetString("email"))
}

func refCodeFor(r *core.Record, isAdmin bool) any {
	if isAdmin {
		return r.GetString("refCode")
	}
	return nil
}

func parseTypes(s string) map[string]struct{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			out[t] = struct{}{}
		}
	}
	return out
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
