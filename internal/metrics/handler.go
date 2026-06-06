/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// Handler returns an http.Handler that scrapes the given registry.
// One handler per binary; mount at /metrics.
//
// The handler also gathers from prometheus.DefaultGatherer so packages that
// register against the default registry (e.g., internal/telemetry via
// promauto) keep showing up in /metrics output. This preserves backwards
// compatibility while the HA metrics use isolated per-binary registries.
//
// Per-binary registries are auto-loaded with the go-runtime and process
// collectors at NewRegistry time, and prometheus.DefaultGatherer has its
// own copies of those collectors registered at init. We dedupe metric
// families by name so a scrape doesn't fail with "collected before".
func Handler(r *Registry) http.Handler {
	gatherer := dedupeGatherer{prometheus.Gatherers{r.Registry, prometheus.DefaultGatherer}}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		Registry:          r.Registry,
		EnableOpenMetrics: true,
	})
}

// dedupeGatherer wraps a Gatherer and removes duplicate metric families
// (same name) keeping only the first occurrence. The per-binary Registry
// is listed first, so binary-owned versions win over default-registry copies.
type dedupeGatherer struct {
	inner prometheus.Gatherer
}

func (d dedupeGatherer) Gather() ([]*dto.MetricFamily, error) {
	mfs, err := d.inner.Gather()
	if err != nil {
		// prometheus.Gatherers wraps duplicate-family errors into a
		// MultiError but still returns the merged families. Suppress the
		// duplicate-family case (we dedupe below); surface anything else.
		if !isDuplicateFamilyError(err) {
			return mfs, err
		}
	}
	seen := make(map[string]bool, len(mfs))
	out := mfs[:0]
	for _, mf := range mfs {
		name := mf.GetName()
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, mf)
	}
	return out, nil
}

func isDuplicateFamilyError(err error) bool {
	// prometheus returns a MultiError; the duplicate text is stable across
	// versions used in this repo.
	return err != nil && (containsAny(err.Error(),
		"collected before with the same name",
		"duplicate metrics collector registration",
	))
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if len(n) > 0 && len(haystack) >= len(n) {
			for i := 0; i+len(n) <= len(haystack); i++ {
				if haystack[i:i+len(n)] == n {
					return true
				}
			}
		}
	}
	return false
}
