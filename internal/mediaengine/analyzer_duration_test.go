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

func testLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	// Keep tests quiet; analyzer only uses logger for debug.
	return zerolog.Nop()
}
