package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
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
