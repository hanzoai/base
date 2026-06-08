package network

import metric "github.com/luxfi/metric"

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
	Shards           metric.Counter
	ActiveShards     metric.Gauge
	WALLagBytes      metric.Gauge
	HotShards        metric.Gauge
	LeaseContentions metric.Counter
	ArchiveLagBytes  metric.Gauge
	// ArchiveFailures counts segment flushes that exhausted their
	// retry deadline. Incremented once per failure regardless of
	// re-buffer outcome.
	ArchiveFailures metric.Counter
	// ArchiveDrops counts segments shed by the per-shard backlog cap
	// (R6). Under a healthy backend this stays at zero; non-zero is a
	// pageable condition — we're losing archive frames to keep the pod
	// alive.
	ArchiveDrops metric.Counter

	FramesSubmitted metric.Counter
	FramesIngested  metric.Counter
	FramesFinalized metric.Counter
	FramesDuplicate metric.Counter
	FramesInvalid   metric.Counter
	// FramesRejectedShardMismatch counts frames dropped because the
	// Envelope's routed shard disagreed with the Frame's internal
	// shardID (R1 probe). Under healthy operation this stays at zero.
	FramesRejectedShardMismatch metric.Counter
	// FramesRejectedSeqGap counts frames whose height != prevHeight+1
	// (R2). The apply path never bumps localSeq from these.
	FramesRejectedSeqGap metric.Counter

	ApplyErrors   metric.Counter
	WALHookErrors metric.Counter
	WALBytes      metric.Counter

	// MembershipSize is the live member count as reported by the
	// Membership watcher. Dashboards alert on unexpected drops
	// (scale-down events show up here before the transport notices).
	MembershipSize metric.Gauge
}

// NewMetrics constructs the collectors. Register them on a caller-owned
// registry with Register(). Calling NewMetrics twice produces two
// independent sets — useful for test isolation.
func NewMetrics() *Metrics {
	return &Metrics{
		Shards: metric.NewCounter(metric.CounterOpts{
			Name: "base_shards",
			Help: "Total shards assigned to this member over its lifetime.",
		}),
		ActiveShards: metric.NewGauge(metric.GaugeOpts{
			Name: "base_active_shards",
			Help: "Shards with a live Quasar engine on this member.",
		}),
		WALLagBytes: metric.NewGauge(metric.GaugeOpts{
			Name: "base_wal_lag_bytes",
			Help: "Bytes of WAL data buffered awaiting archive flush.",
		}),
		HotShards: metric.NewGauge(metric.GaugeOpts{
			Name: "base_hot_shards",
			Help: "Shards above the activity threshold (autoscale signal).",
		}),
		LeaseContentions: metric.NewCounter(metric.CounterOpts{
			Name: "base_lease_contentions",
			Help: "Writer-lease races lost to another member.",
		}),
		ArchiveLagBytes: metric.NewGauge(metric.GaugeOpts{
			Name: "base_archive_lag_bytes",
			Help: "Bytes pending flush to the cold archive.",
		}),
		ArchiveFailures: metric.NewCounter(metric.CounterOpts{
			Name: "base_archive_failures_total",
			Help: "Segment flushes that exhausted their retry deadline.",
		}),
		ArchiveDrops: metric.NewCounter(metric.CounterOpts{
			Name: "base_shard_backlog_drops_total",
			Help: "Per-shard backlog segments dropped after exceeding the backlog cap.",
		}),

		FramesSubmitted: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_submitted_total",
			Help: "Frames produced locally and submitted to Quasar.",
		}),
		FramesIngested: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_ingested_total",
			Help: "Frames received from peers via transport and submitted to Quasar.",
		}),
		FramesFinalized: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_finalized_total",
			Help: "Frames applied locally after Quasar finalisation.",
		}),
		FramesDuplicate: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_duplicate_total",
			Help: "Finalised frames skipped because already applied (idempotency).",
		}),
		FramesInvalid: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_invalid_total",
			Help: "Inbound frames rejected on checksum mismatch.",
		}),
		FramesRejectedShardMismatch: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_rejected_shard_mismatch_total",
			Help: "Inbound frames dropped because envelope shardID != frame shardID (R1).",
		}),
		FramesRejectedSeqGap: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_frames_rejected_seq_gap_total",
			Help: "Finalised frames skipped because Height != prevHeight+1 (R2).",
		}),
		ApplyErrors: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_apply_errors_total",
			Help: "Errors from the per-shard apply callback.",
		}),
		WALHookErrors: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_wal_hook_errors_total",
			Help: "Failures inside the SQLite commit hook path.",
		}),
		WALBytes: metric.NewCounter(metric.CounterOpts{
			Name: "base_network_wal_bytes_total",
			Help: "Total WAL payload bytes captured by the commit hook.",
		}),
		MembershipSize: metric.NewGauge(metric.GaugeOpts{
			Name: "base_network_membership_size",
			Help: "Live member count reported by the Membership watcher.",
		}),
	}
}

// Register adds every collector in m to r. Duplicate registration returns
// the first error; callers that need per-metric control can register one
// field at a time.
func (m *Metrics) Register(r metric.Registerer) error {
	if m == nil || r == nil {
		return nil
	}
	collectors := []metric.Collector{
		m.Shards, m.ActiveShards, m.WALLagBytes, m.HotShards,
		m.LeaseContentions, m.ArchiveLagBytes,
		m.ArchiveFailures, m.ArchiveDrops,
		m.FramesSubmitted, m.FramesIngested, m.FramesFinalized,
		m.FramesDuplicate, m.FramesInvalid,
		m.FramesRejectedShardMismatch, m.FramesRejectedSeqGap,
		m.ApplyErrors, m.WALHookErrors, m.WALBytes,
		m.MembershipSize,
	}
	for _, c := range collectors {
		if err := r.Register(c); err != nil {
			return err
		}
	}
	return nil
}
