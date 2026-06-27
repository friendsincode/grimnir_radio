/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDirector_ListenerBreakdown checks the per-transport / per-channel listener
// accounting added for issue #18: WebRTC is counted under its own channel, ICY
// mounts keep transport=icy_http, and the parts reconcile to the platform total.
func TestDirector_ListenerBreakdown(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Mount{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// Station s1 has an ICY mount; WebRTC peers attach to its base channel.
	if err := db.Create(&models.Mount{StationID: "s1", Name: "show-a"}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	bsrv := broadcast.NewServer(zerolog.Nop(), events.NewBus())
	bsrv.CreateMount("show-a", "audio/mpeg", 128) // HQ; 0 clients in this test

	d := &Director{db: db, broadcast: bsrv}

	// s1 has 3 WebRTC peers; s2 has 2 but no DB mount (fallback naming).
	bd, err := d.ListenerBreakdown(context.Background(), map[string]int{"s1": 3, "s2": 2})
	if err != nil {
		t.Fatalf("ListenerBreakdown: %v", err)
	}

	byChannel := make(map[string]ListenerStat, len(bd))
	platform := 0
	byTransport := map[string]int{}
	for _, s := range bd {
		byChannel[s.Channel] = s
		platform += s.Count
		byTransport[s.Transport] += s.Count
	}

	if s, ok := byChannel["show-a"]; !ok || s.Transport != TransportICYHTTP || s.StationID != "s1" {
		t.Errorf("icy_http channel show-a for s1 missing/wrong: %+v (ok=%v)", s, ok)
	}
	if s, ok := byChannel["show-a-webrtc"]; !ok || s.Transport != TransportWebRTC || s.Count != 3 || s.StationID != "s1" {
		t.Errorf("webrtc channel show-a-webrtc count 3 missing/wrong: %+v (ok=%v)", s, ok)
	}
	if s, ok := byChannel["s2-webrtc"]; !ok || s.Count != 2 {
		t.Errorf("fallback channel s2-webrtc count 2 missing/wrong: %+v (ok=%v)", s, ok)
	}

	// Parts reconcile to the whole: platform == webrtc(3+2) + icy(0).
	if platform != 5 {
		t.Errorf("platform total = %d, want 5", platform)
	}
	if byTransport[TransportWebRTC] != 5 || byTransport[TransportICYHTTP] != 0 {
		t.Errorf("by_transport = %v, want webrtc=5 icy_http=0", byTransport)
	}
}
