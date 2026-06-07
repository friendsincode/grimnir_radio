/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// listenerEventRequest is the JSON contract the JS player posts
// (Track B-3, docs/superpowers/plans/2026-06-06-custom-js-player.md, Chunk 5).
// No listener ID, no client IP in the body; the server reads the socket IP
// for rate limiting only & never stores it.
type listenerEventRequest struct {
	EventType   string `json:"event_type"`
	StationID   string `json:"station_id"`
	StreamLabel string `json:"stream_label"`
	DurationMs  *int   `json:"duration_ms,omitempty"`
}

// validListenerEventTypes is the closed set the handler accepts. Any other
// string returns 400; this stops a noisy client from inflating cardinality.
var validListenerEventTypes = map[string]struct{}{
	"reconnect": {},
	"degrade":   {},
	"upgrade":   {},
	"exhausted": {},
	"play":      {},
	"stop":      {},
}

// listenerEventRateLimiter is a per-IP token bucket. 10 events/min/IP, refilled
// continuously. The store grows as new IPs appear; a sweep removes entries
// older than 10 minutes so a long-lived process doesn't leak memory.
//
// Intentionally process-local: the JS player retries on 429 with backoff, and
// distributed rate limiting belongs at the LB / edge if it ever matters.
type listenerEventRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	// capacity is the max tokens in a bucket; refillPerSec is tokens/sec.
	// 10 events/min => capacity=10, refill=10/60.
	capacity     float64
	refillPerSec float64
	// lastSweep avoids walking the whole map on every request.
	lastSweep time.Time
}

type ipBucket struct {
	tokens   float64
	lastSeen time.Time
}

func newListenerEventRateLimiter() *listenerEventRateLimiter {
	return &listenerEventRateLimiter{
		buckets:      make(map[string]*ipBucket),
		capacity:     10,
		refillPerSec: 10.0 / 60.0,
		// lastSweep zero-value forces the first sweep on the first allow()
		// call where elapsed > 1 minute; safe for tests that inject a
		// fixed time, & for production where real wall clock is always
		// well past the zero value.
	}
}

// allow returns true if the request from ip is permitted; false if the bucket
// is empty. now is injected for testability.
func (l *listenerEventRateLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[ip]
	if !ok {
		b = &ipBucket{tokens: l.capacity, lastSeen: now}
		l.buckets[ip] = b
	} else {
		elapsed := now.Sub(b.lastSeen).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.refillPerSec
			if b.tokens > l.capacity {
				b.tokens = l.capacity
			}
		}
		b.lastSeen = now
	}

	if now.Sub(l.lastSweep) > time.Minute {
		l.sweepLocked(now)
		l.lastSweep = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepLocked drops buckets unseen for >10 minutes. Caller holds the mutex.
func (l *listenerEventRateLimiter) sweepLocked(now time.Time) {
	cutoff := now.Add(-10 * time.Minute)
	for ip, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, ip)
		}
	}
}

// clientIP extracts the socket peer IP. r.RemoteAddr is "host:port" for TCP;
// strip the port so the rate-limit key is just the host. Trust no proxy
// headers: this is for local rate limiting only, not auth.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleListenerEventCreate accepts an anonymous telemetry event from the
// browser JS player & writes one listener_events row. Returns 204 on success.
//
// Rate limit: 10 events/min/IP via process-local token bucket; 429 if exceeded.
// The IP is logged at debug level for ops; never stored.
func (a *API) handleListenerEventCreate(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	if a.listenerEventLimiter != nil && !a.listenerEventLimiter.allow(ip, time.Now()) {
		a.logger.Debug().Str("ip", ip).Msg("listener-events: rate limit exceeded")
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var req listenerEventRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if _, ok := validListenerEventTypes[req.EventType]; !ok {
		writeError(w, http.StatusBadRequest, "invalid_event_type")
		return
	}
	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}
	if req.StreamLabel == "" {
		writeError(w, http.StatusBadRequest, "stream_label_required")
		return
	}
	if req.DurationMs != nil && *req.DurationMs < 0 {
		writeError(w, http.StatusBadRequest, "invalid_duration_ms")
		return
	}

	// Verify station exists; 404 if not. Stops a typo or stale embed from
	// silently filling the table with orphan rows.
	var station models.Station
	if err := a.db.WithContext(r.Context()).
		Select("id").
		Where("id = ?", req.StationID).
		First(&station).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "station_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	now := time.Now().UTC()
	event := models.ListenerEvent{
		ID:          uuid.NewString(),
		Timestamp:   now,
		EventType:   req.EventType,
		StationID:   req.StationID,
		StreamLabel: req.StreamLabel,
		DurationMs:  req.DurationMs,
		CreatedAt:   now,
	}

	if err := a.db.WithContext(r.Context()).Create(&event).Error; err != nil {
		a.logger.Error().Err(err).Str("station_id", req.StationID).Msg("listener-events: write failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	a.logger.Debug().
		Str("ip", ip).
		Str("event_type", req.EventType).
		Str("station_id", req.StationID).
		Str("stream_label", req.StreamLabel).
		Msg("listener-events: recorded")

	w.WriteHeader(http.StatusNoContent)
}
