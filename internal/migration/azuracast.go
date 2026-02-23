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
	"sync"
	"sync/atomic"
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
	ID           int               `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	MediaCount   int               `json:"media_count"`
	Playlists    []PlaylistSummary `json:"playlists"`
	Mounts       []MountSummary    `json:"mounts"`
	Streamers    []StreamerSummary `json:"streamers"`
	StorageBytes int64             `json:"storage_bytes"`
}

// PlaylistSummary provides a summary of a playlist for reporting.
type PlaylistSummary struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Source    string `json:"source"`
	ItemCount int    `json:"item_count"`
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
		var station *models.Station
		var isExisting bool

		// Check if station with same name already exists - use it instead of creating duplicate
		var existingStation models.Station
		if err := a.db.WithContext(ctx).Where("name = ?", azStation.Name).First(&existingStation).Error; err == nil {
			// Station exists - use it
			station = &existingStation
			isExisting = true
			a.logger.Info().
				Str("station_id", station.ID).
				Str("name", station.Name).
				Msg("using existing station")
			result.Skipped["stations_existing"]++
		} else {
			// Create new station
			station = &models.Station{
				ID:          uuid.New().String(),
				Name:        azStation.Name,
				Description: azStation.Description,
				Shortcode:   azStation.ShortName,
				Timezone:    "UTC",
				Active:      true,
				Public:      azStation.IsPublic,
				Approved:    true,
				ListenURL:   azStation.ListenURL,
				Website:     azStation.URL,
			}

			// Fetch detailed station profile for additional metadata
			profile, err := client.GetStationProfile(ctx, azStation.ID)
			if err == nil && profile != nil {
				if profile.Genre != "" {
					station.Genre = profile.Genre
				}
				if profile.Timezone != "" {
					station.Timezone = profile.Timezone
				}
				if profile.URL != "" && station.Website == "" {
					station.Website = profile.URL
				}
			}

			// Download station logo/artwork
			logoData, logoMime, err := client.DownloadStationArt(ctx, azStation.ID)
			if err == nil && len(logoData) > 0 {
				station.Logo = logoData
				station.LogoMime = logoMime
				a.logger.Debug().Int("station_id", azStation.ID).Int("logo_size", len(logoData)).Msg("downloaded station logo")
			}

			// Set owner if importing user specified
			if options.ImportingUserID != "" {
				station.OwnerID = options.ImportingUserID
			}

			if err := a.db.WithContext(ctx).Create(station).Error; err != nil {
				return nil, fmt.Errorf("create station: %w", err)
			}

			// Create station-user association for the owner (only for new stations)
			if options.ImportingUserID != "" {
				// Check if association already exists
				var existingAssoc models.StationUser
				if err := a.db.WithContext(ctx).Where("user_id = ? AND station_id = ?", options.ImportingUserID, station.ID).First(&existingAssoc).Error; err != nil {
					stationUser := &models.StationUser{
						ID:        uuid.New().String(),
						UserID:    options.ImportingUserID,
						StationID: station.ID,
						Role:      models.StationRoleOwner,
					}
					if err := a.db.WithContext(ctx).Create(stationUser).Error; err != nil {
						a.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to create owner association")
					}
				}
			}

			result.StationsCreated++
			a.logger.Info().
				Str("station_id", station.ID).
				Str("name", station.Name).
				Msg("created new station")
		}

		stationMap[azStation.ID] = station.ID

		result.Mappings[fmt.Sprintf("station_%d", azStation.ID)] = Mapping{
			OldID:   fmt.Sprintf("%d", azStation.ID),
			NewID:   station.ID,
			Type:    "station",
			Name:    station.Name,
			Skipped: isExisting,
			Reason: func() string {
				if isExisting {
					return "already exists"
				}
				return ""
			}(),
		}

		a.logger.Info().
			Str("station_id", station.ID).
			Str("name", station.Name).
			Bool("existing", isExisting).
			Bool("has_logo", len(station.Logo) > 0).
			Str("genre", station.Genre).
			Msg("station imported with branding")

		progressCallback(Progress{
			Phase:            "importing_stations",
			CurrentStep:      fmt.Sprintf("Imported station: %s (with branding)", station.Name),
			TotalSteps:       5,
			CompletedSteps:   1,
			Percentage:       20 + (float64(i+1)/float64(len(stations)))*10,
			StationsTotal:    len(stations),
			StationsImported: i + 1,
			StartTime:        startTime,
		})

		// Import mounts for this station
		mounts, err := client.GetMounts(ctx, azStation.ID)
		mountsImported := 0
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", azStation.ID).Msg("failed to get mounts")
		} else {
			for _, azMount := range mounts {
				// Strip leading slash to avoid double slashes in URL construction
				// e.g., "/radio.mp3" becomes "radio.mp3"
				mountPath := strings.TrimPrefix(azMount.Path, "/")
				if mountPath == "" {
					mountPath = strings.TrimPrefix(azMount.Name, "/")
					// Also strip any description suffix like " (128kbps MP3)"
					if idx := strings.Index(mountPath, " "); idx > 0 {
						mountPath = mountPath[:idx]
					}
				}

				mount := &models.Mount{
					ID:         uuid.New().String(),
					StationID:  station.ID,
					Name:       mountPath, // Without leading slash (e.g., "radio.mp3")
					URL:        "/live/" + mountPath,
					Format:     azMount.AutodjFormat,
					Bitrate:    azMount.AutodjBitrate,
					Channels:   2,
					SampleRate: 44100,
				}

				if err := a.db.WithContext(ctx).Create(mount).Error; err != nil {
					a.logger.Error().Err(err).Str("mount", mountPath).Msg("failed to create mount")
				} else {
					mountsImported++
				}
			}
		}

		// Create a default mount if no mounts were imported
		if mountsImported == 0 {
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
			if err := a.db.WithContext(ctx).Create(mount).Error; err != nil {
				a.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to create default mount")
			} else {
				a.logger.Info().
					Str("station_id", station.ID).
					Str("mount", mountName).
					Msg("created default mount (no mounts from source)")
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
	if options.JobID != "" {
		if err := a.verifyImportDurations(ctx, options.JobID, options.DurationVerifyStrict, result); err != nil {
			return nil, err
		}
	}

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

// azMediaDownloadResult holds the result of a single media file download from AzuraCast.
type azMediaDownloadResult struct {
	azMedia     AzuraCastAPIMediaFile
	data        []byte
	contentHash string
	artwork     []byte // Album art
	artworkMime string // Artwork MIME type
	err         error
	errType     string // "download", "read"
}

// importMediaFromAPI imports media files from AzuraCast API with concurrent HTTP downloads.
// Downloads up to 12 files concurrently for optimal performance.
func (a *AzuraCastImporter) importMediaFromAPI(ctx context.Context, client *AzuraCastAPIClient, azStationID int, grimnirStationID string, result *Result, progressCallback ProgressCallback, startTime time.Time) error {
	mediaList, err := client.GetMedia(ctx, azStationID)
	if err != nil {
		return fmt.Errorf("get media: %w", err)
	}

	if len(mediaList) == 0 {
		return nil
	}

	a.logger.Info().Int("count", len(mediaList)).Int("station_id", azStationID).Msg("importing media files via API (concurrent HTTP downloads)")

	// Concurrent download settings
	const maxConcurrentDownloads = 12 // Download 12 files at a time
	semaphore := make(chan struct{}, maxConcurrentDownloads)
	resultsChan := make(chan azMediaDownloadResult, maxConcurrentDownloads)

	var wg sync.WaitGroup
	var processedCount int32
	var deduplicatedCount int32
	var mu sync.Mutex // Protects result.Mappings and result.Skipped

	// Start download workers in goroutines
	for _, azMedia := range mediaList {
		wg.Add(1)
		go func(media AzuraCastAPIMediaFile) {
			defer wg.Done()

			// Acquire semaphore slot (limits concurrent downloads to 12)
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check if context is cancelled
			if ctx.Err() != nil {
				resultsChan <- azMediaDownloadResult{azMedia: media, err: ctx.Err(), errType: "download"}
				return
			}

			// Download file via HTTP from AzuraCast API
			a.logger.Debug().
				Int("media_id", media.ID).
				Str("title", media.Title).
				Msg("downloading file and artwork via HTTP")

			reader, _, err := client.DownloadMedia(ctx, azStationID, media.ID)
			if err != nil {
				resultsChan <- azMediaDownloadResult{azMedia: media, err: err, errType: "download"}
				return
			}
			defer reader.Close()

			// Read into buffer and compute hash simultaneously
			var buf bytes.Buffer
			hasher := sha256.New()
			teeReader := io.TeeReader(reader, hasher)
			if _, err := io.Copy(&buf, teeReader); err != nil {
				resultsChan <- azMediaDownloadResult{azMedia: media, err: err, errType: "read"}
				return
			}

			contentHash := hex.EncodeToString(hasher.Sum(nil))

			// Also download album art if available
			// Always try to download - DownloadMediaArt handles missing art gracefully
			artwork, artworkMime, _ := client.DownloadMediaArt(ctx, azStationID, media.ID)
			if len(artwork) > 0 {
				a.logger.Debug().
					Int("media_id", media.ID).
					Int("artwork_size", len(artwork)).
					Msg("downloaded album artwork")
			}

			resultsChan <- azMediaDownloadResult{
				azMedia:     media,
				data:        buf.Bytes(),
				contentHash: contentHash,
				artwork:     artwork,
				artworkMime: artworkMime,
			}
		}(azMedia)
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
			a.logger.Error().
				Err(downloadResult.err).
				Str("title", downloadResult.azMedia.Title).
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
			a.updateMediaProgress(progressCallback, int(current), len(mediaList), startTime)
			continue
		}

		azMedia := downloadResult.azMedia
		contentHash := downloadResult.contentHash
		data := downloadResult.data
		artwork := downloadResult.artwork
		artworkMime := downloadResult.artworkMime

		// Check for existing media with same hash (deduplication across stations)
		var existingMedia models.MediaItem
		err := a.db.WithContext(ctx).Where("content_hash = ?", contentHash).First(&existingMedia).Error
		if err == nil {
			// Media already exists - create a link instead of re-uploading
			// Still include artwork and new metadata even for deduplicated files
			mediaItem := a.createMediaItemFromAzMedia(azMedia, grimnirStationID, contentHash, artwork, artworkMime)
			mediaItem.StorageKey = existingMedia.StorageKey
			mediaItem.Path = existingMedia.Path

			if err := a.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
				a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to create linked media item")
				mu.Lock()
				result.Skipped["media_db_failed"]++
				mu.Unlock()

				a.updateMediaProgress(progressCallback, int(current), len(mediaList), startTime)
				continue
			}

			atomic.AddInt32(&deduplicatedCount, 1)

			mu.Lock()
			result.MediaItemsImported++
			result.Mappings[fmt.Sprintf("media_%d", azMedia.ID)] = Mapping{
				OldID: fmt.Sprintf("%d", azMedia.ID),
				NewID: mediaItem.ID,
				Type:  "media",
				Name:  fmt.Sprintf("%s (deduplicated)", mediaItem.Title),
			}
			mu.Unlock()

			a.logger.Debug().
				Str("title", azMedia.Title).
				Str("hash", contentHash[:12]).
				Str("existing_id", existingMedia.ID).
				Bool("has_artwork", len(artwork) > 0).
				Msg("deduplicated media file")

			a.updateMediaProgress(progressCallback, int(current), len(mediaList), startTime)
			continue
		}

		// New media - upload to storage
		mediaItem := a.createMediaItemFromAzMedia(azMedia, grimnirStationID, contentHash, artwork, artworkMime)

		storageKey, err := a.mediaService.Store(ctx, grimnirStationID, mediaItem.ID, bytes.NewReader(data))
		if err != nil {
			a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to upload media to storage")
			mu.Lock()
			result.Skipped["media_upload_failed"]++
			mu.Unlock()

			a.updateMediaProgress(progressCallback, int(current), len(mediaList), startTime)
			continue
		}

		mediaItem.StorageKey = storageKey
		mediaItem.Path = a.mediaService.URL(storageKey)

		if err := a.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
			a.logger.Error().Err(err).Str("title", azMedia.Title).Msg("failed to create media item in database")
			mu.Lock()
			result.Skipped["media_db_failed"]++
			mu.Unlock()

			a.updateMediaProgress(progressCallback, int(current), len(mediaList), startTime)
			continue
		}

		mu.Lock()
		result.MediaItemsImported++
		result.Mappings[fmt.Sprintf("media_%d", azMedia.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", azMedia.ID),
			NewID: mediaItem.ID,
			Type:  "media",
			Name:  mediaItem.Title,
		}
		mu.Unlock()

		a.logger.Debug().
			Str("title", azMedia.Title).
			Str("storage_key", storageKey).
			Bool("has_artwork", len(artwork) > 0).
			Bool("has_isrc", azMedia.ISRC != "").
			Bool("has_lyrics", azMedia.Lyrics != "").
			Msg("media file imported with full metadata")

		a.updateMediaProgress(progressCallback, int(current), len(mediaList), startTime)
	}

	finalDedup := int(atomic.LoadInt32(&deduplicatedCount))
	if finalDedup > 0 {
		a.logger.Info().Int("count", finalDedup).Msg("deduplicated media files (linked to existing)")
		result.Skipped["media_deduplicated"] = finalDedup
	}

	a.logger.Info().
		Int("total", len(mediaList)).
		Int("imported", result.MediaItemsImported).
		Int("deduplicated", finalDedup).
		Msg("concurrent media import complete")

	return nil
}

// createMediaItemFromAzMedia creates a MediaItem from an AzuraCast media file.
func (a *AzuraCastImporter) createMediaItemFromAzMedia(azMedia AzuraCastAPIMediaFile, stationID, contentHash string, artwork []byte, artworkMime string) *models.MediaItem {
	mediaItem := &models.MediaItem{
		ID:            uuid.New().String(),
		StationID:     stationID,
		Title:         azMedia.Title,
		Artist:        azMedia.Artist,
		Album:         azMedia.Album,
		Genre:         azMedia.Genre,
		Duration:      time.Duration(azMedia.Length * float64(time.Second)),
		ImportPath:    azMedia.Path,
		ContentHash:   contentHash,
		ISRC:          azMedia.ISRC,
		Lyrics:        azMedia.Lyrics,
		Artwork:       artwork,
		ArtworkMime:   artworkMime,
		ShowInArchive: true, // Explicitly set for imported media
	}

	// Copy custom fields if any
	if len(azMedia.CustomFields) > 0 {
		mediaItem.CustomFields = azMedia.CustomFields
	}

	// Set cue points if available (from extra_metadata)
	em := azMedia.ExtraMetadata
	if em.CueIn != nil || em.CueOut != nil || em.FadeIn != nil || em.FadeOut != nil {
		cuePoints := models.CuePointSet{}
		if em.CueIn != nil {
			cuePoints.IntroEnd = *em.CueIn
		}
		if em.CueOut != nil {
			cuePoints.OutroIn = *em.CueOut
		}
		if em.FadeIn != nil {
			cuePoints.FadeIn = *em.FadeIn
		}
		if em.FadeOut != nil {
			cuePoints.FadeOut = *em.FadeOut
		}
		mediaItem.CuePoints = cuePoints
	}

	// Set replay gain if available (from extra_metadata)
	if em.Amplify != nil {
		mediaItem.ReplayGain = *em.Amplify
	}

	return mediaItem
}

// updateMediaProgress updates the progress callback with current media import status.
func (a *AzuraCastImporter) updateMediaProgress(progressCallback ProgressCallback, current, total int, startTime time.Time) {
	eta := calculateETA(startTime, current, total)
	progressCallback(Progress{
		Phase:              "importing_media",
		CurrentStep:        fmt.Sprintf("Downloading & importing media via HTTP: %d/%d", current, total),
		TotalSteps:         5,
		CompletedSteps:     2,
		Percentage:         30 + (float64(current)/float64(total))*40,
		MediaTotal:         total,
		MediaImported:      current,
		StartTime:          startTime,
		EstimatedRemaining: eta,
	})
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
			// Create new user with default platform role
			user := &models.User{
				ID:           uuid.New().String(),
				Email:        email,
				Password:     uuid.New().String(), // Random password, must be reset
				PlatformRole: models.PlatformRoleUser,
			}

			if err := a.db.WithContext(ctx).Create(user).Error; err != nil {
				a.logger.Error().Err(err).Str("streamer", azStreamer.DisplayName).Msg("failed to create user")
				continue
			}
			userID = user.ID
			result.UsersCreated++
		}

		// Create station-user association with DJ role
		stationUser := &models.StationUser{
			ID:        uuid.New().String(),
			UserID:    userID,
			StationID: grimnirStationID,
			Role:      models.StationRoleDJ,
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
		Warnings: []string{},
		Skipped:  make(map[string]int),
		Mappings: make(map[string]Mapping),
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

	if err := a.importStationsWithOptions(ctx, backup, result, progressCallback, startTime, options); err != nil {
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
			Phase:          "importing_playlists",
			CurrentStep:    "Importing playlists",
			TotalSteps:     5,
			CompletedSteps: 4,
			Percentage:     80,
			PlaylistsTotal: backup.PlaylistCount,
			StartTime:      startTime,
		})

		if err := a.importPlaylists(ctx, backup, result); err != nil {
			return nil, fmt.Errorf("import playlists: %w", err)
		}
	} else {
		result.Skipped["playlists"] = backup.PlaylistCount
	}

	// Complete
	if options.JobID != "" {
		if err := a.verifyImportDurations(ctx, options.JobID, options.DurationVerifyStrict, result); err != nil {
			return nil, err
		}
	}

	progressCallback(Progress{
		Phase:             "completed",
		CurrentStep:       "Migration completed",
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

		target, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
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
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("unsupported archive entry type for %q", header.Name)
		}
	}

	a.logger.Info().Str("dest", destDir).Msg("backup extracted")
	return nil
}

func safeExtractPath(destDir, entryName string) (string, error) {
	clean := filepath.Clean(entryName)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("invalid archive entry path %q", entryName)
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute archive entry path %q is not allowed", entryName)
	}

	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolve destination root: %w", err)
	}
	targetAbs, err := filepath.Abs(filepath.Join(destAbs, clean))
	if err != nil {
		return "", fmt.Errorf("resolve archive entry path: %w", err)
	}
	rel, err := filepath.Rel(destAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("verify archive entry path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes extraction root", entryName)
	}
	return targetAbs, nil
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
	return a.importStationsWithOptions(ctx, backup, result, progressCallback, startTime, Options{})
}

// importStationsWithOptions imports stations from the backup with full options.
func (a *AzuraCastImporter) importStationsWithOptions(ctx context.Context, backup *AzuraCastBackup, result *Result, progressCallback ProgressCallback, startTime time.Time, options Options) error {
	a.logger.Info().Int("count", len(backup.Stations)).Msg("importing stations from backup")

	for i, azStation := range backup.Stations {
		// Create Grimnir station
		station := &models.Station{
			ID:          uuid.New().String(),
			Name:        azStation.Name,
			Description: azStation.Description,
			Timezone:    "UTC",
			Active:      azStation.IsEnabled,
			Public:      false,
			Approved:    true,
		}

		// Set owner if importing user specified
		if options.ImportingUserID != "" {
			station.OwnerID = options.ImportingUserID
		}

		if err := a.db.WithContext(ctx).Create(station).Error; err != nil {
			a.logger.Error().Err(err).Str("station_name", azStation.Name).Msg("failed to create station")
			return fmt.Errorf("create station: %w", err)
		}

		a.logger.Info().
			Str("station_id", station.ID).
			Str("station_name", station.Name).
			Int("azuracast_id", azStation.ID).
			Msg("station created")

		// Create station-user association for the owner
		if options.ImportingUserID != "" {
			stationUser := &models.StationUser{
				ID:        uuid.New().String(),
				UserID:    options.ImportingUserID,
				StationID: station.ID,
				Role:      models.StationRoleOwner,
			}
			if err := a.db.WithContext(ctx).Create(stationUser).Error; err != nil {
				a.logger.Warn().Err(err).Str("station_id", station.ID).Msg("failed to create owner association")
			} else {
				a.logger.Debug().
					Str("station_id", station.ID).
					Str("owner_id", options.ImportingUserID).
					Msg("station owner association created")
			}
		}

		// Track mapping
		result.Mappings[fmt.Sprintf("station_%d", azStation.ID)] = Mapping{
			OldID: fmt.Sprintf("%d", azStation.ID),
			NewID: station.ID,
			Type:  "station",
			Name:  station.Name,
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

	a.logger.Info().Int("stations_created", result.StationsCreated).Msg("stations import complete")
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
			ID:            mediaID,
			StationID:     stationID,
			Title:         title,
			Path:          "", // Will be set after upload
			StorageKey:    "", // Will be set after upload
			ImportPath:    mediaFile.relPath,
			Duration:      0,    // Will be analyzed later
			ShowInArchive: true, // Explicitly set for imported media
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

// AnalyzeForStaging performs deep analysis for staged review.
func (a *AzuraCastImporter) AnalyzeForStaging(ctx context.Context, jobID string, options Options) (*models.StagedImport, error) {
	a.logger.Info().Str("job_id", jobID).Msg("analyzing AzuraCast for staged import")

	if !isAPIMode(options) {
		return a.analyzeForStagingBackup(ctx, jobID, options)
	}

	analyzer := NewStagedAnalyzer(a.db, a.logger)
	staged, err := analyzer.CreateStagedImport(ctx, jobID, string(SourceTypeAzuraCast))
	if err != nil {
		return nil, fmt.Errorf("create staged import: %w", err)
	}

	client, err := createAPIClient(options)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	stations, err := client.GetStations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get stations: %w", err)
	}

	var azuraWarnings models.ImportWarnings
	if len(stations) > 1 {
		azuraWarnings = append(azuraWarnings, models.ImportWarning{
			Code:     "multi_station_detected",
			Severity: "info",
			Message:  fmt.Sprintf("%d stations detected", len(stations)),
			Details:  "Station-level selection is not yet available; review and deselect items per tab as needed.",
		})
	}

	if !options.SkipUsers {
		azuraWarnings = append(azuraWarnings, models.ImportWarning{
			Code:     "streamers_not_staged",
			Severity: "info",
			Message:  "Streamer/DJ accounts are excluded from staged review",
			Details:  "Use non-staged import if you need automatic DJ account migration in this run.",
		})
	}

	for _, station := range stations {
		azuraWarnings = append(azuraWarnings, models.ImportWarning{
			Code:     "source_station_label",
			Severity: "info",
			Message:  strings.TrimSpace(station.Name),
			ItemType: "station",
			ItemID:   fmt.Sprintf("%d", station.ID),
		})

		playlistNames := make(map[int]string)

		if !options.SkipMedia {
			mediaList, err := client.GetMedia(ctx, station.ID)
			if err != nil {
				a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to fetch media for staged analysis")
			} else {
				for _, media := range mediaList {
					title := media.Title
					if title == "" {
						title = filepath.Base(media.Path)
					}

					durationMs := int(media.Length * float64(time.Second/time.Millisecond))
					scopedSourceID := makeScopedSourceID(station.ID, fmt.Sprintf("%d", media.ID))

					isDuplicate := false
					duplicateOfID := ""
					var existing models.MediaItem
					if err := a.db.WithContext(ctx).
						Where("import_source = ? AND import_source_id IN ?", string(SourceTypeAzuraCast), []string{scopedSourceID, fmt.Sprintf("%d", media.ID)}).
						First(&existing).Error; err == nil {
						isDuplicate = true
						duplicateOfID = existing.ID
					}

					staged.StagedMedia = append(staged.StagedMedia, models.StagedMediaItem{
						SourceID:      scopedSourceID,
						Title:         title,
						Artist:        media.Artist,
						Album:         media.Album,
						Genre:         media.Genre,
						DurationMs:    durationMs,
						FilePath:      media.Path,
						FileSize:      media.Size,
						IsDuplicate:   isDuplicate,
						DuplicateOfID: duplicateOfID,
						Selected:      !isDuplicate,
					})
				}
			}
		}

		playlists, err := client.GetPlaylists(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to fetch playlists for staged analysis")
			continue
		}

		for _, playlist := range playlists {
			playlistNames[playlist.ID] = playlist.Name

			if playlist.Source == "remote_url" && playlist.RemoteURL != "" {
				staged.StagedWebstreams = append(staged.StagedWebstreams, models.StagedWebstreamItem{
					SourceID:    makeScopedSourceID(station.ID, fmt.Sprintf("%d", playlist.ID)),
					Name:        playlist.Name,
					Description: fmt.Sprintf("Station: %s", station.Name),
					URL:         playlist.RemoteURL,
					Selected:    true,
				})
				continue
			}

			order := "shuffle"
			if playlist.Order == "sequential" {
				order = "sequential"
			}
			summary := fmt.Sprintf("Type: %s, Order: %s, Weight: %d", playlist.Type, order, playlist.Weight)

			staged.StagedSmartBlocks = append(staged.StagedSmartBlocks, models.StagedSmartBlockItem{
				SourceID:        makeScopedSourceID(station.ID, fmt.Sprintf("%d", playlist.ID)),
				Name:            playlist.Name,
				Description:     fmt.Sprintf("Station: %s", station.Name),
				CriteriaCount:   0,
				CriteriaSummary: summary,
				Selected:        true,
			})
		}

		if options.SkipSchedules {
			continue
		}

		schedules, err := client.GetSchedules(ctx, station.ID)
		if err != nil {
			a.logger.Warn().Err(err).Int("station_id", station.ID).Msg("failed to fetch schedules for staged analysis")
			continue
		}

		for _, sched := range schedules {
			name := "Scheduled block"
			if plName := playlistNames[sched.PlaylistID]; plName != "" {
				name = plName
			}

			durationMinutes := 60
			if sched.EndTime > sched.StartTime {
				durationMinutes = (sched.EndTime - sched.StartTime) / 60
			}

			stagedShow := models.StagedShowItem{
				SourceID:        makeScopedSourceID(station.ID, fmt.Sprintf("%d", sched.ID)),
				Name:            name,
				Description:     fmt.Sprintf("Imported from station %s playlist schedule", station.Name),
				DurationMinutes: durationMinutes,
				Timezone:        "UTC",
				InstanceCount:   1,
				Selected:        true,
			}

			dtStart, rrule, pattern := buildAzuraScheduleRecurrence(sched)
			stagedShow.DTStart = dtStart
			stagedShow.DetectedRRule = rrule
			stagedShow.PatternNote = pattern
			if rrule != "" {
				stagedShow.PatternConfidence = 1.0
				stagedShow.CreateShow = true
			} else {
				stagedShow.CreateClock = true
			}

			staged.StagedShows = append(staged.StagedShows, stagedShow)
		}
	}

	analyzer.ApplyDefaultSelections(staged)
	analyzer.GenerateWarnings(staged)
	staged.Warnings = append(staged.Warnings, azuraWarnings...)
	analyzer.GenerateSuggestions(staged)

	now := time.Now()
	staged.AnalyzedAt = &now
	staged.Status = models.StagedImportStatusReady
	if err := analyzer.UpdateStagedImport(ctx, staged); err != nil {
		return nil, fmt.Errorf("update staged import: %w", err)
	}

	return staged, nil
}

func (a *AzuraCastImporter) analyzeForStagingBackup(ctx context.Context, jobID string, options Options) (*models.StagedImport, error) {
	if options.AzuraCastBackupPath == "" {
		return nil, fmt.Errorf("backup path required for backup staged analysis")
	}

	tempDir, err := os.MkdirTemp("", "azuracast-staged-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := a.extractBackup(options.AzuraCastBackupPath, tempDir); err != nil {
		return nil, fmt.Errorf("extract backup: %w", err)
	}

	backup, err := a.parseBackup(tempDir)
	if err != nil {
		return nil, fmt.Errorf("parse backup: %w", err)
	}

	analyzer := NewStagedAnalyzer(a.db, a.logger)
	staged, err := analyzer.CreateStagedImport(ctx, jobID, string(SourceTypeAzuraCast))
	if err != nil {
		return nil, fmt.Errorf("create staged import: %w", err)
	}

	if len(backup.Stations) > 1 {
		staged.Warnings = append(staged.Warnings, models.ImportWarning{
			Code:     "multi_station_detected",
			Severity: "info",
			Message:  fmt.Sprintf("%d stations detected in backup", len(backup.Stations)),
			Details:  "Station-level selection is not yet available; review and deselect items per tab as needed.",
		})
	}
	for _, station := range backup.Stations {
		staged.Warnings = append(staged.Warnings, models.ImportWarning{
			Code:     "source_station_label",
			Severity: "info",
			Message:  strings.TrimSpace(station.Name),
			ItemType: "station",
			ItemID:   fmt.Sprintf("%d", station.ID),
		})
	}

	mediaFiles, err := scanBackupMediaFiles(filepath.Join(tempDir, "media"))
	if err != nil {
		return nil, fmt.Errorf("scan backup media: %w", err)
	}
	primaryStationID := 1
	if len(backup.Stations) > 0 {
		primaryStationID = backup.Stations[0].ID
	}

	for _, mf := range mediaFiles {
		contentHash := ""
		f, err := os.Open(mf.AbsPath)
		if err == nil {
			hasher := sha256.New()
			_, _ = io.Copy(hasher, f)
			_ = f.Close()
			contentHash = hex.EncodeToString(hasher.Sum(nil))
		}

		staged.StagedMedia = append(staged.StagedMedia, models.StagedMediaItem{
			SourceID:    makeScopedSourceID(primaryStationID, mf.RelPath),
			Title:       strings.TrimSuffix(filepath.Base(mf.RelPath), filepath.Ext(mf.RelPath)),
			FilePath:    mf.RelPath,
			FileSize:    mf.Size,
			ContentHash: contentHash,
			Selected:    true,
		})
	}

	staged.StagedMedia = analyzer.DetectDuplicates(ctx, staged.StagedMedia, options.TargetStationID)
	analyzer.ApplyDefaultSelections(staged)
	analyzer.GenerateWarnings(staged)
	analyzer.GenerateSuggestions(staged)

	staged.Warnings = append(staged.Warnings, models.ImportWarning{
		Code:     "backup_staged_limited_metadata",
		Severity: "info",
		Message:  "Backup staged analysis includes media-first review with limited metadata",
		Details:  "Playlist/schedule enrichment depends on backup schema availability and may be incomplete.",
	})

	now := time.Now()
	staged.AnalyzedAt = &now
	staged.Status = models.StagedImportStatusReady
	if err := analyzer.UpdateStagedImport(ctx, staged); err != nil {
		return nil, fmt.Errorf("update staged import: %w", err)
	}

	return staged, nil
}

type backupMediaFile struct {
	AbsPath string
	RelPath string
	Size    int64
}

func scanBackupMediaFiles(mediaRoot string) ([]backupMediaFile, error) {
	stat, err := os.Stat(mediaRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !stat.IsDir() {
		return nil, nil
	}

	out := make([]backupMediaFile, 0, 256)
	err = filepath.Walk(mediaRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".mp3", ".flac", ".ogg", ".m4a", ".wav", ".aac":
		default:
			return nil
		}
		relPath, err := filepath.Rel(mediaRoot, path)
		if err != nil {
			return err
		}
		out = append(out, backupMediaFile{
			AbsPath: path,
			RelPath: filepath.ToSlash(relPath),
			Size:    info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CommitStagedImport imports selected items from a staged AzuraCast plan.
func (a *AzuraCastImporter) CommitStagedImport(ctx context.Context, staged *models.StagedImport, jobID string, options Options, cb ProgressCallback) (*Result, error) {
	startTime := time.Now()
	result := &Result{
		Warnings: []string{},
		Skipped:  make(map[string]int),
		Mappings: make(map[string]Mapping),
	}
	importedItems := &ImportedItems{}

	client, err := createAPIClient(options)
	if err != nil {
		return nil, fmt.Errorf("create API client: %w", err)
	}

	stations, err := client.GetStations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get stations: %w", err)
	}

	stationByID := make(map[int]AzuraCastAPIStation)
	for _, s := range stations {
		stationByID[s.ID] = s
	}

	stationMap := make(map[int]string) // source station -> grimnir station
	for sourceStationID, sourceStation := range stationByID {
		stationID, created, err := a.ensureTargetStation(ctx, options, sourceStation)
		if err != nil {
			return nil, err
		}
		stationMap[sourceStationID] = stationID
		if created {
			result.StationsCreated++
		}
	}

	// Build selected media id map per station, then fetch media catalogs once per station.
	selectedMedia := make(map[int]map[int]bool)
	for _, item := range staged.StagedMedia {
		if !item.Selected {
			result.Skipped["media_deselected"]++
			continue
		}
		sourceStationID, sourceID, err := parseScopedSourceID(item.SourceID)
		if err != nil {
			result.Skipped["media_invalid_id"]++
			continue
		}
		mediaID := 0
		_, _ = fmt.Sscanf(sourceID, "%d", &mediaID)
		if mediaID == 0 {
			result.Skipped["media_invalid_id"]++
			continue
		}
		if selectedMedia[sourceStationID] == nil {
			selectedMedia[sourceStationID] = make(map[int]bool)
		}
		selectedMedia[sourceStationID][mediaID] = true
	}

	mediaCatalog := make(map[int]map[int]AzuraCastAPIMediaFile)
	for sourceStationID := range selectedMedia {
		mediaList, err := client.GetMedia(ctx, sourceStationID)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to fetch media list for station %d: %v", sourceStationID, err))
			continue
		}
		mediaCatalog[sourceStationID] = make(map[int]AzuraCastAPIMediaFile, len(mediaList))
		for _, m := range mediaList {
			mediaCatalog[sourceStationID][m.ID] = m
		}
	}

	totalMediaSelected := 0
	for _, ids := range selectedMedia {
		totalMediaSelected += len(ids)
	}
	mediaImported := 0

	for sourceStationID, idSet := range selectedMedia {
		targetStationID, ok := stationMap[sourceStationID]
		if !ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf("No target station mapping found for source station %d", sourceStationID))
			continue
		}

		for mediaID := range idSet {
			azMedia, ok := mediaCatalog[sourceStationID][mediaID]
			if !ok {
				result.Skipped["media_not_found"]++
				continue
			}

			scopedSourceID := makeScopedSourceID(sourceStationID, fmt.Sprintf("%d", mediaID))
			var existingImported models.MediaItem
			if err := a.db.WithContext(ctx).
				Where("station_id = ? AND import_source = ? AND import_source_id IN ?", targetStationID, string(SourceTypeAzuraCast), []string{scopedSourceID, fmt.Sprintf("%d", mediaID)}).
				First(&existingImported).Error; err == nil {
				result.Skipped["media_already_imported"]++
				continue
			}

			reader, _, err := client.DownloadMedia(ctx, sourceStationID, mediaID)
			if err != nil {
				result.Skipped["media_download_failed"]++
				continue
			}

			var buf bytes.Buffer
			hasher := sha256.New()
			if _, err := io.Copy(io.MultiWriter(&buf, hasher), reader); err != nil {
				reader.Close()
				result.Skipped["media_read_failed"]++
				continue
			}
			reader.Close()

			contentHash := hex.EncodeToString(hasher.Sum(nil))
			artwork, artworkMime, _ := client.DownloadMediaArt(ctx, sourceStationID, mediaID)

			mediaItem := a.createMediaItemFromAzMedia(azMedia, targetStationID, contentHash, artwork, artworkMime)
			mediaItem.ImportJobID = &jobID
			mediaItem.ImportSource = string(SourceTypeAzuraCast)
			mediaItem.ImportSourceID = scopedSourceID

			var existingHash models.MediaItem
			if err := a.db.WithContext(ctx).Where("content_hash = ?", contentHash).First(&existingHash).Error; err == nil {
				mediaItem.StorageKey = existingHash.StorageKey
				mediaItem.Path = existingHash.Path
			} else {
				storageKey, err := a.mediaService.Store(ctx, targetStationID, mediaItem.ID, bytes.NewReader(buf.Bytes()))
				if err != nil {
					result.Skipped["media_storage_failed"]++
					continue
				}
				mediaItem.StorageKey = storageKey
				mediaItem.Path = a.mediaService.URL(storageKey)
			}

			if err := a.db.WithContext(ctx).Create(mediaItem).Error; err != nil {
				result.Skipped["media_db_failed"]++
				continue
			}

			importedItems.MediaIDs = append(importedItems.MediaIDs, mediaItem.ID)
			result.MediaItemsImported++
			mediaImported++
			result.Mappings[fmt.Sprintf("media_%s", scopedSourceID)] = Mapping{
				OldID: scopedSourceID,
				NewID: mediaItem.ID,
				Type:  "media",
				Name:  mediaItem.Title,
			}

			if totalMediaSelected > 0 {
				cb(Progress{
					Phase:         "importing_media",
					CurrentStep:   fmt.Sprintf("Importing media: %d/%d", mediaImported, totalMediaSelected),
					MediaTotal:    totalMediaSelected,
					MediaImported: mediaImported,
					Percentage:    10 + (float64(mediaImported)/float64(totalMediaSelected))*50,
					StartTime:     startTime,
				})
			}
		}
	}

	// Import selected smart blocks
	for _, sb := range staged.StagedSmartBlocks {
		if !sb.Selected {
			result.Skipped["smartblock_deselected"]++
			continue
		}
		sourceStationID, sourceID, err := parseScopedSourceID(sb.SourceID)
		if err != nil {
			result.Skipped["smartblock_invalid_id"]++
			continue
		}
		targetStationID, ok := stationMap[sourceStationID]
		if !ok {
			result.Skipped["smartblock_no_station"]++
			continue
		}
		scoped := makeScopedSourceID(sourceStationID, sourceID)
		if exists, err := sourceImportExists(ctx, a.db, &models.SmartBlock{}, targetStationID, string(SourceTypeAzuraCast), scoped, sourceID); err == nil && exists {
			result.Skipped["smartblock_already_imported"]++
			continue
		}

		smartBlock := &models.SmartBlock{
			ID:             uuid.New().String(),
			StationID:      targetStationID,
			Name:           sb.Name,
			Description:    sb.Description,
			Rules:          sb.RawCriteria,
			ImportJobID:    &jobID,
			ImportSource:   string(SourceTypeAzuraCast),
			ImportSourceID: scoped,
		}
		if err := a.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
			result.Skipped["smartblock_db_failed"]++
			continue
		}
		importedItems.SmartBlockIDs = append(importedItems.SmartBlockIDs, smartBlock.ID)
		result.PlaylistsCreated++
	}

	// Import selected shows/schedules
	for _, sh := range staged.StagedShows {
		if !sh.Selected {
			result.Skipped["show_deselected"]++
			continue
		}
		sourceStationID, sourceID, err := parseScopedSourceID(sh.SourceID)
		if err != nil {
			result.Skipped["show_invalid_id"]++
			continue
		}
		targetStationID, ok := stationMap[sourceStationID]
		if !ok {
			result.Skipped["show_no_station"]++
			continue
		}
		scoped := makeScopedSourceID(sourceStationID, sourceID)
		if exists, err := sourceImportExists(ctx, a.db, &models.Show{}, targetStationID, string(SourceTypeAzuraCast), scoped, sourceID); err == nil && exists {
			result.Skipped["show_already_imported"]++
			continue
		}

		rrule := sh.CustomRRule
		if rrule == "" {
			rrule = sh.DetectedRRule
		}

		if sh.CreateShow && rrule != "" {
			show := &models.Show{
				ID:                     uuid.New().String(),
				StationID:              targetStationID,
				Name:                   sh.Name,
				Description:            sh.Description,
				DefaultDurationMinutes: sh.DurationMinutes,
				Color:                  sh.Color,
				RRule:                  rrule,
				DTStart:                sh.DTStart,
				Timezone:               "UTC",
				Active:                 true,
				ImportJobID:            &jobID,
				ImportSource:           string(SourceTypeAzuraCast),
				ImportSourceID:         scoped,
			}
			if sh.Timezone != "" {
				show.Timezone = sh.Timezone
			}
			if err := a.db.WithContext(ctx).Create(show).Error; err != nil {
				result.Skipped["show_db_failed"]++
				continue
			}
			importedItems.ShowIDs = append(importedItems.ShowIDs, show.ID)
			result.SchedulesCreated++
			continue
		}

		clock := &models.ClockHour{
			ID:          uuid.New().String(),
			StationID:   targetStationID,
			Name:        sh.Name,
			ImportJobID: &jobID,
		}
		var existingClock models.ClockHour
		if err := a.db.WithContext(ctx).Where("station_id = ? AND name = ?", targetStationID, sh.Name).First(&existingClock).Error; err == nil {
			result.Skipped["clock_already_imported"]++
			continue
		}
		if err := a.db.WithContext(ctx).Create(clock).Error; err != nil {
			result.Skipped["clock_db_failed"]++
			continue
		}
		importedItems.ClockIDs = append(importedItems.ClockIDs, clock.ID)
		result.SchedulesCreated++
	}

	// Import selected webstreams
	for _, ws := range staged.StagedWebstreams {
		if !ws.Selected {
			result.Skipped["webstream_deselected"]++
			continue
		}
		sourceStationID, sourceID, err := parseScopedSourceID(ws.SourceID)
		if err != nil {
			result.Skipped["webstream_invalid_id"]++
			continue
		}
		targetStationID, ok := stationMap[sourceStationID]
		if !ok {
			result.Skipped["webstream_no_station"]++
			continue
		}
		scoped := makeScopedSourceID(sourceStationID, sourceID)
		if exists, err := sourceImportExists(ctx, a.db, &models.Webstream{}, targetStationID, string(SourceTypeAzuraCast), scoped, sourceID); err == nil && exists {
			result.Skipped["webstream_already_imported"]++
			continue
		}
		webstream := &models.Webstream{
			ID:             uuid.New().String(),
			StationID:      targetStationID,
			Name:           ws.Name,
			Description:    ws.Description,
			URLs:           []string{ws.URL},
			Active:         true,
			ImportJobID:    &jobID,
			ImportSource:   string(SourceTypeAzuraCast),
			ImportSourceID: scoped,
		}
		if err := a.db.WithContext(ctx).Create(webstream).Error; err != nil {
			result.Skipped["webstream_db_failed"]++
			continue
		}
		importedItems.WebstreamIDs = append(importedItems.WebstreamIDs, webstream.ID)
		result.PlaylistsCreated++
	}

	now := time.Now()
	staged.Status = models.StagedImportStatusCommitted
	staged.CommittedAt = &now
	_ = a.db.WithContext(ctx).Save(staged).Error

	var job Job
	if err := a.db.WithContext(ctx).First(&job, "id = ?", jobID).Error; err == nil {
		job.ImportedItems = importedItems
		_ = a.db.WithContext(ctx).Save(&job).Error
	}

	if options.JobID != "" {
		if err := a.verifyImportDurations(ctx, options.JobID, options.DurationVerifyStrict, result); err != nil {
			return nil, err
		}
	}

	result.DurationSeconds = time.Since(startTime).Seconds()
	return result, nil
}

func (a *AzuraCastImporter) ensureTargetStation(ctx context.Context, options Options, source AzuraCastAPIStation) (stationID string, created bool, err error) {
	if options.TargetStationID != "" {
		return options.TargetStationID, false, nil
	}

	var existing models.Station
	if err := a.db.WithContext(ctx).Where("name = ?", source.Name).First(&existing).Error; err == nil {
		return existing.ID, false, nil
	}

	station := &models.Station{
		ID:          uuid.New().String(),
		Name:        source.Name,
		Description: source.Description,
		Shortcode:   source.ShortName,
		Timezone:    "UTC",
		Active:      true,
		Public:      source.IsPublic,
		Approved:    true,
		ListenURL:   source.ListenURL,
		Website:     source.URL,
	}
	if options.ImportingUserID != "" {
		station.OwnerID = options.ImportingUserID
	}

	if err := a.db.WithContext(ctx).Create(station).Error; err != nil {
		return "", false, fmt.Errorf("create station %q: %w", source.Name, err)
	}

	if options.ImportingUserID != "" {
		stationUser := &models.StationUser{
			ID:        uuid.New().String(),
			UserID:    options.ImportingUserID,
			StationID: station.ID,
			Role:      models.StationRoleOwner,
		}
		_ = a.db.WithContext(ctx).Create(stationUser).Error
	}

	return station.ID, true, nil
}

func makeScopedSourceID(sourceStationID int, sourceID string) string {
	return fmt.Sprintf("%d::%s", sourceStationID, sourceID)
}

func parseScopedSourceID(scoped string) (sourceStationID int, sourceID string, err error) {
	parts := strings.SplitN(scoped, "::", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid scoped source ID: %s", scoped)
	}
	if _, err := fmt.Sscanf(parts[0], "%d", &sourceStationID); err != nil {
		return 0, "", fmt.Errorf("invalid source station ID in scoped ID: %s", scoped)
	}
	return sourceStationID, parts[1], nil
}

func sourceImportExists(ctx context.Context, db *gorm.DB, model any, stationID, source, scopedSourceID, rawSourceID string) (bool, error) {
	var count int64
	err := db.WithContext(ctx).
		Model(model).
		Where("station_id = ? AND import_source = ? AND import_source_id IN ?", stationID, source, []string{scopedSourceID, rawSourceID}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *AzuraCastImporter) verifyImportDurations(ctx context.Context, jobID string, strict bool, result *Result) error {
	if jobID == "" {
		return nil
	}

	var total int64
	if err := a.db.WithContext(ctx).Model(&models.MediaItem{}).Where("import_job_id = ?", jobID).Count(&total).Error; err != nil {
		return fmt.Errorf("duration verification count failed: %w", err)
	}
	if total == 0 {
		return nil
	}

	var zeroDuration int64
	if err := a.db.WithContext(ctx).
		Model(&models.MediaItem{}).
		Where("import_job_id = ? AND duration <= ?", jobID, 0).
		Count(&zeroDuration).Error; err != nil {
		return fmt.Errorf("duration verification zero-count failed: %w", err)
	}

	if zeroDuration == 0 {
		return nil
	}

	result.Skipped["media_duration_zero"] = int(zeroDuration)
	result.Warnings = append(result.Warnings, fmt.Sprintf("Duration verification: %d imported media items have zero/missing duration", zeroDuration))
	a.logger.Warn().
		Str("job_id", jobID).
		Int64("imported_media_total", total).
		Int64("zero_duration", zeroDuration).
		Bool("strict", strict).
		Msg("import duration verification found anomalies")

	if strict {
		return fmt.Errorf("duration verification failed: %d media items with zero/missing duration", zeroDuration)
	}
	return nil
}

func buildAzuraScheduleRecurrence(sched AzuraCastAPISchedule) (dtStart time.Time, rrule string, pattern string) {
	// start/end time are seconds from midnight in AzuraCast API.
	startHour := sched.StartTime / 3600
	startMinute := (sched.StartTime % 3600) / 60

	date := time.Now().UTC()
	if sched.StartDate != nil && *sched.StartDate != "" {
		if parsed, err := time.Parse("2006-01-02", *sched.StartDate); err == nil {
			date = parsed
		}
	}

	dtStart = time.Date(date.Year(), date.Month(), date.Day(), startHour, startMinute, 0, 0, time.UTC)

	if sched.LoopOnce || len(sched.Days) == 0 {
		return dtStart, "", "One-time schedule"
	}

	byDay := azuraDaysToByDay(sched.Days)
	if byDay == "" {
		return dtStart, "", "Schedule with unsupported day mapping"
	}

	rrule = fmt.Sprintf("FREQ=WEEKLY;BYDAY=%s;BYHOUR=%d;BYMINUTE=%d", byDay, startHour, startMinute)
	pattern = fmt.Sprintf("Weekly on %s", byDay)
	return dtStart, rrule, pattern
}

func azuraDaysToByDay(days []int) string {
	var result []string
	for _, day := range days {
		switch day {
		case 1:
			result = append(result, "MO")
		case 2:
			result = append(result, "TU")
		case 3:
			result = append(result, "WE")
		case 4:
			result = append(result, "TH")
		case 5:
			result = append(result, "FR")
		case 6:
			result = append(result, "SA")
		case 7:
			result = append(result, "SU")
		}
	}
	return strings.Join(result, ",")
}
