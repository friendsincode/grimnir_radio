/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package webrtc provides WebRTC-based audio broadcasting using Pion.
package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Broadcaster manages WebRTC audio broadcasting to multiple peers.
type Broadcaster struct {
	mu     sync.RWMutex
	peers  map[string]*peerConnection
	track  *webrtc.TrackLocalStaticRTP
	api    *webrtc.API
	logger zerolog.Logger

	// RTP listener
	rtpPort   int
	rtpConn   *net.UDPConn
	rtpCancel context.CancelFunc

	// Stats
	totalPeers    int64
	bytesReceived int64
}

type peerConnection struct {
	id   string
	pc   *webrtc.PeerConnection
	done chan struct{}
}

// SignalMessage is the WebSocket signaling message format.
type SignalMessage struct {
	Type      string                    `json:"type"`
	SDP       *webrtc.SessionDescription `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Error     string                    `json:"error,omitempty"`
}

// Config holds broadcaster configuration.
type Config struct {
	RTPPort    int      // UDP port to receive RTP audio (default: 5004)
	STUNServer string   // STUN server URL (default: stun:stun.l.google.com:19302)
	ICEServers []string // Additional ICE servers
}

// NewBroadcaster creates a new WebRTC audio broadcaster.
func NewBroadcaster(cfg Config, logger zerolog.Logger) (*Broadcaster, error) {
	if cfg.RTPPort == 0 {
		cfg.RTPPort = 5004
	}
	if cfg.STUNServer == "" {
		cfg.STUNServer = "stun:stun.l.google.com:19302"
	}

	// Create MediaEngine with Opus codec
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register opus codec: %w", err)
	}

	// Create interceptor registry for RTCP handling
	i := &interceptor.Registry{}

	// Add PLI interval interceptor for keyframe requests (audio doesn't need this but good practice)
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		return nil, fmt.Errorf("create pli interceptor: %w", err)
	}
	i.Add(intervalPliFactory)

	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}

	// Create API with custom MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))

	// Create the audio track that will be shared by all peers
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"grimnir-radio",
	)
	if err != nil {
		return nil, fmt.Errorf("create audio track: %w", err)
	}

	b := &Broadcaster{
		peers:   make(map[string]*peerConnection),
		track:   track,
		api:     api,
		rtpPort: cfg.RTPPort,
		logger:  logger.With().Str("component", "webrtc-broadcaster").Logger(),
	}

	return b, nil
}

// Start begins listening for RTP audio and accepting WebRTC connections.
func (b *Broadcaster) Start(ctx context.Context) error {
	// Start RTP listener
	addr := &net.UDPAddr{Port: b.rtpPort}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP %d: %w", b.rtpPort, err)
	}
	b.rtpConn = conn

	rtpCtx, cancel := context.WithCancel(ctx)
	b.rtpCancel = cancel

	b.logger.Info().Int("port", b.rtpPort).Msg("RTP listener started")

	go b.readRTP(rtpCtx)

	return nil
}

// Stop shuts down the broadcaster.
func (b *Broadcaster) Stop() error {
	if b.rtpCancel != nil {
		b.rtpCancel()
	}

	if b.rtpConn != nil {
		b.rtpConn.Close()
	}

	// Close all peer connections
	b.mu.Lock()
	for _, peer := range b.peers {
		peer.pc.Close()
		close(peer.done)
	}
	b.peers = make(map[string]*peerConnection)
	b.mu.Unlock()

	b.logger.Info().Msg("broadcaster stopped")
	return nil
}

// readRTP reads RTP packets from UDP and writes them to the broadcast track.
func (b *Broadcaster) readRTP(ctx context.Context) {
	buf := make([]byte, 1500)
	packet := &rtp.Packet{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline to allow checking context
		b.rtpConn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, _, err := b.rtpConn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			b.logger.Error().Err(err).Msg("RTP read error")
			continue
		}

		// Parse RTP packet
		if err := packet.Unmarshal(buf[:n]); err != nil {
			b.logger.Debug().Err(err).Msg("invalid RTP packet")
			continue
		}

		b.bytesReceived += int64(n)

		// Write to all connected peers via the shared track
		if _, err := b.track.Write(buf[:n]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			b.logger.Debug().Err(err).Msg("track write error")
		}
	}
}

// HandleSignaling handles WebSocket signaling for a new peer.
func (b *Broadcaster) HandleSignaling(w http.ResponseWriter, r *http.Request) {
	// Accept WebSocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		b.logger.Error().Err(err).Msg("websocket accept failed")
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()
	peerID := fmt.Sprintf("peer-%d", time.Now().UnixNano())

	b.logger.Info().Str("peer_id", peerID).Msg("new signaling connection")

	// Create peer connection
	pc, err := b.createPeerConnection(peerID)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to create peer connection")
		wsjson.Write(ctx, conn, SignalMessage{Type: "error", Error: err.Error()})
		return
	}

	peer := &peerConnection{
		id:   peerID,
		pc:   pc,
		done: make(chan struct{}),
	}

	// Register peer
	b.mu.Lock()
	b.peers[peerID] = peer
	b.totalPeers++
	peerCount := len(b.peers)
	b.mu.Unlock()

	b.logger.Info().Str("peer_id", peerID).Int("total_peers", peerCount).Msg("peer registered")

	defer func() {
		b.mu.Lock()
		delete(b.peers, peerID)
		peerCount := len(b.peers)
		b.mu.Unlock()
		pc.Close()
		b.logger.Info().Str("peer_id", peerID).Int("total_peers", peerCount).Msg("peer disconnected")
	}()

	// Handle ICE candidates
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		wsjson.Write(ctx, conn, SignalMessage{
			Type:      "candidate",
			Candidate: &candidate,
		})
	})

	// Handle connection state changes
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		b.logger.Debug().Str("peer_id", peerID).Str("state", s.String()).Msg("connection state changed")
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			close(peer.done)
		}
	})

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		b.logger.Error().Err(err).Msg("failed to create offer")
		wsjson.Write(ctx, conn, SignalMessage{Type: "error", Error: err.Error()})
		return
	}

	// Set local description
	if err := pc.SetLocalDescription(offer); err != nil {
		b.logger.Error().Err(err).Msg("failed to set local description")
		wsjson.Write(ctx, conn, SignalMessage{Type: "error", Error: err.Error()})
		return
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	// Send offer to client
	if err := wsjson.Write(ctx, conn, SignalMessage{
		Type: "offer",
		SDP:  pc.LocalDescription(),
	}); err != nil {
		b.logger.Error().Err(err).Msg("failed to send offer")
		return
	}

	// Read messages from client
	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.done:
			return
		default:
		}

		var msg SignalMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			if websocket.CloseStatus(err) != -1 {
				return // Normal close
			}
			b.logger.Debug().Err(err).Msg("websocket read error")
			return
		}

		switch msg.Type {
		case "answer":
			if msg.SDP != nil {
				if err := pc.SetRemoteDescription(*msg.SDP); err != nil {
					b.logger.Error().Err(err).Msg("failed to set remote description")
				}
			}
		case "candidate":
			if msg.Candidate != nil {
				if err := pc.AddICECandidate(*msg.Candidate); err != nil {
					b.logger.Error().Err(err).Msg("failed to add ICE candidate")
				}
			}
		}
	}
}

// createPeerConnection creates a new peer connection with the audio track.
func (b *Broadcaster) createPeerConnection(peerID string) (*webrtc.PeerConnection, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	pc, err := b.api.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// Add the audio track
	if _, err := pc.AddTrack(b.track); err != nil {
		pc.Close()
		return nil, fmt.Errorf("add track: %w", err)
	}

	return pc, nil
}

// PeerCount returns the number of connected peers.
func (b *Broadcaster) PeerCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.peers)
}

// Stats returns broadcaster statistics.
func (b *Broadcaster) Stats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return map[string]interface{}{
		"peers":          len(b.peers),
		"total_peers":    b.totalPeers,
		"bytes_received": b.bytesReceived,
		"rtp_port":       b.rtpPort,
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// MarshalJSON implements json.Marshaler for stats endpoint.
func (b *Broadcaster) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Stats())
}
