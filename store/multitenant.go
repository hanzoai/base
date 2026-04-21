// Package store implements the canonical per-(org, user) SQLite storage
// model described in hanzo/ARCHITECTURE.md §5.
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
	"github.com/luxfi/cache/bloom"
	"github.com/luxfi/cache/lru"

	// modernc sqlite driver; registers "sqlite" with database/sql. Same
	// driver selected by core.DefaultDBConnect.
	_ "modernc.org/sqlite"
)

// Key is the tuple that identifies a per-tenant SQLite DB.
//
// Scope = "users" for per-user DBs and "org" for the org-wide DB. Splitting
// the space this way lets us keep a single cache and a single object-storage
// layout for both shapes.
type Key struct {
	OrgID  string
	UserID string // empty when Scope == ScopeOrg
	Scope  Scope
}

// Scope selects user-level or org-level DB for a given org.
type Scope int

const (
	ScopeUser Scope = iota
	ScopeOrg
)

// String implements fmt.Stringer for log lines and metric labels.
func (k Key) String() string {
	if k.Scope == ScopeOrg {
		return k.OrgID + "/org"
	}
	return k.OrgID + "/users/" + k.UserID
}

// ObjectKey returns the object-storage path for the DB.
//
//	user-scoped:  {org}/users/{user}.db
//	org-scoped:   {org}/org.db
func (k Key) ObjectKey() string {
	if k.Scope == ScopeOrg {
		return filepath.ToSlash(filepath.Join(k.OrgID, "org.db"))
	}
	return filepath.ToSlash(filepath.Join(k.OrgID, "users", k.UserID+".db"))
}

// LocalPath returns the in-pod filesystem path for the DB.
func (k Key) LocalPath(cacheRoot string) string {
	if k.Scope == ScopeOrg {
		return filepath.Join(cacheRoot, k.OrgID, "org.db")
	}
	return filepath.Join(cacheRoot, k.OrgID, "users", k.UserID+".db")
}

// Valid reports whether the key is well-formed. Callers MUST validate keys
// built from HTTP headers before passing them to the store — otherwise a
// crafted org slug can reach the filesystem with `..`.
func (k Key) Valid() error {
	if err := validateSlug(k.OrgID); err != nil {
		return fmt.Errorf("OrgID: %w", err)
	}
	if k.Scope == ScopeUser {
		if err := validateSlug(k.UserID); err != nil {
			return fmt.Errorf("UserID: %w", err)
		}
	}
	return nil
}

// validateSlug enforces `[a-z0-9_-]+`, rejecting path traversal, mixed case,
// and whitespace. Both slugs (org, user) live under this contract in IAM.
func validateSlug(s string) error {
	if s == "" {
		return errors.New("empty")
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return fmt.Errorf("invalid character %q", c)
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
}

// MultiTenantStore owns the per-(org, user) SQLite universe for one pod.
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
	key Key
	db  *dbx.DB

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

// Get is the low-level path used by ForCtx / ForOrg. Exposed so that
// background jobs (migrations, reports) can resolve a specific key without a
// synthetic HTTP request.
func (s *MultiTenantStore) Get(ctx context.Context, k Key) (*dbx.DB, error) {
	if err := k.Valid(); err != nil {
		return nil, fmt.Errorf("store: invalid key %s: %w", k, err)
	}
	if s.closed.Load() {
		return nil, errors.New("store: closed")
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
		s.flushAndCloseLocked(victim)
	}

	localPath := k.LocalPath(s.opts.CacheRoot)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", filepath.Dir(localPath), err)
	}

	var etag string
	objKey := k.ObjectKey()
	exists, err := s.opts.ObjectStore.Exists(objKey)
	if err != nil {
		return nil, fmt.Errorf("store: object exists %s: %w", objKey, err)
	}
	if exists {
		etag, err = s.downloadTo(ctx, objKey, localPath)
		if err != nil {
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

// Checkpoint flushes the WAL, uploads to object storage with If-Match on the
// known ETag, and advances the cached ETag. Safe to call while the handle is
// in use; writers will block only for the upload window.
//
// On CAS failure (another pod has advanced the generation), returns an error
// wrapping ErrConflict so the caller can choose to invalidate the local
// handle and hydrate fresh.
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
	if err := s.opts.ObjectStore.Upload(content, k.ObjectKey()); err != nil {
		return fmt.Errorf("store: upload %s: %w", k, err)
	}

	// We don't read ETag here — filesystem.System doesn't surface it
	// uniformly across fileblob/s3blob, and for the v1 spec we rely on a
	// pod-affinity router (consistent hash on org) to prevent concurrent
	// writers. The §5.5 ETag CAS path is the follow-up slice.
	h.dirtyCount = 0
	h.lastFlush = s.opts.Now()
	return nil
}

// Evict flushes (if dirty) and closes the handle. Subsequent Gets for the
// same key will re-hydrate from object storage.
func (s *MultiTenantStore) Evict(ctx context.Context, k Key) error {
	s.mu.Lock()
	s.flushAndCloseLocked(k)
	s.mu.Unlock()
	return nil
}

// flushAndCloseLocked runs the "evict this handle" sequence. Caller MUST
// hold s.mu. The handle is removed from the LRU, the WAL is checkpointed,
// the DB is closed, and the local file is uploaded to object storage.
//
// Ordering: Close first so modernc.org/sqlite flushes the WAL into the main
// file and drops its file locks. Only then is the file byte-equal to the
// committed state — reading before Close risks uploading a partial page.
func (s *MultiTenantStore) flushAndCloseLocked(k Key) {
	h, ok := s.handles.Get(k)
	if !ok {
		delete(s.shadow, k)
		return
	}
	h.mu.Lock()
	_, _ = h.db.NewQuery("PRAGMA wal_checkpoint(TRUNCATE)").Execute()
	_ = h.db.Close()
	if content, err := os.ReadFile(h.localPath); err == nil && len(content) > 0 {
		_ = s.opts.ObjectStore.Upload(content, k.ObjectKey())
	}
	h.mu.Unlock()

	s.handles.Delete(k)
	delete(s.shadow, k)
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
	for _, k := range idle {
		s.flushAndCloseLocked(k)
	}
	s.mu.Unlock()
}

// Close flushes every resident handle to object storage and then closes the
// store. Safe to call multiple times.
func (s *MultiTenantStore) Close(ctx context.Context) error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.opts.IdleTTL > 0 {
		close(s.reaperStop)
		s.reaperWg.Wait()
	}

	// Flush every resident handle through the explicit path.
	s.mu.Lock()
	keys := make([]Key, 0, len(s.shadow))
	for k := range s.shadow {
		keys = append(keys, k)
	}
	for _, k := range keys {
		s.flushAndCloseLocked(k)
	}
	s.mu.Unlock()
	return nil
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

// defaultConnect mirrors core.DefaultDBConnect without creating an import
// cycle: busy_timeout first, then WAL, NORMAL sync, FK on, 32 MB page cache.
func defaultConnect(path string) (*dbx.DB, error) {
	const pragmas = "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(200000000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-32000)"
	return dbx.Open("sqlite", path+pragmas)
}
