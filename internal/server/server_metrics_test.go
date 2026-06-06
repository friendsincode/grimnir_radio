/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
	// Importing telemetry forces its init/promauto registrations to run so
	// the default registry has its legacy metrics by scrape time.
	_ "github.com/friendsincode/grimnir_radio/internal/telemetry"
)

// /metrics must expose BOTH legacy telemetry metrics (default registry)
// and new HA metrics (GrimnirRadioRegistry).
func TestMetricsHandlerExposesLegacyAndHA(t *testing.T) {
	srv := httptest.NewServer(metrics.Handler(metrics.GrimnirRadioRegistry))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	// HA metric from GrimnirRadioRegistry:
	if !strings.Contains(s, "grimnir_postgres_replication_lag_seconds") {
		t.Errorf("missing HA metric in scrape: %s", s)
	}
	// Legacy metric from internal/telemetry (default registry, gathered via bridge):
	if !strings.Contains(s, "grimnir_scheduler_ticks_total") {
		t.Errorf("missing legacy metric in scrape: %s", s)
	}
}
