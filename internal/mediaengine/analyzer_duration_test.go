package mediaengine

import (
	"testing"

	"github.com/rs/zerolog"

	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

func TestFracToMilliseconds(t *testing.T) {
	cases := []struct {
		name string
		frac string
		want int64
	}{
		{"empty", "", 0},
		{"ms_3_digits", "345", 345},
		{"ns_9_digits", "345000000", 345},
		{"one_digit_tenths", "1", 100},
		{"two_digits_hundredths", "12", 120},
		{"leading_zeros", "004", 4},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := fracToMilliseconds(tt.frac); got != tt.want {
				t.Fatalf("fracToMilliseconds(%q) = %d, want %d", tt.frac, got, tt.want)
			}
		})
	}
}

func TestParseDiscovererOutput_DurationNanosecondsFraction(t *testing.T) {
	a := NewAnalyzer(testLogger(t))
	resp := &pb.AnalyzeMediaResponse{Metadata: &pb.MediaMetadata{}}

	a.parseDiscovererOutput(`
Analyzing file: test.mp3
Done discovering test.mp3
Duration: 0:58:12.345000000
`, resp)

	// 58m12.345s => 3492345ms
	const want int64 = 58*60*1000 + 12*1000 + 345
	if resp.DurationMs != want {
		t.Fatalf("DurationMs = %d, want %d", resp.DurationMs, want)
	}
}

// TestParseDiscovererOutput_CapitalizedFields pins the case-insensitive parse of
// the audio stream properties. gst-discoverer prints "Sample rate:" and
// "Channels:" capitalized; before the (?i) fix the lowercase-only regexes never
// matched and every analyzed track reported 0 channels / 0 sample rate. This is
// a captured slice of real gst-discoverer -v output, so it guards the fix
// without needing GStreamer installed.
func TestParseDiscovererOutput_CapitalizedFields(t *testing.T) {
	a := NewAnalyzer(testLogger(t))
	resp := &pb.AnalyzeMediaResponse{Metadata: &pb.MediaMetadata{}}

	a.parseDiscovererOutput(`
Analyzing file:///tmp/f.wav
Done discovering file:///tmp/f.wav
Properties:
  Duration: 0:00:02.000000000
  container format: WAV
  audio codec: Uncompressed 16-bit PCM audio
  audio #0: audio/x-wav
    Channels: 2 (front-left, front-right)
    Sample rate: 44100
    Depth: 16
    Bitrate: 1411200
`, resp)

	if resp.Channels != 2 {
		t.Errorf("Channels = %d, want 2 (capitalized 'Channels:' must match)", resp.Channels)
	}
	if resp.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100 (capitalized 'Sample rate:' must match)", resp.SampleRate)
	}
	if resp.DurationMs != 2000 {
		t.Errorf("DurationMs = %d, want 2000", resp.DurationMs)
	}
	if resp.Bitrate != 1411 { // bps -> kbps
		t.Errorf("Bitrate = %d, want 1411", resp.Bitrate)
	}
}

func testLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	// Keep tests quiet; analyzer only uses logger for debug.
	return zerolog.Nop()
}
