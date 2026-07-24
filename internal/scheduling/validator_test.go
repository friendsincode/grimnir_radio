/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduling

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func testValidator(t *testing.T) (*Validator, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{}, &models.User{}, &models.Show{}, &models.ShowInstance{},
		&models.ScheduleEntry{}, &models.ScheduleRule{}, &models.Playlist{},
		&models.SmartBlock{}, &models.ClockHour{}, &models.Webstream{}, &models.MediaItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewValidator(db, zerolog.Nop()), db
}

func at(h, m int) time.Time {
	return time.Date(2026, 3, 1, h, m, 0, 0, time.UTC)
}

func item(id string, startH, endH int) ScheduleItem {
	return ScheduleItem{ID: id, Type: "show_instance", StationID: "st1", StartsAt: at(startH, 0), EndsAt: at(endH, 0)}
}

func hostItem(id, host string, startH, endH int) ScheduleItem {
	i := item(id, startH, endH)
	i.HostUserID = &host
	return i
}

// ---------------------------------------------------------------------------
// pure helpers
// ---------------------------------------------------------------------------

func TestMaxMinTime(t *testing.T) {
	a, b := at(9, 0), at(10, 0)
	if !maxTime(a, b).Equal(b) {
		t.Fatal("maxTime wrong")
	}
	if !minTime(a, b).Equal(a) {
		t.Fatal("minTime wrong")
	}
	if !maxTime(b, a).Equal(b) || !minTime(b, a).Equal(a) {
		t.Fatal("max/minTime not symmetric")
	}
}

func TestItemLabel(t *testing.T) {
	if got := itemLabel(ScheduleItem{Display: "Show: Morning"}); got != "Show: Morning" {
		t.Fatalf("display label = %q", got)
	}
	if got := itemLabel(ScheduleItem{SourceType: "playlist"}); got != "playlist" {
		t.Fatalf("sourcetype label = %q", got)
	}
	if got := itemLabel(ScheduleItem{Type: "schedule_entry"}); got != "schedule_entry" {
		t.Fatalf("type label = %q", got)
	}
}

func TestItemsOverlap(t *testing.T) {
	v := &Validator{}
	if !v.itemsOverlap(item("a", 9, 11), item("b", 10, 12)) {
		t.Fatal("overlapping items reported as non-overlapping")
	}
	if v.itemsOverlap(item("a", 9, 10), item("b", 10, 11)) {
		t.Fatal("adjacent items must not overlap")
	}
	if !v.itemsOverlap(item("a", 9, 12), item("b", 10, 11)) {
		t.Fatal("contained item must overlap")
	}
}

// ---------------------------------------------------------------------------
// overlap / gap / duration / consecutive checks
// ---------------------------------------------------------------------------

func TestCheckOverlaps(t *testing.T) {
	v := &Validator{}
	none := v.checkOverlaps([]ScheduleItem{item("a", 9, 10), item("b", 10, 11)})
	if len(none) != 0 {
		t.Fatalf("expected no overlap, got %d", len(none))
	}
	got := v.checkOverlaps([]ScheduleItem{item("a", 9, 11), item("b", 10, 12)})
	if len(got) != 1 {
		t.Fatalf("expected 1 overlap, got %d", len(got))
	}
	if got[0].Severity != models.RuleSeverityError || got[0].RuleType != models.RuleTypeOverlap {
		t.Fatalf("wrong violation shape: %+v", got[0])
	}
	if got[0].Details["overlap_minutes"] != 60 {
		t.Fatalf("overlap_minutes = %v, want 60", got[0].Details["overlap_minutes"])
	}
}

func TestCheckGaps(t *testing.T) {
	v := &Validator{}
	rule := models.ScheduleRule{RuleType: models.RuleTypeGap, Severity: models.RuleSeverityWarning, Config: map[string]any{"max_gap_minutes": float64(30)}}
	// 9-10 then 11-12 => 60-minute gap > 30.
	items := []ScheduleItem{item("a", 9, 10), item("b", 11, 12)}
	if got := v.checkGaps(rule, items, at(9, 0), at(12, 0)); len(got) != 1 {
		t.Fatalf("expected 1 gap violation, got %d", len(got))
	}
	// Adjacent items: no gap.
	if got := v.checkGaps(rule, []ScheduleItem{item("a", 9, 10), item("b", 10, 11)}, at(9, 0), at(11, 0)); len(got) != 0 {
		t.Fatalf("expected no gap, got %d", len(got))
	}
	// ignore_hours skips the gap at hour 10.
	ignore := models.ScheduleRule{RuleType: models.RuleTypeGap, Config: map[string]any{"max_gap_minutes": float64(30), "ignore_hours": []any{float64(10)}}}
	if got := v.checkGaps(ignore, items, at(9, 0), at(12, 0)); len(got) != 0 {
		t.Fatalf("ignore_hours should suppress the gap, got %d", len(got))
	}
}

func TestCheckMinDuration(t *testing.T) {
	v := &Validator{}
	rule := models.ScheduleRule{RuleType: models.RuleTypeMinDuration, Config: map[string]any{"minutes": float64(30)}}
	short := ScheduleItem{ID: "s", StartsAt: at(9, 0), EndsAt: at(9, 10)} // 10 min
	if got := v.checkMinDuration(rule, []ScheduleItem{short}); len(got) != 1 {
		t.Fatalf("expected min-duration violation, got %d", len(got))
	}
	if got := v.checkMinDurationForItem(rule, short); len(got) != 1 {
		t.Fatalf("ForItem expected violation, got %d", len(got))
	}
	ok := ScheduleItem{ID: "o", StartsAt: at(9, 0), EndsAt: at(10, 0)}
	if got := v.checkMinDurationForItem(rule, ok); len(got) != 0 {
		t.Fatalf("ForItem should pass a 60-min item, got %d", len(got))
	}
}

func TestCheckMaxDuration(t *testing.T) {
	v := &Validator{}
	rule := models.ScheduleRule{RuleType: models.RuleTypeMaxDuration, Config: map[string]any{"minutes": float64(60)}}
	long := ScheduleItem{ID: "l", StartsAt: at(9, 0), EndsAt: at(12, 0)} // 180 min
	if got := v.checkMaxDuration(rule, []ScheduleItem{long}); len(got) != 1 {
		t.Fatalf("expected max-duration violation, got %d", len(got))
	}
	if got := v.checkMaxDurationForItem(rule, long); len(got) != 1 {
		t.Fatalf("ForItem expected violation, got %d", len(got))
	}
	ok := ScheduleItem{ID: "o", StartsAt: at(9, 0), EndsAt: at(9, 30)}
	if got := v.checkMaxDurationForItem(rule, ok); len(got) != 0 {
		t.Fatalf("ForItem should pass a 30-min item, got %d", len(got))
	}
}

func TestCheckMaxConsecutive(t *testing.T) {
	v := &Validator{}
	rule := models.ScheduleRule{RuleType: models.RuleTypeMaxConsecutive, Config: map[string]any{"max_hours": float64(4)}}
	// Same host back-to-back 9-11, 11-13, 13-15 => 6h consecutive > 4h.
	items := []ScheduleItem{hostItem("a", "dj1", 9, 11), hostItem("b", "dj1", 11, 13), hostItem("c", "dj1", 13, 15)}
	got := v.checkMaxConsecutive(rule, items)
	if len(got) != 1 {
		t.Fatalf("expected 1 consecutive violation, got %d", len(got))
	}
	if got[0].Details["consecutive_hours"].(float64) != 6 {
		t.Fatalf("consecutive_hours = %v, want 6", got[0].Details["consecutive_hours"])
	}
	// A >15min gap splits the block so neither half exceeds 4h.
	split := []ScheduleItem{hostItem("a", "dj1", 9, 11), hostItem("b", "dj1", 13, 15)}
	if got := v.checkMaxConsecutive(rule, split); len(got) != 0 {
		t.Fatalf("split blocks should not violate, got %d", len(got))
	}
}

func TestRunRule_Dispatch(t *testing.T) {
	v := &Validator{}
	items := []ScheduleItem{item("a", 9, 11), item("b", 10, 12)}
	// Unknown rule type returns nil.
	if got := v.runRule(models.ScheduleRule{RuleType: "nonexistent"}, items, at(9, 0), at(12, 0)); got != nil {
		t.Fatalf("unknown rule should return nil, got %v", got)
	}
	// Min-duration dispatches to the per-item aggregate check.
	if got := v.runRuleForItem(models.ScheduleRule{RuleType: models.RuleTypeMinDuration, Config: map[string]any{"minutes": float64(120)}}, item("a", 9, 10), items); len(got) != 1 {
		t.Fatalf("runRuleForItem min_duration expected 1, got %d", len(got))
	}
	if got := v.runRuleForItem(models.ScheduleRule{RuleType: "nope"}, item("a", 9, 10), items); got != nil {
		t.Fatalf("runRuleForItem unknown expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// DB-backed
// ---------------------------------------------------------------------------

func seedInstance(t *testing.T, db *gorm.DB, id, stationID, host string, start, end time.Time) {
	t.Helper()
	inst := models.ShowInstance{ID: id, ShowID: "show-" + id, StationID: stationID, StartsAt: start, EndsAt: end, Status: models.ShowInstanceScheduled}
	if host != "" {
		inst.HostUserID = &host
	}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	db.Create(&models.Show{ID: "show-" + id, StationID: stationID, Name: "Show " + id, DefaultDurationMinutes: 60})
}

func TestFetchScheduleItems(t *testing.T) {
	v, db := testValidator(t)
	seedInstance(t, db, "i1", "st1", "", at(9, 0), at(10, 0))
	db.Create(&models.Playlist{ID: "pl1", StationID: "st1", Name: "Drive Time"})
	db.Create(&models.ScheduleEntry{ID: "e1", StationID: "st1", StartsAt: at(11, 0), EndsAt: at(12, 0), SourceType: "playlist", SourceID: "pl1"})

	items, err := v.fetchScheduleItems("st1", at(0, 0), at(23, 0))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("fetched %d items, want 2", len(items))
	}
	// Sorted by start; first is the 9am instance, labelled from its show name.
	if items[0].Type != "show_instance" || items[0].Display != "Show: Show i1" {
		t.Fatalf("instance item wrong: %+v", items[0])
	}
	if items[1].Display != "Playlist: Drive Time" {
		t.Fatalf("entry item wrong: %+v", items[1])
	}
}

func TestValidate_DetectsOverlap(t *testing.T) {
	v, db := testValidator(t)
	seedInstance(t, db, "i1", "st1", "", at(9, 0), at(11, 0))
	seedInstance(t, db, "i2", "st1", "", at(10, 0), at(12, 0))

	res, err := v.Validate("st1", at(0, 0), at(23, 0))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.Valid {
		t.Fatal("expected schedule to be invalid due to overlap")
	}
	if len(res.Errors) != 1 || res.Errors[0].RuleType != models.RuleTypeOverlap {
		t.Fatalf("expected 1 overlap error, got %+v", res.Errors)
	}
}

func TestValidate_CustomRuleSeverities(t *testing.T) {
	v, db := testValidator(t)
	seedInstance(t, db, "i1", "st1", "", at(9, 0), at(9, 5)) // 5-minute show
	db.Create(&models.ScheduleRule{ID: "r1", StationID: "st1", Name: "Min 15", RuleType: models.RuleTypeMinDuration, Severity: models.RuleSeverityWarning, Config: map[string]any{"minutes": float64(15)}, Active: true})

	res, err := v.Validate("st1", at(0, 0), at(23, 0))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("expected 1 warning from the min-duration rule, got %d", len(res.Warnings))
	}
	if !res.Valid {
		t.Fatal("warnings alone should keep the schedule valid")
	}
}

func TestValidateItem_OverlapWithExisting(t *testing.T) {
	v, db := testValidator(t)
	seedInstance(t, db, "existing", "st1", "", at(9, 0), at(11, 0))

	candidate := ScheduleItem{ID: "new", Type: "show_instance", StationID: "st1", StartsAt: at(10, 0), EndsAt: at(12, 0)}
	violations, err := v.ValidateItem(candidate)
	if err != nil {
		t.Fatalf("validate item: %v", err)
	}
	if len(violations) != 1 || violations[0].RuleType != models.RuleTypeOverlap {
		t.Fatalf("expected 1 overlap, got %+v", violations)
	}
}

func TestCheckDJDoubleBooking(t *testing.T) {
	v, db := testValidator(t)
	// dj1 booked on st2 from 9-11 (the "other station").
	seedInstance(t, db, "other", "st2", "dj1", at(9, 0), at(11, 0))
	rule := models.ScheduleRule{ID: "r", RuleType: models.RuleTypeDJDoubleBooking, Severity: models.RuleSeverityError, Name: "No double booking"}

	// Same dj1 on st1 overlapping 10-12.
	items := []ScheduleItem{hostItem("here", "dj1", 10, 12)}
	got := v.checkDJDoubleBooking(rule, items)
	if len(got) != 1 {
		t.Fatalf("expected 1 double-booking violation, got %d", len(got))
	}
	if got[0].Details["other_station_id"] != "st2" {
		t.Fatalf("other_station_id = %v", got[0].Details["other_station_id"])
	}

	// Per-item variant.
	if fi := v.checkDJDoubleBookingForItem(rule, hostItem("here", "dj1", 10, 12), items); len(fi) != 1 {
		t.Fatalf("ForItem expected 1, got %d", len(fi))
	}
	// No host id => no check.
	if fi := v.checkDJDoubleBookingForItem(rule, item("nohost", 10, 12), items); fi != nil {
		t.Fatalf("hostless item should yield nil, got %v", fi)
	}
}
