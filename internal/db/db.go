/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// gormStdLogger writes gorm's output to stdout, where the container log
// collector reads it, matching where gorm's own default writer sends it.
var gormStdLogger = log.New(os.Stdout, "", log.LstdFlags)

// gormLogWriter satisfies gorm's logger.Writer. It exists to drop the "\r\n"
// prefix gorm's default writer carries, which emitted a blank line ahead of
// every gorm message: 75,090 of the 930,783 lines in the 2026-08-11 prod
// capture were those blanks.
type gormLogWriter struct{}

func (gormLogWriter) Printf(format string, args ...any) {
	gormStdLogger.Printf(format, args...)
}

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
		// Warn level: logs slow queries and errors only.
		// Info would log every SQL statement, generating ~500K lines/day in
		// production and evicting useful log entries from Docker's ring buffer.
		//
		// Warn alone was not enough. gorm treats ErrRecordNotFound as an error
		// and logs it regardless of level unless told otherwise, and a miss is
		// normal here: the live-session lookup polls for a row that usually does
		// not exist. In a 3.5h prod capture (2026-08-11) that one query wrote
		// 104,492 of 930,783 lines, a "record not found" plus the full SELECT
		// every time, at about four per second. Slow queries and real errors are
		// still logged.
		Logger: logger.New(gormLogWriter{}, logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
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
