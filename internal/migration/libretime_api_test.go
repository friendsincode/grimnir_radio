/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ltServer stands up an httptest server and returns a client pointed at it,
// plus the request URIs the client sent, in order.
func ltServer(t *testing.T, handler http.HandlerFunc) (*LibreTimeAPIClient, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	client, err := NewLibreTimeAPIClient(srv.URL, "lt-key")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &paths
}

func intPtr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// construction and plumbing
// ---------------------------------------------------------------------------

func TestNewLibreTimeAPIClient_NormalizesBaseURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://lt.example.com", "https://lt.example.com"},
		{"https://lt.example.com/", "https://lt.example.com"},
		{"http://10.0.0.5:8080", "http://10.0.0.5:8080"},
		{"lt.example.com", "https://lt.example.com"},
	} {
		client, err := NewLibreTimeAPIClient(tc.in, "k")
		if err != nil {
			t.Fatalf("new(%q): %v", tc.in, err)
		}
		if client.baseURL != tc.want {
			t.Fatalf("new(%q) baseURL = %q, want %q", tc.in, client.baseURL, tc.want)
		}
	}
}

// LibreTime uses "Api-Key <key>", not Bearer.
func TestLibreTimeDoRequest_SendsApiKeyAuthorization(t *testing.T) {
	var gotAuth, gotAccept string
	client, _ := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		writeJSON(t, w, []LTFile{})
	})

	if _, err := client.GetFiles(actx()); err != nil {
		t.Fatalf("get files: %v", err)
	}
	if gotAuth != "Api-Key lt-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
}

func TestLibreTimeDecode_ErrorStatusIncludesBody(t *testing.T) {
	client, _ := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "invalid api key")
	})

	_, err := client.GetFiles(actx())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("err = %v", err)
	}
}

func TestLibreTimeDoRequest_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, err := NewLibreTimeAPIClient(srv.URL, "k")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	srv.Close()

	if _, err := client.GetFiles(actx()); err == nil {
		t.Fatal("expected a transport error")
	}
}

// ---------------------------------------------------------------------------
// read endpoints
// ---------------------------------------------------------------------------

func TestLibreTimeReadEndpoints(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{{ID: 1, Title: "Blue Monday", Artist: "New Order", Length: "00:07:29.000", Size: 7180000}})
		case "/api/v2/playlists":
			writeJSON(t, w, []LTPlaylist{{ID: 4, Name: "Morning", Length: "01:00:00"}})
		case "/api/v2/shows":
			writeJSON(t, w, []LTShow{{ID: 8, Name: "Nightshift", Genre: "Electronic", Timezone: "Europe/Oslo"}})
		case "/api/v2/show-days":
			writeJSON(t, w, []LTShowDays{{ID: 3, ShowID: 8, Day: 0, StartTime: "22:00:00", Duration: "02:00:00", RepeatType: 0}})
		case "/api/v2/webstreams":
			writeJSON(t, w, []LTWebstream{{ID: 2, Name: "Relay", URL: "http://upstream.example/live"}})
		case "/api/v2/smart-blocks":
			writeJSON(t, w, []LTSmartBlock{{ID: 6, Name: "Recent Adds", Kind: "dynamic"}})
		case "/api/v2/info":
			writeJSON(t, w, &LTStationInfo{StationName: "Grimnir", StationTimezone: "Europe/Oslo"})
		case "/api/v2/listener-count":
			writeJSON(t, w, &LTListenerStats{ListenerCount: 137})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	files, err := client.GetFiles(actx())
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	if len(files) != 1 || files[0].Title != "Blue Monday" || files[0].Size != 7180000 {
		t.Fatalf("files = %+v", files)
	}

	playlists, err := client.GetPlaylists(actx())
	if err != nil {
		t.Fatalf("playlists: %v", err)
	}
	if len(playlists) != 1 || playlists[0].Name != "Morning" {
		t.Fatalf("playlists = %+v", playlists)
	}

	shows, err := client.GetShows(actx())
	if err != nil {
		t.Fatalf("shows: %v", err)
	}
	if len(shows) != 1 || shows[0].Timezone != "Europe/Oslo" {
		t.Fatalf("shows = %+v", shows)
	}

	days, err := client.GetShowDays(actx())
	if err != nil {
		t.Fatalf("show days: %v", err)
	}
	if len(days) != 1 || days[0].StartTime != "22:00:00" {
		t.Fatalf("show days = %+v", days)
	}

	streams, err := client.GetWebstreams(actx())
	if err != nil {
		t.Fatalf("webstreams: %v", err)
	}
	if len(streams) != 1 || streams[0].URL != "http://upstream.example/live" {
		t.Fatalf("webstreams = %+v", streams)
	}

	blocks, err := client.GetSmartBlocks(actx())
	if err != nil {
		t.Fatalf("smart blocks: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Kind != "dynamic" {
		t.Fatalf("smart blocks = %+v", blocks)
	}

	info, err := client.GetStationInfo(actx())
	if err != nil {
		t.Fatalf("station info: %v", err)
	}
	if info.StationName != "Grimnir" {
		t.Fatalf("station info = %+v", info)
	}

	stats, err := client.GetListenerStats(actx())
	if err != nil {
		t.Fatalf("listener stats: %v", err)
	}
	if stats.ListenerCount != 137 {
		t.Fatalf("listeners = %d, want 137", stats.ListenerCount)
	}

	if len(*paths) != 8 {
		t.Fatalf("requests = %d, want 8: %v", len(*paths), *paths)
	}
}

// Show instances are fetched unfiltered when no show ID is given, and with a
// ?show= filter when one is.
func TestLibreTimeGetShowInstances_FilterQuery(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []LTShowInstance{{ID: 1, ShowID: 8, Starts: time.Now(), Ends: time.Now().Add(time.Hour)}})
	})

	if _, err := client.GetShowInstances(actx(), 0); err != nil {
		t.Fatalf("unfiltered: %v", err)
	}
	if _, err := client.GetShowInstances(actx(), 8); err != nil {
		t.Fatalf("filtered: %v", err)
	}

	if (*paths)[0] != "/api/v2/show-instances" {
		t.Fatalf("unfiltered path = %q", (*paths)[0])
	}
	if (*paths)[1] != "/api/v2/show-instances?show=8" {
		t.Fatalf("filtered path = %q", (*paths)[1])
	}
}

func TestLibreTimeGetSmartBlockCriteria_FilterQuery(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []LTSmartBlockCriteria{{ID: 1, BlockID: 6, Criteria: "genre", Condition: "is", Value: "Electronic"}})
	})

	criteria, err := client.GetSmartBlockCriteria(actx(), 6)
	if err != nil {
		t.Fatalf("criteria: %v", err)
	}
	if len(criteria) != 1 || criteria[0].Value != "Electronic" {
		t.Fatalf("criteria = %+v", criteria)
	}
	if (*paths)[0] != "/api/v2/smart-block-criteria?block=6" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

func TestLibreTimeGetSchedule_EncodesTimeRange(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("starts_after") == "" || q.Get("ends_before") == "" {
			t.Errorf("missing range params: %v", r.URL.RawQuery)
		}
		writeJSON(t, w, []LTScheduleEntry{{ID: 1, InstanceID: 5, ClipLength: "00:03:30"}})
	})

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)

	entries, err := client.GetSchedule(actx(), start, end)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(entries) != 1 || entries[0].ClipLength != "00:03:30" {
		t.Fatalf("entries = %+v", entries)
	}
	if !strings.Contains((*paths)[0], "2026-07-01T00%3A00%3A00Z") {
		t.Fatalf("path = %q, want the RFC3339 start escaped into the query", (*paths)[0])
	}
}

// ---------------------------------------------------------------------------
// playlist contents caching
// ---------------------------------------------------------------------------

// The LibreTime API ignores the playlist filter, so the client fetches every
// row once and filters client-side. Re-requesting per playlist on a library
// with hundreds of playlists is what the cache exists to prevent.
func TestLibreTimeGetPlaylistContents_CachesAndFilters(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []LTPlaylistContent{
			{ID: 1, PlaylistID: intPtr(4), FileID: intPtr(100), Position: 0},
			{ID: 2, PlaylistID: intPtr(4), FileID: intPtr(101), Position: 1},
			{ID: 3, PlaylistID: intPtr(9), FileID: intPtr(102), Position: 0},
			{ID: 4, PlaylistID: nil, FileID: intPtr(103), Position: 0}, // orphaned row
		})
	})

	four, err := client.GetPlaylistContents(actx(), 4)
	if err != nil {
		t.Fatalf("playlist 4: %v", err)
	}
	if len(four) != 2 {
		t.Fatalf("playlist 4 items = %d, want 2", len(four))
	}
	if four[0].ID != 1 || four[1].ID != 2 {
		t.Fatalf("playlist 4 = %+v", four)
	}

	nine, err := client.GetPlaylistContents(actx(), 9)
	if err != nil {
		t.Fatalf("playlist 9: %v", err)
	}
	if len(nine) != 1 || nine[0].ID != 3 {
		t.Fatalf("playlist 9 = %+v", nine)
	}

	// A playlist with no rows returns empty, not an error.
	empty, err := client.GetPlaylistContents(actx(), 999)
	if err != nil {
		t.Fatalf("playlist 999: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("playlist 999 = %+v, want empty", empty)
	}

	if len(*paths) != 1 {
		t.Fatalf("requests = %d, want 1 cached fetch: %v", len(*paths), *paths)
	}
	if (*paths)[0] != "/api/v2/playlist-contents" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

func TestLibreTimeGetPlaylistContents_FetchErrorIsNotCached(t *testing.T) {
	var calls int
	client, _ := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := client.GetPlaylistContents(actx(), 4); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := client.GetPlaylistContents(actx(), 4); err == nil {
		t.Fatal("expected an error on the retry too")
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want the failure to be retried rather than cached", calls)
	}
}

// ---------------------------------------------------------------------------
// downloads
// ---------------------------------------------------------------------------

func TestLibreTimeDownloadFile(t *testing.T) {
	payload := "fake-ogg-bytes"
	client, paths := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, payload)
	})

	body, size, err := client.DownloadFile(actx(), 42)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("body = %q", got)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
	if (*paths)[0] != "/api/v2/files/42/download" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

func TestLibreTimeDownloadFile_ErrorStatus(t *testing.T) {
	client, _ := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "storage offline")
	})

	_, _, err := client.DownloadFile(actx(), 42)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "storage offline") {
		t.Fatalf("err = %v", err)
	}
}

func TestLibreTimeDownloadShowImage(t *testing.T) {
	img := []byte{0x89, 0x50, 0x4e, 0x47}

	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		wantData    bool
		wantMime    string
	}{
		{"png image", http.StatusOK, "image/png", true, "image/png"},
		{"missing content type defaults to jpeg", http.StatusOK, "", true, "image/jpeg"},
		{"not found", http.StatusNotFound, "", false, ""},
		{"no content", http.StatusNoContent, "", false, ""},
		{"server error is skipped silently", http.StatusInternalServerError, "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, paths := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				} else {
					w.Header()["Content-Type"] = nil
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write(img)
				}
			})

			data, mime, err := client.DownloadShowImage(actx(), 8)
			if err != nil {
				t.Fatalf("download: %v", err)
			}
			if tc.wantData {
				if string(data) != string(img) {
					t.Fatalf("data = %v", data)
				}
				if mime != tc.wantMime {
					t.Fatalf("mime = %q, want %q", mime, tc.wantMime)
				}
			} else if len(data) != 0 {
				t.Fatalf("got %d bytes, want none", len(data))
			}
			if (*paths)[0] != "/api/v2/shows/8/image" {
				t.Fatalf("path = %q", (*paths)[0])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestConnection
// ---------------------------------------------------------------------------

func TestLibreTimeTestConnection_Healthy(t *testing.T) {
	client, _ := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/version":
			writeJSON(t, w, map[string]string{"api_version": "4.2.0"})
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{{ID: 1, Title: "Blue Monday"}})
		case "/api/v2/files/1/download":
			_, _ = io.WriteString(w, "audio")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	status, err := client.TestConnection(actx())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if !status.Online || !status.FilesAccessible {
		t.Fatalf("status = %+v", status)
	}
	if status.Version != "4.2.0" {
		t.Fatalf("version = %q, want the value from /version", status.Version)
	}
	if status.Warning != "" {
		t.Fatalf("warning = %q, want none", status.Warning)
	}
}

// An empty library is a valid LibreTime install, so no download is attempted
// and nothing is flagged.
func TestLibreTimeTestConnection_EmptyLibrary(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/version":
			writeJSON(t, w, map[string]string{"api_version": "4.2.0"})
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	status, err := client.TestConnection(actx())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if !status.FilesAccessible || status.Warning != "" {
		t.Fatalf("status = %+v", status)
	}
	if len(*paths) != 2 {
		t.Fatalf("requests = %v, want no download attempt", *paths)
	}
}

// The API answering but the file listing failing is reported as a warning, not
// a hard failure: the operator can still see the connection works.
func TestLibreTimeTestConnection_FileListingFails(t *testing.T) {
	client, _ := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/version":
			writeJSON(t, w, map[string]string{"api_version": "4.2.0"})
		case "/api/v2/files":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "no permission")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	status, err := client.TestConnection(actx())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if !status.Online {
		t.Fatal("should still report online")
	}
	if !strings.Contains(status.Warning, "file listing may fail") {
		t.Fatalf("warning = %q", status.Warning)
	}
}

// Listing works but downloads 403: that is the case where an import would
// create every DB row and fetch no audio, so it has to be flagged up front.
func TestLibreTimeTestConnection_DownloadsBlocked(t *testing.T) {
	client, _ := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/version":
			writeJSON(t, w, map[string]string{"api_version": "4.2.0"})
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{{ID: 1}})
		case "/api/v2/files/1/download":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	status, err := client.TestConnection(actx())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if status.FilesAccessible {
		t.Fatal("FilesAccessible should be false when downloads are refused")
	}
	if !strings.Contains(status.Warning, "file downloads may fail") {
		t.Fatalf("warning = %q", status.Warning)
	}
}

// Older builds have no /version endpoint; the client falls back to /info.
func TestLibreTimeTestConnection_FallsBackToInfo(t *testing.T) {
	client, paths := ltServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/version":
			w.WriteHeader(http.StatusNotFound)
		case "/api/v2/info":
			writeJSON(t, w, &LTStationInfo{StationName: "Grimnir"})
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	status, err := client.TestConnection(actx())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if !status.Online {
		t.Fatal("should report online via the /info fallback")
	}
	if status.Version != "" {
		t.Fatalf("version = %q, want empty when /version is absent", status.Version)
	}
	if len(*paths) < 2 || (*paths)[1] != "/api/v2/info" {
		t.Fatalf("paths = %v, want the /info fallback", *paths)
	}
}

// Both endpoints failing is a genuine connection failure.
func TestLibreTimeTestConnection_BothEndpointsFail(t *testing.T) {
	client, _ := ltServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	if _, err := client.TestConnection(actx()); err == nil {
		t.Fatal("expected a connection failure")
	}
}

func TestLibreTimeTestConnection_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, err := NewLibreTimeAPIClient(srv.URL, "k")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	srv.Close()

	_, err = client.TestConnection(actx())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "API unreachable") {
		t.Fatalf("err = %v", err)
	}
}
