package apis

import (
	"strings"
	"testing"
)

// The admin renders standalone at its own host and inside the Hanzo surfaces
// that offer Base as a section, so who may frame it is deployment data. These
// pin the two halves of that: the default is this origin and nobody else, and
// what a deployment says is what the header carries.
func TestFrameAncestorsDefaultsToThisOriginAlone(t *testing.T) {
	t.Setenv("BASE_FRAME_ANCESTORS", "")

	if got := frameAncestors(); got != "'self'" {
		t.Fatalf("expected the default to name this origin alone, got %q", got)
	}
}

func TestFrameAncestorsCarriesTheHostsADeploymentNames(t *testing.T) {
	const hosts = "'self' https://console.hanzo.ai https://hanzo.app"

	t.Setenv("BASE_FRAME_ANCESTORS", hosts)

	if got := frameAncestors(); got != hosts {
		t.Fatalf("expected %q, got %q", hosts, got)
	}
}

// Whitespace is what an env var picks up from YAML block scalars, and a value
// that is only whitespace is a deployment that said nothing.
func TestFrameAncestorsTreatsBlankAsUnset(t *testing.T) {
	t.Setenv("BASE_FRAME_ANCESTORS", "   ")

	if got := frameAncestors(); got != "'self'" {
		t.Fatalf("expected blank to read as unset, got %q", got)
	}
}

// The directive has to reach the policy the admin is actually served with —
// a value read into a string nothing concatenates protects nothing. This is
// the same composition apis/serve.go performs on the admin response.
func TestAdminPolicyCarriesTheFrameRule(t *testing.T) {
	t.Setenv("BASE_FRAME_ANCESTORS", "'self' https://console.hanzo.ai")

	policy := "default-src 'self'; script-src 'self' 'unsafe-inline'; frame-ancestors " + frameAncestors()

	if !strings.Contains(policy, "frame-ancestors 'self' https://console.hanzo.ai") {
		t.Fatalf("frame rule missing from the served policy: %q", policy)
	}
}
