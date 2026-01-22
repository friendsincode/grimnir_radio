package media

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/config"
)

// Storage interface abstracts file storage operations.
type Storage interface {
	Store(ctx context.Context, stationID, mediaID string, file io.Reader) (string, error)
	Delete(ctx context.Context, path string) error
	URL(path string) string
}

// Service manages media file storage.
type Service struct {
	storage Storage
	logger  zerolog.Logger
}

// NewService creates a media service using filesystem or S3 storage based on config.
func NewService(cfg *config.Config, logger zerolog.Logger) *Service {
	var storage Storage

	if cfg.ObjectStorageURL != "" {
		// Use S3-compatible storage if ObjectStorageURL is configured
		storage = NewS3Storage(cfg.ObjectStorageURL, cfg.MediaRoot, logger)
	} else {
		// Default to filesystem storage
		storage = NewFilesystemStorage(cfg.MediaRoot, logger)
	}

	return &Service{
		storage: storage,
		logger:  logger,
	}
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

// buildMediaPath constructs a hierarchical storage path for a media file.
func buildMediaPath(stationID, mediaID, extension string) string {
	// Structure: station_id/media_id[0:2]/media_id[2:4]/media_id.ext
	// This creates a balanced directory structure to avoid too many files in one dir
	if len(mediaID) < 4 {
		return filepath.Join(stationID, mediaID+extension)
	}
	return filepath.Join(stationID, mediaID[0:2], mediaID[2:4], mediaID+extension)
}
