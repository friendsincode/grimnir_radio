package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

const testJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiI0ZjQ0YjhjZCJ9.ms8Gc7TTL45ZcCVx"

func TestRedactRequest_ScrubsTokenKeepsEverythingElse(t *testing.T) {
	target := "/api/v1/events?types=schedule_update,now_playing,health&token=" + testJWT
	req := httptest.NewRequest(http.MethodGet, target, nil)

	got := redactRequest(req)

	if strings.Contains(got.URL.RawQuery, testJWT) {
		t.Fatalf("token survived redaction: %q", got.URL.RawQuery)
	}
	if strings.Contains(got.RequestURI, testJWT) {
		t.Fatalf("token survived in RequestURI: %q", got.RequestURI)
	}
	if !strings.Contains(got.URL.RawQuery, "token="+redactedValue) {
		t.Fatalf("RawQuery=%q, want token=%s", got.URL.RawQuery, redactedValue)
	}
	if got.URL.Query().Get("types") != "schedule_update,now_playing,health" {
		t.Fatalf("types param lost: %q", got.URL.RawQuery)
	}
	if got.URL.Path != "/api/v1/events" {
		t.Fatalf("URL.Path=%q, want /api/v1/events", got.URL.Path)
	}
}

func TestRedactRequest_LeavesOriginalRequestUsable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?token="+testJWT, nil)

	_ = redactRequest(req)

	if got := req.URL.Query().Get("token"); got != testJWT {
		t.Fatalf("handler-visible token was mutated: %q", got)
	}
	if !strings.Contains(req.RequestURI, testJWT) {
		t.Fatalf("handler-visible RequestURI was mutated: %q", req.RequestURI)
	}
}

func TestRedactRequest_UntouchedWhenNothingSensitive(t *testing.T) {
	for _, target := range []string{
		"/api/v1/analytics/now-playing?station_id=b8eb301f",
		"/live/rlmradioxyz",
		"/dashboard?tokenizer=on&mytoken=keep",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if got := redactRequest(req); got != req {
			t.Fatalf("%s: request was copied, want the original untouched (RawQuery=%q)", target, got.URL.RawQuery)
		}
	}
}

func TestRedactRequest_AllSensitiveKeys(t *testing.T) {
	for _, key := range redactedQueryParams {
		req := httptest.NewRequest(http.MethodGet, "/x?"+key+"="+testJWT, nil)
		got := redactRequest(req)
		if strings.Contains(got.RequestURI, testJWT) {
			t.Fatalf("%s not redacted: %q", key, got.RequestURI)
		}
		if got.URL.Query().Get(key) != redactedValue {
			t.Fatalf("%s=%q, want %s", key, got.URL.Query().Get(key), redactedValue)
		}
	}
}

func TestRedactRequest_MalformedQueryDropsWholeQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.URL.RawQuery = "token=" + testJWT + "&%zz=broken"
	req.RequestURI = "/x?" + req.URL.RawQuery

	got := redactRequest(req)

	if strings.Contains(got.RequestURI, testJWT) {
		t.Fatalf("token survived a malformed query: %q", got.RequestURI)
	}
	if got.URL.RawQuery != redactedValue {
		t.Fatalf("RawQuery=%q, want the query dropped to %s", got.URL.RawQuery, redactedValue)
	}
}

// capturingLogger collects what chi's formatter prints.
type capturingLogger struct{ buf bytes.Buffer }

func (c *capturingLogger) Print(v ...interface{}) { fmt.Fprint(&c.buf, v...) }

func TestRequestLoggerMiddleware_NoTokenInLoggedLine(t *testing.T) {
	cap := &capturingLogger{}
	mw := middleware.RequestLogger(&redactingLogFormatter{
		inner: &middleware.DefaultLogFormatter{Logger: cap, NoColor: true},
	})

	var handlerSawToken string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerSawToken = r.URL.Query().Get("token")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?types=health&token="+testJWT, nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	logged := cap.buf.String()
	if strings.Contains(logged, testJWT) {
		t.Fatalf("JWT reached the log line: %s", logged)
	}
	if !strings.Contains(logged, "/api/v1/events") {
		t.Fatalf("path missing from log line: %s", logged)
	}
	if !strings.Contains(logged, redactedValue) {
		t.Fatalf("no redaction marker in log line: %s", logged)
	}
	if handlerSawToken != testJWT {
		t.Fatalf("handler saw token %q, want the real one", handlerSawToken)
	}
}
