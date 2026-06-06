/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegistryIsIsolated(t *testing.T) {
	r1 := NewRegistry("test1")
	r2 := NewRegistry("test2")

	c1 := prometheus.NewCounter(prometheus.CounterOpts{Name: "shared_name"})
	c2 := prometheus.NewCounter(prometheus.CounterOpts{Name: "shared_name"})

	if err := r1.Register(c1); err != nil {
		t.Fatalf("r1 register: %v", err)
	}
	// Same name on a *separate* registry must NOT collide.
	if err := r2.Register(c2); err != nil {
		t.Fatalf("r2 register: %v", err)
	}

	c1.Inc()
	c1.Inc()
	if got := testutil.ToFloat64(c1); got != 2 {
		t.Errorf("c1 = %v, want 2", got)
	}
	if got := testutil.ToFloat64(c2); got != 0 {
		t.Errorf("c2 = %v, want 0 (isolated)", got)
	}
}

func TestRegistryHandlerEmitsRegisteredMetrics(t *testing.T) {
	r := NewRegistry("test-handler")
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "handler_test_total", Help: "x"})
	r.MustRegister(c)
	c.Add(7)

	body := scrapeRegistry(t, r)
	if !strings.Contains(body, "handler_test_total 7") {
		t.Errorf("scrape output missing metric: %s", body)
	}
}

func scrapeRegistry(t *testing.T, r *Registry) string {
	t.Helper()
	srv := httptest.NewServer(Handler(r))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(body)
}
