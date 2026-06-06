/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
	// Importing telemetry forces its promauto registrations into the default
	// registry so the bridge sees them.
	_ "github.com/friendsincode/grimnir_radio/internal/telemetry"
)

// The mediaengine /metrics handler must expose both HA metrics
// (MediaEngineRegistry) and the legacy mediaengine telemetry metrics
// (default registry, registered via promauto).
//
// Note: we exercise gauge/counter families that always appear in scrape
// output, not GaugeVecs without observed label combinations (those don't
// emit any samples until first Set).
func TestMediaEngineMetricsHandlerExposesLegacyAndHA(t *testing.T) {
	// Set a sample value so the GaugeVec materializes its HELP/TYPE lines.
	metrics.EngineHealth.WithLabelValues("test-node").Set(1)

	srv := httptest.NewServer(metrics.Handler(metrics.MediaEngineRegistry))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	// HA metric from MediaEngineRegistry:
	if !strings.Contains(s, "grimnir_engine_health") {
		t.Errorf("missing HA metric in scrape: %s", s)
	}
	// Legacy metric from internal/telemetry (default registry). Use a plain
	// Counter that's always present after init, not a GaugeVec.
	if !strings.Contains(s, "grimnir_scheduler_ticks_total") {
		t.Errorf("missing legacy metric in scrape: %s", s)
	}
}
