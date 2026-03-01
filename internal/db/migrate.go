/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"fmt"
	"path/filepath"

	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// Migrate applies database schema migrations using GORM auto-migrate.
func Migrate(database *gorm.DB) error {
	if err := database.AutoMigrate(
		// Platform-level models
		&models.User{},
		&models.SystemSettings{},
		&models.APIKey{},
		&models.PlatformGroup{},
		&models.PlatformGroupMember{},
		&models.AuditLog{},

		// Station-level models
		&models.Station{},
		&models.StationUser{},
		&models.StationGroup{},
		&models.StationGroupMember{},

		// Station resources
		&models.Mount{},
		&models.EncoderPreset{},
		&models.MediaItem{},
		&models.Tag{},
		&models.MediaTagLink{},
		&models.SmartBlock{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.ScheduleEntry{},
		&models.PlayHistory{},
		&models.MountPlayoutState{},
		&models.AnalysisJob{},
		&models.PrioritySource{},
		&models.ExecutorState{},
		&models.LiveSession{},
		&models.Webstream{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.Clock{},

		// Shows and scheduling (Phase 8)
		&models.Show{},
		&models.ShowInstance{},
		&models.ScheduleRule{},
		&models.ScheduleTemplate{},
		&models.ScheduleVersion{},

		// DJ self-service (Phase 8E)
		&models.DJAvailability{},
		&models.ScheduleRequest{},
		&models.ScheduleLock{},

		// Notifications (Phase 8F)
		&models.NotificationPreference{},
		&models.Notification{},

		// Webhooks (Phase 8G)
		&models.WebhookTarget{},
		&models.WebhookLog{},

		// Analytics, Syndication, Underwriting (Phase 8H)
		&models.ListenerSample{},
		&models.ScheduleAnalytics{},
		&models.ScheduleAnalyticsDaily{},
		&models.Network{},
		&models.NetworkShow{},
		&models.NetworkSubscription{},
		&models.Sponsor{},
		&models.UnderwritingObligation{},
		&models.UnderwritingSpot{},

		// Landing Page Editor (Phase 9)
		&models.LandingPage{},
		&models.LandingPageAsset{},
		&models.LandingPageVersion{},

		// Migration jobs and staged imports (Phase 10)
		&migration.Job{},
		&models.StagedImport{},

		// Orphan media tracking
		&models.OrphanMedia{},

		// WebDJ console
		&models.WebDJSession{},
		&models.WaveformCache{},
	); err != nil {
		return err
	}

	if err := applyPostgresScheduleOverlapGuard(database); err != nil {
		return err
	}
	if err := normalizeLegacyPlatformRoles(database); err != nil {
		return err
	}
	if err := migrateWebstreamHealthMethod(database); err != nil {
		return err
	}
	if err := backfillOriginalFilenames(database); err != nil {
		return err
	}

	return nil
}

// migrateWebstreamHealthMethod updates existing webstreams from HEAD to GET.
// Most Icecast/Shoutcast servers do not support HEAD requests properly.
func migrateWebstreamHealthMethod(database *gorm.DB) error {
	return database.Exec(
		"UPDATE webstreams SET health_check_method = 'GET' WHERE health_check_method = 'HEAD'",
	).Error
}

func applyPostgresScheduleOverlapGuard(database *gorm.DB) error {
	if database.Dialector.Name() != "postgres" {
		return nil
	}

	stmt := `
CREATE OR REPLACE FUNCTION prevent_station_schedule_overlap()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.ends_at <= NEW.starts_at THEN
    RAISE EXCEPTION 'schedule entry end must be after start'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM schedule_entries se
    WHERE se.station_id = NEW.station_id
      AND se.id <> NEW.id
      AND tstzrange(se.starts_at, se.ends_at, '[)') && tstzrange(NEW.starts_at, NEW.ends_at, '[)')
  ) THEN
    RAISE EXCEPTION 'overlapping programming is not allowed for station %', NEW.station_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_prevent_station_schedule_overlap ON schedule_entries;

CREATE TRIGGER trg_prevent_station_schedule_overlap
BEFORE INSERT OR UPDATE OF station_id, starts_at, ends_at
ON schedule_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_station_schedule_overlap();
`
	if err := database.Exec(stmt).Error; err != nil {
		return fmt.Errorf("apply postgres schedule overlap guard: %w", err)
	}

	return nil
}

// backfillOriginalFilenames populates original_filename for existing records
// that have an import_path or a recognisable path but no original_filename yet.
func backfillOriginalFilenames(database *gorm.DB) error {
	type row struct {
		ID         string
		ImportPath string
		Path       string
	}
	var rows []row
	if err := database.
		Model(&models.MediaItem{}).
		Select("id, import_path, path").
		Where("(original_filename IS NULL OR original_filename = '') AND (import_path != '' OR path != '')").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("backfill original filenames query: %w", err)
	}

	for _, r := range rows {
		source := r.ImportPath
		if source == "" {
			source = r.Path
		}
		name := filepath.Base(source)
		if name == "" || name == "." {
			continue
		}
		database.Model(&models.MediaItem{}).
			Where("id = ?", r.ID).
			Update("original_filename", name)
	}

	return nil
}

// RepairOriginalFilenames is a more aggressive backfill that can be called
// on-demand (e.g. from an admin endpoint) to recover original filenames.
// It tries import_path first, then path, stripping the internal
// "{uuid}.audio" naming convention when possible.
func RepairOriginalFilenames(database *gorm.DB) (updated int64, err error) {
	type row struct {
		ID         string
		ImportPath string
		Path       string
	}
	var rows []row
	if err := database.
		Model(&models.MediaItem{}).
		Select("id, import_path, path").
		Where("original_filename IS NULL OR original_filename = ''").
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("repair original filenames query: %w", err)
	}

	var count int64
	for _, r := range rows {
		name := ""
		// Prefer import_path — it preserves the source system's filename.
		if r.ImportPath != "" {
			name = filepath.Base(r.ImportPath)
		}
		// Fall back to storage path, but skip internal "{uuid}.audio" names.
		if name == "" && r.Path != "" {
			base := filepath.Base(r.Path)
			ext := filepath.Ext(base)
			if ext != ".audio" {
				name = base
			}
		}
		if name == "" || name == "." {
			continue
		}
		if err := database.Model(&models.MediaItem{}).
			Where("id = ?", r.ID).
			Update("original_filename", name).Error; err == nil {
			count++
		}
	}

	return count, nil
}

func normalizeLegacyPlatformRoles(database *gorm.DB) error {
	if err := database.Exec("UPDATE users SET platform_role = ? WHERE LOWER(TRIM(platform_role)) = ?", models.PlatformRoleAdmin, "admin").Error; err != nil {
		return fmt.Errorf("normalize legacy admin platform role: %w", err)
	}
	if err := database.Exec("UPDATE users SET platform_role = ? WHERE LOWER(TRIM(platform_role)) IN ?", models.PlatformRoleMod, []string{"manager", "mod", "moderator"}).Error; err != nil {
		return fmt.Errorf("normalize legacy moderator platform role: %w", err)
	}
	return nil
}
