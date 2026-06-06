/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import "github.com/prometheus/client_golang/prometheus"

// HA metrics — see docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md
// Section 8.1 for the policy these metrics drive.
//
// Naming follows Prometheus conventions: counters end in _total, gauges
// describe the current value, histograms end in _seconds for timings.
// Per-binary registration: see registry assignments at the bottom of this file.
//
// Note on derived names: the design doc lists e.g.
// `grimnir_listener_reconnect_rate_per_5min`. That's a derived view, not a
// raw metric. Prometheus convention is to expose a `_total` counter and
// derive `rate(... [5m])` in queries. The implementation uses `_total`;
// the alert rule (Chunk 7) does the rate derivation.

var (
	// ListenerReconnectTotal — increments each time a listener's TCP stream
	// reconnects within a short window. Rate over 5m drives the soak-window
	// auto-rollback alert.
	ListenerReconnectTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_listener_reconnect_total",
			Help: "Total listener reconnects (rate [5m] feeds the soak-window auto-rollback alert).",
		},
		[]string{"mount"},
	)

	// EdgeEncoderBytesTotal — bytes the edge encoder has shipped to clients
	// per node. Tier-3 alert: both nodes hit zero during soak window.
	EdgeEncoderBytesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_edge_encoder_bytes_total",
			Help: "Total bytes shipped by the edge encoder; rate gives bytes/sec.",
		},
		[]string{"node"},
	)

	// PostgresReplicationLagSeconds — primary-to-replica WAL lag.
	// Tier-1 alert > 5s; tier-2 alert > 30s.
	PostgresReplicationLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_postgres_replication_lag_seconds",
			Help: "Streaming replication lag in seconds (queried from pg_stat_replication).",
		},
	)

	// VrrpHolderCount — count of nodes claiming a given VIP. Should always
	// equal 1. Tier-2 alert at 0 (no holder) or 2 (split-brain).
	VrrpHolderCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_vrrp_holder_count",
			Help: "Number of nodes claiming a given VIP (must equal 1).",
		},
		[]string{"vip"},
	)

	// EngineHealth — per-node engine health: 1=serving, 0=not_serving.
	// Tier-2 alert when both nodes in a region report 0.
	EngineHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_engine_health",
			Help: "Engine health (1=serving, 0=not_serving), per node.",
		},
		[]string{"node"},
	)

	// PcmInputPacketsTotal — RTP packet arrival count per engine-source pair.
	// Tier-1 alert when rate falls below an engine-specific threshold; the
	// edge encoder switches internally, this metric is observational.
	PcmInputPacketsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_pcm_input_packets_total",
			Help: "RTP PCM packets received per engine-source pair.",
		},
		[]string{"engine", "source"},
	)

	// DeployHistoryFailedTotal — incremented by grimnir-deploy on a failed
	// deploy. An increment is itself the tier-2 alert condition (alertmanager
	// fires on rate > 0 over 5m).
	DeployHistoryFailedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "grimnir_deploy_history_failed_total",
			Help: "Total failed deploys (an increment is the alert condition).",
		},
	)

	// RedisUnreachableSeconds — cumulative seconds the control plane's
	// Redis client has been unable to reach Redis. Tier-2 alert > 60s.
	RedisUnreachableSeconds = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "grimnir_redis_unreachable_seconds",
			Help: "Cumulative seconds Redis has been unreachable.",
		},
	)

	// CacheHitRateRatio — rolling hourly hit rate for the media cache.
	// Tier-1 alert < 0.8 (informational; capacity-planning signal, not paging).
	CacheHitRateRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_cache_hit_rate_ratio",
			Help: "Rolling 1h media-cache hit rate (0.0..1.0).",
		},
	)
)

func init() {
	// Edge encoder owns listener-facing metrics + the PCM input view.
	EdgeEncoderRegistry.MustRegister(
		ListenerReconnectTotal,
		EdgeEncoderBytesTotal,
		PcmInputPacketsTotal,
		EngineHealth, // mirror of the engines it talks to
	)

	// Control plane owns DB + Redis + VIP + cache metrics.
	GrimnirRadioRegistry.MustRegister(
		PostgresReplicationLagSeconds,
		VrrpHolderCount,
		RedisUnreachableSeconds,
		CacheHitRateRatio,
	)

	// Engine self-reports its own health.
	MediaEngineRegistry.MustRegister(
		EngineHealth,
	)

	// Deploy binary owns its failure counter.
	DeployRegistry.MustRegister(
		DeployHistoryFailedTotal,
	)
}
