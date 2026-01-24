/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	db           *gorm.DB
	mediaService *media.Service
	logger       zerolog.Logger
}

// NewLibreTimeImporter creates a new LibreTime importer.
func NewLibreTimeImporter(db *gorm.DB, mediaService *media.Service, logger zerolog.Logger) *LibreTimeImporter {
	return &LibreTimeImporter{
		db:           db,
		mediaService: mediaService,
		logger:       logger.With().Str("importer", "libretime").Logger(),
	}
}

// Validate checks if the migration can proceed.
func (l *LibreTimeImporter) Validate(ctx context.Context, options Options) error {
	var errors ValidationErrors

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

// Analyze performs a dry-run analysis.
func (l *LibreTimeImporter) Analyze(ctx context.Context, options Options) (*Result, error) {
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
		mediaItem := &models.MediaItem{
			ID:         uuid.New().String(),
			StationID:  stationID,
			Title:      ltFile.TrackTitle.String,
			Artist:     ltFile.Artist.String,
			Album:      ltFile.Album.String,
			Genre:      ltFile.Genre.String,
			ImportPath: ltFile.Filepath,
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
		// Parse duration (HH:MM:SS format)
		durationTimeDuration, err := parseDuration(ltShow.Duration)
		if err != nil {
			l.logger.Warn().Err(err).Int("show_id", ltShow.ID).Str("duration", ltShow.Duration).Msg("failed to parse show duration")
			durationTimeDuration = time.Hour // Default to 1 hour
		}

		// Convert time.Duration to seconds (int) for Clock model
		durationSeconds := int(durationTimeDuration.Seconds())

		// Create Grimnir clock (shows become hour templates)
		clock := &models.Clock{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        ltShow.Name,
			Description: ltShow.Description.String,
			Duration:    durationSeconds,
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
