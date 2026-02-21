package integrity

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestScanDetectsFindings(t *testing.T) {
	db := openIntegrityTestDB(t)
	seedIntegrityFixtures(t, db)

	svc := NewService(db, zerolog.Nop())
	report, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if report.Total < 5 {
		t.Fatalf("expected at least 5 findings, got %d", report.Total)
	}
	for _, ft := range []FindingType{
		FindingStationMissingMount,
		FindingStationOwnerMembershipGap,
		FindingOrphanStationUser,
		FindingOrphanShowInstance,
		FindingShowInstanceStationMismatch,
	} {
		if report.ByType[ft] == 0 {
			t.Fatalf("expected finding type %s", ft)
		}
	}
}

func TestRepairActionsAreIdempotent(t *testing.T) {
	tests := []struct {
		name       string
		finding    FindingType
		resourceID string
		verify     func(t *testing.T, db *gorm.DB)
	}{
		{
			name:       "station_missing_mount",
			finding:    FindingStationMissingMount,
			resourceID: "station-no-mount",
			verify: func(t *testing.T, db *gorm.DB) {
				var count int64
				if err := db.Model(&models.Mount{}).Where("station_id = ?", "station-no-mount").Count(&count).Error; err != nil {
					t.Fatalf("count mounts: %v", err)
				}
				if count == 0 {
					t.Fatalf("expected mount to be created")
				}
			},
		},
		{
			name:       "station_owner_membership_gap",
			finding:    FindingStationOwnerMembershipGap,
			resourceID: "station-owner-gap",
			verify: func(t *testing.T, db *gorm.DB) {
				var su models.StationUser
				if err := db.Where("station_id = ? AND user_id = ?", "station-owner-gap", "owner-gap-user").First(&su).Error; err != nil {
					t.Fatalf("owner membership missing: %v", err)
				}
				if su.Role != models.StationRoleOwner {
					t.Fatalf("expected owner role, got %s", su.Role)
				}
			},
		},
		{
			name:       "orphan_station_user",
			finding:    FindingOrphanStationUser,
			resourceID: "orphan-station-user",
			verify: func(t *testing.T, db *gorm.DB) {
				var count int64
				if err := db.Model(&models.StationUser{}).Where("id = ?", "orphan-station-user").Count(&count).Error; err != nil {
					t.Fatalf("count station_user: %v", err)
				}
				if count != 0 {
					t.Fatalf("expected orphan station_user deleted")
				}
			},
		},
		{
			name:       "orphan_show_instance",
			finding:    FindingOrphanShowInstance,
			resourceID: "orphan-instance",
			verify: func(t *testing.T, db *gorm.DB) {
				var count int64
				if err := db.Model(&models.ShowInstance{}).Where("id = ?", "orphan-instance").Count(&count).Error; err != nil {
					t.Fatalf("count show instance: %v", err)
				}
				if count != 0 {
					t.Fatalf("expected orphan show instance deleted")
				}
			},
		},
		{
			name:       "show_instance_station_mismatch",
			finding:    FindingShowInstanceStationMismatch,
			resourceID: "mismatch-instance",
			verify: func(t *testing.T, db *gorm.DB) {
				var inst models.ShowInstance
				if err := db.First(&inst, "id = ?", "mismatch-instance").Error; err != nil {
					t.Fatalf("load show instance: %v", err)
				}
				if inst.StationID != "station-show-parent" {
					t.Fatalf("expected station-show-parent, got %s", inst.StationID)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openIntegrityTestDB(t)
			seedIntegrityFixtures(t, db)
			svc := NewService(db, zerolog.Nop())

			first, err := svc.Repair(context.Background(), RepairInput{
				Type:       tc.finding,
				ResourceID: tc.resourceID,
			})
			if err != nil {
				t.Fatalf("repair failed: %v", err)
			}
			if !first.Changed {
				t.Fatalf("expected first repair to change state, message=%s", first.Message)
			}

			second, err := svc.Repair(context.Background(), RepairInput{
				Type:       tc.finding,
				ResourceID: tc.resourceID,
			})
			if err != nil {
				t.Fatalf("second repair failed: %v", err)
			}
			if second.Changed {
				t.Fatalf("expected second repair to be idempotent no-op")
			}

			tc.verify(t, db)
		})
	}
}

func openIntegrityTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.Mount{},
		&models.StationUser{},
		&models.Show{},
		&models.ShowInstance{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedIntegrityFixtures(t *testing.T, db *gorm.DB) {
	t.Helper()

	users := []models.User{
		{ID: "owner-gap-user", Email: "gap@example.com", PlatformRole: models.PlatformRoleUser},
		{ID: "normal-user", Email: "normal@example.com", PlatformRole: models.PlatformRoleUser},
	}
	for _, u := range users {
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	stations := []models.Station{
		{ID: "station-no-mount", Name: "No Mount Station", OwnerID: "normal-user"},
		{ID: "station-owner-gap", Name: "Owner Gap Station", OwnerID: "owner-gap-user"},
		{ID: "station-with-mount", Name: "Good Station", OwnerID: "normal-user"},
		{ID: "station-show-parent", Name: "Show Parent Station", OwnerID: "normal-user"},
		{ID: "station-show-wrong", Name: "Wrong Station", OwnerID: "normal-user"},
	}
	for _, s := range stations {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed station: %v", err)
		}
	}

	mount := models.Mount{
		ID:         "mount-good",
		StationID:  "station-with-mount",
		Name:       "good",
		URL:        "/good",
		Format:     "mp3",
		Bitrate:    128,
		Channels:   2,
		SampleRate: 44100,
	}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	goodMembership := models.StationUser{
		ID:        "membership-good",
		UserID:    "normal-user",
		StationID: "station-with-mount",
		Role:      models.StationRoleOwner,
	}
	if err := db.Create(&goodMembership).Error; err != nil {
		t.Fatalf("seed good membership: %v", err)
	}

	orphanMembership := models.StationUser{
		ID:        "orphan-station-user",
		UserID:    "missing-user",
		StationID: "station-with-mount",
		Role:      models.StationRoleDJ,
	}
	if err := db.Create(&orphanMembership).Error; err != nil {
		t.Fatalf("seed orphan membership: %v", err)
	}

	show := models.Show{
		ID:                     "show-parent",
		StationID:              "station-show-parent",
		Name:                   "Test Show",
		DefaultDurationMinutes: 60,
		DTStart:                time.Now().UTC(),
		Timezone:               "UTC",
	}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	orphanInstance := models.ShowInstance{
		ID:        "orphan-instance",
		ShowID:    "missing-show",
		StationID: "station-show-parent",
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	if err := db.Create(&orphanInstance).Error; err != nil {
		t.Fatalf("seed orphan instance: %v", err)
	}

	mismatchInstance := models.ShowInstance{
		ID:        "mismatch-instance",
		ShowID:    "show-parent",
		StationID: "station-show-wrong",
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	if err := db.Create(&mismatchInstance).Error; err != nil {
		t.Fatalf("seed mismatch instance: %v", err)
	}
}
