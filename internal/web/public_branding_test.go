/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/events"
)

// The public listener chrome must render the configurable PlatformName (default
// "Grimnir Radio") and demote the software brand to a footer credit, instead of
// hardcoding "Grimnir Radio ... Powered by open source radio automation" (#62).
func TestPublicChromeUsesPlatformName(t *testing.T) {
	db, _ := newScheduleEdgeTestDB(t)
	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/listen", nil)
	rr := httptest.NewRecorder()
	h.Render(rr, req, "pages/public/listen", PageData{Title: "Listen"})
	body := rr.Body.String()

	// PlatformName defaults to "Grimnir Radio", so it still appears — but the old
	// hardcoded footer tagline must be gone, replaced by the Grimnir credit.
	if strings.Contains(body, "Powered by open source radio automation") {
		t.Error("old hardcoded footer tagline still present")
	}
	if !strings.Contains(body, "Powered by Grimnir Radio") {
		t.Error("footer should carry the small 'Powered by Grimnir Radio' credit")
	}
}
