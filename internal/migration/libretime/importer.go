/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package libretime

import (
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
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Importer handles importing from LibreTime databases
type Importer struct {
	db       *gorm.DB
	logger   zerolog.Logger
	options  migration.MigrationOptions
	stats    migration.MigrationStats
	progress migration.ProgressCallback
}

// NewImporter creates a new LibreTime importer
func NewImporter(db *gorm.DB, logger zerolog.Logger, options migration.MigrationOptions) *Importer {
	return &Importer{
		db:      db,
		logger:  logger.With().Str("component", "libretime_importer").Logger(),
		options: options,
	}
}

// SetProgressCallback sets the progress callback function
func (i *Importer) SetProgressCallback(callback migration.ProgressCallback) {
	i.progress = callback
}

// Import imports from a LibreTime database
func (i *Importer) Import(ctx context.Context, dbDSN string) (*migration.MigrationStats, error) {
	i.logger.Info().
		Str("dsn", maskDSN(dbDSN)).
		Bool("dry_run", i.options.DryRun).
		Msg("starting LibreTime import")

	// Connect to LibreTime database
	i.reportProgress(1, 10, "Connecting to LibreTime database")
	ltDB, err := sql.Open("postgres", dbDSN)
	if err != nil {
		return nil, fmt.Errorf("connect to libretime db: %w", err)
	}
	defer ltDB.Close()

	// Test connection
	if err := ltDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping libretime db: %w", err)
	}

	// Create default station (LibreTime is single-station)
	i.reportProgress(2, 10, "Creating station")
	stationID, err := i.createStation(ctx)
	if err != nil {
		return nil, fmt.Errorf("create station: %w", err)
	}

	// Import media files
	if !i.options.SkipMedia {
		i.reportProgress(3, 10, "Importing media files")
		if err := i.importMedia(ctx, ltDB, stationID); err != nil {
			return nil, fmt.Errorf("import media: %w", err)
		}
	}

	// Import webstreams
	i.reportProgress(4, 10, "Importing webstreams")
	if err := i.importWebstreams(ctx, ltDB, stationID); err != nil {
		return nil, fmt.Errorf("import webstreams: %w", err)
	}

	// Import smart blocks
	i.reportProgress(5, 10, "Importing smart blocks")
	if err := i.importSmartBlocks(ctx, ltDB, stationID); err != nil {
		return nil, fmt.Errorf("import smart blocks: %w", err)
	}

	// Import playlists
	i.reportProgress(6, 10, "Importing playlists")
	if err := i.importPlaylists(ctx, ltDB, stationID); err != nil {
		return nil, fmt.Errorf("import playlists: %w", err)
	}

	// Import shows as clocks
	i.reportProgress(7, 10, "Importing shows")
	if err := i.importShows(ctx, ltDB, stationID); err != nil {
		return nil, fmt.Errorf("import shows: %w", err)
	}

	// Import users
	i.reportProgress(8, 10, "Importing users")
	if err := i.importUsers(ctx, ltDB); err != nil {
		i.logger.Warn().Err(err).Msg("failed to import users, continuing")
		i.stats.ErrorsEncountered++
	}

	i.reportProgress(10, 10, "Import completed")

	i.logger.Info().
		Interface("stats", i.stats).
		Msg("LibreTime import completed")

	return &i.stats, nil
}

// createStation creates a default station for LibreTime import
func (i *Importer) createStation(ctx context.Context) (string, error) {
	station := &models.Station{
		ID:          uuid.New().String(),
		Name:        "LibreTime Station",
		Description: "Imported from LibreTime",
		Timezone:    "UTC",
	}

	if !i.options.DryRun {
		if err := i.db.WithContext(ctx).Create(station).Error; err != nil {
			return "", fmt.Errorf("create station: %w", err)
		}
	}

	i.stats.StationsImported++
	i.logger.Info().Str("station_id", station.ID).Msg("created station")

	return station.ID, nil
}

// importMedia imports media files from cc_files
func (i *Importer) importMedia(ctx context.Context, ltDB *sql.DB, stationID string) error {
	rows, err := ltDB.QueryContext(ctx, `
		SELECT id, name, filepath, track_title, artist_name, album_title, genre, mood,
		       year, bpm, replay_gain, length, cuein, cueout, label, language, isrc
		FROM cc_files
		WHERE ftype = 'audioclip' AND hidden = false
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var f File
		var year, bpm, replayGain, length, cueIn, cueOut sql.NullString

		if err := rows.Scan(&f.ID, &f.Name, &f.Filepath, &f.TrackTitle, &f.ArtistName,
			&f.AlbumTitle, &f.Genre, &f.Mood, &year, &bpm, &replayGain, &length,
			&cueIn, &cueOut, &f.Label, &f.Language, &f.Isrc); err != nil {
			i.logger.Error().Err(err).Msg("scan file")
			i.stats.ErrorsEncountered++
			continue
		}

		// Parse duration
		var duration time.Duration
		if length.Valid {
			duration, _ = parseLibreTimeDuration(length.String)
		}

		// Parse cue points
		cuePoints := models.CuePointSet{}
		if cueIn.Valid {
			if d, err := parseLibreTimeDuration(cueIn.String); err == nil {
				cuePoints.IntroEnd = d.Seconds()
			}
		}
		if cueOut.Valid {
			if d, err := parseLibreTimeDuration(cueOut.String); err == nil {
				cuePoints.OutroIn = d.Seconds()
			}
		}

		// Handle media file
		var destPath string
		if i.options.MediaCopyMethod != "none" {
			srcPath := f.Filepath
			if _, err := os.Stat(srcPath); err == nil {
				destPath = filepath.Join("/media", stationID, filepath.Base(srcPath))
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
				destPath = srcPath // Keep original path if file doesn't exist
			}
		}

		// Parse year as string
		yearStr := ""
		if year.Valid {
			yearStr = year.String
		}

		// Create media item
		media := &models.MediaItem{
			ID:            uuid.New().String(),
			StationID:     stationID,
			Title:         f.TrackTitle,
			Artist:        f.ArtistName,
			Album:         f.AlbumTitle,
			Genre:         f.Genre,
			Mood:          f.Mood,
			Label:         f.Label,
			Language:      f.Language,
			Year:          yearStr,
			Duration:      duration,
			Path:          destPath,
			CuePoints:     cuePoints,
			AnalysisState: models.AnalysisComplete,
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(media).Error; err != nil {
				i.logger.Error().Err(err).Str("title", f.TrackTitle).Msg("create media")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.MediaImported++

		if i.stats.MediaImported%100 == 0 {
			i.logger.Info().Int("count", i.stats.MediaImported).Msg("imported media files")
		}
	}

	return rows.Err()
}

// importWebstreams imports webstreams from cc_webstream
func (i *Importer) importWebstreams(ctx context.Context, ltDB *sql.DB, stationID string) error {
	rows, err := ltDB.QueryContext(ctx, `
		SELECT id, name, description, url, length
		FROM cc_webstream
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query webstreams: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var w Webstream
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.URL, &w.Length); err != nil {
			i.logger.Error().Err(err).Msg("scan webstream")
			i.stats.ErrorsEncountered++
			continue
		}

		webstream := &models.Webstream{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        w.Name,
			Description: w.Description,
			URLs:        []string{w.URL},
			Active:      true,
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(webstream).Error; err != nil {
				i.logger.Error().Err(err).Str("name", w.Name).Msg("create webstream")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.PlaylistsImported++
	}

	return rows.Err()
}

// importSmartBlocks imports smart blocks from cc_block
func (i *Importer) importSmartBlocks(ctx context.Context, ltDB *sql.DB, stationID string) error {
	rows, err := ltDB.QueryContext(ctx, `
		SELECT id, name, description, type, shuffle
		FROM cc_block
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query smart blocks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sb SmartBlock
		if err := rows.Scan(&sb.ID, &sb.Name, &sb.Description, &sb.Type, &sb.Shuffle); err != nil {
			i.logger.Error().Err(err).Msg("scan smart block")
			i.stats.ErrorsEncountered++
			continue
		}

		// Query criteria/rules for this smart block
		rules := make(map[string]any)
		sequence := make(map[string]any)

		criteriaRows, err := ltDB.QueryContext(ctx, `
			SELECT criteria, modifier, value
			FROM cc_blockcriteria
			WHERE block_id = $1
			ORDER BY id
		`, sb.ID)
		if err == nil {
			var criteriaList []map[string]string
			for criteriaRows.Next() {
				var criteria, modifier, value string
				if err := criteriaRows.Scan(&criteria, &modifier, &value); err == nil {
					criteriaList = append(criteriaList, map[string]string{
						"field":    criteria,
						"operator": modifier,
						"value":    value,
					})
				}
			}
			criteriaRows.Close()
			rules["criteria"] = criteriaList
		}

		// Set sequence options
		if sb.Shuffle {
			sequence["order"] = "shuffle"
		} else {
			sequence["order"] = "sequential"
		}

		smartBlock := &models.SmartBlock{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        sb.Name,
			Description: sb.Description,
			Rules:       rules,
			Sequence:    sequence,
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
				i.logger.Error().Err(err).Str("name", sb.Name).Msg("create smart block")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.PlaylistsImported++
	}

	return rows.Err()
}

// importPlaylists imports static playlists from cc_playlist
func (i *Importer) importPlaylists(ctx context.Context, ltDB *sql.DB, stationID string) error {
	rows, err := ltDB.QueryContext(ctx, `
		SELECT id, name, description
		FROM cc_playlist
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query playlists: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.Description); err != nil {
			i.logger.Error().Err(err).Msg("scan playlist")
			i.stats.ErrorsEncountered++
			continue
		}

		// Convert to smart block (static type)
		smartBlock := &models.SmartBlock{
			ID:          uuid.New().String(),
			StationID:   stationID,
			Name:        p.Name,
			Description: fmt.Sprintf("%s (Static Playlist)", p.Description),
			Rules:       make(map[string]any),
			Sequence:    map[string]any{"order": "sequential"},
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(smartBlock).Error; err != nil {
				i.logger.Error().Err(err).Str("name", p.Name).Msg("create playlist")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.PlaylistsImported++
	}

	return rows.Err()
}

// importShows imports shows as clock templates from cc_show
func (i *Importer) importShows(ctx context.Context, ltDB *sql.DB, stationID string) error {
	rows, err := ltDB.QueryContext(ctx, `
		SELECT id, name, description, genre, url, color
		FROM cc_show
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query shows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s Show
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Genre, &s.URL, &s.Color); err != nil {
			i.logger.Error().Err(err).Msg("scan show")
			i.stats.ErrorsEncountered++
			continue
		}

		// Create clock hour for this show
		clockHour := &models.ClockHour{
			ID:        uuid.New().String(),
			StationID: stationID,
			Name:      s.Name,
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(clockHour).Error; err != nil {
				i.logger.Error().Err(err).Str("name", s.Name).Msg("create clock hour")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.SchedulesImported++

		i.logger.Info().
			Str("name", s.Name).
			Str("clock_id", clockHour.ID).
			Msg("imported show as clock")
	}

	return rows.Err()
}

// importUsers imports users from cc_subjs
func (i *Importer) importUsers(ctx context.Context, ltDB *sql.DB) error {
	rows, err := ltDB.QueryContext(ctx, `
		SELECT id, login, first_name, last_name, email, type
		FROM cc_subjs
		WHERE type IN ('A', 'P', 'H', 'G') -- Admin, Program Manager, Host, Guest
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Login, &u.FirstName, &u.LastName, &u.Email, &u.Type); err != nil {
			i.logger.Error().Err(err).Msg("scan user")
			i.stats.ErrorsEncountered++
			continue
		}

		// Map LibreTime user type to Grimnir role
		role := models.RoleDJ
		switch u.Type {
		case "A": // Admin
			role = models.RoleAdmin
		case "P": // Program Manager
			role = models.RoleManager
		case "H", "G": // Host, Guest
			role = models.RoleDJ
		}

		email := u.Email
		if email == "" {
			email = u.Login + "@localhost" // Generate email if missing
		}

		user := &models.User{
			ID:       uuid.New().String(),
			Email:    email,
			Password: uuid.New().String(), // Random password, must be reset
			Role:     role,
		}

		if !i.options.DryRun {
			if err := i.db.WithContext(ctx).Create(user).Error; err != nil {
				i.logger.Error().Err(err).Str("login", u.Login).Msg("create user")
				i.stats.ErrorsEncountered++
				continue
			}
		}

		i.stats.UsersImported++

		i.logger.Info().
			Str("login", u.Login).
			Str("role", string(role)).
			Msg("imported user (password must be reset)")
	}

	i.logger.Warn().Msg("imported users require password reset (passwords cannot be migrated)")
	return rows.Err()
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

// parseLibreTimeDuration parses LibreTime duration format (HH:MM:SS.mmm)
func parseLibreTimeDuration(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid duration format: %s", s)
	}

	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	secondsParts := strings.Split(parts[2], ".")
	seconds, _ := strconv.Atoi(secondsParts[0])

	var milliseconds int
	if len(secondsParts) > 1 {
		milliseconds, _ = strconv.Atoi(secondsParts[1])
	}

	duration := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(milliseconds)*time.Millisecond

	return duration, nil
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

// maskDSN masks sensitive parts of a database DSN for logging
func maskDSN(dsn string) string {
	// Simple masking: replace password in postgres://user:password@host/db
	if strings.Contains(dsn, "://") {
		parts := strings.SplitN(dsn, "@", 2)
		if len(parts) == 2 {
			userParts := strings.SplitN(parts[0], ":", 3)
			if len(userParts) >= 2 {
				return userParts[0] + ":***@" + parts[1]
			}
		}
	}
	return dsn
}
