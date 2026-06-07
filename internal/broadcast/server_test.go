/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package broadcast

import (
	"bytes"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

func TestMount_BytesReceivedAt_ZeroBeforeFeed(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)
	got := m.BytesReceivedAt()
	if !got.IsZero() {
		t.Errorf("BytesReceivedAt() = %v, want zero time before any feed", got)
	}
}

func TestMount_BytesReceivedAt_UpdatedAfterFeed(t *testing.T) {
	bus := events.NewBus()
	m := NewMount("test", "audio/mpeg", 128, zerolog.Nop(), bus)

	before := time.Now()
	// FeedFrom reads until EOF; give it a small payload
	r := bytes.NewReader(make([]byte, 1024))
	_ = m.FeedFrom(r) // returns io.EOF

	got := m.BytesReceivedAt()
	if got.IsZero() {
		t.Fatal("BytesReceivedAt() is zero after FeedFrom wrote bytes")
	}
	if got.Before(before) {
		t.Errorf("BytesReceivedAt() = %v, want after %v", got, before)
	}
}
