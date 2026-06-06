/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// WebRTCIngressConfig configures the inbound WebRTC signaling server for the
// fan-out. The browser-side WebDJ client POSTs an SDP offer to /offer; the
// ingress builds a pion PeerConnection that accepts a single Opus audio track,
// fans it out via Pipeline to every engine in Engines.
//
// SessionMgr is required: every accepted peer becomes a Session managed by
// the central manager so gRPC GetStatus surfaces it correctly.
//
// Clock is optional; when non-nil it's passed straight to PipelineConfig so
// the fanned-out PCM-RTP carries NetClock-aligned timestamps.
type WebRTCIngressConfig struct {
	BindAddr   string
	Port       int
	Engines    []string
	SessionMgr *SessionMgr
	Clock      *gst.Clock

	// PipelineBuilder is the factory the ingress calls per accepted peer to
	// build the fan-out pipeline. Nil = use NewPipeline directly. Tests can
	// inject a stub here once the pipeline construction grows enough surface
	// to warrant it; currently only the WebRTC E2E in Chunk 10 needs it.
	PipelineBuilder func(PipelineConfig) (*Pipeline, error)
}

// WebRTCIngress owns the HTTP signaling server + every active pion peer.
// Construction is via NewWebRTCIngress; lifecycle is ListenAndServe + Shutdown.
type WebRTCIngress struct {
	cfg WebRTCIngressConfig
	srv *http.Server
	api *webrtc.API

	mu    sync.Mutex
	peers map[string]*webrtcPeer
}

type webrtcPeer struct {
	id      string
	pc      *webrtc.PeerConnection
	session *Session
	decoder *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
}

// NewWebRTCIngress validates cfg and prepares the HTTP server + a pion API
// with the Opus codec registered. Does not bind a socket; call ListenAndServe
// to start serving.
func NewWebRTCIngress(cfg WebRTCIngressConfig) (*WebRTCIngress, error) {
	if cfg.SessionMgr == nil {
		return nil, errors.New("WebRTCIngress: SessionMgr is required")
	}
	if len(cfg.Engines) == 0 {
		return nil, errors.New("WebRTCIngress: at least one engine target required")
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = "0.0.0.0"
	}
	if cfg.PipelineBuilder == nil {
		cfg.PipelineBuilder = NewPipeline
	}

	// Build a MediaEngine that only knows Opus; the WebDJ client always
	// sends Opus & registering fewer codecs keeps the SDP small.
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

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}

	ing := &WebRTCIngress{
		cfg:   cfg,
		api:   webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i)),
		peers: make(map[string]*webrtcPeer),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/offer", ing.handleOffer)
	ing.srv = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return ing, nil
}

// Handler returns the HTTP handler for /offer; main.go uses ListenAndServe but
// tests use httptest.NewServer(ing.Handler()) so they don't have to race on
// port availability.
func (ing *WebRTCIngress) Handler() http.Handler {
	return ing.srv.Handler
}

// ListenAndServe blocks until Shutdown or a fatal listener error.
func (ing *WebRTCIngress) ListenAndServe() error {
	return ing.srv.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server, closes every peer connection,
// and tears down every per-peer decoder subprocess + pipeline. Idempotent.
func (ing *WebRTCIngress) Shutdown(ctx context.Context) error {
	err := ing.srv.Shutdown(ctx)

	ing.mu.Lock()
	peers := make([]*webrtcPeer, 0, len(ing.peers))
	for _, p := range ing.peers {
		peers = append(peers, p)
	}
	ing.peers = make(map[string]*webrtcPeer)
	ing.mu.Unlock()

	for _, p := range peers {
		ing.tearDownPeer(p)
	}
	return err
}

// handleOffer is the POST /offer handler. Accepts an SDP offer JSON body,
// builds a PeerConnection that subscribes to one Opus audio track, attaches a
// Pipeline + decoder to fan the PCM out, and returns the answer SDP.
func (ing *WebRTCIngress) handleOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var offer webrtc.SessionDescription
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&offer); err != nil {
		http.Error(w, "bad offer json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if offer.Type != webrtc.SDPTypeOffer {
		http.Error(w, "expected SDP type 'offer'", http.StatusBadRequest)
		return
	}

	pc, err := ing.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, "create peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Recvonly transceiver for audio so the offer's m=audio line matches a
	// recvonly direction on our side; we never send back.
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		_ = pc.Close()
		http.Error(w, "add transceiver: "+err.Error(), http.StatusInternalServerError)
		return
	}

	session := ing.cfg.SessionMgr.Create(ProtocolWebRTC)
	peerCtx, cancel := context.WithCancel(context.Background())
	peer := &webrtcPeer{
		id:      session.ID,
		pc:      pc,
		session: session,
		cancel:  cancel,
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		ing.attachTrack(peerCtx, peer, track)
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed ||
			s == webrtc.PeerConnectionStateClosed ||
			s == webrtc.PeerConnectionStateDisconnected {
			ing.removePeer(peer.id)
		}
	})

	if err := pc.SetRemoteDescription(offer); err != nil {
		ing.cfg.SessionMgr.Remove(session.ID)
		_ = pc.Close()
		cancel()
		http.Error(w, "set remote desc: "+err.Error(), http.StatusBadRequest)
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		ing.cfg.SessionMgr.Remove(session.ID)
		_ = pc.Close()
		cancel()
		http.Error(w, "create answer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		ing.cfg.SessionMgr.Remove(session.ID)
		_ = pc.Close()
		cancel()
		http.Error(w, "set local desc: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Non-trickle: block on ICE gathering so the returned SDP carries every
	// candidate. The WebDJ frontend POSTs once & is done; we don't need a
	// separate /candidate endpoint for the v1 happy path.
	<-webrtc.GatheringCompletePromise(pc)

	ing.mu.Lock()
	ing.peers[peer.id] = peer
	ing.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pc.LocalDescription()); err != nil {
		// At this point headers may already be sent; just log via http.
		return
	}
}

// attachTrack runs once per incoming audio track. Builds the per-session
// Pipeline + opus-decode subprocess, then streams RTP payloads from the
// pion track into the decoder's stdin. The decoder writes PCM to stdout
// which we read & push into the Pipeline.
//
// The decoder is a `gst-launch-1.0` subprocess running
//
//	fdsrc fd=0 ! "application/x-rtp,..." ! rtpopusdepay ! opusparse !
//	opusdec ! audioconvert ! audioresample !
//	audio/x-raw,format=S16LE,rate=48000,channels=2 ! fdsink fd=1
//
// We feed it raw RTP packets so the rtp jitter buffer + depayloader live
// inside the subprocess; this matches the plan's "depay -> Opus decoder
// subprocess" wording.
func (ing *WebRTCIngress) attachTrack(ctx context.Context, peer *webrtcPeer, track *webrtc.TrackRemote) {
	if track.Kind() != webrtc.RTPCodecTypeAudio {
		return
	}

	pipeline, err := ing.cfg.PipelineBuilder(PipelineConfig{
		Engines: ing.cfg.Engines,
		Clock:   ing.cfg.Clock,
	})
	if err != nil {
		ing.removePeer(peer.id)
		return
	}
	if err := pipeline.Start(); err != nil {
		_ = pipeline.Stop()
		ing.removePeer(peer.id)
		return
	}
	peer.session.AttachPipeline(pipeline)
	_ = peer.session.transitionTo(SessionAuthenticating)
	_ = peer.session.transitionTo(SessionActive)

	decoder, stdin, stdout, err := startOpusDecoder()
	if err != nil {
		_ = pipeline.Stop()
		ing.removePeer(peer.id)
		return
	}
	peer.decoder = decoder
	peer.stdin = stdin

	// Pump PCM stdout -> pipeline appsrc.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				_ = pipeline.PushPCM(buf[:n])
				peer.session.recordBytesIn(uint64(n))
			}
			if err != nil {
				return
			}
		}
	}()

	// Pump pion RTP -> decoder stdin.
	go func() {
		defer func() { _ = stdin.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			peer.session.markPacket(time.Now())
			raw, err := pkt.Marshal()
			if err != nil {
				continue
			}
			if _, err := stdin.Write(raw); err != nil {
				return
			}
		}
	}()
}

// startOpusDecoder spawns the gst-launch decoder subprocess. The plan calls
// for this layout; keeping the launch string here (rather than in pipeline.go)
// because the existing Pipeline owns the PCM -> RTP path & doesn't know about
// Opus.
func startOpusDecoder() (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	cmd := exec.Command("gst-launch-1.0", "-q",
		"fdsrc", "fd=0", "!",
		"application/x-rtp,media=audio,clock-rate=48000,encoding-name=OPUS,payload=111", "!",
		"rtpjitterbuffer", "latency=80", "!",
		"rtpopusdepay", "!",
		"opusdec", "!",
		"audioconvert", "!",
		"audioresample", "!",
		"audio/x-raw,format=S16LE,rate=48000,channels=2", "!",
		"fdsink", "fd=1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, nil, err
	}
	return cmd, stdin, stdout, nil
}

// removePeer pulls the peer out of the live map & tears it down. Idempotent.
func (ing *WebRTCIngress) removePeer(id string) {
	ing.mu.Lock()
	peer, ok := ing.peers[id]
	if ok {
		delete(ing.peers, id)
	}
	ing.mu.Unlock()
	if !ok {
		return
	}
	ing.tearDownPeer(peer)
}

func (ing *WebRTCIngress) tearDownPeer(p *webrtcPeer) {
	if p.cancel != nil {
		p.cancel()
	}
	if p.session != nil && p.session.Pipeline != nil {
		_ = p.session.Pipeline.Stop()
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.decoder != nil && p.decoder.Process != nil {
		_ = p.decoder.Process.Kill()
		_ = p.decoder.Wait()
	}
	if p.pc != nil {
		_ = p.pc.Close()
	}
	if p.session != nil {
		ing.cfg.SessionMgr.Remove(p.session.ID)
	}
}

// ensure imports stay used even when one path strips the rtp import (helps
// `goimports` keep the package in sync if a future refactor inlines the
// marshal call).
var _ = (*rtp.Packet)(nil)
