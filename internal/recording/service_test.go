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
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newSvc(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := dbtest.Open(t, &models.Station{}, &models.StationUser{}, &models.Recording{}, &models.RecordingChapter{})
	// meClient is nil: only DB paths and quota rejections are exercised here.
	return NewService(db, nil, t.TempDir(), zerolog.Nop()), db
}

func bg() context.Context { return context.Background() }

// ---------------------------------------------------------------------------
// pure helpers (chapter_embed.go)
// ---------------------------------------------------------------------------

func TestFormatChapterTimestamp(t *testing.T) {
	cases := map[int64]string{
		0:       "00:00:00.000",
		1500:    "00:00:01.500",
		61000:   "00:01:01.000",
		3661250: "01:01:01.250",
	}
	for ms, want := range cases {
		if got := formatChapterTimestamp(ms); got != want {
			t.Fatalf("formatChapterTimestamp(%d) = %q, want %q", ms, got, want)
		}
	}
}

func TestFormatChapterName(t *testing.T) {
	if got := formatChapterName("Enjoy the Silence", "Depeche Mode"); got != "Depeche Mode - Enjoy the Silence" {
		t.Fatalf("both = %q", got)
	}
	if got := formatChapterName("Just Title", ""); got != "Just Title" {
		t.Fatalf("title-only = %q", got)
	}
	if got := formatChapterName("", "Just Artist"); got != "Just Artist" {
		t.Fatalf("artist-only = %q", got)
	}
	if got := formatChapterName("", ""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

// ---------------------------------------------------------------------------
// StartRecording quota guards (reject before the media engine is touched)
// ---------------------------------------------------------------------------

func TestStartRecording_StationNotFound(t *testing.T) {
	svc, _ := newSvc(t)
	if _, err := svc.StartRecording(bg(), StartRequest{StationID: dbtest.UUID("missing"), UserID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected error for missing station")
	}
}

func TestStartRecording_StationQuotaExceeded(t *testing.T) {
	svc, db := newSvc(t)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S", RecordingQuotaBytes: 1000, RecordingStorageUsed: 1000})
	if _, err := svc.StartRecording(bg(), StartRequest{StationID: dbtest.UUID("st1"), UserID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected station quota-exceeded error")
	}
}

func TestStartRecording_DJQuotaExceeded(t *testing.T) {
	svc, db := newSvc(t)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S", RecordingQuotaBytes: 0})
	db.Create(&models.StationUser{ID: dbtest.UUID("su"), UserID: dbtest.UUID("u1"), StationID: dbtest.UUID("st1"), RecordingQuotaBytes: 500, RecordingStorageUsed: 500})
	if _, err := svc.StartRecording(bg(), StartRequest{StationID: dbtest.UUID("st1"), UserID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected DJ quota-exceeded error")
	}
}

// ---------------------------------------------------------------------------
// chapters + recording CRUD
// ---------------------------------------------------------------------------

func seedRecording(t *testing.T, db *gorm.DB, id, station string, status string, size int64) {
	t.Helper()
	db.Where("id = ?", station).FirstOrCreate(&models.Station{ID: station, OwnerID: dbtest.UUID("owner"), Name: station})
	r := &models.Recording{
		ID: id, StationID: station, UserID: dbtest.UUID("u1"), Status: status, MountID: dbtest.UUID("m1"),
		StartedAt: time.Now().Add(-time.Minute), SizeBytes: size,
	}
	if err := db.Create(r).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}
}

func TestAddChapter(t *testing.T) {
	svc, db := newSvc(t)
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)

	if err := svc.AddChapter(bg(), dbtest.UUID("r1"), "Song A", "Artist A", "Album"); err != nil {
		t.Fatalf("add chapter 1: %v", err)
	}
	if err := svc.AddChapter(bg(), dbtest.UUID("r1"), "Song B", "Artist B", "Album"); err != nil {
		t.Fatalf("add chapter 2: %v", err)
	}
	var chapters []models.RecordingChapter
	db.Where("recording_id = ?", dbtest.UUID("r1")).Order("position ASC").Find(&chapters)
	if len(chapters) != 2 || chapters[0].Position != 0 || chapters[1].Position != 1 {
		t.Fatalf("chapter positions wrong: %+v", chapters)
	}

	// Chapters can't be added to an inactive/missing recording.
	seedRecording(t, db, dbtest.UUID("r2"), dbtest.UUID("st1"), models.RecordingStatusComplete, 0)
	if err := svc.AddChapter(bg(), dbtest.UUID("r2"), "x", "y", "z"); err == nil {
		t.Fatal("expected error adding chapter to a non-active recording")
	}
}

func TestListAndGetRecording(t *testing.T) {
	svc, db := newSvc(t)
	for _, id := range []string{dbtest.UUID("r1"), dbtest.UUID("r2"), dbtest.UUID("r3")} {
		seedRecording(t, db, id, dbtest.UUID("st1"), models.RecordingStatusComplete, 100)
	}
	seedRecording(t, db, dbtest.UUID("other"), dbtest.UUID("st2"), models.RecordingStatusComplete, 100)

	recs, total, err := svc.ListRecordings(bg(), dbtest.UUID("st1"), 2, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(recs) != 2 {
		t.Fatalf("list st1: total=%d page=%d, want 3/2", total, len(recs))
	}

	// GetRecording preloads chapters.
	db.Create(&models.RecordingChapter{ID: dbtest.UUID("c1"), RecordingID: dbtest.UUID("r1"), Position: 0, Title: "T"})
	got, err := svc.GetRecording(bg(), dbtest.UUID("r1"))
	if err != nil || len(got.Chapters) != 1 {
		t.Fatalf("get with chapters: %v / %+v", err, got)
	}
	if _, err := svc.GetRecording(bg(), dbtest.UUID("missing")); err == nil {
		t.Fatal("expected error for missing recording")
	}
}

func TestUpdateAndDeleteRecording(t *testing.T) {
	svc, db := newSvc(t)
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusComplete, 0)
	db.Create(&models.RecordingChapter{ID: dbtest.UUID("c1"), RecordingID: dbtest.UUID("r1"), Position: 0})

	if err := svc.UpdateRecording(bg(), dbtest.UUID("r1"), map[string]any{"title": "Renamed"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	var reloaded models.Recording
	db.First(&reloaded, "id = ?", dbtest.UUID("r1"))
	if reloaded.Title != "Renamed" {
		t.Fatalf("title = %q", reloaded.Title)
	}

	// Active recordings can't be deleted.
	seedRecording(t, db, dbtest.UUID("active"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)
	if err := svc.DeleteRecording(bg(), dbtest.UUID("active")); err == nil {
		t.Fatal("expected error deleting an active recording")
	}

	// Zero-size complete recording deletes cleanly and cascades chapters.
	if err := svc.DeleteRecording(bg(), dbtest.UUID("r1")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var recs, chaps int64
	db.Model(&models.Recording{}).Where("id = ?", dbtest.UUID("r1")).Count(&recs)
	db.Model(&models.RecordingChapter{}).Where("recording_id = ?", dbtest.UUID("r1")).Count(&chaps)
	if recs != 0 || chaps != 0 {
		t.Fatalf("delete left rows: recs=%d chaps=%d", recs, chaps)
	}

	if err := svc.DeleteRecording(bg(), dbtest.UUID("missing")); err == nil {
		t.Fatal("expected error deleting a missing recording")
	}
}

func TestGetQuotaUsage(t *testing.T) {
	svc, db := newSvc(t)
	db.Create(&models.Station{
		ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S",
		RecordingQuotaBytes: 5000, RecordingQuotaMode: "block",
		RecordingStorageUsed: 1200, RecordingDefaultFormat: "flac",
	})
	q, err := svc.GetQuotaUsage(bg(), dbtest.UUID("st1"))
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if q.StationQuotaBytes != 5000 || q.StationUsedBytes != 1200 || !q.QuotaEnabled {
		t.Fatalf("quota info wrong: %+v", q)
	}

	// A station with a zero quota reports QuotaEnabled=false.
	db.Create(&models.Station{ID: dbtest.UUID("st2"), OwnerID: dbtest.UUID("owner"), Name: "S2", RecordingQuotaBytes: 0})
	q2, _ := svc.GetQuotaUsage(bg(), dbtest.UUID("st2"))
	if q2.QuotaEnabled {
		t.Fatal("zero quota should report disabled")
	}
}
