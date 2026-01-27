/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package migration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// isAPIMode returns true if we should use API import instead of backup import.
func isAPIMode(options Options) bool {
	hasAPIKey := options.AzuraCastAPIURL != "" && options.AzuraCastAPIKey != ""
	hasCredentials := options.AzuraCastAPIURL != "" && options.AzuraCastUsername != "" && options.AzuraCastPassword != ""
	return hasAPIKey || hasCredentials
}

// createAPIClient creates an AzuraCast API client using either API key or credentials.
func createAPIClient(options Options) (*AzuraCastAPIClient, error) {
	if options.AzuraCastAPIKey != "" {
		return NewAzuraCastAPIClient(options.AzuraCastAPIURL, options.AzuraCastAPIKey)
	}
	return NewAzuraCastAPIClientWithCredentials(options.AzuraCastAPIURL, options.AzuraCastUsername, options.AzuraCastPassword)
}

// calculateETA calculates the estimated time remaining based on progress.
func calculateETA(startTime time.Time, completed, total int) string {
	if completed == 0 || total == 0 {
		return "calculating..."
	}

	elapsed := time.Since(startTime)
	avgTimePerItem := elapsed / time.Duration(completed)
	remaining := total - completed
	eta := avgTimePerItem * time.Duration(remaining)

	// Format nicely
	if eta < time.Minute {
		return fmt.Sprintf("%ds remaining", int(eta.Seconds()))
	} else if eta < time.Hour {
		mins := int(eta.Minutes())
		secs := int(eta.Seconds()) % 60
		return fmt.Sprintf("%dm %ds remaining", mins, secs)
	} else {
		hours := int(eta.Hours())
		mins := int(eta.Minutes()) % 60
		return fmt.Sprintf("%dh %dm remaining", hours, mins)
	}
}

// AnalysisReport provides detailed dry-run analysis results.
type AnalysisReport struct {
	// Summary counts
	TotalStations  int `json:"total_stations"`
	TotalMedia     int `json:"total_media"`
	TotalPlaylists int `json:"total_playlists"`
	TotalSchedules int `json:"total_schedules"`
	TotalStreamers int `json:"total_streamers"`

	// Detailed breakdown per station
	Stations []StationAnalysis `json:"stations"`

	// Estimated storage requirements
	EstimatedStorageBytes int64  `json:"estimated_storage_bytes"`
	EstimatedStorageHuman string `json:"estimated_storage_human"`

	// Potential issues
	Warnings []string `json:"warnings"`
}

// StationAnalysis provides detailed analysis for a single station.
type StationAnalysis struct {
	ID          int               `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	MediaCount  int               `json:"media_count"`
	Playlists   []PlaylistSummary `json:"playlists"`
	Mounts      []MountSummary    `json:"mounts"`
	Streamers   []StreamerSummary `json:"streamers"`
	StorageBytes int64            `json:"storage_bytes"`
}

// PlaylistSummary provides a summary of a playlist for reporting.
type PlaylistSummary struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Source   string `json:"source"`
	ItemCount int   `json:"item_count"`
}

// MountSummary provides a summary of a mount point for reporting.
type MountSummary struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Format  string `json:"format"`
	Bitrate int    `json:"bitrate"`
}

// StreamerSummary provides a summary of a streamer/DJ for reporting.
type StreamerSummary struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// formatBytes converts bytes to human-readable format.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// Validate checks if the migration can proceed.
func (a *AzuraCastImporter) Validate(ctx context.Context, options Options) error {
	var errors ValidationErrors

	// Check if we have either backup path or API credentials
	hasBackup := options.AzuraCastBackupPath != ""
	hasAPIKey := options.AzuraCastAPIURL != "" && options.AzuraCastAPIKey != ""
	hasCredentials := options.AzuraCastAPIURL != "" && options.AzuraCastUsername != "" && options.AzuraCastPassword != ""
	hasAPI := hasAPIKey || hasCredentials

	if !hasBackup && !hasAPI {
		errors = append(errors, ValidationError{
			Field:   "azuracast",
			Message: "either backup path, or API URL with API key, or API URL with username/password is required",
		})
	}

	if hasBackup && hasAPI {
		errors = append(errors, ValidationError{
			Field:   "azuracast",
			Message: "cannot specify both backup path and API credentials; choose one import method",
		})
	}

	// Validate backup mode
	if hasBackup {
		// Check if backup file exists
		if _, err := os.Stat(options.AzuraCastBackupPath); os.IsNotExist(err) {
			errors = append(errors, ValidationError{
				Field:   "azuracast_backup_path",
				Message: fmt.Sprintf("backup file does not exist: %s", options.AzuraCastBackupPath),
			})
		}

		// Check if it's a valid tar.gz file
		if !strings.HasSuffix(options.AzuraCastBackupPath, ".tar.gz") {
			errors = append(errors, ValidationError{
				Field:   "azuracast_backup_path",
				Message: "backup file must be a .tar.gz archive",
			})
		}
	}

	// Validate API mode
	if hasAPI {
		// Test API connection
		client, err := createAPIClient(options)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   "azuracast_api",
				Message: fmt.Sprintf("API authentication failed: %v", err),
			})
		} else {
			// Test connection
			_, err := client.TestConnection(ctx)
			if err != nil {
				errors = append(errors, ValidationError{
					Field:   "azuracast_api",
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
func (a *AzuraCastImporter) Analyze(ctx context.Context, options Options) (*Result, error) {
	if isAPIMode(options) {
		return a.analyzeAPI(ctx, options)
	}
	return a.analyzeBackup(ctx, options)
}

// analyzeAPI analyzes an AzuraCast instance via API.
func (a *AzuraCastImporter) analyzeAPI(ctx context.Context, options Options) (*Result, error) {
	report, err := a.AnalyzeDetailed(ctx, options)
	if err != nil {
		return nil, err
	}

	// Convert AnalysisReport to Result
	result := &Result{
		StationsCreated:    report.TotalStations,
		MediaItemsImported: report.TotalMedia,
		PlaylistsCreated:   report.TotalPlaylists,
		SchedulesCreated:   report.TotalSchedules,
		UsersCreated:       report.TotalStreamers,
		Warnings:           report.Warnings,
		Skipped:            make(map[string]int),
		Mappings:           make(map[string]Mapping),
	}

	return result, nil
}

// AnalyzeDetailed performs a detailed dry-run analysis and returns a full report.
func (a *AzuraCastImporter) AnalyzeDetailed(ctx context.Context, options Options) (*AnalysisReport, error) {
	a.logger.Info().Str("api_url", options.AzuraCastAPIURL).Msg("analyzing AzuraCast via API (detailed)")

	client, err := createAPIClient(options)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	// Get stations
	stations, err := client.GetStations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get stations: %w", err)
	}

	report := &AnalysisReport{
		TotalStations: len(stations),
		Stations:      make([]StationAnalysis, 0, len(stations)),
		Warnings:      []string{},
	}

	var totalStorageBytes int64

	// Analyze each station in detail
	for _, station := range stations {
		stationAnalysis := StationAnalysis{
			ID:          station.ID,
			Name:        station.Name,
			Description: station.Description,
			Playlists:   []PlaylistSummary{},
			Mounts:      []MountSummary{},
			Streamers:   []StreamerSummary{},
		}

		// Get media for this station
		media, err := client.GetMedia(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to get media")
			report.Warnings = append(report.Warnings, fmt.Sprintf("Could not fetch media for station %s: %v", station.Name, err))
		} else {
			stationAnalysis.MediaCount = len(media)
			report.TotalMedia += len(media)

			// Calculate storage estimate from media sizes or duration
			for _, m := range media {
				if m.Size > 0 {
					stationAnalysis.StorageBytes += m.Size
					totalStorageBytes += m.Size
				} else if m.Length > 0 {
					// Estimate size based on duration (assume 192kbps average bitrate)
					estimatedBytes := int64(m.Length * 192 * 1000 / 8) // 192kbps = 24KB/sec
					stationAnalysis.StorageBytes += estimatedBytes
					totalStorageBytes += estimatedBytes
				}
			}
		}

		// Get playlists for this station
		playlists, err := client.GetPlaylists(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to get playlists")
		} else {
			report.TotalPlaylists += len(playlists)
			for _, pl := range playlists {
				stationAnalysis.Playlists = append(stationAnalysis.Playlists, PlaylistSummary{
					ID:        pl.ID,
					Name:      pl.Name,
					Type:      pl.Type,
					Source:    pl.Source,
					ItemCount: pl.NumSongs,
				})
			}
		}

		// Get mounts for this station
		mounts, err := client.GetMounts(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to get mounts")
		} else {
			for _, mt := range mounts {
				stationAnalysis.Mounts = append(stationAnalysis.Mounts, MountSummary{
					ID:      mt.ID,
					Name:    mt.Name,
					Format:  mt.AutodjFormat,
					Bitrate: mt.AutodjBitrate,
				})
			}
		}

		// Get schedules for this station
		schedules, err := client.GetSchedules(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to get schedules")
		} else {
			report.TotalSchedules += len(schedules)
		}

		// Get streamers for this station
		streamers, err := client.GetStreamers(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to get streamers")
		} else {
			report.TotalStreamers += len(streamers)
			for _, st := range streamers {
				stationAnalysis.Streamers = append(stationAnalysis.Streamers, StreamerSummary{
					ID:          st.ID,
					Username:    st.StreamerUsername,
					DisplayName: st.DisplayName,
				})
			}
		}

		report.Stations = append(report.Stations, stationAnalysis)
	}

	// Set storage estimates
	report.EstimatedStorageBytes = totalStorageBytes
	report.EstimatedStorageHuman = formatBytes(totalStorageBytes)

	// Check for potential issues
	if len(stations) == 0 {
		report.Warnings = append(report.Warnings, "No stations found (check API key permissions)")
	}

	if report.TotalMedia == 0 {
		report.Warnings = append(report.Warnings, "No media files found")
	}

	// Note about deduplication
	if report.TotalStations > 1 && report.TotalMedia > 0 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Actual storage may be less due to deduplication across %d stations", report.TotalStations))
	}

	a.logger.Info().
		Int("stations", report.TotalStations).
		Int("media", report.TotalMedia).
		Int("playlists", report.TotalPlaylists).
		Int("streamers", report.TotalStreamers).
		Str("storage", report.EstimatedStorageHuman).
		Msg("detailed API analysis complete")

	return report, nil
}

// analyzeBackup analyzes an AzuraCast backup file.
func (a *AzuraCastImporter) analyzeBackup(ctx context.Context, options Options) (*Result, error) {
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
	if isAPIMode(options) {
		return a.importAPI(ctx, options, progressCallback)
	}
	return a.importBackup(ctx, options, progressCallback)
}

// importAPI imports from a live AzuraCast instance via API.
func (a *AzuraCastImporter) importAPI(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error) {
	startTime := time.Now()
	a.logger.Info().Str("api_url", options.AzuraCastAPIURL).Msg("starting AzuraCast API import")

	client, err := createAPIClient(options)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	result := &Result{
		Warnings: []string{},
		Skipped:  make(map[string]int),
		Mappings: make(map[string]Mapping),
	}

	// Phase 1: Get stations
	progressCallback(Progress{
		Phase:          "fetching",
		CurrentStep:    "Fetching stations from AzuraCast",
		TotalSteps:     5,
		CompletedSteps: 0,
		Percentage:     0,
		StartTime:      startTime,
	})

	stations, err := client.GetStations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get stations: %w", err)
	}

	if len(stations) == 0 {
		result.Warnings = append(result.Warnings, "No stations found (check API key permissions)")
		return result, nil
	}

	// Phase 2: Import stations
	progressCallback(Progress{
		Phase:          "importing_stations",
		CurrentStep:    "Importing stations",
		TotalSteps:     5,
		CompletedSteps: 1,
		Percentage:     20,
		StationsTotal:  len(stations),
		StartTime:      startTime,
	})

	stationMap := make(map[int]string) // AzuraCast ID -> Grimnir ID

	for i, azStation := range stations {
		station := &models.Station{
			ID:          uuid.New().String(),
			Name:        azStation.Name,
			Description: azStation.Description,
			Timezone:    "UTC",
			Active:      true,
		}

		if err := a.db.WithContext(ctx).Create(station).Error; err != nil {
			return nil, fmt.Errorf("create station: %w", err)
		}

		stationMap[azStation.ID] = station.ID
		result.StationsCreated++

		result.Mappings[fmt.Sprintf("station_%d", azStation.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", azStation.ID),
			NewID: station.ID,
			Type:  "station",
			Name:  station.Name,
		}

		progressCallback(Progress{
			Phase:            "importing_stations",
			CurrentStep:      fmt.Sprintf("Imported station: %s", station.Name),
			TotalSteps:       5,
			CompletedSteps:   1,
			Percentage:       20 + (float64(i+1)/float64(len(stations)))*10,
			StationsTotal:    len(stations),
			StationsImported: i + 1,
			StartTime:        startTime,
		})

		// Import mounts for this station
		mounts, err := client.GetMounts(ctx, azStation.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", azStation.ID).Msg("failed to get mounts")
		} else {
			for _, azMount := range mounts {
				mount := &models.Mount{
					ID:         uuid.New().String(),
					StationID:  station.ID,
					Name:       azMount.Name,
					URL:        "/listen/" + azMount.Name,
					Format:     azMount.AutodjFormat,
					Bitrate:    azMount.AutodjBitrate,
					Channels:   2,
					SampleRate: 44100,
				}

				if err := a.db.WithContext(ctx).Create(mount).Error; err != nil {
					a.logger.Error().Err(err).Str("mount", azMount.Name).Msg("failed to create mount")
				}
			}
		}
	}

	// Phase 3: Import media for each station
	if !options.SkipMedia {
		progressCallback(Progress{
			Phase:          "importing_media",
			CurrentStep:    "Importing media files",
			TotalSteps:     5,
			CompletedSteps: 2,
			Percentage:     30,
			StartTime:      startTime,
		})

		for azStationID, grimnirStationID := range stationMap {
			if err := a.importMediaFromAPI(ctx, client, azStationID, grimnirStationID, result, progressCallback, startTime); err != nil {
				a.logger.Error().Err(err).Int("station_id", azStationID).Msg("failed to import media")
				result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import media for station %d: %v", azStationID, err))
			}
		}
	}

	// Phase 4: Import playlists
	if !options.SkipPlaylists {
		progressCallback(Progress{
			Phase:          "importing_playlists",
			CurrentStep:    "Importing playlists",
			TotalSteps:     5,
			CompletedSteps: 3,
			Percentage:     70,
			StartTime:      startTime,
		})

		for azStationID, grimnirStationID := range stationMap {
			if err := a.importPlaylistsFromAPI(ctx, client, azStationID, grimnirStationID, result); err != nil {
				a.logger.Error().Err(err).Int("station_id", azStationID).Msg("failed to import playlists")
				result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import playlists for station %d: %v", azStationID, err))
			}
		}
	}

	// Phase 5: Import streamers as DJs
	if !options.SkipUsers {
		progressCallback(Progress{
			Phase:          "importing_users",
			CurrentStep:    "Importing streamers/DJs",
			TotalSteps:     5,
			CompletedSteps: 4,
			Percentage:     85,
			StartTime:      startTime,
		})

		for azStationID, grimnirStationID := range stationMap {
			if err := a.importStreamersFromAPI(ctx, client, azStationID, grimnirStationID, result); err != nil {
				a.logger.Error().Err(err).Int("station_id", azStationID).Msg("failed to import streamers")
				result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to import streamers for station %d: %v", azStationID, err))
			}
		}
	}

	// Complete
	progressCallback(Progress{
		Phase:             "completed",
		CurrentStep:       "Import completed",
		TotalSteps:        5,
		CompletedSteps:    5,
		Percentage:        100,
		StationsImported:  result.StationsCreated,
		MediaImported:     result.MediaItemsImported,
		PlaylistsImported: result.PlaylistsCreated,
		StartTime:         startTime,
	})

	result.DurationSeconds = time.Since(startTime).Seconds()

	a.logger.Info().
		Int("stations", result.StationsCreated).
		Int("media", result.MediaItemsImported).
		Int("playlists", result.PlaylistsCreated).
		Int("users", result.UsersCreated).
		Float64("duration", result.DurationSeconds).
		Msg("AzuraCast API import completed")

	return result, nil
}

// importMediaFromAPI imports media files from AzuraCast API.
func (a *AzuraCastImporter) importMediaFromAPI(ctx context.Context, client *AzuraCastAPIClient, azStationID int, grimnirStationID string, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	mediaList, err := client.GetMedia(ctx, azStationID)
	if err != nil {
		return fmt.Errorf("get media: %w", err)
	}

	if len(mediaList) == 0 {
		return nil
	}

	a.logger.Info().Int("count", len(mediaList)).Int("station_id", azStationID).Msg("importing media files")

	deduplicatedCount := 0

	for i, azMedia := range mediaList {
		// Download media file to buffer so we can compute hash
		reader, _, err := client.DownloadMedia(ctx, azStationID, azMedia.ID)
		if err != nil {
			a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to download media")
			result.Skipped["media_download_failed"]++
			continue
		}

		// Read into buffer and compute hash
		var buf bytes.Buffer
		hasher := sha256.New()
		teeReader := io.TeeReader(reader, hasher)
		if _, err := io.Copy(&buf, teeReader); err != nil {
			reader.Close()
			a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to read media")
			result.Skipped["media_read_failed"]++
			continue
		}
		reader.Close()

		contentHash := hex.EncodeToString(hasher.Sum(nil))

		// Check for existing media with same hash (deduplication across stations)
		var existingMedia models.MediaItem
		err = a.db.WithContext(ctx).Where("content_hash = ?", contentHash).First(&existingMedia).Error
		if err == nil {
			// Media already exists - create a link instead of re-uploading
			// We create a new MediaItem record that points to the same storage key
			mediaItem := &models.MediaItem{
				ID:          uuid.New().String(),
				StationID:   grimnirStationID,
				Title:       azMedia.Title,
				Artist:      azMedia.Artist,
				Album:       azMedia.Album,
				Genre:       azMedia.Genre,
				Duration:    time.Duration(azMedia.Length * float64(time.Second)),
				ImportPath:  azMedia.Path,
				ContentHash: contentHash,
				StorageKey:  existingMedia.StorageKey, // Reuse existing storage
				Path:        existingMedia.Path,
			}

			// Set cue points
			if azMedia.CueIn != nil || azMedia.CueOut != nil || azMedia.FadeIn != nil || azMedia.FadeOut != nil {
				cuePoints := models.CuePointSet{}
				if azMedia.CueIn != nil {
					cuePoints.IntroEnd = *azMedia.CueIn
				}
				if azMedia.CueOut != nil {
					cuePoints.OutroIn = *azMedia.CueOut
				}
				if azMedia.FadeIn != nil {
					cuePoints.FadeIn = *azMedia.FadeIn
				}
				if azMedia.FadeOut != nil {
					cuePoints.FadeOut = *azMedia.FadeOut
				}
				mediaItem.CuePoints = cuePoints
			}

			if err := a.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
				a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to create linked media item")
				result.Skipped["media_db_failed"]++
				continue
			}

			deduplicatedCount++
			result.MediaItemsImported++

			result.Mappings[fmt.Sprintf("media_%d", azMedia.ID)] = Mapping{
				OldID: fmt.Sprintf("%d", azMedia.ID),
				NewID: mediaItem.ID,
				Type:  "media",
				Name:  fmt.Sprintf("%s (deduplicated)", mediaItem.Title),
			}

			a.logger.Debug().
				Str("title", azMedia.Title).
				Str("hash", contentHash[:12]).
				Str("existing_id", existingMedia.ID).
				Msg("deduplicated media file")

			continue
		}

		// New media - upload to storage
		mediaItem := &models.MediaItem{
			ID:          uuid.New().String(),
			StationID:   grimnirStationID,
			Title:       azMedia.Title,
			Artist:      azMedia.Artist,
			Album:       azMedia.Album,
			Genre:       azMedia.Genre,
			Duration:    time.Duration(azMedia.Length * float64(time.Second)),
			ImportPath:  azMedia.Path,
			ContentHash: contentHash,
		}

		// Set cue points if available
		if azMedia.CueIn != nil || azMedia.CueOut != nil || azMedia.FadeIn != nil || azMedia.FadeOut != nil {
			cuePoints := models.CuePointSet{}
			if azMedia.CueIn != nil {
				cuePoints.IntroEnd = *azMedia.CueIn
			}
			if azMedia.CueOut != nil {
				cuePoints.OutroIn = *azMedia.CueOut
			}
			if azMedia.FadeIn != nil {
				cuePoints.FadeIn = *azMedia.FadeIn
			}
			if azMedia.FadeOut != nil {
				cuePoints.FadeOut = *azMedia.FadeOut
			}
			mediaItem.CuePoints = cuePoints
		}

		// Upload to media service from buffer
		storageKey, err := a.mediaService.Store(ctx, grimnirStationID, mediaItem.ID, bytes.NewReader(buf.Bytes()))
		if err != nil {
			a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to upload media")
			result.Skipped["media_upload_failed"]++
			continue
		}

		mediaItem.StorageKey = storageKey
		mediaItem.Path = a.mediaService.URL(storageKey)

		// Save to database
		if err := a.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
			a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to create media item")
			result.Skipped["media_db_failed"]++
			continue
		}

		result.MediaItemsImported++

		result.Mappings[fmt.Sprintf("media_%d", azMedia.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", azMedia.ID),
			NewID: mediaItem.ID,
			Type:  "media",
			Name:  mediaItem.Title,
		}

		// Update progress periodically
		if i%10 == 0 || i == len(mediaList)-1 {
			eta := calculateETA(startTime, i+1, len(mediaList))
			progressCallback(Progress{
				Phase:              "importing_media",
				CurrentStep:        fmt.Sprintf("Importing media: %d/%d", i+1, len(mediaList)),
				TotalSteps:         5,
				CompletedSteps:     2,
				Percentage:         30 + (float64(i+1)/float64(len(mediaList)))*40,
				MediaTotal:         len(mediaList),
				MediaImported:      i + 1,
				StartTime:          startTime,
				EstimatedRemaining: eta,
			})
		}
	}

	if deduplicatedCount > 0 {
		a.logger.Info().Int("count", deduplicatedCount).Msg("deduplicated media files (linked to existing)")
		result.Skipped["media_deduplicated"] = deduplicatedCount
	}

	return nil
}

// importPlaylistsFromAPI imports playlists from AzuraCast API.
func (a *AzuraCastImporter) importPlaylistsFromAPI(ctx context.Context, client *AzuraCastAPIClient, azStationID int, grimnirStationID string, result *Result) error {
	playlists, err := client.GetPlaylists(ctx, azStationID)
	if err != nil {
		return fmt.Errorf("get playlists: %w", err)
	}

	for _, azPlaylist := range playlists {
		// Convert to SmartBlock or Webstream based on source
		if azPlaylist.Source == "remote_url" && azPlaylist.RemoteURL != "" {
			// Create webstream
			webstream := &models.Webstream{
				ID:          uuid.New().String(),
				StationID:   grimnirStationID,
				Name:        azPlaylist.Name,
				Description: fmt.Sprintf("Imported from AzuraCast (Remote: %s)", azPlaylist.RemoteType),
				URLs:        []string{azPlaylist.RemoteURL},
				Active:      azPlaylist.IsEnabled,
			}

			if err := a.db.WithContext(ctx).Create(webstream).Error; err != nil {
				a.logger.Error().Err(err).Str("playlist", azPlaylist.Name).Msg("failed to create webstream")
				continue
			}
		} else {
			// Create smart block
			rules := make(map[string]any)
			sequence := make(map[string]any)

			switch azPlaylist.Order {
			case "shuffle", "random":
				sequence["order"] = "shuffle"
			case "sequential":
				sequence["order"] = "sequential"
			default:
				sequence["order"] = "shuffle"
			}
			sequence["limit"] = 10

			smartBlock := &models.SmartBlock{
				ID:          uuid.New().String(),
				StationID:   grimnirStationID,
				Name:        azPlaylist.Name,
				Description: fmt.Sprintf("Imported from AzuraCast (Type: %s, Weight: %d)", azPlaylist.Type, azPlaylist.Weight),
				Rules:       rules,
				Sequence:    sequence,
			}

			if err := a.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
				a.logger.Error().Err(err).Str("playlist", azPlaylist.Name).Msg("failed to create smart block")
				continue
			}
		}

		result.PlaylistsCreated++

		result.Mappings[fmt.Sprintf("playlist_%d", azPlaylist.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", azPlaylist.ID),
			NewID: uuid.New().String(),
			Type:  "playlist",
			Name:  azPlaylist.Name,
		}
	}

	return nil
}

// importStreamersFromAPI imports streamers/DJs from AzuraCast API.
func (a *AzuraCastImporter) importStreamersFromAPI(ctx context.Context, client *AzuraCastAPIClient, azStationID int, grimnirStationID string, result *Result) error {
	streamers, err := client.GetStreamers(ctx, azStationID)
	if err != nil {
		return fmt.Errorf("get streamers: %w", err)
	}

	for _, azStreamer := range streamers {
		// Check if user already exists (by email-like identifier)
		email := fmt.Sprintf("%s@imported.local", azStreamer.StreamerUsername)
		var existingUser models.User
		err := a.db.WithContext(ctx).Where("email = ?", email).First(&existingUser).Error

		var userID string
		if err == nil {
			// User exists - just create station association
			userID = existingUser.ID
			a.logger.Info().Str("email", email).Str("station_id", grimnirStationID).Msg("user already exists, adding station association")
		} else {
			// Create new user with DJ role
			user := &models.User{
				ID:       uuid.New().String(),
				Email:    email,
				Password: uuid.New().String(), // Random password, must be reset
				Role:     models.RoleDJ,
			}

			if err := a.db.WithContext(ctx).Create(user).Error; err != nil {
				a.logger.Error().Err(err).Str("streamer", azStreamer.DisplayName).Msg("failed to create user")
				continue
			}
			userID = user.ID
			result.UsersCreated++
		}

		// Create station-user association
		stationUser := &models.StationUser{
			ID:        uuid.New().String(),
			UserID:    userID,
			StationID: grimnirStationID,
			Role:      models.RoleDJ,
		}

		// Check if association already exists
		var existingAssoc models.StationUser
		err = a.db.WithContext(ctx).Where("user_id = ? AND station_id = ?", userID, grimnirStationID).First(&existingAssoc).Error
		if err != nil {
			// Association doesn't exist, create it
			if err := a.db.WithContext(ctx).Create(stationUser).Error; err != nil {
				a.logger.Error().Err(err).Str("user_id", userID).Str("station_id", grimnirStationID).Msg("failed to create station-user association")
			}
		}

		result.Mappings[fmt.Sprintf("streamer_%d", azStreamer.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", azStreamer.ID),
			NewID: userID,
			Type:  "user",
			Name:  azStreamer.DisplayName,
		}
	}

	if len(streamers) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Imported %d streamers as DJ users - passwords must be reset", len(streamers)))
	}

	return nil
}

// importBackup imports from an AzuraCast backup file (original implementation).
func (a *AzuraCastImporter) importBackup(ctx context.Context, options Options, progressCallback ProgressCallback) (*Result, error) {
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
