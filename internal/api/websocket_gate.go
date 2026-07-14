/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// websocketGate refuses dashboard live-update WebSocket connections when the
// runtime WebsocketEnabled toggle is off, responding 403 before the upgrade.
// It gates only the live-update event stream; the WebDJ live-broadcast socket
// is deliberately left untouched so toggling this never drops a live DJ. The
// toggle is read live per connection; see models.IsWebsocketEnabled for the
// fail-open behaviour.
func websocketGate(db *gorm.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !models.IsWebsocketEnabled(db) {
			http.Error(w, "websocket live updates are disabled", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
