/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/playout"
)

// SourceConnection tracks an active source connection.
type SourceConnection struct {
	SessionID   string
	StationID   string
	MountID     string
	MountName   string
	ConnectedAt time.Time
	Metadata    map[string]string
	cancel      context.CancelFunc
}

// Config holds harbor-specific configuration.
type Config struct {
	Bind         string
	Port         int
	MountPrefix  string
	MaxSources   int
	GStreamerBin string
}

// Server is the built-in Icecast source receiver ("harbor").
// It accepts PUT and SOURCE method connections from BUTT, Mixxx, and other
// Icecast-compatible streaming software, decodes audio to raw PCM, and
// injects it into the playout encoder pipeline.
type Server struct {
	cfg      Config
	db       *gorm.DB
	liveSvc  *live.Service
	director *playout.Director
	bus      *events.Bus
	logger   zerolog.Logger

	httpServer *http.Server

	mu    sync.Mutex
	conns map[string]*SourceConnection // sessionID -> connection
}

// NewServer creates a new harbor server.
func NewServer(cfg Config, db *gorm.DB, liveSvc *live.Service, director *playout.Director, bus *events.Bus, logger zerolog.Logger) *Server {
	return &Server{
		cfg:      cfg,
		db:       db,
		liveSvc:  liveSvc,
		director: director,
		bus:      bus,
		logger:   logger.With().Str("component", "harbor").Logger(),
		conns:    make(map[string]*SourceConnection),
	}
}

// ListenAndServe starts the harbor HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleSource)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// No read/write timeout — source connections stream indefinitely.
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
		// Accept the non-standard SOURCE method used by legacy Icecast clients.
		ConnState: func(conn net.Conn, state http.ConnState) {},
	}

	s.logger.Info().Str("addr", addr).Msg("harbor server starting")
	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the harbor and disconnects all sources.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("harbor server shutting down")

	// Cancel all active source connections.
	s.mu.Lock()
	for _, conn := range s.conns {
		if conn.cancel != nil {
			conn.cancel()
		}
	}
	s.mu.Unlock()

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// ActiveConnections returns the number of active source connections.
func (s *Server) ActiveConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// handleSource is the main HTTP handler for incoming source connections.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	method := r.Method

	// Accept PUT only. Legacy SOURCE method is rewritten to PUT at the
	// connection level by sourceMethodConn (see ListenAndServeWithSOURCE).
	if method != http.MethodPut {
		http.Error(w, "Method not allowed. Use PUT.", http.StatusMethodNotAllowed)
		return
	}

	// Check max sources.
	s.mu.Lock()
	if len(s.conns) >= s.cfg.MaxSources {
		s.mu.Unlock()
		s.logger.Warn().Int("max", s.cfg.MaxSources).Msg("max sources reached, rejecting connection")
		http.Error(w, "Too many sources", http.StatusServiceUnavailable)
		return
	}
	s.mu.Unlock()

	// Parse Basic auth: username is ignored ("source"), password is the token.
	token, ok := s.parseBasicAuth(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return
	}

	// Validate mount path early to preserve explicit 400 for empty mount requests.
	requestPath := strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), " ")
	if prefix := strings.Trim(strings.TrimSpace(s.cfg.MountPrefix), "/"); prefix != "" {
		requestPath = strings.TrimPrefix(requestPath, prefix+"/")
	}
	if requestPath == "" {
		http.Error(w, "Mount path required", http.StatusBadRequest)
		return
	}

	sessionRecord, mount, err := s.resolveSessionAndMount(r.Context(), token, r.URL.Path)
	if err != nil {
		s.logger.Warn().Err(err).Str("path", r.URL.Path).Msg("failed to resolve session/mount for harbor source")
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	mountPath := strings.TrimPrefix(r.URL.Path, "/")

	// Validate and consume the token before connecting.
	if _, err := s.liveSvc.AuthorizeSource(r.Context(), mount.StationID, mount.ID, token); err != nil {
		s.logger.Warn().Err(err).Str("mount", mountPath).Msg("harbor token validation failed")
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Activate the session and trigger priority handover.
	sourceIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	session, err := s.liveSvc.HandleConnect(r.Context(), live.ConnectRequest{
		StationID: mount.StationID,
		MountID:   mount.ID,
		Token:     token,
		SourceIP:  sourceIP,
		UserAgent: r.Header.Get("User-Agent"),
	})
	if err != nil {
		s.logger.Warn().Err(err).Str("mount", mountPath).Msg("harbor auth failed")
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if session.ID != sessionRecord.ID {
		s.logger.Warn().
			Str("expected_session_id", sessionRecord.ID).
			Str("connected_session_id", session.ID).
			Msg("connected live session differs from resolved token session")
	}

	meta := parseIceHeaders(r)
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mpeg"
	}

	s.logger.Info().
		Str("session_id", session.ID).
		Str("station_id", mount.StationID).
		Str("mount", mountPath).
		Str("username", session.Username).
		Str("content_type", contentType).
		Str("remote_addr", r.RemoteAddr).
		Int64("content_length", r.ContentLength).
		Str("transfer_encoding", fmt.Sprintf("%v", r.TransferEncoding)).
		Str("proto", r.Proto).
		Msg("harbor source connected")

	// Create connection context. Use Background instead of r.Context() because
	// we will hijack the connection below, which causes r.Context() to cancel.
	connCtx, connCancel := context.WithCancel(context.Background())

	conn := &SourceConnection{
		SessionID:   session.ID,
		StationID:   mount.StationID,
		MountID:     mount.ID,
		MountName:   mountPath,
		ConnectedAt: time.Now(),
		Metadata:    meta,
		cancel:      connCancel,
	}

	s.mu.Lock()
	s.conns[session.ID] = conn
	s.mu.Unlock()

	// Cleanup on disconnect.
	defer func() {
		connCancel()
		s.mu.Lock()
		delete(s.conns, session.ID)
		s.mu.Unlock()

		if err := s.liveSvc.HandleDisconnect(context.Background(), session.ID); err != nil {
			s.logger.Error().Err(err).Str("session_id", session.ID).Msg("harbor disconnect error")
		}

		s.logger.Info().
			Str("session_id", session.ID).
			Str("mount", mountPath).
			Msg("harbor source disconnected")
	}()

	// Send 200 OK with Content-Length: 0 to prevent Go from using chunked
	// Transfer-Encoding on the response. Without this, nginx waits for chunk
	// data that never comes, blocking the response from reaching the client.
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.logger.Info().Str("session_id", session.ID).Msg("200 OK sent, entering streamAudio")

	// Read audio directly from the request body. Requires nginx to forward
	// the body with Transfer-Encoding: chunked (proxy_set_header Transfer-Encoding chunked).
	audioSource := r.Body

	// Inject live audio into the playout pipeline.
	s.streamAudio(connCtx, conn, mount, contentType, audioSource)
	s.logger.Info().Str("session_id", session.ID).Msg("streamAudio returned")
}

// streamAudio decodes compressed audio and injects raw PCM into the encoder pipeline.
func (s *Server) streamAudio(ctx context.Context, conn *SourceConnection, mount models.Mount, contentType string, audioSource io.Reader) {
	sampleRate := mount.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	channels := mount.Channels
	if channels == 0 {
		channels = 2
	}

	// Acquire the encoder's stdin from the playout director.
	s.logger.Info().
		Str("session_id", conn.SessionID).
		Str("station_id", conn.StationID).
		Str("mount_id", conn.MountID).
		Msg("calling InjectLiveSource")
	encoderIn, release, err := s.director.InjectLiveSource(ctx, conn.StationID, mount.ID)
	s.logger.Info().
		Str("session_id", conn.SessionID).
		Err(err).
		Msg("InjectLiveSource returned")
	if err != nil {
		s.logger.Error().Err(err).
			Str("session_id", conn.SessionID).
			Str("mount_id", conn.MountID).
			Msg("failed to inject live source into pipeline")
		return
	}
	defer release()

	// Start decoder: compressed audio → raw PCM.
	dec, err := startDecoder(ctx, s.cfg.GStreamerBin, contentType, sampleRate, channels, s.logger)
	if err != nil {
		s.logger.Error().Err(err).
			Str("session_id", conn.SessionID).
			Msg("failed to start harbor decoder")
		return
	}
	defer dec.Close()

	// Pipe decoded PCM from decoder stdout to encoder stdin.
	type copyResult struct {
		label string
		n     int64
		err   error
	}
	done := make(chan copyResult, 2)

	// Goroutine 1: decoder stdout → encoder stdin.
	go func() {
		n, err := io.Copy(encoderIn, dec.stdout)
		done <- copyResult{"decoder→encoder", n, err}
	}()

	// Goroutine 2: audio source (HTTP body) → decoder stdin.
	go func() {
		n, err := io.Copy(dec.stdin, audioSource)
		// Close decoder stdin to signal EOF to GStreamer.
		_ = dec.stdin.Close()
		done <- copyResult{"source→decoder", n, err}
	}()

	// Wait for either goroutine to finish (source disconnect or error).
	select {
	case <-ctx.Done():
		s.logger.Warn().Str("session_id", conn.SessionID).Msg("harbor connection context cancelled")
	case r := <-done:
		if r.err != nil {
			s.logger.Warn().Err(r.err).
				Str("session_id", conn.SessionID).
				Str("pipe", r.label).
				Int64("bytes", r.n).
				Msg("harbor stream pipe error")
		} else {
			s.logger.Info().
				Str("session_id", conn.SessionID).
				Str("pipe", r.label).
				Int64("bytes", r.n).
				Msg("harbor stream pipe closed (EOF)")
		}
		if r.label == "source→decoder" && r.n == 0 {
			s.logger.Warn().
				Str("session_id", conn.SessionID).
				Msg("harbor received zero audio bytes; check nginx proxy_request_buffering off and chunked transfer settings for harbor route")
		}
	}

	// Log decoder stderr if it captured any errors.
	if stderrOutput := dec.Stderr(); stderrOutput != "" {
		s.logger.Warn().
			Str("session_id", conn.SessionID).
			Str("stderr", stderrOutput).
			Msg("harbor decoder stderr output")
	}
}

// parseBasicAuth extracts the password from a Basic auth header.
// The username is ignored (conventionally "source" for Icecast).
func (s *Server) parseBasicAuth(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", false
	}

	if !strings.HasPrefix(auth, "Basic ") {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		return "", false
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", false
	}

	// parts[0] = username (ignored), parts[1] = token
	return parts[1], true
}

func (s *Server) resolveSessionAndMount(ctx context.Context, token, rawPath string) (*models.LiveSession, models.Mount, error) {
	path := strings.TrimPrefix(rawPath, "/")
	path = strings.TrimSpace(path)
	if prefix := strings.Trim(strings.TrimSpace(s.cfg.MountPrefix), "/"); prefix != "" {
		path = strings.TrimPrefix(path, prefix+"/")
	}
	if path == "" {
		return nil, models.Mount{}, fmt.Errorf("mount path required")
	}

	var session models.LiveSession
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&session).Error; err != nil {
		return nil, models.Mount{}, err
	}

	var mount models.Mount
	if err := s.db.WithContext(ctx).First(&mount, "id = ?", session.MountID).Error; err != nil {
		return nil, models.Mount{}, err
	}

	base := path
	if idx := strings.LastIndex(path, "."); idx > 0 {
		base = path[:idx]
	}
	if mount.Name != path && mount.Name != base {
		return nil, models.Mount{}, fmt.Errorf("mount mismatch")
	}

	return &session, mount, nil
}

// Addr returns the listen address of the harbor server.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
}

// sourceMethodHandler wraps the standard http.Server to also accept the non-standard
// SOURCE method used by legacy Icecast clients. Go's HTTP server rejects unknown methods
// with 501 by default, so we intercept at the connection level.
type sourceMethodListener struct {
	net.Listener
}

func (l *sourceMethodListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &sourceMethodConn{Conn: conn}, nil
}

// sourceMethodConn peeks at the first bytes to detect the SOURCE method and
// rewrites it to PUT so the standard HTTP parser can handle it.
type sourceMethodConn struct {
	net.Conn
	reader *bufio.Reader
	once   sync.Once
}

func (c *sourceMethodConn) Read(b []byte) (int, error) {
	c.once.Do(func() {
		c.reader = bufio.NewReaderSize(c.Conn, 4096)

		// Peek to see if this is a SOURCE request.
		peek, err := c.reader.Peek(7)
		if err != nil {
			return
		}
		if string(peek) == "SOURCE " {
			// Read and discard "SOURCE "
			buf := make([]byte, 7)
			_, _ = c.reader.Read(buf)
			// Prepend "PUT " for the HTTP parser
			c.reader = bufio.NewReaderSize(
				io.MultiReader(strings.NewReader("PUT "), c.reader),
				4096,
			)
		}
	})
	return c.reader.Read(b)
}

// ListenAndServeWithSOURCE starts the harbor server with support for the
// non-standard SOURCE HTTP method used by legacy Icecast clients.
func (s *Server) ListenAndServeWithSOURCE() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleSource)

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("harbor listen: %w", err)
	}

	s.logger.Info().Str("addr", addr).Msg("harbor server starting (with SOURCE method support)")
	err = s.httpServer.Serve(&sourceMethodListener{Listener: ln})
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
