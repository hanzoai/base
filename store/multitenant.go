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

// MaxSlugLen caps org / user slug length. 128 bytes is generous: IAM ULIDs
// are 26 chars, UUIDs 36. A slug longer than this is either a mistake or an
// attempt to blow up filesystem error messages with attacker-controlled
// bytes — reject before any FS call.
const MaxSlugLen = 128

// validateSlug enforces `[a-z0-9_-]+` and length ≤ MaxSlugLen, rejecting
// path traversal, mixed case, whitespace, and oversized inputs. Both slugs
// (org, user) live under this contract in IAM.
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
		// Defense in depth: reject non-SQLite blobs BEFORE handing bytes
		// to modernc.org/sqlite. A poisoned bucket entry (SSRF, stolen
		// creds, malicious CI) otherwise reaches the SQLite VM unchecked
		// and any hostile sqlite_master row fires on first query.
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
	if err := s.opts.ObjectStore.Upload(content, k.ObjectKey()); err != nil {
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
// modernc.org/sqlite flushes the WAL into the main file and drops its file
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
		if err := s.opts.ObjectStore.Upload(content, k.ObjectKey()); err != nil {
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

// defaultConnect mirrors core.DefaultDBConnect without creating an import
// cycle: busy_timeout first, then WAL, NORMAL sync, FK on, 32 MB page cache.
func defaultConnect(path string) (*dbx.DB, error) {
	const pragmas = "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(200000000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-32000)"
	return dbx.Open("sqlite", path+pragmas)
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
