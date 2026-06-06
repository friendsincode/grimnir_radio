package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDirector_PersistAndLoadMountState(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.MountPlayoutState{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	state := playoutState{
		MediaID:    "media-1",
		EntryID:    "entry-1",
		StationID:  "station-1",
		Started:    now.Add(-30 * time.Second),
		Ends:       now.Add(10 * time.Minute),
		SourceType: "smart_block",
		SourceID:   "sb-1",
		Position:   2,
		TotalItems: 5,
		Items:      []string{"m1", "m2", "m3", "m4", "m5"},
	}

	d := &Director{
		db:     db,
		active: make(map[string]playoutState),
		logger: zerolog.Nop(),
	}
	d.persistMountState(context.Background(), "mount-1", state)

	d2 := &Director{
		db:     db,
		active: make(map[string]playoutState),
		logger: zerolog.Nop(),
	}
	d2.loadPersistedMountStates(context.Background())

	got, ok := d2.active["mount-1"]
	if !ok {
		t.Fatalf("expected mount state to be loaded")
	}
	if got.EntryID != state.EntryID || got.StationID != state.StationID || got.MediaID != state.MediaID {
		t.Fatalf("loaded state mismatch: got entry=%q station=%q media=%q", got.EntryID, got.StationID, got.MediaID)
	}
	if got.SourceType != state.SourceType || got.SourceID != state.SourceID {
		t.Fatalf("loaded source mismatch: got type=%q id=%q", got.SourceType, got.SourceID)
	}
	if got.Position != state.Position || got.TotalItems != state.TotalItems {
		t.Fatalf("loaded position mismatch: got pos=%d total=%d", got.Position, got.TotalItems)
	}
	if len(got.Items) != len(state.Items) {
		t.Fatalf("loaded items length mismatch: got=%d want=%d", len(got.Items), len(state.Items))
	}
	for i := range got.Items {
		if got.Items[i] != state.Items[i] {
			t.Fatalf("loaded items mismatch at %d: got=%q want=%q", i, got.Items[i], state.Items[i])
		}
	}

	d2.clearPersistedMountState(context.Background(), "mount-1")
	var count int64
	if err := db.Model(&models.MountPlayoutState{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected state to be cleared, count=%d", count)
	}
}

// TestSmartBlockGeneration_PersistsAcrossDirectors guards #238 C9: the
// sbGeneration counter is persisted on smart_block_generations and is
// re-read after a fresh Director is constructed against the same DB. Two
// HA control planes serving the same schedule rely on this so the shuffle
// seed stays in sync even if one of them restarts mid-occurrence.
func TestSmartBlockGeneration_PersistsAcrossDirectors(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.SmartBlockGeneration{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	entryID := uuid.NewString()
	startsAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	playKey := entryID + "@" + startsAt.Format(time.RFC3339Nano)
	ctx := context.Background()

	d1 := &Director{
		db:           db,
		logger:       zerolog.Nop(),
		sbGeneration: make(map[string]int),
	}

	// Fresh: nothing in DB, nothing in memory → 0.
	if got := d1.loadSmartBlockGeneration(ctx, entryID, startsAt, playKey); got != 0 {
		t.Fatalf("expected initial generation 0, got %d", got)
	}

	// Bump twice (simulating two smart-block exhaustions in handleTrackEnded).
	d1.bumpSmartBlockGeneration(ctx, entryID, startsAt, playKey)
	d1.bumpSmartBlockGeneration(ctx, entryID, startsAt, playKey)

	if got := d1.loadSmartBlockGeneration(ctx, entryID, startsAt, playKey); got != 2 {
		t.Fatalf("after two bumps in-process, expected 2, got %d", got)
	}

	// New director (simulating a process restart, or instance B coming up
	// while instance A has been running): map is empty, must read from DB.
	d2 := &Director{
		db:           db,
		logger:       zerolog.Nop(),
		sbGeneration: make(map[string]int),
	}
	if got := d2.loadSmartBlockGeneration(ctx, entryID, startsAt, playKey); got != 2 {
		t.Fatalf("after restart, expected DB-persisted generation 2, got %d", got)
	}

	// In-process cache should also be populated now so a second read avoids DB.
	d2.mu.Lock()
	cached, ok := d2.sbGeneration[playKey]
	d2.mu.Unlock()
	if !ok || cached != 2 {
		t.Fatalf("expected in-memory cache to be primed (ok=%v val=%d) after first load", ok, cached)
	}

	// One more bump on the fresh director — should land on 3 (it loaded 2,
	// in-memory increments to 3, persists 3).
	d2.bumpSmartBlockGeneration(ctx, entryID, startsAt, playKey)

	d3 := &Director{
		db:           db,
		logger:       zerolog.Nop(),
		sbGeneration: make(map[string]int),
	}
	if got := d3.loadSmartBlockGeneration(ctx, entryID, startsAt, playKey); got != 3 {
		t.Fatalf("after second restart, expected DB-persisted generation 3, got %d", got)
	}

	// Distinct occurrence (same entry, different start) must not collide.
	otherStart := startsAt.Add(24 * time.Hour)
	otherKey := entryID + "@" + otherStart.Format(time.RFC3339Nano)
	if got := d3.loadSmartBlockGeneration(ctx, entryID, otherStart, otherKey); got != 0 {
		t.Fatalf("distinct occurrence should start at 0, got %d", got)
	}

	// And the row count matches what we expect: 2 distinct (entry, occurrence) pairs.
	var count int64
	if err := db.Model(&models.SmartBlockGeneration{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	// The 'otherStart' read doesn't insert; only bumps insert. So we expect 1.
	if count != 1 {
		t.Fatalf("expected 1 persisted row, got %d", count)
	}
}

func TestRun_GracefulShutdown_FlushesPositions(t *testing.T) {
	d, _ := newMockDirector(t)

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	mediaID := uuid.NewString()
	entryID := uuid.NewString()

	startedAt := time.Now().UTC().Add(-30 * time.Second)
	if err := d.db.Create(&models.MountPlayoutState{
		MountID:   mountID,
		StationID: stationID,
		EntryID:   entryID,
		MediaID:   mediaID,
		StartedAt: startedAt,
		EndsAt:    time.Now().UTC().Add(5 * time.Minute),
		UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed MountPlayoutState: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	var state models.MountPlayoutState
	if err := d.db.First(&state, "mount_id = ?", mountID).Error; err != nil {
		t.Fatalf("load MountPlayoutState: %v", err)
	}
	if state.TrackPositionMS <= 0 {
		t.Errorf("expected TrackPositionMS > 0 after shutdown flush, got %d", state.TrackPositionMS)
	}
}
