/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// withStationCtx sets the selected station on a request, as the station-select
// middleware does in production. Handlers that scope by the selected station
// need it present.
func withStationCtx(r *http.Request, s *models.Station) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyStation, s))
}

// TestCrossStationScoping_Rejects guards the IDOR cluster (#82): a manager with
// station A selected must not be able to load/mutate station B's schedule
// entries or shows by passing B's id in the URL. Every mutation handler must
// scope its lookup to the selected station and reject a foreign id, the way
// ScheduleDeleteEntry already does. Before the fix these return 2xx/3xx and
// mutate B's data.
func TestCrossStationScoping_Rejects(t *testing.T) {
	h, stationA := newShowsWebPGTest(t)

	stationB := dbtest.UUID("stationB")
	if err := h.db.Create(&models.Station{ID: stationB, OwnerID: dbtest.UUID("ownerB"), Name: "Station B"}).Error; err != nil {
		t.Fatalf("seed station B: %v", err)
	}
	showB := dbtest.UUID("showB")
	must(t, h.db.Create(&models.Show{ID: showB, StationID: stationB, Name: "B Show"}).Error)
	instB := dbtest.UUID("instB")
	must(t, h.db.Create(&models.ShowInstance{
		ID: instB, ShowID: showB, StationID: stationB,
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), Status: models.ShowInstanceScheduled,
	}).Error)
	entryB := dbtest.UUID("entryB")
	must(t, h.db.Create(&models.ScheduleEntry{
		ID: entryB, StationID: stationB, MountID: dbtest.UUID("mnt"), SourceID: dbtest.UUID("src"),
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
	}).Error)

	// Request with station A selected in context, targeting B's object id.
	reqA := func(method, id string, body string) *http.Request {
		r := webReqWithID(method, id, []byte(body))
		return r.WithContext(context.WithValue(r.Context(), ctxKeyStation, &models.Station{ID: stationA}))
	}

	cases := []struct {
		name   string
		call   func(w http.ResponseWriter, r *http.Request)
		method string
		id     string
		body   string
	}{
		{"ShowUpdate", h.ShowUpdate, "PUT", showB, `{"name":"HACKED"}`},
		{"ShowDelete", h.ShowDelete, "DELETE", showB, ""},
		{"ShowMaterialize", h.ShowMaterialize, "POST", showB, `{}`},
		{"ShowInstanceUpdate", h.ShowInstanceUpdate, "PUT", instB, `{"metadata":{"x":"y"}}`},
		{"ShowInstanceCancel", h.ShowInstanceCancel, "POST", instB, ""},
		{"ScheduleUpdateEntry", h.ScheduleUpdateEntry, "PUT", entryB, `{}`},
		{"ScheduleEntryDetails", h.ScheduleEntryDetails, "GET", entryB, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			c.call(rr, reqA(c.method, c.id, c.body))
			if rr.Code < 400 {
				t.Errorf("%s: cross-station request returned %d (want a 4xx rejection); it reached B's object", c.name, rr.Code)
			}
		})
	}

	// B's data must be intact.
	var show models.Show
	must(t, h.db.First(&show, "id = ?", showB).Error)
	if show.Name != "B Show" {
		t.Errorf("station B's show was mutated: name=%q", show.Name)
	}
}
