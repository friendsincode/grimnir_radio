/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-gst/go-gst/gst"
)

// srtGstInitOnce guards a single gst.Init(nil) for the SRT listener path.
// Production main.go reaches Serve() with no prior gst init (the SRT
// pipeline binds the listening UDP port, so building it has to happen
// before any client connects). Idempotent; cheap on the hot path.
var srtGstInitOnce sync.Once

// SRTListenerConfig configures the SRT ingress terminator. SRT (Secure
// Reliable Transport) is a UDP-based protocol tuned for low-latency
// contribution links; the fan-out exposes a single SRT endpoint per
// instance and decodes whatever payload (typically MPEG-TS or raw audio)
// the upstream broadcaster sends.
//
// BindAddr + Port name the socket. In listener mode (the default) remote
// DJs dial the fan-out as the SRT caller. In caller mode the fan-out dials
// out to a remote SRT listener, useful when the DJ side is the publishing
// endpoint of a managed studio link.
//
// Engines is the slice of media-engine PCM-RTP destinations the per-session
// Pipeline duplicates audio to; mirrors PipelineConfig.Engines.
//
// Sessions is the manager that owns the lifecycle of the SRT Session. The
// listener calls Sessions.Create(ProtocolSRT) on accept and Sessions.Remove
// on EOS.
//
// SourceLaunchOverride is the unit-test seam: when non-empty it replaces
// the real srtsrc fragment so tests can drive the listener with
// audiotestsrc and never bind to the network. Production callers leave it
// blank.
type SRTListenerConfig struct {
	BindAddr             string
	Port                 int
	Mode                 string // "listener" (default) or "caller"
	Engines              []string
	Sessions             *SessionMgr
	SourceLaunchOverride string
}

// SRTListener is the per-instance SRT terminator. One listener owns one
// SRT port; one Session is active at a time (the srtsrc element accepts a
// single caller). Future chunks may multiplex by spawning N listeners on N
// ports if the deployment needs concurrent SRT DJs.
type SRTListener struct {
	cfg SRTListenerConfig
}

// NewSRTListener validates cfg and returns a listener ready for Serve.
// Returns an error when required fields are missing.
func NewSRTListener(cfg SRTListenerConfig) (*SRTListener, error) {
	if len(cfg.Engines) == 0 {
		return nil, fmt.Errorf("srt listener: at least one engine target required")
	}
	if cfg.Sessions == nil {
		return nil, fmt.Errorf("srt listener: SessionMgr required")
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = "0.0.0.0"
	}
	if cfg.Mode == "" {
		cfg.Mode = "listener"
	}
	return &SRTListener{cfg: cfg}, nil
}

// Serve runs the SRT terminator until ctx is canceled. Blocks the caller.
// Construction errors (bad pipeline) are returned directly; runtime EOS or
// peer disconnect is reported as nil so the caller can loop reconnect if
// desired.
//
// Lifecycle per connection:
//  1. Build per-session Pipeline with srtsrc as SourceLaunch.
//  2. Register Session with SessionMgr (Protocol = ProtocolSRT).
//  3. Start pipeline; transition session to Active.
//  4. Wait for pipeline Done (EOS, error, or Stop via ctx cancel).
//  5. Stop pipeline + Remove session from manager.
func (l *SRTListener) Serve(ctx context.Context) error {
	srtGstInitOnce.Do(func() { gst.Init(nil) })

	sess := l.cfg.Sessions.Create(ProtocolSRT)
	defer l.cfg.Sessions.Remove(sess.ID)

	pipe, err := NewPipeline(PipelineConfig{
		Engines:      l.cfg.Engines,
		SourceLaunch: buildSRTSourceLaunch(l.cfg),
	})
	if err != nil {
		return fmt.Errorf("srt: build pipeline: %w", err)
	}
	sess.AttachPipeline(pipe)
	// Authenticating -> Active best-effort; transition failure is non-fatal
	// (real auth lands in Chunk 7).
	_ = sess.transitionTo(SessionAuthenticating)
	_ = sess.transitionTo(SessionActive)

	if err := pipe.Start(); err != nil {
		_ = pipe.Stop()
		return fmt.Errorf("srt: start pipeline: %w", err)
	}

	select {
	case <-pipe.Done():
		_ = pipe.Stop()
		return nil
	case <-ctx.Done():
		_ = pipe.Stop()
		return nil
	}
}

// buildSRTSourceLaunch assembles the srtsrc fragment for the per-session
// pipeline. Pulled out so unit tests can assert the resulting string
// without standing up a real pipeline.
//
// The decode chain (decodebin ! audioconvert ! audioresample) handles
// whatever payload the SRT caller sends; SRT transports MPEG-TS by
// convention but srtsrc + decodebin will negotiate raw audio too.
func buildSRTSourceLaunch(cfg SRTListenerConfig) string {
	if cfg.SourceLaunchOverride != "" {
		return cfg.SourceLaunchOverride
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "listener"
	}
	uri := fmt.Sprintf("srt://%s:%d?mode=%s", cfg.BindAddr, cfg.Port, mode)
	return fmt.Sprintf("srtsrc uri=%s ! decodebin ! audioconvert ! audioresample", uri)
}
