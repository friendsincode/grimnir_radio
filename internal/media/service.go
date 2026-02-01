/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/config"
)

// Storage interface abstracts file storage operations.
type Storage interface {
	Store(ctx context.Context, stationID, mediaID string, file io.Reader) (string, error)
	Delete(ctx context.Context, path string) error
	URL(path string) string
	CheckAccess(ctx context.Context) error
}

// Service manages media file storage.
type Service struct {
	storage Storage
	logger  zerolog.Logger
}

// NewService creates a media service using filesystem or S3 storage based on config.
func NewService(cfg *config.Config, logger zerolog.Logger) (*Service, error) {
	var storage Storage

	// Use S3 storage if bucket is configured
	if cfg.S3Bucket != "" {
		s3cfg := S3Config{
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			Region:          cfg.S3Region,
			Bucket:          cfg.S3Bucket,
			Endpoint:        cfg.S3Endpoint,
			PublicBaseURL:   cfg.S3PublicBaseURL,
			UsePathStyle:    cfg.S3UsePathStyle,
			ForcePathStyle:  cfg.S3UsePathStyle,
		}

		// Use default values if not configured
		if s3cfg.AccessKeyID == "" || s3cfg.SecretAccessKey == "" {
			logger.Warn().Msg("S3 credentials not configured, some operations may fail")
		}

		s3Storage, err := NewS3Storage(context.Background(), s3cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize S3 storage: %w", err)
		}
		storage = s3Storage
	} else {
		// Default to filesystem storage
		storage = NewFilesystemStorage(cfg.MediaRoot, logger)
	}

	return &Service{
		storage: storage,
		logger:  logger,
	}, nil
}

// Store saves an uploaded file and returns the storage path.
func (s *Service) Store(ctx context.Context, stationID, mediaID string, file io.Reader) (string, error) {
	path, err := s.storage.Store(ctx, stationID, mediaID, file)
	if err != nil {
		s.logger.Error().Err(err).
			Str("station_id", stationID).
			Str("media_id", mediaID).
			Msg("media store failed")
		return "", fmt.Errorf("store media: %w", err)
	}

	s.logger.Info().
		Str("station_id", stationID).
		Str("media_id", mediaID).
		Str("path", path).
		Msg("media stored successfully")

	return path, nil
}

// Delete removes a media file from storage.
func (s *Service) Delete(ctx context.Context, path string) error {
	if err := s.storage.Delete(ctx, path); err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("media delete failed")
		return fmt.Errorf("delete media: %w", err)
	}

	s.logger.Info().Str("path", path).Msg("media deleted successfully")
	return nil
}

// URL returns the accessible URL for a stored media file.
func (s *Service) URL(path string) string {
	return s.storage.URL(path)
}

// CheckStorageAccess verifies that the storage backend is accessible.
func (s *Service) CheckStorageAccess() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.storage.CheckAccess(ctx)
}

// buildMediaPath constructs a hierarchical storage path for a media file.
func buildMediaPath(stationID, mediaID, extension string) string {
	// Structure: station_id/media_id[0:2]/media_id[2:4]/media_id.ext
	// This creates a balanced directory structure to avoid too many files in one dir
	if len(mediaID) < 4 {
		return filepath.Join(stationID, mediaID+extension)
	}
	return filepath.Join(stationID, mediaID[0:2], mediaID[2:4], mediaID+extension)
}
