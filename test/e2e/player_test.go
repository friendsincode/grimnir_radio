/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package e2e

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/api"
	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/integrity"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

// TestCustomJSPlayer_ListenPage spins up the control plane + web handler,
// seeds a public station with two StationStream rows (HQ + LQ), navigates a
// real browser to /listen, & verifies the new Grimnir player module:
//
//  1. mounts into the DOM (data-grimnir-player container picked up by the
//     module's querySelectorAll loop)
//  2. fetches /api/v1/stations/<id>/streams successfully
//  3. renders its own play button + quality dropdown
//  4. sets data-grimnir-state to a non-empty value
//
// We don't try to actually play audio (no real Icecast in the e2e harness);
// the audio element will error & the module will enter "reconnecting" or
// "unavailable". Both are acceptable for this test — the point is the module
// loaded, talked to the API, & rendered.
func TestCustomJSPlayer_ListenPage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}
	if os.Getenv("CI") != "" || os.Getenv("SKIP_BROWSER_TESTS") != "" {
		t.Skip("skipping browser tests in CI environment")
	}

	headless := os.Getenv("E2E_HEADLESS") != "false"

	db := setupTestDB(t)
	// StationStream isn't in setupTestDB's default migration set.
	if err := db.AutoMigrate(&models.StationStream{}, &models.ListenerEvent{}); err != nil {
		t.Fatalf("migrate StationStream/ListenerEvent: %v", err)
	}

	// Seed a public, approved, active station so /listen renders it.
	station := &models.Station{
		ID:       uuid.New().String(),
		OwnerID:  uuid.New().String(),
		Name:     "Player E2E Station",
		Timezone: "UTC",
		Active:   true,
		Public:   true,
		Approved: true,
	}
	if err := db.Create(station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	// At least one mount so listen.html renders the player-mount div.
	mount := &models.Mount{
		ID: uuid.New().String(), StationID: station.ID,
		Name: "main", URL: "http://localhost:8000/stream", Format: "mp3",
	}
	if err := db.Create(mount).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}
	// Two streams so the player has something to walk through.
	streams := []models.StationStream{
		{ID: uuid.New().String(), StationID: station.ID,
			URL: "http://127.0.0.1:1/hq", Format: "mp3", BitrateKbps: 128, Label: "HQ", Priority: 1},
		{ID: uuid.New().String(), StationID: station.ID,
			URL: "http://127.0.0.1:1/lq", Format: "mp3", BitrateKbps: 64, Label: "LQ", Priority: 2},
	}
	for _, s := range streams {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("create stream %s: %v", s.Label, err)
		}
	}

	// Wire web + api into the same chi router. The API constructor takes many
	// optional services; nil is fine for the streams + listener-events paths.
	webHandler := createTestHandler(t, db)
	bus := events.NewBus()
	logger := zerolog.Nop()
	a := api.New(
		db, []byte("test-secret"),
		nil, nil, nil, nil, nil, nil,
		priority.NewService(db, bus, logger),
		executor.NewStateManager(db, logger),
		audit.NewService(db, bus, logger),
		integrity.NewService(db, logger),
		nil, bus, nil, 0,
		logger,
	)

	r := chi.NewRouter()
	webHandler.Routes(r)
	a.Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	// Sanity: hit the streams endpoint directly so we know the API mount worked
	// before paying the browser-launch cost.
	resp, err := http.Get(server.URL + "/api/v1/stations/" + station.ID + "/streams")
	if err != nil {
		t.Fatalf("streams endpoint: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("streams endpoint: got %d, want 200", resp.StatusCode)
	}

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

	page := browser.MustPage(server.URL + "/listen")
	defer page.MustClose()

	if err := page.WaitLoad(); err != nil {
		t.Skipf("page load failed: %v", err)
	}

	// Give the module a beat to fetch streams + render its shell.
	time.Sleep(1200 * time.Millisecond)

	t.Run("mount_present", func(t *testing.T) {
		el, err := page.Element("[data-grimnir-player]")
		if err != nil || el == nil {
			t.Fatalf("data-grimnir-player element missing: %v", err)
		}
		stationAttr, _ := el.Attribute("data-station-id")
		if stationAttr == nil || *stationAttr != station.ID {
			t.Errorf("data-station-id mismatch: got %v, want %s", stationAttr, station.ID)
		}
	})

	t.Run("module_loaded", func(t *testing.T) {
		// The module renders [data-grimnir-role="ui-root"] inside the mount on
		// construct. If this element is absent the import failed (404 on the
		// module URL, parse error, or createPlayer threw).
		el, err := page.Element("[data-grimnir-role=\"ui-root\"]")
		if err != nil || el == nil {
			html, _ := page.HTML()
			preview := html
			if len(preview) > 800 {
				preview = preview[:800]
			}
			t.Fatalf("module shell did not render; ui-root missing: %v\nfirst 800 chars: %s", err, preview)
		}
	})

	t.Run("controls_rendered", func(t *testing.T) {
		if _, err := page.Element("[data-grimnir-role=\"toggle\"]"); err != nil {
			t.Errorf("toggle button missing: %v", err)
		}
		if _, err := page.Element("[data-grimnir-role=\"quality\"]"); err != nil {
			t.Errorf("quality select missing: %v", err)
		}
		if _, err := page.Element("[data-grimnir-role=\"status\"]"); err != nil {
			t.Errorf("status region missing: %v", err)
		}
	})

	t.Run("state_attribute_set", func(t *testing.T) {
		el, err := page.Element("[data-grimnir-player]")
		if err != nil {
			t.Fatalf("mount missing: %v", err)
		}
		state, err := el.Attribute("data-grimnir-state")
		if err != nil || state == nil {
			t.Fatalf("data-grimnir-state missing: %v", err)
		}
		if *state == "" {
			t.Errorf("data-grimnir-state is empty; module did not call setState()")
		}
	})

	t.Run("quality_dropdown_populated", func(t *testing.T) {
		// "Auto" plus one option per stream = 3 options.
		sel, err := page.Element("[data-grimnir-role=\"quality\"]")
		if err != nil {
			t.Fatalf("quality select missing: %v", err)
		}
		opts, err := sel.Elements("option")
		if err != nil {
			t.Fatalf("quality options enumeration: %v", err)
		}
		// 1 ("Auto") + N streams. The fetch is async so allow a brief settle.
		if len(opts) < 3 {
			t.Errorf("expected >=3 options in quality dropdown (Auto + HQ + LQ), got %d", len(opts))
		}
		// Check that at least one option text contains "HQ" or "LQ".
		seen := false
		for _, o := range opts {
			txt, _ := o.Text()
			if strings.Contains(txt, "HQ") || strings.Contains(txt, "LQ") {
				seen = true
				break
			}
		}
		if !seen {
			t.Errorf("quality dropdown options do not contain HQ/LQ labels")
		}
	})
}
