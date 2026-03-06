package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTrackOverridesTestDirector(t *testing.T) *Director {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaItem{}); err != nil {
		t.Fatalf("auto migrate media_items: %v", err)
	}

	return &Director{db: db}
}

func seedTestMedia(t *testing.T, d *Director, stationID string, ids ...string) {
	t.Helper()

	for _, id := range ids {
		item := models.MediaItem{
			ID:        id,
			StationID: stationID,
			Title:     id,
			Duration:  30 * time.Second,
			Path:      "/tmp/" + id + ".mp3",
		}
		if err := d.db.Create(&item).Error; err != nil {
			t.Fatalf("seed media %s: %v", id, err)
		}
	}
}

func TestApplyTrackOverrides_ReplacesAndRemoves(t *testing.T) {
	d := newTrackOverridesTestDirector(t)
	seedTestMedia(t, d, "station-1", "m1", "m2", "m3")

	entry := models.ScheduleEntry{
		StationID: "station-1",
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"1": "m3",
			},
		},
	}

	got := d.applyTrackOverrides(context.Background(), entry, []string{"m1", "m2"})
	want := []string{"m1", "m3"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("replace: got %v, want %v", got, want)
	}

	entry.Metadata["track_overrides"] = map[string]string{
		"0": "__remove__",
	}
	got = d.applyTrackOverrides(context.Background(), entry, []string{"m1", "m2"})
	want = []string{"m2"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("remove: got %v, want %v", got, want)
	}
}

func TestApplyTrackOverrides_RejectsCrossStationReplacement(t *testing.T) {
	d := newTrackOverridesTestDirector(t)
	seedTestMedia(t, d, "station-1", "m1", "m2")
	seedTestMedia(t, d, "station-2", "other-station-media")

	entry := models.ScheduleEntry{
		StationID: "station-1",
		Metadata: map[string]any{
			"track_overrides": map[string]any{
				"0": "other-station-media",
				"1": "missing-media",
			},
		},
	}

	got := d.applyTrackOverrides(context.Background(), entry, []string{"m1", "m2"})
	want := []string{"m1", "m2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("cross-station fallback: got %v, want %v", got, want)
	}
}
