// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

import (
	"crypto/subtle"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/dbx"
)

// --- POST /v1/waitlist/join ---

type joinRequest struct {
	Waitlist       string `json:"waitlist"` // slug (preferred) or id
	Email          string `json:"email"`
	ReferrerCode   string `json:"referrerCode,omitempty"`
	TurnstileToken string `json:"turnstileToken,omitempty"`
}

type joinResponse struct {
	OK            bool   `json:"ok"`
	Waitlist      string `json:"waitlist"`
	Email         string `json:"email"`
	RefCode       string `json:"refCode"`
	Rank          int    `json:"rank"`
	Total         int    `json:"total"`
	ReferralCount int    `json:"referralCount"`
	Score         int    `json:"score"`
	Boost         int    `json:"boost"`
	HasAccess     bool   `json:"hasAccess"`
	ShareURL      string `json:"shareUrl"`
	AlreadyJoined bool   `json:"alreadyJoined,omitempty"`
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
	if !p.isServiceAuthed(e) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("waitlist not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
	}

	entriesName := p.config.entriesCollection()
	entriesCol, err := p.app.FindCollectionByNameOrId(entriesName)
	if err != nil {
		return e.InternalServerError("entries collection missing", err)
	}

	var entry *core.Record
	var referralCount int
	var alreadyJoined bool

	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		// Idempotency: same (waitlist, email) returns the existing entry.
		existing, lookupErr := txApp.FindFirstRecordByFilter(
			entriesName,
			"waitlist = {:wl} && email = {:email}",
			dbx.Params{"wl": wl.Id, "email": email},
		)
		if lookupErr == nil {
			entry = existing
			referralCount = int(existing.GetFloat("referralCount"))
			alreadyJoined = true
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}

		// Resolve referrer (best-effort: bad code is ignored, not rejected).
		var referrerCode string
		if strings.TrimSpace(req.ReferrerCode) != "" {
			referrer, refErr := txApp.FindFirstRecordByFilter(
				entriesName,
				"waitlist = {:wl} && refCode = {:rc}",
				dbx.Params{"wl": wl.Id, "rc": strings.TrimSpace(req.ReferrerCode)},
			)
			if refErr == nil && referrer.GetString("email") != email {
				referrer.Set("referralCount", referrer.GetFloat("referralCount")+1)
				if saveErr := txApp.Save(referrer); saveErr != nil {
					return saveErr
				}
				referrerCode = referrer.GetString("refCode")
			}
		}

		// Generate a unique refCode (collision-safe loop).
		var newCode string
		for attempt := 0; attempt < 8; attempt++ {
			candidate := generateRefCode()
			_, dupErr := txApp.FindFirstRecordByFilter(
				entriesName,
				"waitlist = {:wl} && refCode = {:rc}",
				dbx.Params{"wl": wl.Id, "rc": candidate},
			)
			if errors.Is(dupErr, sql.ErrNoRows) {
				newCode = candidate
				break
			}
			if dupErr != nil {
				return dupErr
			}
		}
		if newCode == "" {
			return errors.New("waitlist: failed to allocate unique refCode")
		}

		fresh := core.NewRecord(entriesCol)
		fresh.Set("waitlist", wl.Id)
		fresh.Set("email", email)
		fresh.Set("refCode", newCode)
		fresh.Set("referredBy", referrerCode)
		fresh.Set("referralCount", 0)
		fresh.Set("boost", 0)
		fresh.Set("accessGranted", false)
		if saveErr := txApp.Save(fresh); saveErr != nil {
			return saveErr
		}
		entry = fresh
		referralCount = 0
		return nil
	})
	if txErr != nil {
		p.logger.Error("waitlist: join failed", "error", txErr)
		return e.InternalServerError("join failed", txErr)
	}

	rank, total, err := p.computeRank(wl.Id, entry)
	if err != nil {
		return e.InternalServerError("rank failed", err)
	}

	return e.JSON(http.StatusOK, joinResponse{
		OK:            true,
		Waitlist:      wl.GetString("slug"),
		Email:         email,
		RefCode:       entry.GetString("refCode"),
		Rank:          rank,
		Total:         total,
		ReferralCount: referralCount,
		Score:         int(entryScore(entry)),
		Boost:         int(entry.GetFloat("boost")),
		HasAccess:     p.hasAccess(entry, rank),
		ShareURL:      "?ref=" + entry.GetString("refCode"),
		AlreadyJoined: alreadyJoined,
	})
}

// --- GET /v1/waitlist/status ---

type statusResponse struct {
	OK            bool   `json:"ok"`
	Waitlist      string `json:"waitlist"`
	Email         string `json:"email"`
	RefCode       string `json:"refCode"`
	Rank          int    `json:"rank"`
	Total         int    `json:"total"`
	AheadOf       int    `json:"aheadOf"`
	ReferralCount int    `json:"referralCount"`
	Score         int    `json:"score"`
	Boost         int    `json:"boost"`
	HasAccess     bool   `json:"hasAccess"`
	Capacity      int    `json:"capacity"`
	Open          bool   `json:"open"`
	ShareURL      string `json:"shareUrl"`
}

func (p *plugin) handleStatus(e *core.RequestEvent) error {
	slug := strings.TrimSpace(e.Request.URL.Query().Get("waitlist"))
	email := normalizeEmail(e.Request.URL.Query().Get("email"))
	if slug == "" || email == "" {
		return e.BadRequestError("waitlist and email are required", nil)
	}

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("waitlist not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
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

	rank, total, err := p.computeRank(wl.Id, entry)
	if err != nil {
		return e.InternalServerError("rank failed", err)
	}

	return e.JSON(http.StatusOK, statusResponse{
		OK:            true,
		Waitlist:      wl.GetString("slug"),
		Email:         email,
		RefCode:       entry.GetString("refCode"),
		Rank:          rank,
		Total:         total,
		AheadOf:       max0(total - rank),
		ReferralCount: int(entry.GetFloat("referralCount")),
		Score:         int(entryScore(entry)),
		Boost:         int(entry.GetFloat("boost")),
		HasAccess:     p.hasAccess(entry, rank),
		Capacity:      p.config.AccessCapacity,
		Open:          p.config.Open,
		ShareURL:      "?ref=" + entry.GetString("refCode"),
	})
}

// --- POST /v1/waitlist/boost ---

type boostRequest struct {
	Waitlist    string  `json:"waitlist"` // slug (preferred) or id
	Email       string  `json:"email"`
	Source      string  `json:"source"`
	Amount      float64 `json:"amount,omitempty"`
	GrantAccess bool    `json:"grantAccess,omitempty"`
}

type boostResponse struct {
	OK        bool   `json:"ok"`
	Email     string `json:"email"`
	Score     int    `json:"score"`
	Boost     int    `json:"boost"`
	Rank      int    `json:"rank"`
	HasAccess bool   `json:"hasAccess"`
}

// boostSources enumerates the accepted position-boost origins. There is no
// per-boost ledger today: boost is a simple accumulator. Idempotent,
// node-nonce-keyed boosting (replay-safe hanzod crediting) is a follow-up.
var boostSources = map[string]struct{}{
	"hanzod":   {},
	"referral": {},
	"share":    {},
	"grant":    {},
}

func (p *plugin) handleBoost(e *core.RequestEvent) error {
	// Service-authed ONLY: superuser or admin secret. Mirrors handleExport —
	// no secret configured and no superuser looks like the route is absent.
	if !p.isServiceAuthed(e) {
		if p.config.AdminSecret == "" {
			return e.NotFoundError("boost disabled", nil)
		}
		return e.UnauthorizedError("admin auth required", nil)
	}

	var req boostRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	email := normalizeEmail(req.Email)
	slug := strings.TrimSpace(req.Waitlist)
	if email == "" || slug == "" {
		return e.BadRequestError("waitlist and email are required", nil)
	}
	if _, ok := boostSources[req.Source]; !ok {
		return e.BadRequestError("unknown boost source", nil)
	}
	amount := req.Amount
	if amount == 0 {
		amount = 1
	}

	wl, err := p.findWaitlistBySlugOrID(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("waitlist not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
	}

	entriesName := p.config.entriesCollection()
	var entry *core.Record
	txErr := p.app.RunInTransaction(func(txApp core.App) error {
		found, lookupErr := txApp.FindFirstRecordByFilter(
			entriesName,
			"waitlist = {:wl} && email = {:email}",
			dbx.Params{"wl": wl.Id, "email": email},
		)
		if lookupErr != nil {
			return lookupErr
		}
		found.Set("boost", found.GetFloat("boost")+amount)
		if req.GrantAccess {
			found.Set("accessGranted", true)
		}
		if saveErr := txApp.Save(found); saveErr != nil {
			return saveErr
		}
		entry = found
		return nil
	})
	if txErr != nil {
		if errors.Is(txErr, sql.ErrNoRows) {
			return e.NotFoundError("entry not found", nil)
		}
		p.logger.Error("waitlist: boost failed", "error", txErr)
		return e.InternalServerError("boost failed", txErr)
	}

	rank, _, err := p.computeRank(wl.Id, entry)
	if err != nil {
		return e.InternalServerError("rank failed", err)
	}

	return e.JSON(http.StatusOK, boostResponse{
		OK:        true,
		Email:     email,
		Score:     int(entryScore(entry)),
		Boost:     int(entry.GetFloat("boost")),
		Rank:      rank,
		HasAccess: p.hasAccess(entry, rank),
	})
}

// --- GET /v1/waitlist/export ---

func (p *plugin) handleExport(e *core.RequestEvent) error {
	// Superuser auth OR shared admin secret in Authorization header.
	if !p.isServiceAuthed(e) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("waitlist not found", nil)
		}
		return e.InternalServerError("lookup failed", err)
	}

	// Ordered by referralCount for a stable, index-backed sort. Score order
	// (referralCount + boost) is the ideal; the per-row `score` column below
	// lets a consumer re-sort exactly when boost is in play.
	entries, err := p.app.FindRecordsByFilter(
		p.config.entriesCollection(),
		"waitlist = {:wl}",
		"-referralCount,createdAt",
		0,
		0,
		dbx.Params{"wl": wl.Id},
	)
	if err != nil {
		return e.InternalServerError("query failed", err)
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"rank", "email", "refCode", "referredBy", "referralCount", "boost", "accessGranted", "score", "createdAt"})
	for i, r := range entries {
		_ = w.Write([]string{
			strconv.Itoa(i + 1),
			r.GetString("email"),
			r.GetString("refCode"),
			r.GetString("referredBy"),
			strconv.Itoa(int(r.GetFloat("referralCount"))),
			strconv.Itoa(int(r.GetFloat("boost"))),
			strconv.FormatBool(r.GetBool("accessGranted")),
			strconv.Itoa(int(entryScore(r))),
			r.GetDateTime("createdAt").String(),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return e.InternalServerError("csv encode failed", err)
	}

	e.Response.Header().Set(
		"Content-Disposition",
		fmt.Sprintf(`attachment; filename="waitlist-%s-%d.csv"`, wl.GetString("slug"), time.Now().Unix()),
	)
	return e.Blob(http.StatusOK, "text/csv; charset=utf-8", []byte(buf.String()))
}

// --- helpers ---

// findWaitlistBySlugOrID resolves slug first; falls back to id. The slug
// path covers the common case (widget posts a human-readable name).
func (p *plugin) findWaitlistBySlugOrID(slugOrID string) (*core.Record, error) {
	rec, err := p.app.FindFirstRecordByData(p.config.waitlistsCollection(), "slug", slugOrID)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// fall through to ID lookup
	return p.app.FindRecordById(p.config.waitlistsCollection(), slugOrID)
}

// scoreExpr is the SQL scalar mirroring entryScore: referral credits plus
// accumulated boost points. Kept in one place so rank and score never drift.
const scoreExpr = "(referralCount + boost)"

// score combines referral credits and boost points into a single ranking
// scalar. Higher score = better rank.
func score(referralCount, boost float64) float64 {
	return referralCount + boost
}

// entryScore reads an entry's ranking score (referralCount + boost).
func entryScore(entry *core.Record) float64 {
	return score(entry.GetFloat("referralCount"), entry.GetFloat("boost"))
}

// grantsAccess is the pure access policy: the master open switch, a sticky
// per-entry grant, or a rank within the auto-grant capacity window.
func grantsAccess(open, accessGranted bool, capacity, rank int) bool {
	return open || accessGranted || (capacity > 0 && rank <= capacity)
}

// hasAccess reports whether the entry may access the gated product at the
// given rank. When access is earned via the capacity threshold it is
// persisted (sticky) so later rank drift never revokes it. Persist errors
// are swallowed — access is a read concern and must never fail the request.
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

// isServiceAuthed reports whether the request comes from a trusted server:
// a superuser session, or a constant-time match of the shared admin secret in
// `Authorization: Bearer <AdminSecret>`.
func (p *plugin) isServiceAuthed(e *core.RequestEvent) bool {
	if e.HasSuperuserAuth() {
		return true
	}
	if p.config.AdminSecret == "" {
		return false
	}
	header := e.Request.Header.Get("Authorization")
	expected := "Bearer " + p.config.AdminSecret
	return subtle.ConstantTimeCompare([]byte(header), []byte(expected)) == 1
}

// computeRank returns this entry's 1-based rank and the total count.
// Score = referralCount + boost; tie-break on ascending createdAt — earlier
// joiners win on an equal score.
func (p *plugin) computeRank(waitlistID string, entry *core.Record) (rank int, total int, err error) {
	count, err := p.app.CountRecords(
		p.config.entriesCollection(),
		dbx.NewExp("waitlist = {:wl}", dbx.Params{"wl": waitlistID}),
	)
	if err != nil {
		return 0, 0, err
	}
	total = int(count)

	myScore := entryScore(entry)
	myCreated := entry.GetDateTime("createdAt")

	// Rank = 1 + (# entries that beat me).
	ahead, err := p.app.CountRecords(
		p.config.entriesCollection(),
		dbx.NewExp(
			"waitlist = {:wl} AND ("+scoreExpr+" > {:s} OR ("+scoreExpr+" = {:s} AND createdAt < {:t}))",
			dbx.Params{"wl": waitlistID, "s": myScore, "t": myCreated.String()},
		),
	)
	if err != nil {
		return 0, 0, err
	}
	return int(ahead) + 1, total, nil
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
