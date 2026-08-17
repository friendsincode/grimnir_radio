/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestAutoRecordHandler_StartsRecordingOnEvent(t *testing.T) {
	me := &fakeME{}
	svc, db := newSvcWithME(t, me)
	db.Create(&models.Station{ID: "st1", Name: "Station One"})

	bus := events.NewBus()
	h := NewAutoRecordHandler(svc, bus, zerolog.Nop())

	ctx, cancel := context.WithCancel(bg())
	defer cancel()
	h.Start(ctx)
	defer h.Stop()

	time.Sleep(20 * time.Millisecond) // let the subscription settle
	bus.Publish(events.EventRecordingAutoStart, events.Payload{
		"station_id": "st1", "mount_id": "m1", "user_id": "u1", "username": "dj-nova",
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int64
		db.Model(&models.Recording{}).Count(&n)
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auto-record did not create a recording (count stayed 0)")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Stop waits for the handler goroutine, so reading startCalls is race-free.
	h.Stop()
	if me.startCalls != 1 {
		t.Fatalf("media engine StartRecording calls = %d, want 1", me.startCalls)
	}
}

func TestHandleAutoRecord_MissingFields_NoOp(t *testing.T) {
	me := &fakeME{}
	svc, db := newSvcWithME(t, me)
	db.Create(&models.Station{ID: "st1", Name: "Station One"})
	h := NewAutoRecordHandler(svc, events.NewBus(), zerolog.Nop())

	// user_id missing → handler returns without starting anything.
	h.handleAutoRecord(bg(), events.Payload{"station_id": "st1", "mount_id": "m1"})

	var n int64
	db.Model(&models.Recording{}).Count(&n)
	if n != 0 || me.startCalls != 0 {
		t.Fatalf("expected no recording for incomplete payload; rows=%d startCalls=%d", n, me.startCalls)
	}
}
