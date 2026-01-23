/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package db

import (
	"fmt"
	"time"

    "github.com/friendsincode/grimnir_radio/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect establishes a gorm DB connection for the configured backend.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch cfg.DBBackend {
	case config.DatabasePostgres:
		dialector = postgres.Open(cfg.DBDSN)
	case config.DatabaseMySQL:
		dialector = mysql.Open(cfg.DBDSN)
	case config.DatabaseSQLite:
		dialector = sqlite.Open(cfg.DBDSN)
	default:
		return nil, fmt.Errorf("unknown database backend: %s", cfg.DBBackend)
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	// Register telemetry callbacks
	if err := RegisterCallbacks(db); err != nil {
		return nil, fmt.Errorf("register telemetry callbacks: %w", err)
	}

	return db, nil
}

// Close releases database resources.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
