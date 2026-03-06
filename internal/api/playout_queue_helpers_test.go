package api

import (
	"errors"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestReorderQueueItem_ClampAndNotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	seed := []models.PlayoutQueueItem{
		{ID: "q1", StationID: "s1", MountID: "m1", MediaID: "track-1", Position: 1},
		{ID: "q2", StationID: "s1", MountID: "m1", MediaID: "track-2", Position: 2},
		{ID: "q3", StationID: "s1", MountID: "m1", MediaID: "track-3", Position: 3},
	}
	for _, it := range seed {
		if err := db.Create(&it).Error; err != nil {
			t.Fatalf("seed queue item: %v", err)
		}
	}

	a := &API{db: db}
	req := httptest.NewRequest("PATCH", "/api/v1/playout/queue/q1", nil)

	// Move q1 beyond bounds; should clamp to last position.
	if err := a.reorderQueueItem(req, seed[0], 99); err != nil {
		t.Fatalf("reorder clamp failed: %v", err)
	}

	var got []models.PlayoutQueueItem
	if err := db.Where("station_id = ? AND mount_id = ?", "s1", "m1").
		Order("position ASC").
		Find(&got).Error; err != nil {
		t.Fatalf("load queue: %v", err)
	}
	if len(got) != 3 || got[2].ID != "q1" || got[2].Position != 3 {
		t.Fatalf("unexpected order after clamp reorder: %+v", got)
	}

	// Missing item in sequence should return ErrRecordNotFound.
	err = a.reorderQueueItem(req, models.PlayoutQueueItem{
		ID:        "missing",
		StationID: "s1",
		MountID:   "m1",
	}, 1)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestRequireStationQueueRole_AuthAndRoleMatrix(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su-dj",
		UserID:    "u-dj",
		StationID: "s1",
		Role:      models.StationRoleDJ,
	}).Error; err != nil {
		t.Fatalf("seed station user: %v", err)
	}

	a := &API{db: db}

	t.Run("unauthorized without claims", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/playout/queue", nil)
		rr := httptest.NewRecorder()
		ok := a.requireStationQueueRole(rr, req, "s1", true)
		if ok {
			t.Fatal("expected role check to fail")
		}
		if rr.Code != 401 {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("platform admin bypass", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/playout/queue", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID: "u-admin",
			Roles:  []string{string(models.PlatformRoleAdmin)},
		}))
		rr := httptest.NewRecorder()
		ok := a.requireStationQueueRole(rr, req, "s1", false)
		if !ok {
			t.Fatalf("expected platform admin to pass, status=%d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("dj allowed only when allowDJ=true", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/playout/queue", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID:    "u-dj",
			StationID: "s1",
			Roles:     []string{string(models.RoleDJ)},
		}))

		allowRec := httptest.NewRecorder()
		if !a.requireStationQueueRole(allowRec, req, "s1", true) {
			t.Fatalf("expected DJ to pass read role check, status=%d", allowRec.Code)
		}

		denyRec := httptest.NewRecorder()
		if a.requireStationQueueRole(denyRec, req, "s1", false) {
			t.Fatal("expected DJ mutate role check to fail")
		}
		if denyRec.Code != 403 {
			t.Fatalf("expected 403, got %d", denyRec.Code)
		}
	})
}

func TestMountBelongsToStation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Mount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID:        "m1",
		StationID: "s1",
		Name:      "Main",
		Format:    "mp3",
		URL:       "/live/main",
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	a := &API{db: db}
	req := httptest.NewRequest("GET", "/api/v1/playout/queue", nil)

	if !a.mountBelongsToStation(req, "s1", "m1") {
		t.Fatal("expected mount to belong to station")
	}
	if a.mountBelongsToStation(req, "s2", "m1") {
		t.Fatal("expected cross-station mount check to fail")
	}
	if a.mountBelongsToStation(req, "s1", "") {
		t.Fatal("expected empty mount id to fail")
	}
}
