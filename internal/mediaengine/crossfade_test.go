/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func TestCrossfadeManager_New(t *testing.T) {
	logger := zerolog.Nop()

	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	if cfm.stationID != "station-123" {
		t.Errorf("stationID = %s, want station-123", cfm.stationID)
	}
	if cfm.mountID != "mount-456" {
		t.Errorf("mountID = %s, want mount-456", cfm.mountID)
	}
	if cfm.fadeState != FadeStateIdle {
		t.Errorf("Initial fadeState = %s, want idle", cfm.fadeState)
	}
}

func TestCrossfadeManager_GetFadeState(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	state := cfm.GetFadeState()
	if state != FadeStateIdle {
		t.Errorf("GetFadeState() = %s, want idle", state)
	}
}

func TestCrossfadeManager_CalculateFadeTiming(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	tests := []struct {
		name         string
		currentTrack *Track
		nextTrack    *Track
		fadeConfig   *pb.FadeConfig
		wantOutro    time.Duration
		wantIntro    time.Duration
	}{
		{
			name: "no cue points, default fade",
			currentTrack: &Track{
				Duration: 180 * time.Second,
			},
			nextTrack: &Track{},
			fadeConfig: &pb.FadeConfig{
				FadeInMs:  3000,
				FadeOutMs: 3000,
			},
			wantOutro: 177 * time.Second, // 180 - 3
			wantIntro: 0,
		},
		{
			name: "with outro cue point",
			currentTrack: &Track{
				Duration: 180 * time.Second,
				CuePoints: &pb.CuePoints{
					OutroIn: 170.0, // Outro starts at 170 seconds
				},
			},
			nextTrack: &Track{},
			fadeConfig: &pb.FadeConfig{
				FadeInMs:  3000,
				FadeOutMs: 3000,
			},
			wantOutro: 170 * time.Second,
			wantIntro: 0,
		},
		{
			name: "with intro cue point",
			currentTrack: &Track{
				Duration: 180 * time.Second,
			},
			nextTrack: &Track{
				CuePoints: &pb.CuePoints{
					IntroEnd: 5.5, // Skip 5.5 seconds of intro
				},
			},
			fadeConfig: &pb.FadeConfig{
				FadeInMs:  3000,
				FadeOutMs: 3000,
			},
			wantOutro: 177 * time.Second,
			wantIntro: 5500 * time.Millisecond,
		},
		{
			name: "with both cue points",
			currentTrack: &Track{
				Duration: 240 * time.Second,
				CuePoints: &pb.CuePoints{
					OutroIn: 230.0,
				},
			},
			nextTrack: &Track{
				CuePoints: &pb.CuePoints{
					IntroEnd: 8.2,
				},
			},
			fadeConfig: &pb.FadeConfig{
				FadeInMs:  4000,
				FadeOutMs: 5000,
			},
			wantOutro: 230 * time.Second,
			wantIntro: 8200 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timing := cfm.calculateFadeTiming(tt.currentTrack, tt.nextTrack, tt.fadeConfig)

			// Allow for small floating point precision differences (within 1ms)
			timingTolerance := time.Millisecond

			if !durationsEqual(timing.CurrentOutroStart, tt.wantOutro, timingTolerance) {
				t.Errorf("CurrentOutroStart = %v, want %v", timing.CurrentOutroStart, tt.wantOutro)
			}
			if !durationsEqual(timing.NextIntroEnd, tt.wantIntro, timingTolerance) {
				t.Errorf("NextIntroEnd = %v, want %v", timing.NextIntroEnd, tt.wantIntro)
			}
			if timing.FadeOutDuration != time.Duration(tt.fadeConfig.FadeOutMs)*time.Millisecond {
				t.Errorf("FadeOutDuration = %v, want %v", timing.FadeOutDuration,
					time.Duration(tt.fadeConfig.FadeOutMs)*time.Millisecond)
			}
			if timing.FadeInDuration != time.Duration(tt.fadeConfig.FadeInMs)*time.Millisecond {
				t.Errorf("FadeInDuration = %v, want %v", timing.FadeInDuration,
					time.Duration(tt.fadeConfig.FadeInMs)*time.Millisecond)
			}

			// Overlap should be min of fade in/out
			expectedOverlap := timing.FadeOutDuration
			if timing.FadeInDuration < timing.FadeOutDuration {
				expectedOverlap = timing.FadeInDuration
			}
			if timing.OverlapDuration != expectedOverlap {
				t.Errorf("OverlapDuration = %v, want %v", timing.OverlapDuration, expectedOverlap)
			}

			t.Logf("Timing: outro=%v, intro=%v, overlap=%v",
				timing.CurrentOutroStart, timing.NextIntroEnd, timing.OverlapDuration)
		})
	}
}

// durationsEqual checks if two durations are equal within a tolerance
func durationsEqual(a, b, tolerance time.Duration) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

func TestCrossfadeManager_CalculateFadeTiming_Defaults(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	// Test with zero fade durations (should use defaults)
	timing := cfm.calculateFadeTiming(
		&Track{Duration: 180 * time.Second},
		&Track{},
		&pb.FadeConfig{
			FadeInMs:  0,
			FadeOutMs: 0,
		},
	)

	// Should default to 3 seconds
	if timing.FadeOutDuration != 3*time.Second {
		t.Errorf("Default FadeOutDuration = %v, want 3s", timing.FadeOutDuration)
	}
	if timing.FadeInDuration != 3*time.Second {
		t.Errorf("Default FadeInDuration = %v, want 3s", timing.FadeInDuration)
	}
}

func TestCrossfadeManager_buildCrossfadePipeline(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	currentTrack := &Track{
		SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
		Path:       "/media/current.mp3",
	}

	nextTrack := &Track{
		SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
		Path:       "/media/next.mp3",
		CuePoints: &pb.CuePoints{
			IntroEnd: 5.0, // Skip 5 seconds
		},
	}

	fadeConfig := &pb.FadeConfig{
		FadeInMs:  3000,
		FadeOutMs: 3000,
		Curve:     pb.FadeCurve_FADE_CURVE_SCURVE,
	}

	// Set fadeConfig before calling buildCrossfadePipeline
	cfm.mu.Lock()
	cfm.fadeConfig = fadeConfig
	cfm.mu.Unlock()

	timing := cfm.calculateFadeTiming(currentTrack, nextTrack, fadeConfig)
	pipeline, err := cfm.buildCrossfadePipeline(currentTrack, nextTrack, timing)
	if err != nil {
		t.Fatalf("buildCrossfadePipeline() failed: %v", err)
	}

	// Check pipeline contains expected elements
	requiredElements := []string{
		"filesrc", "location", "/media/current.mp3",
		"filesrc", "location", "/media/next.mp3",
		"decodebin",
		"audioconvert",
		"audioresample",
		"volume", "current_volume",
		"volume", "next_volume",
		"queue", "current_queue",
		"queue", "next_queue",
		"audiomixer", "name=mix",
	}

	for _, elem := range requiredElements {
		if !contains(pipeline, elem) {
			t.Errorf("Pipeline missing %q", elem)
		}
	}

	t.Logf("Crossfade pipeline: %s", pipeline)
}

func TestCrossfadeManager_buildSourceElement(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	tests := []struct {
		name         string
		track        *Track
		wantContains []string
	}{
		{
			name: "media file source",
			track: &Track{
				SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
				Path:       "/media/track.mp3",
			},
			wantContains: []string{"filesrc", "location=\"/media/track.mp3\"", "decodebin"},
		},
		{
			name: "webstream source",
			track: &Track{
				SourceType: pb.SourceType_SOURCE_TYPE_WEBSTREAM,
				Path:       "http://example.com/stream.mp3",
			},
			wantContains: []string{"souphttpsrc", "location=\"http://example.com/stream.mp3\"", "decodebin"},
		},
		{
			name: "media with intro cue point",
			track: &Track{
				SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
				Path:       "/media/track.mp3",
				CuePoints: &pb.CuePoints{
					IntroEnd: 10.5,
				},
			},
			wantContains: []string{"filesrc", "decodebin", "identity"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := cfm.buildSourceElement(tt.track, "test")
			if err != nil {
				t.Fatalf("buildSourceElement() failed: %v", err)
			}

			for _, want := range tt.wantContains {
				if !contains(source, want) {
					t.Errorf("Source missing %q: %s", want, source)
				}
			}

			t.Logf("Source: %s", source)
		})
	}
}

func TestCrossfadeManager_buildFadeController(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	fadeIn := cfm.buildFadeController("next", pb.FadeCurve_FADE_CURVE_LINEAR, 3*time.Second, true)
	if !contains(fadeIn, "volume") {
		t.Error("Fade in controller missing volume element")
	}
	if !contains(fadeIn, "volume=0.0") {
		t.Error("Fade in should start at volume=0.0")
	}

	fadeOut := cfm.buildFadeController("current", pb.FadeCurve_FADE_CURVE_LINEAR, 3*time.Second, false)
	if !contains(fadeOut, "volume") {
		t.Error("Fade out controller missing volume element")
	}
	if !contains(fadeOut, "volume=1.0") {
		t.Error("Fade out should start at volume=1.0")
	}
}

func TestCrossfadeManager_GetCurrentTrack(t *testing.T) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	// Initially nil
	track := cfm.GetCurrentTrack()
	if track != nil {
		t.Error("GetCurrentTrack() should return nil initially")
	}

	// Set a track
	cfm.mu.Lock()
	cfm.currentTrack = &Track{
		SourceID: "track-123",
	}
	cfm.mu.Unlock()

	track = cfm.GetCurrentTrack()
	if track == nil {
		t.Fatal("GetCurrentTrack() returned nil")
	}
	if track.SourceID != "track-123" {
		t.Errorf("SourceID = %s, want track-123", track.SourceID)
	}
}

func TestFadeTiming_OverlapCalculation(t *testing.T) {
	tests := []struct {
		name            string
		fadeOutMs       int32
		fadeInMs        int32
		expectedOverlap time.Duration
	}{
		{
			name:            "equal fades",
			fadeOutMs:       3000,
			fadeInMs:        3000,
			expectedOverlap: 3 * time.Second,
		},
		{
			name:            "fade out shorter",
			fadeOutMs:       2000,
			fadeInMs:        4000,
			expectedOverlap: 2 * time.Second,
		},
		{
			name:            "fade in shorter",
			fadeOutMs:       5000,
			fadeInMs:        3000,
			expectedOverlap: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			cfm := NewCrossfadeManager("station-123", "mount-456", logger)

			timing := cfm.calculateFadeTiming(
				&Track{Duration: 180 * time.Second},
				&Track{},
				&pb.FadeConfig{
					FadeOutMs: tt.fadeOutMs,
					FadeInMs:  tt.fadeInMs,
				},
			)

			if timing.OverlapDuration != tt.expectedOverlap {
				t.Errorf("OverlapDuration = %v, want %v", timing.OverlapDuration, tt.expectedOverlap)
			}
		})
	}
}

func BenchmarkCrossfadeManager_calculateFadeTiming(b *testing.B) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	currentTrack := &Track{
		Duration: 180 * time.Second,
		CuePoints: &pb.CuePoints{
			OutroIn: 170.0,
		},
	}

	nextTrack := &Track{
		CuePoints: &pb.CuePoints{
			IntroEnd: 5.5,
		},
	}

	fadeConfig := &pb.FadeConfig{
		FadeInMs:  3000,
		FadeOutMs: 3000,
		Curve:     pb.FadeCurve_FADE_CURVE_SCURVE,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfm.calculateFadeTiming(currentTrack, nextTrack, fadeConfig)
	}
}

func BenchmarkCrossfadeManager_buildCrossfadePipeline(b *testing.B) {
	logger := zerolog.Nop()
	cfm := NewCrossfadeManager("station-123", "mount-456", logger)

	currentTrack := &Track{
		SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
		Path:       "/media/current.mp3",
	}

	nextTrack := &Track{
		SourceType: pb.SourceType_SOURCE_TYPE_MEDIA,
		Path:       "/media/next.mp3",
	}

	fadeConfig := &pb.FadeConfig{
		FadeInMs:  3000,
		FadeOutMs: 3000,
		Curve:     pb.FadeCurve_FADE_CURVE_LINEAR,
	}

	timing := cfm.calculateFadeTiming(currentTrack, nextTrack, fadeConfig)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cfm.buildCrossfadePipeline(currentTrack, nextTrack, timing)
		if err != nil {
			b.Fatalf("buildCrossfadePipeline() failed: %v", err)
		}
	}
}
