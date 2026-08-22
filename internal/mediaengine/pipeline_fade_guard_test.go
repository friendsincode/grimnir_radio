/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// TestPipeline_Fade_Preconditions guards #85: Fade refuses to start a crossfade
// unless a track is actually playing. Both guards return before the crossfade
// manager is touched, so a broken precondition can never launch a mixer against
// a missing or idle source. Loosen either check and one of these fails.
func TestPipeline_Fade_Preconditions(t *testing.T) {
	t.Run("no current track", func(t *testing.T) {
		p := &Pipeline{logger: zerolog.Nop()} // CurrentTrack nil
		err := p.Fade(context.Background(), nil, nil, nil)
		if err == nil {
			t.Fatal("Fade with no current track should error")
		}
	})

	t.Run("not in playing state", func(t *testing.T) {
		p := &Pipeline{
			logger:       zerolog.Nop(),
			CurrentTrack: &Track{SourceID: "s1"},
			State:        pb.PlaybackState_PLAYBACK_STATE_IDLE,
		}
		err := p.Fade(context.Background(), nil, nil, nil)
		if err == nil {
			t.Fatal("Fade while not playing should error")
		}
		// The guard must not have mutated state toward FADING.
		if p.State != pb.PlaybackState_PLAYBACK_STATE_IDLE {
			t.Errorf("state changed to %v, guard should leave it IDLE", p.State)
		}
	})
}
