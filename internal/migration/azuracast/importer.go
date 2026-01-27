/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package azuracast

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

// Importer handles importing from AzuraCast backups
type Importer struct {
	db       *gorm.DB
	logger   zerolog.Logger
	options  migration.MigrationOptions
	stats    migration.MigrationStats
	progress migration.ProgressCallback
}

// NewImporter creates a new AzuraCast importer
func NewImporter(db *gorm.DB, logger zerolog.Logger, options migration.MigrationOptions) *Importer {
	return &Importer{
		db:      db,
		logger:  logger.With().Str("component", "azuracast_importer").Logger(),
		options: options,
	}
}

// SetProgressCallback sets the progress callback function
func (i *Importer) SetProgressCallback(callback migration.ProgressCallback) {
	i.progress = callback
}

// Import imports an AzuraCast backup
func (i *Importer) Import(ctx context.Context, backupPath string) (*migration.MigrationStats, error) {
	i.logger.Info().
		Str("backup", backupPath).
		Bool("dry_run", i.options.DryRun).
		Msg("starting AzuraCast import")

	// Extract backup to temp directory
	tempDir, err := os.MkdirTemp("", "azuracast-import-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	i.reportProgress(1, 10, "Extracting backup archive")
	if err := i.extractBackup(backupPath, tempDir); err != nil {
		return nil, fmt.Errorf("extract backup: %w", err)
	}

	// Find and open database file
	i.reportProgress(2, 10, "Opening AzuraCast database")
	dbPath := filepath.Join(tempDir, "db.db")
	azuraDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open azuracast db: %w", err)
	}
	defer azuraDB.Close()

	// Import stations
	i.reportProgress(3, 10, "Importing stations")
	stations, err := i.importStations(ctx, azuraDB)
	if err != nil {
		return nil, fmt.Errorf("import stations: %w", err)
	}

	// Import mounts
	i.reportProgress(4, 10, "Importing mounts")
	if err := i.importMounts(ctx, azuraDB, stations); err != nil {
		return nil, fmt.Errorf("import mounts: %w", err)
	}

	// Import media
	if !i.options.SkipMedia {
		i.reportProgress(5, 10, "Importing media")
		mediaDir := filepath.Join(tempDir, "media")
		if err := i.importMedia(ctx, azuraDB, stations, mediaDir); err != nil {
			return nil, fmt.Errorf("import media: %w", err)
		}
	}

	// Import playlists
	i.reportProgress(6, 10, "Importing playlists")
	if err := i.importPlaylists(ctx, azuraDB, stations); err != nil {
		return nil, fmt.Errorf("import playlists: %w", err)
	}

	// Import schedules
	i.reportProgress(7, 10, "Importing schedules")
	if err := i.importSchedules(ctx, azuraDB, stations); err != nil {
		return nil, fmt.Errorf("import schedules: %w", err)
	}

	// Import users (optional)
	i.reportProgress(8, 10, "Importing users")
	if err := i.importUsers(ctx, azuraDB); err != nil {
		i.logger.Warn().Err(err).Msg("failed to import users, continuing")
		i.stats.ErrorsEncountered++
	}

	i.reportProgress(10, 10, "Import completed")

	i.logger.Info().
		Interface("stats", i.stats).
		Msg("AzuraCast import completed")

	return &i.stats, nil
}

// extractBackup extracts a tar.gz backup to a directory
func (i *Importer) extractBackup(backupPath, destDir string) error {
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open backup: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
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
				return fmt.Errorf("mkdir: %w", err)
			}
		case tar.TypeReg:
			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("mkdir parent: %w", err)
			}

			outFile, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("copy file: %w", err)
			}
			outFile.Close()
		}
	}

	return nil
}

// importStations imports stations and returns a mapping of old ID to new ID
func (i *Importer) importStations(ctx context.Context, azuraDB *sql.DB) (map[int]string, error) {
	rows, err := azuraDB.QueryContext(ctx, `
		SELECT id, name, short_name, description, timezone, is_enabled
		FROM station
	`)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	defer rows.Close()

	stationMap := make(map[int]string)

	for rows.Next() {
		var s Station
		var timezone sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.ShortName, &s.Description, &timezone, &s.IsEnabled); err != nil {
			i.logger.Error().Err(err).Msg("scan station")
			i.stats.ErrorsEncountered++
			continue
		}

		if timezone.Valid {
			s.Timezone = timezone.String
		} else {
			s.Timezone = "UTC"
		}

		// Create Grimnir station
		station := &models.Station{
			ID:          uuid.New().String(),
			Name:        s.Name,
			Description: s.Description,
			Timezone:    s.Timezone,
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(station).Error; err != nil {
				i.logger.Error().Err(err).Str("station", s.Name).Msg("create station")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		stationMap[s.ID] = station.ID
		i.stats.StationsImported++

		i.logger.Info().
			Str("name", s.Name).
			Str("old_id", strconv.Itoa(s.ID)).
			Str("new_id", station.ID).
			Msg("imported station")
	}

	return stationMap, rows.Err()
}

// importMounts imports mount points
func (i *Importer) importMounts(ctx context.Context, azuraDB *sql.DB, stationMap map[int]string) error {
	rows, err := azuraDB.QueryContext(ctx, `
		SELECT id, station_id, name, display_name, is_visible_on_public_pages, is_default, autodj_bitrate
		FROM station_mounts
	`)
	if err != nil {
		return fmt.Errorf("query mounts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m StationMount
		if err := rows.Scan(&m.ID, &m.StationID, &m.Name, &m.DisplayName, &m.IsVisible, &m.IsDefault, &m.AutodjBitrate); err != nil {
			i.logger.Error().Err(err).Msg("scan mount")
			i.stats.ErrorsEncountered++
			continue
		}

		stationID, ok := stationMap[m.StationID]
		if !ok {
			i.logger.Warn().Int("station_id", m.StationID).Msg("mount for unknown station")
			continue
		}

		// Create Grimnir mount
		mount := &models.Mount{
			ID:        uuid.New().String(),
			StationID: stationID,
			Name:      m.Name,
			URL:       "/listen/" + m.Name, // Generate URL from name
			Format:    "mp3", // AzuraCast doesn't store format separately
			Bitrate:   m.AutodjBitrate,
			Channels:  2, // Default stereo
			SampleRate: 44100, // Default sample rate
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(mount).Error; err != nil {
				i.logger.Error().Err(err).Str("mount", m.Name).Msg("create mount")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.MountsImported++
	}

	return rows.Err()
}

// importMedia imports media files
func (i *Importer) importMedia(ctx context.Context, azuraDB *sql.DB, stationMap map[int]string, mediaDir string) error {
	rows, err := azuraDB.QueryContext(ctx, `
		SELECT id, storage_location_id, title, artist, album, genre, length, path,
		       amplify, fade_overlap, fade_in, fade_out, cue_in, cue_out
		FROM station_media
	`)
	if err != nil {
		return fmt.Errorf("query media: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m StationMedia
		var amplify, fadeOverlap, fadeIn, fadeOut, cueIn, cueOut sql.NullFloat64
		if err := rows.Scan(&m.ID, &m.StorageID, &m.Title, &m.Artist, &m.Album, &m.Genre,
			&m.Length, &m.Path, &amplify, &fadeOverlap, &fadeIn, &fadeOut, &cueIn, &cueOut); err != nil {
			i.logger.Error().Err(err).Msg("scan media")
			i.stats.ErrorsEncountered++
			continue
		}

		// Get station ID from storage location
		// Note: AzuraCast storage_location_id maps to station_id in most cases
		stationID, ok := stationMap[m.StorageID]
		if !ok {
			i.logger.Warn().Int("storage_id", m.StorageID).Msg("media for unknown station")
			continue
		}

		// Copy or link media file
		var destPath string
		if i.options.MediaCopyMethod != "none" {
			srcPath := filepath.Join(mediaDir, m.Path)
			if _, err := os.Stat(srcPath); err == nil {
				// Destination: /media/<station_id>/<filename>
				destPath = filepath.Join("/media", stationID, filepath.Base(m.Path))
				destDir := filepath.Dir(destPath)

				if !i.options.DryRun {
					if err := os.MkdirAll(destDir, 0755); err != nil {
						i.logger.Error().Err(err).Str("dir", destDir).Msg("create media dir")
						i.stats.ErrorsEncountered++
						continue
					}

					if i.options.MediaCopyMethod == "copy" {
						if err := copyFile(srcPath, destPath); err != nil {
							i.logger.Error().Err(err).Str("src", srcPath).Msg("copy media file")
							i.stats.ErrorsEncountered++
							continue
						}
					} else if i.options.MediaCopyMethod == "symlink" {
						if err := os.Symlink(srcPath, destPath); err != nil {
							i.logger.Error().Err(err).Str("src", srcPath).Msg("symlink media file")
							i.stats.ErrorsEncountered++
							continue
						}
					}
				}
			} else {
				i.logger.Warn().Str("path", srcPath).Msg("media file not found in backup")
			}
		}

		// Create Grimnir media record
		cuePoints := models.CuePointSet{}
		if cueIn.Valid {
			cuePoints.OutroIn = cueIn.Float64
		}
		if cueOut.Valid {
			cuePoints.OutroIn = cueOut.Float64
		}

		media := &models.MediaItem{
			ID:            uuid.New().String(),
			StationID:     stationID,
			Title:         m.Title,
			Artist:        m.Artist,
			Album:         m.Album,
			Genre:         m.Genre,
			Duration:      time.Duration(m.Length) * time.Second,
			Path:          destPath,
			CuePoints:     cuePoints,
			AnalysisState: models.AnalysisComplete, // Mark as complete since we have metadata
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(media).Error; err != nil {
				i.logger.Error().Err(err).Str("title", m.Title).Msg("create media")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.MediaImported++
	}

	return rows.Err()
}

// importPlaylists imports playlists as smart blocks
func (i *Importer) importPlaylists(ctx context.Context, azuraDB *sql.DB, stationMap map[int]string) error {
	rows, err := azuraDB.QueryContext(ctx, `
		SELECT id, station_id, name, type, source, is_enabled, weight, include_in_automation, remote_url
		FROM station_playlists
	`)
	if err != nil {
		return fmt.Errorf("query playlists: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p StationPlaylist
		var remoteURL sql.NullString
		if err := rows.Scan(&p.ID, &p.StationID, &p.Name, &p.Type, &p.Source, &p.IsEnabled, &p.Weight, &p.IncludeInAutomation, &remoteURL); err != nil {
			i.logger.Error().Err(err).Msg("scan playlist")
			i.stats.ErrorsEncountered++
			continue
		}

		stationID, ok := stationMap[p.StationID]
		if !ok {
			i.logger.Warn().Int("station_id", p.StationID).Msg("playlist for unknown station")
			continue
		}

		if remoteURL.Valid {
			p.RemoteURL = remoteURL.String
		}

		// Convert to Smart Block or Webstream
		if p.Source == "remote_url" && p.RemoteURL != "" {
			// Create webstream
			webstream := &models.Webstream{
				ID:          uuid.New().String(),
				StationID:   stationID,
				Name:        p.Name,
				Description: "Imported from AzuraCast",
				URLs:        []string{p.RemoteURL},
				Active:      p.IsEnabled,
			}

			if !i.options.DryRun {
				if err := i.db.WithContext(ctx).Create(webstream).Error; err != nil {
					i.logger.Error().Err(err).Str("webstream", p.Name).Msg("create webstream")
					i.stats.ErrorsEncountered++
					continue
				}
			}
		} else {
			// Create smart block
			// Convert order type to rules/sequence
			rules := make(map[string]any)
			sequence := make(map[string]any)

			// Store ordering preference in sequence config
			sequence["order"] = convertPlaylistOrder(p.Order)
			sequence["limit"] = 10 // Default limit

			smartBlock := &models.SmartBlock{
				ID:          uuid.New().String(),
				StationID:   stationID,
				Name:        p.Name,
				Description: fmt.Sprintf("Imported from AzuraCast (Type: %s, Weight: %d)", p.Type, p.Weight),
				Rules:       rules,
				Sequence:    sequence,
			}

			if !i.options.DryRun {
				if err := i.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
					i.logger.Error().Err(err).Str("smart_block", p.Name).Msg("create smart block")
					i.stats.ErrorsEncountered++
					continue
				}
			}
		}

		i.stats.PlaylistsImported++
	}

	return rows.Err()
}

// importSchedules imports schedules as clocks
func (i *Importer) importSchedules(ctx context.Context, azuraDB *sql.DB, stationMap map[int]string) error {
	// First, query all schedules with their associated playlists
	rows, err := azuraDB.QueryContext(ctx, `
		SELECT s.id, s.playlist_id, s.start_time, s.end_time, s.start_date, s.end_date, s.days, s.loop_once,
		       p.station_id, p.name
		FROM station_schedules s
		JOIN station_playlists p ON s.playlist_id = p.id
		ORDER BY p.station_id, s.start_time
	`)
	if err != nil {
		return fmt.Errorf("query schedules: %w", err)
	}
	defer rows.Close()

	// Group schedules by station
	schedulesByStation := make(map[string][]StationSchedule)

	for rows.Next() {
		var s StationSchedule
		var azuraStationID int
		var playlistName string
		var startDate, endDate sql.NullString

		if err := rows.Scan(&s.ID, &s.PlaylistID, &s.StartTime, &s.EndTime,
			&startDate, &endDate, &s.Days, &s.LoopOnce, &azuraStationID, &playlistName); err != nil {
			i.logger.Error().Err(err).Msg("scan schedule")
			i.stats.ErrorsEncountered++
			continue
		}

		stationID, ok := stationMap[azuraStationID]
		if !ok {
			i.logger.Warn().Int("station_id", azuraStationID).Msg("schedule for unknown station")
			continue
		}

		if startDate.Valid {
			s.StartDate = &startDate.String
		}
		if endDate.Valid {
			s.EndDate = &endDate.String
		}

		schedulesByStation[stationID] = append(schedulesByStation[stationID], s)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Create clocks for each station
	for stationID, schedules := range schedulesByStation {
		// Group schedules by hour to create clock templates
		hourSchedules := make(map[int][]StationSchedule)
		for _, sched := range schedules {
			hour := sched.StartTime / 60 // Convert minutes to hour
			hourSchedules[hour] = append(hourSchedules[hour], sched)
		}

		// Create a clock template for each hour
		for hour := range hourSchedules {
			clockHour := &models.ClockHour{
				ID:        uuid.New().String(),
				StationID: stationID,
				Name:      fmt.Sprintf("Hour %02d:00 (Imported)", hour),
			}

			if !i.options.DryRun {
				if err := i.db.WithContext(ctx).Create(clockHour).Error; err != nil {
					i.logger.Error().Err(err).Str("station", stationID).Msg("create clock hour")
					i.stats.ErrorsEncountered++
					continue
				}
			}

			i.stats.SchedulesImported++

			i.logger.Info().
				Str("station_id", stationID).
				Int("hour", hour).
				Str("clock_id", clockHour.ID).
				Msg("imported clock hour")
		}
	}

	return nil
}

// importUsers imports users
func (i *Importer) importUsers(ctx context.Context, azuraDB *sql.DB) error {
	rows, err := azuraDB.QueryContext(ctx, `
		SELECT id, email, name, locale
		FROM users
	`)
	if err != nil {
		return fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u User
		var locale sql.NullString
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &locale); err != nil {
			i.logger.Error().Err(err).Msg("scan user")
			i.stats.ErrorsEncountered++
			continue
		}

		if locale.Valid {
			u.Locale = locale.String
		}

		// Create Grimnir user
		// Note: Password cannot be migrated (different hashing algorithm)
		// Generate a temporary password that must be reset
		user := &models.User{
			ID:           uuid.New().String(),
			Email:        u.Email,
			Password:     uuid.New().String(), // Random password, user must reset
			PlatformRole: models.PlatformRoleUser, // Default platform role
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(user).Error; err != nil {
				i.logger.Error().Err(err).Str("email", u.Email).Msg("create user")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.UsersImported++

		i.logger.Info().
			Str("email", u.Email).
			Msg("imported user (password must be reset)")
	}

	i.logger.Warn().Msg("imported users require password reset (passwords cannot be migrated)")
	return rows.Err()
}

// convertPlaylistOrder converts AzuraCast order to Grimnir order type
func convertPlaylistOrder(order string) string {
	switch strings.ToLower(order) {
	case "shuffle", "random":
		return "shuffle"
	case "sequential":
		return "sequential"
	default:
		return "shuffle"
	}
}

// reportProgress calls the progress callback if set
func (i *Importer) reportProgress(step, total int, message string) {
	if i.progress != nil {
		progress := migration.Progress{
			Phase:          "importing",
			TotalSteps:     total,
			CompletedSteps: step,
			CurrentStep:    message,
			Percentage:     float64(step) / float64(total) * 100,
		}
		i.progress(progress)
	}
	i.logger.Info().
		Int("step", step).
		Int("total", total).
		Str("message", message).
		Msg("import progress")
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	return destination.Sync()
}
