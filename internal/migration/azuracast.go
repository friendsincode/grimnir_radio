/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package migration

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// AzuraCastImporter implements the Importer interface for AzuraCast backups.
type AzuraCastImporter struct {
	db           *gorm.DB
	mediaService *media.Service
	logger       zerolog.Logger
}

// NewAzuraCastImporter creates a new AzuraCast importer.
func NewAzuraCastImporter(db *gorm.DB, mediaService *media.Service, logger zerolog.Logger) *AzuraCastImporter {
	return &AzuraCastImporter{
		db:           db,
		mediaService: mediaService,
		logger:       logger.With().Str("importer", "azuracast").Logger(),
	}
}

// Validate checks if the migration can proceed.
func (a *AzuraCastImporter) Validate(ctx context.Context, options Options) error {
	var errors ValidationErrors

	if options.AzuraCastBackupPath == "" {
		errors = append(errors, ValidationError{
			Field:   "azuracast_backup_path",
			Message: "backup path is required",
		})
	}

	// Check if backup file exists
	if options.AzuraCastBackupPath != "" {
		if _, err := os.Stat(options.AzuraCastBackupPath); os.IsNotExist(err) {
			errors = append(errors, ValidationError{
				Field:   "azuracast_backup_path",
				Message: fmt.Sprintf("backup file does not exist: %s", options.AzuraCastBackupPath),
			})
		}
	}

	// Check if it's a valid tar.gz file
	if options.AzuraCastBackupPath != "" && !strings.HasSuffix(options.AzuraCastBackupPath, ".tar.gz") {
		errors = append(errors, ValidationError{
			Field:   "azuracast_backup_path",
			Message: "backup file must be a .tar.gz archive",
		})
	}

	if len(errors) > 0 {
		return errors
	}

	return nil
}

// Analyze performs a dry-run analysis.
func (a *AzuraCastImporter) Analyze(ctx context.Context, options Options) (*Result, error) {
	a.logger.Info().Str("backup_path", options.AzuraCastBackupPath).Msg("analyzing AzuraCast backup")

	// Extract backup to temporary directory
	tempDir, err := os.MkdirTemp("", "azuracast-backup-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := a.extractBackup(options.AzuraCastBackupPath, tempDir); err != nil {
		return nil, fmt.Errorf("extract backup: %w", err)
	}

	// Parse backup structure
	backup, err := a.parseBackup(tempDir)
	if err != nil {
		return nil, fmt.Errorf("parse backup: %w", err)
	}

	// Build result with counts
	result := &Result{
		StationsCreated:    len(backup.Stations),
		MediaItemsImported: backup.MediaCount,
		PlaylistsCreated:   backup.PlaylistCount,
		SchedulesCreated:   backup.ScheduleCount,
		UsersCreated:       len(backup.Users),
		Warnings:           []string{},
		Skipped:            make(map[string]int),
		Mappings:           make(map[string]Mapping),
	}

	// Check for potential issues
	if len(backup.Stations) == 0 {
		result.Warnings = append(result.Warnings, "No stations found in backup")
	}

	if backup.MediaCount == 0 {
		result.Warnings = append(result.Warnings, "No media files found in backup")
	}

	a.logger.Info().
		Int("stations", result.StationsCreated).
		Int("media", result.MediaItemsImported).
		Int("playlists", result.PlaylistsCreated).
		Msg("backup analysis complete")

	return result, nil
}

// Import performs the actual migration.
func (a *AzuraCastImporter) Import(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error) {
	startTime := time.Now()
	a.logger.Info().Str("backup_path", options.AzuraCastBackupPath).Msg("starting AzuraCast import")

	// Phase 1: Extract backup
	progressCallback(Progress{
		Phase:          "extracting",
		CurrentStep:    "Extracting backup archive",
		TotalSteps:     5,
		CompletedSteps: 0,
		Percentage:     0,
		StartTime:      startTime,
	})

	tempDir, err := os.MkdirTemp("", "azuracast-import-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := a.extractBackup(options.AzuraCastBackupPath, tempDir); err != nil {
		return nil, fmt.Errorf("extract backup: %w", err)
	}

	// Phase 2: Parse backup
	progressCallback(Progress{
		Phase:          "parsing",
		CurrentStep:    "Parsing backup data",
		TotalSteps:     5,
		CompletedSteps: 1,
		Percentage:     20,
		StartTime:      startTime,
	})

	backup, err := a.parseBackup(tempDir)
	if err != nil {
		return nil, fmt.Errorf("parse backup: %w", err)
	}

	result := &Result{
		Warnings:  []string{},
		Skipped:   make(map[string]int),
		Mappings:  make(map[string]Mapping),
	}

	// Phase 3: Import stations
	progressCallback(Progress{
		Phase:          "importing_stations",
		CurrentStep:    "Importing stations",
		TotalSteps:     5,
		CompletedSteps: 2,
		Percentage:     40,
		StationsTotal:  len(backup.Stations),
		StartTime:      startTime,
	})

	if err := a.importStations(ctx, backup, result, progressCallback, startTime); err != nil {
		return nil, fmt.Errorf("import stations: %w", err)
	}

	// Phase 4: Import media
	if !options.SkipMedia {
		progressCallback(Progress{
			Phase:          "importing_media",
			CurrentStep:    "Importing media files",
			TotalSteps:     5,
			CompletedSteps: 3,
			Percentage:     60,
			MediaTotal:     backup.MediaCount,
			StartTime:      startTime,
		})

		if err := a.importMedia(ctx, tempDir, backup, result, progressCallback, startTime); err != nil {
			return nil, fmt.Errorf("import media: %w", err)
		}
	} else {
		result.Skipped["media"] = backup.MediaCount
	}

	// Phase 5: Import playlists
	if !options.SkipPlaylists {
		progressCallback(Progress{
			Phase:           "importing_playlists",
			CurrentStep:     "Importing playlists",
			TotalSteps:      5,
			CompletedSteps:  4,
			Percentage:      80,
			PlaylistsTotal:  backup.PlaylistCount,
			StartTime:       startTime,
		})

		if err := a.importPlaylists(ctx, backup, result); err != nil {
			return nil, fmt.Errorf("import playlists: %w", err)
		}
	} else {
		result.Skipped["playlists"] = backup.PlaylistCount
	}

	// Complete
	progressCallback(Progress{
		Phase:           "completed",
		CurrentStep:     "Migration completed",
		TotalSteps:      5,
		CompletedSteps:  5,
		Percentage:      100,
		StationsImported: result.StationsCreated,
		MediaImported:    result.MediaItemsImported,
		PlaylistsImported: result.PlaylistsCreated,
		StartTime:        startTime,
	})

	result.DurationSeconds = time.Since(startTime).Seconds()

	a.logger.Info().
		Int("stations", result.StationsCreated).
		Int("media", result.MediaItemsImported).
		Int("playlists", result.PlaylistsCreated).
		Float64("duration", result.DurationSeconds).
		Msg("AzuraCast import completed")

	return result, nil
}

// extractBackup extracts a tar.gz backup to a directory.
func (a *AzuraCastImporter) extractBackup(backupPath, destDir string) error {
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open backup: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent directory: %w", err)
			}

			outFile, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("write file: %w", err)
			}
			outFile.Close()
		}
	}

	a.logger.Info().Str("dest", destDir).Msg("backup extracted")
	return nil
}

// AzuraCastBackup represents the parsed backup structure.
type AzuraCastBackup struct {
	Stations      []AzuraCastStation
	MediaCount    int
	PlaylistCount int
	ScheduleCount int
	Users         []AzuraCastUser
}

// AzuraCastStation represents an AzuraCast station.
type AzuraCastStation struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ShortName   string `json:"short_name"`
	IsEnabled   bool   `json:"is_enabled"`
}

// AzuraCastUser represents an AzuraCast user.
type AzuraCastUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// parseBackup parses the extracted backup.
func (a *AzuraCastImporter) parseBackup(dir string) (*AzuraCastBackup, error) {
	backup := &AzuraCastBackup{
		Stations: []AzuraCastStation{},
		Users:    []AzuraCastUser{},
	}

	// Look for backup.json or database dump
	backupFile := filepath.Join(dir, "backup.json")
	if _, err := os.Stat(backupFile); err == nil {
		// Parse JSON backup
		data, err := os.ReadFile(backupFile)
		if err != nil {
			return nil, fmt.Errorf("read backup.json: %w", err)
		}

		var metadata struct {
			Stations []AzuraCastStation `json:"stations"`
			Users    []AzuraCastUser    `json:"users"`
		}

		if err := json.Unmarshal(data, &metadata); err != nil {
			return nil, fmt.Errorf("parse backup.json: %w", err)
		}

		backup.Stations = metadata.Stations
		backup.Users = metadata.Users
	} else {
		// Create a default station from directory structure
		a.logger.Warn().Msg("backup.json not found, creating default station")
		backup.Stations = []AzuraCastStation{
			{
				ID:          1,
				Name:        "Imported Station",
				Description: "Imported from AzuraCast backup",
				ShortName:   "imported",
				IsEnabled:   true,
			},
		}
	}

	// Count media files
	mediaDir := filepath.Join(dir, "media")
	if stat, err := os.Stat(mediaDir); err == nil && stat.IsDir() {
		filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				ext := strings.ToLower(filepath.Ext(path))
				if ext == ".mp3" || ext == ".flac" || ext == ".ogg" || ext == ".m4a" {
					backup.MediaCount++
				}
			}
			return nil
		})
	}

	a.logger.Info().
		Int("stations", len(backup.Stations)).
		Int("media", backup.MediaCount).
		Int("users", len(backup.Users)).
		Msg("backup parsed")

	return backup, nil
}

// importStations imports stations from the backup.
func (a *AzuraCastImporter) importStations(ctx context.Context, backup *AzuraCastBackup, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	for i, azStation := range backup.Stations {
		// Create Grimnir station
		station := &models.Station{
			ID:          uuid.New().String(),
			Name:        azStation.Name,
			Description: azStation.Description,
			Timezone:    "UTC",
			Active:      azStation.IsEnabled,
		}

		if err := a.db.WithContext(ctx).Create(station).Error; err != nil {
			return fmt.Errorf("create station: %w", err)
		}

		// Track mapping
		result.Mappings[fmt.Sprintf("station_%d", azStation.ID)] = Mapping{
			OldID:  fmt.Sprintf("%d", azStation.ID),
			NewID:  station.ID,
			Type:   "station",
			Name:   station.Name,
		}

		result.StationsCreated++

		// Update progress
		progressCallback(Progress{
			Phase:            "importing_stations",
			CurrentStep:      fmt.Sprintf("Imported station: %s", station.Name),
			TotalSteps:       5,
			CompletedSteps:   2,
			Percentage:       40 + (float64(i+1)/float64(len(backup.Stations)))*20,
			StationsTotal:    len(backup.Stations),
			StationsImported: i + 1,
			StartTime:        startTime,
		})
	}

	return nil
}

// importMedia imports media files from the backup.
func (a *AzuraCastImporter) importMedia(ctx context.Context, tempDir string, backup *AzuraCastBackup, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	// Get the first station (or create default if none)
	var stationID string
	if len(backup.Stations) > 0 {
		// Find the mapped station ID
		mapping, ok := result.Mappings[fmt.Sprintf("station_%d", backup.Stations[0].ID)]
		if !ok {
			return fmt.Errorf("station mapping not found")
		}
		stationID = mapping.NewID
	} else {
		return fmt.Errorf("no station found for media import")
	}

	// Find media directory in the extracted backup
	mediaDir := filepath.Join(tempDir, "media")
	if _, err := os.Stat(mediaDir); os.IsNotExist(err) {
		a.logger.Warn().Str("media_dir", mediaDir).Msg("media directory not found in backup")
		return nil
	}

	a.logger.Info().Str("media_dir", mediaDir).Int("expected_count", backup.MediaCount).Msg("starting media file import")

	// Collect all media files
	var mediaFiles []struct {
		path     string
		relPath  string
		fileSize int64
	}

	err := filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Check if it's an audio file
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".mp3" || ext == ".flac" || ext == ".ogg" || ext == ".m4a" || ext == ".wav" || ext == ".aac" {
			relPath, _ := filepath.Rel(mediaDir, path)
			mediaFiles = append(mediaFiles, struct {
				path     string
				relPath  string
				fileSize int64
			}{
				path:     path,
				relPath:  relPath,
				fileSize: info.Size(),
			})
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("scan media directory: %w", err)
	}

	a.logger.Info().Int("files_found", len(mediaFiles)).Msg("media files collected")

	if len(mediaFiles) == 0 {
		a.logger.Warn().Msg("no media files found in backup")
		return nil
	}

	// Create file operations handler
	fileOps := NewFileOperations(a.mediaService, a.logger)

	// Prepare copy jobs
	var copyJobs []FileCopyJob
	mediaItemsByID := make(map[string]*models.MediaItem)

	for _, mediaFile := range mediaFiles {
		// Create MediaItem record
		mediaID := uuid.New().String()

		// Extract basic metadata from filename
		filename := filepath.Base(mediaFile.path)
		title := strings.TrimSuffix(filename, filepath.Ext(filename))

		mediaItem := &models.MediaItem{
			ID:         mediaID,
			StationID:  stationID,
			Title:      title,
			Path:       "",        // Will be set after upload
			StorageKey: "",        // Will be set after upload
			ImportPath: mediaFile.relPath,
			Duration:   0,         // Will be analyzed later
		}

		// Save to database
		if err := a.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
			a.logger.Error().Err(err).Str("title", title).Msg("failed to create media item")
			continue
		}

		mediaItemsByID[mediaID] = mediaItem

		// Create copy job
		copyJobs = append(copyJobs, FileCopyJob{
			SourcePath: mediaFile.path,
			StationID:  stationID,
			MediaID:    mediaID,
			FileSize:   mediaFile.fileSize,
		})
	}

	// Copy files with progress tracking
	copyOptions := DefaultCopyOptions()
	copyOptions.Concurrency = 4
	copyOptions.ProgressCallback = func(copied, total int) {
		percentage := 60 + (float64(copied)/float64(total))*20
		progressCallback(Progress{
			Phase:          "copying_media",
			CurrentStep:    fmt.Sprintf("Copying media files: %d/%d", copied, total),
			TotalSteps:     5,
			CompletedSteps: 3,
			Percentage:     percentage,
			MediaTotal:     len(copyJobs),
			MediaCopied:    copied,
			StartTime:      startTime,
		})
	}

	copyResults, err := fileOps.CopyFiles(ctx, copyJobs, copyOptions)
	if err != nil {
		return fmt.Errorf("copy media files: %w", err)
	}

	// Update MediaItem records with storage keys
	successCount := 0
	failCount := 0

	for _, copyResult := range copyResults {
		if copyResult.Success {
			mediaItem := mediaItemsByID[copyResult.MediaID]
			if mediaItem != nil {
				// Update with storage key
				mediaItem.StorageKey = copyResult.StorageKey
				mediaItem.Path = a.mediaService.URL(copyResult.StorageKey)

				if err := a.db.WithContext(ctx).Save(mediaItem).Error; err != nil {
					a.logger.Error().Err(err).Str("media_id", copyResult.MediaID).Msg("failed to update media item with storage key")
					failCount++
					continue
				}

				successCount++

				// Track mapping
				result.Mappings[fmt.Sprintf("media_%s", mediaItem.ImportPath)] = Mapping{
					OldID: mediaItem.ImportPath,
					NewID: mediaItem.ID,
					Type:  "media",
					Name:  mediaItem.Title,
				}
			}
		} else {
			a.logger.Error().Err(copyResult.Error).Str("media_id", copyResult.MediaID).Msg("failed to copy media file")
			failCount++
		}
	}

	result.MediaItemsImported = successCount

	if failCount > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d media files failed to import", failCount))
	}

	a.logger.Info().
		Int("success", successCount).
		Int("failed", failCount).
		Msg("media import complete")

	return nil
}

// importPlaylists imports playlists from the backup.
func (a *AzuraCastImporter) importPlaylists(ctx context.Context, backup *AzuraCastBackup, result *Result) error {
	// Placeholder for playlist import
	result.PlaylistsCreated = backup.PlaylistCount

	if backup.PlaylistCount > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Playlist import not fully implemented - %d playlists found but not imported", backup.PlaylistCount))
	}

	return nil
}
