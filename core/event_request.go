package core

import (
	"maps"
	"net/netip"
	"strings"
	"sync"

	"github.com/hanzoai/base/tools/inflector"
	"github.com/hanzoai/base/tools/router"
)

// Common request store keys used by the middlewares and api handlers.
const (
	RequestEventKeyInfoContext = "infoContext"

	// RequestEventKeyDeployment carries the Base the process serves from, put
	// on every request by the router's event factory before anything can move
	// it onto a tenant's. Read it through [RequestEvent.Deployment].
	RequestEventKeyDeployment = "__deployment"
)

// RequestEvent defines the Base router handler event.
type RequestEvent struct {
	App App

	cachedRequestInfo *RequestInfo

	Auth *Record

	router.Event

	mu sync.Mutex
}

// Deployment is the Base the process serves from.
//
// It is not e.App, which answers a different question — which Base this
// request's records are in — and moves onto a tenant's the moment a credential
// names an org. Anything that is a property of the process rather than of a
// tenant's data reads it here: the rate limit and its counters, the activity
// log, the batch size, and which proxy headers are believed. Asking e.App for
// those let a tenant answer a question about the deployment, and a Base that
// has just been opened answers with the zero value.
//
// Falling back to e.App keeps a request built outside the router — a test, a
// batch child — reading the only Base it knows.
func (e *RequestEvent) Deployment() App {
	if app, ok := e.Get(RequestEventKeyDeployment).(App); ok && app != nil {
		return app
	}
	return e.App
}

// RealIP returns the "real" IP address from the configured trusted proxy headers.
//
// If Settings.TrustedProxy is not configured or the found IP is empty,
// it fallbacks to e.RemoteIP().
//
// Which proxies are believed is the deployment's answer and never a tenant's.
// Only a superuser writes settings and a tenant's Base has no superuser, so a
// tenant's TrustedProxy is empty by construction and can never be anything
// else; reading it there meant that behind an ingress every authenticated
// request reported the ingress address, and everything keyed on the client —
// the rate limit above all — put the whole estate in one bucket.
//
// NB!
// Be careful when used in a security critical context as it relies on
// the trusted proxy to be properly configured and your app to be accessible only through it.
// If you are not sure, use e.RemoteIP().
func (e *RequestEvent) RealIP() string {
	settings := e.Deployment().Settings()

	for _, h := range settings.TrustedProxy.Headers {
		headerValues := e.Request.Header.Values(h)
		if len(headerValues) == 0 {
			continue
		}

		// extract the last header value as it is expected to be the one controlled by the proxy
		ipsList := headerValues[len(headerValues)-1]
		if ipsList == "" {
			continue
		}

		ips := strings.Split(ipsList, ",")

		if settings.TrustedProxy.UseLeftmostIP {
			for _, ip := range ips {
				parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
				if err == nil {
					return parsed.StringExpanded()
				}
			}
		} else {
			for i := len(ips) - 1; i >= 0; i-- {
				parsed, err := netip.ParseAddr(strings.TrimSpace(ips[i]))
				if err == nil {
					return parsed.StringExpanded()
				}
			}
		}
	}

	return e.RemoteIP()
}

// HasSuperuserAuth checks whether the current RequestEvent has superuser authentication loaded.
func (e *RequestEvent) HasSuperuserAuth() bool {
	return e.Auth != nil && e.Auth.IsSuperuser()
}

// RequestInfo parses the current request into RequestInfo instance.
//
// Note that the returned result is cached to avoid copying the request data multiple times
// but the auth state and other common store items are always refreshed in case they were changed by another handler.
func (e *RequestEvent) RequestInfo() (*RequestInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cachedRequestInfo != nil {
		e.cachedRequestInfo.Auth = e.Auth

		infoCtx, _ := e.Get(RequestEventKeyInfoContext).(string)
		if infoCtx != "" {
			e.cachedRequestInfo.Context = infoCtx
		} else {
			e.cachedRequestInfo.Context = RequestInfoContextDefault
		}
	} else {
		// (re)init e.cachedRequestInfo based on the current request event
		if err := e.initRequestInfo(); err != nil {
			return nil, err
		}
	}

	return e.cachedRequestInfo, nil
}

func (e *RequestEvent) initRequestInfo() error {
	infoCtx, _ := e.Get(RequestEventKeyInfoContext).(string)
	if infoCtx == "" {
		infoCtx = RequestInfoContextDefault
	}

	info := &RequestInfo{
		Context: infoCtx,
		Method:  e.Request.Method,
		Query:   map[string]string{},
		Headers: map[string]string{},
		Body:    map[string]any{},
	}

	if err := e.BindBody(&info.Body); err != nil {
		return err
	}

	// extract the first value of all query params
	query := e.Request.URL.Query()
	for k, v := range query {
		if len(v) > 0 {
			info.Query[k] = v[0]
		}
	}

	// extract the first value of all headers and normalizes the keys
	// ("X-Token" is converted to "x_token")
	for k, v := range e.Request.Header {
		if len(v) > 0 {
			info.Headers[inflector.Snakecase(k)] = v[0]
		}
	}

	info.Auth = e.Auth

	e.cachedRequestInfo = info

	return nil
}

// -------------------------------------------------------------------

const (
	RequestInfoContextDefault       = "default"
	RequestInfoContextExpand        = "expand"
	RequestInfoContextRealtime      = "realtime"
	RequestInfoContextProtectedFile = "protectedFile"
	RequestInfoContextBatch         = "batch"
	RequestInfoContextOAuth2        = "oauth2"
	RequestInfoContextOTP           = "otp"
	RequestInfoContextPasswordAuth  = "password"
)

// RequestInfo defines a HTTP request data struct, usually used
// as part of the `@request.*` filter resolver.
//
// The Query and Headers fields contains only the first value for each found entry.
type RequestInfo struct {
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
	Auth    *Record           `json:"auth"`
	Method  string            `json:"method"`
	Context string            `json:"context"`
}

// HasSuperuserAuth checks whether the current RequestInfo instance
// has superuser authentication loaded.
func (info *RequestInfo) HasSuperuserAuth() bool {
	return info.Auth != nil && info.Auth.IsSuperuser()
}

// Clone creates a new shallow copy of the current RequestInfo and its Auth record (if any).
func (info *RequestInfo) Clone() *RequestInfo {
	clone := &RequestInfo{
		Method:  info.Method,
		Context: info.Context,
		Query:   maps.Clone(info.Query),
		Body:    maps.Clone(info.Body),
		Headers: maps.Clone(info.Headers),
	}

	if info.Auth != nil {
		clone.Auth = info.Auth.Fresh()
	}

	return clone
}
