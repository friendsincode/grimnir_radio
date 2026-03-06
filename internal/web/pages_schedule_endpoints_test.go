package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
)

type stubSchedulerService struct {
	stationID string
	err       error
}

func (s *stubSchedulerService) RefreshStation(_ context.Context, stationID string) error {
	s.stationID = stationID
	return s.err
}

func (s *stubSchedulerService) Materialize(_ context.Context, _ smartblock.GenerateRequest) (smartblock.GenerateResult, error) {
	return smartblock.GenerateResult{}, errors.New("unused")
}

func newScheduleEndpointTestHandler(t *testing.T) (*Handler, *gorm.DB, models.User, models.Station) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.Show{},
		&models.ShowInstance{},
		&models.ScheduleRule{},
		&models.ScheduleEntry{},
		&models.PlayHistory{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.MediaItem{},
		&models.SmartBlock{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.Webstream{},
		&models.SystemSettings{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{ID: "u1", Email: "manager@example.com", Password: "x", CalendarColorTheme: "forest"}
	station := models.Station{ID: "s1", Name: "Station One", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{ID: "su1", UserID: user.ID, StationID: station.ID, Role: models.StationRoleManager}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	return h, db, user, station
}

func scheduleRequest(method, target string, user *models.User, station *models.Station, routeID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if routeID != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", routeID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	return req.WithContext(ctx)
}

func TestScheduleCalendarRendersMountsAndTheme(t *testing.T) {
	h, db, user, station := newScheduleEndpointTestHandler(t)
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "Main Mount", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	rr := httptest.NewRecorder()
	h.ScheduleCalendar(rr, scheduleRequest(http.MethodGet, "/dashboard/schedule", &user, &station, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Schedule", "Main Mount", "const colorTheme = 'forest'", "validateScheduleBtn"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}

func TestScheduleRefreshHTMX(t *testing.T) {
	h, _, _, station := newScheduleEndpointTestHandler(t)

	t.Run("success returns hx success message", func(t *testing.T) {
		stub := &stubSchedulerService{}
		h.scheduler = stub
		req := scheduleRequest(http.MethodPost, "/dashboard/schedule/refresh", nil, &station, "")
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()

		h.ScheduleRefresh(rr, req)
		if rr.Code != http.StatusOK || stub.stationID != station.ID {
			t.Fatalf("unexpected refresh response: code=%d station=%q body=%s", rr.Code, stub.stationID, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "Schedule refresh queued") {
			t.Fatalf("unexpected success body: %s", rr.Body.String())
		}
	})

	t.Run("failure returns hx error message", func(t *testing.T) {
		stub := &stubSchedulerService{err: errors.New("boom")}
		h.scheduler = stub
		req := scheduleRequest(http.MethodPost, "/dashboard/schedule/refresh", nil, &station, "")
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()

		h.ScheduleRefresh(rr, req)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Failed to refresh schedule") {
			t.Fatalf("unexpected error response: code=%d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestScheduleValidateCapsRangeAndReturnsJSON(t *testing.T) {
	h, _, _, station := newScheduleEndpointTestHandler(t)
	req := scheduleRequest(
		http.MethodGet,
		"/dashboard/schedule/validate?start=2026-03-01T00:00:00Z&end=2026-07-01T00:00:00Z",
		nil,
		&station,
		"",
	)
	rr := httptest.NewRecorder()

	h.ScheduleValidate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var result models.ValidationResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode validation response: %v", err)
	}
	if !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	if got := result.RangeEnd.Sub(result.RangeStart); got != 90*24*time.Hour {
		t.Fatalf("expected 90 day cap, got %v", got)
	}
}

func TestScheduleEventsReturnsExpandedRecurringAndOrphanedEntries(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	for _, record := range []any{
		&models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3"},
		&models.Playlist{ID: "pl-1", StationID: station.ID, Name: "Playlist One"},
		&models.MediaItem{ID: "media-1", StationID: station.ID, Title: "Track One", Artist: "Artist One", Duration: 3 * time.Minute, Path: "track.mp3"},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	entries := []models.ScheduleEntry{
		{
			ID:             "weekly-parent",
			StationID:      station.ID,
			MountID:        "m1",
			StartsAt:       time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC),
			EndsAt:         time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC),
			SourceType:     "playlist",
			SourceID:       "pl-1",
			RecurrenceType: models.RecurrenceWeekly,
		},
		{
			ID:                 "weekly-override",
			StationID:          station.ID,
			MountID:            "m1",
			StartsAt:           time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC),
			EndsAt:             time.Date(2026, 3, 11, 11, 0, 0, 0, time.UTC),
			SourceType:         "media",
			SourceID:           "media-1",
			IsInstance:         true,
			RecurrenceParentID: func() *string { s := "weekly-parent"; return &s }(),
		},
		{
			ID:         "orphan-entry",
			StationID:  station.ID,
			MountID:    "m1",
			StartsAt:   time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC),
			EndsAt:     time.Date(2026, 3, 5, 13, 0, 0, 0, time.UTC),
			SourceType: "playlist",
			SourceID:   "missing-playlist",
		},
		{
			ID:         "webstream-entry",
			StationID:  station.ID,
			MountID:    "m1",
			StartsAt:   time.Date(2026, 3, 6, 9, 0, 0, 0, time.UTC),
			EndsAt:     time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC),
			SourceType: "live",
			Metadata:   map[string]any{"session_name": "Morning Live"},
		},
	}
	for _, entry := range entries {
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create entry: %v", err)
		}
	}

	req := scheduleRequest(
		http.MethodGet,
		"/dashboard/schedule/events?start=2026-03-01T00:00:00Z&end=2026-03-20T00:00:00Z&view=timeGridDay&mount_id=m1",
		nil,
		&station,
		"",
	)
	rr := httptest.NewRecorder()

	h.ScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode events payload: %v", err)
	}
	if len(payload) != 5 {
		t.Fatalf("expected 5 events, got %+v", payload)
	}

	titles := make(map[string]map[string]any, len(payload))
	playlistCount := 0
	for _, event := range payload {
		titles[event["title"].(string)] = event
		if event["title"] == "Playlist One" {
			playlistCount++
		}
	}
	if playlistCount != 2 {
		t.Fatalf("expected 2 recurring playlist instances, got %+v", payload)
	}
	if _, ok := titles["Playlist One"]; !ok {
		t.Fatalf("expected recurring playlist event in payload: %+v", payload)
	}
	if _, ok := titles["Artist One - Track One"]; !ok {
		t.Fatalf("expected override media event in payload: %+v", payload)
	}
	orphan, ok := titles["⚠ MISSING Playlist"]
	if !ok {
		t.Fatalf("expected orphaned event in payload: %+v", payload)
	}
	if orphan["className"] != "event-orphaned" {
		t.Fatalf("expected orphaned class, got %+v", orphan)
	}
	if _, ok := titles["Morning Live"]; !ok {
		t.Fatalf("expected live metadata title in payload: %+v", payload)
	}
}

func TestScheduleEventsHonorsMountFilterAndMetadataFallbacks(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	for _, record := range []any{
		&models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3"},
		&models.Mount{ID: "m2", StationID: station.ID, Name: "Alt", Format: "mp3"},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	entries := []models.ScheduleEntry{
		{ID: "show-meta", StationID: station.ID, MountID: "m1", StartsAt: time.Date(2026, 3, 7, 8, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 7, 9, 0, 0, 0, time.UTC), SourceType: "show", Metadata: map[string]any{"title": "Metadata Show"}},
		{ID: "fallback-yellow", StationID: station.ID, MountID: "m1", StartsAt: time.Date(2026, 3, 7, 9, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC), SourceType: "live", Metadata: map[string]any{"emergency_fallback": true}},
		{ID: "other-mount", StationID: station.ID, MountID: "m2", StartsAt: time.Date(2026, 3, 7, 11, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC), SourceType: "live", Metadata: map[string]any{"session_name": "Other Mount"}},
	}
	for _, entry := range entries {
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create entry: %v", err)
		}
	}

	req := scheduleRequest(http.MethodGet, "/dashboard/schedule/events?start=2026-03-07T00:00:00Z&end=2026-03-08T00:00:00Z&mount_id=m1&view=listWeek", nil, &station, "")
	rr := httptest.NewRecorder()
	h.ScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("expected 2 filtered events, got %+v", payload)
	}
	byID := map[string]map[string]any{}
	for _, event := range payload {
		byID[event["id"].(string)] = event
	}
	if byID["show-meta"]["title"] != "Metadata Show" {
		t.Fatalf("expected metadata show title, got %+v", byID["show-meta"])
	}
	props := byID["fallback-yellow"]["extendedProps"].(map[string]any)
	if props["health"] != "yellow" || byID["fallback-yellow"]["title"] != "Live Session" {
		t.Fatalf("expected yellow fallback live event, got %+v %+v", byID["fallback-yellow"], props)
	}
}

func TestScheduleEventsExpandsSmartBlocksAndCarriesRecurrenceMetadata(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	for _, record := range []any{
		&models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3"},
		&models.MediaItem{ID: "media-1", StationID: station.ID, Title: "Block Track", Artist: "Block Artist", Duration: 180 * time.Second, Path: "block.mp3", Genre: "rock"},
		&models.SmartBlock{ID: "sb-expand", StationID: station.ID, Name: "Expandable Block", Rules: map[string]any{"genre": "rock", "targetMinutes": 3}},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	entry := models.ScheduleEntry{
		ID:                "smart-recurring",
		StationID:         station.ID,
		MountID:           "m1",
		StartsAt:          time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC),
		EndsAt:            time.Date(2026, 3, 8, 13, 3, 0, 0, time.UTC),
		SourceType:        "smart_block",
		SourceID:          "sb-expand",
		RecurrenceType:    models.RecurrenceDaily,
		RecurrenceDays:    []int{},
		RecurrenceEndDate: func() *time.Time { t := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC); return &t }(),
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	req := scheduleRequest(http.MethodGet, "/dashboard/schedule/events?start=2026-03-08T00:00:00Z&end=2026-03-10T23:59:59Z&view=timeGridDay&mount_id=m1", nil, &station, "")
	rr := httptest.NewRecorder()
	h.ScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var parentFound, expandedFound bool
	for _, event := range payload {
		id := event["id"].(string)
		props := event["extendedProps"].(map[string]any)
		if strings.HasPrefix(id, "smart-recurring_") && props["source_type"] == "smart_block" {
			parentFound = true
			recurrence := props["recurrence"].(map[string]any)
			if recurrence["type"] != "daily" || recurrence["end_date"] != "2026-03-10" {
				t.Fatalf("unexpected recurrence metadata: %+v", recurrence)
			}
		}
		if strings.Contains(id, "-t-media-1") {
			expandedFound = true
			if props["source_type"] != "media" {
				t.Fatalf("expected expanded child media source, got %+v", props)
			}
			metadata := props["metadata"].(map[string]any)
			if metadata["expanded"] != true || metadata["smart_block_id"] != "sb-expand" {
				t.Fatalf("unexpected expanded metadata: %+v", metadata)
			}
		}
	}
	if !parentFound || !expandedFound {
		t.Fatalf("expected recurring smart block parent and expanded child events, got %+v", payload)
	}
}

func TestScheduleEntryDetailsReturnsMediaAndWebstreamDetails(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	if err := db.Create(&models.MediaItem{ID: "media-1", StationID: station.ID, Title: "Track One", Artist: "Artist One", Duration: 3 * time.Minute, Path: "track.mp3"}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := db.Create(&models.Webstream{ID: "ws-1", StationID: station.ID, Name: "Relay", URLs: []string{"https://relay.example/stream"}}).Error; err != nil {
		t.Fatalf("create webstream: %v", err)
	}
	entries := []models.ScheduleEntry{
		{ID: "entry-media", StationID: station.ID, MountID: "m1", StartsAt: time.Now().UTC(), EndsAt: time.Now().UTC().Add(time.Hour), SourceType: "media", SourceID: "media-1"},
		{ID: "entry-webstream", StationID: station.ID, MountID: "m1", StartsAt: time.Now().UTC(), EndsAt: time.Now().UTC().Add(time.Hour), SourceType: "webstream", SourceID: "ws-1"},
	}
	for _, entry := range entries {
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create entry: %v", err)
		}
	}

	t.Run("media details include track metadata", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-media", nil, &station, "entry-media"))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		media := payload["media"].(map[string]any)
		if media["title"] != "Track One" || media["artist"] != "Artist One" {
			t.Fatalf("unexpected media payload: %+v", media)
		}
	})

	t.Run("webstream details include primary url", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-webstream", nil, &station, "entry-webstream"))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		webstream := payload["webstream"].(map[string]any)
		if webstream["name"] != "Relay" || webstream["url"] != "https://relay.example/stream" {
			t.Fatalf("unexpected webstream payload: %+v", webstream)
		}
	})
}

func TestScheduleSourceTracksAppliesPlaylistOverrides(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	media := []models.MediaItem{
		{ID: "track-1", StationID: station.ID, Title: "First", Artist: "Artist A", Duration: 2 * time.Minute, Path: "first.mp3"},
		{ID: "track-2", StationID: station.ID, Title: "Second", Artist: "Artist B", Duration: 3 * time.Minute, Path: "second.mp3"},
		{ID: "track-3", StationID: station.ID, Title: "Replacement", Artist: "Artist C", Duration: 4 * time.Minute, Path: "replacement.mp3"},
	}
	for _, item := range media {
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}
	playlist := models.Playlist{ID: "pl-1", StationID: station.ID, Name: "Playlist One"}
	if err := db.Create(&playlist).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	for i, mediaID := range []string{"track-1", "track-2"} {
		if err := db.Create(&models.PlaylistItem{ID: "pli-" + mediaID, PlaylistID: playlist.ID, MediaID: mediaID, Position: i + 1}).Error; err != nil {
			t.Fatalf("create playlist item: %v", err)
		}
	}
	entry := models.ScheduleEntry{
		ID:        "entry-overrides",
		StationID: station.ID,
		MountID:   "m1",
		StartsAt:  time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 3, 6, 11, 0, 0, 0, time.UTC),
		Metadata:  map[string]any{"track_overrides": map[string]any{"0": "track-3", "1": "__remove__"}},
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	req := scheduleRequest(http.MethodGet,
		"/dashboard/schedule/source-tracks?source_type=playlist&source_id=pl-1&starts_at=2026-03-06T10:00:00Z&ends_at=2026-03-06T11:00:00Z&mount_id=m1&entry_id=entry-overrides",
		nil, &station, "")
	rr := httptest.NewRecorder()

	h.ScheduleSourceTracks(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		SourceName    string `json:"source_name"`
		TrackCount    int    `json:"track_count"`
		TotalDuration int64  `json:"total_duration"`
		Tracks        []struct {
			MediaID  string `json:"media_id"`
			Title    string `json:"title"`
			Artist   string `json:"artist"`
			Duration int64  `json:"duration"`
		} `json:"tracks"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.SourceName != "Playlist One" || payload.TrackCount != 1 || len(payload.Tracks) != 1 {
		t.Fatalf("unexpected source payload: %+v", payload)
	}
	if payload.Tracks[0].MediaID != "track-3" || payload.Tracks[0].Title != "Replacement" || payload.TotalDuration != 240 {
		t.Fatalf("unexpected tracks after overrides: %+v", payload.Tracks)
	}
}

func TestScheduleDropdownAndSearchEndpoints(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	otherStation := models.Station{ID: "s2", Name: "Archive Station", Active: true}
	for _, record := range []any{
		&models.Playlist{ID: "pl-1", StationID: station.ID, Name: "Morning Playlist"},
		&models.SmartBlock{ID: "sb-1", StationID: station.ID, Name: "Rotation Block"},
		&models.ClockHour{ID: "clock-1", StationID: station.ID, Name: "Top Hour"},
		&models.Webstream{ID: "ws-1", StationID: station.ID, Name: "News Relay", URLs: []string{"https://relay.example/stream"}},
		&models.MediaItem{ID: "media-local", StationID: station.ID, Title: "Local Track", Artist: "Artist Local", Duration: 90 * time.Second, Path: "local.mp3"},
		&otherStation,
		&models.MediaItem{ID: "media-archive", StationID: otherStation.ID, Title: "Archive Track", Artist: "Artist Archive", Duration: 75 * time.Second, Path: "archive.mp3", ShowInArchive: true},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	if err := db.Create(&models.ClockSlot{ID: "slot-1", ClockHourID: "clock-1", Position: 1, Type: models.SlotTypeHardItem, Payload: map[string]any{"media_id": "media-local"}}).Error; err != nil {
		t.Fatalf("create clock slot: %v", err)
	}

	t.Run("playlist smartblock clock and webstream dropdowns return station records", func(t *testing.T) {
		tests := []struct {
			name string
			path string
			key  string
			want string
		}{
			{name: "playlists", path: "/dashboard/schedule/playlists", key: "playlists", want: "Morning Playlist"},
			{name: "smartblocks", path: "/dashboard/schedule/smartblocks", key: "smart_blocks", want: "Rotation Block"},
			{name: "clocks", path: "/dashboard/schedule/clocks", key: "clocks", want: "Top Hour"},
			{name: "webstreams", path: "/dashboard/schedule/webstreams", key: "webstreams", want: "News Relay"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := scheduleRequest(http.MethodGet, tt.path, nil, &station, "")
				rr := httptest.NewRecorder()
				switch tt.name {
				case "playlists":
					h.SchedulePlaylistsJSON(rr, req)
				case "smartblocks":
					h.ScheduleSmartBlocksJSON(rr, req)
				case "clocks":
					h.ScheduleClocksJSON(rr, req)
				case "webstreams":
					h.ScheduleWebstreamsJSON(rr, req)
				}
				if rr.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
				}
				if !strings.Contains(rr.Body.String(), tt.want) {
					t.Fatalf("expected body to contain %q, got %s", tt.want, rr.Body.String())
				}
			})
		}
	})

	t.Run("media search can include archive items from other stations", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/media-search?q=track&include_archive=true", nil, &station, "")
		rr := httptest.NewRecorder()

		h.ScheduleMediaSearchJSON(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload struct {
			Items []struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				StationName string `json:"station_name"`
				IsArchive   bool   `json:"is_archive"`
			} `json:"items"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("expected 2 items, got %+v", payload.Items)
		}
		if !payload.Items[0].IsArchive && !payload.Items[1].IsArchive {
			t.Fatalf("expected archive item in results: %+v", payload.Items)
		}
	})
}

func TestScheduleCreateUpdateDeleteRoundTripAndEvents(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	h.eventBus = events.NewBus()
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	createSub := h.eventBus.Subscribe(events.EventScheduleUpdate)
	defer h.eventBus.Unsubscribe(events.EventScheduleUpdate, createSub)

	createBody, _ := json.Marshal(map[string]any{
		"mount_id":            "m1",
		"starts_at":           time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC),
		"ends_at":             time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC),
		"source_type":         "live",
		"source_id":           "",
		"recurrence_type":     "custom",
		"recurrence_days":     []int{2, 4},
		"recurrence_end_date": "2026-03-31",
		"metadata": map[string]any{
			"session_name": "Afternoon Live",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/entries", bytes.NewReader(createBody))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleCreateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created models.ScheduleEntry
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.SourceType != "live" || created.SourceID == "" {
		t.Fatalf("expected normalized live source id, got %+v", created)
	}
	if created.RecurrenceType != models.RecurrenceCustom || len(created.RecurrenceDays) != 2 {
		t.Fatalf("unexpected recurrence fields: %+v", created)
	}
	if created.RecurrenceEndDate == nil || created.RecurrenceEndDate.Format("2006-01-02") != "2026-03-31" {
		t.Fatalf("unexpected recurrence end date: %+v", created.RecurrenceEndDate)
	}
	select {
	case payload := <-createSub:
		if payload["event"] != "create" || payload["entry_id"] != created.ID {
			t.Fatalf("unexpected create event payload: %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected create schedule event")
	}

	updateSub := h.eventBus.Subscribe(events.EventScheduleUpdate)
	defer h.eventBus.Unsubscribe(events.EventScheduleUpdate, updateSub)
	updateBody, _ := json.Marshal(map[string]any{
		"starts_at":           time.Date(2026, 3, 10, 16, 0, 0, 0, time.UTC),
		"ends_at":             time.Date(2026, 3, 10, 17, 30, 0, 0, time.UTC),
		"source_type":         "webstream",
		"source_id":           "ws-1",
		"recurrence_type":     "weekly",
		"recurrence_days":     []int{},
		"recurrence_end_date": "",
		"metadata": map[string]any{
			"note": "updated",
		},
	})
	req = httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/"+created.ID, bytes.NewReader(updateBody))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", created.ID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr = httptest.NewRecorder()

	h.ScheduleUpdateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var updated models.ScheduleEntry
	if err := db.First(&updated, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("reload updated entry: %v", err)
	}
	if updated.SourceType != "webstream" || updated.SourceID != "ws-1" {
		t.Fatalf("unexpected updated source fields: %+v", updated)
	}
	if updated.RecurrenceType != models.RecurrenceWeekly || updated.RecurrenceEndDate != nil {
		t.Fatalf("unexpected updated recurrence fields: %+v", updated)
	}
	if updated.Metadata["note"] != "updated" {
		t.Fatalf("unexpected updated metadata: %+v", updated.Metadata)
	}
	select {
	case payload := <-updateSub:
		if payload["event"] != "update" || payload["entry_id"] != created.ID {
			t.Fatalf("unexpected update event payload: %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected update schedule event")
	}

	deleteSub := h.eventBus.Subscribe(events.EventScheduleUpdate)
	defer h.eventBus.Unsubscribe(events.EventScheduleUpdate, deleteSub)
	req = httptest.NewRequest(http.MethodDelete, "/dashboard/schedule/entries/"+created.ID, nil)
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("id", created.ID)
	ctx = context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr = httptest.NewRecorder()

	h.ScheduleDeleteEntry(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	var count int64
	db.Model(&models.ScheduleEntry{}).Where("id = ?", created.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected deleted entry, remaining=%d", count)
	}
	select {
	case payload := <-deleteSub:
		if payload["event"] != "delete" || payload["entry_id"] != created.ID {
			t.Fatalf("unexpected delete event payload: %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected delete schedule event")
	}
}

func TestScheduleWritePathValidationErrors(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}
	existing := models.ScheduleEntry{ID: "existing", StationID: station.ID, MountID: "m1", StartsAt: time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 12, 11, 0, 0, 0, time.UTC), SourceType: "media", SourceID: "m"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create existing entry: %v", err)
	}

	t.Run("create rejects inverted time range", func(t *testing.T) {
		body := bytes.NewBufferString(`{"mount_id":"m1","starts_at":"2026-03-12T12:00:00Z","ends_at":"2026-03-12T11:00:00Z","source_type":"media","source_id":"m1"}`)
		req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/entries", body)
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
		rr := httptest.NewRecorder()
		h.ScheduleCreateEntry(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("update rejects overlap with sibling entry", func(t *testing.T) {
		entry := models.ScheduleEntry{ID: "update-me", StationID: station.ID, MountID: "m1", StartsAt: time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 12, 13, 0, 0, 0, time.UTC), SourceType: "media", SourceID: "m2"}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create update entry: %v", err)
		}
		body := bytes.NewBufferString(`{"starts_at":"2026-03-12T10:30:00Z","ends_at":"2026-03-12T11:30:00Z"}`)
		req := httptest.NewRequest(http.MethodPut, "/dashboard/schedule/entries/update-me", body)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "update-me")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, ctxKeyStation, &station)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		h.ScheduleUpdateEntry(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("delete rejects missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/dashboard/schedule/entries/missing", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "missing")
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		ctx = context.WithValue(ctx, ctxKeyStation, &station)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		h.ScheduleDeleteEntry(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestScheduleEntryDetailsAndSourceTracksClockAndPlaylistBranches(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	for _, record := range []any{
		&models.MediaItem{ID: "media-hard", StationID: station.ID, Title: "Hard Track", Artist: "Artist Hard", Duration: 150 * time.Second, Path: "hard.mp3"},
		&models.MediaItem{ID: "media-pl", StationID: station.ID, Title: "Playlist Track", Artist: "Artist Playlist", Duration: 180 * time.Second, Path: "playlist.mp3"},
		&models.MediaItem{ID: "media-sb", StationID: station.ID, Title: "Smart Slot Track", Artist: "Artist Smart", Genre: "ClockGenre", Duration: 210 * time.Second, Path: "smart.mp3", AnalysisState: models.AnalysisComplete},
		&models.Playlist{ID: "pl-clock", StationID: station.ID, Name: "Clock Playlist"},
		&models.SmartBlock{ID: "sb-clock", StationID: station.ID, Name: "Clock Smart Block", Rules: map[string]any{"genre": "ClockGenre"}},
		&models.ClockHour{ID: "clock-1", StationID: station.ID, Name: "Clock One"},
		&models.Webstream{ID: "ws-slot", StationID: station.ID, Name: "Slot Relay", URLs: []string{"https://slot.example/stream"}},
		&models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3"},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	if err := db.Create(&models.PlaylistItem{ID: "pli-1", PlaylistID: "pl-clock", MediaID: "media-pl", Position: 1}).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}
	for _, slot := range []models.ClockSlot{
		{ID: "slot-pl", ClockHourID: "clock-1", Position: 1, Offset: 0, Type: models.SlotTypePlaylist, Payload: map[string]any{"playlist_id": "pl-clock"}},
		{ID: "slot-hard", ClockHourID: "clock-1", Position: 2, Offset: 15 * time.Minute, Type: models.SlotTypeHardItem, Payload: map[string]any{"media_id": "media-hard"}},
		{ID: "slot-web", ClockHourID: "clock-1", Position: 3, Offset: 30 * time.Minute, Type: models.SlotTypeWebstream, Payload: map[string]any{"webstream_id": "ws-slot"}},
		{ID: "slot-sb", ClockHourID: "clock-1", Position: 4, Offset: 45 * time.Minute, Type: models.SlotTypeSmartBlock, Payload: map[string]any{"smart_block_id": "sb-clock"}},
	} {
		if err := db.Create(&slot).Error; err != nil {
			t.Fatalf("create clock slot: %v", err)
		}
	}
	entry := models.ScheduleEntry{ID: "entry-clock", StationID: station.ID, MountID: "m1", StartsAt: time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 3, 12, 13, 0, 0, 0, time.UTC), SourceType: "clock_template", SourceID: "clock-1"}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if err := db.Create(&models.PlayHistory{
		ID:        "ph-1",
		StationID: station.ID,
		MountID:   "m1",
		MediaID:   "media-hard",
		Title:     "Hard Track",
		Artist:    "Artist Hard",
		StartedAt: entry.StartsAt.Add(16 * time.Minute),
		EndedAt:   entry.StartsAt.Add(18 * time.Minute),
		Metadata:  map[string]any{"clock_id": "clock-1"},
	}).Error; err != nil {
		t.Fatalf("create play history: %v", err)
	}

	t.Run("entry details returns clock slots and trace", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-clock", nil, &station, "entry-clock")
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["clock"] == nil || payload["clock_trace"] == nil || len(payload["slots"].([]any)) != 4 {
			t.Fatalf("unexpected clock detail payload: %+v", payload)
		}
		clockTrace := payload["clock_trace"].(map[string]any)
		played := clockTrace["played_tracks"].([]any)
		if len(played) != 1 {
			t.Fatalf("expected one played track in trace, got %+v", played)
		}
		queued := clockTrace["queued_slots"].([]any)
		if len(queued) != 4 {
			t.Fatalf("expected four queued slot traces, got %+v", queued)
		}
		webSlot := queued[2].(map[string]any)
		if webSlot["webstream"] == nil {
			t.Fatalf("expected webstream trace on third slot, got %+v", webSlot)
		}
		smartSlot := queued[3].(map[string]any)
		preview := smartSlot["smart_block_preview"].(map[string]any)
		if preview["status"] != "ready" || int(preview["track_count"].(float64)) == 0 {
			t.Fatalf("expected resolved smart block preview on fourth slot, got %+v", smartSlot)
		}
	})

	t.Run("source tracks expands clock playlist hard item and smart block", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/source-tracks?source_type=clock_template&source_id=clock-1&starts_at=2026-03-12T12:00:00Z&ends_at=2026-03-12T13:00:00Z&mount_id=m1", nil, &station, "")
		rr := httptest.NewRecorder()
		h.ScheduleSourceTracks(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload struct {
			SourceName string `json:"source_name"`
			TrackCount int    `json:"track_count"`
			Tracks     []struct {
				Title string `json:"title"`
			} `json:"tracks"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.SourceName != "Clock One" || payload.TrackCount != 3 {
			t.Fatalf("unexpected clock source tracks payload: %+v", payload)
		}
		titles := []string{payload.Tracks[0].Title, payload.Tracks[1].Title, payload.Tracks[2].Title}
		if !(containsStringValue(titles, "Playlist Track") && containsStringValue(titles, "Hard Track") && containsStringValue(titles, "Smart Slot Track")) {
			t.Fatalf("unexpected track titles: %+v", titles)
		}
	})
}

func containsStringValue(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestScheduleHelpersUseSystemSettingsAndDeterministicSeed(t *testing.T) {
	h, db, _, _ := newScheduleEndpointTestHandler(t)

	t.Run("scheduler lookahead defaults and honors valid settings", func(t *testing.T) {
		if got := h.schedulerLookaheadDuration(); got != 168*time.Hour {
			t.Fatalf("expected default 168h lookahead, got %v", got)
		}
		if err := db.Save(&models.SystemSettings{ID: 1, SchedulerLookahead: "48h"}).Error; err != nil {
			t.Fatalf("save settings: %v", err)
		}
		if got := h.schedulerLookaheadDuration(); got != 48*time.Hour {
			t.Fatalf("expected configured 48h lookahead, got %v", got)
		}
		if err := db.Save(&models.SystemSettings{ID: 1, SchedulerLookahead: "garbage"}).Error; err != nil {
			t.Fatalf("save invalid settings: %v", err)
		}
		if got := h.schedulerLookaheadDuration(); got != 168*time.Hour {
			t.Fatalf("expected invalid setting to fall back to 168h, got %v", got)
		}
	})

	t.Run("smart block seed is deterministic and changes with entry fields", func(t *testing.T) {
		entry := models.ScheduleEntry{
			ID:        "entry-1",
			StationID: "s1",
			MountID:   "m1",
			StartsAt:  time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC),
			EndsAt:    time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
		}
		seedA := deterministicSmartBlockSeed(entry, "sb-1")
		seedB := deterministicSmartBlockSeed(entry, "sb-1")
		if seedA != seedB {
			t.Fatalf("expected deterministic seed, got %d and %d", seedA, seedB)
		}
		entry.EndsAt = entry.EndsAt.Add(time.Minute)
		seedC := deterministicSmartBlockSeed(entry, "sb-1")
		if seedC == seedA {
			t.Fatalf("expected changed entry timing to change seed, got %d", seedC)
		}
	})
}

func TestScheduleSourceTracksMediaWebstreamAndValidationSummary(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	for _, record := range []any{
		&models.MediaItem{ID: "media-plain", StationID: station.ID, Title: "Plain Track", Artist: "Artist Plain", Duration: 95 * time.Second, Path: "plain.mp3"},
		&models.Webstream{ID: "ws-1", StationID: station.ID, Name: "News Relay", URLs: []string{"https://relay.example/stream"}},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}

	t.Run("source tracks rejects missing source params", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/source-tracks", nil, &station, "")
		rr := httptest.NewRecorder()
		h.ScheduleSourceTracks(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("media source returns direct track payload", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/source-tracks?source_type=media&source_id=media-plain", nil, &station, "")
		rr := httptest.NewRecorder()
		h.ScheduleSourceTracks(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload struct {
			TrackCount int `json:"track_count"`
			Tracks     []struct {
				Title string `json:"title"`
			} `json:"tracks"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.TrackCount != 1 || payload.Tracks[0].Title != "Plain Track" {
			t.Fatalf("unexpected media tracks payload: %+v", payload)
		}
	})

	t.Run("webstream source returns metadata without tracks", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/source-tracks?source_type=webstream&source_id=ws-1", nil, &station, "")
		rr := httptest.NewRecorder()
		h.ScheduleSourceTracks(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload struct {
			SourceName string `json:"source_name"`
			URL        string `json:"url"`
			TrackCount int    `json:"track_count"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.SourceName != "News Relay" || payload.URL != "https://relay.example/stream" || payload.TrackCount != 0 {
			t.Fatalf("unexpected webstream payload: %+v", payload)
		}
	})

	t.Run("smart block source returns error when unresolved", func(t *testing.T) {
		if err := db.Create(&models.SmartBlock{ID: "sb-empty-2", StationID: station.ID, Name: "Empty Block", Rules: map[string]any{"genre": "NoSuchGenreAnywhere"}}).Error; err != nil {
			t.Fatalf("create smart block: %v", err)
		}
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/source-tracks?source_type=smart_block&source_id=sb-empty-2&starts_at=2026-03-12T10:00:00Z&ends_at=2026-03-12T11:00:00Z&mount_id=m1", nil, &station, "")
		rr := httptest.NewRecorder()
		h.ScheduleSourceTracks(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["error"] == nil || int(payload["track_count"].(float64)) != 0 {
			t.Fatalf("unexpected smart block error payload: %+v", payload)
		}
	})

	t.Run("validation summary helper handles nil and overlap batches", func(t *testing.T) {
		h.logValidationSummary(station.ID, time.Now().UTC(), time.Now().UTC().Add(time.Hour), nil)
		h.logValidationSummary(station.ID, time.Now().UTC(), time.Now().UTC().Add(time.Hour), &models.ValidationResult{
			Valid:    false,
			Errors:   []models.ValidationViolation{{RuleType: models.RuleTypeOverlap, Message: "error overlap", Details: map[string]any{"overlap_minutes": 10}}},
			Warnings: []models.ValidationViolation{{RuleType: models.RuleTypeOverlap, Message: "warning overlap"}},
			Info:     []models.ValidationViolation{{RuleType: models.RuleTypeGap, Message: "info gap"}},
		})
	})
}

func TestPublicScheduleAndEventsFilterToPublicStations(t *testing.T) {
	h, db, user, _ := newScheduleEndpointTestHandler(t)
	now := time.Now().UTC()
	publicStation := models.Station{ID: "pub-1", Name: "Public One", Active: true, Public: true, Approved: true, SortOrder: 2}
	publicStation2 := models.Station{ID: "pub-2", Name: "Public Two", Active: true, Public: true, Approved: true, SortOrder: 1}
	privateStation := models.Station{ID: "priv-1", Name: "Private", Active: true, Public: false, Approved: true}
	for _, record := range []any{
		&publicStation,
		&publicStation2,
		&privateStation,
		&models.ScheduleEntry{ID: "pub-live", StationID: publicStation.ID, MountID: "m1", StartsAt: now.Add(2 * time.Hour), EndsAt: now.Add(3 * time.Hour), SourceType: "live", Metadata: map[string]any{"session_name": "Public Live"}},
		&models.ScheduleEntry{ID: "pub-media", StationID: publicStation.ID, MountID: "m1", StartsAt: now.Add(2 * time.Hour), EndsAt: now.Add(2*time.Hour + 3*time.Minute), SourceType: "media", SourceID: "media-hidden"},
		&models.ScheduleEntry{ID: "priv-live", StationID: privateStation.ID, MountID: "m1", StartsAt: now.Add(2 * time.Hour), EndsAt: now.Add(3 * time.Hour), SourceType: "live", Metadata: map[string]any{"session_name": "Private Live"}},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}

	t.Run("public schedule page lists only public stations", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/schedule?station_id=pub-1", &user, nil, "")
		rr := httptest.NewRecorder()
		h.PublicSchedule(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		for _, want := range []string{"Public One", "Public Two", "const colorTheme = 'forest'"} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected body to contain %q", want)
			}
		}
		if strings.Contains(body, "Private") {
			t.Fatalf("did not expect private station in body: %s", body)
		}
	})

	t.Run("public schedule events returns only public non-media events", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/schedule/events?start=2026-01-01T00:00:00Z&end=2099-01-01T00:00:00Z&theme=ocean", nil, nil, "")
		rr := httptest.NewRecorder()
		h.PublicScheduleEvents(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload []map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if len(payload) != 1 {
			t.Fatalf("expected one public non-media event, got %+v", payload)
		}
		if payload[0]["title"] != "Public Live" || payload[0]["backgroundColor"] != "#14b8a6" {
			t.Fatalf("unexpected public events payload: %+v", payload)
		}
		props := payload[0]["extendedProps"].(map[string]any)
		if props["station_name"] != "Public One" {
			t.Fatalf("expected station name in public event, got %+v", props)
		}
	})

	t.Run("public schedule events with no public stations returns empty array", func(t *testing.T) {
		if err := db.Model(&models.Station{}).Where("public = ?", true).Updates(map[string]any{"active": false}).Error; err != nil {
			t.Fatalf("deactivate public stations: %v", err)
		}
		req := scheduleRequest(http.MethodGet, "/schedule/events", nil, nil, "")
		rr := httptest.NewRecorder()
		h.PublicScheduleEvents(rr, req)
		if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
			t.Fatalf("expected empty array, got code=%d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestPublicScheduleEventsExpandsRecurringProgramsAndPrefersOverrides(t *testing.T) {
	h, db, _, _ := newScheduleEndpointTestHandler(t)
	publicStation := models.Station{ID: "pub-rec", Name: "Recurring Public", Active: true, Public: true, Approved: true}
	if err := db.Create(&publicStation).Error; err != nil {
		t.Fatalf("create public station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "pub-mount", StationID: publicStation.ID, Name: "Public Mount", Format: "mp3"}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	startA := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
	parentID := "recurring-live"
	if err := db.Create(&models.ScheduleEntry{
		ID:                 parentID,
		StationID:          publicStation.ID,
		MountID:            "pub-mount",
		StartsAt:           startA,
		EndsAt:             startA.Add(time.Hour),
		SourceType:         "live",
		RecurrenceType:     models.RecurrenceDaily,
		Metadata:           map[string]any{"title": "Daily Public Show"},
		RecurrenceDays:     []int{},
		IsInstance:         false,
		RecurrenceParentID: nil,
	}).Error; err != nil {
		t.Fatalf("create recurring parent: %v", err)
	}

	overrideParent := parentID
	overrideStart := startA.Add(24 * time.Hour)
	if err := db.Create(&models.ScheduleEntry{
		ID:                 "recurring-live-override",
		StationID:          publicStation.ID,
		MountID:            "pub-mount",
		StartsAt:           overrideStart,
		EndsAt:             overrideStart.Add(90 * time.Minute),
		SourceType:         "live",
		IsInstance:         true,
		RecurrenceParentID: &overrideParent,
		Metadata:           map[string]any{"session_name": "Override Special"},
	}).Error; err != nil {
		t.Fatalf("create recurring override: %v", err)
	}

	req := scheduleRequest(http.MethodGet, "/schedule/events?start=2026-03-10T00:00:00Z&end=2026-03-12T23:59:59Z&theme=forest", nil, nil, "")
	rr := httptest.NewRecorder()
	h.PublicScheduleEvents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload) != 3 {
		t.Fatalf("expected three events after recurrence expansion, got %+v", payload)
	}

	var sawParentDay1, sawParentDay3, sawOverride bool
	for _, event := range payload {
		switch event["id"] {
		case "recurring-live_20260310":
			sawParentDay1 = true
			if event["title"] != "Live Session" {
				t.Fatalf("expected public live fallback title on day 1, got %+v", event)
			}
		case "recurring-live_20260311":
			t.Fatalf("virtual instance should have been suppressed by explicit override: %+v", event)
		case "recurring-live_20260312":
			sawParentDay3 = true
		case "recurring-live-override":
			sawOverride = true
			if event["title"] != "Override Special" {
				t.Fatalf("expected override session name, got %+v", event)
			}
		}
	}
	if !sawParentDay1 || !sawParentDay3 || !sawOverride {
		t.Fatalf("missing expected recurring/override events: %+v", payload)
	}
}

func TestScheduleEntryDetailsPlaylistAndPendingSmartBlock(t *testing.T) {
	h, db, _, station := newScheduleEndpointTestHandler(t)
	if err := db.Save(&models.SystemSettings{ID: 1, SchedulerLookahead: "24h"}).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}
	for _, record := range []any{
		&models.MediaItem{ID: "media-1", StationID: station.ID, Title: "Track One", Artist: "Artist One", Duration: 210 * time.Second, Path: "track1.mp3"},
		&models.MediaItem{ID: "media-ready-1", StationID: station.ID, Title: "Ready One", Artist: "Artist Ready", Genre: "ReadyGenre", Duration: 240 * time.Second, Path: "ready1.mp3", AnalysisState: models.AnalysisComplete},
		&models.MediaItem{ID: "media-ready-2", StationID: station.ID, Title: "Ready Two", Artist: "Artist Ready", Genre: "ReadyGenre", Duration: 180 * time.Second, Path: "ready2.mp3", AnalysisState: models.AnalysisComplete},
		&models.Playlist{ID: "pl-1", StationID: station.ID, Name: "Playlist One"},
		&models.SmartBlock{ID: "sb-1", StationID: station.ID, Name: "Smart Future", Rules: map[string]any{"mode": "test"}},
		&models.SmartBlock{ID: "sb-ready", StationID: station.ID, Name: "Smart Ready", Rules: map[string]any{"genre": "ReadyGenre"}},
		&models.SmartBlock{ID: "sb-empty", StationID: station.ID, Name: "Smart Empty", Rules: map[string]any{"genre": "NoSuchGenreAnywhere"}},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	if err := db.Create(&models.PlaylistItem{ID: "pli-1", PlaylistID: "pl-1", MediaID: "media-1", Position: 1}).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}
	entries := []models.ScheduleEntry{
		{ID: "entry-playlist", StationID: station.ID, MountID: "m1", StartsAt: time.Now().UTC(), EndsAt: time.Now().UTC().Add(time.Hour), SourceType: "playlist", SourceID: "pl-1"},
		{ID: "entry-smart-future", StationID: station.ID, MountID: "m1", StartsAt: time.Now().UTC().Add(72 * time.Hour), EndsAt: time.Now().UTC().Add(73 * time.Hour), SourceType: "smart_block", SourceID: "sb-empty"},
	}
	for _, entry := range entries {
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create entry: %v", err)
		}
	}

	t.Run("playlist details return track listing", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-playlist", nil, &station, "entry-playlist")
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		playlist := payload["playlist"].(map[string]any)
		if playlist["name"] != "Playlist One" || int(playlist["track_count"].(float64)) != 1 {
			t.Fatalf("unexpected playlist payload: %+v", playlist)
		}
	})

	t.Run("future smart block details stay pending before lookahead window", func(t *testing.T) {
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-smart-future", nil, &station, "entry-smart-future")
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		preview := payload["smart_block_preview"].(map[string]any)
		if preview["status"] != "pending_materialization" {
			t.Fatalf("unexpected smart block preview payload: %+v", preview)
		}
	})

	t.Run("smart block inside lookahead returns generation error when unresolved", func(t *testing.T) {
		entry := models.ScheduleEntry{ID: "entry-smart-now", StationID: station.ID, MountID: "m1", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(2 * time.Hour), SourceType: "smart_block", SourceID: "sb-empty"}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create smart block now entry: %v", err)
		}
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-smart-now", nil, &station, "entry-smart-now")
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		preview := payload["smart_block_preview"].(map[string]any)
		if preview["status"] != "error" {
			t.Fatalf("expected smart block error preview, got %+v", preview)
		}
	})

	t.Run("smart block inside lookahead returns resolved preview tracks", func(t *testing.T) {
		start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
		entry := models.ScheduleEntry{ID: "entry-smart-ready", StationID: station.ID, MountID: "m1", StartsAt: start, EndsAt: start.Add(30 * time.Minute), SourceType: "smart_block", SourceID: "sb-ready"}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create ready smart block entry: %v", err)
		}
		req := scheduleRequest(http.MethodGet, "/dashboard/schedule/entries/entry-smart-ready", nil, &station, "entry-smart-ready")
		rr := httptest.NewRecorder()
		h.ScheduleEntryDetails(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		preview := payload["smart_block_preview"].(map[string]any)
		if preview["status"] != "ready" {
			t.Fatalf("expected ready smart block preview, got %+v", preview)
		}
		if int(preview["track_count"].(float64)) == 0 || int(preview["total_duration_s"].(float64)) == 0 {
			t.Fatalf("expected generated tracks in smart block preview, got %+v", preview)
		}
		tracks := preview["tracks"].([]any)
		if len(tracks) == 0 {
			t.Fatalf("expected preview tracks, got %+v", preview)
		}
		firstTrack := tracks[0].(map[string]any)
		if firstTrack["title"] == "" || firstTrack["artist"] != "Artist Ready" {
			t.Fatalf("unexpected preview track payload: %+v", firstTrack)
		}
	})
}
