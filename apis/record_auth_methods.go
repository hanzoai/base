package apis

import (
	"net/http"
	"os"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/auth"
	"github.com/hanzoai/base/tools/security"
	"golang.org/x/oauth2"
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
	base := strings.TrimSuffix(jwksURL, "/.well-known/jwks")

	state := security.RandomString(30)
	verifier := security.RandomString(43)
	challenge := security.S256Challenge(verifier)

	authURL := ""
	if base != "" {
		authURL = base + "/oauth/authorize?response_type=code&state=" + state +
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
	collection, err := findAuthCollection(e)
	if err != nil {
		return err
	}

	// In IAM-native mode (the only mode the platform plugin allows)
	// every collection — including _superusers — advertises a single
	// generic "iam" OAuth2 provider. Base does not name the provider;
	// IAM does. The local IAM_DISPLAY_NAME env (if set) becomes the
	// UI label; otherwise the client renders a neutral "Sign in"
	// button.
	if externalOnly, _ := e.App.Store().Get(StoreKeyExternalAuthOnly).(bool); externalOnly {
		return e.JSON(http.StatusOK, externalIAMAuthMethods(e))
	}

	// Standalone Base (no IAM configured) — keep the OAuth2 provider
	// discovery response so the admin panel can still show whatever
	// providers were registered on the collection.
	result := authMethodsResponse{
		OAuth2: oauth2Response{
			Providers: make([]providerInfo, 0, len(collection.OAuth2.Providers)),
		},
	}

	if !collection.OAuth2.Enabled {
		result.fillLegacyFields()

		return e.JSON(http.StatusOK, result)
	}

	result.OAuth2.Enabled = true

	for _, config := range collection.OAuth2.Providers {
		provider, err := config.InitProvider()
		if err != nil {
			e.App.Logger().Debug(
				"Failed to setup OAuth2 provider",
				"name", config.Name,
				"error", err.Error(),
			)
			continue // skip provider
		}

		info := providerInfo{
			Name:        config.Name,
			DisplayName: provider.DisplayName(),
			State:       security.RandomString(30),
		}

		if info.DisplayName == "" {
			info.DisplayName = config.Name
		}

		urlOpts := []oauth2.AuthCodeOption{}

		// custom providers url options
		switch config.Name {
		case auth.NameApple:
			// see https://developer.apple.com/documentation/sign_in_with_apple/sign_in_with_apple_js/incorporating_sign_in_with_apple_into_other_platforms#3332113
			urlOpts = append(urlOpts, oauth2.SetAuthURLParam("response_mode", "form_post"))
		}

		if provider.PKCE() {
			info.CodeVerifier = security.RandomString(43)
			info.CodeChallenge = security.S256Challenge(info.CodeVerifier)
			info.CodeChallengeMethod = "S256"
			urlOpts = append(urlOpts,
				oauth2.SetAuthURLParam("code_challenge", info.CodeChallenge),
				oauth2.SetAuthURLParam("code_challenge_method", info.CodeChallengeMethod),
			)
		}

		info.AuthURL = provider.BuildAuthURL(
			info.State,
			urlOpts...,
		) + "&redirect_uri=" // empty redirect_uri so that users can append their redirect url

		info.AuthUrl = info.AuthURL

		result.OAuth2.Providers = append(result.OAuth2.Providers, info)
	}

	result.fillLegacyFields()

	return e.JSON(http.StatusOK, result)
}
