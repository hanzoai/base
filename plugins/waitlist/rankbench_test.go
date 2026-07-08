// Copyright (C) 2020-2026, Hanzo AI Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package waitlist

// Scale benchmark for the points -> position model. It exercises the EXACT
// SQLite mechanics the plugin uses (same hanzoai/sqlite driver + production
// pragmas, same index shape, same rank/neighborhood SQL) against 100k / 1M /
// 10M synthetic rows and reports the numbers that decide "cheap at 1M+".
//
// Model under test (matches plugins/waitlist/points.go):
//
//	position = COMPETITION rank = 1 + COUNT(entries with strictly more points).
//	Tied users share a rank (standard leaderboard semantics) — so rank never
//	has to count within a big tie band, only the users genuinely ahead.
//
//	neighborhood (rank +/- W) = pure index SEEKS, no OR: the W higher-point
//	rows above + the W same-point earlier peers, and symmetrically below.
//	Each sub-query is `SEARCH ... USING COVERING INDEX`, so it is O(log n + W)
//	— flat in the list size.
//
// It also builds a points histogram (points -> count) and computes rank as
// SUM(count) over buckets with more points — O(distinct points), sub-ms even
// at 10M — to prove the ceiling-buster past the COUNT scan's limit.
//
// Gated behind WL_BENCH so it never runs in normal CI:
//
//	WL_BENCH=1 WL_BENCH_N=100000,1000000,10000000 \
//	  go test -run TestPointsScaleBenchmark -v -timeout 60m ./plugins/waitlist/

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hanzoai/sqlite"
)

const benchWaitlist = "launch"

func TestPointsScaleBenchmark(t *testing.T) {
	if os.Getenv("WL_BENCH") == "" {
		t.Skip("set WL_BENCH=1 to run the scale benchmark")
	}
	sizes := parseSizes(os.Getenv("WL_BENCH_N"))
	dir := os.Getenv("WL_BENCH_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else {
		_ = os.MkdirAll(dir, 0o755)
	}

	fmt.Printf("\n=== waitlist points->position scale benchmark ===\n")
	fmt.Printf("driver: hanzoai/sqlite (WAL, busy_timeout=10s, synchronous=NORMAL)\n")
	fmt.Printf("model:  competition rank = 1 + COUNT(points>p); neighborhood = keyset SEEKS\n\n")

	for _, n := range sizes {
		runBenchAtSize(t, dir, n)
	}
}

func runBenchAtSize(t *testing.T, dir string, n int) {
	dbPath := filepath.Join(dir, fmt.Sprintf("bench-%d.db", n))
	for _, suf := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(dbPath + suf)
	}

	db, err := sql.Open("sqlite", sqlite.PragmaDSN(dbPath, sqlite.DefaultPragmas))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	mustExec(t, db, `CREATE TABLE entries (
		seq      INTEGER PRIMARY KEY,
		id       TEXT NOT NULL,
		waitlist TEXT NOT NULL,
		email    TEXT NOT NULL,
		points   INTEGER NOT NULL
	)`)
	// The one index that makes rank + neighborhood cheap. points DESC => the
	// leaderboard order is a forward scan; seq ASC => earlier joiners win ties.
	mustExec(t, db, "CREATE INDEX idx_rank ON entries (waitlist, points DESC, seq ASC)")

	// --- seed ---------------------------------------------------------------
	rng := rand.New(rand.NewSource(0xB1A5E ^ int64(n)))
	insertStart := time.Now()
	const batch = 20000
	seq := 0
	for seq < n {
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		stmt, err := tx.Prepare("INSERT INTO entries (seq,id,waitlist,email,points) VALUES (?,?,?,?,?)")
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		end := min(seq+batch, n)
		for ; seq < end; seq++ {
			if _, err := stmt.Exec(seq+1, synthID(seq), benchWaitlist, synthEmail(seq), zipfPoints(rng)); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
		_ = stmt.Close()
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	insertDur := time.Since(insertStart)

	mustExec(t, db, "PRAGMA wal_checkpoint(TRUNCATE)")
	mustExec(t, db, "ANALYZE")
	diskBytes := fileSize(dbPath) + fileSize(dbPath+"-wal")

	// --- histogram (points -> count), the O(distinct-points) rank path ------
	mustExec(t, db, "CREATE TABLE hist (waitlist TEXT, points INTEGER, cnt INTEGER, PRIMARY KEY(waitlist,points))")
	mustExec(t, db, `INSERT INTO hist SELECT waitlist, points, COUNT(*) FROM entries GROUP BY waitlist, points`)
	var distinct int
	_ = db.QueryRow("SELECT COUNT(*) FROM hist WHERE waitlist=?", benchWaitlist).Scan(&distinct)

	// --- sample pivots (random rows + the guaranteed worst case) ------------
	const K = 500
	pivots := make([]pivot, 0, K+1)
	for i := 0; i < K; i++ {
		pivots = append(pivots, loadPivot(t, db, rng.Intn(n)+1))
	}
	worst := worstPivot(t, db) // lowest points, latest seq => max rank
	pivots = append(pivots, worst)

	// --- measure: competition rank via COUNT(points>p) ----------------------
	rankLat := make([]time.Duration, 0, len(pivots))
	var worstRank int
	var worstRankLat time.Duration
	for _, p := range pivots {
		st := time.Now()
		r := countRank(t, db, p)
		d := time.Since(st)
		rankLat = append(rankLat, d)
		if p.seq == worst.seq {
			worstRank, worstRankLat = r, d
		}
	}

	// --- measure: competition rank via histogram SUM ------------------------
	histLat := make([]time.Duration, 0, len(pivots))
	for _, p := range pivots {
		st := time.Now()
		hr := histRank(t, db, p)
		histLat = append(histLat, time.Since(st))
		// sanity: histogram rank must equal count rank
		if hr != countRank(t, db, p) {
			t.Fatalf("hist rank %d != count rank at seq %d", hr, p.seq)
		}
	}

	// --- measure: neighborhood rank +/- 100 via keyset seeks ----------------
	const window = 100
	nbrLat := make([]time.Duration, 0, len(pivots))
	for _, p := range pivots {
		st := time.Now()
		above := neighborsAbove(t, db, p, window)
		below := neighborsBelow(t, db, p, window)
		nbrLat = append(nbrLat, time.Since(st))
		if len(above) > window || len(below) > window {
			t.Fatalf("neighborhood over-fetched: %d/%d", len(above), len(below))
		}
	}

	rss := rssMiB()

	fmt.Printf("── N = %s ──────────────────────────────────────────────\n", commas(n))
	fmt.Printf("  insert:        %s in %s  (%s rows/sec, 20k/txn)\n",
		commas(n), insertDur.Round(time.Millisecond), commas(int(float64(n)/insertDur.Seconds())))
	fmt.Printf("  on-disk:       %.1f MB  (%.0f bytes/row, main+wal after checkpoint)\n",
		float64(diskBytes)/1e6, float64(diskBytes)/float64(n))
	fmt.Printf("  process RSS:   %d MB  (list lives on disk; Go heap holds none of it)\n", rss)
	fmt.Printf("  rank COUNT:    p50=%s p95=%s p99=%s max=%s   (competition rank, %d samples)\n",
		ms(pctl(rankLat, 50)), ms(pctl(rankLat, 95)), ms(pctl(rankLat, 99)), ms(maxDur(rankLat)), len(rankLat))
	fmt.Printf("  worst rank:    #%s of %s in %s  (0-point user: COUNT scans all users ahead)\n",
		commas(worstRank), commas(n), worstRankLat.Round(time.Microsecond))
	fmt.Printf("  rank HISTO:    p50=%s p95=%s max=%s   (SUM over %s distinct-point buckets — the ceiling-buster)\n",
		ms(pctl(histLat, 50)), ms(pctl(histLat, 95)), ms(maxDur(histLat)), commas(distinct))
	fmt.Printf("  neighborhood:  p50=%s p95=%s p99=%s max=%s   (rank +/-100, keyset seek — flat in N)\n",
		ms(pctl(nbrLat, 50)), ms(pctl(nbrLat, 95)), ms(pctl(nbrLat, 99)), ms(maxDur(nbrLat)))
	fmt.Printf("\n")
}

// ── pivot + queries ───────────────────────────────────────────────────────

type pivot struct {
	seq    int
	points int64
}

func loadPivot(t *testing.T, db *sql.DB, seq int) pivot {
	var p int64
	if err := db.QueryRow("SELECT points FROM entries WHERE seq=?", seq).Scan(&p); err != nil {
		t.Fatalf("loadPivot: %v", err)
	}
	return pivot{seq: seq, points: p}
}

func worstPivot(t *testing.T, db *sql.DB) pivot {
	var seq int
	var pts int64
	if err := db.QueryRow(
		"SELECT seq, points FROM entries WHERE waitlist=? ORDER BY points ASC, seq DESC LIMIT 1",
		benchWaitlist).Scan(&seq, &pts); err != nil {
		t.Fatalf("worstPivot: %v", err)
	}
	return pivot{seq: seq, points: pts}
}

// countRank = competition rank = 1 + #(strictly more points). One index-range
// COUNT. O(#users-ahead): cheap near the top, grows toward the tail.
func countRank(t *testing.T, db *sql.DB, p pivot) int {
	var ahead int64
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM entries WHERE waitlist=? AND points>?",
		benchWaitlist, p.points).Scan(&ahead); err != nil {
		t.Fatalf("countRank: %v", err)
	}
	return int(ahead) + 1
}

// histRank = 1 + SUM(cnt) over buckets with more points. O(distinct points) —
// independent of how many users sit in the tail. This is the scale path.
func histRank(t *testing.T, db *sql.DB, p pivot) int {
	var ahead sql.NullInt64
	if err := db.QueryRow(
		"SELECT SUM(cnt) FROM hist WHERE waitlist=? AND points>?",
		benchWaitlist, p.points).Scan(&ahead); err != nil {
		t.Fatalf("histRank: %v", err)
	}
	return int(ahead.Int64) + 1
}

// neighborsAbove: the `window` entries immediately ahead (better rank). Two
// pure-index SEEKS, no OR: same-point earlier peers first (closer), then higher
// points. Merged and truncated to `window`.
func neighborsAbove(t *testing.T, db *sql.DB, p pivot, window int) []int64 {
	band := queryPoints(t, db,
		"SELECT points FROM entries WHERE waitlist=? AND points=? AND seq<? ORDER BY seq DESC LIMIT ?",
		benchWaitlist, p.points, p.seq, window)
	if len(band) >= window {
		return band[:window]
	}
	higher := queryPoints(t, db,
		"SELECT points FROM entries WHERE waitlist=? AND points>? ORDER BY points ASC, seq DESC LIMIT ?",
		benchWaitlist, p.points, window-len(band))
	return append(band, higher...)
}

// neighborsBelow: the `window` entries immediately behind (worse rank).
func neighborsBelow(t *testing.T, db *sql.DB, p pivot, window int) []int64 {
	band := queryPoints(t, db,
		"SELECT points FROM entries WHERE waitlist=? AND points=? AND seq>? ORDER BY seq ASC LIMIT ?",
		benchWaitlist, p.points, p.seq, window)
	if len(band) >= window {
		return band[:window]
	}
	lower := queryPoints(t, db,
		"SELECT points FROM entries WHERE waitlist=? AND points<? ORDER BY points DESC, seq ASC LIMIT ?",
		benchWaitlist, p.points, window-len(band))
	return append(band, lower...)
}

func queryPoints(t *testing.T, db *sql.DB, q string, args ...any) []int64 {
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, v)
	}
	return out
}

// ── synthetic data ────────────────────────────────────────────────────────

// zipfPoints: heavy-tailed. ~60% of users land on 0 points (the realistic
// waitlist shape — a few power-referrers, a long flat tail). This is the WORST
// case for COUNT rank because the tail users must count everyone ahead.
func zipfPoints(rng *rand.Rand) int64 {
	return int64(math.Pow(rng.Float64(), 8) * 5000)
}

func synthID(i int) string    { return "e" + strconv.FormatInt(int64(i), 36) }
func synthEmail(i int) string { return fmt.Sprintf("u%d@example.com", i) }

// ── helpers ───────────────────────────────────────────────────────────────

func parseSizes(s string) []int {
	if strings.TrimSpace(s) == "" {
		return []int{100_000, 1_000_000}
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return []int{100_000, 1_000_000}
	}
	return out
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func fileSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

func pctl(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), ds...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[(p*(len(cp)-1))/100]
}

func maxDur(ds []time.Duration) time.Duration {
	var m time.Duration
	for _, d := range ds {
		if d > m {
			m = d
		}
	}
	return m
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.3fms", float64(d.Nanoseconds())/1e6)
}

func commas(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func rssMiB() int {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			if f := strings.Fields(line); len(f) >= 2 {
				kb, _ := strconv.Atoi(f[1])
				return kb / 1024
			}
		}
	}
	return 0
}
