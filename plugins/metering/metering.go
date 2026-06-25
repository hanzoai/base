// Package metering is base's prepaid pay-for-everything plugin: it gates every
// data-plane request on the caller's commerce balance (fail-closed) and records
// per-org usage (by default, per record write) to commerce — the single billing
// source of truth. It is the in-process counterpart of the metering reverse
// proxy: base's router is core's router.Router (not net/http), so instead of
// wrapping an http.Handler this binds a global router middleware that calls the
// SAME github.com/hanzoai/commerce/metering client. One metering core, two
// adapters (proxy for non-Go products, this hook for base).
//
// Identity comes from the gateway-minted X-Org-Id / X-User-Id headers (the same
// trust boundary every Hanzo service uses). Billing is per-org: the balance key
// is the org slug, exactly like the LLM gate.
//
// Opt in by registering the plugin and supplying the commerce wiring via the
// canonical env vars (COMMERCE_URL, COMMERCE_SERVICE_TOKEN, COMMERCE_SERVICE_ORG);
// the token MUST come from a KMS-backed secret. With no COMMERCE_URL the plugin
// is inert (forwards everything), so base runs unchanged until billing is wired.
package metering

import (
	"context"
	"net/http"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/commerce/metering"
)

// PriceFunc computes the cents to charge for a completed request, given its
// method, path, and final status. Return 0 to charge nothing. The default
// (DefaultPrice) charges per successful record write.
type PriceFunc func(method, path string, status int) int64

// Config configures the plugin.
type Config struct {
	// Provider labels recorded usage. Default "base".
	Provider string

	// Price computes the per-request charge. Default DefaultPrice (per record write).
	Price PriceFunc

	// Skip returns true for requests that bypass metering entirely (health,
	// auth, the admin UI). Default DefaultSkip.
	Skip func(method, path string) bool

	// Client overrides the metering client. When nil it is built from the
	// canonical env vars (metering.FromEnv) — the normal path.
	Client *metering.Client
}

// DefaultProvider is the usage label when Config.Provider is empty.
const DefaultProvider = "base"

// CentsPerRecordWrite is the default charge for a successful create/update/
// delete of a record. Reads are free (to encourage adoption); writes are the
// billable unit because they are what consumes durable per-tenant storage and
// sync/anchor work.
const CentsPerRecordWrite int64 = 1

// DefaultPrice charges CentsPerRecordWrite for a successful write to a record
// collection (POST/PATCH/DELETE under .../records), nothing otherwise. The
// batch endpoint (a single request carrying many writes) is charged once here;
// per-operation batch pricing is a future refinement.
func DefaultPrice(method, path string, status int) int64 {
	if status < 200 || status >= 400 {
		return 0 // only successful work is billed (matches the LLM gate).
	}
	if !isRecordPath(path) {
		return 0
	}
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return CentsPerRecordWrite
	default:
		return 0 // GET/HEAD/OPTIONS: reads are free.
	}
}

func isRecordPath(path string) bool {
	// matches /v1/collections/{c}/records and .../records/{id}, regardless of
	// the configured BASE_API_PREFIX (we look for the /collections/.../records
	// segment which is stable).
	i := strings.Index(path, "/collections/")
	if i < 0 {
		return false
	}
	return strings.Contains(path[i:], "/records")
}

// DefaultSkip bypasses metering for liveness, the IAM sibling, the admin UI,
// and realtime — none of which are billable data-plane work.
func DefaultSkip(_, path string) bool {
	for _, p := range []string{"/healthz", "/v1/iam/", "/_/", "/v1/realtime"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// MustRegister registers the plugin or panics. Use in main() wiring.
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register installs the prepaid metering middleware on base's data plane.
func Register(app core.App, config Config) error {
	client := config.Client
	if client == nil {
		c, err := metering.FromEnv()
		if err != nil {
			return err
		}
		client = c
	}

	provider := config.Provider
	if provider == "" {
		provider = DefaultProvider
	}
	price := config.Price
	if price == nil {
		price = DefaultPrice
	}
	skip := config.Skip
	if skip == nil {
		skip = DefaultSkip
	}

	p := &plugin{app: app, client: client, provider: provider, price: price, skip: skip}

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Global middleware: runs for every data-plane request. Bound at the
		// default middleware priority so it sits after auth (which populates the
		// identity headers) and before route handlers.
		e.Router.BindFunc(p.gate)
		return e.Next()
	})

	return nil
}

type plugin struct {
	app      core.App
	client   *metering.Client
	provider string
	price    PriceFunc
	skip     func(method, path string) bool
}

// gate is the per-request middleware: authorize (fail-closed) -> run -> record.
func (p *plugin) gate(e *core.RequestEvent) error {
	r := e.Request
	if p.skip(r.Method, r.URL.Path) {
		return e.Next()
	}

	in := metering.IdentityFromGatewayHeaders(r)

	// Pre-request balance gate. Inert (allows) when the client is not configured.
	if err := p.client.Authorize(r.Context(), in); err != nil {
		return denied(e, err)
	}

	// Run the rest of the chain (and the handler).
	if err := e.Next(); err != nil {
		return err
	}

	// Post-request record, priced by outcome. Best-effort, detached from the
	// response: the work already happened and must be billed.
	cents := p.price(r.Method, r.URL.Path, e.Status())
	if cents <= 0 || !p.client.Enabled() {
		return nil
	}
	u := metering.Usage{
		User:        in.User,  // per-org billing key
		Actor:       in.Actor, // org/sub, audit only
		Org:         in.Org,
		AmountCents: cents,
		Provider:    p.provider,
		RequestID:   r.Header.Get("X-Request-Id"),
		Status:      "success",
		ClientIP:    e.RemoteIP(), // dependency-free (no TrustedProxy settings); best-effort metadata.
	}
	go func() { _, _ = p.client.Record(context.Background(), u) }()
	return nil
}

// denied maps the gate outcome to base's JSON error shape: 402 out-of-funds,
// 503 balance-unknown (fail-closed). Same status mapping the proxy/gateway use.
func denied(e *core.RequestEvent, err error) error {
	if err == metering.ErrInsufficientBalance {
		return e.JSON(http.StatusPaymentRequired, map[string]any{
			"status":  http.StatusPaymentRequired,
			"message": "Insufficient balance. Please add credits at console.hanzo.ai",
			"data":    map[string]any{"code": "insufficient_balance"},
		})
	}
	return e.JSON(http.StatusServiceUnavailable, map[string]any{
		"status":  http.StatusServiceUnavailable,
		"message": "Billing temporarily unavailable",
		"data":    map[string]any{"code": "balance_unavailable"},
	})
}

// ensure router import is used even if the signature evolves; the plugin binds
// middleware via e.Router (router.Router[*core.RequestEvent]).
var _ = func(*router.Router[*core.RequestEvent]) {}
