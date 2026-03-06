/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func newSocketAwareTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprint(r)
			if strings.Contains(msg, "httptest: failed to listen on a port") ||
				strings.Contains(strings.ToLower(msg), "operation not permitted") {
				t.Skipf("skipping test: local listener unavailable in this environment: %v", r)
			}
			panic(r)
		}
	}()
	return httptest.NewServer(handler)
}

func TestIsMasterPlaylist(t *testing.T) {
	master := []string{
		"#EXTM3U",
		"#EXT-X-STREAM-INF:BANDWIDTH=128000",
		"low/stream.m3u8",
	}
	if !isMasterPlaylist(master) {
		t.Error("expected master playlist")
	}

	media := []string{
		"#EXTM3U",
		"#EXT-X-TARGETDURATION:10",
		"#EXTINF:10,Artist - Song",
		"segment001.ts",
	}
	if isMasterPlaylist(media) {
		t.Error("expected media playlist")
	}
}

func TestResolveFirstVariant(t *testing.T) {
	lines := []string{
		"#EXTM3U",
		"#EXT-X-STREAM-INF:BANDWIDTH=128000",
		"low/stream.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=256000",
		"high/stream.m3u8",
	}

	got := resolveFirstVariant("https://cdn.example.com/live/master.m3u8", lines)
	want := "https://cdn.example.com/live/low/stream.m3u8"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFirstVariant_Absolute(t *testing.T) {
	lines := []string{
		"#EXTM3U",
		"#EXT-X-STREAM-INF:BANDWIDTH=128000",
		"https://other.cdn.com/stream.m3u8",
	}
	got := resolveFirstVariant("https://cdn.example.com/master.m3u8", lines)
	if got != "https://other.cdn.com/stream.m3u8" {
		t.Errorf("got %q", got)
	}
}

func TestSplitArtistTitle(t *testing.T) {
	tests := []struct {
		raw       string
		wantTitle string
		wantArt   string
	}{
		{"Radiohead - Creep", "Creep", "Radiohead"},
		{"Just A Title", "Just A Title", ""},
		{"A - B - C", "B - C", "A"},
	}
	for _, tt := range tests {
		title, artist, _ := splitArtistTitle(tt.raw)
		if title != tt.wantTitle || artist != tt.wantArt {
			t.Errorf("splitArtistTitle(%q) = (%q, %q), want (%q, %q)",
				tt.raw, title, artist, tt.wantTitle, tt.wantArt)
		}
	}
}

func TestHLSPoller_ParseMediaPlaylist(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXTINF:10,BBC Radio 1
segment001.ts
#EXTINF:10,Dua Lipa - Levitating
segment002.ts
`
	srv := newSocketAwareTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprint(w, playlist)
	}))
	defer srv.Close()

	p := NewHLSPoller("ws1", "st1", "m1", srv.URL+"/live.m3u8", nil, nil, zerolog.Nop())
	title, artist, err := p.parseHLSMetadata(t.Context(), srv.URL+"/live.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Levitating" || artist != "Dua Lipa" {
		t.Errorf("got title=%q artist=%q, want Levitating / Dua Lipa", title, artist)
	}
}

func TestHLSPoller_ParseMasterPlaylist(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=128000
media.m3u8
`
	media := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXTINF:10,Pink Floyd - Comfortably Numb
segment001.ts
`
	srv := newSocketAwareTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.URL.Path == "/media.m3u8" {
			fmt.Fprint(w, media)
		} else {
			fmt.Fprint(w, master)
		}
	}))
	defer srv.Close()

	p := NewHLSPoller("ws1", "st1", "m1", srv.URL+"/master.m3u8", nil, nil, zerolog.Nop())
	title, artist, err := p.parseHLSMetadata(t.Context(), srv.URL+"/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Comfortably Numb" || artist != "Pink Floyd" {
		t.Errorf("got title=%q artist=%q", title, artist)
	}
}

func TestHLSPoller_ExtXTitle(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-TITLE:Arctic Monkeys - Do I Wanna Know?
#EXTINF:10,
segment001.ts
`
	srv := newSocketAwareTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, playlist)
	}))
	defer srv.Close()

	p := NewHLSPoller("ws1", "st1", "m1", srv.URL+"/live.m3u8", nil, nil, zerolog.Nop())
	title, artist, err := p.parseHLSMetadata(t.Context(), srv.URL+"/live.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Do I Wanna Know?" || artist != "Arctic Monkeys" {
		t.Errorf("got title=%q artist=%q", title, artist)
	}
}
