/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newLPService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.LandingPage{}, &models.LandingPageVersion{}, &models.LandingPageAsset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(db, nil, t.TempDir(), zerolog.Nop()), db
}

func bg() context.Context { return context.Background() }

func TestGetOrCreate_Idempotent(t *testing.T) {
	svc, db := newLPService(t)
	p1, err := svc.GetOrCreate(bg(), "st1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p1.Theme != "default" || p1.PublishedConfig == nil {
		t.Fatalf("new page not seeded with defaults: %+v", p1)
	}
	p2, _ := svc.GetOrCreate(bg(), "st1")
	if p2.ID != p1.ID {
		t.Fatal("GetOrCreate should return the existing page, not a new one")
	}
	var n int64
	db.Model(&models.LandingPage{}).Count(&n)
	if n != 1 {
		t.Fatalf("expected exactly 1 row, got %d", n)
	}
}

func TestGetOrCreatePlatform_AndGetPlatform(t *testing.T) {
	svc, _ := newLPService(t)

	// GetPlatform before creation returns ErrNotFound.
	if _, err := svc.GetPlatform(bg()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPlatform before create = %v, want ErrNotFound", err)
	}

	p, err := svc.GetOrCreatePlatform(bg())
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	if p.StationID != nil {
		t.Fatal("platform page should have nil StationID")
	}
	// Idempotent.
	again, _ := svc.GetOrCreatePlatform(bg())
	if again.ID != p.ID {
		t.Fatal("platform GetOrCreate not idempotent")
	}
	if _, err := svc.GetPlatform(bg()); err != nil {
		t.Fatalf("GetPlatform after create: %v", err)
	}
}

func TestConfigGetters(t *testing.T) {
	svc, _ := newLPService(t)

	// Station getters seed via GetOrCreate; with no draft, draft falls back to published.
	pub, err := svc.GetPublished(bg(), "st1")
	if err != nil || pub == nil {
		t.Fatalf("GetPublished: %v", err)
	}
	draft, err := svc.GetDraft(bg(), "st1")
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if len(draft) != len(pub) {
		t.Fatal("draft should fall back to published when no draft is set")
	}

	// Platform variants.
	if _, err := svc.GetPlatformPublished(bg()); err != nil {
		t.Fatalf("GetPlatformPublished: %v", err)
	}
	if _, err := svc.GetPlatformDraft(bg()); err != nil {
		t.Fatalf("GetPlatformDraft: %v", err)
	}
}

func TestGet_NotFoundThenFound(t *testing.T) {
	svc, _ := newLPService(t)
	if _, err := svc.Get(bg(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
	svc.GetOrCreate(bg(), "st1")
	if _, err := svc.Get(bg(), "st1"); err != nil {
		t.Fatalf("Get existing: %v", err)
	}
}

func TestUpdateThemeCSSHead(t *testing.T) {
	svc, db := newLPService(t)
	svc.GetOrCreate(bg(), "st1")

	// Unknown theme is rejected.
	if err := svc.UpdateTheme(bg(), "st1", "no-such-theme"); err == nil {
		t.Fatal("expected error for unknown theme")
	}
	valid := BuiltInThemes[0].ID
	if err := svc.UpdateTheme(bg(), "st1", valid); err != nil {
		t.Fatalf("update theme: %v", err)
	}
	if err := svc.UpdateCustomCSS(bg(), "st1", "body{color:red}"); err != nil {
		t.Fatalf("update css: %v", err)
	}
	if err := svc.UpdateCustomHead(bg(), "st1", "<meta>"); err != nil {
		t.Fatalf("update head: %v", err)
	}

	var page models.LandingPage
	db.Where("station_id = ?", "st1").First(&page)
	if page.Theme != valid || page.CustomCSS != "body{color:red}" || page.CustomHead != "<meta>" {
		t.Fatalf("updates not persisted: %+v", page)
	}
}

func TestListAndGetTheme(t *testing.T) {
	svc, _ := newLPService(t)
	if len(svc.ListThemes()) == 0 {
		t.Fatal("expected built-in themes")
	}
	if svc.GetTheme(BuiltInThemes[0].ID) == nil {
		t.Fatal("GetTheme should find a built-in theme")
	}
	if svc.GetTheme("nope") != nil {
		t.Fatal("GetTheme unknown should be nil")
	}
}

func TestListVersions_Empty(t *testing.T) {
	svc, _ := newLPService(t)
	svc.GetOrCreate(bg(), "st1")
	versions, total, err := svc.ListVersions(bg(), "st1", 10, 0)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != 0 || len(versions) != 0 {
		t.Fatalf("expected no versions yet, got total=%d len=%d", total, len(versions))
	}
}
