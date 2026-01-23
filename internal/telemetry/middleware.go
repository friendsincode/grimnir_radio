package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// MetricsMiddleware tracks HTTP request metrics.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track active connections
		APIActiveConnections.Inc()
		defer APIActiveConnections.Dec()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
			written:        false,
		}

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Record metrics
		duration := time.Since(start).Seconds()

		// Get route pattern if available
		endpoint := r.URL.Path
		if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
			endpoint = routeCtx.RoutePattern()
		}

		statusCode := strconv.Itoa(wrapped.statusCode)

		APIRequestDuration.WithLabelValues(
			r.Method,
			endpoint,
			statusCode,
		).Observe(duration)

		APIRequestsTotal.WithLabelValues(
			r.Method,
			endpoint,
			statusCode,
		).Inc()
	})
}
