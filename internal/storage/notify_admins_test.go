/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package storage

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestNotifyAdmins_MatchesNormalizedAndLegacyRoles guards #86: notifyAdmins
// filters platform_role IN (platform_admin, 'admin') so it still reaches
// un-normalized legacy admin rows, but no fixture seeded a legacy 'admin' user,
// so that half was unproven. Assert both admins are notified and a regular user
// is not.
func TestNotifyAdmins_MatchesNormalizedAndLegacyRoles(t *testing.T) {
	db := monitorTestDB(t)
	m := NewMonitor(db, MonitorConfig{}, zerolog.Nop())

	db.Create(&models.User{ID: "a1", Email: "a1@t.local", PlatformRole: models.PlatformRoleAdmin})
	db.Create(&models.User{ID: "a2", Email: "a2@t.local", PlatformRole: models.PlatformRole("admin")}) // legacy
	db.Create(&models.User{ID: "u1", Email: "u1@t.local", PlatformRole: models.PlatformRoleUser})

	th := &threshold{Percent: 90, Severity: "warning", Subject: "Storage high"}
	if err := m.notifyAdmins(context.Background(), th, 91.0, 1000, 90); err != nil {
		t.Fatalf("notifyAdmins: %v", err)
	}

	var total int64
	db.Model(&models.Notification{}).Count(&total)
	if total != 2 {
		t.Fatalf("notifications = %d, want 2 (both admins, not the regular user)", total)
	}
	var legacy int64
	db.Model(&models.Notification{}).Where("user_id = ?", "a2").Count(&legacy)
	if legacy != 1 {
		t.Error("legacy 'admin' role user was not notified")
	}
	var regular int64
	db.Model(&models.Notification{}).Where("user_id = ?", "u1").Count(&regular)
	if regular != 0 {
		t.Error("a regular user was wrongly notified")
	}
}
