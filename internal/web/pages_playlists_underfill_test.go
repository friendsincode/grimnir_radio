/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestPlaylistUnderfillModeFromForm checks the select value normalization: only
// "randomize" is honored, and every other value (including the empty string a
// missing field yields) falls back to the default replay mode.
func TestPlaylistUnderfillModeFromForm(t *testing.T) {
	cases := map[string]string{
		"randomize": models.UnderfillRandomize,
		"replay":    models.UnderfillReplay,
		"":          models.UnderfillReplay,
		"garbage":   models.UnderfillReplay,
		"RANDOMIZE": models.UnderfillReplay, // case-sensitive by design
	}
	for in, want := range cases {
		if got := playlistUnderfillModeFromForm(in); got != want {
			t.Errorf("playlistUnderfillModeFromForm(%q) = %q, want %q", in, got, want)
		}
	}
}
