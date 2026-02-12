package playout

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestEffectiveCrossfade_DefaultsAndOverrides(t *testing.T) {
	station := crossfadeConfig{Enabled: true, Duration: 3 * time.Second}
	entry := models.ScheduleEntry{Metadata: map[string]any{}}

	got := effectiveCrossfade(entry, station)
	if !got.Enabled || got.Duration != 3*time.Second {
		t.Fatalf("expected station cfg, got %+v", got)
	}

	entry.Metadata["crossfade"] = map[string]any{
		"override":    true,
		"enabled":     "off",
		"duration_ms": float64(5000),
	}
	got = effectiveCrossfade(entry, station)
	if got.Enabled {
		t.Fatalf("expected off, got %+v", got)
	}
	if got.Duration != 5*time.Second {
		t.Fatalf("expected 5s, got %s", got.Duration)
	}
}

