/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newRenderer(t *testing.T) (*Renderer, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	r, err := NewRenderer(db)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r, db
}

func TestNewRenderer_ParsesTemplates(t *testing.T) {
	r, _ := newRenderer(t)
	if r.templates == nil {
		t.Fatal("templates not parsed")
	}
}

func TestRenderWidget_StaticText(t *testing.T) {
	r, _ := newRenderer(t)
	station := &models.Station{ID: "st1", Name: "Test FM"}
	theme := &BuiltInThemes[0]

	widget := WidgetConfig{ID: "w1", Type: WidgetText, Config: map[string]any{"content": "hello world"}}
	html, err := r.RenderWidget(bg(), station, widget, theme)
	if err != nil {
		t.Fatalf("RenderWidget: %v", err)
	}
	if strings.TrimSpace(string(html)) == "" {
		t.Fatal("expected non-empty widget HTML")
	}
}

func TestRenderWidget_InvalidType(t *testing.T) {
	r, _ := newRenderer(t)
	station := &models.Station{ID: "st1"}
	theme := &BuiltInThemes[0]

	_, err := r.RenderWidget(bg(), station, WidgetConfig{Type: "no-such-widget"}, theme)
	if !errors.Is(err, ErrInvalidWidgetType) {
		t.Fatalf("RenderWidget(bad type) err = %v, want ErrInvalidWidgetType", err)
	}
}

func TestRenderPage_RendersWidgets(t *testing.T) {
	r, _ := newRenderer(t)
	station := &models.Station{ID: "st1", Name: "Test FM"}
	theme := &BuiltInThemes[0]

	config := map[string]any{
		"content": map[string]any{
			"widgets": []any{
				map[string]any{"id": "w1", "type": "text", "config": map[string]any{"content": "hi"}},
				map[string]any{"id": "w2", "type": "divider", "config": map[string]any{}},
			},
		},
	}
	html, err := r.RenderPage(bg(), station, config, theme, "body{}", "<meta>")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if strings.TrimSpace(string(html)) == "" {
		t.Fatal("expected non-empty page HTML")
	}
}

func TestRenderWidget_RecentTracks_FetchesFromDB(t *testing.T) {
	r, db := newRenderer(t)
	station := &models.Station{ID: "st1", Name: "Test FM"}
	theme := &BuiltInThemes[0]

	if err := db.Create(&models.PlayHistory{
		ID: "h1", StationID: "st1", Artist: "Artist", Title: "Song",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}

	widget := WidgetConfig{ID: "w1", Type: WidgetRecentTracks, Config: map[string]any{"count": 5}}
	html, err := r.RenderWidget(bg(), station, widget, theme)
	if err != nil {
		t.Fatalf("RenderWidget(recent-tracks): %v", err)
	}
	if strings.TrimSpace(string(html)) == "" {
		t.Fatal("expected non-empty recent-tracks HTML")
	}
}
