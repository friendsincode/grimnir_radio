/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package broadcast provides a simple audio broadcast server that receives
// audio from GStreamer and serves it to multiple HTTP clients.
package broadcast

import (
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

// Mount represents a single audio stream mount point.
type Mount struct {
	Name        string
	ContentType string
	Bitrate     int

	mu         sync.RWMutex
	clients    map[*client]struct{}
	buffer     *ringBuffer
	logger     zerolog.Logger
	inputDone  chan struct{}
	inputCount int         // tracks active input feeds
	bus        *events.Bus // for publishing listener stats
}

type client struct {
	ch     chan []byte
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

// ringBuffer holds recent audio data for new clients to start with.
type ringBuffer struct {
	data []byte
	size int
	pos  int
	mu   sync.RWMutex
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		data: make([]byte, size),
		size: size,
	}
}

func (rb *ringBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, b := range p {
		rb.data[rb.pos] = b
		rb.pos = (rb.pos + 1) % rb.size
	}
}

func (rb *ringBuffer) GetRecent(n int) []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.size {
		n = rb.size
	}

	result := make([]byte, n)
	start := (rb.pos - n + rb.size) % rb.size

	for i := 0; i < n; i++ {
		result[i] = rb.data[(start+i)%rb.size]
	}
	return result
}

func (rb *ringBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	// Zero out the buffer and reset position
	for i := range rb.data {
		rb.data[i] = 0
	}
	rb.pos = 0
}

// NewMount creates a new broadcast mount point.
func NewMount(name, contentType string, bitrate int, logger zerolog.Logger, bus *events.Bus) *Mount {
	// Buffer 5 seconds of audio for new clients at the stream's bitrate
	// Larger buffer helps with connection stability and quick starts
	// At 128kbps: 128000 bits/sec = 16000 bytes/sec, so 80000 bytes = 5 seconds
	// At 64kbps: 64000 bits/sec = 8000 bytes/sec, so 40000 bytes = 5 seconds
	bufferSize := (bitrate * 1000 / 8) * 5
	if bufferSize < 20000 {
		bufferSize = 20000 // Minimum ~2.5 seconds at 64kbps
	}

	return &Mount{
		Name:        name,
		ContentType: contentType,
		Bitrate:     bitrate,
		clients:     make(map[*client]struct{}),
		buffer:      newRingBuffer(bufferSize),
		logger:      logger.With().Str("mount", name).Logger(),
		inputDone:   make(chan struct{}),
		bus:         bus,
	}
}

// Broadcast sends audio data to all connected clients.
func (m *Mount) Broadcast(data []byte) {
	if len(data) == 0 {
		return
	}

	// Store in ring buffer for new clients
	m.buffer.Write(data)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for c := range m.clients {
		c.mu.Lock()
		if !c.closed {
			select {
			case c.ch <- data:
			default:
				// Client is slow, skip this chunk
			}
		}
		c.mu.Unlock()
	}
}

// FeedFrom reads from an io.Reader and broadcasts the data.
// This is typically connected to GStreamer's stdout.
// Note: Call ClearBuffer() on all related mounts BEFORE calling FeedFrom
// to ensure synchronized buffer clearing across HQ/LQ mount pairs.
func (m *Mount) FeedFrom(r io.Reader) error {
	// Register this feed
	m.mu.Lock()
	m.inputCount++
	// Reset inputDone channel if this is the first active feed
	if m.inputCount == 1 {
		m.inputDone = make(chan struct{})
	}
	count := m.inputCount
	m.mu.Unlock()

	m.logger.Info().Int("input_count", count).Msg("feed started")

	defer func() {
		m.mu.Lock()
		m.inputCount--
		// Only signal done when no more active feeds
		if m.inputCount == 0 {
			select {
			case <-m.inputDone:
				// Already closed
			default:
				close(m.inputDone)
			}
		}
		m.mu.Unlock()
	}()

	buf := make([]byte, 4096)
	totalBytes := 0
	lastLog := time.Now()
	for {
		n, err := r.Read(buf)
		if n > 0 {
			totalBytes += n
			// Log every 10 seconds to show data is flowing
			if time.Since(lastLog) > 10*time.Second {
				m.logger.Info().Int("bytes_last_10s", totalBytes).Int("clients", m.ClientCount()).Msg("feed active")
				totalBytes = 0
				lastLog = time.Now()
			}
			// Make a copy since we're broadcasting asynchronously
			data := make([]byte, n)
			copy(data, buf[:n])
			m.Broadcast(data)
		}
		if err != nil {
			if err == io.EOF {
				m.logger.Info().Msg("input stream ended (EOF)")
			} else {
				m.logger.Error().Err(err).Msg("input read error")
			}
			return err
		}
	}
}

// ServeHTTP handles HTTP client connections for streaming.
func (m *Mount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set headers for streaming audio
	// NOTE: Do NOT set Transfer-Encoding manually - Go handles it automatically
	// when Content-Length is not set
	w.Header().Set("Content-Type", m.ContentType)
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")
	// Explicitly delete Content-Length to force chunked transfer
	w.Header().Del("Content-Length")

	// ICY metadata headers
	w.Header().Set("icy-br", itoa(m.Bitrate))
	w.Header().Set("icy-name", m.Name)

	// Check if client wants to skip buffer (used when switching quality)
	// This prevents audio "jumping" when switching between HQ and LQ
	skipBuffer := r.URL.Query().Get("nobuffer") == "1"

	// Log buffer state on connect for debugging sync issues
	m.buffer.mu.RLock()
	bufPos := m.buffer.pos
	m.buffer.mu.RUnlock()
	m.logger.Debug().Int("buffer_pos", bufPos).Bool("skip_buffer", skipBuffer).Msg("client connecting, buffer state")

	// Try to get the Flusher interface - check for wrapped writers
	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
	} else {
		// Try ResponseController as fallback (Go 1.20+)
		rc := http.NewResponseController(w)
		// Create a wrapper that uses ResponseController
		flusher = &rcFlusher{rc: rc, logger: m.logger}
	}

	// Helper to write and flush data
	writeAndFlush := func(data []byte) error {
		_, err := w.Write(data)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Create client with larger buffer for stability
	// 256 chunks * ~4KB = ~1MB buffer, helps prevent drops during network hiccups
	c := &client{
		ch:   make(chan []byte, 256),
		done: make(chan struct{}),
	}

	// Register client
	m.mu.Lock()
	m.clients[c] = struct{}{}
	clientCount := len(m.clients)
	m.mu.Unlock()

	m.logger.Info().Int("clients", clientCount).Bool("quality_switch", skipBuffer).Msg("client connected")

	// Publish listener stats event
	m.publishListenerStats(clientCount, "connect")

	// Send buffered data to help client start faster
	// For quality switches (nobuffer=1), send minimal data to prime connection
	// This prevents browser timeout while waiting for live data
	if skipBuffer {
		// Send ~200ms of audio to prime the connection without affecting sync much
		// At 128kbps: 200ms = ~3KB, At 64kbps: 200ms = ~1.5KB
		primeBytes := (m.Bitrate * 1000 / 8) / 5 // 200ms of audio
		if primeBytes < 1000 {
			primeBytes = 1000
		}
		if recent := m.buffer.GetRecent(primeBytes); len(recent) > 0 {
			if err := writeAndFlush(recent); err != nil {
				m.logger.Info().Err(err).Msg("initial buffer write failed (skipBuffer)")
				return
			}
		}
	} else {
		// Send 2 seconds of audio for quick start and stable playback
		// At 128kbps: 2s = 32KB, At 64kbps: 2s = 16KB
		bufferBytes := (m.Bitrate * 1000 / 8) * 2 // 2 seconds of audio
		if bufferBytes > 64000 {
			bufferBytes = 64000 // Cap at 64KB (~4s at 128kbps)
		}
		if bufferBytes < 8000 {
			bufferBytes = 8000 // Minimum 8KB
		}
		if recent := m.buffer.GetRecent(bufferBytes); len(recent) > 0 {
			if err := writeAndFlush(recent); err != nil {
				m.logger.Info().Err(err).Int("bytes", len(recent)).Msg("initial buffer write failed")
				return
			}
			m.logger.Info().Int("bytes", len(recent)).Msg("initial buffer sent, entering main loop")
		}
	}

	// Cleanup on disconnect
	defer func() {
		c.mu.Lock()
		c.closed = true
		close(c.done)
		c.mu.Unlock()

		m.mu.Lock()
		delete(m.clients, c)
		clientCount := len(m.clients)
		m.mu.Unlock()

		m.logger.Info().Int("clients", clientCount).Msg("client disconnected")

		// Publish listener stats event
		m.publishListenerStats(clientCount, "disconnect")
	}()

	// Create a single timer for keepalive - reused instead of creating new ones
	keepalive := time.NewTimer(30 * time.Second)
	defer keepalive.Stop()

	m.logger.Info().Int("channel_len", len(c.ch)).Msg("entering main streaming loop")

	// Stream data to client - keep connected through track transitions
	writeCount := 0
	for {
		select {
		case <-r.Context().Done():
			m.logger.Info().Int("writes", writeCount).Err(r.Context().Err()).Msg("client context cancelled")
			return
		case data := <-c.ch:
			if err := writeAndFlush(data); err != nil {
				m.logger.Info().Err(err).Int("writes", writeCount).Msg("write failed, disconnecting client")
				return
			}
			writeCount++
			// Log first few writes to debug streaming issues
			if writeCount <= 5 || writeCount%100 == 0 {
				m.logger.Info().Int("writes", writeCount).Int("bytes", len(data)).Msg("wrote chunk to client")
			}
			// Reset keepalive timer after successful write
			if !keepalive.Stop() {
				select {
				case <-keepalive.C:
				default:
				}
			}
			keepalive.Reset(30 * time.Second)
		case <-keepalive.C:
			// No data for 30 seconds - flush and continue waiting
			// This keeps the connection alive during gaps between tracks
			m.logger.Debug().Int("writes", writeCount).Msg("keepalive flush")
			flusher.Flush()
			keepalive.Reset(30 * time.Second)
		}
	}
}

// ClientCount returns the number of connected clients.
func (m *Mount) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// publishListenerStats publishes a listener_stats event via the event bus
func (m *Mount) publishListenerStats(clientCount int, eventType string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(events.EventListenerStats, events.Payload{
		"mount":        m.Name,
		"bitrate":      m.Bitrate,
		"listeners":    clientCount,
		"event":        eventType, // "connect" or "disconnect"
		"content_type": m.ContentType,
	})
}

// ClearBuffer clears the ring buffer to prepare for a new track.
// This should be called on all related mounts (e.g., HQ and LQ) together
// before starting new feeds to ensure synchronized playback.
func (m *Mount) ClearBuffer() {
	m.logger.Debug().Msg("clearing buffer for new track")
	m.buffer.Clear()
}

// Close disconnects all clients.
func (m *Mount) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for c := range m.clients {
		c.mu.Lock()
		c.closed = true
		close(c.done)
		c.mu.Unlock()
	}
	m.clients = make(map[*client]struct{})
}

// rcFlusher wraps http.ResponseController to implement http.Flusher
type rcFlusher struct {
	rc        *http.ResponseController
	logger    zerolog.Logger
	errLogged bool // only log flush errors once per connection
}

func (f *rcFlusher) Flush() {
	if err := f.rc.Flush(); err != nil && !f.errLogged {
		f.logger.Debug().Err(err).Msg("ResponseController flush failed")
		f.errLogged = true
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// Server manages multiple broadcast mounts.
type Server struct {
	mounts map[string]*Mount
	mu     sync.RWMutex
	logger zerolog.Logger
	bus    *events.Bus
}

// NewServer creates a new broadcast server.
func NewServer(logger zerolog.Logger, bus *events.Bus) *Server {
	return &Server{
		mounts: make(map[string]*Mount),
		logger: logger,
		bus:    bus,
	}
}

// CreateMount creates a new mount point.
func (s *Server) CreateMount(name, contentType string, bitrate int) *Mount {
	s.mu.Lock()
	defer s.mu.Unlock()

	mount := NewMount(name, contentType, bitrate, s.logger, s.bus)
	s.mounts[name] = mount
	return mount
}

// GetMount returns a mount by name.
func (s *Server) GetMount(name string) *Mount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mounts[name]
}

// RemoveMount removes and closes a mount.
func (s *Server) RemoveMount(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if mount, ok := s.mounts[name]; ok {
		mount.Close()
		delete(s.mounts, name)
	}
}

// MountStats contains listener statistics for a mount.
type MountStats struct {
	Name        string `json:"name"`
	Bitrate     int    `json:"bitrate"`
	ContentType string `json:"content_type"`
	Listeners   int    `json:"listeners"`
}

// GetListenerStats returns listener counts for all mounts.
func (s *Server) GetListenerStats() []MountStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make([]MountStats, 0, len(s.mounts))
	for _, mount := range s.mounts {
		stats = append(stats, MountStats{
			Name:        mount.Name,
			Bitrate:     mount.Bitrate,
			ContentType: mount.ContentType,
			Listeners:   mount.ClientCount(),
		})
	}
	return stats
}

// TotalListeners returns the total number of listeners across all mounts.
func (s *Server) TotalListeners() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, mount := range s.mounts {
		total += mount.ClientCount()
	}
	return total
}

// ServeHTTP routes requests to the appropriate mount.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract mount name from path (e.g., /broadcast/main -> main)
	name := r.URL.Path
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	mount := s.GetMount(name)
	if mount == nil {
		http.Error(w, "Mount not found", http.StatusNotFound)
		return
	}

	mount.ServeHTTP(w, r)
}
