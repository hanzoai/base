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
	Waitlist       string `json:"waitlist"`       // slug (preferred) or id
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
		ShareURL:      "?ref=" + entry.GetString("refCode"),
	})
}

// --- GET /v1/waitlist/export ---

func (p *plugin) handleExport(e *core.RequestEvent) error {
	// Superuser auth OR shared admin secret in Authorization header.
	if !e.HasSuperuserAuth() {
		if p.config.AdminSecret == "" {
			return e.NotFoundError("export disabled", nil)
		}
		header := e.Request.Header.Get("Authorization")
		expected := "Bearer " + p.config.AdminSecret
		if subtle.ConstantTimeCompare([]byte(header), []byte(expected)) != 1 {
			return e.UnauthorizedError("admin auth required", nil)
		}
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
	_ = w.Write([]string{"rank", "email", "refCode", "referredBy", "referralCount", "createdAt"})
	for i, r := range entries {
		_ = w.Write([]string{
			strconv.Itoa(i + 1),
			r.GetString("email"),
			r.GetString("refCode"),
			r.GetString("referredBy"),
			strconv.Itoa(int(r.GetFloat("referralCount"))),
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

// computeRank returns this entry's 1-based rank and the total count.
// Tie-break: ascending createdAt — earlier joiners win on a tie.
func (p *plugin) computeRank(waitlistID string, entry *core.Record) (rank int, total int, err error) {
	count, err := p.app.CountRecords(
		p.config.entriesCollection(),
		dbx.NewExp("waitlist = {:wl}", dbx.Params{"wl": waitlistID}),
	)
	if err != nil {
		return 0, 0, err
	}
	total = int(count)

	myCount := entry.GetFloat("referralCount")
	myCreated := entry.GetDateTime("createdAt")

	// Rank = 1 + (# entries that beat me).
	ahead, err := p.app.CountRecords(
		p.config.entriesCollection(),
		dbx.NewExp(
			"waitlist = {:wl} AND (referralCount > {:c} OR (referralCount = {:c} AND createdAt < {:t}))",
			dbx.Params{"wl": waitlistID, "c": myCount, "t": myCreated.String()},
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
