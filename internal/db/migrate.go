/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"fmt"

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

func normalizeLegacyPlatformRoles(database *gorm.DB) error {
	if err := database.Exec("UPDATE users SET platform_role = ? WHERE LOWER(TRIM(platform_role)) = ?", models.PlatformRoleAdmin, "admin").Error; err != nil {
		return fmt.Errorf("normalize legacy admin platform role: %w", err)
	}
	if err := database.Exec("UPDATE users SET platform_role = ? WHERE LOWER(TRIM(platform_role)) IN ?", models.PlatformRoleMod, []string{"manager", "mod", "moderator"}).Error; err != nil {
		return fmt.Errorf("normalize legacy moderator platform role: %w", err)
	}
	return nil
}
