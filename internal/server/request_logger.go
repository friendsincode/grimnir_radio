package server

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// redactedQueryParams name query parameters whose values must never reach a log.
// The events WebSocket authenticates with ?token=<JWT>, so an unscrubbed request
// line hands a usable admin token to anyone who can read stdout, the container
// log, or a reverse proxy's access log.
var redactedQueryParams = []string{"token", "access_token", "refresh_token", "api_key"}

const redactedValue = "REDACTED"

// requestLogger behaves like chi's middleware.Logger, except sensitive query
// values are replaced before the request line is formatted.
func requestLogger() func(http.Handler) http.Handler {
	return middleware.RequestLogger(&redactingLogFormatter{
		inner: &middleware.DefaultLogFormatter{
			Logger:  log.New(os.Stdout, "", log.LstdFlags),
			NoColor: runtime.GOOS == "windows",
		},
	})
}

// redactingLogFormatter wraps a chi LogFormatter so the underlying formatter
// only ever sees a scrubbed request.
type redactingLogFormatter struct {
	inner middleware.LogFormatter
}

func (f *redactingLogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	return f.inner.NewLogEntry(redactRequest(r))
}

// redactRequest returns a shallow copy of r whose URL and RequestURI carry
// redactedValue in place of sensitive query values. The original request is
// untouched, so handlers still read the real token. Requests without a
// sensitive parameter are returned as-is and log byte-for-byte as before.
func redactRequest(r *http.Request) *http.Request {
	if r.URL == nil || r.URL.RawQuery == "" || !hasRedactedParam(r.URL.RawQuery) {
		return r
	}

	redactedURL := *r.URL
	if q, err := url.ParseQuery(r.URL.RawQuery); err == nil {
		for _, key := range redactedQueryParams {
			if q.Has(key) {
				q.Set(key, redactedValue)
			}
		}
		redactedURL.RawQuery = q.Encode()
	} else {
		// The query does not parse, so it cannot be rewritten key by key. Drop
		// the whole thing rather than gamble on a token surviving in it.
		redactedURL.RawQuery = redactedValue
	}

	clone := *r
	clone.URL = &redactedURL
	clone.RequestURI = redactedURL.RequestURI()
	return &clone
}

// hasRedactedParam reports whether raw carries any sensitive parameter, matching
// only at a key boundary so "token" does not fire on an unrelated "csrf_token"
// that a later entry in redactedQueryParams already covers by name.
func hasRedactedParam(raw string) bool {
	for _, key := range redactedQueryParams {
		for i := strings.Index(raw, key+"="); i >= 0; {
			if i == 0 || raw[i-1] == '&' || raw[i-1] == '?' || raw[i-1] == ';' {
				return true
			}
			next := strings.Index(raw[i+1:], key+"=")
			if next < 0 {
				break
			}
			i += next + 1
		}
	}
	return false
}
