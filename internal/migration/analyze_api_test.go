/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// azuraAnalyzeServer answers the endpoints AnalyzeDetailed walks for a single
// station, and returns Options pointed at it.
func azuraAnalyzeServer(t *testing.T, handler http.HandlerFunc) Options {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return Options{AzuraCastAPIURL: srv.URL, AzuraCastAPIKey: "test-key"}
}

func ltAnalyzeServer(t *testing.T, handler http.HandlerFunc) Options {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return Options{LibreTimeAPIURL: srv.URL, LibreTimeAPIKey: "lt-key"}
}

// deadServerOptions returns options pointing at a closed port.
func deadServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

func newAzuraImporter() *AzuraCastImporter {
	return NewAzuraCastImporter(nil, nil, zerolog.Nop())
}

func newLTImporter() *LibreTimeImporter {
	return NewLibreTimeImporter(nil, nil, zerolog.Nop())
}

// ---------------------------------------------------------------------------
// mode detection and small helpers
// ---------------------------------------------------------------------------

func TestIsAPIMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want bool
	}{
		{"api key", Options{AzuraCastAPIURL: "https://a", AzuraCastAPIKey: "k"}, true},
		{"credentials", Options{AzuraCastAPIURL: "https://a", AzuraCastUsername: "u", AzuraCastPassword: "p"}, true},
		{"url alone is not enough", Options{AzuraCastAPIURL: "https://a"}, false},
		{"key without url", Options{AzuraCastAPIKey: "k"}, false},
		{"username without password", Options{AzuraCastAPIURL: "https://a", AzuraCastUsername: "u"}, false},
		{"backup path only", Options{AzuraCastBackupPath: "/tmp/backup.tar.gz"}, false},
	} {
		if got := isAPIMode(tc.opts); got != tc.want {
			t.Fatalf("%s: isAPIMode = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsLibreTimeAPIMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want bool
	}{
		{"url and key", Options{LibreTimeAPIURL: "https://lt", LibreTimeAPIKey: "k"}, true},
		{"url alone", Options{LibreTimeAPIURL: "https://lt"}, false},
		{"key alone", Options{LibreTimeAPIKey: "k"}, false},
		{"db mode", Options{LibreTimeDBHost: "db", LibreTimeDBName: "libretime", LibreTimeDBUser: "lt"}, false},
	} {
		if got := isLibreTimeAPIMode(tc.opts); got != tc.want {
			t.Fatalf("%s: isLibreTimeAPIMode = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCreateAPIClient_PrefersAPIKey(t *testing.T) {
	// A key never triggers a login round trip, so a dead URL is fine here.
	client, err := createAPIClient(Options{AzuraCastAPIURL: "https://radio.example.com", AzuraCastAPIKey: "k"})
	if err != nil {
		t.Fatalf("key mode: %v", err)
	}
	if client.apiKey != "k" {
		t.Fatalf("api key = %q", client.apiKey)
	}
	if client.sessionToken != "" {
		t.Fatal("key mode should not perform a credential login")
	}
}

func TestCreateAPIClient_CredentialsLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/login" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{"success": true, "token": "tok-77"})
	}))
	defer srv.Close()

	client, err := createAPIClient(Options{AzuraCastAPIURL: srv.URL, AzuraCastUsername: "admin", AzuraCastPassword: "pw"})
	if err != nil {
		t.Fatalf("credential mode: %v", err)
	}
	if client.sessionToken != "tok-77" {
		t.Fatalf("session token = %q", client.sessionToken)
	}
}

func TestCalculateETA(t *testing.T) {
	if got := calculateETA(time.Now(), 0, 100); got != "calculating..." {
		t.Fatalf("no progress = %q", got)
	}
	if got := calculateETA(time.Now(), 10, 0); got != "calculating..." {
		t.Fatalf("no total = %q", got)
	}

	// 10 items in 10s is 1s each; 20 remaining is 20s.
	start := time.Now().Add(-10 * time.Second)
	if got := calculateETA(start, 10, 30); !strings.HasSuffix(got, "s remaining") || strings.Contains(got, "m ") {
		t.Fatalf("sub-minute ETA = %q, want a seconds-only estimate", got)
	}

	// 10 items in 100s is 10s each; 50 remaining is 500s, just over 8 minutes.
	start = time.Now().Add(-100 * time.Second)
	got := calculateETA(start, 10, 60)
	if !strings.Contains(got, "m ") || !strings.HasSuffix(got, "s remaining") {
		t.Fatalf("minutes ETA = %q, want a minutes and seconds estimate", got)
	}

	// 1 item in an hour, 5 remaining, is a 5 hour estimate.
	start = time.Now().Add(-time.Hour)
	got = calculateETA(start, 1, 6)
	if !strings.Contains(got, "h ") || !strings.HasSuffix(got, "m remaining") {
		t.Fatalf("hours ETA = %q, want an hours and minutes estimate", got)
	}
}

// ---------------------------------------------------------------------------
// AzuraCast Validate
// ---------------------------------------------------------------------------

func TestAzuraCastValidate_RequiresOneImportMethod(t *testing.T) {
	err := newAzuraImporter().Validate(actx(), Options{})
	if err == nil {
		t.Fatal("empty options accepted")
	}
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("err = %T, want ValidationErrors", err)
	}
	if !strings.Contains(verrs[0].Message, "either backup path") {
		t.Fatalf("message = %q", verrs[0].Message)
	}
}

// Supplying both a backup and API credentials is ambiguous, and silently
// picking one would import from a source the operator did not intend.
func TestAzuraCastValidate_RejectsBothMethods(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.tar.gz")
	if err := os.WriteFile(backup, []byte("x"), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	err := newAzuraImporter().Validate(actx(), Options{
		AzuraCastBackupPath: backup,
		AzuraCastAPIURL:     "https://radio.example.com",
		AzuraCastAPIKey:     "k",
	})
	if err == nil {
		t.Fatal("both methods accepted")
	}
	if !strings.Contains(err.Error(), "cannot specify both") {
		t.Fatalf("err = %v", err)
	}
}

func TestAzuraCastValidate_BackupFileChecks(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		err := newAzuraImporter().Validate(actx(), Options{AzuraCastBackupPath: filepath.Join(dir, "absent.tar.gz")})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("wrong extension", func(t *testing.T) {
		zipPath := filepath.Join(dir, "backup.zip")
		if err := os.WriteFile(zipPath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		err := newAzuraImporter().Validate(actx(), Options{AzuraCastBackupPath: zipPath})
		if err == nil {
			t.Fatal("non-tar.gz accepted")
		}
		var verrs ValidationErrors
		if !errors.As(err, &verrs) {
			t.Fatalf("err = %T", err)
		}
		if !strings.Contains(verrs[0].Message, ".tar.gz") {
			t.Fatalf("message = %q", verrs[0].Message)
		}
	})

	t.Run("valid archive", func(t *testing.T) {
		good := filepath.Join(dir, "good.tar.gz")
		if err := os.WriteFile(good, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := newAzuraImporter().Validate(actx(), Options{AzuraCastBackupPath: good}); err != nil {
			t.Fatalf("valid backup rejected: %v", err)
		}
	})
}

func TestAzuraCastValidate_APIConnection(t *testing.T) {
	t.Run("reachable", func(t *testing.T) {
		opts := azuraAnalyzeServer(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, &AzuraCastAPIStatus{Online: true})
		})
		if err := newAzuraImporter().Validate(actx(), opts); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		err := newAzuraImporter().Validate(actx(), Options{AzuraCastAPIURL: deadServerURL(t), AzuraCastAPIKey: "k"})
		if err == nil {
			t.Fatal("dead server accepted")
		}
		if !strings.Contains(err.Error(), "API connection failed") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("bad credentials", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := newAzuraImporter().Validate(actx(), Options{
			AzuraCastAPIURL:   srv.URL,
			AzuraCastUsername: "admin",
			AzuraCastPassword: "wrong",
		})
		if err == nil {
			t.Fatal("bad credentials accepted")
		}
		if !strings.Contains(err.Error(), "API authentication failed") {
			t.Fatalf("err = %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// AzuraCast AnalyzeDetailed
// ---------------------------------------------------------------------------

func TestAzuraCastAnalyzeDetailed(t *testing.T) {
	opts := azuraAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/stations":
			writeJSON(t, w, []AzuraCastAPIStation{
				{ID: 1, Name: "Main", Description: "Primary"},
				{ID: 2, Name: "Side", Description: "Secondary"},
			})
		case "/api/station/1/files":
			writeJSON(t, w, []AzuraCastAPIMediaFile{
				{ID: 10, Title: "A", Size: 5_000_000},
				{ID: 11, Title: "B", Size: 3_000_000},
			})
		case "/api/station/2/files":
			// No Size, so storage is estimated from Length at 192kbps.
			writeJSON(t, w, []AzuraCastAPIMediaFile{{ID: 20, Title: "C", Length: 100}})
		case "/api/station/1/playlists":
			writeJSON(t, w, []AzuraCastAPIPlaylist{
				{ID: 100, Name: "Morning", Type: "default", Source: "songs", NumSongs: 42,
					ScheduleItems: []AzuraCastAPISchedule{{ID: 1}, {ID: 2}}},
			})
		case "/api/station/2/playlists":
			writeJSON(t, w, []AzuraCastAPIPlaylist{{ID: 200, Name: "Night", NumSongs: 7}})
		case "/api/station/1/mounts":
			writeJSON(t, w, []AzuraCastAPIMount{{ID: 3, Name: "/radio.mp3", AutodjFormat: "mp3", AutodjBitrate: 192}})
		case "/api/station/2/mounts":
			writeJSON(t, w, []AzuraCastAPIMount{})
		case "/api/station/1/streamers":
			writeJSON(t, w, []AzuraCastAPIStreamer{{ID: 9, StreamerUsername: "dj_night", DisplayName: "Night DJ"}})
		case "/api/station/2/streamers":
			writeJSON(t, w, []AzuraCastAPIStreamer{})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	report, err := newAzuraImporter().AnalyzeDetailed(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if report.TotalStations != 2 {
		t.Fatalf("stations = %d, want 2", report.TotalStations)
	}
	if report.TotalMedia != 3 {
		t.Fatalf("media = %d, want 3", report.TotalMedia)
	}
	if report.TotalPlaylists != 2 {
		t.Fatalf("playlists = %d, want 2", report.TotalPlaylists)
	}
	// Schedules come from the first station's two schedule_items.
	if report.TotalSchedules != 2 {
		t.Fatalf("schedules = %d, want 2", report.TotalSchedules)
	}
	if report.TotalStreamers != 1 {
		t.Fatalf("streamers = %d, want 1", report.TotalStreamers)
	}

	// 8 MB of real sizes, plus 100s at 192kbps = 2,400,000 bytes estimated.
	const wantBytes = 5_000_000 + 3_000_000 + 100*192*1000/8
	if report.EstimatedStorageBytes != wantBytes {
		t.Fatalf("storage = %d, want %d", report.EstimatedStorageBytes, wantBytes)
	}
	if !strings.HasSuffix(report.EstimatedStorageHuman, "MB") {
		t.Fatalf("human storage = %q, want MB", report.EstimatedStorageHuman)
	}

	if len(report.Stations) != 2 {
		t.Fatalf("station analyses = %d", len(report.Stations))
	}
	main := report.Stations[0]
	if main.MediaCount != 2 || main.StorageBytes != 8_000_000 {
		t.Fatalf("main station = %+v", main)
	}
	if len(main.Playlists) != 1 || main.Playlists[0].ItemCount != 42 {
		t.Fatalf("main playlists = %+v", main.Playlists)
	}
	if len(main.Mounts) != 1 || main.Mounts[0].Bitrate != 192 || main.Mounts[0].Format != "mp3" {
		t.Fatalf("main mounts = %+v", main.Mounts)
	}
	if len(main.Streamers) != 1 || main.Streamers[0].Username != "dj_night" {
		t.Fatalf("main streamers = %+v", main.Streamers)
	}

	// Multiple stations get the deduplication caveat.
	var sawDedup bool
	for _, warn := range report.Warnings {
		if strings.Contains(warn, "deduplication") {
			sawDedup = true
		}
	}
	if !sawDedup {
		t.Fatalf("warnings = %v, want the multi-station deduplication note", report.Warnings)
	}
}

// A station whose media endpoint 403s must produce a warning naming the
// station, not abort the whole analysis.
func TestAzuraCastAnalyzeDetailed_PartialFailuresBecomeWarnings(t *testing.T) {
	opts := azuraAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/stations":
			writeJSON(t, w, []AzuraCastAPIStation{{ID: 1, Name: "Main"}})
		case "/api/station/1/files":
			w.WriteHeader(http.StatusForbidden)
		default:
			// Playlists, mounts and streamers all fail too.
			w.WriteHeader(http.StatusForbidden)
		}
	})

	report, err := newAzuraImporter().AnalyzeDetailed(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if report.TotalStations != 1 {
		t.Fatalf("stations = %d", report.TotalStations)
	}
	if report.TotalMedia != 0 {
		t.Fatalf("media = %d, want 0", report.TotalMedia)
	}

	var sawMediaWarning bool
	for _, warn := range report.Warnings {
		if strings.Contains(warn, "Could not fetch media for station Main") {
			sawMediaWarning = true
		}
	}
	if !sawMediaWarning {
		t.Fatalf("warnings = %v, want one naming the station", report.Warnings)
	}
}

// An API key scoped to nothing returns an empty station list; the report has
// to say so rather than reporting a clean, empty migration.
func TestAzuraCastAnalyzeDetailed_NoStations(t *testing.T) {
	opts := azuraAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stations" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		writeJSON(t, w, []AzuraCastAPIStation{})
	})

	report, err := newAzuraImporter().AnalyzeDetailed(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	joined := strings.Join(report.Warnings, "|")
	if !strings.Contains(joined, "No stations found") {
		t.Fatalf("warnings = %v, want the permissions hint", report.Warnings)
	}
	if !strings.Contains(joined, "No media files found") {
		t.Fatalf("warnings = %v, want the empty-media warning", report.Warnings)
	}
}

func TestAzuraCastAnalyzeDetailed_StationsFetchFails(t *testing.T) {
	opts := azuraAnalyzeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := newAzuraImporter().AnalyzeDetailed(actx(), opts); err == nil {
		t.Fatal("expected an error when stations cannot be listed")
	}
}

// Analyze in API mode reduces the detailed report to the summary Result the
// migration UI renders.
func TestAzuraCastAnalyze_MapsReportToResult(t *testing.T) {
	opts := azuraAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/stations":
			writeJSON(t, w, []AzuraCastAPIStation{{ID: 1, Name: "Main"}})
		case "/api/station/1/files":
			writeJSON(t, w, []AzuraCastAPIMediaFile{{ID: 10, Size: 100}, {ID: 11, Size: 200}})
		case "/api/station/1/playlists":
			writeJSON(t, w, []AzuraCastAPIPlaylist{{ID: 100, ScheduleItems: []AzuraCastAPISchedule{{ID: 1}}}})
		case "/api/station/1/mounts":
			writeJSON(t, w, []AzuraCastAPIMount{})
		case "/api/station/1/streamers":
			writeJSON(t, w, []AzuraCastAPIStreamer{{ID: 9}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	result, err := newAzuraImporter().Analyze(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if result.StationsCreated != 1 {
		t.Fatalf("stations = %d", result.StationsCreated)
	}
	if result.MediaItemsImported != 2 {
		t.Fatalf("media = %d, want 2", result.MediaItemsImported)
	}
	if result.PlaylistsCreated != 1 {
		t.Fatalf("playlists = %d", result.PlaylistsCreated)
	}
	if result.SchedulesCreated != 1 {
		t.Fatalf("schedules = %d", result.SchedulesCreated)
	}
	if result.UsersCreated != 1 {
		t.Fatalf("streamers mapped to users = %d, want 1", result.UsersCreated)
	}
	if result.Skipped == nil || result.Mappings == nil {
		t.Fatal("Skipped and Mappings must be initialized, not nil")
	}
}

// ---------------------------------------------------------------------------
// LibreTime Validate
// ---------------------------------------------------------------------------

// Database mode reports every missing field at once so the operator fixes the
// form in one pass.
func TestLibreTimeValidate_DatabaseModeMissingFields(t *testing.T) {
	err := newLTImporter().Validate(actx(), Options{})
	if err == nil {
		t.Fatal("empty options accepted")
	}
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("err = %T, want ValidationErrors", err)
	}
	if len(verrs) != 3 {
		t.Fatalf("errors = %d, want one each for host, name and user: %v", len(verrs), verrs)
	}

	fields := map[string]bool{}
	for _, e := range verrs {
		fields[e.Field] = true
	}
	for _, want := range []string{"libretime_db_host", "libretime_db_name", "libretime_db_user"} {
		if !fields[want] {
			t.Fatalf("missing %s in %v", want, verrs)
		}
	}
}

func TestLibreTimeValidate_APIMode(t *testing.T) {
	t.Run("reachable", func(t *testing.T) {
		opts := ltAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/version":
				writeJSON(t, w, map[string]string{"api_version": "4.2.0"})
			case "/api/v2/files":
				writeJSON(t, w, []LTFile{})
			default:
				t.Errorf("unexpected path %q", r.URL.Path)
			}
		})
		if err := newLTImporter().Validate(actx(), opts); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		err := newLTImporter().Validate(actx(), Options{
			LibreTimeAPIURL: deadServerURL(t),
			LibreTimeAPIKey: "k",
		})
		if err == nil {
			t.Fatal("dead server accepted")
		}
		var verrs ValidationErrors
		if !errors.As(err, &verrs) {
			t.Fatalf("err = %T", err)
		}
		if verrs[0].Field != "libretime_api" {
			t.Fatalf("field = %q", verrs[0].Field)
		}
	})
}

// ---------------------------------------------------------------------------
// LibreTime AnalyzeDetailed
// ---------------------------------------------------------------------------

func TestLibreTimeAnalyzeDetailed(t *testing.T) {
	opts := ltAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{
				{ID: 1, Title: "A", Artist: "X", Size: 4_000_000, FileExists: true},
				{ID: 2, Title: "B", Artist: "Y", Size: 2_000_000, FileExists: true},
				// Hidden and missing files count toward the total but are
				// excluded from the storage estimate and the file list.
				{ID: 3, Title: "Hidden", Size: 9_000_000, FileExists: true, Hidden: true},
				{ID: 4, Title: "Gone", Size: 9_000_000, FileExists: false},
			})
		case "/api/v2/playlists":
			writeJSON(t, w, []LTPlaylist{
				{ID: 10, Name: "Morning", Length: "01:00:00"},
				{ID: 11, Name: "Empty"},
			})
		case "/api/v2/playlist-contents":
			writeJSON(t, w, []LTPlaylistContent{
				{ID: 1, PlaylistID: intPtr(10), FileID: intPtr(1)},
				{ID: 2, PlaylistID: intPtr(10), FileID: intPtr(2)},
				{ID: 3, PlaylistID: intPtr(10), BlockID: intPtr(5)},
				{ID: 4, PlaylistID: intPtr(10), StreamID: intPtr(7)},
			})
		case "/api/v2/shows":
			writeJSON(t, w, []LTShow{
				{ID: 20, Name: "Nightshift", Description: "Late", Genre: "Electronic"},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	report, err := newLTImporter().AnalyzeDetailed(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if report.TotalFiles != 4 {
		t.Fatalf("total files = %d, want all 4 counted", report.TotalFiles)
	}
	if len(report.Files) != 2 {
		t.Fatalf("listed files = %d, want the 2 visible and present ones", len(report.Files))
	}
	if report.EstimatedStorageBytes != 6_000_000 {
		t.Fatalf("storage = %d, want 6000000 (hidden and missing excluded)", report.EstimatedStorageBytes)
	}
	if report.EstimatedStorageHuman == "" {
		t.Fatal("human storage empty")
	}

	if report.TotalPlaylists != 2 {
		t.Fatalf("playlists = %d, want 2", report.TotalPlaylists)
	}
	morning := report.Playlists[0]
	if morning.ItemCount != 4 {
		t.Fatalf("morning items = %d, want 4", morning.ItemCount)
	}
	if morning.FileCount != 2 || morning.BlockCount != 1 || morning.StreamCount != 1 {
		t.Fatalf("morning breakdown = %+v, want 2 files, 1 block, 1 stream", morning)
	}
	if report.Playlists[1].ItemCount != 0 {
		t.Fatalf("empty playlist = %+v", report.Playlists[1])
	}

	if report.TotalShows != 1 || report.Shows[0].Genre != "Electronic" {
		t.Fatalf("shows = %+v", report.Shows)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
}

func TestLibreTimeAnalyzeDetailed_EmptyLibraryWarns(t *testing.T) {
	opts := ltAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{})
		case "/api/v2/playlists":
			writeJSON(t, w, []LTPlaylist{})
		case "/api/v2/shows":
			writeJSON(t, w, []LTShow{})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	report, err := newLTImporter().AnalyzeDetailed(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !strings.Contains(strings.Join(report.Warnings, "|"), "No media files found") {
		t.Fatalf("warnings = %v", report.Warnings)
	}
}

// Unlike AzuraCast, a failed file listing here is a warning rather than a hard
// error, so the operator still sees the playlist and show counts.
func TestLibreTimeAnalyzeDetailed_FileFetchFailureIsAWarning(t *testing.T) {
	opts := ltAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/files":
			w.WriteHeader(http.StatusForbidden)
		case "/api/v2/playlists":
			writeJSON(t, w, []LTPlaylist{{ID: 10, Name: "Morning"}})
		case "/api/v2/playlist-contents":
			writeJSON(t, w, []LTPlaylistContent{})
		case "/api/v2/shows":
			writeJSON(t, w, []LTShow{{ID: 20, Name: "Nightshift"}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	report, err := newLTImporter().AnalyzeDetailed(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if report.TotalPlaylists != 1 || report.TotalShows != 1 {
		t.Fatalf("report = %+v, want playlist and show counts to survive", report)
	}
	if !strings.Contains(strings.Join(report.Warnings, "|"), "Could not fetch files") {
		t.Fatalf("warnings = %v", report.Warnings)
	}
}

func TestLibreTimeAnalyze_MapsReportToResult(t *testing.T) {
	opts := ltAnalyzeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/files":
			writeJSON(t, w, []LTFile{{ID: 1, FileExists: true, Size: 100}})
		case "/api/v2/playlists":
			writeJSON(t, w, []LTPlaylist{{ID: 10}, {ID: 11}})
		case "/api/v2/playlist-contents":
			writeJSON(t, w, []LTPlaylistContent{})
		case "/api/v2/shows":
			writeJSON(t, w, []LTShow{{ID: 20}, {ID: 21}, {ID: 22}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	result, err := newLTImporter().Analyze(actx(), opts)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// LibreTime is single-station by design.
	if result.StationsCreated != 1 {
		t.Fatalf("stations = %d, want 1", result.StationsCreated)
	}
	if result.MediaItemsImported != 1 {
		t.Fatalf("media = %d", result.MediaItemsImported)
	}
	if result.PlaylistsCreated != 2 {
		t.Fatalf("playlists = %d", result.PlaylistsCreated)
	}
	if result.SchedulesCreated != 3 {
		t.Fatalf("shows mapped to schedules = %d, want 3", result.SchedulesCreated)
	}
	if result.UsersCreated != 0 {
		t.Fatalf("users = %d, want 0", result.UsersCreated)
	}
}
