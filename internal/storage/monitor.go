/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package storage

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Severity tiers for storage warnings.
const (
	severityWarning   = "warning"
	severityCritical  = "critical"
	severityEmergency = "emergency"
)

// threshold defines a disk-usage threshold that triggers a notification.
type threshold struct {
	Percent  float64
	Severity string
	Subject  string
}

var defaultThresholds = []threshold{
	{Percent: 95, Severity: severityEmergency, Subject: "EMERGENCY: Storage almost full"},
	{Percent: 90, Severity: severityCritical, Subject: "CRITICAL: Storage usage high"},
	{Percent: 80, Severity: severityWarning, Subject: "WARNING: Storage usage elevated"},
}

// MonitorConfig holds storage monitor configuration.
type MonitorConfig struct {
	MediaRoot     string
	CheckInterval time.Duration
}

// Monitor periodically checks disk usage of the media volume and creates
// in-app notifications for platform admins when configurable thresholds
// are exceeded.  It remembers the last notified severity so each
// threshold crossing triggers at most one notification.
type Monitor struct {
	db     *gorm.DB
	cfg    MonitorConfig
	logger zerolog.Logger

	mu                sync.Mutex
	lastNotifiedLevel string // severity of the last notification sent
}

// NewMonitor creates a new storage monitor.
func NewMonitor(db *gorm.DB, cfg MonitorConfig, logger zerolog.Logger) *Monitor {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 30 * time.Minute
	}
	return &Monitor{
		db:     db,
		cfg:    cfg,
		logger: logger.With().Str("component", "storage-monitor").Logger(),
	}
}

// Start runs the monitor loop until ctx is cancelled.
func (m *Monitor) Start(ctx context.Context) {
	m.logger.Info().
		Str("media_root", m.cfg.MediaRoot).
		Dur("interval", m.cfg.CheckInterval).
		Msg("storage monitor starting")

	// Run an initial check immediately.
	m.check(ctx)

	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info().Msg("storage monitor stopping")
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

// diskUsage returns total, free, and used-percent for the filesystem that
// contains path.
func diskUsage(path string) (total, free uint64, usedPct float64, err error) {
	var stat syscall.Statfs_t
	if err = syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free = stat.Bavail * uint64(stat.Bsize) // available to non-root
	used := total - free
	if total > 0 {
		usedPct = float64(used) / float64(total) * 100
	}
	return total, free, usedPct, nil
}

func (m *Monitor) check(ctx context.Context) {
	total, free, usedPct, err := diskUsage(m.cfg.MediaRoot)
	if err != nil {
		m.logger.Error().Err(err).Msg("failed to check disk usage")
		return
	}

	m.logger.Debug().
		Float64("used_pct", usedPct).
		Str("total", humanBytes(total)).
		Str("free", humanBytes(free)).
		Msg("disk usage checked")

	// Determine which threshold (if any) has been crossed.
	// Thresholds are ordered highest-first so we match the most severe one.
	var matched *threshold
	for i := range defaultThresholds {
		if usedPct >= defaultThresholds[i].Percent {
			matched = &defaultThresholds[i]
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if matched == nil {
		// Usage is below all thresholds — reset so we re-notify if it rises again.
		if m.lastNotifiedLevel != "" {
			m.logger.Info().Float64("used_pct", usedPct).Msg("disk usage returned below thresholds, resetting notification state")
			m.lastNotifiedLevel = ""
		}
		return
	}

	// Only notify if the severity has increased since the last notification.
	if severityRank(matched.Severity) <= severityRank(m.lastNotifiedLevel) {
		return
	}

	m.logger.Warn().
		Float64("used_pct", usedPct).
		Str("severity", matched.Severity).
		Str("total", humanBytes(total)).
		Str("free", humanBytes(free)).
		Msg("disk usage threshold crossed, notifying platform admins")

	if err := m.notifyAdmins(ctx, matched, usedPct, total, free); err != nil {
		m.logger.Error().Err(err).Msg("failed to create storage warning notifications")
		return
	}

	m.lastNotifiedLevel = matched.Severity
}

// notifyAdmins creates an in-app notification for every platform admin.
func (m *Monitor) notifyAdmins(ctx context.Context, t *threshold, usedPct float64, total, free uint64) error {
	var admins []models.User
	if err := m.db.WithContext(ctx).Where("platform_role IN ?", []models.PlatformRole{
		models.PlatformRoleAdmin,
		"admin", // normalised form handled at read time
	}).Find(&admins).Error; err != nil {
		return fmt.Errorf("query admins: %w", err)
	}

	if len(admins) == 0 {
		m.logger.Warn().Msg("no platform admins found to notify about storage usage")
		return nil
	}

	body := fmt.Sprintf(
		"Media storage is at %.1f%% capacity.\n\nTotal: %s\nAvailable: %s\nSeverity: %s\n\nConsider removing unused media or expanding the storage volume.",
		usedPct, humanBytes(total), humanBytes(free), t.Severity,
	)

	now := time.Now()
	for _, admin := range admins {
		notification := &models.Notification{
			ID:               uuid.NewString(),
			UserID:           admin.ID,
			NotificationType: models.NotificationTypeStorageWarning,
			Channel:          models.NotificationChannelInApp,
			Subject:          t.Subject,
			Body:             body,
			Status:           models.NotificationStatusSent,
			SentAt:           &now,
			ReferenceType:    "system",
			ReferenceID:      "",
			Metadata: map[string]any{
				"used_pct": usedPct,
				"total":    total,
				"free":     free,
				"severity": t.Severity,
			},
			CreatedAt: now,
		}

		if err := m.db.WithContext(ctx).Create(notification).Error; err != nil {
			m.logger.Error().Err(err).Str("user_id", admin.ID).Msg("failed to save storage notification")
			// Continue to next admin rather than aborting entirely.
		}
	}

	return nil
}

// severityRank returns a numeric rank so we can compare severity levels.
func severityRank(s string) int {
	switch s {
	case severityWarning:
		return 1
	case severityCritical:
		return 2
	case severityEmergency:
		return 3
	default:
		return 0
	}
}

// humanBytes formats a byte count as a human-readable string.
func humanBytes(b uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
