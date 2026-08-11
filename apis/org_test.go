package apis

import (
	"testing"

	"github.com/hanzoai/authz"
)

// TestOrgOf pins which org a token acts in, one claim shape at a time.
//
// The case that matters most is the third: `owner` carries the org of the
// application a token was minted through, so a token for alpha minted through a
// hanzo application says owner=hanzo. Reading it put every tenant of one
// application on one Base.
func TestOrgOf(t *testing.T) {
	member := func(orgs ...string) *authz.Claims {
		c := &authz.Claims{Owner: "hanzo"}
		for _, o := range orgs {
			c.Orgs = append(c.Orgs, authz.Membership{Org: o, Role: authz.Member})
		}
		return c
	}

	cases := []struct {
		name    string
		claims  *authz.Claims
		stated  string
		want    string
		refused bool
	}{
		{
			name:   "nothing stated is the home org",
			claims: member("alpha", "beta"),
			want:   "alpha",
		},
		{
			name:   "an org the token carries",
			claims: member("alpha", "beta"),
			stated: "beta",
			want:   "beta",
		},
		{
			name:    "an org the token does not carry",
			claims:  member("alpha"),
			stated:  "beta",
			refused: true,
		},
		{
			name:    "the org of the application the token was minted through",
			claims:  member("alpha"),
			stated:  "hanzo",
			refused: true,
		},
		{
			name:    "a machine, which carries no membership at all",
			claims:  &authz.Claims{Owner: "hanzo", TokenType: "access-token"},
			refused: true,
		},
		{
			name:    "a machine naming an org",
			claims:  &authz.Claims{Owner: "hanzo"},
			stated:  "hanzo",
			refused: true,
		},
		{
			// The reserved org is an org like any other here. What is special
			// about it is which Base it names, and that is the Bases' business.
			name:   "a platform operator, at home in the reserved org",
			claims: member(authz.AdminOrg, "alpha"),
			want:   authz.AdminOrg,
		},
		{
			// Not a fold: "Admin" and "admin " are orgs someone can register.
			name:    "an org that merely looks like the reserved one",
			claims:  member("alpha"),
			stated:  "Admin",
			refused: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := orgOf(c.claims, c.stated)
			if c.refused {
				if err == nil {
					t.Fatalf("got %q, want a refusal", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestOperatorMayReachATenant states the one cross-tenant scope out loud.
//
// A member of the reserved admin org is the estate's SuperAdmin, and
// authz.EffectiveOrg admits its selection of any org. That is deliberate and it
// is the only way a request reaches a Base its subject is not a member of, so
// it is written down here rather than left to be discovered.
func TestOperatorMayReachATenant(t *testing.T) {
	operator := &authz.Claims{Orgs: []authz.Membership{
		{Org: authz.AdminOrg, Role: authz.Admin},
	}}

	got, err := orgOf(operator, "alpha")
	if err != nil {
		t.Fatalf("an operator was refused a tenant: %v", err)
	}
	if got != "alpha" {
		t.Fatalf("got %q, want alpha", got)
	}

	// A tenant admin holds no part of it.
	tenant := &authz.Claims{IsAdmin: true, Orgs: []authz.Membership{
		{Org: "alpha", Role: authz.Admin},
	}}
	if got, err := orgOf(tenant, "beta"); err == nil {
		t.Fatalf("an org admin reached beta and got %q", got)
	}
}
