/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package clock

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
)

// TestCompileMultiHourSmartBlockDedup verifies that a smart-block slot with a
// duration longer than one hour is only planned ONCE per clock window.
// Regression test for the bug where the 11:00 plan for a 10–12 window with a
// 2-hour smart block would be emitted even after the 10:00 plan was generated,
// causing the scheduler to create a second batch of entries at 11:00 when the
// smart block only partially filled the slot.
func TestCompileMultiHourSmartBlockDedup(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-sb-dedup"
	if err := db.Create(&models.Station{ID: stationID, Name: "Dedup", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	// 2-hour clock window (10-12) with a single 2-hour smart block slot
	if err := db.Create(&models.ClockHour{
		ID:        "clock-sb",
		StationID: stationID,
		Name:      "Show",
		StartHour: 10,
		EndHour:   12,
		Slots: []models.ClockSlot{{
			ID:          "slot-sb",
			ClockHourID: "clock-sb",
			Position:    0,
			Offset:      0,
			Type:        models.SlotTypeSmartBlock,
			// 2-hour duration — same as the clock window
			Payload: map[string]any{"smart_block_id": "sb1", "duration_ms": float64(7200000)},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	start := time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 24*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Should only generate ONE plan (at 10:00), not two (10:00 and 11:00).
	if len(plans) != 1 {
		t.Fatalf("plans len = %d, want 1 (multi-hour slot must not be planned twice per window)", len(plans))
	}
	if !plans[0].StartsAt.Equal(time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("plan.StartsAt = %v, want 10:00", plans[0].StartsAt)
	}
	if plans[0].Duration != 2*time.Hour {
		t.Errorf("plan.Duration = %v, want 2h", plans[0].Duration)
	}
}

func TestCompileFullDayClock(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-fullday"
	if err := db.Create(&models.Station{ID: stationID, Name: "Full Day", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.ClockHour{
		ID: "clock-full", StationID: stationID, Name: "All Day",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-full", ClockHourID: "clock-full", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "p1"},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 24*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(plans) != 24 {
		t.Fatalf("plans len = %d, want 24", len(plans))
	}
	for i, plan := range plans {
		wantHour := time.Date(2026, 3, 1, i, 0, 0, 0, time.UTC)
		if !plan.StartsAt.Equal(wantHour) {
			t.Errorf("plan[%d].StartsAt = %v, want %v", i, plan.StartsAt, wantHour)
		}
	}
}

func TestCompileEmptyClockNoSlots(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-empty"
	if err := db.Create(&models.Station{ID: stationID, Name: "Empty", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.ClockHour{
		ID: "clock-empty", StationID: stationID, Name: "No Slots",
		StartHour: 0, EndHour: 24,
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 4*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("plans len = %d, want 0 for empty clock", len(plans))
	}
}

func TestCompileMultipleSlotTypes(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-multi"
	if err := db.Create(&models.Station{ID: stationID, Name: "Multi", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.ClockHour{
		ID: "clock-multi", StationID: stationID, Name: "Mixed",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{
			{ID: "slot-sb", ClockHourID: "clock-multi", Position: 0, Offset: 0,
				Type: models.SlotTypeSmartBlock, Payload: map[string]any{"smart_block_id": "sb1", "duration_ms": float64(900000)}},
			{ID: "slot-pl", ClockHourID: "clock-multi", Position: 1, Offset: 15 * time.Minute,
				Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "pl1", "duration_ms": float64(900000)}},
			{ID: "slot-hi", ClockHourID: "clock-multi", Position: 2, Offset: 30 * time.Minute,
				Type: models.SlotTypeHardItem, Payload: map[string]any{"media_id": "m1", "duration_ms": float64(180000)}},
			{ID: "slot-ws", ClockHourID: "clock-multi", Position: 3, Offset: 45 * time.Minute,
				Type: models.SlotTypeWebstream, Payload: map[string]any{"webstream_id": "ws1"}},
		},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(plans) != 4 {
		t.Fatalf("plans len = %d, want 4", len(plans))
	}

	wantTypes := []string{"smart_block", "playlist", "hard_item", "webstream"}
	wantOffsets := []time.Duration{0, 15 * time.Minute, 30 * time.Minute, 45 * time.Minute}
	for i, plan := range plans {
		if plan.SlotType != wantTypes[i] {
			t.Errorf("plan[%d].SlotType = %q, want %q", i, plan.SlotType, wantTypes[i])
		}
		wantStart := start.Add(wantOffsets[i])
		if !plan.StartsAt.Equal(wantStart) {
			t.Errorf("plan[%d].StartsAt = %v, want %v", i, plan.StartsAt, wantStart)
		}
	}
}

func TestCompileOvernightClock22to6(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-overnight"
	if err := db.Create(&models.Station{ID: stationID, Name: "Overnight", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.ClockHour{
		ID: "clock-night", StationID: stationID, Name: "Night",
		StartHour: 22, EndHour: 6,
		Slots: []models.ClockSlot{{
			ID: "slot-night", ClockHourID: "clock-night", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "night"},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	start := time.Date(2026, 3, 1, 20, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 12*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Should have plans for hours 22,23,0,1,2,3,4,5 = 8 plans
	if len(plans) != 8 {
		t.Fatalf("plans len = %d, want 8", len(plans))
	}
	wantHours := []int{22, 23, 0, 1, 2, 3, 4, 5}
	for i, plan := range plans {
		gotHour := plan.StartsAt.Hour()
		if gotHour != wantHours[i] {
			t.Errorf("plan[%d] hour = %d, want %d", i, gotHour, wantHours[i])
		}
	}
}

func TestCompileOverlappingClocksThreeTiers(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-tiers"
	if err := db.Create(&models.Station{ID: stationID, Name: "Tiers", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	// Broadest: 0-24
	if err := db.Create(&models.ClockHour{
		ID: "clock-broad", StationID: stationID, Name: "Broad",
		StartHour: 0, EndHour: 24,
		Slots: []models.ClockSlot{{
			ID: "slot-broad", ClockHourID: "clock-broad", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "broad"},
		}},
	}).Error; err != nil {
		t.Fatalf("create broad clock: %v", err)
	}
	// Medium: 8-20
	if err := db.Create(&models.ClockHour{
		ID: "clock-medium", StationID: stationID, Name: "Medium",
		StartHour: 8, EndHour: 20,
		Slots: []models.ClockSlot{{
			ID: "slot-medium", ClockHourID: "clock-medium", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "medium"},
		}},
	}).Error; err != nil {
		t.Fatalf("create medium clock: %v", err)
	}
	// Narrowest: 10-14
	if err := db.Create(&models.ClockHour{
		ID: "clock-narrow", StationID: stationID, Name: "Narrow",
		StartHour: 10, EndHour: 14,
		Slots: []models.ClockSlot{{
			ID: "slot-narrow", ClockHourID: "clock-narrow", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "narrow"},
		}},
	}).Error; err != nil {
		t.Fatalf("create narrow clock: %v", err)
	}

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 24*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	slotByHour := make(map[int]string)
	for _, plan := range plans {
		slotByHour[plan.StartsAt.Hour()] = plan.SlotID
	}

	// Hour 12 → narrowest (10-14)
	if slotByHour[12] != "slot-narrow" {
		t.Errorf("hour 12 slot = %q, want slot-narrow", slotByHour[12])
	}
	// Hour 16 → medium (8-20)
	if slotByHour[16] != "slot-medium" {
		t.Errorf("hour 16 slot = %q, want slot-medium", slotByHour[16])
	}
	// Hour 4 → broadest (0-24)
	if slotByHour[4] != "slot-broad" {
		t.Errorf("hour 4 slot = %q, want slot-broad", slotByHour[4])
	}
}

func TestCompileTimezoneConversion(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-tz"
	if err := db.Create(&models.Station{ID: stationID, Name: "TZ", Timezone: "America/New_York"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	// Clock active 9-17 local (EST = UTC-5)
	if err := db.Create(&models.ClockHour{
		ID: "clock-tz", StationID: stationID, Name: "Business",
		StartHour: 9, EndHour: 17,
		Slots: []models.ClockSlot{{
			ID: "slot-tz", ClockHourID: "clock-tz", Position: 0, Offset: 0,
			Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "biz"},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	// 13:00 UTC = 8:00 AM EST → clock should NOT apply (before 9am local)
	start13 := time.Date(2026, 1, 15, 13, 0, 0, 0, time.UTC)
	plans13, err := planner.Compile(stationID, start13, time.Hour)
	if err != nil {
		t.Fatalf("compile at 13 UTC: %v", err)
	}
	if len(plans13) != 0 {
		t.Errorf("at 13:00 UTC (8am EST) expected 0 plans, got %d", len(plans13))
	}

	// 14:00 UTC = 9:00 AM EST → clock SHOULD apply
	start14 := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	plans14, err := planner.Compile(stationID, start14, time.Hour)
	if err != nil {
		t.Fatalf("compile at 14 UTC: %v", err)
	}
	if len(plans14) != 1 {
		t.Errorf("at 14:00 UTC (9am EST) expected 1 plan, got %d", len(plans14))
	}
}

func TestCompileWebstreamSpanDuration(t *testing.T) {
	db := newPlannerTestDB(t)
	planner := NewPlanner(db, zerolog.Nop())

	stationID := "station-ws-span"
	if err := db.Create(&models.Station{ID: stationID, Name: "WS", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	// 6-hour clock with webstream (no duration_ms)
	if err := db.Create(&models.ClockHour{
		ID: "clock-ws", StationID: stationID, Name: "WS Clock",
		StartHour: 10, EndHour: 16,
		Slots: []models.ClockSlot{{
			ID: "slot-ws", ClockHourID: "clock-ws", Position: 0, Offset: 0,
			Type: models.SlotTypeWebstream, Payload: map[string]any{"webstream_id": "ws1"},
		}},
	}).Error; err != nil {
		t.Fatalf("create clock: %v", err)
	}

	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	plans, err := planner.Compile(stationID, start, 6*time.Hour)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Webstream dedup should collapse multiple hours into one plan
	if len(plans) != 1 {
		t.Fatalf("plans len = %d, want 1 (dedup should collapse webstream)", len(plans))
	}
	// Duration should cover remaining window from start
	if plans[0].Duration < 5*time.Hour {
		t.Errorf("webstream duration = %v, want >= 5h (remaining in window)", plans[0].Duration)
	}
}

func TestSlotPayloadDuration(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    time.Duration
	}{
		{"duration_ms float64", map[string]any{"duration_ms": float64(5000)}, 5 * time.Second},
		{"duration_seconds float64", map[string]any{"duration_seconds": float64(120)}, 2 * time.Minute},
		{"both present ms wins", map[string]any{"duration_ms": float64(3000), "duration_seconds": float64(120)}, 3 * time.Second},
		{"neither present", map[string]any{}, 0},
		{"nil payload", nil, 0},
		{"zero ms", map[string]any{"duration_ms": float64(0)}, 0},
		{"negative ms", map[string]any{"duration_ms": float64(-100)}, 0},
		{"json.Number ms", map[string]any{"duration_ms": json.Number("5000")}, 5 * time.Second},
		{"int ms", map[string]any{"duration_ms": int(3000)}, 3 * time.Second},
		{"int64 ms", map[string]any{"duration_ms": int64(3000)}, 3 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slotPayloadDuration(tt.payload)
			if got != tt.want {
				t.Errorf("slotPayloadDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeClockWindow(t *testing.T) {
	tests := []struct {
		name               string
		startHour, endHour int
		wantStart, wantEnd int
	}{
		{"valid 6-12", 6, 12, 6, 12},
		{"start negative", -1, 12, 0, 12},
		{"start too high", 25, 12, 0, 12},
		{"end zero", 6, 0, 6, 24},
		{"end too high", 6, 25, 6, 24},
		{"full day", 0, 24, 0, 24},
		{"both invalid", -1, 0, 0, 24},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := normalizeClockWindow(tt.startHour, tt.endHour)
			if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Errorf("normalizeClockWindow(%d, %d) = (%d, %d), want (%d, %d)",
					tt.startHour, tt.endHour, gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestClockWindowApplies(t *testing.T) {
	tests := []struct {
		name               string
		startHour, endHour int
		hour               int
		want               bool
	}{
		{"same start end always true", 10, 10, 5, true},
		{"0-24 always true", 0, 24, 15, true},
		{"in range", 6, 12, 8, true},
		{"at start", 6, 12, 6, true},
		{"at exclusive end", 6, 12, 12, false},
		{"before range", 6, 12, 4, false},
		{"overnight in late", 22, 6, 23, true},
		{"overnight in early", 22, 6, 3, true},
		{"overnight at start", 22, 6, 22, true},
		{"overnight at end excluded", 22, 6, 6, false},
		{"overnight outside", 22, 6, 12, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := models.ClockHour{StartHour: tt.startHour, EndHour: tt.endHour}
			got := clockWindowApplies(ch, tt.hour)
			if got != tt.want {
				t.Errorf("clockWindowApplies(start=%d, end=%d, hour=%d) = %v, want %v",
					tt.startHour, tt.endHour, tt.hour, got, tt.want)
			}
		})
	}
}

func TestClockSpan(t *testing.T) {
	tests := []struct {
		name               string
		startHour, endHour int
		want               time.Duration
	}{
		{"6-12", 6, 12, 6 * time.Hour},
		{"0-24", 0, 24, 24 * time.Hour},
		{"22-6 overnight", 22, 6, 8 * time.Hour},
		{"10-10 same", 10, 10, 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := models.ClockHour{StartHour: tt.startHour, EndHour: tt.endHour}
			got := clockSpan(ch)
			if got != tt.want {
				t.Errorf("clockSpan(start=%d, end=%d) = %v, want %v",
					tt.startHour, tt.endHour, got, tt.want)
			}
		})
	}
}
