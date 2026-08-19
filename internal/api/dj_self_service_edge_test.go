/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
)

// TestDJSelfServiceHandlers_EdgeCases covers the auth and validation guards on
// the DJ self-service handlers. These all reject before any DB write, so an
// empty database is enough.
func TestDJSelfServiceHandlers_EdgeCases(t *testing.T) {
	a := &API{db: dbtest.Open(t), bus: events.NewBus(), logger: zerolog.Nop()}

	type tc struct {
		name   string
		call   func(rr *httptest.ResponseRecorder, req *http.Request)
		req    *http.Request
		claims bool
		want   int
	}
	get := func() *http.Request { return httptest.NewRequest("GET", "/", nil) }

	cases := []tc{
		{"CreateAvailability no auth", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateAvailability(rr, r) }, postJSON("/", `{}`), false, http.StatusUnauthorized},
		{"CreateAvailability bad json", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateAvailability(rr, r) }, postJSON("/", `{`), true, http.StatusBadRequest},
		{"CreateAvailability missing times", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateAvailability(rr, r) }, postJSON("/", `{}`), true, http.StatusBadRequest},
		{"CreateAvailability missing day/date", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateAvailability(rr, r) }, postJSON("/", `{"start_time":"08:00","end_time":"10:00"}`), true, http.StatusBadRequest},
		{"ListScheduleRequests no auth", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleListScheduleRequests(rr, r) }, get(), false, http.StatusUnauthorized},
		{"ListScheduleRequests missing station", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleListScheduleRequests(rr, r) }, get(), true, http.StatusBadRequest},
		{"CreateScheduleRequest no auth", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateScheduleRequest(rr, r) }, postJSON("/", `{}`), false, http.StatusUnauthorized},
		{"CreateScheduleRequest missing fields", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateScheduleRequest(rr, r) }, postJSON("/", `{}`), true, http.StatusBadRequest},
		{"CreateScheduleRequest invalid type", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleCreateScheduleRequest(rr, r) }, postJSON("/", `{"station_id":"x","request_type":"bogus"}`), true, http.StatusBadRequest},
		{"GetScheduleLock missing station", func(rr *httptest.ResponseRecorder, r *http.Request) { a.handleGetScheduleLock(rr, r) }, get(), true, http.StatusBadRequest},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := c.req
			if c.claims {
				req = withAdminClaims(req)
			}
			rr := httptest.NewRecorder()
			c.call(rr, req)
			if rr.Code != c.want {
				t.Fatalf("got %d, want %d (body=%s)", rr.Code, c.want, rr.Body.String())
			}
		})
	}
}
