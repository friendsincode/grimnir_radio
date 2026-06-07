/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// icyTestServer serves an ICY-metaint stream that always carries the same
// StreamTitle. Enough audio bytes are streamed so the poller's discard loop
// reaches the metadata block on the first read.
func icyTestServer(t *testing.T, streamTitle string) *httptest.Server {
	t.Helper()
	const metaInt = 16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", strconv.Itoa(metaInt))
		w.Header().Set("icy-name", "ICY Test")
		w.WriteHeader(http.StatusOK)

		audio := make([]byte, metaInt)
		_, _ = w.Write(audio)

		raw := "StreamTitle='" + streamTitle + "';"
		blocks := (len(raw) + 15) / 16
		buf := make([]byte, 1+blocks*16)
		buf[0] = byte(blocks)
		copy(buf[1:], raw)
		_, _ = w.Write(buf)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(50 * time.Millisecond):
		}
	}))
	return srv
}

// TestICYPoller_UpdatePlayHistory_DoesNotWriteMediaID regresses issue #218.
// The pre-fix code did `db.Save(&history)` on a PlayHistory row whose
// `media_id` is empty (always true for webstream-sourced rows). Save
// re-serializes every column, so Postgres rejected the UPDATE with
// SQLSTATE 22P02 "invalid input syntax for type uuid".
//
// The fix switched to
//
//	db.Model(&history).Updates(map[string]any{
//	    "artist": …, "title": …, "metadata": …,
//	})
//
// which writes only the listed columns and leaves media_id alone.
//
// This test registers a GORM `Before("gorm:update")` callback that inspects
// the statement GORM is about to issue and FAILS if the column set includes
// `media_id`. The pre-fix Save path passed a *PlayHistory struct, so every
// column (including media_id) shows up in `tx.Statement.Selects`/`Omits`-
// computed assignment set. The post-fix map path lists only the three
// columns. Either way we don't need a real Postgres — the callback is the
// regression assertion.
func TestICYPoller_UpdatePlayHistory_DoesNotWriteMediaID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var (
		mu                 sync.Mutex
		updateSawMediaID   bool
		updateUsedStruct   bool
		updateAttemptCount int
	)
	if err := db.Callback().Update().Before("gorm:update").Register(
		"detect_media_id_write",
		func(tx *gorm.DB) {
			if tx.Statement == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			updateAttemptCount++
			// Save(&history) sets Dest to *PlayHistory. Updates(map) sets it to a map.
			switch dest := tx.Statement.Dest.(type) {
			case *models.PlayHistory:
				updateUsedStruct = true
				if dest.MediaID == "" {
					updateSawMediaID = true
				}
			case map[string]any:
				if _, present := dest["media_id"]; present {
					updateSawMediaID = true
				}
			}
		},
	); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	stationID := uuid.NewString()
	mountID := uuid.NewString()
	wsID := uuid.NewString()

	// Seed the row the poller will update.
	historyID := uuid.NewString()
	row := models.PlayHistory{
		ID:        historyID,
		StationID: stationID,
		MountID:   mountID,
		MediaID:   "", // webstream rows always have this empty (#218 root cause)
		EntryID:   uuid.NewString(),
		Position:  0,
		Title:     "Initial Webstream Name",
		StartedAt: time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed PlayHistory: %v", err)
	}

	srv := icyTestServer(t, "Real Artist - Real Song")
	defer srv.Close()

	bus := events.NewBus()
	p := NewICYPoller(wsID, stationID, mountID, srv.URL, bus, db, zerolog.Nop())
	p.pollInterval = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.poll(ctx)

	mu.Lock()
	defer mu.Unlock()
	if updateAttemptCount == 0 {
		t.Fatal("poll() never issued an UPDATE — ICY test server likely misbehaved; test scaffolding bug")
	}
	if updateUsedStruct {
		t.Errorf("ICY poller called Save(&PlayHistory): regression of #218. " +
			"Must use Updates(map[string]any{...}) so the empty media_id is not " +
			"serialized into the UPDATE.")
	}
	if updateSawMediaID {
		t.Errorf("ICY poller UPDATE statement included media_id column: regression of #218. " +
			"Postgres rejects empty media_id with SQLSTATE 22P02.")
	}
}
