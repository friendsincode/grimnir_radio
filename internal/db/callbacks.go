/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package db

import (
	"time"

	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"gorm.io/gorm"
)

const (
	_startTime = "gorm:start_time"
)

// RegisterCallbacks registers telemetry callbacks for GORM operations.
func RegisterCallbacks(db *gorm.DB) error {
	// Register callbacks for all CRUD operations
	if err := registerQueryCallbacks(db); err != nil {
		return err
	}

	if err := registerCreateCallbacks(db); err != nil {
		return err
	}

	if err := registerUpdateCallbacks(db); err != nil {
		return err
	}

	if err := registerDeleteCallbacks(db); err != nil {
		return err
	}

	return nil
}

func registerQueryCallbacks(db *gorm.DB) error {
	// Before query - record start time
	if err := db.Callback().Query().Before("gorm:query").Register("telemetry:before_query", beforeCallback); err != nil {
		return err
	}

	// After query - record metrics
	if err := db.Callback().Query().After("gorm:query").Register("telemetry:after_query", afterCallback("query")); err != nil {
		return err
	}

	return nil
}

func registerCreateCallbacks(db *gorm.DB) error {
	if err := db.Callback().Create().Before("gorm:create").Register("telemetry:before_create", beforeCallback); err != nil {
		return err
	}

	if err := db.Callback().Create().After("gorm:create").Register("telemetry:after_create", afterCallback("create")); err != nil {
		return err
	}

	return nil
}

func registerUpdateCallbacks(db *gorm.DB) error {
	if err := db.Callback().Update().Before("gorm:update").Register("telemetry:before_update", beforeCallback); err != nil {
		return err
	}

	if err := db.Callback().Update().After("gorm:update").Register("telemetry:after_update", afterCallback("update")); err != nil {
		return err
	}

	return nil
}

func registerDeleteCallbacks(db *gorm.DB) error {
	if err := db.Callback().Delete().Before("gorm:delete").Register("telemetry:before_delete", beforeCallback); err != nil {
		return err
	}

	if err := db.Callback().Delete().After("gorm:delete").Register("telemetry:after_delete", afterCallback("delete")); err != nil {
		return err
	}

	return nil
}

// beforeCallback records the start time before a database operation.
func beforeCallback(db *gorm.DB) {
	db.InstanceSet(_startTime, time.Now())
}

// afterCallback creates a callback that records metrics after a database operation.
func afterCallback(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		// Get start time
		startTimeValue, exists := db.InstanceGet(_startTime)
		if !exists {
			return
		}

		startTime, ok := startTimeValue.(time.Time)
		if !ok {
			return
		}

		// Calculate duration
		duration := time.Since(startTime).Seconds()

		// Get table name
		tableName := db.Statement.Table
		if tableName == "" {
			tableName = "unknown"
		}

		// Record duration
		telemetry.DatabaseQueryDuration.WithLabelValues(operation, tableName).Observe(duration)

		// Record error if any
		if db.Error != nil && db.Error != gorm.ErrRecordNotFound {
			telemetry.DatabaseErrorsTotal.WithLabelValues(operation, "query_error").Inc()
		}
	}
}

// UpdateConnectionMetrics updates connection pool metrics.
// Should be called periodically (e.g., every 30 seconds).
func UpdateConnectionMetrics(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}

	stats := sqlDB.Stats()
	telemetry.DatabaseConnectionsActive.Set(float64(stats.OpenConnections))
}
