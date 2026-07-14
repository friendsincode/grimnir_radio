/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// metricsGate wraps the Prometheus metrics handler so it is only served when the
// runtime MetricsEnabled toggle is on. When disabled the endpoint returns 404,
// so it looks unmounted rather than forbidden. The toggle is read live per
// request; see models.IsMetricsEnabled for the fail-open behaviour.
func metricsGate(db *gorm.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !models.IsMetricsEnabled(db) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
