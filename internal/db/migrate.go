/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// Migrate applies database schema migrations using GORM auto-migrate.
func Migrate(database *gorm.DB) error {
	return database.AutoMigrate(
		// Platform-level models
		&models.User{},
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
		&models.ScheduleAnalytics{},
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
	)
}
