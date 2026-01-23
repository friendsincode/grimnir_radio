package db

import (
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// Migrate applies database schema migrations using GORM auto-migrate.
func Migrate(database *gorm.DB) error {
	return database.AutoMigrate(
		&models.User{},
		&models.Station{},
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
		&migration.Job{},
	)
}
