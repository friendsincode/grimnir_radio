/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

// TestWebDJNilAPI tests that all webdj delegating handlers return 503
// when the webdjAPI or webdjWS is nil (not configured).
func TestWebDJNilAPI(t *testing.T) {
	a := &API{logger: zerolog.Nop()}

	handlers := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"StartSession", a.handleWebDJStartSession},
		{"ListSessions", a.handleWebDJListSessions},
		{"GetSession", a.handleWebDJGetSession},
		{"EndSession", a.handleWebDJEndSession},
		{"WebSocket", a.handleWebDJWebSocket},
		{"LoadTrack", a.handleWebDJLoadTrack},
		{"Play", a.handleWebDJPlay},
		{"Pause", a.handleWebDJPause},
		{"Seek", a.handleWebDJSeek},
		{"SetCue", a.handleWebDJSetCue},
		{"DeleteCue", a.handleWebDJDeleteCue},
		{"Eject", a.handleWebDJEject},
		{"SetVolume", a.handleWebDJSetVolume},
		{"SetEQ", a.handleWebDJSetEQ},
		{"SetPitch", a.handleWebDJSetPitch},
		{"SetCrossfader", a.handleWebDJSetCrossfader},
		{"SetMasterVolume", a.handleWebDJSetMasterVolume},
		{"GoLive", a.handleWebDJGoLive},
		{"GoOffAir", a.handleWebDJGoOffAir},
		{"GetWaveform", a.handleWebDJGetWaveform},
	}

	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			rr := httptest.NewRecorder()
			h.handler(rr, req)
			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s with nil webdjAPI: got %d, want 503", h.name, rr.Code)
			}
		})
	}
}
