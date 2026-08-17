/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	meclient "github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// fakeME is an injectable stand-in for the media-engine client.
type fakeME struct {
	startErr   error
	stopResult *meclient.StopRecordingResult
	stopErr    error
	startCalls int
	stopCalls  int
}

func (f *fakeME) StartRecording(ctx context.Context, req *meclient.StartRecordingRequest) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeME) StopRecording(ctx context.Context, stationID, recordingID string) (*meclient.StopRecordingResult, error) {
	f.stopCalls++
	return f.stopResult, f.stopErr
}

func newSvcWithME(t *testing.T, me MediaEngine) (*Service, *gorm.DB) {
	t.Helper()
	svc, db := newSvc(t) // reuse the sqlite harness from service_test.go
	svc.meClient = me
	return svc, db
}

func TestStartRecording_Success(t *testing.T) {
	me := &fakeME{}
	svc, db := newSvcWithME(t, me)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S", RecordingQuotaBytes: 0})

	rec, err := svc.StartRecording(bg(), StartRequest{StationID: dbtest.UUID("st1"), MountID: dbtest.UUID("m1"), UserID: dbtest.UUID("u1"), Title: "Live Set"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if rec.Status != models.RecordingStatusActive {
		t.Fatalf("status = %s, want active", rec.Status)
	}
	if me.startCalls != 1 {
		t.Fatalf("media engine StartRecording calls = %d, want 1", me.startCalls)
	}
	var n int64
	db.Model(&models.Recording{}).Count(&n)
	if n != 1 {
		t.Fatalf("expected 1 recording row, got %d", n)
	}
}

func TestStartRecording_MediaEngineError_CleansUp(t *testing.T) {
	me := &fakeME{startErr: errors.New("engine down")}
	svc, db := newSvcWithME(t, me)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})

	if _, err := svc.StartRecording(bg(), StartRequest{StationID: dbtest.UUID("st1"), MountID: dbtest.UUID("m1"), UserID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected error when the media engine fails")
	}
	// The DB entry must be rolled back so no orphan recording is left behind.
	var n int64
	db.Model(&models.Recording{}).Count(&n)
	if n != 0 {
		t.Fatalf("failed start should leave no recording rows, got %d", n)
	}
}

func TestStopRecording_Success_UpdatesQuota(t *testing.T) {
	me := &fakeME{stopResult: &meclient.StopRecordingResult{RecordingID: dbtest.UUID("r1"), FileSizeBytes: 2048, DurationMs: 60000}}
	svc, db := newSvcWithME(t, me)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S", RecordingStorageUsed: 100})
	db.Create(&models.StationUser{ID: dbtest.UUID("su"), UserID: dbtest.UUID("u1"), StationID: dbtest.UUID("st1"), RecordingStorageUsed: 100})
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)

	rec, err := svc.StopRecording(bg(), dbtest.UUID("r1"))
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if rec.Status != models.RecordingStatusComplete || rec.SizeBytes != 2048 || rec.DurationMs != 60000 {
		t.Fatalf("stopped recording not finalized: %+v", rec)
	}
	if rec.StoppedAt == nil {
		t.Fatal("StoppedAt should be set")
	}
	if me.stopCalls != 1 {
		t.Fatalf("media engine StopRecording calls = %d, want 1", me.stopCalls)
	}

	// Station quota usage grows by the recorded size.
	var station models.Station
	db.First(&station, "id = ?", dbtest.UUID("st1"))
	if station.RecordingStorageUsed != 100+2048 {
		t.Fatalf("station storage used = %d, want %d", station.RecordingStorageUsed, 100+2048)
	}
}

func TestStopRecording_NotActive(t *testing.T) {
	svc, db := newSvcWithME(t, &fakeME{})
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusComplete, 0)
	if _, err := svc.StopRecording(bg(), dbtest.UUID("r1")); err == nil {
		t.Fatal("expected error stopping a non-active recording")
	}
}

func TestStopRecording_MediaEngineError_MarksFailed(t *testing.T) {
	me := &fakeME{stopErr: errors.New("engine down")}
	svc, db := newSvcWithME(t, me)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"})
	seedRecording(t, db, dbtest.UUID("r1"), dbtest.UUID("st1"), models.RecordingStatusActive, 0)

	if _, err := svc.StopRecording(bg(), dbtest.UUID("r1")); err == nil {
		t.Fatal("expected error when the media engine stop fails")
	}
	var rec models.Recording
	db.First(&rec, "id = ?", dbtest.UUID("r1"))
	if rec.Status != models.RecordingStatusFailed {
		t.Fatalf("status = %s, want failed", rec.Status)
	}
}
