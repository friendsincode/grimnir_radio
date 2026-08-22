/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package underwriting

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestFulfillmentReport_TiersAndBoundaries covers every fulfillment status tier
// and both thresholds. The pre-existing TestFulfillmentReport_StatusTiers only
// asserted the on_track case, so a regression flipping the behind (<80) or
// fulfilled (>=100) branch, or drifting the boundary, would have shipped green.
// These feed sponsor billing/compliance, so they must be exact.
func TestFulfillmentReport_TiersAndBoundaries(t *testing.T) {
	svc, db := newTestService(t)
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 7) // 1 week => required == SpotsPerWeek

	// report builds a fresh obligation with 10 required spots and `aired` aired.
	report := func(t *testing.T, aired int) *models.FulfillmentReport {
		t.Helper()
		sp := &models.Sponsor{StationID: "st1", Name: "Acme", Active: true}
		if err := svc.CreateSponsor(ctx(), sp); err != nil {
			t.Fatalf("sponsor: %v", err)
		}
		obl := &models.UnderwritingObligation{SponsorID: sp.ID, StationID: "st1", Name: "O", SpotsPerWeek: 10, StartDate: periodStart, Active: true}
		if err := svc.CreateObligation(ctx(), obl); err != nil {
			t.Fatalf("obligation: %v", err)
		}
		for i := 0; i < aired; i++ {
			s := models.NewUnderwritingSpot(obl.ID, periodStart.Add(time.Duration(i)*time.Minute))
			s.Status = models.SpotStatusAired
			db.Create(s)
		}
		r, err := svc.GetFulfillmentReport(ctx(), obl.ID, periodStart, periodEnd)
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		return r
	}

	cases := []struct {
		aired int
		want  string
	}{
		{7, "behind"},     // 70%
		{8, "on_track"},   // 80% exactly -> not behind
		{9, "on_track"},   // 90%
		{10, "fulfilled"}, // 100% exactly
		{12, "fulfilled"}, // 120% (over-delivered)
	}
	for _, c := range cases {
		r := report(t, c.aired)
		if r.Status != c.want {
			t.Errorf("%d/10 aired => status %q, want %q (rate %.0f%%)", c.aired, r.Status, c.want, r.FulfillmentRate)
		}
	}
}

// TestFulfillmentReport_MultiWeekRequired guards the spotsRequired math over a
// multi-week period: SpotsPerWeek * weeks. Only 1-week periods were tested.
func TestFulfillmentReport_MultiWeekRequired(t *testing.T) {
	svc, _ := newTestService(t)
	sp := &models.Sponsor{StationID: "st1", Name: "Acme", Active: true}
	svc.CreateSponsor(ctx(), sp)
	obl := &models.UnderwritingObligation{SponsorID: sp.ID, StationID: "st1", Name: "Q", SpotsPerWeek: 5, StartDate: time.Now(), Active: true}
	svc.CreateObligation(ctx(), obl)

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 21) // 3 weeks

	r, err := svc.GetFulfillmentReport(ctx(), obl.ID, start, end)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if r.SpotsRequired != 15 {
		t.Fatalf("SpotsRequired = %d, want 15 (5/week * 3 weeks)", r.SpotsRequired)
	}
}
