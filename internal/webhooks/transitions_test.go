/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webhooks

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestCheckTransitions_EndAndChange guards #86: only the "new show started"
// transition was tested. The show-ended (previous set, none current) and
// show-changed (previous and current both set) branches are the ones that
// actually fire the end/start webhooks; a bug in the map bookkeeping would
// silently stop firing them.
func TestCheckTransitions_EndAndChange(t *testing.T) {
	svc, db := testService(t)
	now := time.Now()
	db.Create(&models.Station{ID: "st1", Name: "Rock", Public: true})
	db.Create(&models.Show{ID: "sh1", StationID: "st1", Name: "New Show"})
	db.Create(&models.ShowInstance{ID: "cur", ShowID: "sh1", StationID: "st1",
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Status: models.ShowInstanceScheduled})

	// Show changed: a previous show is active, now "cur" is on air.
	changed := map[string]string{"st1": "old"}
	svc.checkTransitions(t.Context(), changed)
	if changed["st1"] != "cur" {
		t.Fatalf("show-changed: active = %q, want cur", changed["st1"])
	}

	// Show ended: "cur" was active but its window has now passed and nothing
	// replaces it -> the station goes to no active show.
	db.Model(&models.ShowInstance{}).Where("id = ?", "cur").Update("ends_at", now.Add(-time.Minute))
	ended := map[string]string{"st1": "cur"}
	svc.checkTransitions(t.Context(), ended)
	if ended["st1"] != "" {
		t.Fatalf("show-ended: active = %q, want empty", ended["st1"])
	}
}
