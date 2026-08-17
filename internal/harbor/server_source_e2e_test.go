/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

// TestHandleSource_HappyPath_StreamsAndDisconnects drives a full source session
// end to end: a real live.Service over sqlite authorizes a seeded token, the
// connection is hijacked, audio flows through the (stubbed) decoder into the
// injected encoder, and disconnecting drops the active-connection count.
func TestHandleSource_HappyPath_StreamsAndDisconnects(t *testing.T) {
	db := dbtest.Open(t, &models.Station{}, &models.Mount{}, &models.LiveSession{}, &models.PlayHistory{}, &models.PrioritySource{})

	var (
		stationID = dbtest.UUID("st-1")
		mountID   = dbtest.UUID("mnt-1")
		token     = "secret-token-123"
	)
	if err := db.Create(&models.Station{ID: stationID, OwnerID: dbtest.UUID("owner")}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "live",
		Format: "mp3", Bitrate: 128, SampleRate: 44100, Channels: 2,
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	if err := db.Create(&models.LiveSession{
		ID: dbtest.UUID("sess-1"), StationID: stationID, MountID: mountID, UserID: dbtest.UUID("dj"),
		Token: token, Username: "dj-test", Priority: models.PriorityLiveOverride,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	bus := events.NewBus()
	liveSvc := live.NewService(db, priority.NewService(db, bus, zerolog.Nop()), bus, zerolog.Nop())

	pr, pw := io.Pipe()
	inj := &fakeInjector{w: pw, releaseCall: make(chan struct{})}

	port := freePort(t)
	s := &Server{
		cfg:      Config{Bind: "127.0.0.1", Port: port, MaxSources: 1, GStreamerBin: decoderStub},
		db:       db,
		liveSvc:  liveSvc,
		director: inj,
		bus:      bus,
		logger:   zerolog.Nop(),
		conns:    make(map[string]*SourceConnection),
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.ListenAndServeWithSOURCE() }()
	defer func() {
		_ = s.Shutdown(t.Context())
		<-serveErr
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn := dialUntilUp(t, addr)
	defer conn.Close()

	// Icecast-style PUT: Basic auth (username ignored, password is the token),
	// no Content-Length, then a raw audio stream on the same socket.
	auth := base64.StdEncoding.EncodeToString([]byte("source:" + token))
	fmt.Fprintf(conn, "PUT /live HTTP/1.1\r\nHost: %s\r\nAuthorization: Basic %s\r\nContent-Type: audio/mpeg\r\n\r\n", addr, auth)

	want := []byte("live-dj-audio-payload")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	// The decoder passthrough should deliver the audio to the injected encoder.
	got := make([]byte, len(want))
	readErr := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(pr, got)
		readErr <- err
	}()
	select {
	case err := <-readErr:
		if err != nil {
			t.Fatalf("read injected audio: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("audio never reached the encoder; source likely rejected before streaming")
	}
	if string(got) != string(want) {
		t.Fatalf("encoder received %q, want %q", got, want)
	}

	if n := s.ActiveConnections(); n != 1 {
		t.Fatalf("ActiveConnections during stream = %d, want 1", n)
	}

	// Disconnect: closing the socket ends the source, which must tear the
	// connection down and run HandleDisconnect.
	_ = conn.(*net.TCPConn).CloseWrite()
	deadline := time.Now().Add(5 * time.Second)
	for s.ActiveConnections() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("connection not cleaned up after disconnect; ActiveConnections = %d", s.ActiveConnections())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// handleSource writes an initial "Live DJ" PlayHistory row. It has no media,
	// so MediaID is empty; the nulluuid serializer stores that as NULL (before the
	// fix it sent '' and failed on Postgres with 22P02, dropping the row).
	var histCount int64
	db.Model(&models.PlayHistory{}).Where("station_id = ?", stationID).Count(&histCount)
	if histCount == 0 {
		t.Error("expected an initial Live DJ play-history row")
	}
}
