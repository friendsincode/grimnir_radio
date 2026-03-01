/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// LibreTimeImporter implements the Importer interface for LibreTime databases.
type LibreTimeImporter struct {
	db            *gorm.DB
	mediaService  *media.Service
	orphanScanner *media.OrphanScanner
	logger        zerolog.Logger
}

// NewLibreTimeImporter creates a new LibreTime importer.
func NewLibreTimeImporter(db *gorm.DB, mediaService *media.Service, logger zerolog.Logger) *LibreTimeImporter {
	return &LibreTimeImporter{
		db:           db,
		mediaService: mediaService,
		logger:       logger.With().Str("importer", "libretime").Logger(),
	}
}

// SetOrphanScanner sets the orphan scanner for orphan matching during import.
func (l *LibreTimeImporter) SetOrphanScanner(scanner *media.OrphanScanner) {
	l.orphanScanner = scanner
}

// isLibreTimeAPIMode returns true if we should use API import instead of database import.
func isLibreTimeAPIMode(options Options) bool {
	return options.LibreTimeAPIURL != "" && options.LibreTimeAPIKey != ""
}

// Validate checks if the migration can proceed.
func (l *LibreTimeImporter) Validate(ctx context.Context, options Options) error {
	var errors ValidationErrors

	// Check if we're using API mode
	if isLibreTimeAPIMode(options) {
		return l.validateAPI(ctx, options)
	}

	// Database mode validation
	if options.LibreTimeDBHost == "" {
		errors = append(errors, ValidationError{
			Field:   "libretime_db_host",
			Message: "database host is required",
		})
	}

	if options.LibreTimeDBName == "" {
		errors = append(errors, ValidationError{
			Field:   "libretime_db_name",
			Message: "database name is required",
		})
	}

	if options.LibreTimeDBUser == "" {
		errors = append(errors, ValidationError{
			Field:   "libretime_db_user",
			Message: "database user is required",
		})
	}

	// Try to connect to LibreTime database
	if len(errors) == 0 {
		ltDB, err := l.connectLibreTime(options)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   "libretime_db_host",
				Message: fmt.Sprintf("failed to connect to LibreTime database: %v", err),
			})
		} else {
			sqlDB, _ := ltDB.DB()
			sqlDB.Close()
		}
	}

	if len(errors) > 0 {
		return errors
	}

	return nil
}

// validateAPI validates API mode configuration.
func (l *LibreTimeImporter) validateAPI(ctx context.Context, options Options) error {
	var errors ValidationErrors

	if options.LibreTimeAPIURL == "" {
		errors = append(errors, ValidationError{
			Field:   "libretime_api_url",
			Message: "API URL is required",
		})
	}

	if options.LibreTimeAPIKey == "" {
		errors = append(errors, ValidationError{
			Field:   "libretime_api_key",
			Message: "API key is required",
		})
	}

	// Test API connection
	if len(errors) == 0 {
		client, err := NewLibreTimeAPIClient(options.LibreTimeAPIURL, options.LibreTimeAPIKey)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   "libretime_api",
				Message: fmt.Sprintf("failed to create API client: %v", err),
			})
		} else {
			_, err := client.TestConnection(ctx)
			if err != nil {
				errors = append(errors, ValidationError{
					Field:   "libretime_api",
					Message: fmt.Sprintf("API connection failed: %v", err),
				})
			}
		}
	}

	if len(errors) > 0 {
		return errors
	}

	return nil
}

// Analyze performs a dry-run analysis.
func (l *LibreTimeImporter) Analyze(ctx context.Context, options Options) (*Result, error) {
	if isLibreTimeAPIMode(options) {
		return l.analyzeAPI(ctx, options)
	}
	return l.analyzeDatabase(ctx, options)
}

// analyzeAPI performs analysis via the LibreTime API.
func (l *LibreTimeImporter) analyzeAPI(ctx context.Context, options Options) (*Result, error) {
	report, err := l.AnalyzeDetailed(ctx, options)
	if err != nil {
		return nil, err
	}

	result := &Result{
		StationsCreated:    1, // LibreTime is single-station
		MediaItemsImported: report.TotalFiles,
		PlaylistsCreated:   report.TotalPlaylists,
		SchedulesCreated:   report.TotalShows,
		UsersCreated:       0,
		Warnings:           report.Warnings,
		Skipped:            make(map[string]int),
		Mappings:           make(map[string]Mapping),
	}

	return result, nil
}

// AnalyzeDetailed performs a detailed dry-run analysis and returns a full report.
func (l *LibreTimeImporter) AnalyzeDetailed(ctx context.Context, options Options) (*LibreTimeAnalysisReport, error) {
	l.logger.Info().Str("api_url", options.LibreTimeAPIURL).Msg("analyzing LibreTime via API (detailed)")

	client, err := NewLibreTimeAPIClient(options.LibreTimeAPIURL, options.LibreTimeAPIKey)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	report := &LibreTimeAnalysisReport{
		Warnings: []string{},
	}

	// Get files
	files, err := client.GetFiles(ctx)
	if err != nil {
		l.logger.Warn().Err(err).Msg("failed to get files")
		report.Warnings = append(report.Warnings, fmt.Sprintf("Could not fetch files: %v", err))
	} else {
		report.TotalFiles = len(files)
		var totalSize int64
		for _, f := range files {
			if !f.Hidden && f.FileExists {
				totalSize += f.Size
				report.Files = append(report.Files, LTFileSummary{
					ID:     f.ID,
					Title:  f.Title,
					Artist: f.Artist,
					Size:   f.Size,
				})
			}
		}
		report.EstimatedStorageBytes = totalSize
		report.EstimatedStorageHuman = formatBytes(totalSize)
	}

	// Get playlists
	playlists, err := client.GetPlaylists(ctx)
	if err != nil {
		l.logger.Warn().Err(err).Msg("failed to get playlists")
	} else {
		report.TotalPlaylists = len(playlists)
		for _, pl := range playlists {
			// Try to get playlist contents to count items by type
			contents, _ := client.GetPlaylistContents(ctx, pl.ID)
			var fileCount, blockCount, streamCount int
			for _, c := range contents {
				if c.FileID != nil {
					fileCount++
				} else if c.BlockID != nil {
					blockCount++
				} else if c.StreamID != nil {
					streamCount++
				}
			}
			report.Playlists = append(report.Playlists, LTPlaylistSummary{
				ID:          pl.ID,
				Name:        pl.Name,
				ItemCount:   len(contents),
				FileCount:   fileCount,
				BlockCount:  blockCount,
				StreamCount: streamCount,
				Length:      pl.Length,
			})
		}
	}

	// Get shows
	shows, err := client.GetShows(ctx)
	if err != nil {
		l.logger.Warn().Err(err).Msg("failed to get shows")
	} else {
		report.TotalShows = len(shows)
		for _, show := range shows {
			report.Shows = append(report.Shows, LTShowSummary{
				ID:          show.ID,
				Name:        show.Name,
				Description: show.Description,
				Genre:       show.Genre,
			})
		}
	}

	// Check for potential issues
	if report.TotalFiles == 0 {
		report.Warnings = append(report.Warnings, "No media files found")
	}

	l.logger.Info().
		Int("files", report.TotalFiles).
		Int("playlists", report.TotalPlaylists).
		Int("shows", report.TotalShows).
		Str("storage", report.EstimatedStorageHuman).
		Msg("detailed API analysis complete")

	return report, nil
}

// analyzeDatabase performs analysis via direct database access.
func (l *LibreTimeImporter) analyzeDatabase(ctx context.Context, options Options) (*Result, error) {
	l.logger.Info().Str("db_host", options.LibreTimeDBHost).Msg("analyzing LibreTime database")

	// Connect to LibreTime database
	ltDB, err := l.connectLibreTime(options)
	if err != nil {
		return nil, fmt.Errorf("connect to LibreTime: %w", err)
	}
	defer func() {
		sqlDB, _ := ltDB.DB()
		sqlDB.Close()
	}()

	// Query counts
	var stationCount int64
	var mediaCount int64
	var playlistCount int64
	var showCount int64
	var userCount int64

	// LibreTime is typically single-station, check preferences
	ltDB.Raw("SELECT COUNT(*) FROM cc_pref WHERE keystr = 'stationName'").Scan(&stationCount)
	if stationCount == 0 {
		stationCount = 1 // Default station
	}

	// Count media files
	ltDB.Raw("SELECT COUNT(*) FROM cc_files WHERE file_exists = TRUE").Scan(&mediaCount)

	// Count playlists
	ltDB.Raw("SELECT COUNT(*) FROM cc_playlist").Scan(&playlistCount)

	// Count shows (these become clocks in Grimnir)
	ltDB.Raw("SELECT COUNT(*) FROM cc_show").Scan(&showCount)

	// Count users
	ltDB.Raw("SELECT COUNT(*) FROM cc_subjs").Scan(&userCount)

	result := &Result{
		StationsCreated:    int(stationCount),
		MediaItemsImported: int(mediaCount),
		PlaylistsCreated:   int(playlistCount),
		SchedulesCreated:   int(showCount),
		UsersCreated:       int(userCount),
		Warnings:           []string{},
		Skipped:            make(map[string]int),
		Mappings:           make(map[string]Mapping),
	}

	if stationCount == 0 {
		result.Warnings = append(result.Warnings, "No station configuration found in LibreTime")
	}

	if mediaCount == 0 {
		result.Warnings = append(result.Warnings, "No media files found in LibreTime library")
	}

	l.logger.Info().
		Int64("stations", stationCount).
		Int64("media", mediaCount).
		Int64("playlists", playlistCount).
		Int64("shows", showCount).
		Msg("LibreTime analysis complete")

	return result, nil
}

// Import performs the actual migration.
func (l *LibreTimeImporter) Import(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error) {
	if isLibreTimeAPIMode(options) {
		return l.importAPI(ctx, options, progressCallback)
	}
	return l.importDatabase(ctx, options, progressCallback)
}

// importAPI imports from a live LibreTime instance via API.
func (l *LibreTimeImporter) importAPI(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error) {
	startTime := time.Now()
	l.logger.Info().Str("api_url", options.LibreTimeAPIURL).Msg("starting LibreTime API import")

	client, err := NewLibreTimeAPIClient(options.LibreTimeAPIURL, options.LibreTimeAPIKey)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	result := &Result{
		Warnings: []string{},
		Skipped:  make(map[string]int),
		Mappings: make(map[string]Mapping),
	}

	// Phase 1: Create or select station
	progressCallback(Progress{
		Phase:          "setup",
		CurrentStep:    "Setting up station",
		TotalSteps:     5,
		CompletedSteps: 0,
		Percentage:     0,
		StartTime:      startTime,
	})

	var stationID string
	if options.TargetStationID != "" {
		// Use existing station
		stationID = options.TargetStationID
		l.logger.Info().Str("station_id", stationID).Msg("using existing station")
	} else {
		// Fetch station info from LibreTime for branding
		stationInfo, err := client.GetStationInfo(ctx)

		// Create new station with LibreTime metadata
		station := &models.Station{
			ID:          uuid.New().String(),
			Name:        "Imported from LibreTime",
			Description: "Station imported from LibreTime via API",
			Timezone:    "UTC",
			Active:      true,
			Public:      false,
			Approved:    true,
		}

		// Apply station info from LibreTime if available
		if err == nil && stationInfo != nil {
			if stationInfo.StationName != "" {
				station.Name = stationInfo.StationName
			}
			if stationInfo.StationDescription != "" {
				station.Description = stationInfo.StationDescription
			}
			if stationInfo.StationWebsite != "" {
				station.Website = stationInfo.StationWebsite
			}
			if stationInfo.StationGenre != "" {
				station.Genre = stationInfo.StationGenre
			}
			if stationInfo.StationLanguage != "" {
				station.Language = stationInfo.StationLanguage
			}
			if stationInfo.StationTimezone != "" {
				station.Timezone = stationInfo.StationTimezone
			}
			if stationInfo.StationContactEmail != "" {
				station.ContactEmail = stationInfo.StationContactEmail
			}

			l.logger.Info().
				Str("name", station.Name).
				Str("genre", station.Genre).
				Str("website", station.Website).
				Msg("applied LibreTime station branding")
		}

		if options.ImportingUserID != "" {
			station.OwnerID = options.ImportingUserID
		}

		if err := l.db.WithContext(ctx).Create(station).Error; err != nil {
			return nil, fmt.Errorf("create station: %w", err)
		}

		// Create station-user association for the owner
		if options.ImportingUserID != "" {
			stationUser := &models.StationUser{
				ID:        uuid.New().String(),
				UserID:    options.ImportingUserID,
				StationID: station.ID,
				Role:      models.StationRoleOwner,
			}
			if err := l.db.WithContext(ctx).Create(stationUser).Error; err != nil {
				l.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to create owner association")
			}
		}

		stationID = station.ID
		result.StationsCreated++

		result.Mappings["station_main"] = Mapping{
			OldID: "libretime_station",
			NewID: station.ID,
			Type:  "station",
			Name:  station.Name,
		}

		// Auto-generate default mount point
		mountName := models.GenerateMountName(station.Shortcode)
		if mountName == "" || mountName == "radio" {
			mountName = models.GenerateMountName(station.Name)
		}
		mount := &models.Mount{
			ID:         uuid.New().String(),
			StationID:  station.ID,
			Name:       mountName,
			URL:        "/live/" + mountName,
			Format:     "mp3",
			Bitrate:    128,
			Channels:   2,
			SampleRate: 44100,
		}
		if err := l.db.WithContext(ctx).Create(mount).Error; err != nil {
			l.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to create default mount")
		} else {
			l.logger.Info().
				Str("station_id", station.ID).
				Str("mount", mountName).
				Msg("created default mount")
		}

		l.logger.Info().
			Str("station_id", station.ID).
			Str("name", station.Name).
			Msg("station created with branding")
	}

	// Phase 2: Import media files
	if !options.SkipMedia {
		progressCallback(Progress{
			Phase:          "importing_media",
			CurrentStep:    "Fetching media files from LibreTime",
			TotalSteps:     5,
			CompletedSteps: 1,
			Percentage:     20,
			StartTime:      startTime,
		})

		if err := l.importMediaFromAPI(ctx, client, stationID, result, progressCallback, startTime); err != nil {
			l.logger.Error().Err(err).Msg("failed to import media")
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import media: %v", err))
		}
	}

	// Phase 3: Import playlists
	if !options.SkipPlaylists {
		progressCallback(Progress{
			Phase:          "importing_playlists",
			CurrentStep:    "Importing playlists",
			TotalSteps:     5,
			CompletedSteps: 3,
			Percentage:     60,
			StartTime:      startTime,
		})

		if err := l.importPlaylistsFromAPI(ctx, client, stationID, result, progressCallback, startTime); err != nil {
			l.logger.Error().Err(err).Msg("failed to import playlists")
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import playlists: %v", err))
		}
	}

	// Phase 4: Import shows as clocks
	if !options.SkipSchedules {
		progressCallback(Progress{
			Phase:          "importing_schedules",
			CurrentStep:    "Importing shows as clocks",
			TotalSteps:     7,
			CompletedSteps: 4,
			Percentage:     60,
			StartTime:      startTime,
		})

		if err := l.importShowsFromAPI(ctx, client, stationID, result); err != nil {
			l.logger.Error().Err(err).Msg("failed to import shows")
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import shows: %v", err))
		}
	}

	// Phase 5: Import webstreams
	if !options.SkipWebstreams {
		progressCallback(Progress{
			Phase:          "importing_webstreams",
			CurrentStep:    "Importing webstreams",
			TotalSteps:     7,
			CompletedSteps: 5,
			Percentage:     75,
			StartTime:      startTime,
		})

		if err := l.importWebstreamsFromAPI(ctx, client, stationID, result); err != nil {
			l.logger.Error().Err(err).Msg("failed to import webstreams")
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import webstreams: %v", err))
		}
	}

	// Phase 6: Import smart blocks
	if !options.SkipSmartblocks {
		progressCallback(Progress{
			Phase:          "importing_smartblocks",
			CurrentStep:    "Importing smart blocks",
			TotalSteps:     7,
			CompletedSteps: 6,
			Percentage:     90,
			StartTime:      startTime,
		})

		if err := l.importSmartBlocksFromAPI(ctx, client, stationID, result); err != nil {
			l.logger.Error().Err(err).Msg("failed to import smart blocks")
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import smart blocks: %v", err))
		}
	}

	// Complete
	progressCallback(Progress{
		Phase:             "completed",
		CurrentStep:       "Import completed",
		TotalSteps:        7,
		CompletedSteps:    7,
		Percentage:        100,
		StationsImported:  result.StationsCreated,
		MediaImported:     result.MediaItemsImported,
		PlaylistsImported: result.PlaylistsCreated,
		SchedulesImported: result.SchedulesCreated,
		StartTime:         startTime,
	})

	result.DurationSeconds = time.Since(startTime).Seconds()

	l.logger.Info().
		Int("stations", result.StationsCreated).
		Int("media", result.MediaItemsImported).
		Int("playlists", result.PlaylistsCreated).
		Int("schedules", result.SchedulesCreated).
		Float64("duration", result.DurationSeconds).
		Msg("LibreTime API import completed")

	return result, nil
}

// mediaDownloadResult holds the result of a single media file download.
type mediaDownloadResult struct {
	ltFile      LTFile
	data        []byte
	contentHash string
	err         error
	errType     string // "download", "read"
}

// importMediaFromAPI imports media files from LibreTime API with concurrent HTTP downloads.
// Downloads up to 12 files concurrently for optimal performance.
func (l *LibreTimeImporter) importMediaFromAPI(ctx context.Context, client *LibreTimeAPIClient, stationID string, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	files, err := client.GetFiles(ctx)
	if err != nil {
		return fmt.Errorf("get files: %w", err)
	}

	// Filter to only visible, existing files
	var mediaFiles []LTFile
	for _, f := range files {
		if !f.Hidden && f.FileExists {
			mediaFiles = append(mediaFiles, f)
		}
	}

	if len(mediaFiles) == 0 {
		l.logger.Info().Msg("no media files to import")
		return nil
	}

	l.logger.Info().Int("count", len(mediaFiles)).Msg("importing media files via API (concurrent HTTP downloads)")

	// Concurrent download settings
	const maxConcurrentDownloads = 12 // Download 12 files at a time
	semaphore := make(chan struct{}, maxConcurrentDownloads)
	resultsChan := make(chan mediaDownloadResult, maxConcurrentDownloads)

	var wg sync.WaitGroup
	var processedCount int32
	var deduplicatedCount int32
	var mu sync.Mutex // Protects result.Mappings and result.Skipped

	// Start download workers in goroutines
	for _, ltFile := range mediaFiles {
		wg.Add(1)
		go func(file LTFile) {
			defer wg.Done()

			// Acquire semaphore slot (limits concurrent downloads to 12)
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check if context is cancelled
			if ctx.Err() != nil {
				resultsChan <- mediaDownloadResult{ltFile: file, err: ctx.Err(), errType: "download"}
				return
			}

			// Download file via HTTP from LibreTime API
			l.logger.Debug().
				Int("file_id", file.ID).
				Str("title", file.Title).
				Msg("downloading file via HTTP")

			reader, _, err := client.DownloadFile(ctx, file.ID)
			if err != nil {
				resultsChan <- mediaDownloadResult{ltFile: file, err: err, errType: "download"}
				return
			}
			defer reader.Close()

			// Read into buffer and compute hash simultaneously
			var buf bytes.Buffer
			hasher := sha256.New()
			teeReader := io.TeeReader(reader, hasher)
			if _, err := io.Copy(&buf, teeReader); err != nil {
				resultsChan <- mediaDownloadResult{ltFile: file, err: err, errType: "read"}
				return
			}

			contentHash := hex.EncodeToString(hasher.Sum(nil))
			resultsChan <- mediaDownloadResult{
				ltFile:      file,
				data:        buf.Bytes(),
				contentHash: contentHash,
			}
		}(ltFile)
	}

	// Close results channel when all downloads complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Process downloaded files as they complete
	for downloadResult := range resultsChan {
		current := atomic.AddInt32(&processedCount, 1)

		// Handle download errors
		if downloadResult.err != nil {
			l.logger.Error().
				Err(downloadResult.err).
				Str("title", downloadResult.ltFile.Title).
				Str("error_type", downloadResult.errType).
				Msg("failed to download media via HTTP")

			mu.Lock()
			if downloadResult.errType == "download" {
				result.Skipped["media_download_failed"]++
			} else {
				result.Skipped["media_read_failed"]++
			}
			mu.Unlock()

			// Update progress
			l.updateMediaProgress(progressCallback, int(current), len(mediaFiles), startTime)
			continue
		}

		ltFile := downloadResult.ltFile
		contentHash := downloadResult.contentHash
		data := downloadResult.data

		// Check for existing media with same hash (deduplication)
		var existingMedia models.MediaItem
		err := l.db.WithContext(ctx).Where("content_hash = ?", contentHash).First(&existingMedia).Error
		if err == nil {
			// Media already exists - create a link instead of re-uploading
			mediaItem := l.createMediaItemFromLTFile(ltFile, stationID, contentHash)
			mediaItem.StorageKey = existingMedia.StorageKey
			mediaItem.Path = existingMedia.Path

			if err := l.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
				l.logger.Error().Err(err).Str("title", ltFile.Title).Msg("failed to create linked media item")
				mu.Lock()
				result.Skipped["media_db_failed"]++
				mu.Unlock()

				l.updateMediaProgress(progressCallback, int(current), len(mediaFiles), startTime)
				continue
			}

			atomic.AddInt32(&deduplicatedCount, 1)

			mu.Lock()
			result.MediaItemsImported++
			result.Mappings[fmt.Sprintf("media_%d", ltFile.ID)] = Mapping{
				OldID: fmt.Sprintf("%d", ltFile.ID),
				NewID: mediaItem.ID,
				Type:  "media",
				Name:  fmt.Sprintf("%s (deduplicated)", mediaItem.Title),
			}
			mu.Unlock()

			l.logger.Debug().
				Str("title", ltFile.Title).
				Str("hash", contentHash[:12]).
				Str("existing_id", existingMedia.ID).
				Msg("deduplicated media file")

			l.updateMediaProgress(progressCallback, int(current), len(mediaFiles), startTime)
			continue
		}

		// New media - upload to storage
		mediaItem := l.createMediaItemFromLTFile(ltFile, stationID, contentHash)

		storageKey, err := l.mediaService.Store(ctx, stationID, mediaItem.ID, bytes.NewReader(data))
		if err != nil {
			l.logger.Error().Err(err).Str("title", ltFile.Title).Msg("failed to upload media to storage")
			mu.Lock()
			result.Skipped["media_upload_failed"]++
			mu.Unlock()

			l.updateMediaProgress(progressCallback, int(current), len(mediaFiles), startTime)
			continue
		}

		mediaItem.StorageKey = storageKey
		mediaItem.Path = l.mediaService.URL(storageKey)

		if err := l.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
			l.logger.Error().Err(err).Str("title", ltFile.Title).Msg("failed to create media item in database")
			mu.Lock()
			result.Skipped["media_db_failed"]++
			mu.Unlock()

			l.updateMediaProgress(progressCallback, int(current), len(mediaFiles), startTime)
			continue
		}

		mu.Lock()
		result.MediaItemsImported++
		result.Mappings[fmt.Sprintf("media_%d", ltFile.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltFile.ID),
			NewID: mediaItem.ID,
			Type:  "media",
			Name:  mediaItem.Title,
		}
		mu.Unlock()

		l.logger.Debug().
			Str("title", ltFile.Title).
			Str("storage_key", storageKey).
			Msg("media file imported successfully")

		l.updateMediaProgress(progressCallback, int(current), len(mediaFiles), startTime)
	}

	finalDedup := int(atomic.LoadInt32(&deduplicatedCount))
	if finalDedup > 0 {
		l.logger.Info().Int("count", finalDedup).Msg("deduplicated media files (linked to existing)")
		result.Skipped["media_deduplicated"] = finalDedup
	}

	l.logger.Info().
		Int("total", len(mediaFiles)).
		Int("imported", result.MediaItemsImported).
		Int("deduplicated", finalDedup).
		Msg("concurrent media import complete")

	return nil
}

// updateMediaProgress updates the progress callback with current media import status.
func (l *LibreTimeImporter) updateMediaProgress(progressCallback ProgressCallback, current, total int, startTime time.Time) {
	eta := calculateETA(startTime, current, total)
	progressCallback(Progress{
		Phase:              "importing_media",
		CurrentStep:        fmt.Sprintf("Downloading & importing media via HTTP: %d/%d", current, total),
		TotalSteps:         5,
		CompletedSteps:     2,
		Percentage:         20 + (float64(current)/float64(total))*40,
		MediaTotal:         total,
		MediaImported:      current,
		StartTime:          startTime,
		EstimatedRemaining: eta,
	})
}

// createMediaItemFromLTFile creates a MediaItem from a LibreTime file.
func (l *LibreTimeImporter) createMediaItemFromLTFile(ltFile LTFile, stationID, contentHash string) *models.MediaItem {
	origFilename := filepath.Base(ltFile.Filepath)
	if origFilename == "" || origFilename == "." {
		origFilename = ltFile.Name
	}

	mediaItem := &models.MediaItem{
		ID:               uuid.New().String(),
		StationID:        stationID,
		Title:            ltFile.Title,
		Artist:           ltFile.Artist,
		Album:            ltFile.Album,
		Genre:            ltFile.Genre,
		ImportPath:       ltFile.Filepath,
		OriginalFilename: origFilename,
		ContentHash:      contentHash,
	}

	if ltFile.Title == "" {
		mediaItem.Title = filepath.Base(ltFile.Name)
	}

	// Date field contains the year as a string
	if ltFile.Date != "" {
		mediaItem.Year = ltFile.Date
	}

	if ltFile.TrackNumber != nil {
		mediaItem.TrackNumber = *ltFile.TrackNumber
	}

	// Parse duration from "HH:MM:SS.mmm" format
	if ltFile.Length != "" {
		if duration, err := parseDuration(ltFile.Length); err == nil {
			mediaItem.Duration = duration
		}
	}

	if ltFile.Bitrate != nil {
		mediaItem.Bitrate = *ltFile.Bitrate
	}

	if ltFile.Samplerate != nil {
		mediaItem.Samplerate = *ltFile.Samplerate
	}

	// Set cue points if available (API returns duration strings)
	if ltFile.CueIn != nil || ltFile.CueOut != nil {
		cuePoints := models.CuePointSet{}
		if ltFile.CueIn != nil {
			if dur, err := parseDuration(*ltFile.CueIn); err == nil {
				cuePoints.IntroEnd = dur.Seconds()
			}
		}
		if ltFile.CueOut != nil {
			if dur, err := parseDuration(*ltFile.CueOut); err == nil {
				cuePoints.OutroIn = dur.Seconds()
			}
		}
		mediaItem.CuePoints = cuePoints
	}

	return mediaItem
}

// importPlaylistsFromAPI imports playlists from LibreTime API.
func (l *LibreTimeImporter) importPlaylistsFromAPI(ctx context.Context, client *LibreTimeAPIClient, stationID string, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	playlists, err := client.GetPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("get playlists: %w", err)
	}

	l.logger.Info().Int("count", len(playlists)).Msg("importing playlists via API")

	// Track skipped items for reporting
	var skippedMediaItems int

	for i, ltPlaylist := range playlists {
		// Create Grimnir playlist
		playlist := &models.Playlist{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        ltPlaylist.Name,
			Description: ltPlaylist.Description,
		}

		if err := l.db.WithContext(ctx).Create(playlist).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_playlist_id", ltPlaylist.ID).Msg("failed to create playlist")
			continue
		}

		// Get and import playlist contents
		contents, err := client.GetPlaylistContents(ctx, ltPlaylist.ID)
		if err != nil {
			l.logger.Warn().Err(err).Int("playlist_id", ltPlaylist.ID).Msg("failed to get playlist contents")
		} else {
			for _, content := range contents {
				if content.FileID == nil {
					continue // Skip non-file items
				}

				// Find mapped media item
				mediaKey := fmt.Sprintf("media_%d", *content.FileID)
				mediaMapping, ok := result.Mappings[mediaKey]
				if !ok {
					// Media wasn't imported (likely skip_media was enabled)
					skippedMediaItems++
					continue
				}

				playlistItem := &models.PlaylistItem{
					ID:         uuid.New().String(),
					PlaylistID: playlist.ID,
					MediaID:    mediaMapping.NewID,
					Position:   content.Position,
				}

				// Parse fade durations from string format (e.g., "00:00:02.500")
				if content.FadeIn != nil {
					if dur, err := parseDuration(*content.FadeIn); err == nil {
						playlistItem.FadeIn = int(dur.Seconds() * 1000) // Convert to milliseconds
					}
				}

				if content.FadeOut != nil {
					if dur, err := parseDuration(*content.FadeOut); err == nil {
						playlistItem.FadeOut = int(dur.Seconds() * 1000)
					}
				}

				if err := l.db.WithContext(ctx).Create(playlistItem).Error; err != nil {
					l.logger.Warn().Err(err).Str("media_id", mediaMapping.NewID).Msg("failed to create playlist item")
				}
			}
		}

		result.Mappings[fmt.Sprintf("playlist_%d", ltPlaylist.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltPlaylist.ID),
			NewID: playlist.ID,
			Type:  "playlist",
			Name:  playlist.Name,
		}

		result.PlaylistsCreated++

		// Update progress
		progressCallback(Progress{
			Phase:             "importing_playlists",
			CurrentStep:       fmt.Sprintf("Imported playlist: %s", playlist.Name),
			TotalSteps:        5,
			CompletedSteps:    3,
			Percentage:        60 + (float64(i+1)/float64(len(playlists)))*20,
			PlaylistsTotal:    len(playlists),
			PlaylistsImported: i + 1,
			StartTime:         startTime,
		})
	}

	if skippedMediaItems > 0 {
		l.logger.Warn().
			Int("skipped_items", skippedMediaItems).
			Msg("playlist items skipped (media not imported)")
		result.Skipped["playlist_items_no_media"] = skippedMediaItems
	}

	l.logger.Info().Int("count", result.PlaylistsCreated).Msg("playlist import complete")
	return nil
}

// importShowsFromAPI imports shows from LibreTime API as clocks.
func (l *LibreTimeImporter) importShowsFromAPI(ctx context.Context, client *LibreTimeAPIClient, stationID string, result *Result) error {
	shows, err := client.GetShows(ctx)
	if err != nil {
		return fmt.Errorf("get shows: %w", err)
	}

	l.logger.Info().Int("count", len(shows)).Msg("importing shows as clocks via API")

	slotsCreated := 0
	for _, ltShow := range shows {
		// Create Grimnir clock hour (shows become hour templates)
		clock := &models.ClockHour{
			ID:        uuid.New().String(),
			StationID: stationID,
			Name:      ltShow.Name,
		}

		if err := l.db.WithContext(ctx).Create(clock).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_show_id", ltShow.ID).Msg("failed to create clock")
			continue
		}

		// If show has an auto_playlist, create a slot referencing the imported playlist
		if ltShow.HasAutoplaylist && ltShow.AutoplaylistID != nil {
			playlistKey := fmt.Sprintf("playlist_%d", *ltShow.AutoplaylistID)
			if playlistMapping, ok := result.Mappings[playlistKey]; ok {
				slot := &models.ClockSlot{
					ID:          uuid.New().String(),
					ClockHourID: clock.ID,
					Position:    0,
					Offset:      0, // Start at beginning of hour
					Type:        models.SlotTypePlaylist,
					Payload: map[string]any{
						"playlist_id": playlistMapping.NewID,
					},
				}
				if err := l.db.WithContext(ctx).Create(slot).Error; err != nil {
					l.logger.Warn().Err(err).
						Int("lt_show_id", ltShow.ID).
						Int("lt_playlist_id", *ltShow.AutoplaylistID).
						Msg("failed to create clock slot")
				} else {
					slotsCreated++
					l.logger.Debug().
						Str("clock", clock.Name).
						Str("playlist", playlistMapping.Name).
						Msg("created playlist slot for clock")
				}
			} else {
				l.logger.Debug().
					Int("lt_show_id", ltShow.ID).
					Int("lt_playlist_id", *ltShow.AutoplaylistID).
					Msg("show has auto_playlist but playlist not found in mappings")
			}
		}

		result.Mappings[fmt.Sprintf("show_%d", ltShow.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltShow.ID),
			NewID: clock.ID,
			Type:  "clock",
			Name:  clock.Name,
		}

		result.SchedulesCreated++
	}

	if result.SchedulesCreated > 0 {
		msg := fmt.Sprintf("Imported %d LibreTime shows as clocks", result.SchedulesCreated)
		if slotsCreated > 0 {
			msg += fmt.Sprintf(" (%d with playlist slots)", slotsCreated)
		}
		msg += ". Show schedules must be manually recreated using the clock templates."
		result.Warnings = append(result.Warnings, msg)
	}

	l.logger.Info().
		Int("clocks", result.SchedulesCreated).
		Int("slots", slotsCreated).
		Msg("show import complete")
	return nil
}

// importWebstreamsFromAPI imports webstreams (remote streams) from LibreTime API.
func (l *LibreTimeImporter) importWebstreamsFromAPI(ctx context.Context, client *LibreTimeAPIClient, stationID string, result *Result) error {
	webstreams, err := client.GetWebstreams(ctx)
	if err != nil {
		// Webstreams endpoint might not exist in older LibreTime versions
		l.logger.Warn().Err(err).Msg("failed to get webstreams (may not be available)")
		return nil
	}

	if len(webstreams) == 0 {
		l.logger.Info().Msg("no webstreams to import")
		return nil
	}

	l.logger.Info().Int("count", len(webstreams)).Msg("importing webstreams via API")

	imported := 0
	for _, ltWebstream := range webstreams {
		// Create Grimnir webstream
		webstream := &models.Webstream{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        ltWebstream.Name,
			Description: ltWebstream.Description,
			URLs:        []string{ltWebstream.URL},
			Active:      true,
		}

		if err := l.db.WithContext(ctx).Create(webstream).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_webstream_id", ltWebstream.ID).Msg("failed to create webstream")
			continue
		}

		result.Mappings[fmt.Sprintf("webstream_%d", ltWebstream.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltWebstream.ID),
			NewID: webstream.ID,
			Type:  "webstream",
			Name:  webstream.Name,
		}

		imported++
	}

	l.logger.Info().Int("count", imported).Msg("webstream import complete")
	return nil
}

// importSmartBlocksFromAPI imports smart blocks (dynamic playlists) from LibreTime API.
func (l *LibreTimeImporter) importSmartBlocksFromAPI(ctx context.Context, client *LibreTimeAPIClient, stationID string, result *Result) error {
	blocks, err := client.GetSmartBlocks(ctx)
	if err != nil {
		// Smart blocks endpoint might not exist in older LibreTime versions
		l.logger.Warn().Err(err).Msg("failed to get smart blocks (may not be available)")
		return nil
	}

	if len(blocks) == 0 {
		l.logger.Info().Msg("no smart blocks to import")
		return nil
	}

	l.logger.Info().Int("count", len(blocks)).Msg("importing smart blocks via API")

	imported := 0
	for _, ltBlock := range blocks {
		// Build rules from LibreTime criteria
		rules := make(map[string]any)
		sequence := make(map[string]any)

		// Fetch criteria for this block
		criteria, err := client.GetSmartBlockCriteria(ctx, ltBlock.ID)
		if err == nil && len(criteria) > 0 {
			// Convert LibreTime criteria to Grimnir rules format
			criteriaList := make([]map[string]string, 0, len(criteria))
			for _, c := range criteria {
				criteriaList = append(criteriaList, map[string]string{
					"field":    c.Criteria,
					"operator": c.Condition, // API uses "condition" not "modifier"
					"value":    c.Value,
				})
			}
			rules["criteria"] = criteriaList
		}

		// Set sequence options with defaults
		// Note: LibreTime API v2 doesn't expose limit/sort settings, using defaults
		sequence["order"] = "random"
		sequence["limit"] = 10
		sequence["repeat_tracks"] = false

		// Create Grimnir smart block
		smartBlock := &models.SmartBlock{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        ltBlock.Name,
			Description: ltBlock.Description,
			Rules:       rules,
			Sequence:    sequence,
		}

		if err := l.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_block_id", ltBlock.ID).Msg("failed to create smart block")
			continue
		}

		result.Mappings[fmt.Sprintf("smartblock_%d", ltBlock.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltBlock.ID),
			NewID: smartBlock.ID,
			Type:  "smartblock",
			Name:  smartBlock.Name,
		}

		imported++
	}

	l.logger.Info().Int("count", imported).Msg("smart block import complete")
	return nil
}

// importDatabase performs the actual migration from database.
func (l *LibreTimeImporter) importDatabase(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error) {
	startTime := time.Now()
	l.logger.Info().Str("db_host", options.LibreTimeDBHost).Msg("starting LibreTime import")

	// Phase 1: Connect to LibreTime
	progressCallback(Progress{
		Phase:          "connecting",
		CurrentStep:    "Connecting to LibreTime database",
		TotalSteps:     5,
		CompletedSteps: 0,
		Percentage:     0,
		StartTime:      startTime,
	})

	ltDB, err := l.connectLibreTime(options)
	if err != nil {
		return nil, fmt.Errorf("connect to LibreTime: %w", err)
	}
	defer func() {
		sqlDB, _ := ltDB.DB()
		sqlDB.Close()
	}()

	result := &Result{
		Warnings: []string{},
		Skipped:  make(map[string]int),
		Mappings: make(map[string]Mapping),
	}

	// Phase 2: Import station configuration
	progressCallback(Progress{
		Phase:          "importing_station",
		CurrentStep:    "Importing station configuration",
		TotalSteps:     5,
		CompletedSteps: 1,
		Percentage:     20,
		StartTime:      startTime,
	})

	if err := l.importStation(ctx, ltDB, result); err != nil {
		return nil, fmt.Errorf("import station: %w", err)
	}

	// Phase 3: Import media library
	if !options.SkipMedia {
		progressCallback(Progress{
			Phase:          "importing_media",
			CurrentStep:    "Importing media library",
			TotalSteps:     5,
			CompletedSteps: 2,
			Percentage:     40,
			StartTime:      startTime,
		})

		if err := l.importMedia(ctx, ltDB, options, result, progressCallback, startTime); err != nil {
			return nil, fmt.Errorf("import media: %w", err)
		}
	} else {
		var mediaCount int64
		ltDB.Raw("SELECT COUNT(*) FROM cc_files WHERE file_exists = TRUE").Scan(&mediaCount)
		result.Skipped["media"] = int(mediaCount)
	}

	// Phase 4: Import playlists
	if !options.SkipPlaylists {
		progressCallback(Progress{
			Phase:          "importing_playlists",
			CurrentStep:    "Importing playlists",
			TotalSteps:     5,
			CompletedSteps: 3,
			Percentage:     60,
			StartTime:      startTime,
		})

		if err := l.importPlaylists(ctx, ltDB, result, progressCallback, startTime); err != nil {
			return nil, fmt.Errorf("import playlists: %w", err)
		}
	} else {
		var playlistCount int64
		ltDB.Raw("SELECT COUNT(*) FROM cc_playlist").Scan(&playlistCount)
		result.Skipped["playlists"] = int(playlistCount)
	}

	// Phase 5: Import shows and schedules
	if !options.SkipSchedules {
		progressCallback(Progress{
			Phase:          "importing_schedules",
			CurrentStep:    "Importing shows and schedules",
			TotalSteps:     5,
			CompletedSteps: 4,
			Percentage:     80,
			StartTime:      startTime,
		})

		if err := l.importShows(ctx, ltDB, result); err != nil {
			return nil, fmt.Errorf("import shows: %w", err)
		}
	} else {
		var showCount int64
		ltDB.Raw("SELECT COUNT(*) FROM cc_show").Scan(&showCount)
		result.Skipped["schedules"] = int(showCount)
	}

	// Complete
	progressCallback(Progress{
		Phase:             "completed",
		CurrentStep:       "Migration completed",
		TotalSteps:        5,
		CompletedSteps:    5,
		Percentage:        100,
		StationsImported:  result.StationsCreated,
		MediaImported:     result.MediaItemsImported,
		PlaylistsImported: result.PlaylistsCreated,
		SchedulesImported: result.SchedulesCreated,
		StartTime:         startTime,
	})

	result.DurationSeconds = time.Since(startTime).Seconds()

	l.logger.Info().
		Int("stations", result.StationsCreated).
		Int("media", result.MediaItemsImported).
		Int("playlists", result.PlaylistsCreated).
		Int("schedules", result.SchedulesCreated).
		Float64("duration", result.DurationSeconds).
		Msg("LibreTime import completed")

	return result, nil
}

// connectLibreTime establishes a connection to the LibreTime database.
func (l *LibreTimeImporter) connectLibreTime(options Options) (*gorm.DB, error) {
	port := options.LibreTimeDBPort
	if port == 0 {
		port = 5432
	}

	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		options.LibreTimeDBHost,
		port,
		options.LibreTimeDBName,
		options.LibreTimeDBUser,
		options.LibreTimeDBPassword,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}

	// Test connection
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

// LibreTime database structures
type ltPref struct {
	Keystr string `gorm:"column:keystr"`
	Valstr string `gorm:"column:valstr"`
}

type ltFile struct {
	ID           int            `gorm:"column:id;primaryKey"`
	Name         string         `gorm:"column:name"`
	Filepath     string         `gorm:"column:filepath"`
	TrackTitle   sql.NullString `gorm:"column:track_title"`
	Artist       sql.NullString `gorm:"column:artist_name"`
	Album        sql.NullString `gorm:"column:album_title"`
	Length       sql.NullString `gorm:"column:length"`
	Bitrate      sql.NullInt64  `gorm:"column:bit_rate"`
	Samplerate   sql.NullInt64  `gorm:"column:sample_rate"`
	FileExists   bool           `gorm:"column:file_exists"`
	Hidden       bool           `gorm:"column:hidden"`
	Genre        sql.NullString `gorm:"column:genre"`
	Year         sql.NullString `gorm:"column:year"`
	TrackNumber  sql.NullInt64  `gorm:"column:track_number"`
	LastModified time.Time      `gorm:"column:mtime"`
}

type ltPlaylist struct {
	ID          int       `gorm:"column:id;primaryKey"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
	Length      string    `gorm:"column:length"`
	CreatedAt   time.Time `gorm:"column:ctime"`
}

type ltPlaylistContent struct {
	ID         int     `gorm:"column:id;primaryKey"`
	PlaylistID int     `gorm:"column:playlist_id"`
	FileID     int     `gorm:"column:file_id"`
	Position   int     `gorm:"column:position"`
	Offset     float64 `gorm:"column:offset"`
	FadeIn     float64 `gorm:"column:fadein"`
	FadeOut    float64 `gorm:"column:fadeout"`
}

type ltShow struct {
	ID          int            `gorm:"column:id;primaryKey"`
	Name        string         `gorm:"column:name"`
	Description sql.NullString `gorm:"column:description"`
	Duration    string         `gorm:"column:duration"`
	Color       sql.NullString `gorm:"column:color"`
}

// importStation imports the station configuration from LibreTime.
func (l *LibreTimeImporter) importStation(ctx context.Context, ltDB *gorm.DB, result *Result) error {
	// Query station name and description from preferences
	var prefs []ltPref
	ltDB.Table("cc_pref").Where("keystr IN ?", []string{"stationName", "stationDescription"}).Find(&prefs)

	stationName := "Imported Station"
	stationDesc := "Imported from LibreTime"

	for _, pref := range prefs {
		if pref.Keystr == "stationName" && pref.Valstr != "" {
			stationName = pref.Valstr
		}
		if pref.Keystr == "stationDescription" && pref.Valstr != "" {
			stationDesc = pref.Valstr
		}
	}

	// Create Grimnir station
	station := &models.Station{
		ID:          uuid.New().String(),
		Name:        stationName,
		Description: stationDesc,
		Timezone:    "UTC",
		Active:      true,
	}

	if err := l.db.WithContext(ctx).Create(station).Error; err != nil {
		return fmt.Errorf("create station: %w", err)
	}

	// Track mapping
	result.Mappings["station_main"] = Mapping{
		OldID: "libretime_station",
		NewID: station.ID,
		Type:  "station",
		Name:  station.Name,
	}

	result.StationsCreated++

	// Auto-generate default mount point
	mountName := models.GenerateMountName(stationName)
	mount := &models.Mount{
		ID:         uuid.New().String(),
		StationID:  station.ID,
		Name:       mountName,
		URL:        "/live/" + mountName,
		Format:     "mp3",
		Bitrate:    128,
		Channels:   2,
		SampleRate: 44100,
	}
	if err := l.db.WithContext(ctx).Create(mount).Error; err != nil {
		l.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to create default mount")
	} else {
		l.logger.Info().
			Str("station_id", station.ID).
			Str("mount", mountName).
			Msg("created default mount")
	}

	l.logger.Info().Str("station_id", station.ID).Str("name", stationName).Msg("station imported")
	return nil
}

// importMedia imports the media library from LibreTime.
func (l *LibreTimeImporter) importMedia(ctx context.Context, ltDB *gorm.DB, options Options, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	// Get station ID
	stationMapping, ok := result.Mappings["station_main"]
	if !ok {
		return fmt.Errorf("station not found in mappings")
	}
	stationID := stationMapping.NewID

	// Query all media files
	var ltFiles []ltFile
	if err := ltDB.Table("cc_files").Where("file_exists = ?", true).Where("hidden = ?", false).Find(&ltFiles).Error; err != nil {
		return fmt.Errorf("query media files: %w", err)
	}

	totalFiles := len(ltFiles)
	l.logger.Info().Int("count", totalFiles).Msg("importing media files")

	// Validate media path exists
	if options.LibreTimeMediaPath != "" {
		if err := ValidateSourceDirectory(options.LibreTimeMediaPath); err != nil {
			l.logger.Warn().Err(err).Str("media_path", options.LibreTimeMediaPath).Msg("media path validation failed")
			result.Warnings = append(result.Warnings, fmt.Sprintf("Media path not accessible: %s - metadata imported but files not copied", options.LibreTimeMediaPath))
			// Continue with metadata-only import
		}
	}

	// Create file operations handler
	fileOps := NewFileOperations(l.mediaService, l.logger)

	// Prepare media items and copy jobs
	var copyJobs []FileCopyJob
	mediaItemsByID := make(map[string]*models.MediaItem)
	mediaPathAvailable := options.LibreTimeMediaPath != ""

	for i, ltFile := range ltFiles {
		// Create Grimnir media item
		origFilename := filepath.Base(ltFile.Filepath)
		if origFilename == "" || origFilename == "." {
			origFilename = ltFile.Name
		}
		mediaItem := &models.MediaItem{
			ID:               uuid.New().String(),
			StationID:        stationID,
			Title:            ltFile.TrackTitle.String,
			Artist:           ltFile.Artist.String,
			Album:            ltFile.Album.String,
			Genre:            ltFile.Genre.String,
			ImportPath:       ltFile.Filepath,
			OriginalFilename: origFilename,
		}

		if ltFile.TrackTitle.String == "" {
			// Use filename if no title
			mediaItem.Title = filepath.Base(ltFile.Name)
		}

		if ltFile.Year.Valid {
			mediaItem.Year = ltFile.Year.String
		}

		if ltFile.TrackNumber.Valid {
			mediaItem.TrackNumber = int(ltFile.TrackNumber.Int64)
		}

		if ltFile.Length.Valid {
			// Parse duration (format: HH:MM:SS.mmm)
			if duration, err := parseDuration(ltFile.Length.String); err == nil {
				mediaItem.Duration = duration
			}
		}

		if ltFile.Bitrate.Valid {
			mediaItem.Bitrate = int(ltFile.Bitrate.Int64)
		}

		if ltFile.Samplerate.Valid {
			mediaItem.Samplerate = int(ltFile.Samplerate.Int64)
		}

		// Create in database
		if err := l.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_file_id", ltFile.ID).Msg("failed to create media item")
			continue
		}

		mediaItemsByID[mediaItem.ID] = mediaItem

		// Track mapping
		result.Mappings[fmt.Sprintf("media_%d", ltFile.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltFile.ID),
			NewID: mediaItem.ID,
			Type:  "media",
			Name:  mediaItem.Title,
		}

		// If media path is available, prepare file copy job
		if mediaPathAvailable && ltFile.Filepath != "" {
			sourcePath := ResolveFilePath(options.LibreTimeMediaPath, ltFile.Filepath)

			// Check if source file exists
			if fileInfo, err := os.Stat(sourcePath); err == nil && !fileInfo.IsDir() {
				copyJobs = append(copyJobs, FileCopyJob{
					SourcePath: sourcePath,
					StationID:  stationID,
					MediaID:    mediaItem.ID,
					FileSize:   fileInfo.Size(),
				})
			} else {
				l.logger.Warn().Str("path", sourcePath).Msg("source file not found, skipping file copy")
			}
		}

		result.MediaItemsImported++

		// Update progress every 10 files (metadata phase)
		if i%10 == 0 || i == totalFiles-1 {
			progressCallback(Progress{
				Phase:          "importing_media_metadata",
				CurrentStep:    fmt.Sprintf("Imported metadata %d/%d", i+1, totalFiles),
				TotalSteps:     5,
				CompletedSteps: 2,
				Percentage:     40 + (float64(i+1)/float64(totalFiles))*10,
				MediaTotal:     totalFiles,
				MediaImported:  i + 1,
				StartTime:      startTime,
			})
		}
	}

	// Copy files if media path was provided
	if len(copyJobs) > 0 {
		l.logger.Info().Int("files_to_copy", len(copyJobs)).Msg("starting file copy phase")

		copyOptions := DefaultCopyOptions()
		copyOptions.Concurrency = 4
		copyOptions.ProgressCallback = func(copied, total int) {
			percentage := 50 + (float64(copied)/float64(total))*10
			progressCallback(Progress{
				Phase:          "copying_media_files",
				CurrentStep:    fmt.Sprintf("Copying files: %d/%d", copied, total),
				TotalSteps:     5,
				CompletedSteps: 2,
				Percentage:     percentage,
				MediaTotal:     total,
				MediaCopied:    copied,
				StartTime:      startTime,
			})
		}

		copyResults, err := fileOps.CopyFiles(ctx, copyJobs, copyOptions)
		if err != nil {
			l.logger.Error().Err(err).Msg("file copy phase failed")
			result.Warnings = append(result.Warnings, fmt.Sprintf("File copy failed: %v", err))
		} else {
			// Update MediaItem records with storage keys
			successCount := 0
			failCount := 0

			for _, copyResult := range copyResults {
				if copyResult.Success {
					mediaItem := mediaItemsByID[copyResult.MediaID]
					if mediaItem != nil {
						// Update with storage key
						mediaItem.StorageKey = copyResult.StorageKey
						mediaItem.Path = l.mediaService.URL(copyResult.StorageKey)

						if err := l.db.WithContext(ctx).Save(mediaItem).Error; err != nil {
							l.logger.Error().Err(err).Str("media_id", copyResult.MediaID).Msg("failed to update media item with storage key")
							failCount++
							continue
						}

						successCount++
					}
				} else {
					l.logger.Warn().Err(copyResult.Error).Str("media_id", copyResult.MediaID).Msg("failed to copy media file")
					failCount++
				}
			}

			l.logger.Info().
				Int("success", successCount).
				Int("failed", failCount).
				Int("skipped", len(ltFiles)-len(copyJobs)).
				Msg("file copy phase complete")

			if failCount > 0 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%d media files failed to copy", failCount))
			}

			if len(ltFiles)-len(copyJobs) > 0 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%d media files skipped (source not found)", len(ltFiles)-len(copyJobs)))
			}
		}
	} else if mediaPathAvailable {
		result.Warnings = append(result.Warnings, "No media files found at specified path - metadata imported without files")
	} else {
		result.Warnings = append(result.Warnings, "Media path not specified - metadata imported without files")
	}

	l.logger.Info().Int("count", result.MediaItemsImported).Msg("media import complete")
	return nil
}

// importPlaylists imports playlists from LibreTime.
func (l *LibreTimeImporter) importPlaylists(ctx context.Context, ltDB *gorm.DB, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	// Get station ID
	stationMapping, ok := result.Mappings["station_main"]
	if !ok {
		return fmt.Errorf("station not found in mappings")
	}
	stationID := stationMapping.NewID

	// Query all playlists
	var ltPlaylists []ltPlaylist
	if err := ltDB.Table("cc_playlist").Find(&ltPlaylists).Error; err != nil {
		return fmt.Errorf("query playlists: %w", err)
	}

	totalPlaylists := len(ltPlaylists)
	l.logger.Info().Int("count", totalPlaylists).Msg("importing playlists")

	for i, ltPlaylist := range ltPlaylists {
		// Create Grimnir playlist
		playlist := &models.Playlist{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        ltPlaylist.Name,
			Description: ltPlaylist.Description,
		}

		if err := l.db.WithContext(ctx).Create(playlist).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_playlist_id", ltPlaylist.ID).Msg("failed to create playlist")
			continue
		}

		// Query playlist contents
		var contents []ltPlaylistContent
		ltDB.Table("cc_playlistcontents").Where("playlist_id = ?", ltPlaylist.ID).Order("position").Find(&contents)

		// Import playlist items
		for _, content := range contents {
			// Find mapped media item
			mediaKey := fmt.Sprintf("media_%d", content.FileID)
			mediaMapping, ok := result.Mappings[mediaKey]
			if !ok {
				l.logger.Warn().Int("file_id", content.FileID).Msg("media item not found in mappings")
				continue
			}

			playlistItem := &models.PlaylistItem{
				ID:         uuid.New().String(),
				PlaylistID: playlist.ID,
				MediaID:    mediaMapping.NewID,
				Position:   content.Position,
			}

			if content.FadeIn > 0 {
				playlistItem.FadeIn = int(content.FadeIn * 1000) // Convert to milliseconds
			}

			if content.FadeOut > 0 {
				playlistItem.FadeOut = int(content.FadeOut * 1000)
			}

			if err := l.db.WithContext(ctx).Create(playlistItem).Error; err != nil {
				l.logger.Warn().Err(err).Str("media_id", mediaMapping.NewID).Msg("failed to create playlist item")
			}
		}

		// Track mapping
		result.Mappings[fmt.Sprintf("playlist_%d", ltPlaylist.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltPlaylist.ID),
			NewID: playlist.ID,
			Type:  "playlist",
			Name:  playlist.Name,
		}

		result.PlaylistsCreated++

		// Update progress
		progressCallback(Progress{
			Phase:             "importing_playlists",
			CurrentStep:       fmt.Sprintf("Imported playlist: %s", playlist.Name),
			TotalSteps:        5,
			CompletedSteps:    3,
			Percentage:        60 + (float64(i+1)/float64(totalPlaylists))*20,
			PlaylistsTotal:    totalPlaylists,
			PlaylistsImported: i + 1,
			StartTime:         startTime,
		})
	}

	l.logger.Info().Int("count", result.PlaylistsCreated).Msg("playlist import complete")
	return nil
}

// importShows imports shows from LibreTime (converted to clocks).
func (l *LibreTimeImporter) importShows(ctx context.Context, ltDB *gorm.DB, result *Result) error {
	// Get station ID
	stationMapping, ok := result.Mappings["station_main"]
	if !ok {
		return fmt.Errorf("station not found in mappings")
	}
	stationID := stationMapping.NewID

	// Query all shows
	var ltShows []ltShow
	if err := ltDB.Table("cc_show").Find(&ltShows).Error; err != nil {
		return fmt.Errorf("query shows: %w", err)
	}

	l.logger.Info().Int("count", len(ltShows)).Msg("importing shows as clocks")

	for _, ltShow := range ltShows {
		// Create Grimnir clock hour (shows become hour templates)
		clock := &models.ClockHour{
			ID:        uuid.New().String(),
			StationID: stationID,
			Name:      ltShow.Name,
		}

		if err := l.db.WithContext(ctx).Create(clock).Error; err != nil {
			l.logger.Warn().Err(err).Int("lt_show_id", ltShow.ID).Msg("failed to create clock")
			continue
		}

		// Track mapping
		result.Mappings[fmt.Sprintf("show_%d", ltShow.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", ltShow.ID),
			NewID: clock.ID,
			Type:  "clock",
			Name:  clock.Name,
		}

		result.SchedulesCreated++
	}

	l.logger.Info().Int("count", result.SchedulesCreated).Msg("show import complete")

	// Add warning about schedule conversion
	if result.SchedulesCreated > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Imported %d LibreTime shows as clocks. Show schedules must be manually recreated using the clock templates.", result.SchedulesCreated))
	}

	return nil
}

// parseDuration parses LibreTime duration strings (HH:MM:SS or HH:MM:SS.mmm).
func parseDuration(durationStr string) (time.Duration, error) {
	// Remove milliseconds if present
	parts := strings.Split(durationStr, ".")
	timeStr := parts[0]

	// Parse HH:MM:SS
	var hours, minutes, seconds int
	_, err := fmt.Sscanf(timeStr, "%d:%d:%d", &hours, &minutes, &seconds)
	if err != nil {
		return 0, err
	}

	totalSeconds := hours*3600 + minutes*60 + seconds
	return time.Duration(totalSeconds) * time.Second, nil
}

func (ltFile) TableName() string {
	return "cc_files"
}

func (ltPlaylist) TableName() string {
	return "cc_playlist"
}

func (ltPlaylistContent) TableName() string {
	return "cc_playlistcontents"
}

func (ltShow) TableName() string {
	return "cc_show"
}

func (ltPref) TableName() string {
	return "cc_pref"
}

// =============================================================================
// STAGED IMPORT SUPPORT
// =============================================================================

// AnalyzeForStaging performs a deep analysis and creates a StagedImport for review.
func (l *LibreTimeImporter) AnalyzeForStaging(ctx context.Context, jobID string, options Options) (*models.StagedImport, error) {
	l.logger.Info().Str("job_id", jobID).Msg("analyzing LibreTime for staged import")

	// Create staged import record
	analyzer := NewStagedAnalyzer(l.db, l.logger)
	staged, err := analyzer.CreateStagedImport(ctx, jobID, string(SourceTypeLibreTime))
	if err != nil {
		return nil, fmt.Errorf("create staged import: %w", err)
	}

	// Use API mode if configured
	if isLibreTimeAPIMode(options) {
		return l.analyzeForStagingAPI(ctx, staged, options, analyzer)
	}

	return l.analyzeForStagingDB(ctx, staged, options, analyzer)
}

// analyzeForStagingAPI performs staged analysis via API.
func (l *LibreTimeImporter) analyzeForStagingAPI(ctx context.Context, staged *models.StagedImport, options Options, analyzer *StagedAnalyzer) (*models.StagedImport, error) {
	client, err := NewLibreTimeAPIClient(options.LibreTimeAPIURL, options.LibreTimeAPIKey)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	// Get and stage media files
	if !options.SkipMedia {
		files, err := client.GetFiles(ctx)
		if err != nil {
			l.logger.Warn().Err(err).Msg("failed to get files")
		} else {
			for _, f := range files {
				if f.Hidden || !f.FileExists {
					continue
				}

				durationMs := 0
				if f.Length != "" {
					if dur, err := parseDuration(f.Length); err == nil {
						durationMs = int(dur.Milliseconds())
					}
				}

				staged.StagedMedia = append(staged.StagedMedia, models.StagedMediaItem{
					SourceID:   fmt.Sprintf("%d", f.ID),
					Title:      f.Title,
					Artist:     f.Artist,
					Album:      f.Album,
					Genre:      f.Genre,
					DurationMs: durationMs,
					FilePath:   f.Filepath,
					FileSize:   f.Size,
					Selected:   true,
				})
			}
		}
	}

	// Get and stage playlists
	if !options.SkipPlaylists {
		playlists, err := client.GetPlaylists(ctx)
		if err != nil {
			l.logger.Warn().Err(err).Msg("failed to get playlists")
		} else {
			for _, pl := range playlists {
				contents, _ := client.GetPlaylistContents(ctx, pl.ID)
				var itemIDs []string
				for _, c := range contents {
					if c.FileID != nil {
						itemIDs = append(itemIDs, fmt.Sprintf("%d", *c.FileID))
					}
				}

				staged.StagedPlaylists = append(staged.StagedPlaylists, models.StagedPlaylistItem{
					SourceID:    fmt.Sprintf("%d", pl.ID),
					Name:        pl.Name,
					Description: pl.Description,
					ItemCount:   len(contents),
					Duration:    pl.Length,
					ItemIDs:     itemIDs,
					Selected:    true,
				})
			}
		}
	}

	// Get and stage smart blocks
	if !options.SkipSmartblocks {
		blocks, err := client.GetSmartBlocks(ctx)
		if err != nil {
			l.logger.Warn().Err(err).Msg("failed to get smart blocks")
		} else {
			for _, block := range blocks {
				criteria, _ := client.GetSmartBlockCriteria(ctx, block.ID)
				criteriaSummary := ""
				if len(criteria) > 0 {
					criteriaSummary = fmt.Sprintf("%d criteria", len(criteria))
				}

				staged.StagedSmartBlocks = append(staged.StagedSmartBlocks, models.StagedSmartBlockItem{
					SourceID:        fmt.Sprintf("%d", block.ID),
					Name:            block.Name,
					Description:     block.Description,
					CriteriaCount:   len(criteria),
					CriteriaSummary: criteriaSummary,
					Selected:        true,
				})
			}
		}
	}

	// Get and stage shows with recurrence detection
	if !options.SkipSchedules {
		shows, err := client.GetShows(ctx)
		if err != nil {
			l.logger.Warn().Err(err).Msg("failed to get shows")
		} else {
			for _, show := range shows {
				// Get show instances for recurrence detection
				instances, _ := client.GetShowInstances(ctx, show.ID)

				var showInstances []ShowInstance
				for _, inst := range instances {
					showInstances = append(showInstances, ShowInstance{
						SourceID: fmt.Sprintf("%d", inst.ID),
						ShowID:   fmt.Sprintf("%d", show.ID),
						ShowName: show.Name,
						StartsAt: inst.Starts,
						EndsAt:   inst.Ends,
						Timezone: show.Timezone,
					})
				}

				// Detect recurrence pattern
				stagedShow := models.StagedShowItem{
					SourceID:        fmt.Sprintf("%d", show.ID),
					Name:            show.Name,
					Description:     show.Description,
					Genre:           show.Genre,
					InstanceCount:   len(instances),
					DurationMinutes: 60, // Default
					Timezone:        show.Timezone,
					Selected:        true,
				}

				if recurrence := analyzer.DetectRecurrence(showInstances); recurrence != nil {
					stagedShow.DetectedRRule = recurrence.RRule
					stagedShow.PatternConfidence = recurrence.Confidence
					stagedShow.PatternNote = recurrence.Pattern
					stagedShow.DTStart = recurrence.DTStart
					stagedShow.DurationMinutes = recurrence.DurationMinutes
					stagedShow.ExceptionCount = recurrence.ExceptionCount

					if recurrence.Timezone != "" {
						stagedShow.Timezone = recurrence.Timezone
					}

					// High confidence = create as Show, low = create as Clock
					if recurrence.Confidence >= 0.75 {
						stagedShow.CreateShow = true
					} else {
						stagedShow.CreateClock = true
					}
				} else {
					// No pattern detected, create as Clock
					stagedShow.CreateClock = true
					if len(instances) > 0 {
						stagedShow.DTStart = instances[0].Starts
						duration := instances[0].Ends.Sub(instances[0].Starts)
						stagedShow.DurationMinutes = int(duration.Minutes())
					}
				}

				staged.StagedShows = append(staged.StagedShows, stagedShow)
			}
		}
	}

	// Get and stage webstreams
	if !options.SkipWebstreams {
		webstreams, err := client.GetWebstreams(ctx)
		if err != nil {
			l.logger.Warn().Err(err).Msg("failed to get webstreams")
		} else {
			for _, ws := range webstreams {
				staged.StagedWebstreams = append(staged.StagedWebstreams, models.StagedWebstreamItem{
					SourceID:    fmt.Sprintf("%d", ws.ID),
					Name:        ws.Name,
					Description: ws.Description,
					URL:         ws.URL,
					Selected:    true,
				})
			}
		}
	}

	// Detect duplicates
	staged.StagedMedia = analyzer.DetectDuplicates(ctx, staged.StagedMedia, options.TargetStationID)

	// Check for orphan matches (files on disk but not in DB)
	if l.orphanScanner != nil {
		staged.StagedMedia = l.matchOrphans(ctx, staged.StagedMedia)
	}

	// Apply default selections
	analyzer.ApplyDefaultSelections(staged)

	// Generate warnings and suggestions
	analyzer.GenerateWarnings(staged)
	analyzer.GenerateSuggestions(staged)

	// Mark as ready for review
	now := time.Now()
	staged.AnalyzedAt = &now
	staged.Status = models.StagedImportStatusReady

	if err := analyzer.UpdateStagedImport(ctx, staged); err != nil {
		return nil, fmt.Errorf("update staged import: %w", err)
	}

	l.logger.Info().
		Int("media", len(staged.StagedMedia)).
		Int("playlists", len(staged.StagedPlaylists)).
		Int("smartblocks", len(staged.StagedSmartBlocks)).
		Int("shows", len(staged.StagedShows)).
		Int("webstreams", len(staged.StagedWebstreams)).
		Int("orphan_matches", staged.OrphanMatchCount()).
		Msg("staged import analysis complete")

	return staged, nil
}

// analyzeForStagingDB performs staged analysis via direct database access.
func (l *LibreTimeImporter) analyzeForStagingDB(ctx context.Context, staged *models.StagedImport, options Options, analyzer *StagedAnalyzer) (*models.StagedImport, error) {
	// Connect to LibreTime database
	ltDB, err := l.connectLibreTime(options)
	if err != nil {
		return nil, fmt.Errorf("connect to LibreTime: %w", err)
	}
	defer func() {
		sqlDB, _ := ltDB.DB()
		sqlDB.Close()
	}()

	// Get and stage media files
	if !options.SkipMedia {
		var ltFiles []ltFile
		if err := ltDB.Table("cc_files").Where("file_exists = ?", true).Where("hidden = ?", false).Find(&ltFiles).Error; err != nil {
			l.logger.Warn().Err(err).Msg("failed to query media files")
		} else {
			for _, f := range ltFiles {
				durationMs := 0
				if f.Length.Valid {
					if dur, err := parseDuration(f.Length.String); err == nil {
						durationMs = int(dur.Milliseconds())
					}
				}

				title := f.TrackTitle.String
				if title == "" {
					title = filepath.Base(f.Name)
				}

				staged.StagedMedia = append(staged.StagedMedia, models.StagedMediaItem{
					SourceID:   fmt.Sprintf("%d", f.ID),
					Title:      title,
					Artist:     f.Artist.String,
					Album:      f.Album.String,
					Genre:      f.Genre.String,
					DurationMs: durationMs,
					FilePath:   f.Filepath,
					Selected:   true,
				})
			}
		}
	}

	// Get and stage playlists
	if !options.SkipPlaylists {
		var ltPlaylists []ltPlaylist
		if err := ltDB.Table("cc_playlist").Find(&ltPlaylists).Error; err != nil {
			l.logger.Warn().Err(err).Msg("failed to query playlists")
		} else {
			for _, pl := range ltPlaylists {
				var contents []ltPlaylistContent
				ltDB.Table("cc_playlistcontents").Where("playlist_id = ?", pl.ID).Find(&contents)

				var itemIDs []string
				for _, c := range contents {
					itemIDs = append(itemIDs, fmt.Sprintf("%d", c.FileID))
				}

				staged.StagedPlaylists = append(staged.StagedPlaylists, models.StagedPlaylistItem{
					SourceID:    fmt.Sprintf("%d", pl.ID),
					Name:        pl.Name,
					Description: pl.Description,
					ItemCount:   len(contents),
					Duration:    pl.Length,
					ItemIDs:     itemIDs,
					Selected:    true,
				})
			}
		}
	}

	// Get and stage shows with recurrence detection
	if !options.SkipSchedules {
		var ltShows []ltShow
		if err := ltDB.Table("cc_show").Find(&ltShows).Error; err != nil {
			l.logger.Warn().Err(err).Msg("failed to query shows")
		} else {
			for _, show := range ltShows {
				// Query show instances
				var instances []struct {
					ID       int       `gorm:"column:id"`
					ShowID   int       `gorm:"column:show_id"`
					Starts   time.Time `gorm:"column:starts"`
					Ends     time.Time `gorm:"column:ends"`
					Timezone string    `gorm:"column:timezone"`
				}
				ltDB.Table("cc_show_instances").Where("show_id = ?", show.ID).Find(&instances)

				var showInstances []ShowInstance
				for _, inst := range instances {
					showInstances = append(showInstances, ShowInstance{
						SourceID: fmt.Sprintf("%d", inst.ID),
						ShowID:   fmt.Sprintf("%d", show.ID),
						ShowName: show.Name,
						StartsAt: inst.Starts,
						EndsAt:   inst.Ends,
						Timezone: inst.Timezone,
					})
				}

				durationMinutes := 60
				if show.Duration != "" {
					if dur, err := parseDuration(show.Duration); err == nil {
						durationMinutes = int(dur.Minutes())
					}
				}

				stagedShow := models.StagedShowItem{
					SourceID:        fmt.Sprintf("%d", show.ID),
					Name:            show.Name,
					Description:     show.Description.String,
					Color:           show.Color.String,
					InstanceCount:   len(instances),
					DurationMinutes: durationMinutes,
					Selected:        true,
				}

				if recurrence := analyzer.DetectRecurrence(showInstances); recurrence != nil {
					stagedShow.DetectedRRule = recurrence.RRule
					stagedShow.PatternConfidence = recurrence.Confidence
					stagedShow.PatternNote = recurrence.Pattern
					stagedShow.DTStart = recurrence.DTStart
					stagedShow.DurationMinutes = recurrence.DurationMinutes
					stagedShow.Timezone = recurrence.Timezone
					stagedShow.ExceptionCount = recurrence.ExceptionCount

					if recurrence.Confidence >= 0.75 {
						stagedShow.CreateShow = true
					} else {
						stagedShow.CreateClock = true
					}
				} else {
					stagedShow.CreateClock = true
					if len(instances) > 0 {
						stagedShow.DTStart = instances[0].Starts
						stagedShow.Timezone = instances[0].Timezone
					}
				}

				staged.StagedShows = append(staged.StagedShows, stagedShow)
			}
		}
	}

	// Detect duplicates
	staged.StagedMedia = analyzer.DetectDuplicates(ctx, staged.StagedMedia, options.TargetStationID)

	// Check for orphan matches (files on disk but not in DB)
	if l.orphanScanner != nil {
		staged.StagedMedia = l.matchOrphans(ctx, staged.StagedMedia)
	}

	// Apply default selections
	analyzer.ApplyDefaultSelections(staged)

	// Generate warnings and suggestions
	analyzer.GenerateWarnings(staged)
	analyzer.GenerateSuggestions(staged)

	// Mark as ready for review
	now := time.Now()
	staged.AnalyzedAt = &now
	staged.Status = models.StagedImportStatusReady

	if err := analyzer.UpdateStagedImport(ctx, staged); err != nil {
		return nil, fmt.Errorf("update staged import: %w", err)
	}

	l.logger.Info().
		Int("media", len(staged.StagedMedia)).
		Int("playlists", len(staged.StagedPlaylists)).
		Int("shows", len(staged.StagedShows)).
		Int("orphan_matches", staged.OrphanMatchCount()).
		Msg("staged import analysis complete (database mode)")

	return staged, nil
}

// matchOrphans checks staged media items against the orphan pool and marks matches.
// Orphan matching is done by content hash - if a staged item's hash matches an orphan,
// we can adopt the orphan instead of downloading the file again.
func (l *LibreTimeImporter) matchOrphans(ctx context.Context, stagedMedia models.StagedMediaItems) models.StagedMediaItems {
	if l.orphanScanner == nil {
		return stagedMedia
	}

	// Build hash map of all orphans for efficient lookup
	orphanMap, err := l.orphanScanner.BuildOrphanHashMap(ctx)
	if err != nil {
		l.logger.Warn().Err(err).Msg("failed to build orphan hash map, skipping orphan matching")
		return stagedMedia
	}

	if len(orphanMap) == 0 {
		return stagedMedia
	}

	matchCount := 0
	for i := range stagedMedia {
		// Skip items that are already duplicates (they exist in DB)
		if stagedMedia[i].IsDuplicate {
			continue
		}

		// If the staged item has a content hash, check for orphan match
		if stagedMedia[i].ContentHash != "" {
			if orphan, found := orphanMap[stagedMedia[i].ContentHash]; found {
				stagedMedia[i].OrphanMatch = true
				stagedMedia[i].OrphanID = orphan.ID
				stagedMedia[i].OrphanPath = orphan.FilePath
				matchCount++

				l.logger.Debug().
					Str("source_id", stagedMedia[i].SourceID).
					Str("title", stagedMedia[i].Title).
					Str("orphan_path", orphan.FilePath).
					Msg("matched staged media to orphan")
			}
		}
	}

	if matchCount > 0 {
		l.logger.Info().
			Int("orphan_matches", matchCount).
			Msg("found orphan matches for staged media")
	}

	return stagedMedia
}

// CommitStagedImport imports only selected items from a staged import with provenance tracking.
func (l *LibreTimeImporter) CommitStagedImport(ctx context.Context, staged *models.StagedImport, jobID string, options Options, cb ProgressCallback) (*Result, error) {
	startTime := time.Now()
	l.logger.Info().
		Str("job_id", jobID).
		Str("staged_id", staged.ID).
		Int("selected", staged.SelectedCount()).
		Msg("committing staged import")

	result := &Result{
		Warnings: []string{},
		Skipped:  make(map[string]int),
		Mappings: make(map[string]Mapping),
	}

	// Track all imported items for the job
	importedItems := &ImportedItems{}

	// Determine or create station
	var stationID string
	if options.TargetStationID != "" {
		stationID = options.TargetStationID
	} else {
		// Create new station
		station := &models.Station{
			ID:          uuid.New().String(),
			Name:        "Imported from LibreTime",
			Description: "Station imported from LibreTime",
			Timezone:    "UTC",
			Active:      true,
			Public:      false,
			Approved:    true,
		}

		if options.ImportingUserID != "" {
			station.OwnerID = options.ImportingUserID
		}

		if err := l.db.WithContext(ctx).Create(station).Error; err != nil {
			return nil, fmt.Errorf("create station: %w", err)
		}

		// Create station-user association
		if options.ImportingUserID != "" {
			stationUser := &models.StationUser{
				ID:        uuid.New().String(),
				UserID:    options.ImportingUserID,
				StationID: station.ID,
				Role:      models.StationRoleOwner,
			}
			l.db.WithContext(ctx).Create(stationUser)
		}

		stationID = station.ID
		result.StationsCreated++
	}

	// Import selected media
	mediaMapping := make(map[string]string) // source ID -> new ID
	selectedMediaCount := 0
	for _, m := range staged.StagedMedia {
		if m.Selected {
			selectedMediaCount++
		}
	}

	if selectedMediaCount > 0 && isLibreTimeAPIMode(options) {
		client, _ := NewLibreTimeAPIClient(options.LibreTimeAPIURL, options.LibreTimeAPIKey)

		importedCount := 0
		adoptedCount := 0
		for _, m := range staged.StagedMedia {
			if !m.Selected {
				result.Skipped["media_deselected"]++
				continue
			}

			// Check if this item matches an orphan (existing file on disk)
			if m.OrphanMatch && m.OrphanID != "" && l.orphanScanner != nil {
				orphan, err := l.orphanScanner.GetOrphanByID(ctx, m.OrphanID)
				if err == nil && orphan != nil {
					// Adopt the orphan instead of downloading
					mediaItem, err := l.orphanScanner.AdoptOrphanForImport(ctx, orphan, stationID, jobID, m.SourceID)
					if err != nil {
						l.logger.Warn().Err(err).Str("orphan_id", m.OrphanID).Msg("failed to adopt orphan")
						// Fall through to download
					} else {
						// Update with metadata from source
						mediaItem.Title = m.Title
						mediaItem.Artist = m.Artist
						mediaItem.Album = m.Album
						mediaItem.Genre = m.Genre
						mediaItem.ImportPath = m.FilePath
						if m.DurationMs > 0 {
							mediaItem.Duration = time.Duration(m.DurationMs) * time.Millisecond
						}
						if err := l.db.WithContext(ctx).Save(mediaItem).Error; err != nil {
							l.logger.Warn().Err(err).Str("media_id", mediaItem.ID).Msg("failed to update adopted media metadata")
						}

						mediaMapping[m.SourceID] = mediaItem.ID
						importedItems.MediaIDs = append(importedItems.MediaIDs, mediaItem.ID)
						result.MediaItemsImported++
						importedCount++
						adoptedCount++

						l.logger.Info().
							Str("title", m.Title).
							Str("orphan_path", m.OrphanPath).
							Msg("adopted orphan file instead of downloading")

						// Progress callback
						cb(Progress{
							Phase:         "importing_media",
							CurrentStep:   fmt.Sprintf("Adopting existing file: %d/%d", importedCount, selectedMediaCount),
							MediaTotal:    selectedMediaCount,
							MediaImported: importedCount,
							Percentage:    float64(importedCount) / float64(selectedMediaCount) * 40,
							StartTime:     startTime,
						})
						continue
					}
				}
			}

			// Download and import the media file
			sourceID, _ := parseInt(m.SourceID)
			reader, _, err := client.DownloadFile(ctx, sourceID)
			if err != nil {
				l.logger.Warn().Err(err).Str("source_id", m.SourceID).Msg("failed to download media")
				result.Skipped["media_download_failed"]++
				continue
			}

			// Read into buffer
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, reader); err != nil {
				reader.Close()
				result.Skipped["media_read_failed"]++
				continue
			}
			reader.Close()

			// Create media item with provenance
			mediaItem := &models.MediaItem{
				ID:               uuid.New().String(),
				StationID:        stationID,
				Title:            m.Title,
				Artist:           m.Artist,
				Album:            m.Album,
				Genre:            m.Genre,
				ImportPath:       m.FilePath,
				OriginalFilename: filepath.Base(m.FilePath),
				ImportJobID:      &jobID,
				ImportSource:     string(SourceTypeLibreTime),
				ImportSourceID:   m.SourceID,
			}

			if m.DurationMs > 0 {
				mediaItem.Duration = time.Duration(m.DurationMs) * time.Millisecond
			}

			// Store media file
			storageKey, err := l.mediaService.Store(ctx, stationID, mediaItem.ID, bytes.NewReader(buf.Bytes()))
			if err != nil {
				l.logger.Warn().Err(err).Str("title", m.Title).Msg("failed to store media")
				result.Skipped["media_storage_failed"]++
				continue
			}

			mediaItem.StorageKey = storageKey
			mediaItem.Path = l.mediaService.URL(storageKey)

			if err := l.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
				l.logger.Warn().Err(err).Str("title", m.Title).Msg("failed to create media item")
				result.Skipped["media_db_failed"]++
				continue
			}

			mediaMapping[m.SourceID] = mediaItem.ID
			importedItems.MediaIDs = append(importedItems.MediaIDs, mediaItem.ID)
			result.MediaItemsImported++
			importedCount++

			// Progress callback
			cb(Progress{
				Phase:         "importing_media",
				CurrentStep:   fmt.Sprintf("Importing media: %d/%d", importedCount, selectedMediaCount),
				MediaTotal:    selectedMediaCount,
				MediaImported: importedCount,
				Percentage:    float64(importedCount) / float64(selectedMediaCount) * 40,
				StartTime:     startTime,
			})
		}

		if adoptedCount > 0 {
			l.logger.Info().
				Int("adopted", adoptedCount).
				Int("downloaded", importedCount-adoptedCount).
				Msg("media import complete with orphan adoption")
			result.Skipped["media_adopted_from_orphans"] = adoptedCount
		}
	}

	// Import selected playlists
	for _, pl := range staged.StagedPlaylists {
		if !pl.Selected {
			result.Skipped["playlist_deselected"]++
			continue
		}

		playlist := &models.Playlist{
			ID:             uuid.New().String(),
			StationID:      stationID,
			Name:           pl.Name,
			Description:    pl.Description,
			ImportJobID:    &jobID,
			ImportSource:   string(SourceTypeLibreTime),
			ImportSourceID: pl.SourceID,
		}

		if err := l.db.WithContext(ctx).Create(playlist).Error; err != nil {
			l.logger.Warn().Err(err).Str("name", pl.Name).Msg("failed to create playlist")
			continue
		}

		// Create playlist items
		for i, itemSourceID := range pl.ItemIDs {
			if newMediaID, ok := mediaMapping[itemSourceID]; ok {
				playlistItem := &models.PlaylistItem{
					ID:         uuid.New().String(),
					PlaylistID: playlist.ID,
					MediaID:    newMediaID,
					Position:   i,
				}
				l.db.WithContext(ctx).Create(playlistItem)
			}
		}

		importedItems.PlaylistIDs = append(importedItems.PlaylistIDs, playlist.ID)
		result.PlaylistsCreated++
	}

	// Import selected smart blocks
	for _, sb := range staged.StagedSmartBlocks {
		if !sb.Selected {
			result.Skipped["smartblock_deselected"]++
			continue
		}

		smartBlock := &models.SmartBlock{
			ID:             uuid.New().String(),
			StationID:      stationID,
			Name:           sb.Name,
			Description:    sb.Description,
			Rules:          sb.RawCriteria,
			ImportJobID:    &jobID,
			ImportSource:   string(SourceTypeLibreTime),
			ImportSourceID: sb.SourceID,
		}

		if err := l.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
			l.logger.Warn().Err(err).Str("name", sb.Name).Msg("failed to create smart block")
			continue
		}

		importedItems.SmartBlockIDs = append(importedItems.SmartBlockIDs, smartBlock.ID)
	}

	// Import selected shows (as Show with RRULE or as Clock)
	for _, sh := range staged.StagedShows {
		if !sh.Selected {
			result.Skipped["show_deselected"]++
			continue
		}

		// Use custom RRULE if provided, otherwise detected
		rrule := sh.CustomRRule
		if rrule == "" {
			rrule = sh.DetectedRRule
		}

		if sh.CreateShow && rrule != "" {
			// Create as Show with RRULE
			show := &models.Show{
				ID:                     uuid.New().String(),
				StationID:              stationID,
				Name:                   sh.Name,
				Description:            sh.Description,
				DefaultDurationMinutes: sh.DurationMinutes,
				Color:                  sh.Color,
				RRule:                  rrule,
				DTStart:                sh.DTStart,
				Timezone:               sh.Timezone,
				Active:                 true,
				ImportJobID:            &jobID,
				ImportSource:           string(SourceTypeLibreTime),
				ImportSourceID:         sh.SourceID,
			}

			if show.Timezone == "" {
				show.Timezone = "UTC"
			}

			if err := l.db.WithContext(ctx).Create(show).Error; err != nil {
				l.logger.Warn().Err(err).Str("name", sh.Name).Msg("failed to create show")
				continue
			}

			importedItems.ShowIDs = append(importedItems.ShowIDs, show.ID)
			result.SchedulesCreated++
		} else {
			// Create as ClockHour (template only)
			clock := &models.ClockHour{
				ID:        uuid.New().String(),
				StationID: stationID,
				Name:      sh.Name,
			}

			if err := l.db.WithContext(ctx).Create(clock).Error; err != nil {
				l.logger.Warn().Err(err).Str("name", sh.Name).Msg("failed to create clock")
				continue
			}

			importedItems.ClockIDs = append(importedItems.ClockIDs, clock.ID)
			result.SchedulesCreated++
		}
	}

	// Import selected webstreams
	for _, ws := range staged.StagedWebstreams {
		if !ws.Selected {
			result.Skipped["webstream_deselected"]++
			continue
		}

		webstream := &models.Webstream{
			ID:             uuid.New().String(),
			StationID:      stationID,
			Name:           ws.Name,
			Description:    ws.Description,
			URLs:           []string{ws.URL},
			Active:         true,
			ImportJobID:    &jobID,
			ImportSource:   string(SourceTypeLibreTime),
			ImportSourceID: ws.SourceID,
		}

		if err := l.db.WithContext(ctx).Create(webstream).Error; err != nil {
			l.logger.Warn().Err(err).Str("name", ws.Name).Msg("failed to create webstream")
			continue
		}

		importedItems.WebstreamIDs = append(importedItems.WebstreamIDs, webstream.ID)
	}

	// Update staged import status
	now := time.Now()
	staged.Status = models.StagedImportStatusCommitted
	staged.CommittedAt = &now
	l.db.WithContext(ctx).Save(staged)

	// Store imported items on the job
	var job Job
	if err := l.db.WithContext(ctx).First(&job, "id = ?", jobID).Error; err == nil {
		job.ImportedItems = importedItems
		l.db.WithContext(ctx).Save(&job)
	}

	result.DurationSeconds = time.Since(startTime).Seconds()

	l.logger.Info().
		Int("media", result.MediaItemsImported).
		Int("playlists", result.PlaylistsCreated).
		Int("schedules", result.SchedulesCreated).
		Float64("duration", result.DurationSeconds).
		Msg("staged import committed")

	return result, nil
}

// parseInt parses a string to int, returning 0 on error.
func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}
