/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package mediaengine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// CrossfadeManager handles seamless audio transitions with cue-point awareness
type CrossfadeManager struct {
	stationID string
	mountID   string
	logger    zerolog.Logger

	mu                  sync.RWMutex
	currentTrack        *Track
	nextTrack           *Track
	fadeState           FadeState
	fadeStartTime       time.Time
	fadeConfig          *pb.FadeConfig
	mixerProcess        *GStreamerProcess
	mixerPipelineString string
}

// FadeState represents the current crossfade state
type FadeState string

const (
	FadeStateIdle    FadeState = "idle"
	FadeStatePreload FadeState = "preload"  // Loading next track
	FadeStateFading  FadeState = "fading"   // Active crossfade
	FadeStateReady   FadeState = "ready"    // Next track ready to fade
)

// NewCrossfadeManager creates a new crossfade manager
func NewCrossfadeManager(stationID, mountID string, logger zerolog.Logger) *CrossfadeManager {
	return &CrossfadeManager{
		stationID: stationID,
		mountID:   mountID,
		logger:    logger.With().Str("component", "crossfade").Logger(),
		fadeState: FadeStateIdle,
	}
}

// StartCrossfade initiates a crossfade from current to next track
func (cfm *CrossfadeManager) StartCrossfade(ctx context.Context, currentTrack, nextTrack *Track, fadeConfig *pb.FadeConfig) error {
	cfm.mu.Lock()
	defer cfm.mu.Unlock()

	cfm.logger.Info().
		Str("current_track", currentTrack.SourceID).
		Str("next_track", nextTrack.SourceID).
		Int32("fade_in_ms", fadeConfig.FadeInMs).
		Int32("fade_out_ms", fadeConfig.FadeOutMs).
		Msg("starting crossfade")

	// Calculate fade timing based on cue points
	fadeTiming := cfm.calculateFadeTiming(currentTrack, nextTrack, fadeConfig)

	cfm.currentTrack = currentTrack
	cfm.nextTrack = nextTrack
	cfm.fadeConfig = fadeConfig
	cfm.fadeState = FadeStatePreload
	cfm.fadeStartTime = time.Now()

	// Build audiomixer pipeline
	pipeline, err := cfm.buildCrossfadePipeline(currentTrack, nextTrack, fadeTiming)
	if err != nil {
		return fmt.Errorf("build crossfade pipeline: %w", err)
	}

	cfm.mixerPipelineString = pipeline
	cfm.logger.Debug().Str("pipeline", pipeline).Msg("crossfade pipeline built")

	// Launch GStreamer mixer process
	if err := cfm.launchMixer(ctx, pipeline); err != nil {
		return fmt.Errorf("launch mixer: %w", err)
	}

	cfm.fadeState = FadeStateFading

	// Monitor fade completion
	go cfm.monitorFadeCompletion(fadeTiming.TotalDurationMs)

	return nil
}

// FadeTiming contains calculated timing for crossfade
type FadeTiming struct {
	CurrentOutroStart time.Duration // When to start fading out current track
	NextIntroEnd      time.Duration // When next track intro ends
	FadeOutDuration   time.Duration
	FadeInDuration    time.Duration
	OverlapDuration   time.Duration // How long both tracks play simultaneously
	TotalDurationMs   int64
}

// calculateFadeTiming determines optimal fade timing based on cue points
func (cfm *CrossfadeManager) calculateFadeTiming(current, next *Track, config *pb.FadeConfig) *FadeTiming {
	fadeOutMs := config.FadeOutMs
	fadeInMs := config.FadeInMs

	if fadeOutMs == 0 {
		fadeOutMs = 3000 // Default 3 second fade out
	}
	if fadeInMs == 0 {
		fadeInMs = 3000 // Default 3 second fade in
	}

	timing := &FadeTiming{
		FadeOutDuration: time.Duration(fadeOutMs) * time.Millisecond,
		FadeInDuration:  time.Duration(fadeInMs) * time.Millisecond,
	}

	// Use outro cue point if available
	if current.CuePoints != nil && current.CuePoints.OutroIn > 0 {
		// Start fade out when outro begins
		timing.CurrentOutroStart = time.Duration(current.CuePoints.OutroIn * float32(time.Second))
		cfm.logger.Debug().
			Float32("outro_in", current.CuePoints.OutroIn).
			Dur("outro_start", timing.CurrentOutroStart).
			Msg("using outro cue point")
	} else {
		// No cue point - start fade at end minus fade duration
		if current.Duration > 0 {
			timing.CurrentOutroStart = current.Duration - timing.FadeOutDuration
		}
	}

	// Use intro cue point if available
	if next.CuePoints != nil && next.CuePoints.IntroEnd > 0 {
		timing.NextIntroEnd = time.Duration(next.CuePoints.IntroEnd * float32(time.Second))
		cfm.logger.Debug().
			Float32("intro_end", next.CuePoints.IntroEnd).
			Dur("intro_end", timing.NextIntroEnd).
			Msg("using intro cue point")
	}

	// Calculate overlap - how long both tracks play together
	// Overlap = min(fade_out, fade_in)
	if timing.FadeOutDuration < timing.FadeInDuration {
		timing.OverlapDuration = timing.FadeOutDuration
	} else {
		timing.OverlapDuration = timing.FadeInDuration
	}

	timing.TotalDurationMs = int64(timing.OverlapDuration / time.Millisecond)

	return timing
}

// buildCrossfadePipeline constructs a GStreamer pipeline with audiomixer for crossfade
func (cfm *CrossfadeManager) buildCrossfadePipeline(current, next *Track, timing *FadeTiming) (string, error) {
	// Build source elements for both tracks
	currentSource, err := cfm.buildSourceElement(current, "current")
	if err != nil {
		return "", fmt.Errorf("build current source: %w", err)
	}

	nextSource, err := cfm.buildSourceElement(next, "next")
	if err != nil {
		return "", fmt.Errorf("build next source: %w", err)
	}

	// Build fade curve controllers
	currentFadeOut := cfm.buildFadeController("current", cfm.fadeConfig.Curve, timing.FadeOutDuration, false)
	nextFadeIn := cfm.buildFadeController("next", cfm.fadeConfig.Curve, timing.FadeInDuration, true)

	// Build audiomixer with crossfade
	var pipeline strings.Builder

	// Current track branch with fade out
	pipeline.WriteString(currentSource)
	pipeline.WriteString(" ! audioconvert ! audioresample ! ")
	pipeline.WriteString(currentFadeOut)
	pipeline.WriteString(" ! queue name=current_queue ! audiomixer name=mix. ")

	// Next track branch with fade in
	pipeline.WriteString(nextSource)
	pipeline.WriteString(" ! audioconvert ! audioresample ! ")
	pipeline.WriteString(nextFadeIn)
	pipeline.WriteString(" ! queue name=next_queue ! mix. ")

	// Mixer output
	pipeline.WriteString("mix. ! audioconvert ! audioresample ! ")
	pipeline.WriteString("autoaudiosink") // TODO: Replace with encoder/streamer

	return pipeline.String(), nil
}

// buildSourceElement creates a GStreamer source element for a track
func (cfm *CrossfadeManager) buildSourceElement(track *Track, namePrefix string) (string, error) {
	var source string

	switch track.SourceType {
	case pb.SourceType_SOURCE_TYPE_MEDIA:
		// File playback with optional start position (for cue points)
		source = fmt.Sprintf("filesrc location=\"%s\" name=%s_src ! decodebin name=%s_dec",
			track.Path, namePrefix, namePrefix)

		// Apply cue in point if specified
		if track.CuePoints != nil && track.CuePoints.IntroEnd > 0 {
			// Seek to intro end point (skip intro)
			seekTime := int64(track.CuePoints.IntroEnd * 1000000000) // Convert to nanoseconds
			source += fmt.Sprintf(" ! queue ! audiorate ! audioconvert ! audio/x-raw ! identity name=%s_identity sync=true start-time=%d",
				namePrefix, seekTime)
		}

	case pb.SourceType_SOURCE_TYPE_WEBSTREAM:
		source = fmt.Sprintf("souphttpsrc location=\"%s\" name=%s_src ! decodebin name=%s_dec",
			track.Path, namePrefix, namePrefix)

	case pb.SourceType_SOURCE_TYPE_LIVE:
		source = fmt.Sprintf("tcpserversrc port=8001 name=%s_src ! decodebin name=%s_dec",
			namePrefix, namePrefix)

	default:
		return "", fmt.Errorf("unsupported source type: %v", track.SourceType)
	}

	return source, nil
}

// buildFadeController creates a volume controller with fade curve
func (cfm *CrossfadeManager) buildFadeController(name string, curve pb.FadeCurve, duration time.Duration, fadeIn bool) string {
	// Use volume element with controller for smooth fades
	_ = float64(duration) / float64(time.Second) // durationSec for future use

	// Determine curve type (for future use with controller API)
	_ = curve // volumeCurve for future use

	if fadeIn {
		// Fade in: 0 → 1
		return fmt.Sprintf("volume name=%s_volume volume=0.0 ! identity name=%s_fade_in sync=true",
			name, name)
	} else {
		// Fade out: 1 → 0
		return fmt.Sprintf("volume name=%s_volume volume=1.0 ! identity name=%s_fade_out sync=true",
			name, name)
	}

	// Note: Actual volume automation would be done via GStreamer controller API
	// which requires GObject bindings, not available in command-line mode.
	// For production, we'd use proper GStreamer Go bindings with controller support.
}

// launchMixer starts the GStreamer mixer process
func (cfm *CrossfadeManager) launchMixer(ctx context.Context, pipeline string) error {
	processID := fmt.Sprintf("%s-%s-crossfade", cfm.stationID, cfm.mountID)

	// Create GStreamer process with callbacks
	cfm.mixerProcess = NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       processID,
		Pipeline: pipeline,
		LogLevel: "info",
		OnStateChange: func(state ProcessState) {
			cfm.logger.Debug().
				Str("process_state", string(state)).
				Msg("mixer process state changed")
		},
		OnTelemetry: func(telemetry *GStreamerTelemetry) {
			// Update crossfade manager with telemetry if needed
			// This could be used to track fade progress
			cfm.logger.Trace().
				Float32("audio_level_l", telemetry.AudioLevelL).
				Float32("peak_level_l", telemetry.PeakLevelL).
				Int64("underrun_count", telemetry.UnderrunCount).
				Msg("mixer telemetry update")
		},
		OnExit: func(exitCode int, err error) {
			if err != nil {
				cfm.logger.Error().
					Err(err).
					Int("exit_code", exitCode).
					Msg("mixer process exited with error")
			} else {
				cfm.logger.Info().Msg("mixer process completed normally")
			}

			// Clean up on exit
			cfm.mu.Lock()
			cfm.mixerProcess = nil
			cfm.mu.Unlock()
		},
	}, cfm.logger)

	// Start the process
	if err := cfm.mixerProcess.Start(pipeline); err != nil {
		return fmt.Errorf("failed to start mixer process: %w", err)
	}

	cfm.logger.Info().
		Int("pid", cfm.mixerProcess.GetPID()).
		Str("pipeline", pipeline).
		Msg("crossfade mixer process started")

	return nil
}

// monitorFadeCompletion waits for fade to complete and updates state
func (cfm *CrossfadeManager) monitorFadeCompletion(durationMs int64) {
	duration := time.Duration(durationMs) * time.Millisecond

	// Add buffer time for safety
	duration += 500 * time.Millisecond

	time.Sleep(duration)

	cfm.mu.Lock()
	defer cfm.mu.Unlock()

	cfm.logger.Info().
		Dur("duration", duration).
		Str("next_track", cfm.nextTrack.SourceID).
		Msg("crossfade completed")

	// Transition: next becomes current
	cfm.currentTrack = cfm.nextTrack
	cfm.nextTrack = nil
	cfm.fadeState = FadeStateIdle
}

// Stop stops the crossfade mixer
func (cfm *CrossfadeManager) Stop() error {
	cfm.mu.Lock()
	mixerProcess := cfm.mixerProcess
	cfm.mu.Unlock()

	if mixerProcess != nil {
		cfm.logger.Info().Msg("stopping crossfade mixer")
		if err := mixerProcess.Stop(); err != nil {
			cfm.logger.Error().Err(err).Msg("failed to stop mixer process gracefully")
			// Try force kill
			if killErr := mixerProcess.Kill(); killErr != nil {
				return fmt.Errorf("failed to kill mixer process: %w", killErr)
			}
		}

		cfm.mu.Lock()
		cfm.mixerProcess = nil
		cfm.mu.Unlock()
	}

	cfm.mu.Lock()
	cfm.fadeState = FadeStateIdle
	cfm.mu.Unlock()

	return nil
}

// GetCurrentTrack returns the currently playing track
func (cfm *CrossfadeManager) GetCurrentTrack() *Track {
	cfm.mu.RLock()
	defer cfm.mu.RUnlock()
	return cfm.currentTrack
}

// GetFadeState returns the current fade state
func (cfm *CrossfadeManager) GetFadeState() FadeState {
	cfm.mu.RLock()
	defer cfm.mu.RUnlock()
	return cfm.fadeState
}

// calculateFadeCurveVolume calculates volume at time t for a given curve
// progress: 0.0 to 1.0 (0 = start, 1 = end)
// fadeIn: true for fade in (0→1), false for fade out (1→0)
func calculateFadeCurveVolume(progress float64, curve pb.FadeCurve, fadeIn bool) float64 {
	// Clamp progress
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	var volume float64

	switch curve {
	case pb.FadeCurve_FADE_CURVE_LINEAR:
		volume = progress

	case pb.FadeCurve_FADE_CURVE_LOGARITHMIC:
		// Logarithmic curve: slower at start, faster at end
		if progress == 0 {
			volume = 0
		} else {
			volume = math.Log10(progress*9 + 1) // Log10(1) to Log10(10)
		}

	case pb.FadeCurve_FADE_CURVE_EXPONENTIAL:
		// Exponential curve: faster at start, slower at end
		volume = math.Pow(progress, 2)

	case pb.FadeCurve_FADE_CURVE_SCURVE:
		// S-curve (smooth): slow→fast→slow using cubic ease-in-out
		if progress < 0.5 {
			volume = 4 * progress * progress * progress
		} else {
			p := 2*progress - 2
			volume = 1 + p*p*p/2
		}

	default:
		volume = progress
	}

	// Invert for fade out
	if !fadeIn {
		volume = 1 - volume
	}

	return volume
}
