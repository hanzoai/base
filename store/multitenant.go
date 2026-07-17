// Package store implements the canonical per-tenant SQLite storage model
// described in hanzo/ARCHITECTURE.md §5: a composable org / app / project /
// user isolation hierarchy (see the tenant-data-hierarchy HIP).
//
// There is exactly one way to fetch the SQLite handle for the current
// request:
//
//	db, err := store.ForCtx(r.Context())
//
// Which goes through this package's MultiTenantStore. Behind the scenes the
// store hydrates the right DB from object storage, caches it in an LRU, and
// checkpoints dirty DBs back on eviction / shutdown.
//
// No handler ever opens SQLite directly. No handler ever reads object
// storage directly. No handler ever knows whether its SQLite file is in
// memory or in a bucket.
//
// # Consistency model (v1)
//
// A single tenant is served by exactly one pod at a time via
// gateway-side sticky-session affinity (consistent hash on X-Org-Id). The
// store does NOT perform object-storage CAS (ETag If-Match) on upload —
// see [CAS]. During an HPA rebalance a (short) single-writer window may
// overlap across pods; ops MUST drain before scaling. This is the
// "at-most-once delivery under rebalance" model. The ARCHITECTURE.md §5.5
// document describes this contract verbatim; consumers that require
// generation-checked writes must wait for the CAS follow-up slice.
package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/hanzoai/base/tools/claims"
	"github.com/hanzoai/base/tools/filesystem"
	"github.com/hanzoai/dbx"
	"github.com/hanzoai/sqlite"
	"github.com/luxfi/cache/bloom"
	"github.com/luxfi/cache/lru"
)

// Key is the tuple that identifies a per-tenant SQLite DB.
//
// The tenant space is a composable hierarchy under one org: an org-wide DB,
// a per-app DB, a per-app-per-project DB, and a per-user DB. Splitting the
// space this way lets us keep a single cache and a single object-storage
// layout for every shape. Scope selects which fields are significant:
//
//	ScopeOrg      OrgID
//	ScopeApp      OrgID, App
//	ScopeProject  OrgID, App, Project
//	ScopeUser     OrgID, UserID
type Key struct {
	OrgID   string
	UserID  string // set when Scope == ScopeUser
	App     string // set when Scope == ScopeApp or ScopeProject
	Project string // set when Scope == ScopeProject
	Scope   Scope
}

// Scope selects which tier of the org's tenant hierarchy a Key addresses.
type Scope int

const (
	ScopeUser Scope = iota
	ScopeOrg
	ScopeApp
	ScopeProject
)

// CAS reports whether this package performs object-storage compare-and-swap
// on upload (ETag If-Match). In v1 CAS is false — single-writer is provided
// by gateway sticky-session affinity on X-Org-Id. Consumers that require
// generation-checked writes must feature-gate on this constant and refuse
// to boot until it flips true.
const CAS = false

// sqliteMagic is the 16-byte SQLite 3 file header. Every valid SQLite file
// begins with exactly these bytes; empty files are accepted as the "fresh
// tenant" state. Anything else is rejected on hydrate.
var sqliteMagic = []byte("SQLite format 3\x00")

// Sentinel errors surfaced to callers. They are errors.Is-comparable and
// MUST NOT be wrapped out of recognition.
var (
	// ErrCorruptDB is returned from hydrate when the downloaded object is
	// non-empty and does not begin with the SQLite magic header. Callers
	// must NOT retry blindly — the bucket contents are hostile or
	// corrupted; an operator has to triage.
	ErrCorruptDB = errors.New("store: downloaded object is not a SQLite database (header mismatch)")

	// ErrUploadFailed wraps any object-storage upload failure surfaced
	// from Evict / Close / Checkpoint. The handle is retained in the
	// cache so the next reap cycle can retry.
	ErrUploadFailed = errors.New("store: upload failed")

	// ErrClosed is returned when Get is called after Close.
	ErrClosed = errors.New("store: closed")

	// ErrCrossTenant is returned when an authenticated caller asks for a Key
	// that does not belong to the caller's org. The claims->OrgID binding is
	// the first-line cross-tenant boundary; per-org KMS key separation is the
	// second. errors.Is-comparable.
	ErrCrossTenant = errors.New("store: cross-tenant access denied")
)

// String implements fmt.Stringer for log lines and metric labels.
func (k Key) String() string {
	switch k.Scope {
	case ScopeOrg:
		return k.OrgID + "/org"
	case ScopeApp:
		return k.OrgID + "/apps/" + k.App
	case ScopeProject:
		return k.OrgID + "/apps/" + k.App + "/projects/" + k.Project
	default: // ScopeUser
		return k.OrgID + "/users/" + k.UserID
	}
}

// ObjectKey returns the object-storage path for the DB.
//
//	org-scoped:      {org}/org.db
//	app-scoped:      {org}/apps/{app}.db
//	project-scoped:  {org}/apps/{app}/projects/{project}.db
//	user-scoped:     {org}/users/{user}.db
func (k Key) ObjectKey() string {
	switch k.Scope {
	case ScopeOrg:
		return filepath.ToSlash(filepath.Join(k.OrgID, "org.db"))
	case ScopeApp:
		return filepath.ToSlash(filepath.Join(k.OrgID, "apps", k.App+".db"))
	case ScopeProject:
		return filepath.ToSlash(filepath.Join(k.OrgID, "apps", k.App, "projects", k.Project+".db"))
	default: // ScopeUser
		return filepath.ToSlash(filepath.Join(k.OrgID, "users", k.UserID+".db"))
	}
}

// LocalPath returns the in-pod filesystem path for the DB. It mirrors
// ObjectKey under cacheRoot so the on-disk cache and the bucket share one
// layout — the path structure lives in exactly one place.
func (k Key) LocalPath(cacheRoot string) string {
	return filepath.Join(cacheRoot, filepath.FromSlash(k.ObjectKey()))
}

// Valid reports whether the key is well-formed. Callers MUST validate keys
// built from HTTP headers before passing them to the store — otherwise a
// crafted org / app / project slug can reach the filesystem with `..`.
// Every slug significant to the Scope passes the SAME validateSlug guard,
// so no tier can escape cacheRoot.
func (k Key) Valid() error {
	if err := validateSlug(k.OrgID); err != nil {
		return fmt.Errorf("OrgID: %w", err)
	}
	switch k.Scope {
	case ScopeUser:
		if err := validateSlug(k.UserID); err != nil {
			return fmt.Errorf("UserID: %w", err)
		}
	case ScopeProject:
		if err := validateSlug(k.Project); err != nil {
			return fmt.Errorf("Project: %w", err)
		}
		fallthrough
	case ScopeApp:
		if err := validateSlug(k.App); err != nil {
			return fmt.Errorf("App: %w", err)
		}
	}
	return nil
}

// MaxSlugLen caps every tenant slug length. 128 bytes is generous: IAM ULIDs
// are 26 chars, UUIDs 36. A slug longer than this is either a mistake or an
// attempt to blow up filesystem error messages with attacker-controlled
// bytes — reject before any FS call.
const MaxSlugLen = 128

// validateSlug enforces `[a-z0-9_-]+` and length ≤ MaxSlugLen, rejecting
// path traversal, mixed case, whitespace, and oversized inputs. Every
// tenant slug (org, app, project, user) lives under this contract in IAM.
func validateSlug(s string) error {
	if s == "" {
		return errors.New("empty")
	}
	if len(s) > MaxSlugLen {
		return fmt.Errorf("too long (%d > %d)", len(s), MaxSlugLen)
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return fmt.Errorf("invalid character %q", c)
		}
	}
	return nil
}

// validateSQLiteMagic checks the first 16 bytes of a downloaded blob.
// Empty blobs are allowed (represents a fresh tenant slot). A non-empty
// blob that doesn't start with the SQLite 3 magic header is rejected as
// ErrCorruptDB — no handle is opened, no further byte is read.
func validateSQLiteMagic(header []byte) error {
	if len(header) == 0 {
		return nil
	}
	if len(header) < len(sqliteMagic) {
		return ErrCorruptDB
	}
	for i, b := range sqliteMagic {
		if header[i] != b {
			return ErrCorruptDB
		}
	}
	return nil
}

// DBConnect opens a SQLite file and returns the dbx builder. The default
// matches core.DefaultDBConnect: WAL, busy_timeout, NORMAL sync, FK on.
type DBConnect func(path string) (*dbx.DB, error)

// Options configures the MultiTenantStore. All fields have defaults, and the
// only required one is ObjectStore.
type Options struct {
	// ObjectStore is the durable, cross-pod blob store. Production uses
	// filesystem.NewS3(...) / NewGCS(...); tests can pass filesystem.NewLocal.
	ObjectStore *filesystem.System

	// Keys provides the per-tenant at-rest encryption key for every DB.
	// REQUIRED — the store never writes plaintext SQLite to durable storage.
	// Production wires the KMS-rooted Keyring (NewKeyring + kmskeyring.Source).
	Keys KeyProvider

	// CacheRoot is the in-pod cache directory for hot SQLite files. Defaults
	// to "/data/cache".
	CacheRoot string

	// LRUSize is the max number of open SQLite handles a single pod will
	// keep resident at any time. Hitting the cap triggers checkpoint+close
	// on the coldest handle. Defaults to 1000.
	LRUSize int

	// IdleTTL is the time a handle may sit without any Get before the
	// reaper evicts it. Defaults to 5 minutes. Zero disables the reaper.
	IdleTTL time.Duration

	// CheckpointWrites: after this many writes since the last checkpoint,
	// a handle is eligible for upload. Defaults to 100.
	CheckpointWrites int

	// CheckpointInterval: after this much wall-clock time since the last
	// checkpoint, a handle is eligible for upload. Defaults to 60s.
	CheckpointInterval time.Duration

	// Connect is the SQLite open function. Defaults to a WAL-mode open
	// equivalent to core.DefaultDBConnect.
	Connect DBConnect

	// Now returns the current time. Swap in tests to drive IdleTTL.
	Now func() time.Time

	// OnReapFailure is an optional observer for reap-cycle upload errors.
	// Services wire this to a structured logger + Prometheus counter. Nil
	// means silent — the error is still returned from Evict, but the
	// reaper runs asynchronously and there is nowhere to surface it
	// except through a hook.
	OnReapFailure func(Key, error)
}

// MultiTenantStore owns the per-tenant SQLite universe for one pod.
// Safe for concurrent use.
type MultiTenantStore struct {
	opts Options

	mu      sync.Mutex
	handles *lru.Cache[Key, *openDB]

	// shadow is a parallel set of keys currently resident in handles.
	// The lux lru.Cache doesn't expose iteration; the shadow lets Close
	// walk every resident handle and flush it, and lets the reaper pick
	// up stale handles without racing the cache internals.
	shadow map[Key]struct{}

	// existence is a hash-based bloom filter. Sized for ~10k distinct
	// tenants per pod with <1% FP. On a miss in existence we still do an
	// Exists round-trip (FP path); on a hit we skip the RTT for tenants
	// known to have a DB.
	existence *bloom.Filter

	reaperStop chan struct{}
	reaperWg   sync.WaitGroup
	closed     atomic.Bool
}

// openDB is the resident state for one tenant SQLite.
type openDB struct {
	key  Key
	db   *dbx.DB
	tkey *TenantKey // per-tenant at-rest encryption key (set at hydrate)

	// mu guards writes to the DB and to the checkpoint bookkeeping. Reads
	// through dbx go through sql.DB, which is already safe for concurrent
	// use; this mutex ensures the checkpoint sequence doesn't race with
	// writers.
	mu sync.Mutex

	lastAccess time.Time
	lastFlush  time.Time
	dirtyCount int // writes since last flush
	etag       string
	localPath  string
}

// New constructs a MultiTenantStore with defaults applied.
func New(opts Options) (*MultiTenantStore, error) {
	if opts.ObjectStore == nil {
		return nil, errors.New("store: ObjectStore is required")
	}
	if opts.Keys == nil {
		return nil, errors.New("store: Keys (per-tenant encryption) is required")
	}
	if opts.CacheRoot == "" {
		opts.CacheRoot = "/data/cache"
	}
	if opts.LRUSize <= 0 {
		opts.LRUSize = 1000
	}
	if opts.IdleTTL == 0 {
		opts.IdleTTL = 5 * time.Minute
	}
	if opts.CheckpointWrites <= 0 {
		opts.CheckpointWrites = 100
	}
	if opts.CheckpointInterval <= 0 {
		opts.CheckpointInterval = 60 * time.Second
	}
	if opts.Connect == nil {
		opts.Connect = defaultConnect
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if err := os.MkdirAll(opts.CacheRoot, 0o700); err != nil {
		return nil, fmt.Errorf("store: create cache root: %w", err)
	}

	// 8 hashes × 8 KiB = 64 Kbit capacity, ~0.1% FP for ~5k entries.
	bf, err := bloom.New(8, 8*1024)
	if err != nil {
		return nil, fmt.Errorf("store: bloom: %w", err)
	}
	s := &MultiTenantStore{
		opts:       opts,
		shadow:     make(map[Key]struct{}, opts.LRUSize),
		existence:  bf,
		reaperStop: make(chan struct{}),
	}
	// Note: we intentionally do NOT use NewCacheWithOnEvict — as of
	// luxfi/container v0.0.2 the onEvict callback is silently dropped.
	// We do our own shadow bookkeeping + explicit flush on every eviction
	// path (Put-cap, reapOnce, Evict, Close). This keeps the store correct
	// against the upstream library without forking it.
	s.handles = lru.NewCache[Key, *openDB](opts.LRUSize)

	if opts.IdleTTL > 0 {
		s.reaperWg.Add(1)
		go s.reaperLoop()
	}

	return s, nil
}

// ForCtx resolves the SQLite handle for the caller's (org, user). The caller
// must have a Claims attached via claims.Inject + claims.RequireGateway.
//
// Returns ErrGatewayBypass when identity is missing.
func (s *MultiTenantStore) ForCtx(ctx context.Context) (*dbx.DB, error) {
	c := claims.FromContext(ctx)
	if c.OrgID == "" || c.UserID == "" {
		return nil, claims.ErrGatewayBypass
	}
	return s.Get(ctx, Key{OrgID: c.OrgID, UserID: c.UserID, Scope: ScopeUser})
}

// ForOrg resolves the org-scoped SQLite for the caller. Callers must already
// be inside the tenant via claims.RequireGateway. Used for org-wide state
// (org settings, member list) that isn't per-user.
func (s *MultiTenantStore) ForOrg(ctx context.Context) (*dbx.DB, error) {
	c := claims.FromContext(ctx)
	if c.OrgID == "" {
		return nil, claims.ErrGatewayBypass
	}
	return s.Get(ctx, Key{OrgID: c.OrgID, Scope: ScopeOrg})
}

// appCtxKey / projectCtxKey are unexported context keys. App and project are
// REQUEST scope, not caller identity: per the tenant-data-hierarchy HIP the
// request carries app+project while IAM carries org (via claims). Keeping
// them out of claims.Claims preserves the canonical 3-header identity
// contract — the router attaches these the way claims.Inject attaches identity.
type appCtxKey struct{}
type projectCtxKey struct{}

// WithApp returns a context carrying the request's app scope. Routers set it
// once the app slug is resolved (typically one app per service).
func WithApp(ctx context.Context, app string) context.Context {
	return context.WithValue(ctx, appCtxKey{}, app)
}

// WithProject returns a context carrying the request's project scope.
func WithProject(ctx context.Context, project string) context.Context {
	return context.WithValue(ctx, projectCtxKey{}, project)
}

// AppFromContext returns the request's app scope, or "" when absent
// (composable fallback to the org tier).
func AppFromContext(ctx context.Context) string {
	s, _ := ctx.Value(appCtxKey{}).(string)
	return s
}

// ProjectFromContext returns the request's project scope, or "" when absent
// (composable fallback to the app tier).
func ProjectFromContext(ctx context.Context) string {
	s, _ := ctx.Value(projectCtxKey{}).(string)
	return s
}

// ForApp resolves the app-scoped SQLite for the caller's org (per-org app
// settings). Org comes from IAM (claims); the app slug from the request
// (WithApp). An absent app composably falls back to the org tier.
func (s *MultiTenantStore) ForApp(ctx context.Context) (*dbx.DB, error) {
	c := claims.FromContext(ctx)
	if c.OrgID == "" {
		return nil, claims.ErrGatewayBypass
	}
	app := AppFromContext(ctx)
	if app == "" {
		return s.Get(ctx, Key{OrgID: c.OrgID, Scope: ScopeOrg})
	}
	return s.Get(ctx, Key{OrgID: c.OrgID, App: app, Scope: ScopeApp})
}

// ForProject resolves the project-scoped SQLite for the caller's org+app
// (operational data: fleets, bots, machines). Org comes from IAM (claims);
// app+project from the request (WithApp / WithProject). Composable fallback:
// absent project → app tier, absent app → org tier.
func (s *MultiTenantStore) ForProject(ctx context.Context) (*dbx.DB, error) {
	c := claims.FromContext(ctx)
	if c.OrgID == "" {
		return nil, claims.ErrGatewayBypass
	}
	app := AppFromContext(ctx)
	if app == "" {
		return s.Get(ctx, Key{OrgID: c.OrgID, Scope: ScopeOrg})
	}
	project := ProjectFromContext(ctx)
	if project == "" {
		return s.Get(ctx, Key{OrgID: c.OrgID, App: app, Scope: ScopeApp})
	}
	return s.Get(ctx, Key{OrgID: c.OrgID, App: app, Project: project, Scope: ScopeProject})
}

// Get is the low-level path used by ForCtx / ForOrg / ForApp / ForProject.
// Exposed so that background jobs (migrations, reports) can resolve a
// specific key without a synthetic HTTP request.
func (s *MultiTenantStore) Get(ctx context.Context, k Key) (*dbx.DB, error) {
	if err := k.Valid(); err != nil {
		return nil, fmt.Errorf("store: invalid key %s: %w", k, err)
	}
	// Defense in depth (IDOR guard): when the caller carries an authenticated
	// org (claims), the requested Key MUST belong to that org. This is the
	// first-line cross-tenant boundary — one shared store process serves many
	// orgs and can fetch any org's KEK, so crypto alone does not stop a caller
	// who reaches Get with a foreign OrgID. Claim-less internal callers
	// (background jobs, migrations) are trusted and bypass the check.
	if c := claims.FromContext(ctx); c.OrgID != "" && c.OrgID != k.OrgID {
		return nil, fmt.Errorf("store: caller org %q may not access key %s: %w", c.OrgID, k, ErrCrossTenant)
	}
	if s.closed.Load() {
		return nil, ErrClosed
	}

	// Fast path: already cached.
	s.mu.Lock()
	if h, ok := s.handles.Get(k); ok {
		h.mu.Lock()
		h.lastAccess = s.opts.Now()
		h.mu.Unlock()
		s.mu.Unlock()
		return h.db, nil
	}
	s.mu.Unlock()

	return s.hydrate(ctx, k)
}

// hydrate downloads the DB from object storage (if it exists) and opens it
// locally. Serialized per-key via the big store mutex, which is fine at the
// latencies we're playing in (bucket GET + SQLite open).
func (s *MultiTenantStore) hydrate(ctx context.Context, k Key) (*dbx.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after lock.
	if h, ok := s.handles.Get(k); ok {
		h.mu.Lock()
		h.lastAccess = s.opts.Now()
		h.mu.Unlock()
		return h.db, nil
	}

	// Make room if we're at cap. Evict the coldest (= tail of the LRU) by
	// picking any shadow key that isn't currently in use. Since lux/container
	// doesn't expose its tail and the onEvict callback is dropped, we
	// approximate by evicting a random shadow key — correctness-safe because
	// any evicted handle gets flushed. A follow-up slice ports an LRU that
	// actually respects the callback.
	for s.handles.Len() >= s.opts.LRUSize {
		var victim Key
		for k := range s.shadow {
			victim = k
			break
		}
		if victim == (Key{}) {
			break
		}
		if err := s.flushAndCloseLocked(victim); err != nil {
			// Upload failed for this victim — report it through the
			// reap-failure hook (ops needs to see it) and bail. We do
			// NOT fall back to a different victim: that would make
			// LRU-cap eviction silently drop a retry we owe the data.
			s.logReapFailure(victim, err)
			return nil, err
		}
	}

	localPath := k.LocalPath(s.opts.CacheRoot)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", filepath.Dir(localPath), err)
	}

	// Resolve the per-tenant at-rest key first: for a fresh tenant this
	// generates+persists the wrapped-key sidecar BEFORE any DB object
	// exists, so an existing DB object always has a resolvable key.
	tkey, err := s.opts.Keys.Resolve(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("store: resolve key %s: %w", k, err)
	}

	var etag string
	objKey := k.ObjectKey()
	exists, err := s.opts.ObjectStore.Exists(objKey)
	if err != nil {
		return nil, fmt.Errorf("store: object exists %s: %w", objKey, err)
	}
	if exists {
		// Download ciphertext, then decrypt to the plaintext local path.
		encPath := localPath + ".enc"
		etag, err = s.downloadTo(ctx, objKey, encPath)
		if err != nil {
			return nil, err
		}
		if derr := decryptFileTo(encPath, localPath, tkey); derr != nil {
			_ = os.Remove(encPath)
			_ = os.Remove(localPath)
			// An object we cannot age-decrypt to this tenant is corrupt or
			// hostile — never retry blindly, never open it.
			return nil, fmt.Errorf("store: decrypt %s: %w", k, errors.Join(derr, ErrCorruptDB))
		}
		_ = os.Remove(encPath)
		// Defense in depth: reject non-SQLite plaintext BEFORE handing bytes
		// to the SQLite VM. A poisoned-but-decryptable payload otherwise
		// reaches sqlite_master unchecked.
		if err := verifySQLiteFile(localPath); err != nil {
			_ = os.Remove(localPath)
			return nil, err
		}
	} else {
		// New tenant. Ensure a fresh local file so SQLite writes succeed
		// and the first checkpoint creates the object. We do NOT create an
		// empty object upfront — that races with a concurrent hydrate on
		// another pod.
		if err := os.WriteFile(localPath, nil, 0o600); err != nil {
			return nil, fmt.Errorf("store: touch %s: %w", localPath, err)
		}
	}

	db, err := s.opts.Connect(localPath)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite %s: %w", localPath, err)
	}

	now := s.opts.Now()
	h := &openDB{
		key:        k,
		db:         db,
		tkey:       tkey,
		lastAccess: now,
		lastFlush:  now,
		etag:       etag,
		localPath:  localPath,
	}
	s.handles.Put(k, h)
	s.shadow[k] = struct{}{}
	s.existence.Add(xxhash.Sum64String(objKey))
	return db, nil
}

// MarkDirty is called by instrumentation / orm hooks when a write occurs.
// It increments the dirty counter; Checkpoint drains it. Apps that use the
// default orm integration don't need to call this directly.
func (s *MultiTenantStore) MarkDirty(k Key, n int) {
	s.mu.Lock()
	h, ok := s.handles.Get(k)
	s.mu.Unlock()
	if !ok {
		return
	}
	h.mu.Lock()
	h.dirtyCount += n
	over := h.dirtyCount >= s.opts.CheckpointWrites
	h.mu.Unlock()
	if over {
		_ = s.Checkpoint(context.Background(), k)
	}
}

// Checkpoint flushes the WAL and uploads the DB to object storage.
//
// In v1 there is NO generation check (CAS=false). Sticky-session gateway
// affinity provides the single-writer guarantee; during an HPA rebalance a
// brief dual-writer window may produce a lost-write. Ops MUST drain before
// scaling out. Callers that need generation-checked writes must refuse to
// boot until [CAS] flips true.
//
// The returned error wraps ErrUploadFailed on object-storage failure; the
// handle is retained in the cache so the next Checkpoint / reap tick can
// retry without losing local writes that are still durable in the WAL.
func (s *MultiTenantStore) Checkpoint(ctx context.Context, k Key) error {
	s.mu.Lock()
	h, ok := s.handles.Get(k)
	s.mu.Unlock()
	if !ok {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// WAL checkpoint truncates the -wal file into the main DB.
	if _, err := h.db.NewQuery("PRAGMA wal_checkpoint(TRUNCATE)").Execute(); err != nil {
		return fmt.Errorf("store: wal_checkpoint %s: %w", k, err)
	}

	content, err := os.ReadFile(h.localPath)
	if err != nil {
		return fmt.Errorf("store: read local %s: %w", h.localPath, err)
	}
	ct, err := sealDB(h.tkey, content)
	if err != nil {
		return fmt.Errorf("store: encrypt %s: %w", k, err)
	}
	if err := s.opts.ObjectStore.Upload(ct, k.ObjectKey()); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUploadFailed, k, err)
	}

	h.dirtyCount = 0
	h.lastFlush = s.opts.Now()
	return nil
}

// Evict flushes (if dirty) and closes the handle. Subsequent Gets for the
// same key will re-hydrate from object storage.
//
// On upload failure: returns ErrUploadFailed and RETAINS the handle in the
// cache so that (a) the next reap cycle retries, (b) the caller can
// surface the failure instead of losing the WAL-durable write.
func (s *MultiTenantStore) Evict(ctx context.Context, k Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushAndCloseLocked(k)
}

// flushAndCloseLocked runs the "evict this handle" sequence. Caller MUST
// hold s.mu.
//
// Success path: WAL checkpoint → Close the handle → upload local bytes to
// the bucket → drop from cache + shadow. Ordering matters: Close first so
// hanzoai/sqlite's pure-Go backend flushes the WAL into the main file and drops its file
// locks. Only then is the file byte-equal to the committed state — reading
// before Close risks uploading a partial page.
//
// Failure path: on upload error we re-open the handle and KEEP it resident
// (do NOT remove from cache / shadow). The handle is now marked "still
// dirty" so the next reap or Checkpoint can retry. This makes eviction
// idempotent and prevents silent data loss — the exact bug P7-H1 fixed.
func (s *MultiTenantStore) flushAndCloseLocked(k Key) error {
	h, ok := s.handles.Get(k)
	if !ok {
		delete(s.shadow, k)
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	_, _ = h.db.NewQuery("PRAGMA wal_checkpoint(TRUNCATE)").Execute()
	_ = h.db.Close()

	content, readErr := os.ReadFile(h.localPath)
	if readErr != nil {
		// Local file gone — nothing durable to upload. Drop the handle
		// because re-opening it won't recover data that isn't there.
		s.handles.Delete(k)
		delete(s.shadow, k)
		return fmt.Errorf("store: read local %s: %w", h.localPath, readErr)
	}
	if len(content) > 0 {
		ct, encErr := sealDB(h.tkey, content)
		if encErr != nil {
			// Re-open so subsequent Gets don't hit a closed DB.
			if db, openErr := s.opts.Connect(h.localPath); openErr == nil {
				h.db = db
			} else {
				s.handles.Delete(k)
			}
			return fmt.Errorf("store: encrypt %s: %w", k, encErr)
		}
		if err := s.opts.ObjectStore.Upload(ct, k.ObjectKey()); err != nil {
			// Re-open the handle so subsequent Gets don't hit a closed
			// DB, and the retry path has something to Checkpoint.
			db, openErr := s.opts.Connect(h.localPath)
			if openErr == nil {
				h.db = db
				h.dirtyCount++ // force the next Checkpoint to actually run
			} else {
				// Re-open failed too — drop from LRU so Get will
				// re-hydrate, but keep shadow so the caller knows
				// data is in-flight for this tenant.
				s.handles.Delete(k)
			}
			return fmt.Errorf("%w: %s: %v", ErrUploadFailed, k, err)
		}
	}

	s.handles.Delete(k)
	delete(s.shadow, k)
	return nil
}

// reaperLoop runs in the background, evicting handles that have been idle
// longer than IdleTTL.
func (s *MultiTenantStore) reaperLoop() {
	defer s.reaperWg.Done()
	tick := time.NewTicker(s.opts.IdleTTL / 2)
	defer tick.Stop()
	for {
		select {
		case <-s.reaperStop:
			return
		case <-tick.C:
			s.reapOnce()
		}
	}
}

// reapOnce evicts any handles idle longer than IdleTTL, flushing them to
// object storage on the way out.
//
// Lock discipline (P7-H2): the reaper takes s.mu only to SNAPSHOT the list
// of idle keys. Each per-handle flush (WAL checkpoint + Close + upload)
// runs under a fresh s.mu acquisition — i.e. the lock is released between
// handles. This bounds the maximum blocking window per Get/ForCtx to a
// single handle's flush latency instead of the full reap cycle.
func (s *MultiTenantStore) reapOnce() {
	cutoff := s.opts.Now().Add(-s.opts.IdleTTL)

	s.mu.Lock()
	idle := make([]Key, 0)
	for k := range s.shadow {
		h, ok := s.handles.Get(k)
		if !ok {
			delete(s.shadow, k)
			continue
		}
		h.mu.Lock()
		if h.lastAccess.Before(cutoff) {
			idle = append(idle, k)
		}
		h.mu.Unlock()
	}
	s.mu.Unlock()

	for _, k := range idle {
		// Each Evict re-acquires s.mu briefly; Get calls interleaved
		// with the reaper block for one handle's upload, not all of
		// them. On upload failure we log and leave the handle resident
		// so the next reap tick retries — data is NOT lost silently.
		if err := s.Evict(context.Background(), k); err != nil {
			s.logReapFailure(k, err)
		}
	}
}

// Close flushes every resident handle to object storage and then closes the
// store. Safe to call multiple times.
//
// Lock discipline (P7-H2): snapshot keys under s.mu, release, then flush
// each key with a fresh per-key lock. This bounds the shutdown window to
// sum-of-per-handle-latencies instead of holding s.mu for the entire
// drain. Upload failures are AGGREGATED into the returned error so ops
// can see exactly which tenants did not make it to durable storage.
func (s *MultiTenantStore) Close(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.opts.IdleTTL > 0 {
		close(s.reaperStop)
		s.reaperWg.Wait()
	}

	s.mu.Lock()
	keys := make([]Key, 0, len(s.shadow))
	for k := range s.shadow {
		keys = append(keys, k)
	}
	s.mu.Unlock()

	var errs []error
	for _, k := range keys {
		s.mu.Lock()
		err := s.flushAndCloseLocked(k)
		s.mu.Unlock()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("store: close flushed %d/%d, %d upload failures: %w",
			len(keys)-len(errs), len(keys), len(errs), errors.Join(errs...))
	}
	return nil
}

// logReapFailure is a hookable point for structured reap-failure logging.
// Default: no-op (the Prometheus counter in a follow-up slice will record
// this). Exposed as a method so tests can inject an observer.
func (s *MultiTenantStore) logReapFailure(k Key, err error) {
	if s.opts.OnReapFailure != nil {
		s.opts.OnReapFailure(k, err)
	}
}

// downloadTo fetches obj into localPath atomically (write-then-rename).
// Returns the ETag when available.
func (s *MultiTenantStore) downloadTo(ctx context.Context, objKey, localPath string) (string, error) {
	r, err := s.opts.ObjectStore.GetReader(objKey)
	if err != nil {
		return "", fmt.Errorf("store: open reader %s: %w", objKey, err)
	}
	defer r.Close()

	tmp := localPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("store: create tmp %s: %w", tmp, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("store: download %s: %w", objKey, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, localPath); err != nil {
		return "", fmt.Errorf("store: rename %s: %w", tmp, err)
	}
	// filesystem.System does not expose ETag uniformly across drivers; the
	// follow-up CAS slice will add a driver-specific shim. For v1 we return
	// an empty ETag and rely on pod-affinity routing for single-writer.
	return "", nil
}

// defaultConnect mirrors core.DefaultDBConnect: it opens on the canonical Hanzo
// driver (github.com/hanzoai/sqlite, which registers "sqlite" under both build
// configs) and applies the SAME sqlite.DefaultPragmas via sqlite.PragmaDSN —
// one pragma set, encoded in the active backend's DSN syntax so busy_timeout +
// WAL apply whether Base is built pure-Go (!cgo) or with hanzoai/sqlcipher (cgo).
func defaultConnect(path string) (*dbx.DB, error) {
	return dbx.Open("sqlite", sqlite.PragmaDSN(path, sqlite.DefaultPragmas))
}

// verifySQLiteFile reads the first 16 bytes of path and passes them to
// validateSQLiteMagic. Empty files are accepted (fresh-tenant path).
func verifySQLiteFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("store: open for header check %s: %w", path, err)
	}
	defer f.Close()
	var buf [16]byte
	n, err := io.ReadFull(f, buf[:])
	switch {
	case err == io.EOF:
		// Zero bytes — fresh tenant slot.
		return nil
	case err == io.ErrUnexpectedEOF:
		// Short but non-empty: not a valid SQLite file.
		return ErrCorruptDB
	case err != nil:
		return fmt.Errorf("store: header read %s: %w", path, err)
	}
	if n < len(buf) {
		return ErrCorruptDB
	}
	return validateSQLiteMagic(buf[:])
}

// decryptFileTo reads the age-encrypted DB at encPath, decrypts it with the
// tenant key, and writes the plaintext atomically to plainPath.
func decryptFileTo(encPath, plainPath string, tk *TenantKey) error {
	ciphertext, err := os.ReadFile(encPath)
	if err != nil {
		return fmt.Errorf("store: read enc %s: %w", encPath, err)
	}
	plain, err := openDBBytes(tk, ciphertext)
	if err != nil {
		return err
	}
	tmp := plainPath + ".tmp"
	if err := os.WriteFile(tmp, plain, 0o600); err != nil {
		return fmt.Errorf("store: write plain %s: %w", tmp, err)
	}
	return os.Rename(tmp, plainPath)
}
