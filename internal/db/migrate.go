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
		&models.PlatformGroup{},
		&models.PlatformGroupMember{},

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

		// Migration jobs
		&migration.Job{},
	)
}
