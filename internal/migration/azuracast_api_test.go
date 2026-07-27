/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// azuraServer stands up an httptest server and returns a client pointed at it,
// plus the paths the client requested in order.
func azuraServer(t *testing.T, handler http.HandlerFunc) (*AzuraCastAPIClient, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	client, err := NewAzuraCastAPIClient(srv.URL, "test-key")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &paths
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func actx() context.Context { return context.Background() }

// ---------------------------------------------------------------------------
// construction
// ---------------------------------------------------------------------------

func TestNewAzuraCastAPIClient_NormalizesBaseURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://radio.example.com", "https://radio.example.com"},
		{"https://radio.example.com/", "https://radio.example.com"},
		{"http://192.168.1.10:8080", "http://192.168.1.10:8080"},
		// A bare host gets https, not http: imports carry an API key.
		{"radio.example.com", "https://radio.example.com"},
		{"radio.example.com/", "https://radio.example.com"},
	} {
		client, err := NewAzuraCastAPIClient(tc.in, "k")
		if err != nil {
			t.Fatalf("new(%q): %v", tc.in, err)
		}
		if client.baseURL != tc.want {
			t.Fatalf("new(%q) baseURL = %q, want %q", tc.in, client.baseURL, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// request plumbing
// ---------------------------------------------------------------------------

func TestAzuraCastDoRequest_SendsAPIKeyHeaders(t *testing.T) {
	var gotKey, gotAuth, gotAccept string
	client, _ := azuraServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		writeJSON(t, w, &AzuraCastAPIStatus{Online: true})
	})

	if _, err := client.TestConnection(actx()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if gotKey != "test-key" {
		t.Fatalf("X-API-Key = %q", gotKey)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
}

// With no API key, the session token from a username/password login carries the
// request instead.
func TestAzuraCastDoRequest_FallsBackToSessionToken(t *testing.T) {
	var gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotAuth = r.Header.Get("Authorization")
		writeJSON(t, w, &AzuraCastAPIStatus{Online: true})
	}))
	defer srv.Close()

	client, err := NewAzuraCastAPIClient(srv.URL, "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.sessionToken = "sess-abc"

	if _, err := client.TestConnection(actx()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if gotKey != "" {
		t.Fatalf("X-API-Key = %q, want empty", gotKey)
	}
	if gotAuth != "Bearer sess-abc" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestAzuraCastDecode_ErrorStatusIncludesBody(t *testing.T) {
	client, _ := azuraServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "insufficient permissions")
	})

	_, err := client.GetStations(actx())
	if err == nil {
		t.Fatal("expected an error for a 403")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "insufficient permissions") {
		t.Fatalf("err = %v, want the status and body echoed", err)
	}
}

func TestAzuraCastDecode_MalformedJSON(t *testing.T) {
	client, _ := azuraServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>login page</html>")
	})

	if _, err := client.GetStations(actx()); err == nil {
		t.Fatal("expected a decode error for a non-JSON body")
	}
}

// A dead host must surface as an error rather than a nil response.
func TestAzuraCastDoRequest_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, err := NewAzuraCastAPIClient(srv.URL, "k")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	srv.Close() // nothing is listening now

	if _, err := client.GetStations(actx()); err == nil {
		t.Fatal("expected a transport error")
	}
}

// ---------------------------------------------------------------------------
// read endpoints
// ---------------------------------------------------------------------------

func TestAzuraCastReadEndpoints(t *testing.T) {
	client, paths := azuraServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			writeJSON(t, w, &AzuraCastAPIStatus{Online: true, Station: "Grimnir"})
		case "/api/stations":
			writeJSON(t, w, []AzuraCastAPIStation{{ID: 1, Name: "Main", ShortName: "main"}})
		case "/api/station/7/files":
			writeJSON(t, w, []AzuraCastAPIMediaFile{{ID: 42, Title: "Blue Monday", Artist: "New Order", Length: 449.5}})
		case "/api/station/7/mounts":
			writeJSON(t, w, []AzuraCastAPIMount{{ID: 3, Name: "/radio.mp3", IsDefault: true}})
		case "/api/station/7/streamers":
			writeJSON(t, w, []AzuraCastAPIStreamer{{ID: 9, StreamerUsername: "dj_night", IsActive: true}})
		case "/api/station/7/profile":
			writeJSON(t, w, &AzuraCastAPIStationProfile{Name: "Main", Timezone: "Europe/Oslo"})
		case "/api/station/7/webhooks":
			writeJSON(t, w, []AzuraCastAPIWebhook{{ID: 2, Type: "generic", IsEnabled: true, Triggers: []string{"song_changed"}}})
		case "/api/station/7/podcasts":
			writeJSON(t, w, []AzuraCastAPIPodcast{{ID: 5, Title: "Nightshift"}})
		case "/api/station/7/podcast/5/episodes":
			writeJSON(t, w, []AzuraCastAPIPodcastEpisode{{ID: 11, Title: "Episode 1"}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	status, err := client.TestConnection(actx())
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if !status.Online || status.Station != "Grimnir" {
		t.Fatalf("status = %+v", status)
	}

	stations, err := client.GetStations(actx())
	if err != nil {
		t.Fatalf("stations: %v", err)
	}
	if len(stations) != 1 || stations[0].ShortName != "main" {
		t.Fatalf("stations = %+v", stations)
	}

	media, err := client.GetMedia(actx(), 7)
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	if len(media) != 1 || media[0].Title != "Blue Monday" || media[0].Length != 449.5 {
		t.Fatalf("media = %+v", media)
	}

	mounts, err := client.GetMounts(actx(), 7)
	if err != nil {
		t.Fatalf("mounts: %v", err)
	}
	if len(mounts) != 1 || !mounts[0].IsDefault {
		t.Fatalf("mounts = %+v", mounts)
	}

	streamers, err := client.GetStreamers(actx(), 7)
	if err != nil {
		t.Fatalf("streamers: %v", err)
	}
	if len(streamers) != 1 || streamers[0].StreamerUsername != "dj_night" {
		t.Fatalf("streamers = %+v", streamers)
	}

	profile, err := client.GetStationProfile(actx(), 7)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if profile.Timezone != "Europe/Oslo" {
		t.Fatalf("profile = %+v", profile)
	}

	webhooks, err := client.GetWebhooks(actx(), 7)
	if err != nil {
		t.Fatalf("webhooks: %v", err)
	}
	if len(webhooks) != 1 || webhooks[0].Triggers[0] != "song_changed" {
		t.Fatalf("webhooks = %+v", webhooks)
	}

	podcasts, err := client.GetPodcasts(actx(), 7)
	if err != nil {
		t.Fatalf("podcasts: %v", err)
	}
	if len(podcasts) != 1 || podcasts[0].Title != "Nightshift" {
		t.Fatalf("podcasts = %+v", podcasts)
	}

	episodes, err := client.GetPodcastEpisodes(actx(), 7, 5)
	if err != nil {
		t.Fatalf("episodes: %v", err)
	}
	if len(episodes) != 1 || episodes[0].Title != "Episode 1" {
		t.Fatalf("episodes = %+v", episodes)
	}

	if len(*paths) != 9 {
		t.Fatalf("requests = %d, want 9: %v", len(*paths), *paths)
	}
}

// AzuraCast has no standalone schedules endpoint; schedules live inside each
// playlist's schedule_items and must come back stamped with the playlist ID.
func TestAzuraCastGetSchedules_FlattensPlaylistScheduleItems(t *testing.T) {
	client, paths := azuraServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/station/7/playlists" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		writeJSON(t, w, []AzuraCastAPIPlaylist{
			{
				ID:   10,
				Name: "Morning",
				ScheduleItems: []AzuraCastAPISchedule{
					{ID: 100, StartTime: 21600, EndTime: 36000, Days: []int{1, 2, 3}},
					{ID: 101, StartTime: 36000, EndTime: 43200, Days: []int{6}},
				},
			},
			{ID: 11, Name: "Overnight"}, // no schedule items
			{
				ID:            12,
				Name:          "Weekend",
				ScheduleItems: []AzuraCastAPISchedule{{ID: 102, LoopOnce: true}},
			},
		})
	})

	schedules, err := client.GetSchedules(actx(), 7)
	if err != nil {
		t.Fatalf("schedules: %v", err)
	}
	if len(schedules) != 3 {
		t.Fatalf("schedules = %d, want 3", len(schedules))
	}
	// Only one HTTP call: schedules are derived, not fetched.
	if len(*paths) != 1 {
		t.Fatalf("requests = %v, want a single playlists call", *paths)
	}

	wantPlaylist := map[int]int{100: 10, 101: 10, 102: 12}
	for _, s := range schedules {
		if got := wantPlaylist[s.ID]; s.PlaylistID != got {
			t.Fatalf("schedule %d playlist = %d, want %d", s.ID, s.PlaylistID, got)
		}
	}
	if schedules[0].StartTime != 21600 || len(schedules[0].Days) != 3 {
		t.Fatalf("first schedule = %+v", schedules[0])
	}
}

func TestAzuraCastGetSchedules_PropagatesPlaylistFailure(t *testing.T) {
	client, _ := azuraServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := client.GetSchedules(actx(), 7); err == nil {
		t.Fatal("expected the playlist failure to surface")
	}
}

// ---------------------------------------------------------------------------
// downloads
// ---------------------------------------------------------------------------

func TestAzuraCastDownloadMedia(t *testing.T) {
	payload := "ID3fake-audio-bytes"
	client, paths := azuraServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/station/7/file/42/play" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, payload)
	})

	body, size, err := client.DownloadMedia(actx(), 7, 42)
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
		t.Fatalf("content length = %d, want %d", size, len(payload))
	}
	if (*paths)[0] != "/api/station/7/file/42/play" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

func TestAzuraCastDownloadMedia_ErrorStatus(t *testing.T) {
	client, _ := azuraServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "file missing")
	})

	_, _, err := client.DownloadMedia(actx(), 7, 42)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "file missing") {
		t.Fatalf("err = %v", err)
	}
}

// Artwork is optional everywhere it appears, so a missing or broken art
// endpoint returns empty rather than failing the whole import.
func TestAzuraCastArtworkDownloads(t *testing.T) {
	art := []byte{0xff, 0xd8, 0xff, 0xe0}

	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		wantData    bool
		wantMime    string
	}{
		{"png artwork", http.StatusOK, "image/png", true, "image/png"},
		{"missing content type defaults to jpeg", http.StatusOK, "", true, "image/jpeg"},
		{"not found", http.StatusNotFound, "", false, ""},
		{"no content", http.StatusNoContent, "", false, ""},
		{"server error is skipped silently", http.StatusInternalServerError, "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, _ *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				} else {
					// Stop net/http from sniffing a type onto the body.
					w.Header()["Content-Type"] = nil
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write(art)
				}
			}
			client, _ := azuraServer(t, handler)

			for _, call := range []struct {
				label string
				fn    func() ([]byte, string, error)
			}{
				{"media art", func() ([]byte, string, error) { return client.DownloadMediaArt(actx(), 7, 42) }},
				{"station art", func() ([]byte, string, error) { return client.DownloadStationArt(actx(), 7) }},
				{"streamer art", func() ([]byte, string, error) { return client.DownloadStreamerArt(actx(), 7, 9) }},
			} {
				data, mime, err := call.fn()
				if err != nil {
					t.Fatalf("%s: %v", call.label, err)
				}
				if tc.wantData {
					if string(data) != string(art) {
						t.Fatalf("%s data = %v", call.label, data)
					}
					if mime != tc.wantMime {
						t.Fatalf("%s mime = %q, want %q", call.label, mime, tc.wantMime)
					}
				} else if len(data) != 0 {
					t.Fatalf("%s returned %d bytes, want none", call.label, len(data))
				}
			}
		})
	}
}

func TestAzuraCastArtworkPaths(t *testing.T) {
	client, paths := azuraServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	if _, _, err := client.DownloadMediaArt(actx(), 7, 42); err != nil {
		t.Fatalf("media art: %v", err)
	}
	if _, _, err := client.DownloadStationArt(actx(), 7); err != nil {
		t.Fatalf("station art: %v", err)
	}
	if _, _, err := client.DownloadStreamerArt(actx(), 7, 9); err != nil {
		t.Fatalf("streamer art: %v", err)
	}

	want := []string{"/api/station/7/art/42", "/api/station/7/art", "/api/station/7/streamer/9/art"}
	for i, w := range want {
		if (*paths)[i] != w {
			t.Fatalf("request %d = %q, want %q", i, (*paths)[i], w)
		}
	}
}

// ---------------------------------------------------------------------------
// username/password login
// ---------------------------------------------------------------------------

func TestNewAzuraCastAPIClientWithCredentials_Token(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/login" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(t, w, map[string]any{"success": true, "token": "tok-123"})
	}))
	defer srv.Close()

	client, err := NewAzuraCastAPIClientWithCredentials(srv.URL, "admin", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if client.sessionToken != "tok-123" {
		t.Fatalf("session token = %q", client.sessionToken)
	}
	if gotBody["username"] != "admin" || gotBody["password"] != "hunter2" {
		t.Fatalf("login body = %v", gotBody)
	}
	if client.apiKey != "" {
		t.Fatalf("api key = %q, want empty for a credential login", client.apiKey)
	}
}

func TestNewAzuraCastAPIClientWithCredentials_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := NewAzuraCastAPIClientWithCredentials(srv.URL, "admin", "wrong")
	if err == nil {
		t.Fatal("expected an authentication error")
	}
	if !strings.Contains(err.Error(), "invalid username or password") {
		t.Fatalf("err = %v, want the credential message rather than a raw status", err)
	}
}

func TestNewAzuraCastAPIClientWithCredentials_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream down")
	}))
	defer srv.Close()

	_, err := NewAzuraCastAPIClientWithCredentials(srv.URL, "admin", "hunter2")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("err = %v", err)
	}
}

// Older AzuraCast builds answer the login with an HTML page and a PHP session
// cookie instead of a JSON token.
func TestNewAzuraCastAPIClientWithCredentials_SessionCookieFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "php-sess-9"})
		_, _ = io.WriteString(w, "<html>welcome</html>")
	}))
	defer srv.Close()

	client, err := NewAzuraCastAPIClientWithCredentials(srv.URL, "admin", "hunter2")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if client.sessionToken != "php-sess-9" {
		t.Fatalf("session token = %q, want the cookie value", client.sessionToken)
	}
}

// No token and no session cookie is a failed login, not a silent success.
func TestNewAzuraCastAPIClientWithCredentials_NoTokenNoCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>no session for you</html>")
	}))
	defer srv.Close()

	if _, err := NewAzuraCastAPIClientWithCredentials(srv.URL, "admin", "hunter2"); err == nil {
		t.Fatal("expected a failure when no session token is issued")
	}
}
