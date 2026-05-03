/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

func TestCalculateTimeAwarePosition_BasicCases(t *testing.T) {
	d, _ := newMockDirector(t)
	stationID := uuid.NewString()

	items := make([]string, 5)
	for i := range items {
		m := models.MediaItem{
			ID:            uuid.NewString(),
			StationID:     stationID,
			Path:          "/tmp/t.mp3",
			Duration:      3 * time.Minute,
			AnalysisState: models.AnalysisComplete,
		}
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
		items[i] = m.ID
	}

	// 7 min elapsed: tracks 0+1 = 6min, at 7min we're into track 2
	if pos := d.calculateTimeAwarePosition(context.Background(), items, 7*time.Minute); pos != 2 {
		t.Errorf("7min elapsed → position 2, got %d", pos)
	}

	// 1 second: below threshold, returns 0
	if pos := d.calculateTimeAwarePosition(context.Background(), items, time.Second); pos != 0 {
		t.Errorf("1s elapsed should return 0, got %d", pos)
	}

	// 20 min (past total 15min): returns last index
	if pos := d.calculateTimeAwarePosition(context.Background(), items, 20*time.Minute); pos != 4 {
		t.Errorf("20min elapsed (past end) should return 4, got %d", pos)
	}
}

func TestCalculateTimeAwarePosition_EmptyItems(t *testing.T) {
	d, _ := newMockDirector(t)
	if pos := d.calculateTimeAwarePosition(context.Background(), nil, 10*time.Minute); pos != 0 {
		t.Errorf("empty items should return 0, got %d", pos)
	}
}

func TestCalculateTimeAwarePosition_ExactBoundary(t *testing.T) {
	d, _ := newMockDirector(t)
	stationID := uuid.NewString()

	items := make([]string, 3)
	for i := range items {
		m := models.MediaItem{
			ID:            uuid.NewString(),
			StationID:     stationID,
			Path:          "/tmp/boundary.mp3",
			Duration:      5 * time.Minute,
			AnalysisState: models.AnalysisComplete,
		}
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
		items[i] = m.ID
	}

	// Exactly at 5min boundary: cumulative=5min >= elapsed=5min → position 0
	if pos := d.calculateTimeAwarePosition(context.Background(), items, 5*time.Minute); pos != 0 {
		t.Errorf("exactly 5min should return 0, got %d", pos)
	}

	// 5min + 1s: into track 1
	if pos := d.calculateTimeAwarePosition(context.Background(), items, 5*time.Minute+time.Second); pos != 1 {
		t.Errorf("5min+1s should return 1, got %d", pos)
	}
}
