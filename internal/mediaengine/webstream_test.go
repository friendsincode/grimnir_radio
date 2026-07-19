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

func TestFindURLIndex(t *testing.T) {
	urls := []string{"a", "b", "c"}
	if got := findURLIndex(urls, "b"); got != 1 {
		t.Errorf("findURLIndex(b) = %d, want 1", got)
	}
	// A miss falls back to 0 (the primary) rather than -1.
	if got := findURLIndex(urls, "zzz"); got != 0 {
		t.Errorf("findURLIndex(miss) = %d, want 0", got)
	}
	if got := findURLIndex(nil, "a"); got != 0 {
		t.Errorf("findURLIndex(nil) = %d, want 0", got)
	}
}

func TestPlayWebstream_RequiresID(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	err := wm.PlayWebstream(context.Background(), &WebstreamPlayRequest{URLs: []string{"http://x"}})
	if err == nil {
		t.Fatal("expected error for empty webstream_id")
	}
}

func TestPlayWebstream_RequiresURL(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	err := wm.PlayWebstream(context.Background(), &WebstreamPlayRequest{WebstreamID: "ws1"})
	if err == nil {
		t.Fatal("expected error when no URL is provided")
	}
}

func TestPlayWebstream_BuildsPipelineAndStores(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	req := &WebstreamPlayRequest{
		WebstreamID:     "ws1",
		StationID:       "st1",
		MountID:         "mt1",
		URLs:            []string{"http://a/1", "http://a/2"},
		ExtractMetadata: true,
		BufferSizeMS:    500,
		DSPGraphHandle:  "g1",
		FadeInMS:        1000,
	}
	if err := wm.PlayWebstream(context.Background(), req); err != nil {
		t.Fatalf("PlayWebstream() error: %v", err)
	}

	players := wm.GetActivePlayers()
	p, ok := players["ws1"]
	if !ok {
		t.Fatal("player ws1 not registered")
	}
	if !p.Connected {
		t.Error("expected player marked connected")
	}
	if p.CurrentURL != "http://a/1" {
		t.Errorf("CurrentURL = %q, want first URL", p.CurrentURL)
	}
	for _, want := range []string{"souphttpsrc", "iradio-mode=true", "queue max-size-time=500000000", "decodebin", "audioconvert ! audioresample", "volumeenvelope"} {
		if !strings.Contains(p.Pipeline, want) {
			t.Errorf("pipeline missing %q; got %q", want, p.Pipeline)
		}
	}
}

func TestPlayWebstream_AlreadyPlaying(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	req := &WebstreamPlayRequest{WebstreamID: "ws1", URLs: []string{"http://a"}}
	if err := wm.PlayWebstream(context.Background(), req); err != nil {
		t.Fatalf("first PlayWebstream() error: %v", err)
	}
	if err := wm.PlayWebstream(context.Background(), req); err == nil {
		t.Fatal("expected error for a webstream that is already playing")
	}
}

func TestStopWebstream(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	if err := wm.StopWebstream(context.Background(), "missing"); err == nil {
		t.Error("expected error stopping unknown webstream")
	}

	req := &WebstreamPlayRequest{WebstreamID: "ws1", URLs: []string{"http://a"}}
	_ = wm.PlayWebstream(context.Background(), req)
	if err := wm.StopWebstream(context.Background(), "ws1"); err != nil {
		t.Fatalf("StopWebstream() error: %v", err)
	}
	if _, ok := wm.GetActivePlayers()["ws1"]; ok {
		t.Error("player should be removed after stop")
	}
}

func TestFailoverWebstream(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	if err := wm.FailoverWebstream(context.Background(), "missing", "http://b"); err == nil {
		t.Error("expected error for unknown webstream failover")
	}

	req := &WebstreamPlayRequest{WebstreamID: "ws1", URLs: []string{"http://a", "http://b"}}
	_ = wm.PlayWebstream(context.Background(), req)
	if err := wm.FailoverWebstream(context.Background(), "ws1", "http://b"); err != nil {
		t.Fatalf("FailoverWebstream() error: %v", err)
	}
	p := wm.GetActivePlayers()["ws1"]
	if p.CurrentURL != "http://b" || p.CurrentIndex != 1 {
		t.Errorf("after failover CurrentURL=%q CurrentIndex=%d, want http://b/1", p.CurrentURL, p.CurrentIndex)
	}
}

func TestGetWebstreamMetadata(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	if _, err := wm.GetWebstreamMetadata("missing"); err == nil {
		t.Error("expected error for unknown webstream metadata")
	}

	req := &WebstreamPlayRequest{WebstreamID: "ws1", URLs: []string{"http://a"}}
	_ = wm.PlayWebstream(context.Background(), req)
	wm.GetActivePlayers()["ws1"].Metadata["StreamTitle"] = "Song"
	md, err := wm.GetWebstreamMetadata("ws1")
	if err != nil {
		t.Fatalf("GetWebstreamMetadata() error: %v", err)
	}
	if md["StreamTitle"] != "Song" {
		t.Errorf("metadata StreamTitle = %q, want Song", md["StreamTitle"])
	}
}

func TestWebstreamShutdown(t *testing.T) {
	wm := NewWebstreamManager(zerolog.Nop())
	_ = wm.PlayWebstream(context.Background(), &WebstreamPlayRequest{WebstreamID: "ws1", URLs: []string{"http://a"}})
	_ = wm.PlayWebstream(context.Background(), &WebstreamPlayRequest{WebstreamID: "ws2", URLs: []string{"http://b"}})
	if err := wm.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
	if len(wm.GetActivePlayers()) != 0 {
		t.Error("expected no active players after shutdown")
	}
}
