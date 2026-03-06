package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newQueueTestDirector(t *testing.T) *Director {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaItem{}, &models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("auto migrate queue test tables: %v", err)
	}

	return &Director{db: db}
}

func TestPopNextQueuedMedia_RemovesHeadAndReorders(t *testing.T) {
	d := newQueueTestDirector(t)

	for _, item := range []models.MediaItem{
		{ID: "track-1", StationID: "station-1", Title: "Track 1", Duration: 30 * time.Second, Path: "/tmp/track-1.mp3"},
		{ID: "track-2", StationID: "station-1", Title: "Track 2", Duration: 30 * time.Second, Path: "/tmp/track-2.mp3"},
	} {
		if err := d.db.Create(&item).Error; err != nil {
			t.Fatalf("seed media %s: %v", item.ID, err)
		}
	}

	for _, item := range []models.PlayoutQueueItem{
		{ID: "q1", StationID: "station-1", MountID: "mount-1", MediaID: "track-1", Position: 1},
		{ID: "q2", StationID: "station-1", MountID: "mount-1", MediaID: "track-2", Position: 2},
	} {
		if err := d.db.Create(&item).Error; err != nil {
			t.Fatalf("seed queue %s: %v", item.ID, err)
		}
	}

	media, queueItem, err := d.popNextQueuedMedia(context.Background(), "station-1", "mount-1")
	if err != nil {
		t.Fatalf("popNextQueuedMedia: %v", err)
	}
	if media == nil || queueItem == nil {
		t.Fatal("expected queued media and queue item")
	}
	if media.ID != "track-1" || queueItem.ID != "q1" {
		t.Fatalf("unexpected pop result: media=%+v queue=%+v", media, queueItem)
	}

	var remaining []models.PlayoutQueueItem
	if err := d.db.Order("position ASC").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining queue: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining queue item, got %d", len(remaining))
	}
	if remaining[0].ID != "q2" || remaining[0].Position != 1 {
		t.Fatalf("unexpected remaining queue state: %+v", remaining[0])
	}
}

func TestPopNextQueuedMedia_SkipsMissingMediaAndReturnsNextValid(t *testing.T) {
	d := newQueueTestDirector(t)

	validMedia := models.MediaItem{
		ID:        "track-valid",
		StationID: "station-1",
		Title:     "Valid Track",
		Duration:  30 * time.Second,
		Path:      "/tmp/track-valid.mp3",
	}
	if err := d.db.Create(&validMedia).Error; err != nil {
		t.Fatalf("seed valid media: %v", err)
	}

	for _, item := range []models.PlayoutQueueItem{
		{ID: "q-missing", StationID: "station-1", MountID: "mount-1", MediaID: "missing-track", Position: 1},
		{ID: "q-valid", StationID: "station-1", MountID: "mount-1", MediaID: validMedia.ID, Position: 2},
	} {
		if err := d.db.Create(&item).Error; err != nil {
			t.Fatalf("seed queue %s: %v", item.ID, err)
		}
	}

	media, queueItem, err := d.popNextQueuedMedia(context.Background(), "station-1", "mount-1")
	if err != nil {
		t.Fatalf("popNextQueuedMedia: %v", err)
	}
	if media == nil || queueItem == nil {
		t.Fatal("expected queued media and queue item after skipping missing head")
	}
	if media.ID != validMedia.ID || queueItem.ID != "q-valid" {
		t.Fatalf("unexpected pop result after missing head: media=%+v queue=%+v", media, queueItem)
	}

	var remaining int64
	if err := d.db.Model(&models.PlayoutQueueItem{}).Count(&remaining).Error; err != nil {
		t.Fatalf("count remaining queue items: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected queue to be empty after consuming valid item, got %d", remaining)
	}
}
