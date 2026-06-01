// Helpers for the platform-side KMS bridge.
//
// staticBearerClient returns an http.Client whose Transport stamps a
// fixed Bearer token onto every outbound request. Used in tests and
// the legacy authToken-injected callers; production callers leave
// authToken empty and let kmsclient drive the IAM client_credentials
// exchange.

package platform

import (
	"net/http"
	"os"
	"strings"
	"time"
)

// staticBearerTransport wraps an http.RoundTripper, prepending a fixed
// Authorization header to every outbound request.
type staticBearerTransport struct {
	bearer string
	next   http.RoundTripper
}

func (t *staticBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone — never mutate the caller's request headers in-place.
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.bearer)
	return t.next.RoundTrip(r)
}

// staticBearerClient builds an http.Client that always presents the
// given bearer. The returned client also short-circuits the IAM
// /v1/iam/login/oauth/access_token call kmsclient would otherwise
// make: when the path matches, we return a synthetic response carrying
// the same bearer. This lets a caller hand us a known bearer (e.g. a
// test fixture) and never reach IAM.
func staticBearerClient(bearer string) *http.Client {
	base := http.DefaultTransport
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &iamShortCircuit{bearer: bearer, next: &staticBearerTransport{bearer: bearer, next: base}},
	}
}

// iamShortCircuit intercepts kmsclient's IAM exchange when the
// requested path is the IAM token endpoint, returning a fixed bearer.
// All other requests fall through to the underlying transport.
type iamShortCircuit struct {
	bearer string
	next   http.RoundTripper
}

func (t *iamShortCircuit) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/v1/iam/login/oauth/access_token") ||
		strings.HasSuffix(req.URL.Path, "/oauth/access_token") {
		body := strings.NewReader(`{"access_token":"` + t.bearer + `","expires_in":3600}`)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       newClosableReader(body),
			Request:    req,
		}
		return resp, nil
	}
	return t.next.RoundTrip(req)
}

// closableReader wraps a Reader with a no-op Close so it satisfies
// io.ReadCloser without leaking.
type closableReader struct {
	r interface{ Read(p []byte) (int, error) }
}

func (c *closableReader) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *closableReader) Close() error                { return nil }

func newClosableReader(r *strings.Reader) *closableReader { return &closableReader{r: r} }

// nodeIDFromEnv reads $KMS_NODE_ID, defaulting to fallback. Used as
// the ZAP transport label (mDNS / peer-table entry). NOT the auth
// identity — the auth identity is mnemonic-derived and lives in
// servicePathFromEnv.
func nodeIDFromEnv(fallback string) string {
	if v := strings.TrimSpace(os.Getenv("KMS_NODE_ID")); v != "" {
		return v
	}
	return fallback
}

// servicePathFromEnv reads $KMS_SERVICE_PATH, defaulting to fallback.
// The mnemonic-derived identity is built under this path; same path
// across pods → same NodeID byte-for-byte. The kms-operator's
// consensus-authority snapshot enumerates the NodeIDs for every
// service path.
func servicePathFromEnv(fallback string) string {
	if v := strings.TrimSpace(os.Getenv("KMS_SERVICE_PATH")); v != "" {
		return v
	}
	return fallback
}

// iamEndpointFromEnv reads $IAM_ENDPOINT, defaulting to fallback. The
// HTTP-mode kmsclient uses this for the client_credentials exchange.
func iamEndpointFromEnv(fallback string) string {
	if v := strings.TrimSpace(os.Getenv("IAM_ENDPOINT")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("IAM_URL")); v != "" {
		return v
	}
	return fallback
}

// clientIDFromEnv reads $IAM_CLIENT_ID; empty when unset.
func clientIDFromEnv() string {
	return strings.TrimSpace(os.Getenv("IAM_CLIENT_ID"))
}

// clientSecretFromEnv reads $IAM_CLIENT_SECRET; empty when unset.
func clientSecretFromEnv() string {
	return strings.TrimSpace(os.Getenv("IAM_CLIENT_SECRET"))
}
