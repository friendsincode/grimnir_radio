/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package e2e provides end-to-end browser tests for the web UI.
package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/web"
)

// TestRoutes verifies all web routes are accessible and render correctly.
func TestRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	// Check if we should run headless
	headless := os.Getenv("E2E_HEADLESS") != "false"

	// Setup test database
	db := setupTestDB(t)

	// Create test fixtures
	setupTestFixtures(t, db)

	// Create handler
	logger := zerolog.Nop()
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Create test server with chi router
	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	// Launch browser
	l := launcher.New().Headless(headless)
	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()

	// Test cases for public routes
	publicRoutes := []struct {
		name           string
		path           string
		expectedStatus int
		mustContain    string
	}{
		{"setup page", "/setup", 200, "Setup"},
		{"landing page", "/", 200, "Grimnir"},
		{"login page", "/login", 200, "Login"},
		{"listen page", "/listen", 200, "Listen"},
		{"archive page", "/archive", 200, "Archive"},
		{"schedule page", "/schedule", 200, "Schedule"},
	}

	for _, tc := range publicRoutes {
		t.Run(tc.name, func(t *testing.T) {
			page := browser.MustPage(server.URL + tc.path)
			defer page.MustClose()

			// Wait for page to load
			err := page.WaitLoad()
			if err != nil {
				t.Fatalf("page load failed for %s: %v", tc.path, err)
			}

			// Check page content
			html := page.MustHTML()
			if !strings.Contains(html, tc.mustContain) {
				t.Errorf("expected page %s to contain %q", tc.path, tc.mustContain)
			}
		})
	}
}

// TestAuthenticatedRoutes tests routes that require authentication.
func TestAuthenticatedRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	setupTestFixtures(t, db)

	// Create admin user for testing
	adminUser := createTestUser(t, db, "admin@test.com", "password123", models.RoleAdmin)

	logger := zerolog.Nop()
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	l := launcher.New().Headless(headless)
	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()

	// First, login
	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()

	page.MustWaitLoad()

	// Fill login form
	page.MustElement("input[name=email]").MustInput(adminUser.Email)
	page.MustElement("input[name=password]").MustInput("password123")
	page.MustElement("button[type=submit]").MustClick()

	// Wait for redirect to dashboard
	page.MustWaitNavigation()

	// Now test authenticated routes
	dashboardRoutes := []struct {
		name        string
		path        string
		mustContain string
	}{
		{"dashboard home", "/dashboard", "Dashboard"},
		{"profile", "/dashboard/profile", "Profile"},
		{"stations list", "/dashboard/stations", "Station"},
		{"media list", "/dashboard/media", "Media"},
		{"playlists list", "/dashboard/playlists", "Playlist"},
		{"smart blocks list", "/dashboard/smart-blocks", "Smart Block"},
		{"clocks list", "/dashboard/clocks", "Clock"},
		{"schedule", "/dashboard/schedule", "Schedule"},
		{"live", "/dashboard/live", "Live"},
		{"webstreams list", "/dashboard/webstreams", "Webstream"},
		{"analytics", "/dashboard/analytics", "Analytics"},
		{"users list", "/dashboard/users", "User"},
		{"settings", "/dashboard/settings", "Settings"},
	}

	for _, tc := range dashboardRoutes {
		t.Run(tc.name, func(t *testing.T) {
			page.MustNavigate(server.URL + tc.path)
			page.MustWaitLoad()

			html := page.MustHTML()
			if !strings.Contains(html, tc.mustContain) {
				t.Errorf("expected page %s to contain %q", tc.path, tc.mustContain)
			}
		})
	}
}

// TestFormRoutes tests form pages (new/edit routes).
func TestFormRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	station := setupTestFixtures(t, db)
	createTestUser(t, db, "admin@test.com", "password123", models.RoleAdmin)

	logger := zerolog.Nop()
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	l := launcher.New().Headless(headless)
	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()

	// Login first
	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()
	page.MustWaitLoad()
	page.MustElement("input[name=email]").MustInput("admin@test.com")
	page.MustElement("input[name=password]").MustInput("password123")
	page.MustElement("button[type=submit]").MustClick()
	page.MustWaitNavigation()

	// Select station (required for most routes)
	page.MustNavigate(server.URL + "/dashboard/stations/select")
	page.MustWaitLoad()

	formRoutes := []struct {
		name        string
		path        string
		mustContain string
	}{
		{"new station", "/dashboard/stations/new", "New Station"},
		{"new playlist", "/dashboard/playlists/new", "New"},
		{"new smart block", "/dashboard/smart-blocks/new", "New"},
		{"new clock", "/dashboard/clocks/new", "New"},
		{"new webstream", "/dashboard/webstreams/new", "New"},
		{"new user", "/dashboard/users/new", "New User"},
		{"station mounts", "/dashboard/stations/" + station.ID + "/mounts", "Mount"},
		{"new mount", "/dashboard/stations/" + station.ID + "/mounts/new", "New Mount"},
		{"analytics history", "/dashboard/analytics/history", "History"},
		{"analytics spins", "/dashboard/analytics/spins", "Spin"},
		{"analytics listeners", "/dashboard/analytics/listeners", "Listener"},
		{"migrations", "/dashboard/settings/migrations", "Migration"},
	}

	for _, tc := range formRoutes {
		t.Run(tc.name, func(t *testing.T) {
			page.MustNavigate(server.URL + tc.path)
			page.MustWaitLoad()

			html := page.MustHTML()
			if !strings.Contains(html, tc.mustContain) {
				t.Errorf("expected page %s to contain %q", tc.path, tc.mustContain)
			}
		})
	}
}

// TestTemplateRendering verifies all templates render without errors.
func TestTemplateRendering(t *testing.T) {
	db := setupTestDB(t)
	setupTestFixtures(t, db)

	logger := zerolog.Nop()
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Use chi router since our routes use chi
	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	// Test that public routes return 200
	publicRoutes := []string{
		"/",
		"/login",
		"/setup",
		"/listen",
		"/archive",
		"/schedule",
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, path := range publicRoutes {
		t.Run("GET "+path, func(t *testing.T) {
			resp, err := client.Get(server.URL + path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d for %s", resp.StatusCode, path)
			}

			contentType := resp.Header.Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				t.Errorf("expected HTML content-type, got %s for %s", contentType, path)
			}
		})
	}
}

// TestRouteNotFound verifies 404 handling.
func TestRouteNotFound(t *testing.T) {
	db := setupTestDB(t)

	logger := zerolog.Nop()
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(server.URL + "/nonexistent-route-12345")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

// TestLoginFlow tests the complete login workflow.
func TestLoginFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	setupTestFixtures(t, db)
	createTestUser(t, db, "test@example.com", "testpass123", models.RoleDJ)

	logger := zerolog.Nop()
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	l := launcher.New().Headless(headless)
	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()

	page.MustWaitLoad()

	// Test invalid login
	page.MustElement("input[name=email]").MustInput("wrong@example.com")
	page.MustElement("input[name=password]").MustInput("wrongpass")
	page.MustElement("button[type=submit]").MustClick()

	// Wait for error message
	page.MustWaitStable()
	html := page.MustHTML()
	if !strings.Contains(html, "Invalid") && !strings.Contains(html, "error") && !strings.Contains(html, "alert") {
		t.Log("expected error message on invalid login")
	}

	// Now test valid login
	page.MustNavigate(server.URL + "/login")
	page.MustWaitLoad()

	page.MustElement("input[name=email]").MustInput("test@example.com")
	page.MustElement("input[name=password]").MustInput("testpass123")
	page.MustElement("button[type=submit]").MustClick()

	// Should redirect to dashboard
	page.MustWaitNavigation()

	currentURL := page.MustInfo().URL
	if !strings.Contains(currentURL, "/dashboard") {
		t.Errorf("expected redirect to dashboard, got %s", currentURL)
	}
}

// Helper functions

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Migrate all tables
	err = db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.Mount{},
		&models.MediaItem{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.SmartBlock{},
		&models.Clock{},
		&models.ScheduleEntry{},
		&models.LiveSession{},
		&models.Webstream{},
		&models.PlayHistory{},
	)
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func setupTestFixtures(t *testing.T, db *gorm.DB) *models.Station {
	// Create a test station
	station := &models.Station{
		ID:          "test-station-1",
		Name:        "Test Station",
		Description: "A test radio station",
		Timezone:    "UTC",
		Active:      true,
	}
	if err := db.Create(station).Error; err != nil {
		t.Fatalf("failed to create station: %v", err)
	}

	// Create a mount
	mount := &models.Mount{
		ID:        "test-mount-1",
		StationID: station.ID,
		Name:      "Main Stream",
		URL:       "http://localhost:8000/stream",
		Format:    "mp3",
	}
	if err := db.Create(mount).Error; err != nil {
		t.Fatalf("failed to create mount: %v", err)
	}

	return station
}

func createTestUser(t *testing.T, db *gorm.DB, email, password string, role models.RoleName) *models.User {
	// Hash password
	hashedPassword, err := bcryptHash(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	user := &models.User{
		ID:       fmt.Sprintf("user-%s", strings.Replace(email, "@", "-", -1)),
		Email:    email,
		Password: hashedPassword,
		Role:     role,
	}

	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	return user
}

func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// BenchmarkPageLoad benchmarks page loading times.
func BenchmarkPageLoad(b *testing.B) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.Station{})

	logger := zerolog.Nop()
	handler, _ := web.NewHandler(db, []byte("test"), "/tmp/grimnir-test-media", logger)

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := client.Get(server.URL + "/")
		if resp != nil {
			resp.Body.Close()
		}
	}
}
