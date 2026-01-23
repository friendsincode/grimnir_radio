package mediaengine

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestGStreamerProcess_StateTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping GStreamer process test in short mode")
	}

	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-process",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	// Check initial state
	if process.GetState() != ProcessStateIdle {
		t.Errorf("Initial state = %s, want %s", process.GetState(), ProcessStateIdle)
	}

	// Note: We don't actually start a process here since we'd need GStreamer installed
	// This tests the object creation and state management
}

func TestGStreamerProcess_GetPID(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-pid",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	pid := process.GetPID()
	if pid != 0 {
		t.Errorf("GetPID() before start = %d, want 0", pid)
	}
}

func TestGStreamerProcess_GetUptime(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-uptime",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	uptime := process.GetUptime()
	if uptime != 0 {
		t.Errorf("GetUptime() before start = %v, want 0", uptime)
	}
}

func TestGStreamerProcess_Callbacks(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	stateChanges := make([]ProcessState, 0)
	var lastState ProcessState

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-callbacks",
		Pipeline: "",
		LogLevel: "error",
		OnStateChange: func(state ProcessState) {
			stateChanges = append(stateChanges, state)
			lastState = state
		},
	}, logger)

	if process.onStateChange == nil {
		t.Error("OnStateChange callback not set")
	}

	// Trigger callback manually for testing
	if process.onStateChange != nil {
		process.onStateChange(ProcessStateStarting)
		if lastState != ProcessStateStarting {
			t.Errorf("State callback not triggered correctly, got %s", lastState)
		}
	}
}

func TestGStreamerTelemetry_GetTelemetry(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-telemetry",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	telemetry := process.GetTelemetry()
	if telemetry == nil {
		t.Fatal("GetTelemetry() returned nil")
	}

	// Check initial telemetry values
	if telemetry.AudioLevelL != 0 {
		t.Errorf("Initial AudioLevelL = %f, want 0", telemetry.AudioLevelL)
	}
	if telemetry.UnderrunCount != 0 {
		t.Errorf("Initial UnderrunCount = %d, want 0", telemetry.UnderrunCount)
	}
}

func TestGStreamerProcess_ParseStateChange(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-parse",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	// Test parsing state change line
	line := "Setting pipeline to PLAYING"
	process.parseOutputLine(line, "stderr")

	telemetry := process.GetTelemetry()
	if telemetry.PipelineState != "PLAYING" {
		t.Errorf("Parsed state = %s, want PLAYING", telemetry.PipelineState)
	}
}

func TestGStreamerProcess_ParseError(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-error",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	// Test parsing error line
	line := "ERROR: from element /GstPipeline:pipeline0/GstDecodeBin:decodebin0: Your GStreamer installation is missing a plug-in."
	process.parseOutputLine(line, "stderr")

	telemetry := process.GetTelemetry()
	if telemetry.LastError == "" {
		t.Error("Error not captured from output line")
	}
}

func TestGStreamerProcess_ParseWarning(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-warning",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	// Test parsing warning line
	line := "WARNING: erroneous pipeline: no element \"fakesource\""
	process.parseOutputLine(line, "stderr")

	telemetry := process.GetTelemetry()
	if telemetry.LastWarning == "" {
		t.Error("Warning not captured from output line")
	}
}

func TestGStreamerProcess_ParseUnderrun(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-underrun",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	initialUnderruns := process.GetTelemetry().UnderrunCount

	// Test parsing underrun line
	line := "WARN queue :0:: queue0: underrun, consider increasing buffer size"
	process.parseOutputLine(line, "stderr")

	telemetry := process.GetTelemetry()
	if telemetry.UnderrunCount != initialUnderruns+1 {
		t.Errorf("Underrun count = %d, want %d", telemetry.UnderrunCount, initialUnderruns+1)
	}
}

func TestGStreamerProcess_ConcurrentTelemetryAccess(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "test-concurrent",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	// Test concurrent access to telemetry
	done := make(chan bool)

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = process.GetTelemetry()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Writer goroutine (simulates output parsing)
	go func() {
		for i := 0; i < 100; i++ {
			process.parseOutputLine("Setting pipeline to PLAYING", "stderr")
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// If we get here without deadlock or panic, test passes
}

func TestCalculateFadeCurveVolume_Linear(t *testing.T) {
	tests := []struct {
		progress float64
		fadeIn   bool
		want     float64
	}{
		{0.0, true, 0.0},
		{0.5, true, 0.5},
		{1.0, true, 1.0},
		{0.0, false, 1.0},
		{0.5, false, 0.5},
		{1.0, false, 0.0},
	}

	for _, tt := range tests {
		got := calculateFadeCurveVolume(tt.progress, 1, tt.fadeIn) // 1 = LINEAR
		if got != tt.want {
			t.Errorf("calculateFadeCurveVolume(%.1f, LINEAR, %v) = %.2f, want %.2f",
				tt.progress, tt.fadeIn, got, tt.want)
		}
	}
}

func TestCalculateFadeCurveVolume_Clamp(t *testing.T) {
	// Test that values outside 0-1 are clamped
	tests := []struct {
		progress float64
		want     float64
	}{
		{-0.5, 0.0},
		{-1.0, 0.0},
		{1.5, 1.0},
		{2.0, 1.0},
	}

	for _, tt := range tests {
		got := calculateFadeCurveVolume(tt.progress, 1, true) // 1 = LINEAR, fadeIn = true
		if got != tt.want {
			t.Errorf("calculateFadeCurveVolume(%.1f) = %.2f, want %.2f (clamping test)",
				tt.progress, got, tt.want)
		}
	}
}

func TestCalculateFadeCurveVolume_Curves(t *testing.T) {
	// Test different curve types at 0.5 progress
	progress := 0.5

	// Linear should be 0.5
	linear := calculateFadeCurveVolume(progress, 1, true)
	if linear != 0.5 {
		t.Errorf("Linear curve at 0.5 = %.2f, want 0.5", linear)
	}

	// Logarithmic (slow start, fast end) should be greater than linear at midpoint
	log := calculateFadeCurveVolume(progress, 2, true)
	if log <= linear {
		t.Errorf("Logarithmic curve should be > linear at 0.5, got %.2f <= %.2f", log, linear)
	}

	// Exponential (fast start, slow end) should be less than linear at midpoint
	exp := calculateFadeCurveVolume(progress, 3, true)
	if exp >= linear {
		t.Errorf("Exponential curve should be < linear at 0.5, got %.2f >= %.2f", exp, linear)
	}

	// All should be between 0 and 1
	curves := []float64{linear, log, exp}
	for i, v := range curves {
		if v < 0 || v > 1 {
			t.Errorf("Curve %d value %.2f out of range [0, 1]", i, v)
		}
	}
}

func BenchmarkGStreamerProcess_GetTelemetry(b *testing.B) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "bench-telemetry",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = process.GetTelemetry()
	}
}

func BenchmarkGStreamerProcess_ParseOutputLine(b *testing.B) {
	logger := zerolog.Nop()
	ctx := context.Background()

	process := NewGStreamerProcess(ctx, GStreamerProcessConfig{
		ID:       "bench-parse",
		Pipeline: "",
		LogLevel: "error",
	}, logger)

	line := "0:00:01.234567890 12345 0x7f123456 INFO level :0:: RMS: -12.34 dB, peak: -6.78 dB"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		process.parseOutputLine(line, "stdout")
	}
}

func BenchmarkCalculateFadeCurveVolume(b *testing.B) {
	progress := 0.5

	b.Run("Linear", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = calculateFadeCurveVolume(progress, 1, true)
		}
	})

	b.Run("Logarithmic", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = calculateFadeCurveVolume(progress, 2, true)
		}
	})

	b.Run("Exponential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = calculateFadeCurveVolume(progress, 3, true)
		}
	})

	b.Run("SCurve", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = calculateFadeCurveVolume(progress, 4, true)
		}
	})
}
