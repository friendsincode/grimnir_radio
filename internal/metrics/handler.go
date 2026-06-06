/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns an http.Handler that scrapes the given registry.
// One handler per binary; mount at /metrics.
func Handler(r *Registry) http.Handler {
	return promhttp.HandlerFor(r.Registry, promhttp.HandlerOpts{
		Registry:          r.Registry,
		EnableOpenMetrics: true,
	})
}
