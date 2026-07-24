/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package underwriting

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{}, &models.Show{}, &models.ShowInstance{}, &models.MediaItem{},
		&models.Sponsor{}, &models.UnderwritingObligation{}, &models.UnderwritingSpot{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(db, zerolog.Nop()), db
}

func ctx() context.Context { return context.Background() }

func TestGetDaypart(t *testing.T) {
	cases := map[int]models.Daypart{
		7:  models.DaypartMorning,
		12: models.DaypartMidday,
		16: models.DaypartAfternoon,
		20: models.DaypartEvening,
		3:  models.DaypartOvernight,
		0:  models.DaypartOvernight,
	}
	for hour, want := range cases {
		if got := getDaypart(hour); got != want {
			t.Fatalf("getDaypart(%d) = %s, want %s", hour, got, want)
		}
	}
}

func TestSponsorCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	sp := &models.Sponsor{StationID: "st1", Name: "Acme", Active: true}
	if err := svc.CreateSponsor(ctx(), sp); err != nil {
		t.Fatalf("create: %v", err)
	}
	if sp.ID == "" {
		t.Fatal("CreateSponsor should assign an ID")
	}

	got, err := svc.GetSponsor(ctx(), sp.ID)
	if err != nil || got.Name != "Acme" {
		t.Fatalf("get: %v / %+v", err, got)
	}

	// Second, inactive sponsor to exercise the activeOnly filter. The Active
	// column defaults to true at insert, so force it off afterward.
	inactive := &models.Sponsor{StationID: "st1", Name: "Zzz"}
	svc.CreateSponsor(ctx(), inactive)
	svc.db.Model(inactive).Update("active", false)

	all, _ := svc.ListSponsors(ctx(), "st1", false)
	if len(all) != 2 {
		t.Fatalf("list all = %d, want 2", len(all))
	}
	active, _ := svc.ListSponsors(ctx(), "st1", true)
	if len(active) != 1 || active[0].Name != "Acme" {
		t.Fatalf("list active = %+v", active)
	}

	if err := svc.UpdateSponsor(ctx(), sp.ID, map[string]any{"name": "Acme Corp"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := svc.GetSponsor(ctx(), sp.ID)
	if updated.Name != "Acme Corp" {
		t.Fatalf("name after update = %q", updated.Name)
	}

	if _, err := svc.GetSponsor(ctx(), "missing"); err == nil {
		t.Fatal("expected error for missing sponsor")
	}
}

func TestDeleteSponsor_Cascades(t *testing.T) {
	svc, db := newTestService(t)
	sp := &models.Sponsor{StationID: "st1", Name: "Acme", Active: true}
	svc.CreateSponsor(ctx(), sp)
	obl := &models.UnderwritingObligation{SponsorID: sp.ID, StationID: "st1", SpotsPerWeek: 3, StartDate: time.Now(), Active: true}
	svc.CreateObligation(ctx(), obl)
	svc.ScheduleSpot(ctx(), obl.ID, time.Now(), nil)

	if err := svc.DeleteSponsor(ctx(), sp.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var sponsors, obligations, spots int64
	db.Model(&models.Sponsor{}).Count(&sponsors)
	db.Model(&models.UnderwritingObligation{}).Count(&obligations)
	db.Model(&models.UnderwritingSpot{}).Count(&spots)
	if sponsors != 0 || obligations != 0 || spots != 0 {
		t.Fatalf("cascade left rows: sponsors=%d obligations=%d spots=%d", sponsors, obligations, spots)
	}
}

func TestObligationCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	obl := &models.UnderwritingObligation{SponsorID: "sp1", StationID: "st1", SpotsPerWeek: 5, StartDate: time.Now(), Active: true}
	if err := svc.CreateObligation(ctx(), obl); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.GetObligation(ctx(), obl.ID)
	if err != nil || got.SpotsPerWeek != 5 {
		t.Fatalf("get: %v / %+v", err, got)
	}

	// Expired obligation excluded by activeOnly's end_date guard.
	past := time.Now().AddDate(0, 0, -1)
	svc.CreateObligation(ctx(), &models.UnderwritingObligation{SponsorID: "sp1", StationID: "st1", SpotsPerWeek: 1, StartDate: time.Now().AddDate(0, 0, -30), EndDate: &past, Active: true})

	active, _ := svc.ListObligations(ctx(), "st1", "", true)
	if len(active) != 1 {
		t.Fatalf("active obligations = %d, want 1", len(active))
	}
	bySponsor, _ := svc.ListObligations(ctx(), "", "sp1", false)
	if len(bySponsor) != 2 {
		t.Fatalf("by sponsor = %d, want 2", len(bySponsor))
	}

	if err := svc.UpdateObligation(ctx(), obl.ID, map[string]any{"spots_per_week": 8}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := svc.DeleteObligation(ctx(), obl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetObligation(ctx(), obl.ID); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSpotLifecycleAndMissed(t *testing.T) {
	svc, db := newTestService(t)
	obl := &models.UnderwritingObligation{SponsorID: "sp1", StationID: "st1", SpotsPerWeek: 2, StartDate: time.Now(), Active: true}
	svc.CreateObligation(ctx(), obl)

	spot, err := svc.ScheduleSpot(ctx(), obl.ID, time.Now(), nil)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := svc.MarkSpotAired(ctx(), spot.ID); err != nil {
		t.Fatalf("aired: %v", err)
	}
	var reloaded models.UnderwritingSpot
	db.First(&reloaded, "id = ?", spot.ID)
	if reloaded.Status != models.SpotStatusAired || reloaded.AiredAt == nil {
		t.Fatalf("spot not marked aired: %+v", reloaded)
	}

	// A stale scheduled spot gets swept to missed.
	stale := models.NewUnderwritingSpot(obl.ID, time.Now().Add(-2*time.Hour))
	db.Create(stale)
	n, err := svc.CheckMissedSpots(ctx())
	if err != nil {
		t.Fatalf("check missed: %v", err)
	}
	if n != 1 {
		t.Fatalf("missed swept = %d, want 1", n)
	}

	// MarkSpotMissed on a fresh spot.
	fresh, _ := svc.ScheduleSpot(ctx(), obl.ID, time.Now(), nil)
	if err := svc.MarkSpotMissed(ctx(), fresh.ID); err != nil {
		t.Fatalf("mark missed: %v", err)
	}
}

func TestScheduleWeeklySpots(t *testing.T) {
	svc, db := newTestService(t)
	obl := &models.UnderwritingObligation{SponsorID: "sp1", StationID: "st1", SpotsPerWeek: 2, StartDate: time.Now(), Active: true}
	svc.CreateObligation(ctx(), obl)

	weekStart := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // Monday
	// Three scheduled instances during the week; only 2 spots needed.
	for i, h := range []int{8, 12, 20} {
		db.Create(&models.ShowInstance{
			ID: string(rune('a'+i)) + "-inst", ShowID: "sh1", StationID: "st1",
			StartsAt: weekStart.AddDate(0, 0, i).Add(time.Duration(h) * time.Hour),
			EndsAt:   weekStart.AddDate(0, 0, i).Add(time.Duration(h+1) * time.Hour),
			Status:   models.ShowInstanceScheduled,
		})
	}

	spots, err := svc.ScheduleWeeklySpots(ctx(), "st1", weekStart)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if len(spots) != 2 {
		t.Fatalf("scheduled %d spots, want 2 (capped at SpotsPerWeek)", len(spots))
	}

	// Re-running the same week schedules nothing more (quota already met).
	again, _ := svc.ScheduleWeeklySpots(ctx(), "st1", weekStart)
	if len(again) != 0 {
		t.Fatalf("re-run scheduled %d, want 0", len(again))
	}
}

func TestGetAvailableInstances_DaypartFilter(t *testing.T) {
	svc, db := newTestService(t)
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)
	// One morning (8am) and one evening (20:00) instance.
	db.Create(&models.ShowInstance{ID: "m", ShowID: "sh1", StationID: "st1", StartsAt: start.Add(8 * time.Hour), EndsAt: start.Add(9 * time.Hour), Status: models.ShowInstanceScheduled})
	db.Create(&models.ShowInstance{ID: "e", ShowID: "sh1", StationID: "st1", StartsAt: start.Add(20 * time.Hour), EndsAt: start.Add(21 * time.Hour), Status: models.ShowInstanceScheduled})

	got, err := svc.getAvailableInstances(ctx(), "st1", start, end, "morning", "")
	if err != nil {
		t.Fatalf("get available: %v", err)
	}
	if len(got) != 1 || got[0].ID != "m" {
		t.Fatalf("daypart filter = %+v, want just morning instance", got)
	}
}

func TestFulfillmentReport_StatusTiers(t *testing.T) {
	svc, db := newTestService(t)
	sp := &models.Sponsor{StationID: "st1", Name: "Acme", Active: true}
	svc.CreateSponsor(ctx(), sp)
	obl := &models.UnderwritingObligation{SponsorID: sp.ID, StationID: "st1", Name: "Q1", SpotsPerWeek: 10, StartDate: time.Now(), Active: true}
	svc.CreateObligation(ctx(), obl)

	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 7) // 1 week => 10 required

	mkSpot := func(status models.SpotStatus, when time.Time) {
		s := models.NewUnderwritingSpot(obl.ID, when)
		s.Status = status
		db.Create(s)
	}
	// 9 aired of 10 required => 90% => on_track.
	for i := 0; i < 9; i++ {
		mkSpot(models.SpotStatusAired, periodStart.Add(time.Duration(i)*time.Hour))
	}
	mkSpot(models.SpotStatusMissed, periodStart)

	report, err := svc.GetFulfillmentReport(ctx(), obl.ID, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.SpotsRequired != 10 || report.SpotsAired != 9 || report.SpotsMissed != 1 {
		t.Fatalf("report counts wrong: %+v", report)
	}
	if report.Status != "on_track" {
		t.Fatalf("status = %q, want on_track (90%%)", report.Status)
	}
	if report.SponsorName != "Acme" {
		t.Fatalf("sponsor name = %q", report.SponsorName)
	}

	// All reports across the station.
	reports, err := svc.GetAllFulfillmentReports(ctx(), "st1", periodStart, periodEnd)
	if err != nil || len(reports) != 1 {
		t.Fatalf("all reports: %v / %d", err, len(reports))
	}
}
