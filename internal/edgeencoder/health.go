/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"sync/atomic"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// InputHealth tracks the liveness of one input branch. Combines two signals:
//   - Packet arrival timestamp (updated by RecordPacket; healthy if within window)
//   - gRPC health gate (manual setter; defaults to true so health is purely
//     packet-based when no gRPC subscription is configured)
type InputHealth struct {
	window       time.Duration
	lastPacketNs atomic.Int64
	grpcHealthy  atomic.Bool
}

// NewInputHealth returns a tracker that considers the input healthy when a
// packet was recorded within the last `window` duration AND the gRPC gate is
// open. The gRPC gate defaults to open so callers that don't wire a gRPC
// subscription get packet-only health.
func NewInputHealth(window time.Duration) *InputHealth {
	ih := &InputHealth{window: window}
	ih.grpcHealthy.Store(true)
	return ih
}

// RecordPacket marks the current time as the last packet-arrival timestamp.
// Safe to call from any goroutine (including the GStreamer streaming thread
// via a pad probe).
func (ih *InputHealth) RecordPacket() {
	ih.lastPacketNs.Store(time.Now().UnixNano())
}

// SetGRPCHealthy flips the gRPC health gate. When false, IsHealthy returns
// false regardless of packet arrivals.
func (ih *InputHealth) SetGRPCHealthy(healthy bool) {
	ih.grpcHealthy.Store(healthy)
}

// IsHealthy reports whether both the gRPC gate is open and a packet arrived
// within the configured window.
func (ih *InputHealth) IsHealthy() bool {
	if !ih.grpcHealthy.Load() {
		return false
	}
	last := ih.lastPacketNs.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < ih.window
}

// AttachPadProbe installs a buffer probe on the given pad that calls
// RecordPacket for every buffer that passes through. Returns the probe ID
// so callers can later RemoveProbe on teardown.
func (ih *InputHealth) AttachPadProbe(pad *gst.Pad) uint64 {
	return pad.AddProbe(gst.PadProbeTypeBuffer, func(_ *gst.Pad, _ *gst.PadProbeInfo) gst.PadProbeReturn {
		ih.RecordPacket()
		return gst.PadProbeOK
	})
}
