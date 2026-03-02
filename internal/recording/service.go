/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	meclient "github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Service manages recordings: start, stop, quota, chapters, cleanup.
type Service struct {
	db        *gorm.DB
	meClient  *meclient.Client
	mediaRoot string
	logger    zerolog.Logger
}

// NewService creates a new recording service.
func NewService(db *gorm.DB, meClient *meclient.Client, mediaRoot string, logger zerolog.Logger) *Service {
	return &Service{
		db:        db,
		meClient:  meClient,
		mediaRoot: mediaRoot,
		logger:    logger.With().Str("component", "recording-service").Logger(),
	}
}

// StartRequest contains the parameters for starting a recording.
type StartRequest struct {
	StationID string
	MountID   string
	UserID    string
	Title     string
	Format    string // "flac" or "opus"
}

// StartRecording creates a recording entry and tells the media engine to start.
func (s *Service) StartRecording(ctx context.Context, req StartRequest) (*models.Recording, error) {
	// Check station quota
	var station models.Station
	if err := s.db.First(&station, "id = ?", req.StationID).Error; err != nil {
		return nil, fmt.Errorf("station not found: %w", err)
	}

	if station.RecordingQuotaBytes > 0 && station.RecordingStorageUsed >= station.RecordingQuotaBytes {
		return nil, fmt.Errorf("station recording quota exceeded (%d/%d bytes)", station.RecordingStorageUsed, station.RecordingQuotaBytes)
	}

	// Check per-DJ quota
	var stationUser models.StationUser
	if err := s.db.First(&stationUser, "user_id = ? AND station_id = ?", req.UserID, req.StationID).Error; err == nil {
		if stationUser.RecordingQuotaBytes > 0 && stationUser.RecordingStorageUsed >= stationUser.RecordingQuotaBytes {
			return nil, fmt.Errorf("DJ recording quota exceeded (%d/%d bytes)", stationUser.RecordingStorageUsed, stationUser.RecordingQuotaBytes)
		}
	}

	// Determine format
	format := req.Format
	if format == "" {
		format = station.RecordingDefaultFormat
	}
	if format == "" {
		format = models.RecordingFormatFLAC
	}

	// Build relative path
	recordingID := uuid.New().String()
	dateDir := time.Now().Format("2006/01")
	ext := "flac"
	if format == models.RecordingFormatOpus {
		ext = "ogg"
	}
	relPath := filepath.Join(req.StationID, "recordings", dateDir, recordingID+"."+ext)

	// Create recording entry
	recording := models.Recording{
		ID:         recordingID,
		StationID:  req.StationID,
		MountID:    req.MountID,
		UserID:     req.UserID,
		Title:      req.Title,
		Path:       relPath,
		Format:     format,
		Status:     models.RecordingStatusActive,
		Visibility: models.RecordingVisibilityDefault,
		SampleRate: 44100,
		Channels:   2,
		StartedAt:  time.Now(),
	}

	if err := s.db.Create(&recording).Error; err != nil {
		return nil, fmt.Errorf("create recording entry: %w", err)
	}

	// Tell media engine to start recording
	absPath := filepath.Join(s.mediaRoot, relPath)
	if err := s.meClient.StartRecording(ctx, &meclient.StartRecordingRequest{
		StationID:   req.StationID,
		MountID:     req.MountID,
		RecordingID: recordingID,
		OutputPath:  absPath,
		Codec:       format,
		SampleRate:  44100,
		Channels:    2,
	}); err != nil {
		// Clean up the DB entry
		s.db.Delete(&recording)
		return nil, fmt.Errorf("media engine start recording: %w", err)
	}

	s.logger.Info().
		Str("recording_id", recordingID).
		Str("station_id", req.StationID).
		Str("format", format).
		Msg("recording started")

	return &recording, nil
}

// StopRecording stops a recording and updates the DB with file info.
func (s *Service) StopRecording(ctx context.Context, recordingID string) (*models.Recording, error) {
	var recording models.Recording
	if err := s.db.First(&recording, "id = ?", recordingID).Error; err != nil {
		return nil, fmt.Errorf("recording not found: %w", err)
	}

	if recording.Status != models.RecordingStatusActive {
		return nil, fmt.Errorf("recording is not active (status: %s)", recording.Status)
	}

	// Update status to finalizing
	s.db.Model(&recording).Update("status", models.RecordingStatusFinalizing)

	// Tell media engine to stop
	result, err := s.meClient.StopRecording(ctx, recording.StationID, recordingID)
	if err != nil {
		s.db.Model(&recording).Updates(map[string]any{
			"status": models.RecordingStatusFailed,
		})
		return nil, fmt.Errorf("media engine stop recording: %w", err)
	}

	now := time.Now()
	updates := map[string]any{
		"status":      models.RecordingStatusComplete,
		"stopped_at":  &now,
		"size_bytes":  result.FileSizeBytes,
		"duration_ms": result.DurationMs,
	}
	s.db.Model(&recording).Updates(updates)

	// Update station quota usage
	s.db.Model(&models.Station{}).
		Where("id = ?", recording.StationID).
		Update("recording_storage_used", gorm.Expr("recording_storage_used + ?", result.FileSizeBytes))

	// Update per-DJ quota usage
	s.db.Model(&models.StationUser{}).
		Where("user_id = ? AND station_id = ?", recording.UserID, recording.StationID).
		Update("recording_storage_used", gorm.Expr("recording_storage_used + ?", result.FileSizeBytes))

	s.logger.Info().
		Str("recording_id", recordingID).
		Int64("size_bytes", result.FileSizeBytes).
		Int64("duration_ms", result.DurationMs).
		Msg("recording stopped")

	// Embed chapters into the file (best-effort, non-blocking).
	go func() {
		var chapters []models.RecordingChapter
		s.db.Where("recording_id = ?", recordingID).Order("position ASC").Find(&chapters)
		if len(chapters) > 0 {
			absPath := filepath.Join(s.mediaRoot, recording.Path)
			if err := EmbedChapters(absPath, recording.Format, chapters, s.logger); err != nil {
				s.logger.Warn().Err(err).Str("recording_id", recordingID).Msg("chapter embedding failed")
			}
		}
	}()

	// Reload
	s.db.First(&recording, "id = ?", recordingID)
	return &recording, nil
}

// AddChapter adds a chapter marker to an active recording.
func (s *Service) AddChapter(ctx context.Context, recordingID string, title, artist, album string) error {
	var recording models.Recording
	if err := s.db.First(&recording, "id = ? AND status = ?", recordingID, models.RecordingStatusActive).Error; err != nil {
		return fmt.Errorf("active recording not found: %w", err)
	}

	// Count existing chapters to get next position
	var count int64
	s.db.Model(&models.RecordingChapter{}).Where("recording_id = ?", recordingID).Count(&count)

	offsetMs := time.Since(recording.StartedAt).Milliseconds()
	chapter := models.RecordingChapter{
		ID:          uuid.New().String(),
		RecordingID: recordingID,
		Position:    int(count),
		OffsetMs:    offsetMs,
		Title:       title,
		Artist:      artist,
		Album:       album,
	}

	if err := s.db.Create(&chapter).Error; err != nil {
		return fmt.Errorf("create chapter: %w", err)
	}

	return nil
}

// ListRecordings returns recordings for a station.
func (s *Service) ListRecordings(ctx context.Context, stationID string, limit, offset int) ([]models.Recording, int64, error) {
	var total int64
	s.db.Model(&models.Recording{}).Where("station_id = ?", stationID).Count(&total)

	if limit <= 0 {
		limit = 50
	}

	var recordings []models.Recording
	err := s.db.Where("station_id = ?", stationID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&recordings).Error

	return recordings, total, err
}

// GetRecording returns a single recording with chapters.
func (s *Service) GetRecording(ctx context.Context, recordingID string) (*models.Recording, error) {
	var recording models.Recording
	if err := s.db.Preload("Chapters", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&recording, "id = ?", recordingID).Error; err != nil {
		return nil, err
	}
	return &recording, nil
}

// UpdateRecording updates recording metadata fields.
func (s *Service) UpdateRecording(ctx context.Context, recordingID string, updates map[string]any) error {
	return s.db.Model(&models.Recording{}).Where("id = ?", recordingID).Updates(updates).Error
}

// DeleteRecording deletes a recording, its file, and updates quotas.
func (s *Service) DeleteRecording(ctx context.Context, recordingID string) error {
	var recording models.Recording
	if err := s.db.First(&recording, "id = ?", recordingID).Error; err != nil {
		return fmt.Errorf("recording not found: %w", err)
	}

	if recording.Status == models.RecordingStatusActive {
		return fmt.Errorf("cannot delete active recording — stop it first")
	}

	// Delete file
	if recording.Path != "" {
		absPath := filepath.Join(s.mediaRoot, recording.Path)
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			s.logger.Warn().Err(err).Str("path", absPath).Msg("failed to delete recording file")
		}
	}

	// Delete chapters
	s.db.Where("recording_id = ?", recordingID).Delete(&models.RecordingChapter{})

	// Delete recording
	s.db.Delete(&recording)

	// Decrement quota usage
	if recording.SizeBytes > 0 {
		s.db.Model(&models.Station{}).
			Where("id = ?", recording.StationID).
			Update("recording_storage_used", gorm.Expr("GREATEST(recording_storage_used - ?, 0)", recording.SizeBytes))

		s.db.Model(&models.StationUser{}).
			Where("user_id = ? AND station_id = ?", recording.UserID, recording.StationID).
			Update("recording_storage_used", gorm.Expr("GREATEST(recording_storage_used - ?, 0)", recording.SizeBytes))
	}

	s.logger.Info().Str("recording_id", recordingID).Msg("recording deleted")
	return nil
}

// QuotaInfo contains quota usage information for a station.
type QuotaInfo struct {
	StationQuotaBytes int64
	StationQuotaMode  string
	StationUsedBytes  int64
	StationFormat     string
	QuotaEnabled      bool
}

// GetQuotaUsage returns the quota usage info for a station.
func (s *Service) GetQuotaUsage(ctx context.Context, stationID string) (*QuotaInfo, error) {
	var station models.Station
	if err := s.db.Select("recording_quota_bytes, recording_quota_mode, recording_storage_used, recording_default_format").
		First(&station, "id = ?", stationID).Error; err != nil {
		return nil, err
	}

	return &QuotaInfo{
		StationQuotaBytes: station.RecordingQuotaBytes,
		StationQuotaMode:  station.RecordingQuotaMode,
		StationUsedBytes:  station.RecordingStorageUsed,
		StationFormat:     station.RecordingDefaultFormat,
		QuotaEnabled:      station.RecordingQuotaBytes != 0,
	}, nil
}
