/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package telemetry

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// label sanitization
// ---------------------------------------------------------------------------

// Prometheus panics on invalid UTF-8 label values, so safeLabel is the guard
// between arbitrary ID3 tags and the metrics registry.
func TestSafeLabel(t *testing.T) {
	t.Run("short string passes through", func(t *testing.T) {
		if got := safeLabel("New Order", 60); got != "New Order" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		if got := safeLabel("", 60); got != "" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("truncates to exactly maxRunes", func(t *testing.T) {
		got := safeLabel(strings.Repeat("a", 200), 60)
		if utf8.RuneCountInString(got) != 60 {
			t.Fatalf("length = %d runes, want 60", utf8.RuneCountInString(got))
		}
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("got %q, want an ellipsis suffix", got)
		}
	})

	t.Run("exact length is untouched", func(t *testing.T) {
		in := strings.Repeat("b", 10)
		if got := safeLabel(in, 10); got != in {
			t.Fatalf("got %q, want the input unchanged", got)
		}
	})

	// Truncation counts runes, not bytes, so a multi-byte title cannot be cut
	// mid-sequence into invalid UTF-8.
	t.Run("multi-byte runes are not split", func(t *testing.T) {
		got := safeLabel(strings.Repeat("日", 40), 10)
		if !utf8.ValidString(got) {
			t.Fatalf("got invalid UTF-8: %q", got)
		}
		if utf8.RuneCountInString(got) != 10 {
			t.Fatalf("length = %d runes, want 10", utf8.RuneCountInString(got))
		}
	})

	t.Run("invalid UTF-8 is stripped", func(t *testing.T) {
		got := safeLabel(string([]byte{'o', 'k', 0xff, 0xfe}), 60)
		if !utf8.ValidString(got) {
			t.Fatalf("got invalid UTF-8: %q", got)
		}
		if got != "ok" {
			t.Fatalf("got %q, want %q", got, "ok")
		}
	})
}

// ---------------------------------------------------------------------------
// HTTP middleware
// ---------------------------------------------------------------------------

func TestMetricsMiddleware_RecordsStatusAndPath(t *testing.T) {
	const path = "/telemetry-test/plain"
	before := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("GET", path, "404"))

	h := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	after := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("GET", path, "404"))
	if after != before+1 {
		t.Fatalf("request counter = %v, want %v", after, before+1)
	}
	if got := testutil.ToFloat64(APIActiveConnections); got != 0 {
		t.Fatalf("active connections = %v after the request completed, want 0", got)
	}
}

// A handler that writes a body without calling WriteHeader is a 200; the wrapper
// has to infer that rather than reporting the zero value.
func TestMetricsMiddleware_DefaultsToStatus200(t *testing.T) {
	const path = "/telemetry-test/implicit"
	before := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("POST", path, "200"))

	h := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("hello")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))

	if rec.Body.String() != "hello" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if after := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("POST", path, "200")); after != before+1 {
		t.Fatalf("counter = %v, want %v", after, before+1)
	}
}

// Only the first WriteHeader wins, so a handler that double-writes doesn't
// corrupt the recorded status label.
func TestMetricsMiddleware_FirstStatusWins(t *testing.T) {
	const path = "/telemetry-test/double"
	before := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("GET", path, "503"))

	h := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	if after := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("GET", path, "503")); after != before+1 {
		t.Fatalf("counter for 503 = %v, want %v", after, before+1)
	}
}

// With a chi router in front, the endpoint label must be the route pattern.
// Otherwise every ID in a URL becomes its own time series.
func TestMetricsMiddleware_UsesChiRoutePattern(t *testing.T) {
	before := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("GET", "/stations/{id}", "200"))

	r := chi.NewRouter()
	r.Use(MetricsMiddleware)
	r.Get("/stations/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stations/abc-123", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	after := testutil.ToFloat64(APIRequestsTotal.WithLabelValues("GET", "/stations/{id}", "200"))
	if after != before+1 {
		t.Fatalf("counter = %v, want %v; the raw path was probably used as the label", after, before+1)
	}
}

// hijackableRecorder stands in for a real connection so the WebSocket upgrade
// path can be exercised without a live listener.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	client, server := net.Pipe()
	_ = server.Close()
	return client, bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), nil
}

// WebSocket routes go through this middleware; if the wrapper swallowed
// http.Hijacker, every upgrade under /api would fail.
func TestMetricsMiddleware_PassesThroughHijack(t *testing.T) {
	var hijackErr error
	h := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			hijackErr = errors.New("wrapped writer does not implement http.Hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			hijackErr = err
			return
		}
		_ = conn.Close()
	}))

	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/telemetry-test/ws", nil))

	if hijackErr != nil {
		t.Fatalf("hijack failed: %v", hijackErr)
	}
	if !rec.hijacked {
		t.Fatal("Hijack was not forwarded to the underlying ResponseWriter")
	}
}

func TestResponseWriter_HijackUnsupported(t *testing.T) {
	rw := &responseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusOK}
	conn, buf, err := rw.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("err = %v, want http.ErrNotSupported", err)
	}
	if conn != nil || buf != nil {
		t.Fatal("expected nil conn and buffer when hijacking is unsupported")
	}
}

func TestTracingMiddleware(t *testing.T) {
	t.Run("plain path", func(t *testing.T) {
		called := false
		h := TracingMiddleware("grimnir-test")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusTeapot)
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/telemetry-test/trace", nil))

		if !called {
			t.Fatal("next handler was not invoked")
		}
		if rec.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want 418", rec.Code)
		}
	})

	t.Run("chi route pattern", func(t *testing.T) {
		r := chi.NewRouter()
		r.Use(TracingMiddleware("grimnir-test"))
		r.Get("/mounts/{id}", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mounts/m1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestHandler_ServesMetrics(t *testing.T) {
	StationsTotal.Set(3)

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "grimnir_stations_total") {
		t.Fatal("metrics output missing grimnir_stations_total")
	}
}

// ---------------------------------------------------------------------------
// DB-driven gauges
// ---------------------------------------------------------------------------

func newMetricsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.MediaItem{},
		&models.MountPlayoutState{},
		&models.PlayHistory{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestUpdateStationMetrics(t *testing.T) {
	db := newMetricsDB(t)

	db.Create(&models.Station{ID: "st1", Name: "One"})
	db.Create(&models.Station{ID: "st2", Name: "Two"})
	db.Create(&models.MountPlayoutState{MountID: "m1", StationID: "st1", StartedAt: time.Now()})

	// 2 minutes + 3 minutes = 5 minutes = 300000ms = 0.08333 hours.
	db.Create(&models.MediaItem{ID: "a", StationID: "st1", Path: "a.mp3", Duration: 2 * time.Minute})
	db.Create(&models.MediaItem{ID: "b", StationID: "st1", Path: "b.mp3", Duration: 3 * time.Minute})
	// Excluded: failed analysis and zero duration are not part of the library size.
	db.Create(&models.MediaItem{ID: "c", StationID: "st1", Path: "c.mp3", Duration: time.Minute, AnalysisState: models.AnalysisFailed})
	db.Create(&models.MediaItem{ID: "d", StationID: "st1", Path: "d.mp3", Duration: 0})

	db.Create(&models.PlayHistory{ID: "p1", StationID: "st1", StartedAt: time.Now().Add(-time.Hour)})
	db.Create(&models.PlayHistory{ID: "p2", StationID: "st1", StartedAt: time.Now().Add(-2 * time.Hour)})
	// Older than the 24h window.
	db.Create(&models.PlayHistory{ID: "p3", StationID: "st1", StartedAt: time.Now().Add(-48 * time.Hour)})

	UpdateStationMetrics(db)

	if got := testutil.ToFloat64(StationsTotal); got != 2 {
		t.Fatalf("stations total = %v, want 2", got)
	}
	if got := testutil.ToFloat64(StationsActive); got != 1 {
		t.Fatalf("stations active = %v, want 1", got)
	}
	if got := testutil.ToFloat64(MediaItemsTotal.WithLabelValues("st1")); got != 2 {
		t.Fatalf("media items for st1 = %v, want 2 (failed and zero-duration excluded)", got)
	}
	// 5 minutes of media. Duration is a time.Duration stored as nanoseconds, so
	// the SQL conversion has to divide by nanoseconds-per-hour.
	hours := testutil.ToFloat64(MediaLibraryDurationHours.WithLabelValues("st1"))
	if hours < 0.0833 || hours > 0.0834 {
		t.Fatalf("library hours = %v, want ~0.08333", hours)
	}
	if got := testutil.ToFloat64(PlayHistoryTotal.WithLabelValues("st1")); got != 2 {
		t.Fatalf("24h plays = %v, want 2", got)
	}
	if got := testutil.ToFloat64(UptimeSeconds); got <= 0 {
		t.Fatalf("uptime = %v, want a positive value", got)
	}
}

// The deferred recover exists so a bad row can't take down the ticker loop that
// calls this every cycle. Pointing it at a DB with no tables must not panic.
func TestUpdateStationMetrics_SurvivesBrokenSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	UpdateStationMetrics(db) // no tables exist; must return without panicking
}

func TestUpdateListenerMetrics(t *testing.T) {
	UpdateListenerMetrics(map[string]int{"st1": 42, "st2": 7})

	if got := testutil.ToFloat64(ListenersCurrentTotal.WithLabelValues("st1")); got != 42 {
		t.Fatalf("st1 listeners = %v, want 42", got)
	}
	if got := testutil.ToFloat64(ListenersCurrentTotal.WithLabelValues("st2")); got != 7 {
		t.Fatalf("st2 listeners = %v, want 7", got)
	}

	// Each refresh resets first, so a station that drops out of the map does not
	// keep reporting its last listener count forever.
	UpdateListenerMetrics(map[string]int{"st1": 5})
	if got := testutil.ToFloat64(ListenersCurrentTotal.WithLabelValues("st1")); got != 5 {
		t.Fatalf("st1 listeners = %v, want 5", got)
	}
	if count := testutil.CollectAndCount(ListenersCurrentTotal); count != 1 {
		t.Fatalf("listener series = %d, want 1 after the reset", count)
	}
}

// ---------------------------------------------------------------------------
// tracing
// ---------------------------------------------------------------------------

func TestInitTracer_Disabled(t *testing.T) {
	tp, err := InitTracer(context.Background(), TracerConfig{
		ServiceName: "grimnir-test",
		Enabled:     false,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if tp == nil {
		t.Fatal("nil provider")
	}

	// Shutdown on a disabled provider is a no-op, not a nil dereference.
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestStartSpanAndAttributes(t *testing.T) {
	ctx, span := StartSpan(context.Background(), "grimnir-test", "unit-span")
	if span == nil {
		t.Fatal("nil span")
	}
	if ctx == nil {
		t.Fatal("nil context")
	}

	AddSpanAttributes(span, map[string]any{
		"station_id": "st1",
		"count":      3,
		"bytes":      int64(4096),
		"ratio":      0.75,
		"live":       true,
		"ignored":    []string{"unsupported types are skipped, not panicked on"},
	})
	RecordError(span, errors.New("boom"))
	RecordError(span, nil) // nil errors are ignored
	span.End()

	if tr := Tracer("grimnir-test"); tr == nil {
		t.Fatal("nil tracer")
	}
	var _ trace.Span = span
}
