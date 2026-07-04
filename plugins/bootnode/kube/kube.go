// Package kube is a dependency-free Kubernetes REST client scoped to exactly
// what the bootnode plugin needs: server-side-apply of namespaced custom
// resources (bootno.de/v1 Network, NodeFleet, KMSSecret).
//
// It deliberately avoids k8s.io/client-go and its transitive dependency tree.
// The bootnode plugin applies a handful of CR kinds; a full typed client is
// overkill. This client talks to the apiserver over net/http using the
// in-cluster service-account token + CA (the standard pod identity), falling
// back to KUBE_APISERVER + KUBE_TOKEN env for out-of-cluster development.
//
// Apply uses Kubernetes server-side apply (PATCH with
// application/apply-patch+yaml). It is idempotent: applying the same object
// twice converges to the same state with bootnode as the field manager.
package kube

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"     //nolint:gosec // standard k8s path, not a credential
	saCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"    //nolint:gosec // standard k8s path
	saNSPath    = "/var/run/secrets/kubernetes.io/serviceaccount/namespace" //nolint:gosec // standard k8s path
	fieldOwner  = "bootnode"
)

// Client applies namespaced custom resources to a Kubernetes cluster.
type Client struct {
	apiServer string
	token     string
	namespace string
	http      *http.Client
	available bool
}

// New constructs a Client. It resolves cluster access in this order:
//
//  1. In-cluster service account (token + CA + namespace files).
//  2. KUBE_APISERVER + KUBE_TOKEN environment variables (dev / out-of-cluster).
//
// New never fails: when no cluster is reachable the Client is marked
// unavailable and [Client.Available] returns false. Callers decide whether a
// missing cluster is fatal for a given request (it is for network/node
// provisioning, surfaced as a 503).
func New(defaultNamespace string) *Client {
	c := &Client{
		namespace: defaultNamespace,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
	if defaultNamespace == "" {
		c.namespace = "bootnode"
	}

	// In-cluster path.
	if token, err := os.ReadFile(saTokenPath); err == nil && len(token) > 0 {
		c.token = strings.TrimSpace(string(token))
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host != "" {
			if port == "" {
				port = "443"
			}
			c.apiServer = "https://" + net_JoinHostPort(host, port)
		}
		// Adopt the pod's own namespace ONLY when the caller didn't specify one —
		// an explicit namespace must win over the ambient serviceaccount namespace
		// (otherwise an in-cluster runner, e.g. arc-system, silently overrides it).
		if defaultNamespace == "" {
			if ns, err := os.ReadFile(saNSPath); err == nil {
				c.namespace = strings.TrimSpace(string(ns))
			}
		}
		if ca, err := os.ReadFile(saCAPath); err == nil {
			c.http.Transport = caTransport(ca)
		}
		c.available = c.apiServer != ""
		return c
	}

	// Out-of-cluster dev path.
	if api := os.Getenv("KUBE_APISERVER"); api != "" {
		c.apiServer = strings.TrimRight(api, "/")
		c.token = os.Getenv("KUBE_TOKEN")
		if os.Getenv("KUBE_INSECURE_SKIP_TLS_VERIFY") == "true" {
			c.http.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in dev only
		}
		c.available = true
	}

	return c
}

// caTransport returns an http.Transport that trusts the given CA bundle.
func caTransport(ca []byte) *http.Transport {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	return &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
}

// net_JoinHostPort wraps IPv6 hosts in brackets. Kept local to avoid a net
// import solely for one call shape.
func net_JoinHostPort(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// SetTarget overrides the apiserver and bearer token explicitly, marking the
// client available. Used for out-of-cluster wiring where the target is known
// programmatically (and by tests pointing at a stub apiserver).
func SetTarget(c *Client, apiServer, token string) {
	c.apiServer = strings.TrimRight(apiServer, "/")
	c.token = token
	c.available = apiServer != ""
}

// Available reports whether a Kubernetes cluster was resolved at construction.
func (c *Client) Available() bool { return c != nil && c.available }

// Namespace returns the namespace the client applies resources into.
func (c *Client) Namespace() string { return c.namespace }

// CustomResource is the minimal shape of a namespaced custom resource. Apply
// fills in apiVersion/kind/namespace, so callers provide Name + Spec.
type CustomResource struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   Metadata       `json:"metadata"`
	Spec       map[string]any `json:"spec"`
}

// Metadata is the object metadata Apply sets.
type Metadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// GVR identifies a custom resource's group/version/plural for URL building.
type GVR struct {
	Group    string
	Version  string
	Plural   string
	Kind     string
	Singular string // used for apiVersion display only
}

// bootno.de/v1 resources the bootnode plugin manages.
var (
	NetworkGVR   = GVR{Group: "bootno.de", Version: "v1", Plural: "networks", Kind: "Network"}
	NodeFleetGVR = GVR{Group: "bootno.de", Version: "v1", Plural: "nodefleets", Kind: "NodeFleet"}
	KMSSecretGVR = GVR{Group: "bootno.de", Version: "v1", Plural: "kmssecrets", Kind: "KMSSecret"}
)

func (g GVR) apiVersion() string { return g.Group + "/" + g.Version }

func (g GVR) namespacedPath(namespace, name string) string {
	p := fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s", g.Group, g.Version, url.PathEscape(namespace), g.Plural)
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

// Apply performs a server-side apply of the given custom resource. It is
// idempotent. spec is the resource's .spec; labels are attached to metadata.
//
// Returns the applied object's name on success.
func (c *Client) Apply(ctx context.Context, gvr GVR, name string, labels map[string]string, spec map[string]any) (string, error) {
	if !c.Available() {
		return "", fmt.Errorf("kube: no cluster configured (in-cluster SA or KUBE_APISERVER required)")
	}
	if name == "" {
		return "", fmt.Errorf("kube: resource name is required")
	}

	obj := CustomResource{
		APIVersion: gvr.apiVersion(),
		Kind:       gvr.Kind,
		Metadata: Metadata{
			Name:      name,
			Namespace: c.namespace,
			Labels:    labels,
		},
		Spec: spec,
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("kube: marshal %s/%s: %w", gvr.Kind, name, err)
	}

	// Server-side apply: PATCH the named object with fieldManager + force.
	path := gvr.namespacedPath(c.namespace, name)
	q := url.Values{"fieldManager": {fieldOwner}, "force": {"true"}}
	full := c.apiServer + path + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, full, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("kube: build apply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/apply-patch+yaml") // SSA accepts JSON-encoded body
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("kube: apply %s/%s: %w", gvr.Kind, name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("kube: apply %s/%s returned %d: %s", gvr.Kind, name, resp.StatusCode, truncate(string(raw), 256))
	}
	return name, nil
}

// Get fetches a named custom resource and decodes it into out. Returns
// (false, nil) when the resource does not exist.
func (c *Client) Get(ctx context.Context, gvr GVR, name string, out any) (bool, error) {
	if !c.Available() {
		return false, fmt.Errorf("kube: no cluster configured")
	}
	full := c.apiServer + gvr.namespacedPath(c.namespace, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return false, fmt.Errorf("kube: build get request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("kube: get %s/%s: %w", gvr.Kind, name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("kube: get %s/%s returned %d: %s", gvr.Kind, name, resp.StatusCode, truncate(string(raw), 256))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return false, fmt.Errorf("kube: decode %s/%s: %w", gvr.Kind, name, err)
		}
	}
	return true, nil
}

// Delete removes a named custom resource. A 404 is treated as success
// (idempotent delete).
func (c *Client) Delete(ctx context.Context, gvr GVR, name string) error {
	if !c.Available() {
		return fmt.Errorf("kube: no cluster configured")
	}
	full := c.apiServer + gvr.namespacedPath(c.namespace, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, full, nil)
	if err != nil {
		return fmt.Errorf("kube: build delete request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kube: delete %s/%s: %w", gvr.Kind, name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode < 400 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return fmt.Errorf("kube: delete %s/%s returned %d: %s", gvr.Kind, name, resp.StatusCode, truncate(string(raw), 256))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
