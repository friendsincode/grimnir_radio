package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newScheduleEdgeTestDB(t *testing.T) (*gorm.DB, models.Station) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Mount{}, &models.ScheduleEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := models.Station{ID: "s1", Name: "Station One"}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "main", Format: "mp3", Bitrate: 128}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	return db, station
}

func withScheduleRouteID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func TestScheduleUpdateEntrySingleInstanceCreatesOverride(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	parent := models.ScheduleEntry{
		ID:             "parent-entry",
		StationID:      station.ID,
		MountID:        "m1",
		StartsAt:       time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		SourceType:     "playlist",
		SourceID:       "playlist-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	instanceStart := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	reqBody, _ := json.Marshal(map[string]any{
		"starts_at":   instanceStart,
		"ends_at":     instanceStart.Add(90 * time.Minute),
		"source_type": "media",
		"source_id":   "media-override",
		"metadata": map[string]any{
			"title": "Override",
		},
		"edit_mode": "single",
	})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/"+recurrenceInstanceKey(parent.ID, instanceStart), bytes.NewReader(reqBody))
	req = withScheduleRouteID(req, recurrenceInstanceKey(parent.ID, instanceStart))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var entries []models.ScheduleEntry
	if err := db.Order("created_at ASC").Find(&entries).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	override := entries[1]
	if !override.IsInstance {
		t.Fatal("expected created override to be instance")
	}
	if override.RecurrenceParentID == nil || *override.RecurrenceParentID != parent.ID {
		t.Fatalf("unexpected recurrence parent: %+v", override.RecurrenceParentID)
	}
	if override.SourceType != "media" || override.SourceID != "media-override" {
		t.Fatalf("unexpected override source: type=%q id=%q", override.SourceType, override.SourceID)
	}
	if title, _ := override.Metadata["title"].(string); title != "Override" {
		t.Fatalf("unexpected override metadata: %+v", override.Metadata)
	}
}

func TestExpandRecurringEntrySkipsOverridesAndRespectsEndDate(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)
	h := &Handler{db: db, logger: zerolog.Nop()}

	endDate := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	entry := models.ScheduleEntry{
		ID:                "parent-weekly",
		StationID:         station.ID,
		MountID:           "m1",
		StartsAt:          time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC), // Wednesday
		EndsAt:            time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC),
		SourceType:        "playlist",
		SourceID:          "playlist-1",
		RecurrenceType:    models.RecurrenceWeekly,
		RecurrenceEndDate: &endDate,
	}

	rangeStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)
	overrides := map[string]struct{}{
		recurrenceInstanceKey(entry.ID, time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)): {},
	}

	instances := h.expandRecurringEntry(entry, rangeStart, rangeEnd, overrides, time.UTC)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance after override skip and midnight end date cap, got %d", len(instances))
	}
	if !instances[0].StartsAt.Equal(time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first instance start: %v", instances[0].StartsAt)
	}
}

func TestNextOccurrenceBranches(t *testing.T) {
	h := &Handler{}
	start := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC) // Friday

	tests := []struct {
		name  string
		entry models.ScheduleEntry
		want  time.Time
		zero  bool
	}{
		{name: "daily", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceDaily}, want: start.AddDate(0, 0, 1)},
		{name: "weekdays skips weekend", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekdays}, want: time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)},
		{name: "weekly", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekly}, want: start.AddDate(0, 0, 7)},
		{name: "custom", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceCustom}, want: start.AddDate(0, 0, 1)},
		{name: "none", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceNone}, zero: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.nextOccurrence(tt.entry, start)
			if tt.zero {
				if !got.IsZero() {
					t.Fatalf("expected zero time, got %v", got)
				}
				return
			}
			if !got.Equal(tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesRecurrenceBranches(t *testing.T) {
	h := &Handler{}
	base := models.ScheduleEntry{
		StartsAt: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), // Monday
	}

	tests := []struct {
		name  string
		entry models.ScheduleEntry
		date  time.Time
		want  bool
	}{
		{name: "daily", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceDaily}, date: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), want: true},
		{name: "weekdays friday", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekdays}, date: time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC), want: true},
		{name: "weekdays saturday", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekdays}, date: time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC), want: false},
		{name: "weekly same weekday", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekly, StartsAt: base.StartsAt}, date: time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC), want: true},
		{name: "weekly different weekday", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceWeekly, StartsAt: base.StartsAt}, date: time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC), want: false},
		{name: "custom match", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceCustom, RecurrenceDays: []int{1, 3, 5}}, date: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC), want: true},
		{name: "custom empty means all", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceCustom}, date: time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), want: true},
		{name: "none", entry: models.ScheduleEntry{RecurrenceType: models.RecurrenceNone}, date: time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.matchesRecurrence(tt.entry, tt.date, time.UTC); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestScheduleUpdateEntryClearsRecurrenceEndDate(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	endDate := "2026-04-01"
	parsedEndDate, _ := time.Parse("2006-01-02", endDate)
	entry := models.ScheduleEntry{
		ID:                "entry-1",
		StationID:         station.ID,
		MountID:           "m1",
		StartsAt:          time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
		EndsAt:            time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC),
		SourceType:        "playlist",
		SourceID:          "playlist-1",
		RecurrenceType:    models.RecurrenceWeekly,
		RecurrenceEndDate: &parsedEndDate,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	reqBody, _ := json.Marshal(map[string]any{
		"starts_at":           entry.StartsAt,
		"ends_at":             entry.EndsAt,
		"recurrence_end_date": "",
		"recurrence_type":     "weekly",
		"recurrence_days":     []int{},
	})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/"+entry.ID, bytes.NewReader(reqBody))
	req = withScheduleRouteID(req, entry.ID)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.ScheduleEntry
	if err := db.First(&updated, "id = ?", entry.ID).Error; err != nil {
		t.Fatalf("reload entry: %v", err)
	}
	if updated.RecurrenceEndDate != nil {
		t.Fatalf("expected recurrence_end_date to be cleared, got %v", updated.RecurrenceEndDate)
	}
}

func TestScheduleDeleteEntryVirtualInstanceNotFound(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)
	h := &Handler{db: db, logger: zerolog.Nop()}

	virtualID := recurrenceInstanceKey("missing-parent", time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC))
	req := httptest.NewRequest(http.MethodDelete, "/dashboard/schedule/entries/"+virtualID, nil)
	req = withScheduleRouteID(req, virtualID)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleDeleteEntry(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestParseRecurringInstanceID(t *testing.T) {
	parentID, day, ok := parseRecurringInstanceID("parent_20260320")
	if !ok {
		t.Fatal("expected valid recurring instance id")
	}
	if parentID != "parent" {
		t.Fatalf("parentID = %q, want %q", parentID, "parent")
	}
	if !day.Equal(time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day = %v", day)
	}

	for _, raw := range []string{"", "no_underscore", "parent_bad-date", "short_20263"} {
		if _, _, ok := parseRecurringInstanceID(raw); ok {
			t.Fatalf("expected invalid recurring instance id for %q", raw)
		}
	}
}

func TestScheduleUpdateEntryForwardSplitsSeries(t *testing.T) {
	db, station := newScheduleEdgeTestDB(t)

	parent := models.ScheduleEntry{
		ID:             "parent-weekly-fw",
		StationID:      station.ID,
		MountID:        "m1",
		StartsAt:       time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		EndsAt:         time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		SourceType:     "smart_block",
		SourceID:       "block-1",
		RecurrenceType: models.RecurrenceWeekly,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	instanceStart := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	reqBody, _ := json.Marshal(map[string]any{
		"starts_at":   instanceStart,
		"ends_at":     instanceStart.Add(90 * time.Minute),
		"source_type": "smart_block",
		"source_id":   "block-2",
		"edit_mode":   "forward",
	})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/"+recurrenceInstanceKey(parent.ID, instanceStart), bytes.NewReader(reqBody))
	req = withScheduleRouteID(req, recurrenceInstanceKey(parent.ID, instanceStart))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Parent should now have recurrence_end_date set to the instance date (March 16)
	var updated models.ScheduleEntry
	if err := db.First(&updated, "id = ?", parent.ID).Error; err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	if updated.RecurrenceEndDate == nil {
		t.Fatal("expected parent recurrence_end_date to be set")
	}
	wantEndDate := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	if !updated.RecurrenceEndDate.Equal(wantEndDate) {
		t.Fatalf("recurrence_end_date = %v, want %v", updated.RecurrenceEndDate, wantEndDate)
	}

	// A new non-instance entry should have been created for the new series
	var entries []models.ScheduleEntry
	if err := db.Where("id != ? AND station_id = ? AND is_instance = false", parent.ID, station.ID).Find(&entries).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 new forward entry, got %d", len(entries))
	}
	fwd := entries[0]
	if fwd.IsInstance {
		t.Fatal("forward entry should not be an instance")
	}
	if !fwd.StartsAt.Equal(instanceStart) {
		t.Fatalf("forward entry StartsAt = %v, want %v", fwd.StartsAt, instanceStart)
	}
	if fwd.SourceID != "block-2" {
		t.Fatalf("forward entry SourceID = %q, want %q", fwd.SourceID, "block-2")
	}
	if fwd.RecurrenceType != models.RecurrenceWeekly {
		t.Fatalf("forward entry RecurrenceType = %q, want weekly", fwd.RecurrenceType)
	}
}

func TestScheduleWriteHandlersRequireSelectedStation(t *testing.T) {
	db, _ := newScheduleEdgeTestDB(t)
	h := &Handler{db: db, logger: zerolog.Nop()}

	createReq := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/entries", bytes.NewReader([]byte(`{}`)))
	createRR := httptest.NewRecorder()
	h.ScheduleCreateEntry(createRR, createReq)
	if createRR.Code != http.StatusBadRequest {
		t.Fatalf("create expected 400 without station, got %d", createRR.Code)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/e1", bytes.NewReader([]byte(`{}`)))
	updateReq = withScheduleRouteID(updateReq, "e1")
	updateRR := httptest.NewRecorder()
	h.ScheduleUpdateEntry(updateRR, updateReq)
	if updateRR.Code != http.StatusBadRequest {
		t.Fatalf("update expected 400 without station, got %d", updateRR.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/dashboard/schedule/entries/e1", nil)
	deleteReq = withScheduleRouteID(deleteReq, "e1")
	deleteRR := httptest.NewRecorder()
	h.ScheduleDeleteEntry(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusBadRequest {
		t.Fatalf("delete expected 400 without station, got %d", deleteRR.Code)
	}
}
