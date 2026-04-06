/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	ws "github.com/friendsincode/grimnir_radio/internal/webstream"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newMockDirector creates a Director with a mock manager and a full set of
// dependencies required for methods that call bus.Publish, broadcast, etc.
func newMockDirector(t *testing.T, tables ...any) (*Director, *mockManager) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	all := []any{
		&models.PlayHistory{},
		&models.MountPlayoutState{},
		&models.MediaItem{},
		&models.Mount{},
		&models.Station{},
		&models.PlayoutQueueItem{},
		&models.Webstream{},
	}
	all = append(all, tables...)
	if err := db.AutoMigrate(all...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	bus := events.NewBus()
	broadcastSrv := broadcast.NewServer(zerolog.Nop(), bus)
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}

	mgr := newMockManager()
	d := &Director{
		db:            db,
		cfg:           cfg,
		manager:       mgr,
		bus:           bus,
		broadcast:     broadcastSrv,
		smartblockEng: smartblock.New(db, zerolog.Nop()),
		active:        make(map[string]playoutState),
		played:        make(map[string]time.Time),
		sbGeneration:  make(map[string]int),
		policyCache:   make(map[string]cachedScheduleBoundaryPolicy),
		webrtcCache:   make(map[string]cachedWebRTCPort),
		xfadeSessions: make(map[string]*pcmCrossfadeSession),
		xfadeCfgCache: make(map[string]cachedCrossfadeConfig),
		tzCache:       make(map[string]cachedStationTimezone),
		scheduleCache: cachedScheduleSnapshot{dirty: true},
		webstreamSvc:  ws.NewService(db, bus, zerolog.Nop()),
		icyPollers:    make(map[string]ws.MetadataPoller),
		logger:        zerolog.Nop(),
	}
	return d, mgr
}

// ── NewDirector ───────────────────────────────────────────────────────────

func TestNewDirector_Constructs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	bus := events.NewBus()
	broadcastSrv := broadcast.NewServer(zerolog.Nop(), bus)
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}
	mgr := newMockManager()

	d := NewDirector(db, cfg, mgr, bus, nil, broadcastSrv, zerolog.Nop())
	if d == nil {
		t.Fatal("NewDirector returned nil")
	}
	if d.manager != mgr {
		t.Error("manager not set correctly")
	}
}

// ── Broadcast ─────────────────────────────────────────────────────────────

func TestBroadcast_ReturnsServer(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	bus := events.NewBus()
	broadcastSrv := broadcast.NewServer(zerolog.Nop(), bus)
	cfg := &config.Config{}
	mgr := newMockManager()

	d := NewDirector(db, cfg, mgr, bus, nil, broadcastSrv, zerolog.Nop())
	if got := d.Broadcast(); got != broadcastSrv {
		t.Errorf("Broadcast() = %v, want %v", got, broadcastSrv)
	}
}

// ── WithStateResetter ─────────────────────────────────────────────────────

type fakeStateResetter struct{ called bool }

func (f *fakeStateResetter) SetState(_ context.Context, _ string, _ models.ExecutorStateEnum) error {
	f.called = true
	return nil
}

func TestWithStateResetter_SetsResetter(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	bus := events.NewBus()
	broadcastSrv := broadcast.NewServer(zerolog.Nop(), bus)
	cfg := &config.Config{}
	mgr := newMockManager()
	sr := &fakeStateResetter{}

	d := NewDirector(db, cfg, mgr, bus, nil, broadcastSrv, zerolog.Nop(), WithStateResetter(sr))
	if d.stateResetter != sr {
		t.Error("stateResetter not set by WithStateResetter")
	}
}

// ── markScheduleDirty ─────────────────────────────────────────────────────

func TestMarkScheduleDirty_SetsDirtyFlag(t *testing.T) {
	d, _ := newMockDirector(t)
	d.scheduleMu.Lock()
	d.scheduleCache.dirty = false
	d.scheduleMu.Unlock()

	d.markScheduleDirty()

	d.scheduleMu.Lock()
	dirty := d.scheduleCache.dirty
	d.scheduleMu.Unlock()
	if !dirty {
		t.Error("markScheduleDirty did not set dirty flag")
	}
}

// ── getScheduleSnapshot ───────────────────────────────────────────────────

func TestGetScheduleSnapshot_EmptyDB(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()
	now := time.Now().UTC()

	entries, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// ── getScheduleBoundaryPolicy ─────────────────────────────────────────────

func TestGetScheduleBoundaryPolicy_EmptyDB_ReturnsHard(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	policy := d.getScheduleBoundaryPolicy(ctx, uuid.NewString())
	if policy.Mode != "hard" {
		t.Errorf("policy.Mode = %q, want \"hard\" for unknown station", policy.Mode)
	}
}

func TestGetScheduleBoundaryPolicy_Caches(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	p1 := d.getScheduleBoundaryPolicy(ctx, stationID)
	p2 := d.getScheduleBoundaryPolicy(ctx, stationID)
	if p1.Mode != p2.Mode {
		t.Error("policy not cached correctly")
	}
}

// ── getWebRTCRTPPortForStation ────────────────────────────────────────────

func TestGetWebRTCRTPPortForStation_Disabled_ReturnsZero(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = false
	ctx := context.Background()

	port := d.getWebRTCRTPPortForStation(ctx, uuid.NewString())
	if port != 0 {
		t.Errorf("expected 0 when webrtc disabled, got %d", port)
	}
}

func TestGetWebRTCRTPPortForStation_EnabledNoStation_ReturnsFallback(t *testing.T) {
	d, _ := newMockDirector(t)
	d.webrtcEnabled = true
	d.webrtcRTPPort = 5004
	ctx := context.Background()

	port := d.getWebRTCRTPPortForStation(ctx, uuid.NewString())
	if port != 5004 {
		t.Errorf("expected fallback port 5004, got %d", port)
	}
}

// ── getCrossfadeConfig ────────────────────────────────────────────────────

func TestGetCrossfadeConfig_EmptyDB_ReturnsDisabled(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	cfg := d.getCrossfadeConfig(ctx, uuid.NewString())
	if cfg.Enabled {
		t.Error("expected crossfade disabled for unknown station")
	}
}

// ── getStationTimezone ────────────────────────────────────────────────────

func TestGetStationTimezone_EmptyDB_ReturnsUTC(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	loc := d.getStationTimezone(ctx, uuid.NewString())
	if loc != time.UTC {
		t.Errorf("expected UTC for unknown station, got %v", loc)
	}
}

// ── handleEntry ───────────────────────────────────────────────────────────

func TestHandleEntry_MediaNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "media",
		SourceID:   uuid.NewString(), // does not exist in DB
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.handleEntry(ctx, entry)
	if err == nil {
		t.Error("expected error when media item not found, got nil")
	}
}

func TestHandleEntry_LiveEntry_NoError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "live",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.handleEntry(ctx, entry)
	if err != nil {
		t.Errorf("handleEntry for live entry returned error: %v", err)
	}
}

func TestHandleEntry_UnknownSourceType_NoError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "unknown_type",
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.handleEntry(ctx, entry)
	if err != nil {
		t.Errorf("handleEntry for unknown source type returned error: %v", err)
	}
}

// ── loadPersistedMountStates ──────────────────────────────────────────────

func TestLoadPersistedMountStates_EmptyDB(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	d.loadPersistedMountStates(ctx)

	d.mu.Lock()
	n := len(d.active)
	d.mu.Unlock()
	if n != 0 {
		t.Errorf("expected 0 active mounts after loading empty DB, got %d", n)
	}
}

func TestLoadPersistedMountStates_LoadsFreshRows(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	mountID := uuid.NewString()
	stationID := uuid.NewString()
	row := models.MountPlayoutState{
		MountID:   mountID,
		StationID: stationID,
		EntryID:   uuid.NewString(),
		MediaID:   uuid.NewString(),
		StartedAt: time.Now().UTC().Add(-1 * time.Minute),
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
		UpdatedAt: time.Now().UTC(),
	}
	if err := d.db.Create(&row).Error; err != nil {
		t.Fatalf("seed MountPlayoutState: %v", err)
	}

	d.loadPersistedMountStates(ctx)

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active mount state to be loaded")
	}
	if state.StationID != stationID {
		t.Errorf("stationID = %q, want %q", state.StationID, stationID)
	}
}

// ── startMediaEntry ───────────────────────────────────────────────────────

func TestStartMediaEntry_MediaNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "media",
		SourceID:   uuid.NewString(),
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startMediaEntry(ctx, entry)
	if err == nil {
		t.Error("expected error when media item not in DB")
	}
}

func TestStartMediaEntry_WithMediaAndMount_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "testmount",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	mediaID := uuid.NewString()
	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Test Track",
		Artist:        "Test Artist",
		Path:          "/tmp/test.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "media",
		SourceID:   mediaID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(3 * time.Minute),
	}

	err := d.startMediaEntry(ctx, entry)
	if err != nil {
		t.Errorf("startMediaEntry returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state for mount after startMediaEntry")
	}
	if state.MediaID != mediaID {
		t.Errorf("MediaID = %q, want %q", state.MediaID, mediaID)
	}
}

// ── startPlaylistEntry ────────────────────────────────────────────────────

func TestStartPlaylistEntry_PlaylistNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startPlaylistEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing playlist")
	}
}

func TestStartPlaylistEntry_EmptyPlaylist_NoError(t *testing.T) {
	d, _ := newMockDirector(t, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	playlistID := uuid.NewString()

	pl := models.Playlist{
		ID:        playlistID,
		StationID: stationID,
		Name:      "Empty Playlist",
	}
	if err := d.db.Create(&pl).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "playlist",
		SourceID:   playlistID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startPlaylistEntry(ctx, entry)
	if err != nil {
		t.Errorf("startPlaylistEntry with empty playlist returned error: %v", err)
	}
}

// ── startSmartBlockEntry ──────────────────────────────────────────────────

func TestStartSmartBlockEntry_BlockNotFound_ReturnsError(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{})
	ctx := context.Background()

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "smart_block",
		SourceID:   uuid.NewString(),
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startSmartBlockEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing smart block")
	}
}

// ── StopStation ───────────────────────────────────────────────────────────

func TestStopStation_NoMounts_ReturnsZero(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stopped, err := d.StopStation(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("StopStation returned error: %v", err)
	}
	if stopped != 0 {
		t.Errorf("stopped = %d, want 0 for station with no mounts", stopped)
	}
}

func TestStopStation_WithActiveMounts_StopsAndClears(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "main",
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(),
		StationID: stationID,
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	stopped, err := d.StopStation(ctx, stationID)
	if err != nil {
		t.Fatalf("StopStation returned error: %v", err)
	}
	if stopped != 1 {
		t.Errorf("stopped = %d, want 1", stopped)
	}

	d.mu.Lock()
	_, still := d.active[mountID]
	d.mu.Unlock()
	if still {
		t.Error("mount should be removed from active after StopStation")
	}
}

// ── SkipStation ───────────────────────────────────────────────────────────

func TestSkipStation_NoMounts_ReturnsZero(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	skipped, err := d.SkipStation(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("SkipStation returned error: %v", err)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

func TestSkipStation_WithActiveMount_Skips(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "main",
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(),
		StationID: stationID,
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	skipped, err := d.SkipStation(ctx, stationID)
	if err != nil {
		t.Fatalf("SkipStation returned error: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

// ── ReloadStation ─────────────────────────────────────────────────────────

func TestReloadStation_NoMounts_ReturnsZero(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	reloaded, err := d.ReloadStation(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("ReloadStation returned error: %v", err)
	}
	if reloaded != 0 {
		t.Errorf("reloaded = %d, want 0", reloaded)
	}
}

func TestReloadStation_WithActiveMount_ClearsActive(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	mount := models.Mount{
		ID:        mountID,
		StationID: stationID,
		Name:      "main",
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(),
		StationID: stationID,
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	reloaded, err := d.ReloadStation(ctx, stationID)
	if err != nil {
		t.Fatalf("ReloadStation returned error: %v", err)
	}
	if reloaded != 1 {
		t.Errorf("reloaded = %d, want 1", reloaded)
	}

	d.mu.Lock()
	_, still := d.active[mountID]
	d.mu.Unlock()
	if still {
		t.Error("active state should be cleared after ReloadStation")
	}
}

// ── publishNowPlaying ─────────────────────────────────────────────────────

func TestPublishNowPlaying_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "live",
		StartsAt:   time.Now().UTC(),
		EndsAt:     time.Now().UTC().Add(time.Hour),
	}

	d.publishNowPlaying(entry, map[string]any{"key": "value"})
}

// ── emitHealthSnapshot ────────────────────────────────────────────────────

func TestEmitHealthSnapshot_WithActiveMount_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)

	d.mu.Lock()
	d.active["mount-1"] = playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(),
		StationID: uuid.NewString(),
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	d.emitHealthSnapshot()
}

func TestEmitHealthSnapshot_Empty_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)
	d.emitHealthSnapshot()
}

// ── tick ──────────────────────────────────────────────────────────────────

func TestTick_EmptySchedule_DoesNotError(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	if err := d.tick(ctx); err != nil {
		t.Errorf("tick with empty schedule returned error: %v", err)
	}
}

// ── Run ───────────────────────────────────────────────────────────────────

func TestRun_CancelledContext_ReturnsContextError(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := d.Run(ctx)
	if err == nil {
		t.Error("Run with cancelled context should return an error")
	}
}

// ── startPlaylistEntry with items (covers playMediaWithState) ─────────────

func TestStartPlaylistEntry_WithItems_SetsActiveState(t *testing.T) {
	d, _ := newMockDirector(t, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	playlistID := uuid.NewString()
	mediaID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "testmount",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	media := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         "Playlist Track",
		Artist:        "Playlist Artist",
		Path:          "/tmp/playlist.mp3",
		Duration:      3 * time.Minute,
		AnalysisState: models.AnalysisComplete,
	}
	if err := d.db.Create(&media).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	pl := models.Playlist{
		ID:        playlistID,
		StationID: stationID,
		Name:      "Test Playlist",
	}
	if err := d.db.Create(&pl).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	item := models.PlaylistItem{
		ID:         uuid.NewString(),
		PlaylistID: playlistID,
		MediaID:    mediaID,
		Position:   0,
	}
	if err := d.db.Create(&item).Error; err != nil {
		t.Fatalf("seed playlist item: %v", err)
	}

	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "playlist",
		SourceID:   playlistID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	err := d.startPlaylistEntry(ctx, entry)
	if err != nil {
		t.Errorf("startPlaylistEntry with items returned error: %v", err)
	}

	d.mu.Lock()
	state, ok := d.active[mountID]
	d.mu.Unlock()
	if !ok {
		t.Fatal("expected active state after startPlaylistEntry with items")
	}
	if state.SourceType != "playlist" {
		t.Errorf("SourceType = %q, want \"playlist\"", state.SourceType)
	}
}

// ── startSmartBlockEntry with a block and media ────────────────────────────

func TestStartSmartBlockEntry_WithBlock_NoMediaFallback(t *testing.T) {
	d, _ := newMockDirector(t, &models.SmartBlock{}, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	blockID := uuid.NewString()

	mount := models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "testmount",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	block := models.SmartBlock{
		ID:        blockID,
		StationID: stationID,
		Name:      "Test Block",
	}
	if err := d.db.Create(&block).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	// No media exists → engine will fail → fallback also fails → publishNowPlaying with error
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "smart_block",
		SourceID:   blockID,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}

	// Should not return an error even when no media found — it logs and publishes error state
	err := d.startSmartBlockEntry(ctx, entry)
	if err != nil {
		t.Errorf("startSmartBlockEntry with no media should not error, got: %v", err)
	}
}

// ── getScheduleSnapshot cache behavior ───────────────────────────────────

func TestGetScheduleSnapshot_ReturnsCachedEntries(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()
	now := time.Now().UTC()

	// First call populates cache.
	entries1, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Mark clean so next call uses cache.
	d.scheduleMu.Lock()
	d.scheduleCache.dirty = false
	d.scheduleMu.Unlock()

	entries2, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if len(entries1) != len(entries2) {
		t.Errorf("cached entries count %d != fresh count %d", len(entries2), len(entries1))
	}
}

// ── StopStation with stateResetter ────────────────────────────────────────

func TestStopStation_CallsStateResetter(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()
	sr := &fakeStateResetter{}
	d.stateResetter = sr

	stationID := uuid.NewString()
	_, err := d.StopStation(ctx, stationID)
	if err != nil {
		t.Fatalf("StopStation returned error: %v", err)
	}
	if !sr.called {
		t.Error("stateResetter.SetState should have been called by StopStation")
	}
}

// ── ReloadStation with stateResetter ──────────────────────────────────────

func TestReloadStation_CallsStateResetter(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()
	sr := &fakeStateResetter{}
	d.stateResetter = sr

	stationID := uuid.NewString()
	_, err := d.ReloadStation(ctx, stationID)
	if err != nil {
		t.Fatalf("ReloadStation returned error: %v", err)
	}
	if !sr.called {
		t.Error("stateResetter.SetState should have been called by ReloadStation")
	}
}

// ── getStationTimezone with cached value ─────────────────────────────────

func TestGetStationTimezone_CachedValue(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	loc1 := d.getStationTimezone(ctx, stationID)
	loc2 := d.getStationTimezone(ctx, stationID)
	if loc1 != loc2 {
		t.Error("timezone should be cached between calls")
	}
}

// ── getCrossfadeConfig with cached value ──────────────────────────────────

func TestGetCrossfadeConfig_CachedValue(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()
	stationID := uuid.NewString()

	c1 := d.getCrossfadeConfig(ctx, stationID)
	c2 := d.getCrossfadeConfig(ctx, stationID)
	if c1.Enabled != c2.Enabled || c1.Duration != c2.Duration {
		t.Error("crossfade config should be cached")
	}
}

// ── handleEntry playlist source type ─────────────────────────────────────

func TestHandleEntry_PlaylistSourceType_CallsStartPlaylist(t *testing.T) {
	d, _ := newMockDirector(t, &models.Playlist{}, &models.PlaylistItem{})
	ctx := context.Background()

	// playlist not in DB → startPlaylistEntry returns an error, handleEntry returns it
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  uuid.NewString(),
		MountID:    uuid.NewString(),
		SourceType: "playlist",
		SourceID:   uuid.NewString(),
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}
	err := d.handleEntry(ctx, entry)
	if err == nil {
		t.Error("expected error for missing playlist in handleEntry")
	}
}

// ── tick with an active schedule entry ───────────────────────────────────

func TestTick_WithLiveScheduleEntry_CallsHandleEntry(t *testing.T) {
	d, _ := newMockDirector(t, &models.ScheduleEntry{})
	ctx := context.Background()

	stationID := uuid.NewString()
	mountID := uuid.NewString()

	// Insert a "live" schedule entry that is currently active.
	entry := models.ScheduleEntry{
		ID:         uuid.NewString(),
		StationID:  stationID,
		MountID:    mountID,
		SourceType: "live",
		IsInstance: true,
		StartsAt:   time.Now().UTC().Add(-1 * time.Second),
		EndsAt:     time.Now().UTC().Add(5 * time.Minute),
	}
	if err := d.db.Create(&entry).Error; err != nil {
		t.Fatalf("seed schedule entry: %v", err)
	}

	// Force cache refresh.
	d.markScheduleDirty()

	if err := d.tick(ctx); err != nil {
		t.Errorf("tick returned error: %v", err)
	}
}

// ── SkipStation with stateResetter via xfade session none ────────────────

func TestSkipStation_NoStateResetter_DoesNotPanic(t *testing.T) {
	d, _ := newMockDirector(t)
	ctx := context.Background()
	d.stateResetter = nil // explicitly nil

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mount := models.Mount{ID: mountID, StationID: stationID, Name: "main"}
	if err := d.db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	d.mu.Lock()
	d.active[mountID] = playoutState{
		MediaID:   uuid.NewString(),
		EntryID:   uuid.NewString(),
		StationID: stationID,
		Started:   time.Now().UTC(),
		Ends:      time.Now().UTC().Add(5 * time.Minute),
	}
	d.mu.Unlock()

	// Must not panic.
	_, err := d.SkipStation(ctx, stationID)
	if err != nil {
		t.Errorf("SkipStation returned error: %v", err)
	}
}
