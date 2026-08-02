package claims

import (
	"net/http"
	"testing"
)

// Every header the gateway strips must also be stripped here. This list is
// duplicated from gateway/iamauth.StripIdentityHeaderNames because the two
// modules do not import each other — which is exactly why it needs a test:
// the copies drifted, and the ones that went missing were the payer
// (X-Billing-Account-Id, X-User-Owner) and the permission set. A service
// behind this middleware and not behind the gateway trusted whatever the
// client sent.
func TestStripsEveryGatewayIdentityHeader(t *testing.T) {
	mustStrip := []string{
		"X-User-Id", "X-Org-Id", "X-User-Owner", "X-Roles",
		"X-User-Permissions", "X-User-Email", "X-Phone-Number",
		"X-User-IsAdmin", "X-User-IsGlobalAdmin", "X-Project-Id",
		"X-Billing-Account-Id", "X-User-Role", "X-User-Roles",
		"X-User-Name", "X-Tenant-Id", "X-Tenant-ID", "X-Org",
	}

	h := http.Header{}
	for _, name := range mustStrip {
		h.Set(name, "forged-by-the-client")
	}
	StripIdentityHeaders(h)

	for _, name := range mustStrip {
		if got := h.Get(name); got != "" {
			t.Errorf("%s survived as %q — a client can forge it", name, got)
		}
	}
}
