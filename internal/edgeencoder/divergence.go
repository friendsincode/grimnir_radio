/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DivergenceDetector compares RTP-level state of two clock-synced engines
// (issue #236). When NetClock-synced engines diverge on the RTP timestamp
// for a matched sequence number, one of the engines has clock drift, a
// NetClock bind loss, an encoder bug, or a race. Phase 1 observes & reports
// only; it does not pin or force-switch. Audio-fingerprint comparison is a
// follow-up.
//
// Usage:
//   - Construct via NewDivergenceDetector.
//   - Pipeline pad probes call RecordSample("A"|"B", seq, ts, recordedAtNs)
//     once every N RTP packets (default sample rate is set by the probe).
//   - Run() blocks on a 1-second ticker; each tick Check()s matched sequence
//     numbers and flips state.
//   - Status providers read IsDiverging / Count / LastAt to surface in gRPC.
//
// The detector is intentionally cheap: ring buffers of 32 samples each
// (~12 KB total at 16 bytes per sample), atomic counters, & a single mutex
// only held during the per-tick comparison.
type DivergenceDetector struct {
	// thresholdRTPTicks is the maximum |tsA - tsB| (in RTP clock units) that
	// counts as "in agreement". At 44.1 kHz, 4410 ticks = 100 ms.
	thresholdRTPTicks uint32

	// tickInterval is how often Check() runs. 1 s matches the design.
	tickInterval time.Duration

	mu      sync.Mutex
	bufferA *sampleRing
	bufferB *sampleRing

	count       atomic.Int64
	lastEventAt atomic.Int64 // unix-nanos; 0 = never
	diverging   atomic.Bool

	// Hooks for tests / wiring. nil = no-op.
	onDivergence func(seq uint16, deltaTicks uint32, tsA, tsB uint32)
}

// rtpSample is a fixed-size record so the ring buffer can hold them inline.
type rtpSample struct {
	seq        uint16
	rtpTS      uint32
	recordedAt int64 // unix-nanos
	valid      bool
}

// sampleRing is a fixed-capacity ring of rtpSamples. Writes overwrite the
// oldest entry. Reads scan the whole ring for a sequence-number lookup.
type sampleRing struct {
	samples []rtpSample
	head    int
}

func newSampleRing(capacity int) *sampleRing {
	return &sampleRing{samples: make([]rtpSample, capacity)}
}

func (r *sampleRing) push(s rtpSample) {
	r.samples[r.head] = s
	r.head = (r.head + 1) % len(r.samples)
}

// DivergenceConfig parameterizes a detector.
type DivergenceConfig struct {
	// ThresholdRTPTicks is the maximum allowed |tsA - tsB| for a matched seq.
	// Default 4410 = 100 ms at 44.1 kHz.
	ThresholdRTPTicks uint32

	// TickInterval is how often Check runs. Default 1 s.
	TickInterval time.Duration

	// BufferCapacity is the ring-buffer depth per branch. Default 32.
	BufferCapacity int
}

// NewDivergenceDetector returns a detector with the given config. Zero fields
// fall back to defaults.
func NewDivergenceDetector(cfg DivergenceConfig) *DivergenceDetector {
	if cfg.ThresholdRTPTicks == 0 {
		cfg.ThresholdRTPTicks = 4410 // 100 ms at 44.1 kHz
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = time.Second
	}
	if cfg.BufferCapacity == 0 {
		cfg.BufferCapacity = 32
	}
	return &DivergenceDetector{
		thresholdRTPTicks: cfg.ThresholdRTPTicks,
		tickInterval:      cfg.TickInterval,
		bufferA:           newSampleRing(cfg.BufferCapacity),
		bufferB:           newSampleRing(cfg.BufferCapacity),
	}
}

// SetCallback registers a callback fired on every divergence event detected
// by Check. nil clears it. Useful for logging from the wiring layer.
func (d *DivergenceDetector) SetCallback(cb func(seq uint16, deltaTicks uint32, tsA, tsB uint32)) {
	d.mu.Lock()
	d.onDivergence = cb
	d.mu.Unlock()
}

// RecordSample appends one (seq, rtpTS) reading from the named input branch.
// recordedAt is wall-clock nanos at probe time; pass time.Now().UnixNano().
// Safe to call from the GStreamer streaming thread.
func (d *DivergenceDetector) RecordSample(input string, seq uint16, rtpTS uint32, recordedAt int64) {
	sample := rtpSample{seq: seq, rtpTS: rtpTS, recordedAt: recordedAt, valid: true}
	d.mu.Lock()
	switch input {
	case "A":
		d.bufferA.push(sample)
	case "B":
		d.bufferB.push(sample)
	}
	d.mu.Unlock()
}

// Check scans both ring buffers for matched sequence numbers. If any matched
// pair has |tsA - tsB| > threshold, divergence is flagged: counter incremented,
// timestamp recorded, IsDiverging set to true. Returns the number of matched
// pairs that exceeded threshold on this call (zero means "no divergence this
// tick" — but IsDiverging stays true until ClearDivergence is called).
func (d *DivergenceDetector) Check() int {
	d.mu.Lock()
	// Snapshot under the lock; release before invoking callbacks to keep
	// the streaming thread unblocked.
	bufA := append([]rtpSample(nil), d.bufferA.samples...)
	bufB := append([]rtpSample(nil), d.bufferB.samples...)
	cb := d.onDivergence
	d.mu.Unlock()

	events := 0
	for _, a := range bufA {
		if !a.valid {
			continue
		}
		for _, b := range bufB {
			if !b.valid || b.seq != a.seq {
				continue
			}
			delta := rtpDelta(a.rtpTS, b.rtpTS)
			if delta > d.thresholdRTPTicks {
				events++
				if cb != nil {
					cb(a.seq, delta, a.rtpTS, b.rtpTS)
				}
			}
			break // each A sample needs at most one matching B
		}
	}
	if events > 0 {
		d.count.Add(int64(events))
		d.lastEventAt.Store(time.Now().UnixNano())
		d.diverging.Store(true)
	}
	return events
}

// rtpDelta computes the minimum cyclic distance between two uint32 RTP
// timestamps. RTP timestamps wrap mod 2^32; a raw subtraction near the
// wrap boundary would look like ~2^32 instead of the true tiny offset.
// We pick the smaller of the two directed differences.
func rtpDelta(a, b uint32) uint32 {
	fwd := a - b // mod 2^32 by uint32 arithmetic
	bwd := b - a
	if fwd < bwd {
		return fwd
	}
	return bwd
}

// IsDiverging reports the current sticky state. Cleared by ClearDivergence.
func (d *DivergenceDetector) IsDiverging() bool { return d.diverging.Load() }

// Count returns the total number of divergence events observed since startup.
func (d *DivergenceDetector) Count() int64 { return d.count.Load() }

// LastDivergenceAt returns the wall-clock time of the most recent event, or
// the zero Time if none has occurred.
func (d *DivergenceDetector) LastDivergenceAt() time.Time {
	ns := d.lastEventAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// ClearDivergence resets the sticky IsDiverging flag. Count and LastDivergenceAt
// are preserved so operators can see history. Intended for future use (e.g.,
// after operator acknowledgment, or once a recovery window without events
// elapses).
func (d *DivergenceDetector) ClearDivergence() {
	d.diverging.Store(false)
}

// Run blocks until ctx is cancelled, calling Check on tickInterval.
func (d *DivergenceDetector) Run(ctx context.Context) {
	t := time.NewTicker(d.tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.Check()
		}
	}
}
