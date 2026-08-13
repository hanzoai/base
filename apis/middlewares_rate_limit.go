package apis

import (
	"errors"
	"sync"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/store"
)

const (
	DefaultRateLimitMiddlewareId       = "baseRateLimit"
	DefaultRateLimitMiddlewarePriority = -1000
)

const (
	rateLimitersStoreKey       = "__hzRateLimiters__"
	rateLimitersCronKey        = "__hzRateLimitersCleanup__"
	rateLimitersSettingsHookId = "__hzRateLimitersSettingsHook__"
)

// A limit protects the process, not a tenant's data, so the rule, the counters
// and the client key all come from [core.RequestEvent.Deployment]. Reading any
// of them off e.App asked each tenant how hard the process may be hit, and a
// Base that has just been opened says "no limit at all" — so every
// authenticated caller in the estate was unlimited while anonymous callers were
// held to the rule. The 2-per-3-seconds on auth was the one that mattered and it
// applied to nobody who had a token.

// rateLimit defines the global rate limit middleware.
//
// This middleware is registered by default for all routes.
func rateLimit() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultRateLimitMiddlewareId,
		Priority: DefaultRateLimitMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if skipRateLimit(e) {
				return e.Next()
			}

			rule, ok := e.Deployment().Settings().RateLimits.FindRateLimitRule(
				defaultRateLimitLabels(e),
				defaultRateLimitAudience(e)...,
			)
			if ok {
				err := checkRateLimit(e, rule.Label+rule.Audience, rule)
				if err != nil {
					return err
				}
			}

			return e.Next()
		},
	}
}

// collectionPathRateLimit defines a rate limit middleware for the internal collection handlers.
func collectionPathRateLimit(collectionPathParam string, baseTags ...string) *hook.Handler[*core.RequestEvent] {
	if collectionPathParam == "" {
		collectionPathParam = "collection"
	}

	return &hook.Handler[*core.RequestEvent]{
		Id:       DefaultRateLimitMiddlewareId,
		Priority: DefaultRateLimitMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue(collectionPathParam))
			if err != nil {
				return e.NotFoundError("Missing or invalid collection context.", err)
			}

			if err := checkCollectionRateLimit(e, collection, baseTags...); err != nil {
				return err
			}

			return e.Next()
		},
	}
}

// checkCollectionRateLimit checks whether the current request satisfy the
// rate limit configuration for the specific collection.
//
// Each baseTags entry will be prefixed with the collection name and its wildcard variant.
func checkCollectionRateLimit(e *core.RequestEvent, collection *core.Collection, baseTags ...string) error {
	if skipRateLimit(e) {
		return nil
	}

	labels := make([]string, 0, 2+len(baseTags)*2)

	rtId := collection.Id + e.Request.Pattern

	// add first the primary labels (aka. ["collectionName:action1", "collectionName:action2"])
	for _, baseTag := range baseTags {
		rtId += baseTag
		labels = append(labels, collection.Name+":"+baseTag)
	}

	// add the wildcard labels (aka. [..., "*:action1","*:action2", "*"])
	for _, baseTag := range baseTags {
		labels = append(labels, "*:"+baseTag)
	}
	labels = append(labels, defaultRateLimitLabels(e)...)

	rule, ok := e.Deployment().Settings().RateLimits.FindRateLimitRule(labels, defaultRateLimitAudience(e)...)
	if ok {
		return checkRateLimit(e, rtId+rule.Audience, rule)
	}

	return nil
}

// -------------------------------------------------------------------

// @todo consider exporting as helper?
func checkRateLimit(e *core.RequestEvent, rtId string, rule core.RateLimitRule) error {
	switch rule.Audience {
	case core.RateLimitRuleAudienceAll:
		// valid for both guest and regular users
	case core.RateLimitRuleAudienceGuest:
		if e.Auth != nil {
			return nil
		}
	case core.RateLimitRuleAudienceAuth:
		if e.Auth == nil {
			return nil
		}
	}

	// One set of counters, on the Base the process serves from. Per-tenant
	// counters would multiply every rule by the number of orgs a caller can
	// name, so a limit of 2 would admit 2 per org and the number the operator
	// wrote would not be the number anyone was held to.
	app := e.Deployment()

	rateLimiters := app.Store().GetOrSet(rateLimitersStoreKey, func() any {
		return initRateLimitersStore(app)
	}).(*store.Store[string, *rateLimiter])
	if rateLimiters == nil {
		app.Logger().Warn("Failed to retrieve app rate limiters store")
		return nil
	}

	rt := rateLimiters.GetOrSet(rtId, func() *rateLimiter {
		return newRateLimiter(rule.MaxRequests, rule.Duration, 1800)
	})
	if rt == nil {
		app.Logger().Warn("Failed to retrieve app rate limiter", "id", rtId)
		return nil
	}

	// A limit is per client, so a request that names no client cannot be held
	// to one and is refused. The alternative is a rule that admits whatever it
	// cannot count, which reads as enforcement and is not.
	//
	// A socket peer is where the address usually comes from, and a transport
	// that terminates elsewhere and hands the request on has none. Where the
	// deployment names a header to read it from instead — Settings.TrustedProxy
	// — there is a client again and the rule applies to it.
	key := e.RealIP()
	if key == "" {
		app.Logger().Warn("Rate limited request carries no client address", "rule", rule.String())
		return e.TooManyRequestsError("", errors.New("no client address for rate limit rule: "+rule.String()))
	}

	if !rt.isAllowed(key) {
		return e.TooManyRequestsError("", errors.New("triggered rate limit rule: "+rule.String()))
	}

	return nil
}

func skipRateLimit(e *core.RequestEvent) bool {
	return !e.Deployment().Settings().RateLimits.Enabled || e.HasSuperuserAuth()
}

var defaultAuthAudience = []string{core.RateLimitRuleAudienceAll, core.RateLimitRuleAudienceAuth}
var defaultGuestAudience = []string{core.RateLimitRuleAudienceAll, core.RateLimitRuleAudienceGuest}

func defaultRateLimitAudience(e *core.RequestEvent) []string {
	if e.Auth != nil {
		return defaultAuthAudience
	}

	return defaultGuestAudience
}

func defaultRateLimitLabels(e *core.RequestEvent) []string {
	return []string{e.Request.Method + " " + e.Request.URL.Path, e.Request.URL.Path}
}

func destroyRateLimitersStore(app core.App) {
	app.OnSettingsReload().Unbind(rateLimitersSettingsHookId)
	app.Cron().Remove(rateLimitersCronKey)
	app.Store().Remove(rateLimitersStoreKey)
}

func initRateLimitersStore(app core.App) *store.Store[string, *rateLimiter] {
	app.Cron().Add(rateLimitersCronKey, "2 * * * *", func() { // offset a little since too many cleanup tasks execute at 00
		limitersStore, ok := app.Store().Get(rateLimitersStoreKey).(*store.Store[string, *rateLimiter])
		if !ok {
			return
		}
		limiters := limitersStore.GetAll()
		for _, limiter := range limiters {
			limiter.clean()
		}
	})

	app.OnSettingsReload().Bind(&hook.Handler[*core.SettingsReloadEvent]{
		Id: rateLimitersSettingsHookId,
		Func: func(e *core.SettingsReloadEvent) error {
			err := e.Next()
			if err != nil {
				return err
			}

			// reset
			destroyRateLimitersStore(e.App)

			return nil
		},
	})

	return store.New[string, *rateLimiter](nil)
}

func newRateLimiter(maxAllowed int, intervalInSec int64, minDeleteIntervalInSec int64) *rateLimiter {
	return &rateLimiter{
		maxAllowed:        maxAllowed,
		interval:          intervalInSec,
		minDeleteInterval: minDeleteIntervalInSec,
		clients:           map[string]*rateClient{},
	}
}

type rateLimiter struct {
	clients map[string]*rateClient

	maxAllowed        int
	interval          int64
	minDeleteInterval int64
	totalDeleted      int64

	sync.RWMutex
}

//nolint:unused
func (rt *rateLimiter) getClient(key string) (*rateClient, bool) {
	rt.RLock()
	client, ok := rt.clients[key]
	rt.RUnlock()

	return client, ok
}

func (rt *rateLimiter) isAllowed(key string) bool {
	// lock only reads to minimize locks contention
	rt.RLock()
	client, ok := rt.clients[key]
	rt.RUnlock()

	if !ok {
		rt.Lock()
		// check again in case the client was added by another request
		client, ok = rt.clients[key]
		if !ok {
			client = newRateClient(rt.maxAllowed, rt.interval)
			rt.clients[key] = client
		}
		rt.Unlock()
	}

	return client.consume()
}

func (rt *rateLimiter) clean() {
	rt.Lock()
	defer rt.Unlock()

	nowUnix := time.Now().Unix()

	for k, client := range rt.clients {
		if client.hasExpired(nowUnix, rt.minDeleteInterval) {
			delete(rt.clients, k)
			rt.totalDeleted++
		}
	}

	// "shrink" the map if too may items were deleted
	//
	// @todo remove after https://github.com/golang/go/issues/20135
	if rt.totalDeleted >= 300 {
		shrunk := make(map[string]*rateClient, len(rt.clients))
		for k, v := range rt.clients {
			shrunk[k] = v
		}
		rt.clients = shrunk
		rt.totalDeleted = 0
	}
}

func newRateClient(maxAllowed int, intervalInSec int64) *rateClient {
	return &rateClient{
		maxAllowed: maxAllowed,
		interval:   intervalInSec,
	}
}

// @todo evaluate swiching to sliding window with approximation counter similar to Cloudflare.
//
// rateClient implements fixed window rate limit strategy.
type rateClient struct {
	// use plain Mutex instead of RWMutex since the operations are expected
	// to be mostly writes (e.g. consume()) and it should perform better
	sync.Mutex

	maxAllowed int   // the max allowed tokens per interval
	available  int   // the total available tokens
	start      int64 // the start time of the current window
	interval   int64 // in seconds
}

// hasExpired checks whether it has been at least minElapsed seconds after the last active window.
// (usually used to perform periodic cleanup of staled instances).
func (l *rateClient) hasExpired(relativeNow int64, minElapsed int64) bool {
	l.Lock()
	defer l.Unlock()

	return relativeNow-(l.start+l.interval) > minElapsed
}

// consume decreases the current allowance with 1 (if not exhausted already).
//
// It returns false if the allowance has been already exhausted and the user
// has to wait until it resets back to its maxAllowed value.
func (l *rateClient) consume() bool {
	l.Lock()
	defer l.Unlock()

	nowUnix := time.Now().Unix()

	// reset
	if nowUnix-l.start >= l.interval {
		l.available = l.maxAllowed
		l.start = nowUnix
	}

	if l.available > 0 {
		l.available--
		return true
	}

	return false
}
