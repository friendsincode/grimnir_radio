/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The public schedule page must drop out of the month/week grids on phones:
// those views squeeze 7 columns into ~390px (GitLab #68) and the day-grid
// nowrap rule overflows the card (GitLab #61). Below 768px the calendar
// defaults to the list agenda and only offers Day/List. Assert the rendered
// markup carries the breakpoint logic so a future refactor can't silently
// drop it and reintroduce the overflow.
func TestPublicSchedule_MobileView_RendersBreakpointLogic(t *testing.T) {
	db := newPublicTestDB(t)
	h := newPublicTestHandler(t, db)
	seedPublicStation(t, db, "st1", "Test FM")

	rr := httptest.NewRecorder()
	h.PublicSchedule(rr, publicReq(http.MethodGet, "/schedule?station_id=st1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"MOBILE_BREAKPOINT",          // the 768px guard exists
		"windowResize",               // toolbar/view swap on crossing the breakpoint
		"isMobile() ? 'listWeek'",    // phones default to the list agenda, not the grid
		"isMobile() ? mobileToolbar", // phones drop the month/week buttons
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected schedule markup to contain %q, but it did not", want)
		}
	}
}
