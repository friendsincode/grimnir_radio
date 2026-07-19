/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package mediaengine

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func newRecordingManager() *RecordingManager {
	return NewRecordingManager(&Config{}, zerolog.Nop())
}

func TestBuildRecordingPipeline_Codecs(t *testing.T) {
	rm := newRecordingManager()

	cases := []struct {
		name string
		rec  *ActiveRecording
		want []string
	}{
		{
			name: "opus with explicit bitrate",
			rec:  &ActiveRecording{StationID: "s1", OutputPath: "/rec/a.opus", Codec: "opus", Bitrate: 96},
			want: []string{"interaudiosrc channel=s1-rec", "opusenc bitrate=96000", "oggmux", "filesink location=/rec/a.opus"},
		},
		{
			name: "opus defaults bitrate to 192",
			rec:  &ActiveRecording{StationID: "s2", OutputPath: "/rec/b.opus", Codec: "opus"},
			want: []string{"opusenc bitrate=192000"},
		},
		{
			name: "flac",
			rec:  &ActiveRecording{StationID: "s3", OutputPath: "/rec/c.flac", Codec: "flac"},
			want: []string{"flacenc", "filesink location=/rec/c.flac"},
		},
		{
			name: "unknown codec defaults to flac and 44100/2",
			rec:  &ActiveRecording{StationID: "s4", OutputPath: "/rec/d.out", Codec: "weird"},
			want: []string{"flacenc", "rate=44100,channels=2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rm.buildRecordingPipeline(tc.rec)
			if err != nil {
				t.Fatalf("buildRecordingPipeline() error: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("pipeline missing %q; got %q", w, got)
				}
			}
		})
	}
}

func TestRecordingManager_StateHelpers(t *testing.T) {
	rm := newRecordingManager()
	if rm.IsRecording("s1") {
		t.Error("IsRecording should be false with no recordings")
	}
	if id := rm.GetRecordingID("s1"); id != "" {
		t.Errorf("GetRecordingID = %q, want empty", id)
	}

	// Seed the index directly to exercise the lookups without spawning gst.
	rm.byStation["s1"] = "rec-1"
	if !rm.IsRecording("s1") {
		t.Error("IsRecording should be true after seeding")
	}
	if id := rm.GetRecordingID("s1"); id != "rec-1" {
		t.Errorf("GetRecordingID = %q, want rec-1", id)
	}
}

func TestStartRecording_RejectsDuplicateStation(t *testing.T) {
	rm := newRecordingManager()
	rm.byStation["s1"] = "existing"
	// The duplicate guard returns before any directory or process work.
	err := rm.StartRecording(context.Background(), &ActiveRecording{StationID: "s1", RecordingID: "new", OutputPath: "/rec/x.flac", Codec: "flac"})
	if err == nil {
		t.Fatal("expected error when station already has an active recording")
	}
}

func TestStopRecording_NotFound(t *testing.T) {
	rm := newRecordingManager()
	if _, _, err := rm.StopRecording("nope"); err == nil {
		t.Error("expected error stopping unknown recording")
	}
}

func TestStopAll_Empty(t *testing.T) {
	rm := newRecordingManager()
	rm.StopAll() // must not panic with no recordings
}
