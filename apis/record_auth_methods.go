package apis

import (
	"net/http"
	"os"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/security"
)

// externalIAMAuthMethods returns the auth-methods response a client
// sees when Base is configured with an external IAM. It exposes a
// single OAuth2 provider named "iam" — generic, no brand string in
// Base code. The IAM endpoint is recovered from the JWKS URL the
// platform plugin stored at boot.
//
// IAM_DISPLAY_NAME (optional env) feeds the UI button label; if
// unset, DisplayName is empty and the UI renders a neutral fallback.
func externalIAMAuthMethods(e *core.RequestEvent) authMethodsResponse {
	jwksURL, _ := e.App.Store().Get(StoreKeyJWKSURL).(string)
	base := iamOrigin(jwksURL)

	state := security.RandomString(30)
	verifier := security.RandomString(43)
	challenge := security.S256Challenge(verifier)

	authURL := ""
	if base != "" {
		authURL = base + "/v1/iam/oauth/authorize?response_type=code&state=" + state +
			"&code_challenge=" + challenge +
			"&code_challenge_method=S256" +
			"&redirect_uri="
	}

	info := providerInfo{
		Name:                "iam",
		DisplayName:         os.Getenv("IAM_DISPLAY_NAME"),
		State:               state,
		AuthURL:             authURL,
		AuthUrl:             authURL,
		CodeVerifier:        verifier,
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	}

	resp := authMethodsResponse{
		OAuth2: oauth2Response{
			Enabled:   true,
			Providers: []providerInfo{info},
		},
	}
	resp.AuthProviders = resp.OAuth2.Providers
	return resp
}

type oauth2Response struct {
	Providers []providerInfo `json:"providers"`
	Enabled   bool           `json:"enabled"`
}

type providerInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	State       string `json:"state"`
	AuthURL     string `json:"authURL"`

	// @todo
	// deprecated: use AuthURL instead
	// AuthUrl will be removed after dropping v0.22 support
	AuthUrl string `json:"authUrl"`

	// technically could be omitted if the provider doesn't support PKCE,
	// but to avoid breaking existing typed clients we'll return them as empty string
	CodeVerifier        string `json:"codeVerifier"`
	CodeChallenge       string `json:"codeChallenge"`
	CodeChallengeMethod string `json:"codeChallengeMethod"`
}

type authMethodsResponse struct {
	OAuth2 oauth2Response `json:"oauth2"`

	// legacy alias kept so older SDK clients that read `authProviders`
	// instead of `oauth2.providers` continue to work.
	AuthProviders []providerInfo `json:"authProviders"`
}

func (amr *authMethodsResponse) fillLegacyFields() {
	if amr.OAuth2.Enabled {
		amr.AuthProviders = amr.OAuth2.Providers
	}
}

func recordAuthMethods(e *core.RequestEvent) error {
	if _, err := findAuthCollection(e); err != nil {
		return err
	}

	// Hanzo IAM is the only auth source, so every collection — including
	// _superusers — advertises exactly one generic "iam" provider pointing at
	// IAM's authorize endpoint. Base does not name the provider; IAM does.
	//
	// There is no standalone branch. It used to enumerate whatever OAuth2
	// providers a collection had registered whenever IAM was not configured,
	// which is a fallback that opens precisely when the one auth source is
	// missing. A discovery endpoint that advertises an identity provider whose
	// login route was removed is worse than one that advertises none.
	return e.JSON(http.StatusOK, externalIAMAuthMethods(e))
}
