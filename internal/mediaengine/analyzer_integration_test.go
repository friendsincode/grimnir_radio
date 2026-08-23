/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// makeAudioFixtureDur writes a stereo WAV of the given whole-second duration via
// ffmpeg and returns its path. Skips when ffmpeg is unavailable.
func makeAudioFixtureDur(t *testing.T, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	path := filepath.Join(t.TempDir(), "fixture.wav")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration="+strconv.Itoa(seconds), "-ac", "2", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg fixture generation failed: %v\n%s", err, out)
	}
	return path
}

// TestAnalyzer_DiscovererRaw_Integration runs the real gst-discoverer against a
// fixture of known duration and asserts the parsed result. This is the honest
// way to cover RunDiscovererRaw + parseDiscovererOutput end to end — mocking the
// subprocess boundary would prove nothing about the actual parse.
func TestAnalyzer_DiscovererRaw_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns gst-discoverer")
	}
	if _, err := exec.LookPath("gst-discoverer-1.0"); err != nil {
		t.Skip("gst-discoverer not available")
	}
	file := makeAudioFixtureDur(t, 2)
	a := NewAnalyzer(zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := a.RunDiscovererRaw(ctx, file)
	if err != nil {
		t.Fatalf("RunDiscovererRaw: %v", err)
	}
	// 2s fixture; allow slack for container/encoder rounding.
	if res.Duration < 1800*time.Millisecond || res.Duration > 2200*time.Millisecond {
		t.Errorf("Duration = %v, want ~2s", res.Duration)
	}
	if res.Channels != 2 {
		t.Errorf("Channels = %d, want 2", res.Channels)
	}
	if res.SampleRate <= 0 {
		t.Errorf("SampleRate = %d, want > 0", res.SampleRate)
	}
	if res.RawOutput == "" {
		t.Error("RawOutput should carry the discoverer text")
	}
}

// TestAnalyzer_AnalyzeMediaSync_Integration covers the synchronous analyze path
// (used by batch import) against the same known-duration fixture.
func TestAnalyzer_AnalyzeMediaSync_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns gst-discoverer/ffmpeg")
	}
	if _, err := exec.LookPath("gst-discoverer-1.0"); err != nil {
		t.Skip("gst-discoverer not available")
	}
	file := makeAudioFixtureDur(t, 2)
	a := NewAnalyzer(zerolog.Nop())

	resp, err := a.AnalyzeMediaSync(file, 30*time.Second)
	if err != nil {
		t.Fatalf("AnalyzeMediaSync: %v", err)
	}
	if resp.DurationMs < 1800 || resp.DurationMs > 2200 {
		t.Errorf("DurationMs = %d, want ~2000", resp.DurationMs)
	}
}

// TestAnalyzer_BatchAnalyze_Integration covers the concurrent batch path: every
// input file must come back with a result keyed by its path.
func TestAnalyzer_BatchAnalyze_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns gst-discoverer/ffmpeg")
	}
	if _, err := exec.LookPath("gst-discoverer-1.0"); err != nil {
		t.Skip("gst-discoverer not available")
	}
	f1 := makeAudioFixtureDur(t, 1)
	f2 := makeAudioFixtureDur(t, 2)
	a := NewAnalyzer(zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results := a.BatchAnalyze(ctx, []string{f1, f2}, 2)
	if len(results) != 2 {
		t.Fatalf("BatchAnalyze returned %d results, want 2", len(results))
	}
	if results[f1] == nil || results[f2] == nil {
		t.Errorf("both files should have results: %v", results)
	}
}
