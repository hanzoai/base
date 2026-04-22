package network

import "github.com/prometheus/client_golang/prometheus"

// Metrics owns every collector the network package exposes. Callers wire
// it into their own registry; we never touch prometheus.DefaultRegisterer.
//
// The metric families correspond directly to the design doc:
//
//	base_shards                counter  shards assigned to this member
//	base_active_shards         gauge    shards with a live Quasar engine
//	base_wal_lag_bytes         gauge    bytes pending archive flush (set via Append)
//	base_hot_shards            gauge    shards above activity threshold (set by scaler)
//	base_lease_contentions     counter  times a writer lost a lease race
//	base_archive_lag_bytes     gauge    mirror of wal_lag for Prom naming parity
//
// Plus per-path counters used by the internal wiring:
//
//	base_network_frames_submitted_total
//	base_network_frames_ingested_total
//	base_network_frames_finalized_total
//	base_network_frames_duplicate_total
//	base_network_frames_invalid_total
//	base_network_apply_errors_total
//	base_network_wal_hook_errors_total
//	base_network_wal_bytes_total
type Metrics struct {
	Shards           prometheus.Counter
	ActiveShards     prometheus.Gauge
	WALLagBytes      prometheus.Gauge
	HotShards        prometheus.Gauge
	LeaseContentions prometheus.Counter
	ArchiveLagBytes  prometheus.Gauge
	// ArchiveFailures counts segment flushes that exhausted their
	// retry deadline. Incremented once per failure regardless of
	// re-buffer outcome.
	ArchiveFailures prometheus.Counter
	// ArchiveDrops counts segments shed by the per-shard backlog cap
	// (R6). Under a healthy backend this stays at zero; non-zero is a
	// pageable condition — we're losing archive frames to keep the pod
	// alive.
	ArchiveDrops prometheus.Counter

	FramesSubmitted prometheus.Counter
	FramesIngested  prometheus.Counter
	FramesFinalized prometheus.Counter
	FramesDuplicate prometheus.Counter
	FramesInvalid   prometheus.Counter
	// FramesRejectedShardMismatch counts frames dropped because the
	// Envelope's routed shard disagreed with the Frame's internal
	// shardID (R1 probe). Under healthy operation this stays at zero.
	FramesRejectedShardMismatch prometheus.Counter
	// FramesRejectedSeqGap counts frames whose height != prevHeight+1
	// (R2). The apply path never bumps localSeq from these.
	FramesRejectedSeqGap prometheus.Counter

	ApplyErrors   prometheus.Counter
	WALHookErrors prometheus.Counter
	WALBytes      prometheus.Counter
}

// NewMetrics constructs the collectors. Register them on a caller-owned
// registry with Register(). Calling NewMetrics twice produces two
// independent sets — useful for test isolation.
func NewMetrics() *Metrics {
	return &Metrics{
		Shards: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_shards",
			Help: "Total shards assigned to this member over its lifetime.",
		}),
		ActiveShards: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "base_active_shards",
			Help: "Shards with a live Quasar engine on this member.",
		}),
		WALLagBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "base_wal_lag_bytes",
			Help: "Bytes of WAL data buffered awaiting archive flush.",
		}),
		HotShards: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "base_hot_shards",
			Help: "Shards above the activity threshold (autoscale signal).",
		}),
		LeaseContentions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_lease_contentions",
			Help: "Writer-lease races lost to another member.",
		}),
		ArchiveLagBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "base_archive_lag_bytes",
			Help: "Bytes pending flush to the cold archive.",
		}),
		ArchiveFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_archive_failures_total",
			Help: "Segment flushes that exhausted their retry deadline.",
		}),
		ArchiveDrops: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_shard_backlog_drops_total",
			Help: "Per-shard backlog segments dropped after exceeding the backlog cap.",
		}),

		FramesSubmitted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_submitted_total",
			Help: "Frames produced locally and submitted to Quasar.",
		}),
		FramesIngested: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_ingested_total",
			Help: "Frames received from peers via transport and submitted to Quasar.",
		}),
		FramesFinalized: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_finalized_total",
			Help: "Frames applied locally after Quasar finalisation.",
		}),
		FramesDuplicate: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_duplicate_total",
			Help: "Finalised frames skipped because already applied (idempotency).",
		}),
		FramesInvalid: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_invalid_total",
			Help: "Inbound frames rejected on checksum mismatch.",
		}),
		FramesRejectedShardMismatch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_rejected_shard_mismatch_total",
			Help: "Inbound frames dropped because envelope shardID != frame shardID (R1).",
		}),
		FramesRejectedSeqGap: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_frames_rejected_seq_gap_total",
			Help: "Finalised frames skipped because Height != prevHeight+1 (R2).",
		}),
		ApplyErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_apply_errors_total",
			Help: "Errors from the per-shard apply callback.",
		}),
		WALHookErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_wal_hook_errors_total",
			Help: "Failures inside the SQLite commit hook path.",
		}),
		WALBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "base_network_wal_bytes_total",
			Help: "Total WAL payload bytes captured by the commit hook.",
		}),
	}
}

// Register adds every collector in m to r. Duplicate registration returns
// the first error; callers that need per-metric control can register one
// field at a time.
func (m *Metrics) Register(r prometheus.Registerer) error {
	if m == nil || r == nil {
		return nil
	}
	collectors := []prometheus.Collector{
		m.Shards, m.ActiveShards, m.WALLagBytes, m.HotShards,
		m.LeaseContentions, m.ArchiveLagBytes,
		m.ArchiveFailures, m.ArchiveDrops,
		m.FramesSubmitted, m.FramesIngested, m.FramesFinalized,
		m.FramesDuplicate, m.FramesInvalid,
		m.FramesRejectedShardMismatch, m.FramesRejectedSeqGap,
		m.ApplyErrors, m.WALHookErrors, m.WALBytes,
	}
	for _, c := range collectors {
		if err := r.Register(c); err != nil {
			return err
		}
	}
	return nil
}
