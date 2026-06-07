/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// streamItem is the public response shape; field names are the listener-player
// contract (see docs/superpowers/plans/2026-06-06-custom-js-player.md, Chunk 1).
type streamItem struct {
	URL         string `json:"url"`
	Format      string `json:"format"`
	BitrateKbps int    `json:"bitrate_kbps"`
	Label       string `json:"label"`
}

type streamsListResponse struct {
	Streams []streamItem `json:"streams"`
}

// handleStreamsGet returns the ordered listener-facing stream URLs for a
// station. Public (no auth) so the JS player can fetch it on page load.
//
// Order: ascending Priority (HQ first, LQ second). Empty list returns
// `{"streams":[]}`, not null, so the player can iterate without a nil check.
func (a *API) handleStreamsGet(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	// Verify station exists; 404 if not. This keeps the endpoint honest:
	// a typo in the station id is a client error, not an empty stream list.
	var station models.Station
	if err := a.db.WithContext(r.Context()).
		Select("id").
		Where("id = ?", stationID).
		First(&station).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "station_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	var streams []models.StationStream
	if err := a.db.WithContext(r.Context()).
		Where("station_id = ?", stationID).
		Order("priority ASC, label ASC").
		Find(&streams).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	items := make([]streamItem, 0, len(streams))
	for _, s := range streams {
		items = append(items, streamItem{
			URL:         s.URL,
			Format:      s.Format,
			BitrateKbps: s.BitrateKbps,
			Label:       s.Label,
		})
	}

	writeJSON(w, http.StatusOK, streamsListResponse{Streams: items})
}
