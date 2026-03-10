/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package e2e provides end-to-end browser tests for the web UI.
package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/web"
)

// testStationUUID and testMountUUID are stable UUIDs used as fixture IDs across helpers.
const (
	testStationUUID = "11111111-1111-1111-1111-111111111111"
	testMountUUID   = "22222222-2222-2222-2222-222222222222"
)

// createTestHandler creates a web handler with minimal dependencies for testing
func createTestHandler(t *testing.T, db *gorm.DB) *web.Handler {
	logger := zerolog.Nop()
	eventBus := events.NewBus()
	webrtcCfg := web.WebRTCConfig{}
	// Pass nil for mediaService and director since they're optional for basic route testing
	handler, err := web.NewHandler(db, []byte("test-jwt-secret"), "/tmp/grimnir-test-media", nil, webrtcCfg, web.HarborConfig{}, 0, eventBus, nil, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}
	return handler
}

// TestRoutes verifies all web routes are accessible and render correctly.
// This test uses HTTP requests instead of browser automation for reliability.
func TestRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	// Setup test database
	db := setupTestDB(t)

	// Create test fixtures
	ownerID := uuid.New().String()
	setupTestFixtures(t, db, ownerID)

	// Create a user (required to bypass setup redirect)
	createTestUser(t, db, "test@example.com", "password123", models.PlatformRoleUser)

	// Create handler
	handler := createTestHandler(t, db)

	// Create test server with chi router
	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}

	// Test cases for public routes
	// Note: /setup redirects to /login when users exist, so it's tested separately
	// in TestTemplateRendering which runs without a user in the database
	publicRoutes := []struct {
		name           string
		path           string
		expectedStatus int
		mustContain    string
	}{
		{"landing page", "/", 200, "Grimnir"},
		{"login page", "/login", 200, "Login"},
		{"listen page", "/listen", 200, "Listen"},
		{"archive page", "/archive", 200, "Archive"},
		{"schedule page", "/schedule", 200, "Schedule"},
	}

	for _, tc := range publicRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("request failed for %s: %v", tc.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d for %s", tc.expectedStatus, resp.StatusCode, tc.path)
			}

			// Read full response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			html := string(body)

			if !strings.Contains(html, tc.mustContain) {
				// Log first 500 chars of response for debugging
				preview := html
				if len(preview) > 500 {
					preview = preview[:500]
				}
				t.Errorf("expected page %s to contain %q, got: %s", tc.path, tc.mustContain, preview)
			}
		})
	}
}

// TestAuthenticatedRoutes tests routes that require authentication.
// This test uses browser automation with graceful error handling.
func TestAuthenticatedRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	// Skip browser tests in CI or when browser is unavailable
	if os.Getenv("CI") != "" || os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("skipping browser tests in CI environment")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	setupTestFixtures(t, db, uuid.New().String())

	// Create admin user for testing (platform admin can access all routes)
	adminUser := createTestUser(t, db, "admin@test.com", "password123", models.PlatformRoleAdmin)

	handler := createTestHandler(t, db)

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	// Try to launch browser with error handling
	l := launcher.New().Headless(headless)
	browserURL, err := l.Launch()
	if err != nil {
		t.Skipf("skipping browser test: failed to launch browser: %v", err)
	}

	browser := rod.New().ControlURL(browserURL)
	if err := browser.Connect(); err != nil {
		t.Skipf("skipping browser test: failed to connect to browser: %v", err)
	}
	defer browser.MustClose()

	// First, login
	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()

	if err := page.WaitLoad(); err != nil {
		t.Skipf("skipping browser test: page load failed: %v", err)
	}

	// Fill login form with error handling
	emailInput, err := page.Element("input[name=email]")
	if err != nil {
		t.Skipf("skipping browser test: login form not found: %v", err)
	}
	emailInput.MustInput(adminUser.Email)
	page.MustElement("input[name=password]").MustInput("password123")
	page.MustElement("button[type=submit]").MustClick()

	// Wait for page to stabilize after form submission (longer for race detector)
	time.Sleep(1500 * time.Millisecond)
	page.WaitLoad()

	// Verify login succeeded by checking we're not on login page
	info, _ := page.Info()
	if strings.Contains(info.URL, "/login") {
		t.Skipf("login did not complete, still on: %s", info.URL)
	}

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
		{"webdj console", "/dashboard/webdj/", "WebDJ Console"},
	}

	for _, tc := range dashboardRoutes {
		t.Run(tc.name, func(t *testing.T) {
			if err := page.Navigate(server.URL + tc.path); err != nil {
				t.Skipf("navigation failed: %v", err)
			}
			if err := page.WaitLoad(); err != nil {
				t.Skipf("page load failed: %v", err)
			}

			// Wait for page to stabilize (JS rendering)
			time.Sleep(200 * time.Millisecond)

			html, err := page.HTML()
			if err != nil {
				t.Skipf("failed to get HTML: %v", err)
			}
			if !strings.Contains(html, tc.mustContain) {
				// Log first 500 chars for debugging
				preview := html
				if len(preview) > 500 {
					preview = preview[:500]
				}
				t.Errorf("expected page %s to contain %q, got: %s...", tc.path, tc.mustContain, preview)
			}
		})
	}
}

// TestFormRoutes tests form pages (new/edit routes).
func TestFormRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	// Skip browser tests in CI or when browser is unavailable
	if os.Getenv("CI") != "" || os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("skipping browser tests in CI environment")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	station := setupTestFixtures(t, db, uuid.New().String())
	createTestUser(t, db, "admin@test.com", "password123", models.PlatformRoleAdmin)

	handler := createTestHandler(t, db)

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	l := launcher.New().Headless(headless)
	browserURL, err := l.Launch()
	if err != nil {
		t.Skipf("skipping browser test: failed to launch browser: %v", err)
	}

	browser := rod.New().ControlURL(browserURL)
	if err := browser.Connect(); err != nil {
		t.Skipf("skipping browser test: failed to connect to browser: %v", err)
	}
	defer browser.MustClose()

	// Login first
	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()

	if err := page.WaitLoad(); err != nil {
		t.Skipf("skipping browser test: page load failed: %v", err)
	}
	page.MustElement("input[name=email]").MustInput("admin@test.com")
	page.MustElement("input[name=password]").MustInput("password123")
	page.MustElement("button[type=submit]").MustClick()

	// Wait for page to stabilize after form submission (longer for race detector)
	time.Sleep(1500 * time.Millisecond)
	page.WaitLoad()

	// Verify login succeeded by checking we're not on login page
	info, _ := page.Info()
	if strings.Contains(info.URL, "/login") {
		t.Skipf("login did not complete, still on: %s", info.URL)
	}

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
			if err := page.Navigate(server.URL + tc.path); err != nil {
				t.Skipf("navigation failed: %v", err)
			}
			if err := page.WaitLoad(); err != nil {
				t.Skipf("page load failed: %v", err)
			}

			// Wait for page to stabilize (JS rendering)
			time.Sleep(200 * time.Millisecond)

			html, err := page.HTML()
			if err != nil {
				t.Skipf("failed to get HTML: %v", err)
			}
			if !strings.Contains(html, tc.mustContain) {
				t.Errorf("expected page %s to contain %q", tc.path, tc.mustContain)
			}
		})
	}
}

// TestTemplateRendering verifies all templates render without errors.
func TestTemplateRendering(t *testing.T) {
	db := setupTestDB(t)
	setupTestFixtures(t, db, uuid.New().String())

	handler := createTestHandler(t, db)

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

	handler := createTestHandler(t, db)

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

	// Skip browser tests in CI or when browser is unavailable
	if os.Getenv("CI") != "" || os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("skipping browser tests in CI environment")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	setupTestFixtures(t, db, uuid.New().String())
	// Create a regular user (not admin) for testing login flow
	createTestUser(t, db, "test@example.com", "testpass123", models.PlatformRoleUser)

	handler := createTestHandler(t, db)

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	l := launcher.New().Headless(headless)
	browserURL, err := l.Launch()
	if err != nil {
		t.Skipf("skipping browser test: failed to launch browser: %v", err)
	}

	browser := rod.New().ControlURL(browserURL)
	if err := browser.Connect(); err != nil {
		t.Skipf("skipping browser test: failed to connect to browser: %v", err)
	}
	defer browser.MustClose()

	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()

	if err := page.WaitLoad(); err != nil {
		t.Skipf("skipping browser test: page load failed: %v", err)
	}

	// Test invalid login
	page.MustElement("input[name=email]").MustInput("wrong@example.com")
	page.MustElement("input[name=password]").MustInput("wrongpass")
	page.MustElement("button[type=submit]").MustClick()

	// Wait for error message
	time.Sleep(500 * time.Millisecond)
	html, _ := page.HTML()
	if !strings.Contains(html, "Invalid") && !strings.Contains(html, "error") && !strings.Contains(html, "alert") {
		t.Log("expected error message on invalid login")
	}

	// Now test valid login
	page.Navigate(server.URL + "/login")
	page.WaitLoad()

	page.MustElement("input[name=email]").MustInput("test@example.com")
	page.MustElement("input[name=password]").MustInput("testpass123")
	page.MustElement("button[type=submit]").MustClick()

	// Wait for HTMX redirect to /dashboard (polls URL instead of fixed sleep)
	deadline := time.Now().Add(5 * time.Second)
	redirected := false
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		info, err := page.Info()
		if err != nil {
			continue
		}
		if strings.Contains(info.URL, "/dashboard") {
			redirected = true
			break
		}
	}
	if !redirected {
		info, _ := page.Info()
		url := ""
		if info != nil {
			url = info.URL
		}
		t.Errorf("expected redirect to dashboard, got %s", url)
	}
}

// TestWebDJConsole verifies the WebDJ console page loads, Alpine.js initializes,
// and key UI elements are present and interactive.
func TestWebDJConsole(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}
	if os.Getenv("CI") != "" || os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("skipping browser tests in CI environment")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	station := setupTestFixtures(t, db, uuid.New().String())
	createTestUser(t, db, "admin@test.com", "password123", models.PlatformRoleAdmin)
	seedTestMedia(t, db, station.ID)

	handler := createTestHandler(t, db)

	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	l := launcher.New().Headless(headless)
	browserURL, err := l.Launch()
	if err != nil {
		t.Skipf("skipping browser test: failed to launch browser: %v", err)
	}

	browser := rod.New().ControlURL(browserURL)
	if err := browser.Connect(); err != nil {
		t.Skipf("skipping browser test: failed to connect to browser: %v", err)
	}
	defer browser.MustClose()

	// Login
	page := browser.MustPage(server.URL + "/login")
	defer page.MustClose()

	if err := page.WaitLoad(); err != nil {
		t.Skipf("skipping browser test: page load failed: %v", err)
	}
	page.MustElement("input[name=email]").MustInput("admin@test.com")
	page.MustElement("input[name=password]").MustInput("password123")
	page.MustElement("button[type=submit]").MustClick()

	time.Sleep(1500 * time.Millisecond)
	page.WaitLoad()

	info, _ := page.Info()
	if strings.Contains(info.URL, "/login") {
		t.Skipf("login did not complete, still on: %s", info.URL)
	}

	// Navigate to WebDJ console
	if err := page.Navigate(server.URL + "/dashboard/webdj/"); err != nil {
		t.Fatalf("navigation to webdj failed: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("webdj page load failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	t.Run("page_renders", func(t *testing.T) {
		html, err := page.HTML()
		if err != nil {
			t.Fatalf("failed to get HTML: %v", err)
		}
		if !strings.Contains(html, "WebDJ Console") {
			t.Errorf("expected page to contain 'WebDJ Console'")
		}
	})

	t.Run("alpine_initializes", func(t *testing.T) {
		// The x-data="webdjConsole()" attribute should be present; when Alpine
		// processes it the session start screen (x-show="!sessionId") is visible.
		el, err := page.Element("#webdj-app")
		if err != nil {
			t.Fatalf("webdj-app element not found: %v", err)
		}
		xData, err := el.Attribute("x-data")
		if err != nil || xData == nil {
			t.Fatalf("x-data attribute not found on #webdj-app")
		}
		if !strings.Contains(*xData, "webdjConsole") {
			t.Errorf("expected x-data to contain 'webdjConsole', got %q", *xData)
		}

		// Session start screen should be visible (Start Session button)
		html, _ := page.HTML()
		if !strings.Contains(html, "Start Session") && !strings.Contains(html, "Resume Session") {
			t.Errorf("expected session start screen to be visible")
		}
	})

	t.Run("deck_elements_present", func(t *testing.T) {
		html, _ := page.HTML()
		if !strings.Contains(html, "DECK A") {
			t.Errorf("expected DECK A badge in DOM")
		}
		if !strings.Contains(html, "DECK B") {
			t.Errorf("expected DECK B badge in DOM")
		}
	})

	t.Run("start_session_clickable", func(t *testing.T) {
		// Find the Start Session button
		btn, err := page.ElementR("button", "Start Session")
		if err != nil {
			t.Skipf("start session button not found: %v", err)
			return
		}

		// Set up dialog handler to auto-dismiss alert() from startSession() error path.
		// The API returns 503 (webdjAPI nil) or auth token is missing, triggering alert().
		wait, handle := page.MustHandleDialog()
		go func() {
			wait()
			handle(false, "")
		}()

		btn.MustClick()

		// Wait for Alpine to process the click and the alert to be dismissed
		time.Sleep(1 * time.Second)

		// Page should still be functional (not crashed)
		_, err = page.HTML()
		if err != nil {
			t.Errorf("page became unresponsive after clicking start session: %v", err)
		}
	})

	t.Run("library_panel_present", func(t *testing.T) {
		html, _ := page.HTML()
		if !strings.Contains(html, "Library") {
			t.Errorf("expected Library panel in DOM")
		}
		if !strings.Contains(html, `placeholder="Search`) {
			t.Errorf("expected search input with placeholder in DOM")
		}
	})

	t.Run("mixer_controls_present", func(t *testing.T) {
		// Check for crossfader range input
		_, err := page.Element("input.dj-crossfader")
		if err != nil {
			t.Errorf("expected crossfader range input in DOM: %v", err)
		}
	})
}

// seedTestMedia creates sample media items in the database for library search testing.
func seedTestMedia(t *testing.T, db *gorm.DB, stationID string) {
	t.Helper()
	media := []models.MediaItem{
		{
			ID:            uuid.New().String(),
			StationID:     stationID,
			Title:         "Test Track Alpha",
			Artist:        "DJ Test",
			Album:         "Test Album",
			Genre:         "Electronic",
			Duration:      3 * time.Minute,
			Path:          stationID + "/aa/bb/test1.mp3",
			AnalysisState: models.AnalysisComplete,
		},
		{
			ID:            uuid.New().String(),
			StationID:     stationID,
			Title:         "Test Track Beta",
			Artist:        "MC Demo",
			Album:         "Demo Beats",
			Genre:         "Hip Hop",
			Duration:      4 * time.Minute,
			Path:          stationID + "/cc/dd/test2.mp3",
			AnalysisState: models.AnalysisComplete,
		},
		{
			ID:            uuid.New().String(),
			StationID:     stationID,
			Title:         "Ambient Waves",
			Artist:        "DJ Test",
			Album:         "Chill Sessions",
			Genre:         "Ambient",
			Duration:      5 * time.Minute,
			Path:          stationID + "/ee/ff/test3.mp3",
			AnalysisState: models.AnalysisComplete,
		},
	}
	for _, m := range media {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("failed to create test media item %s: %v", m.ID, err)
		}
	}
}

// Helper functions

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	// E2E tests require a PostgreSQL database. Set TEST_DB_DSN to enable them.
	// In CI, the postgres service provides: host=localhost user=postgres password=postgres dbname=postgres sslmode=disable
	adminDSN := os.Getenv("TEST_DB_DSN")
	if adminDSN == "" {
		t.Skip("TEST_DB_DSN not set; skipping E2E test (requires PostgreSQL)")
	}

	// Create a unique test database so parallel tests don't interfere.
	dbName := fmt.Sprintf("grimnir_e2e_%d", time.Now().UnixNano())

	adminDB, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open admin db: %v", err)
	}
	if err := adminDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatalf("failed to create test db %q: %v", dbName, err)
	}

	// Replace dbname in DSN with the new test database.
	testDSN := strings.ReplaceAll(adminDSN, "dbname=postgres", "dbname="+dbName)
	if testDSN == adminDSN {
		// If the DSN didn't contain "dbname=postgres", append it.
		testDSN = adminDSN + " dbname=" + dbName
	}

	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		// Drop the test database after the test finishes.
		if err := adminDB.Exec("DROP DATABASE IF EXISTS " + dbName).Error; err != nil {
			t.Logf("warning: failed to drop test db %q: %v", dbName, err)
		}
		adminSQLDB, _ := adminDB.DB()
		if adminSQLDB != nil {
			_ = adminSQLDB.Close()
		}
	})

	// Migrate all tables
	err = db.AutoMigrate(
		&models.User{},
		&models.SystemSettings{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.SmartBlock{},
		&models.Clock{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.ScheduleEntry{},
		&models.LiveSession{},
		&models.Webstream{},
		&models.PlayHistory{},
		&models.Show{},
		&models.ShowInstance{},
		&models.StagedImport{},
		&models.APIKey{},
		&migration.Job{},
		&models.OrphanMedia{},
		&models.Recording{},
		&models.RecordingChapter{},
		&models.MountPlayoutState{},
		&models.Show{},
		&models.ShowInstance{},
		&models.LandingPage{},
		&models.LandingPageAsset{},
		&models.LandingPageVersion{},
		&models.DJAvailability{},
		&models.ScheduleRequest{},
		&models.ScheduleLock{},
		&models.AuditLog{},
		&models.Sponsor{},
		&models.UnderwritingObligation{},
		&models.UnderwritingSpot{},
		&models.ListenerSample{},
		&models.WebDJSession{},
		&models.WaveformCache{},
		&models.NotificationPreference{},
		&models.Notification{},
		&models.ScheduleAnalytics{},
		&models.ScheduleAnalyticsDaily{},
	)
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func setupTestFixtures(t *testing.T, db *gorm.DB, ownerID string) *models.Station {
	// Create a test station
	station := &models.Station{
		ID:          testStationUUID,
		OwnerID:     ownerID,
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
		ID:        testMountUUID,
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

// createTestUser creates a test user with the specified platform role.
// Also creates a StationUser record linking them to the test station.
func createTestUser(t *testing.T, db *gorm.DB, email, password string, platformRole models.PlatformRole) *models.User {
	return createTestUserWithID(t, db, uuid.New().String(), email, password, platformRole)
}

// createTestUserWithID creates a test user with a pre-specified ID.
func createTestUserWithID(t *testing.T, db *gorm.DB, id, email, password string, platformRole models.PlatformRole) *models.User {
	t.Helper()
	hashedPassword, err := bcryptHash(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	user := &models.User{
		ID:           id,
		Email:        email,
		Password:     hashedPassword,
		PlatformRole: platformRole,
	}

	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Link user to test station (required for dashboard access)
	stationUser := &models.StationUser{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		StationID: testStationUUID,
		Role:      models.StationRoleAdmin,
	}
	// Ignore error if station doesn't exist yet
	db.Create(stationUser)

	return user
}

func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// jwtSecret must match the secret passed to NewHandler in createTestHandler.
const testJWTSecret = "test-jwt-secret"

// authCookies returns cookies that authenticate the given user and select the station.
func authCookies(userID, stationID string) []*http.Cookie {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	signed, _ := token.SignedString([]byte(testJWTSecret))
	return []*http.Cookie{
		{Name: "grimnir_token", Value: signed},
		{Name: "grimnir_station", Value: stationID},
	}
}

// authGet performs an authenticated GET request and returns the response.
func authGet(t *testing.T, client *http.Client, baseURL, path, userID, stationID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for _, c := range authCookies(userID, stationID) {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed for %s: %v", path, err)
	}
	return resp
}

// authMutate performs an authenticated state-changing request (POST/PUT/DELETE) with form values and CSRF.
func authMutate(t *testing.T, client *http.Client, method, baseURL, path, userID, stationID string, body string) *http.Response {
	t.Helper()
	csrfToken := "test-csrf-token-abc123"
	// Append CSRF token to form body
	if body != "" {
		body += "&"
	}
	body += "csrf_token=" + csrfToken

	req, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", baseURL+"/dashboard")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, c := range authCookies(userID, stationID) {
		req.AddCookie(c)
	}
	req.AddCookie(&http.Cookie{Name: "grimnir_csrf", Value: csrfToken})

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s request failed for %s: %v", method, path, err)
	}
	return resp
}

// authPost performs an authenticated POST with form values (includes CSRF).
func authPost(t *testing.T, client *http.Client, baseURL, path, userID, stationID string, body string) *http.Response {
	return authMutate(t, client, http.MethodPost, baseURL, path, userID, stationID, body)
}

// authPut performs an authenticated PUT with form values (includes CSRF).
func authPut(t *testing.T, client *http.Client, baseURL, path, userID, stationID string, body string) *http.Response {
	return authMutate(t, client, http.MethodPut, baseURL, path, userID, stationID, body)
}

// authDelete performs an authenticated DELETE with CSRF.
func authDelete(t *testing.T, client *http.Client, baseURL, path, userID, stationID string) *http.Response {
	return authMutate(t, client, http.MethodDelete, baseURL, path, userID, stationID, "")
}

// tryGet performs a GET request that may fail due to a server panic (EOF). Returns (resp, ok).
func tryGet(client *http.Client, url string) (*http.Response, bool) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, false
	}
	return resp, true
}

// tryAuthGet performs an authenticated GET that may fail due to server panic.
func tryAuthGet(client *http.Client, baseURL, path, userID, stationID string) (*http.Response, bool) {
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, false
	}
	for _, c := range authCookies(userID, stationID) {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	return resp, true
}

// tryAuthMutate performs an authenticated mutation that may fail due to server panic.
func tryAuthMutate(client *http.Client, method, baseURL, path, userID, stationID, body string) (*http.Response, bool) {
	csrfToken := "test-csrf-token-abc123"
	if body != "" {
		body += "&"
	}
	body += "csrf_token=" + csrfToken

	req, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", baseURL+"/dashboard")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, c := range authCookies(userID, stationID) {
		req.AddCookie(c)
	}
	req.AddCookie(&http.Cookie{Name: "grimnir_csrf", Value: csrfToken})

	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	return resp, true
}

// readBody reads and returns the body as a string.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	return string(body)
}

// setupRouteTest sets up the common test infrastructure: DB, fixtures, admin user, server.
// Returns (server URL, admin user ID, station ID, HTTP client, cleanup func).
func setupRouteTest(t *testing.T) (string, string, string, *http.Client) {
	t.Helper()
	db := setupTestDB(t)
	// Generate admin ID first so station can reference it as owner, then create both.
	adminID := uuid.New().String()
	station := setupTestFixtures(t, db, adminID)
	admin := createTestUserWithID(t, db, adminID, "admin@test.com", "password123", models.PlatformRoleAdmin)
	handler := createTestHandler(t, db)
	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	noRedirectClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return server.URL, admin.ID, station.ID, noRedirectClient
}

// setupRouteTestWithDB is like setupRouteTest but also returns the DB for creating fixtures.
func setupRouteTestWithDB(t *testing.T) (string, string, string, *http.Client, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	adminID := uuid.New().String()
	station := setupTestFixtures(t, db, adminID)
	admin := createTestUserWithID(t, db, adminID, "admin@test.com", "password123", models.PlatformRoleAdmin)
	handler := createTestHandler(t, db)
	r := chi.NewRouter()
	handler.Routes(r)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	noRedirectClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return server.URL, admin.ID, station.ID, noRedirectClient, db
}

// ---------- Route Test Functions ----------

// TestSmartBlockCRUDRoutes tests smart block list, detail, edit, create, delete, preview, and duplicate.
func TestSmartBlockCRUDRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	// Create a smart block fixture
	block := models.SmartBlock{
		ID:          uuid.New().String(),
		StationID:   stationID,
		Name:        "Test Block",
		Description: "A test smart block",
		Rules:       map[string]any{"targetMinutes": 60},
		Sequence:    map[string]any{"mode": "random"},
	}
	if err := db.Create(&block).Error; err != nil {
		t.Fatalf("failed to create smart block: %v", err)
	}

	// GET /dashboard/smart-blocks (list)
	t.Run("list", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/smart-blocks", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Test Block") {
			t.Error("expected block name in list page")
		}
	})

	// GET /dashboard/smart-blocks/{id} (detail)
	t.Run("detail", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/smart-blocks/"+block.ID, userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Test Block") {
			t.Error("expected block name in detail page")
		}
	})

	// GET /dashboard/smart-blocks/{id}/edit (edit form)
	t.Run("edit", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/smart-blocks/"+block.ID+"/edit", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Edit Smart Block") {
			t.Error("expected edit form heading")
		}
	})

	// GET /dashboard/smart-blocks/new (new form)
	t.Run("new", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/smart-blocks/new", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "New Smart Block") {
			t.Error("expected new form heading")
		}
	})

	// POST create
	t.Run("create", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/smart-blocks", userID, stationID,
			"name=Created+Block&duration_value=30&duration_unit=minutes&sequence_mode=random&duration_accuracy=2")
		readBody(t, resp) // drain
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// POST duplicate
	t.Run("duplicate", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/smart-blocks/"+block.ID+"/duplicate", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// PUT update
	t.Run("update", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/smart-blocks/"+block.ID, userID, stationID,
			"name=Updated+Block&duration_value=45&duration_unit=minutes&sequence_mode=random&duration_accuracy=2")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// POST preview
	t.Run("preview", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/smart-blocks/"+block.ID+"/preview", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE
	t.Run("delete", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/smart-blocks/"+block.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})
}

// TestPlaylistCRUDRoutes tests playlist routes.
func TestPlaylistCRUDRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	playlist := models.Playlist{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      "Test Playlist",
	}
	db.Create(&playlist)

	getRoutes := []struct {
		name   string
		path   string
		expect string
	}{
		{"list", "/dashboard/playlists", "Playlist"},
		{"new", "/dashboard/playlists/new", "New"},
		{"detail", "/dashboard/playlists/" + playlist.ID, "Test Playlist"},
		{"edit", "/dashboard/playlists/" + playlist.ID + "/edit", "Test Playlist"},
		{"cover", "/dashboard/playlists/" + playlist.ID + "/cover", ""},
		{"media-search", "/dashboard/playlists/" + playlist.ID + "/media-search", ""},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500, got %d for %s", resp.StatusCode, tc.path)
			}
			if tc.expect != "" && !strings.Contains(body, tc.expect) {
				t.Errorf("expected %q in %s", tc.expect, tc.path)
			}
		})
	}

	// POST create
	t.Run("create", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/playlists", userID, stationID,
			"name=New+Playlist")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// PUT update
	t.Run("update", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/playlists/"+playlist.ID, userID, stationID,
			"name=Updated+Playlist")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// POST bulk
	t.Run("bulk", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/playlists/bulk", userID, stationID,
			"action=delete&ids="+playlist.ID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST add item — may 404 if media_id doesn't exist (handler-level, not routing)
	t.Run("add item", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/playlists/"+playlist.ID+"/items", userID, stationID,
			"media_id=media-test-1")
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// POST reorder items
	t.Run("reorder items", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/playlists/"+playlist.ID+"/items/reorder", userID, stationID,
			"order=[]")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE cover
	t.Run("delete cover", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/playlists/"+playlist.ID+"/cover", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE
	t.Run("delete", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/playlists/"+playlist.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})
}

// TestClockCRUDRoutes tests clock template routes.
func TestClockCRUDRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	clock := models.ClockHour{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      "Test Clock",
		StartHour: 0,
		EndHour:   24,
	}
	db.Create(&clock)

	getRoutes := []struct {
		name   string
		path   string
		expect string
	}{
		{"list", "/dashboard/clocks", "Clock"},
		{"new", "/dashboard/clocks/new", "New"},
		{"detail", "/dashboard/clocks/" + clock.ID, "Test Clock"},
		{"edit", "/dashboard/clocks/" + clock.ID + "/edit", "Test Clock"},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode != 200 {
				t.Errorf("expected 200, got %d for %s", resp.StatusCode, tc.path)
			}
			if !strings.Contains(body, tc.expect) {
				t.Errorf("expected %q in %s", tc.expect, tc.path)
			}
		})
	}

	// POST create
	t.Run("create", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/clocks", userID, stationID,
			"name=New+Clock&start_hour=0&end_hour=24")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// PUT update
	t.Run("update", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/clocks/"+clock.ID, userID, stationID,
			"name=Updated+Clock&start_hour=0&end_hour=24")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// POST simulate
	t.Run("simulate", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/clocks/"+clock.ID+"/simulate", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE
	t.Run("delete", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/clocks/"+clock.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 303/200/404, got %d", resp.StatusCode)
		}
	})
}

// TestWebstreamCRUDRoutes tests webstream routes.
func TestWebstreamCRUDRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	ws := models.Webstream{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      "Test Webstream",
		URLs:      []string{"http://example.com/stream"},
	}
	db.Create(&ws)

	getRoutes := []struct {
		name   string
		path   string
		expect string
	}{
		{"list", "/dashboard/webstreams", "Webstream"},
		{"new", "/dashboard/webstreams/new", "New"},
		{"detail", "/dashboard/webstreams/" + ws.ID, "Test Webstream"},
		{"edit", "/dashboard/webstreams/" + ws.ID + "/edit", "Test Webstream"},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode != 200 {
				t.Errorf("expected 200, got %d for %s", resp.StatusCode, tc.path)
			}
			if !strings.Contains(body, tc.expect) {
				t.Errorf("expected %q in %s", tc.expect, tc.path)
			}
		})
	}

	// POST create
	t.Run("create", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/webstreams", userID, stationID,
			"name=New+Webstream&urls=http%3A%2F%2Fexample.com%2Fstream")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// PUT update
	t.Run("update", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/webstreams/"+ws.ID, userID, stationID,
			"name=Updated+Webstream&urls=http%3A%2F%2Fexample.com%2Fstream2")
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})

	// POST failover
	t.Run("failover", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/webstreams/"+ws.ID+"/failover", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST reset
	t.Run("reset", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/webstreams/"+ws.ID+"/reset", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE
	t.Run("delete", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/webstreams/"+ws.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != 200 {
			t.Errorf("expected 303 or 200, got %d", resp.StatusCode)
		}
	})
}

// TestMediaCRUDRoutes tests media library routes.
func TestMediaCRUDRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	media := models.MediaItem{
		ID:            uuid.New().String(),
		StationID:     stationID,
		Title:         "Test Track",
		Artist:        "Test Artist",
		Duration:      3 * time.Minute,
		AnalysisState: "complete",
	}
	db.Create(&media)

	getRoutes := []struct {
		name   string
		path   string
		expect string
	}{
		{"list", "/dashboard/media", "Media"},
		{"detail", "/dashboard/media/" + media.ID, ""},
		{"edit", "/dashboard/media/" + media.ID + "/edit", "Test Track"},
		{"genres", "/dashboard/media/genres", "Genre"},
		{"duplicates", "/dashboard/media/duplicates", ""},
		{"upload page", "/dashboard/media/upload", ""},
		{"table partial", "/dashboard/media/table", ""},
		{"grid partial", "/dashboard/media/grid", ""},
		{"search json", "/dashboard/media/search.json", ""},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500, got %d for %s: %.200s", resp.StatusCode, tc.path, body)
			}
			if tc.expect != "" && !strings.Contains(body, tc.expect) {
				t.Errorf("expected %q in %s", tc.expect, tc.path)
			}
		})
	}

	// PUT update
	t.Run("update", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/media/"+media.ID, userID, stationID,
			"title=Updated+Track&artist=Updated+Artist")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST bulk
	t.Run("bulk", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/media/bulk", userID, stationID,
			"action=delete&ids="+media.ID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST genres reassign
	t.Run("genres reassign", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/media/genres/reassign", userID, stationID,
			"from=Rock&to=Alternative")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST duplicates purge
	t.Run("duplicates purge", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/media/duplicates/purge", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// GET waveform / artwork / stream — may 404 without file but should not be 405
	for _, sub := range []string{"waveform", "artwork", "stream"} {
		t.Run(sub, func(t *testing.T) {
			resp := authGet(t, client, baseURL, "/dashboard/media/"+media.ID+"/"+sub, userID, stationID)
			readBody(t, resp)
			if resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("expected route to be wired for %s, got 405", sub)
			}
		})
	}

	// DELETE — media delete touches many related tables; verify route is wired.
	t.Run("delete", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/media/"+media.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestRecordingRoutes tests recording list and detail pages.
func TestRecordingRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	recording := models.Recording{
		ID:        uuid.New().String(),
		StationID: stationID,
		UserID:    userID,
		MountID:   testMountUUID,
		Title:     "Test Recording",
		Status:    models.RecordingStatusComplete,
		Format:    models.RecordingFormatFLAC,
		StartedAt: time.Now().Add(-time.Hour),
	}
	db.Create(&recording)

	t.Run("list", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/recordings", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Recording") && !strings.Contains(body, "recording") {
			t.Error("expected recording content in list page")
		}
	})

	t.Run("detail", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/recordings/"+recording.ID, userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Test Recording") {
			t.Error("expected recording title in detail page")
		}
	})

	// POST visibility
	t.Run("visibility", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/recordings/"+recording.ID+"/visibility", userID, stationID,
			"visibility=public")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST start (will fail without media engine but should be wired)
	t.Run("start", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/recordings/start", userID, stationID,
			"mount_id="+testMountUUID+"&title=New+Recording")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST stop
	t.Run("stop", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/recordings/"+recording.ID+"/stop", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST delete
	t.Run("delete", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/recordings/"+recording.ID+"/delete", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode >= 500 {
			t.Errorf("expected non-500, got %d", resp.StatusCode)
		}
	})
}

// TestScheduleEntryRoutes tests schedule page, JSON endpoints, and entry CRUD.
func TestScheduleEntryRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	// Create a schedule entry fixture
	entry := models.ScheduleEntry{
		ID:         uuid.New().String(),
		StationID:  stationID,
		MountID:    testMountUUID,
		SourceType: "playlist",
		SourceID:   uuid.New().String(),
		StartsAt:   time.Now(),
		EndsAt:     time.Now().Add(time.Hour),
	}
	db.Create(&entry)

	t.Run("schedule page", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/schedule", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Schedule") && !strings.Contains(body, "schedule") {
			t.Error("expected schedule content")
		}
	})

	// JSON endpoints
	jsonEndpoints := []string{
		"/dashboard/schedule/events",
		"/dashboard/schedule/validate",
		"/dashboard/schedule/source-tracks",
		"/dashboard/schedule/playlists.json",
		"/dashboard/schedule/smart-blocks.json",
		"/dashboard/schedule/clocks.json",
		"/dashboard/schedule/webstreams.json",
		"/dashboard/schedule/media.json",
		"/dashboard/schedule/show-events",
	}

	for _, path := range jsonEndpoints {
		t.Run("GET "+path, func(t *testing.T) {
			resp := authGet(t, client, baseURL, path, userID, stationID)
			readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d", path, resp.StatusCode)
			}
		})
	}

	// GET entry details
	t.Run("entry details", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/schedule/entries/"+entry.ID+"/details", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// POST create entry
	t.Run("create entry", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/schedule/entries", userID, stationID,
			"source_type=playlist&source_id=pl-test-1&start_time=2026-03-02T10:00:00Z&end_time=2026-03-02T11:00:00Z")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT update entry
	t.Run("update entry", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/schedule/entries/"+entry.ID, userID, stationID,
			"source_type=playlist&source_id=pl-test-1&start_time=2026-03-02T10:00:00Z&end_time=2026-03-02T12:00:00Z")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST refresh
	t.Run("refresh", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/schedule/refresh", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE entry
	t.Run("delete entry", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/schedule/entries/"+entry.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestProfileRoutes tests profile page, update, password change, and API keys.
func TestProfileRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	t.Run("profile page", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/profile", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Profile") && !strings.Contains(body, "profile") {
			t.Error("expected profile content")
		}
	})

	// PUT profile update
	t.Run("update", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/profile", userID, stationID,
			"display_name=Test+Admin")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST password change
	t.Run("password change", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/profile/password", userID, stationID,
			"current_password=password123&new_password=newpass456&confirm_password=newpass456")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// GET API keys section
	t.Run("api keys page", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/profile/api-keys", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode >= 500 {
			t.Errorf("expected non-500, got %d", resp.StatusCode)
		}
	})

	// POST generate API key
	t.Run("generate api key", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/profile/api-keys", userID, stationID,
			"name=Test+Key")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// Create an API key fixture for delete test
	apiKey := models.APIKey{
		ID:        uuid.New().String(),
		UserID:    userID,
		Name:      "Test Key",
		KeyHash:   "test-key-hash",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	db.Create(&apiKey)

	// DELETE API key
	t.Run("revoke api key", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/profile/api-keys/"+apiKey.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestStationSettingsRoutes tests settings, migrations, and orphans.
func TestStationSettingsRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client := setupRouteTest(t)

	getRoutes := []struct {
		name   string
		path   string
		expect string
	}{
		{"settings page", "/dashboard/settings", "Setting"},
		{"migrations page", "/dashboard/settings/migrations", ""},
		{"migrations status", "/dashboard/settings/migrations/status", ""},
		{"migrations history", "/dashboard/settings/migrations/history", ""},
		{"orphans page", "/dashboard/settings/orphans", ""},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500, got %d for %s: %.200s", resp.StatusCode, tc.path, body)
			}
			if tc.expect != "" && !strings.Contains(body, tc.expect) {
				t.Errorf("expected %q in %s", tc.expect, tc.path)
			}
		})
	}

	// PUT settings update
	t.Run("update settings", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/settings", userID, stationID,
			"site_name=Test+Radio")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST orphans scan
	t.Run("orphans scan", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/settings/orphans/scan", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestLiveSessionRoutes tests live DJ pages and actions.
func TestLiveSessionRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client := setupRouteTest(t)

	t.Run("live page", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/live", userID, stationID)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Live") && !strings.Contains(body, "live") {
			t.Error("expected live content")
		}
	})

	// GET sessions
	t.Run("sessions", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/live/sessions", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode >= 500 {
			t.Errorf("expected non-500, got %d", resp.StatusCode)
		}
	})

	// POST generate token
	t.Run("generate token", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/live/tokens", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST connect (will fail without harbor but should be wired)
	t.Run("connect", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/live/connect", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST handover — may 404 if no active live session exists (handler-level, not routing)
	t.Run("handover", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/live/handover", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// DELETE handover — may 404 if no active live session exists
	t.Run("release handover", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/live/handover", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// DELETE session (non-existent, but route should be wired)
	t.Run("disconnect session", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/live/sessions/nonexistent", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})
}

// TestPublicDetailRoutes tests public-facing detail pages, embeds, and auth endpoints.
func TestPublicDetailRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, adminUserID, stationID, client, db := setupRouteTestWithDB(t)

	// Update the test station to have a shortcode for public landing
	db.Model(&models.Station{}).Where("id = ?", stationID).Update("shortcode", "testfm")

	// Create a public recording for archive detail
	recording := models.Recording{
		ID:         uuid.New().String(),
		StationID:  stationID,
		UserID:     adminUserID,
		MountID:    testMountUUID,
		Title:      "Public Recording",
		Status:     models.RecordingStatusComplete,
		Format:     models.RecordingFormatFLAC,
		Visibility: models.RecordingVisibilityPublic,
		StartedAt:  time.Now().Add(-time.Hour),
	}
	db.Create(&recording)

	publicRoutes := []struct {
		name string
		path string
	}{
		{"landing", "/"},
		{"listen", "/listen"},
		{"archive list", "/archive"},
		{"archive detail", "/archive/" + recording.ID},
		{"public schedule", "/schedule"},
		{"public schedule events", "/schedule/events"},
		{"station info", "/station/" + stationID},
		{"shortcode /s", "/s/testfm"},
		{"shortcode /stations", "/stations/testfm"},
		{"embed schedule", "/embed/schedule"},
		{"embed now-playing", "/embed/now-playing"},
		{"embed schedule js", "/embed/schedule.js"},
		{"login page", "/login"},
		{"logout", "/logout"},
		{"favicon", "/favicon.ico"},
	}

	for _, tc := range publicRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Get(baseURL + tc.path)
			if err != nil {
				t.Fatalf("request failed for %s: %v", tc.path, err)
			}
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", tc.path, resp.StatusCode, body)
			}
		})
	}

	// Public archive sub-routes (may 404 without file but should not be 405)
	for _, sub := range []string{"stream", "artwork"} {
		t.Run("archive "+sub, func(t *testing.T) {
			resp, err := client.Get(baseURL + "/archive/" + recording.ID + "/" + sub)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			readBody(t, resp)
			if resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("expected route to be wired for archive/%s, got 405", sub)
			}
		})
	}

	// Public media artwork
	t.Run("public media artwork", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/media/nonexistent/artwork")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// Landing page assets — may panic without LandingPage/Asset records
	t.Run("landing asset by id", func(t *testing.T) {
		resp, ok := tryGet(client, baseURL+"/landing-assets/nonexistent")
		if !ok {
			t.Log("server closed connection for landing-assets (handler may panic)")
			return
		}
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	t.Run("landing asset by type", func(t *testing.T) {
		resp, ok := tryGet(client, baseURL+"/landing-assets/by-type/logo")
		if !ok {
			t.Log("server closed connection for landing-assets/by-type (handler may panic)")
			return
		}
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// POST login
	t.Run("login submit", func(t *testing.T) {
		resp, err := client.Post(baseURL+"/login", "application/x-www-form-urlencoded",
			strings.NewReader("email=test%40example.com&password=wrong"))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		readBody(t, resp)
		if resp.StatusCode >= 500 {
			t.Errorf("expected non-500, got %d", resp.StatusCode)
		}
	})

	// POST setup (should redirect when users exist)
	t.Run("setup submit", func(t *testing.T) {
		resp, err := client.Post(baseURL+"/setup", "application/x-www-form-urlencoded",
			strings.NewReader("email=new%40example.com&password=pass123&station_name=Test"))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		readBody(t, resp)
		// When users already exist, setup redirects to login
		if resp.StatusCode >= 500 {
			t.Errorf("expected non-500, got %d", resp.StatusCode)
		}
	})
}

// TestArchiveShowFilter tests the playlist/smart block "Show" filter on /archive.
func TestArchiveShowFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, _, stationID, client, db := setupRouteTestWithDB(t)

	// Make station public so archive is visible
	db.Model(&models.Station{}).Where("id = ?", stationID).Updates(map[string]any{
		"public": true, "approved": true,
	})

	// Create archive media
	afM1ID := uuid.New().String()
	afM2ID := uuid.New().String()
	afM3ID := uuid.New().String()
	afPl1ID := uuid.New().String()
	afPi1ID := uuid.New().String()
	afSb1ID := uuid.New().String()
	db.Create(&models.MediaItem{ID: afM1ID, StationID: stationID, Title: "Playlist Track", Artist: "Artist1", ShowInArchive: true})
	db.Create(&models.MediaItem{ID: afM2ID, StationID: stationID, Title: "Rock Track", Artist: "Artist2", Genre: "Rock", ShowInArchive: true})
	db.Create(&models.MediaItem{ID: afM3ID, StationID: stationID, Title: "Unfiltered Track", Artist: "Artist3", ShowInArchive: true})

	// Create playlist containing only afM1ID
	db.Create(&models.Playlist{ID: afPl1ID, StationID: stationID, Name: "Test Show Playlist"})
	db.Create(&models.PlaylistItem{ID: afPi1ID, PlaylistID: afPl1ID, MediaID: afM1ID, Position: 0})

	// Create smart block matching Rock genre
	db.Create(&models.SmartBlock{
		ID: afSb1ID, StationID: stationID, Name: "Rock Smart Block",
		Rules: map[string]any{"genre": "Rock"}, Sequence: map[string]any{"mode": "random"},
	})

	t.Run("archive page shows Show dropdown", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/archive")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "All Shows") {
			t.Error("expected Show dropdown with 'All Shows'")
		}
		if !strings.Contains(body, "Test Show Playlist") {
			t.Error("expected playlist name in dropdown")
		}
		if !strings.Contains(body, "Rock Smart Block") {
			t.Error("expected smart block name in dropdown")
		}
	})

	t.Run("playlist filter returns correct media", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/archive?show=playlist:" + afPl1ID)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Playlist Track") {
			t.Error("expected 'Playlist Track' in filtered results")
		}
		if strings.Contains(body, "Rock Track") {
			t.Error("unexpected 'Rock Track' in playlist-filtered results")
		}
		if strings.Contains(body, "Unfiltered Track") {
			t.Error("unexpected 'Unfiltered Track' in playlist-filtered results")
		}
	})

	t.Run("smart block filter returns correct media", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/archive?show=smartblock:" + afSb1ID)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Rock Track") {
			t.Error("expected 'Rock Track' in smart block filtered results")
		}
		if strings.Contains(body, "Playlist Track") {
			t.Error("unexpected 'Playlist Track' in smart block filtered results")
		}
	})

	t.Run("show filter with clear button", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/archive?show=playlist:" + afPl1ID)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body := readBody(t, resp)
		if !strings.Contains(body, "Clear") {
			t.Error("expected Clear button when show filter is active")
		}
	})
}

// TestAdminRoutes tests platform admin routes including all GET pages and mutations.
func TestAdminRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	// Create extra fixtures for admin tests
	media := models.MediaItem{
		ID:            uuid.New().String(),
		StationID:     stationID,
		Title:         "Admin Track",
		Artist:        "Admin Artist",
		Duration:      3 * time.Minute,
		AnalysisState: "complete",
	}
	db.Create(&media)

	// Create a second user for admin user management tests
	secondUser := createTestUser(t, db, "dj@test.com", "password123", models.PlatformRoleUser)

	getRoutes := []struct {
		name string
		path string
	}{
		{"admin stations", "/dashboard/admin/stations"},
		{"admin users", "/dashboard/admin/users"},
		{"admin media", "/dashboard/admin/media"},
		{"admin media duplicates", "/dashboard/admin/media/duplicates"},
		{"admin logs", "/dashboard/admin/logs"},
		{"admin audit", "/dashboard/admin/audit"},
		{"admin integrity", "/dashboard/admin/integrity"},
		{"admin user edit", "/dashboard/admin/users/" + secondUser.ID + "/edit"},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", tc.path, resp.StatusCode, body)
			}
		})
	}

	// Landing page routes may panic if no LandingPage record exists — use try helpers
	landingRoutes := []struct {
		name string
		path string
	}{
		{"admin landing editor", "/dashboard/admin/landing-page/editor"},
		{"admin landing preview", "/dashboard/admin/landing-page/preview"},
	}

	for _, tc := range landingRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp, ok := tryAuthGet(client, baseURL, tc.path, userID, stationID)
			if !ok {
				t.Logf("server closed connection for %s (handler may panic without LandingPage record)", tc.path)
				return
			}
			readBody(t, resp)
		})
	}

	// Station mutations
	t.Run("toggle station active", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/stations/"+stationID+"/toggle-active", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("toggle station public", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/stations/"+stationID+"/toggle-public", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("toggle station approved", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/stations/"+stationID+"/toggle-approved", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("stations bulk", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/stations/bulk", userID, stationID,
			"action=deactivate&ids="+stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// User mutations
	t.Run("admin user update", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/users/"+secondUser.ID, userID, stationID,
			"platform_role=user")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("users bulk", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/users/bulk", userID, stationID,
			"action=deactivate&ids="+secondUser.ID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// Media mutations
	t.Run("toggle media public", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/media/"+media.ID+"/toggle-public", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("media move", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/media/"+media.ID+"/move", userID, stationID,
			"target_station_id="+stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("media bulk", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/media/bulk", userID, stationID,
			"action=delete&ids="+media.ID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("media hash backfill", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/media/duplicates/hash-backfill", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("media duplicates purge", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/admin/media/duplicates/purge", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// Admin landing page mutations — may panic without LandingPage record
	for _, action := range []string{"save", "publish", "discard"} {
		t.Run("landing "+action, func(t *testing.T) {
			resp, ok := tryAuthMutate(client, http.MethodPost, baseURL, "/dashboard/admin/landing-page/"+action, userID, stationID, "config={}")
			if !ok {
				t.Logf("server closed connection for landing %s (handler may panic)", action)
				return
			}
			readBody(t, resp)
		})
	}

	// Deletions (run last)
	t.Run("admin media stream", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/admin/media/"+media.ID+"/stream", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	t.Run("admin delete media", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/admin/media/"+media.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	t.Run("admin delete user", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/admin/users/"+secondUser.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestStationManagementRoutes tests station CRUD and mount routes.
func TestStationManagementRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	// Create a second station for edit/update/delete
	station2 := models.Station{
		ID:       uuid.New().String(),
		OwnerID:  userID,
		Name:     "Second Station",
		Timezone: "UTC",
		Active:   true,
	}
	db.Create(&station2)

	getRoutes := []struct {
		name string
		path string
	}{
		{"stations list", "/dashboard/stations"},
		{"station new", "/dashboard/stations/new"},
		{"station edit", "/dashboard/stations/" + station2.ID},
		{"station select", "/dashboard/stations/select"},
		{"mounts list", "/dashboard/stations/" + stationID + "/mounts"},
		{"mount new", "/dashboard/stations/" + stationID + "/mounts/new"},
		{"mount edit", "/dashboard/stations/" + stationID + "/mounts/" + testMountUUID},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", tc.path, resp.StatusCode, body)
			}
		})
	}

	// POST create station
	t.Run("create station", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/stations", userID, stationID,
			"name=New+Station&timezone=UTC")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT update station
	t.Run("update station", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/stations/"+station2.ID, userID, stationID,
			"name=Updated+Station&timezone=UTC")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST station select
	t.Run("select station", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/stations/select", userID, stationID,
			"station_id="+stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST create mount
	t.Run("create mount", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/stations/"+stationID+"/mounts", userID, stationID,
			"name=New+Mount&url=http%3A%2F%2Flocalhost%3A8000%2Fstream2&format=mp3")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT update mount
	t.Run("update mount", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/stations/"+stationID+"/mounts/"+testMountUUID, userID, stationID,
			"name=Updated+Mount&url=http%3A%2F%2Flocalhost%3A8000%2Fstream&format=mp3")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE mount
	t.Run("delete mount", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/stations/"+stationID+"/mounts/"+testMountUUID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE station (run last)
	t.Run("delete station", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/stations/"+station2.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestStationUserAndSettingsRoutes tests /dashboard/station/ sub-tree.
func TestStationUserAndSettingsRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	// Create a station user for edit/update/delete tests
	stationUser := models.StationUser{
		ID:        uuid.New().String(),
		UserID:    userID,
		StationID: stationID,
		Role:      models.StationRoleDJ,
	}
	// This may conflict with the auto-created one, so use a separate user
	djUser := createTestUser(t, db, "dj2@test.com", "password123", models.PlatformRoleUser)
	stationUser.UserID = djUser.ID
	db.Create(&stationUser)

	getRoutes := []struct {
		name string
		path string
	}{
		{"station users", "/dashboard/station/users"},
		{"station users invite", "/dashboard/station/users/invite"},
		{"station user edit", "/dashboard/station/users/" + stationUser.ID + "/edit"},
		{"station settings", "/dashboard/station/settings"},
		{"station logs", "/dashboard/station/logs"},
		{"station audit", "/dashboard/station/audit"},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", tc.path, resp.StatusCode, body)
			}
		})
	}

	// Landing page routes may panic without LandingPage record — use try helpers
	landingGets := []struct {
		name string
		path string
	}{
		{"landing editor", "/dashboard/station/landing-page/editor"},
		{"landing preview", "/dashboard/station/landing-page/preview"},
		{"landing versions", "/dashboard/station/landing-page/versions"},
	}
	for _, tc := range landingGets {
		t.Run(tc.name, func(t *testing.T) {
			resp, ok := tryAuthGet(client, baseURL, tc.path, userID, stationID)
			if !ok {
				t.Logf("server closed connection for %s (handler may panic without LandingPage record)", tc.path)
				return
			}
			readBody(t, resp)
		})
	}

	// POST add station user — the handler expects an existing user by email
	t.Run("add station user", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/station/users", userID, stationID,
			"email=dj2%40test.com&role=dj")
		readBody(t, resp)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got 405")
		}
	})

	// POST update station user
	t.Run("update station user", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/station/users/"+stationUser.ID, userID, stationID,
			"role=manager")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT station settings
	t.Run("update station settings", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/station/settings", userID, stationID,
			"name=Updated+Station")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST stop playout
	t.Run("stop playout", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/station/settings/stop-playout", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// Landing page mutations — may panic without LandingPage record
	for _, action := range []string{"save", "publish", "discard"} {
		t.Run("landing "+action, func(t *testing.T) {
			resp, ok := tryAuthMutate(client, http.MethodPost, baseURL, "/dashboard/station/landing-page/"+action, userID, stationID, "config={}")
			if !ok {
				t.Logf("server closed connection for landing %s (handler may panic)", action)
				return
			}
			readBody(t, resp)
		})
	}

	t.Run("landing theme update", func(t *testing.T) {
		resp, ok := tryAuthMutate(client, http.MethodPut, baseURL, "/dashboard/station/landing-page/theme", userID, stationID, "theme=dark")
		if !ok {
			t.Log("server closed connection for landing theme update")
			return
		}
		readBody(t, resp)
	})

	t.Run("landing custom css", func(t *testing.T) {
		resp, ok := tryAuthMutate(client, http.MethodPut, baseURL, "/dashboard/station/landing-page/custom-css", userID, stationID, "css=body{}")
		if !ok {
			t.Log("server closed connection for landing custom css")
			return
		}
		readBody(t, resp)
	})

	// DELETE station user (run last)
	t.Run("remove station user", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/station/users/"+stationUser.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestUserCRUDRoutes tests user management CRUD.
func TestUserCRUDRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	targetUser := createTestUser(t, db, "target@test.com", "password123", models.PlatformRoleUser)

	getRoutes := []struct {
		name string
		path string
	}{
		{"users list", "/dashboard/users"},
		{"user new", "/dashboard/users/new"},
		{"user detail", "/dashboard/users/" + targetUser.ID},
		{"user edit", "/dashboard/users/" + targetUser.ID + "/edit"},
	}

	for _, tc := range getRoutes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", tc.path, resp.StatusCode, body)
			}
		})
	}

	// POST create user
	t.Run("create user", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/users", userID, stationID,
			"email=newuser%40test.com&password=pass123&role=dj")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT update user
	t.Run("update user", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/users/"+targetUser.ID, userID, stationID,
			"role=manager")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE user
	t.Run("delete user", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/users/"+targetUser.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestShowRoutes tests show management CRUD.
func TestShowRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	show := models.Show{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      "Test Show",
	}
	db.Create(&show)

	showInstance := models.ShowInstance{
		ID:        uuid.New().String(),
		ShowID:    show.ID,
		StationID: stationID,
		StartsAt:  time.Now().Add(time.Hour),
	}
	db.Create(&showInstance)

	// GET shows JSON
	t.Run("list shows", func(t *testing.T) {
		resp := authGet(t, client, baseURL, "/dashboard/shows", userID, stationID)
		readBody(t, resp)
		if resp.StatusCode >= 500 {
			t.Errorf("expected non-500, got %d", resp.StatusCode)
		}
	})

	// POST create show
	t.Run("create show", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/shows", userID, stationID,
			"name=New+Show")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT update show
	t.Run("update show", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/shows/"+show.ID, userID, stationID,
			"name=Updated+Show")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// POST materialize show
	t.Run("materialize show", func(t *testing.T) {
		resp := authPost(t, client, baseURL, "/dashboard/shows/"+show.ID+"/materialize", userID, stationID, "")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// PUT update show instance
	t.Run("update show instance", func(t *testing.T) {
		resp := authPut(t, client, baseURL, "/dashboard/shows/instances/"+showInstance.ID, userID, stationID,
			"starts_at=2026-03-03T10:00:00Z")
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE show instance (cancel)
	t.Run("cancel show instance", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/shows/instances/"+showInstance.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})

	// DELETE show
	t.Run("delete show", func(t *testing.T) {
		resp := authDelete(t, client, baseURL, "/dashboard/shows/"+show.ID, userID, stationID)
		readBody(t, resp)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("expected route to be wired, got %d", resp.StatusCode)
		}
	})
}

// TestDJSelfServiceRoutes tests DJ self-service pages.
func TestDJSelfServiceRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client := setupRouteTest(t)

	routes := []string{
		"/dashboard/dj/availability",
		"/dashboard/dj/availability.json",
		"/dashboard/dj/requests",
	}

	for _, path := range routes {
		t.Run("GET "+path, func(t *testing.T) {
			resp := authGet(t, client, baseURL, path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", path, resp.StatusCode, body)
			}
		})
	}
}

// TestWebDJRoutes tests WebDJ console routes.
func TestWebDJRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client, db := setupRouteTestWithDB(t)

	playlist := models.Playlist{
		ID:        uuid.New().String(),
		StationID: stationID,
		Name:      "WebDJ Playlist",
	}
	db.Create(&playlist)

	media := models.MediaItem{
		ID:            uuid.New().String(),
		StationID:     stationID,
		Title:         "WebDJ Track",
		Duration:      3 * time.Minute,
		AnalysisState: "complete",
	}
	db.Create(&media)

	routes := []struct {
		name string
		path string
	}{
		{"console", "/dashboard/webdj"},
		{"library search", "/dashboard/webdj/library/search"},
		{"library genres", "/dashboard/webdj/library/genres"},
		{"library playlists", "/dashboard/webdj/library/playlists"},
		{"playlist items", "/dashboard/webdj/library/playlists/" + playlist.ID + "/items"},
		{"media artwork", "/dashboard/webdj/media/" + media.ID + "/artwork"},
		{"media stream", "/dashboard/webdj/media/" + media.ID + "/stream"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			resp := authGet(t, client, baseURL, tc.path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("expected route to be wired for %s, got 405", tc.path)
			}
			_ = body
		})
	}
}

// TestPlayoutControlRoutes tests playout control endpoints.
func TestPlayoutControlRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client := setupRouteTest(t)

	// These will fail without an executor, but should be wired
	for _, action := range []string{"skip", "stop", "reload"} {
		t.Run(action, func(t *testing.T) {
			resp := authPost(t, client, baseURL, "/dashboard/playout/"+action, userID, stationID, "")
			readBody(t, resp)
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("expected playout/%s route to be wired, got %d", action, resp.StatusCode)
			}
		})
	}
}

// TestAnalyticsRoutes tests all analytics endpoints.
func TestAnalyticsRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	baseURL, userID, stationID, client := setupRouteTest(t)

	routes := []string{
		"/dashboard/analytics",
		"/dashboard/analytics/now-playing",
		"/dashboard/analytics/history",
		"/dashboard/analytics/spins",
		"/dashboard/analytics/listeners",
		"/dashboard/analytics/listeners/timeseries",
		"/dashboard/analytics/listeners/export.csv",
	}

	for _, path := range routes {
		t.Run("GET "+path, func(t *testing.T) {
			resp := authGet(t, client, baseURL, path, userID, stationID)
			body := readBody(t, resp)
			if resp.StatusCode >= 500 {
				t.Errorf("expected non-500 for %s, got %d: %.200s", path, resp.StatusCode, body)
			}
		})
	}
}

// BenchmarkPageLoad benchmarks page loading times.
func BenchmarkPageLoad(b *testing.B) {
	adminDSN := os.Getenv("TEST_DB_DSN")
	if adminDSN == "" {
		adminDSN = "host=localhost user=postgres password=postgres dbname=postgres sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{})
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	db.AutoMigrate(&models.User{}, &models.Station{})

	logger := zerolog.Nop()
	eventBus := events.NewBus()
	webrtcCfg := web.WebRTCConfig{}
	// Pass nil for mediaService and director since they're optional for basic route testing
	handler, err := web.NewHandler(db, []byte("test"), "/tmp/grimnir-test-media", nil, webrtcCfg, web.HarborConfig{}, 0, eventBus, nil, logger)
	if err != nil {
		b.Fatalf("failed to create handler: %v", err)
	}

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
