/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// OrphanScanner scans media directories for files not in the database.
type OrphanScanner struct {
	db        *gorm.DB
	mediaRoot string
	logger    zerolog.Logger
}

// NewOrphanScanner creates a new orphan scanner.
func NewOrphanScanner(db *gorm.DB, mediaRoot string, logger zerolog.Logger) *OrphanScanner {
	return &OrphanScanner{
		db:        db,
		mediaRoot: mediaRoot,
		logger:    logger.With().Str("component", "orphan_scanner").Logger(),
	}
}

// ScanForOrphans walks media directory, finds files not in database, and records them.
func (s *OrphanScanner) ScanForOrphans(ctx context.Context) (*models.ScanResult, error) {
	startTime := time.Now()
	result := &models.ScanResult{}

	s.logger.Info().Str("media_root", s.mediaRoot).Msg("starting orphan scan")

	// Get all known media paths from database
	knownPaths, err := s.getKnownMediaPaths(ctx)
	if err != nil {
		return nil, fmt.Errorf("get known paths: %w", err)
	}

	s.logger.Debug().Int("known_paths", len(knownPaths)).Msg("loaded known media paths")

	// Get already-tracked orphan paths
	orphanPaths, err := s.getOrphanPaths(ctx)
	if err != nil {
		return nil, fmt.Errorf("get orphan paths: %w", err)
	}

	// Walk the media directory
	err = filepath.Walk(s.mediaRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			s.logger.Warn().Err(err).Str("path", path).Msg("error accessing path")
			result.Errors++
			return nil // Continue walking
		}

		// Check context for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip non-media files (basic extension check)
		if !isMediaFile(info.Name()) {
			return nil
		}

		result.TotalFiles++

		// Get relative path
		relPath, err := filepath.Rel(s.mediaRoot, path)
		if err != nil {
			s.logger.Warn().Err(err).Str("path", path).Msg("failed to get relative path")
			result.Errors++
			return nil
		}

		// Check if this file is already known in media_items
		if _, known := knownPaths[relPath]; known {
			return nil
		}

		// Check if already tracked as orphan
		if _, tracked := orphanPaths[relPath]; tracked {
			result.AlreadyKnown++
			return nil
		}

		// This is a new orphan - compute hash and store
		orphan, err := s.createOrphanRecord(ctx, path, relPath, info)
		if err != nil {
			s.logger.Warn().Err(err).Str("path", relPath).Msg("failed to create orphan record")
			result.Errors++
			return nil
		}

		if err := s.db.WithContext(ctx).Create(orphan).Error; err != nil {
			s.logger.Warn().Err(err).Str("path", relPath).Msg("failed to save orphan record")
			result.Errors++
			return nil
		}

		result.NewOrphans++
		result.TotalSize += info.Size()

		s.logger.Debug().
			Str("path", relPath).
			Str("hash", orphan.ContentHash[:12]).
			Int64("size", info.Size()).
			Msg("new orphan detected")

		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("walk media directory: %w", err)
	}

	result.Duration = time.Since(startTime)

	s.logger.Info().
		Int("total_files", result.TotalFiles).
		Int("new_orphans", result.NewOrphans).
		Int("already_known", result.AlreadyKnown).
		Int("errors", result.Errors).
		Dur("duration", result.Duration).
		Msg("orphan scan complete")

	return result, nil
}

// GetOrphans returns a paginated list of orphan media records.
func (s *OrphanScanner) GetOrphans(ctx context.Context, page, pageSize int) ([]models.OrphanMedia, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	var orphans []models.OrphanMedia
	var total int64

	// Count total
	if err := s.db.WithContext(ctx).Model(&models.OrphanMedia{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count orphans: %w", err)
	}

	// Get page
	offset := (page - 1) * pageSize
	if err := s.db.WithContext(ctx).
		Order("detected_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&orphans).Error; err != nil {
		return nil, 0, fmt.Errorf("get orphans: %w", err)
	}

	return orphans, total, nil
}

// GetAllOrphans returns all orphan records (for import matching).
func (s *OrphanScanner) GetAllOrphans(ctx context.Context) ([]models.OrphanMedia, error) {
	var orphans []models.OrphanMedia
	if err := s.db.WithContext(ctx).Find(&orphans).Error; err != nil {
		return nil, fmt.Errorf("get all orphans: %w", err)
	}
	return orphans, nil
}

// GetOrphanByHash finds an orphan by content hash (for import matching).
func (s *OrphanScanner) GetOrphanByHash(ctx context.Context, hash string) (*models.OrphanMedia, error) {
	var orphan models.OrphanMedia
	if err := s.db.WithContext(ctx).Where("content_hash = ?", hash).First(&orphan).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get orphan by hash: %w", err)
	}
	return &orphan, nil
}

// GetOrphanByID finds an orphan by ID.
func (s *OrphanScanner) GetOrphanByID(ctx context.Context, id string) (*models.OrphanMedia, error) {
	var orphan models.OrphanMedia
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&orphan).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get orphan by id: %w", err)
	}
	return &orphan, nil
}

// AdoptOrphan converts an orphan to a MediaItem for a station.
func (s *OrphanScanner) AdoptOrphan(ctx context.Context, orphanID, stationID string) (*models.MediaItem, error) {
	orphan, err := s.GetOrphanByID(ctx, orphanID)
	if err != nil {
		return nil, err
	}
	if orphan == nil {
		return nil, fmt.Errorf("orphan not found: %s", orphanID)
	}

	// Create media item from orphan
	mediaItem := &models.MediaItem{
		ID:            uuid.New().String(),
		StationID:     stationID,
		Title:         orphan.Title,
		Artist:        orphan.Artist,
		Album:         orphan.Album,
		Duration:      orphan.Duration,
		StorageKey:    orphan.FilePath,
		Path:          orphan.FilePath,
		ContentHash:   orphan.ContentHash,
		AnalysisState: models.AnalysisPending,
	}

	// Set default title from filename if not set
	if mediaItem.Title == "" {
		mediaItem.Title = strings.TrimSuffix(filepath.Base(orphan.FilePath), filepath.Ext(orphan.FilePath))
	}

	// Create in transaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(mediaItem).Error; err != nil {
			return fmt.Errorf("create media item: %w", err)
		}

		if err := tx.Delete(&models.OrphanMedia{}, "id = ?", orphanID).Error; err != nil {
			return fmt.Errorf("delete orphan: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	s.logger.Info().
		Str("orphan_id", orphanID).
		Str("media_id", mediaItem.ID).
		Str("station_id", stationID).
		Msg("orphan adopted as media item")

	return mediaItem, nil
}

// AdoptOrphanForImport adopts an orphan during import, returning the MediaItem.
// This is used by the import process when an orphan matches by hash.
func (s *OrphanScanner) AdoptOrphanForImport(ctx context.Context, orphan *models.OrphanMedia, stationID, jobID, sourceID string) (*models.MediaItem, error) {
	// Create media item from orphan with import provenance
	mediaItem := &models.MediaItem{
		ID:             uuid.New().String(),
		StationID:      stationID,
		Title:          orphan.Title,
		Artist:         orphan.Artist,
		Album:          orphan.Album,
		Duration:       orphan.Duration,
		StorageKey:     orphan.FilePath,
		Path:           orphan.FilePath,
		ContentHash:    orphan.ContentHash,
		ImportJobID:    &jobID,
		ImportSource:   "libretime",
		ImportSourceID: sourceID,
		AnalysisState:  models.AnalysisPending,
	}

	// Set default title from filename if not set
	if mediaItem.Title == "" {
		mediaItem.Title = strings.TrimSuffix(filepath.Base(orphan.FilePath), filepath.Ext(orphan.FilePath))
	}

	// Create in transaction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(mediaItem).Error; err != nil {
			return fmt.Errorf("create media item: %w", err)
		}

		if err := tx.Delete(&models.OrphanMedia{}, "id = ?", orphan.ID).Error; err != nil {
			return fmt.Errorf("delete orphan: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	s.logger.Info().
		Str("orphan_id", orphan.ID).
		Str("media_id", mediaItem.ID).
		Str("station_id", stationID).
		Str("import_job", jobID).
		Msg("orphan adopted during import")

	return mediaItem, nil
}

// DeleteOrphan removes an orphan record and optionally the file.
func (s *OrphanScanner) DeleteOrphan(ctx context.Context, orphanID string, deleteFile bool) error {
	orphan, err := s.GetOrphanByID(ctx, orphanID)
	if err != nil {
		return err
	}
	if orphan == nil {
		return fmt.Errorf("orphan not found: %s", orphanID)
	}

	// Delete file if requested
	if deleteFile {
		fullPath := filepath.Join(s.mediaRoot, orphan.FilePath)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete file: %w", err)
		}
		s.logger.Info().Str("path", fullPath).Msg("deleted orphan file")
	}

	// Delete record
	if err := s.db.WithContext(ctx).Delete(&models.OrphanMedia{}, "id = ?", orphanID).Error; err != nil {
		return fmt.Errorf("delete orphan record: %w", err)
	}

	s.logger.Info().Str("orphan_id", orphanID).Bool("file_deleted", deleteFile).Msg("orphan deleted")

	return nil
}

// BulkAdoptOrphans adopts multiple orphans to a station.
func (s *OrphanScanner) BulkAdoptOrphans(ctx context.Context, orphanIDs []string, stationID string) (int, error) {
	adopted := 0
	for _, id := range orphanIDs {
		if _, err := s.AdoptOrphan(ctx, id, stationID); err != nil {
			s.logger.Warn().Err(err).Str("orphan_id", id).Msg("failed to adopt orphan")
			continue
		}
		adopted++
	}
	return adopted, nil
}

// BulkDeleteOrphans deletes multiple orphans.
func (s *OrphanScanner) BulkDeleteOrphans(ctx context.Context, orphanIDs []string, deleteFiles bool) (int, error) {
	deleted := 0
	for _, id := range orphanIDs {
		if err := s.DeleteOrphan(ctx, id, deleteFiles); err != nil {
			s.logger.Warn().Err(err).Str("orphan_id", id).Msg("failed to delete orphan")
			continue
		}
		deleted++
	}
	return deleted, nil
}

// GetOrphanStats returns aggregate statistics about orphans.
func (s *OrphanScanner) GetOrphanStats(ctx context.Context) (count int64, totalSize int64, err error) {
	if err := s.db.WithContext(ctx).Model(&models.OrphanMedia{}).Count(&count).Error; err != nil {
		return 0, 0, fmt.Errorf("count orphans: %w", err)
	}

	var result struct {
		TotalSize int64
	}
	if err := s.db.WithContext(ctx).
		Model(&models.OrphanMedia{}).
		Select("COALESCE(SUM(file_size), 0) as total_size").
		Scan(&result).Error; err != nil {
		return 0, 0, fmt.Errorf("sum orphan sizes: %w", err)
	}

	return count, result.TotalSize, nil
}

// BuildOrphanHashMap builds a map of content hash -> orphan for efficient lookups.
func (s *OrphanScanner) BuildOrphanHashMap(ctx context.Context) (map[string]*models.OrphanMedia, error) {
	orphans, err := s.GetAllOrphans(ctx)
	if err != nil {
		return nil, err
	}

	hashMap := make(map[string]*models.OrphanMedia, len(orphans))
	for i := range orphans {
		if orphans[i].ContentHash != "" {
			hashMap[orphans[i].ContentHash] = &orphans[i]
		}
	}

	return hashMap, nil
}

// Helper functions

func (s *OrphanScanner) getKnownMediaPaths(ctx context.Context) (map[string]struct{}, error) {
	// Query both path and storage_key since imports use path while
	// direct uploads may use storage_key. Either field matching means
	// the file is known and not an orphan.
	type pathRow struct {
		Path       string
		StorageKey string
	}
	var rows []pathRow
	if err := s.db.WithContext(ctx).
		Model(&models.MediaItem{}).
		Select("path, storage_key").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[string]struct{}, len(rows)*2)
	for _, r := range rows {
		if r.Path != "" {
			result[r.Path] = struct{}{}
		}
		if r.StorageKey != "" {
			result[r.StorageKey] = struct{}{}
		}
	}
	return result, nil
}

func (s *OrphanScanner) getOrphanPaths(ctx context.Context) (map[string]struct{}, error) {
	var paths []string
	if err := s.db.WithContext(ctx).
		Model(&models.OrphanMedia{}).
		Pluck("file_path", &paths).Error; err != nil {
		return nil, err
	}

	result := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		result[p] = struct{}{}
	}
	return result, nil
}

func (s *OrphanScanner) createOrphanRecord(ctx context.Context, fullPath, relPath string, info os.FileInfo) (*models.OrphanMedia, error) {
	// Compute content hash
	hash, err := computeFileHash(fullPath)
	if err != nil {
		return nil, fmt.Errorf("compute hash: %w", err)
	}

	orphan := &models.OrphanMedia{
		ID:          uuid.New().String(),
		FilePath:    relPath,
		ContentHash: hash,
		FileSize:    info.Size(),
		DetectedAt:  time.Now(),
	}

	// Try to extract title from filename
	baseName := filepath.Base(relPath)
	orphan.Title = strings.TrimSuffix(baseName, filepath.Ext(baseName))

	return orphan, nil
}

func computeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func isMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".audio", ".mp3", ".flac", ".ogg", ".m4a", ".aac", ".wav", ".wma", ".opus":
		return true
	default:
		return false
	}
}
