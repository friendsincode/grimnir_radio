/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import (
	"strings"
	"testing"
)

// Each HA metric must be registered into its declared registry and exposed
// via that registry's /metrics handler. The body check is intentionally a
// string-contains rather than testutil.CollectAndCount so we catch metric
// name typos that would otherwise pass under a name-keyed lookup.
//
// Deviation from plan: the prometheus client only emits `# HELP` lines for
// *Vec metrics once a label combination has been observed. We seed one
// observation per Vec metric here so the scrape body exposes the HELP line.
// The seed values are zero (or unchanged for gauges) so test assertions in
// Chunk 2 that exercise the producer side aren't affected.
func TestHAMetricsRegisteredInExpectedRegistries(t *testing.T) {
	// Seed every *Vec metric with one label-set so its HELP/TYPE lines emit.
	ListenerReconnectTotal.WithLabelValues("seed").Add(0)
	EdgeEncoderBytesTotal.WithLabelValues("seed").Add(0)
	PcmInputPacketsTotal.WithLabelValues("seed", "seed").Add(0)
	EngineHealth.WithLabelValues("seed").Set(0)
	VrrpHolderCount.WithLabelValues("seed").Set(0)

	tests := []struct {
		registry *Registry
		want     []string
	}{
		{EdgeEncoderRegistry, []string{
			"grimnir_listener_reconnect_total",
			"grimnir_edge_encoder_bytes_total",
			"grimnir_pcm_input_packets_total",
			"grimnir_engine_health",
		}},
		{GrimnirRadioRegistry, []string{
			"grimnir_postgres_replication_lag_seconds",
			"grimnir_vrrp_holder_count",
			"grimnir_redis_unreachable_seconds",
			"grimnir_cache_hit_rate_ratio",
		}},
		{MediaEngineRegistry, []string{
			"grimnir_engine_health",
		}},
		{DeployRegistry, []string{
			"grimnir_deploy_history_failed_total",
		}},
	}
	for _, tt := range tests {
		body := scrapeRegistry(t, tt.registry)
		for _, name := range tt.want {
			if !strings.Contains(body, "# HELP "+name) {
				t.Errorf("registry %q missing %q in:\n%s", tt.registry.Name, name, body)
			}
		}
	}
}
