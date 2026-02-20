package playout

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestComputePlaybackResume_UsesPersistedStartAndClampsToDuration(t *testing.T) {
	d := &Director{}
	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		StartsAt: now.Add(-10 * time.Minute),
	}
	media := models.MediaItem{
		Duration: 3 * time.Minute,
	}
	ctx := d.computePlaybackResume(entry, media, map[string]any{
		"resume_started_at": now.Add(-5 * time.Minute),
	})

	// Clamped to duration-1s.
	want := media.Duration - time.Second
	if ctx.Offset != want {
		t.Fatalf("offset mismatch: got %v want %v", ctx.Offset, want)
	}
}

func TestComputePlaybackResume_IgnoresTinyOffsets(t *testing.T) {
	d := &Director{}
	now := time.Now().UTC()
	entry := models.ScheduleEntry{
		StartsAt: now.Add(-1 * time.Second),
	}
	media := models.MediaItem{
		Duration: 10 * time.Minute,
	}
	ctx := d.computePlaybackResume(entry, media, nil)
	if ctx.Offset != 0 {
		t.Fatalf("offset mismatch: got %v want 0", ctx.Offset)
	}
}
