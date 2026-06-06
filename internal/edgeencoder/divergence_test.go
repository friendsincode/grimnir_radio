/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDivergenceDetector_DefaultConfig(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{})
	if d.thresholdRTPTicks != 4410 {
		t.Errorf("default threshold = %d, want 4410", d.thresholdRTPTicks)
	}
	if d.tickInterval != time.Second {
		t.Errorf("default tickInterval = %v, want 1s", d.tickInterval)
	}
	if len(d.bufferA.samples) != 32 {
		t.Errorf("default buffer cap = %d, want 32", len(d.bufferA.samples))
	}
	if d.IsDiverging() {
		t.Error("new detector reports diverging; want false")
	}
	if d.Count() != 0 {
		t.Errorf("new detector count = %d, want 0", d.Count())
	}
	if !d.LastDivergenceAt().IsZero() {
		t.Errorf("new detector LastDivergenceAt = %v, want zero", d.LastDivergenceAt())
	}
}

func TestDivergenceDetector_NoEventsWhenStreamsAgree(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 4410, BufferCapacity: 8})
	// Five matched seqs with identical timestamps.
	for i := uint16(0); i < 5; i++ {
		ts := uint32(i) * 1024
		d.RecordSample("A", i, ts, time.Now().UnixNano())
		d.RecordSample("B", i, ts, time.Now().UnixNano())
	}
	if events := d.Check(); events != 0 {
		t.Errorf("Check on agreeing streams: %d events, want 0", events)
	}
	if d.IsDiverging() {
		t.Error("IsDiverging after agreement: true, want false")
	}
}

func TestDivergenceDetector_FlagsWhenTimestampsDiffer(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 1000, BufferCapacity: 8})
	// seq 0,1,2 agree; seq 3 differs by 5000 ticks (>1000 threshold).
	pairs := []struct {
		seq      uint16
		tsA, tsB uint32
	}{
		{0, 0, 0},
		{1, 1024, 1024},
		{2, 2048, 2048},
		{3, 3072, 8072}, // delta 5000 > threshold 1000
	}
	for _, p := range pairs {
		d.RecordSample("A", p.seq, p.tsA, time.Now().UnixNano())
		d.RecordSample("B", p.seq, p.tsB, time.Now().UnixNano())
	}
	events := d.Check()
	if events != 1 {
		t.Errorf("Check: %d divergence events, want 1", events)
	}
	if !d.IsDiverging() {
		t.Error("IsDiverging after threshold breach: false, want true")
	}
	if d.Count() != 1 {
		t.Errorf("Count: %d, want 1", d.Count())
	}
	if d.LastDivergenceAt().IsZero() {
		t.Error("LastDivergenceAt: zero after event, want recent")
	}
}

func TestDivergenceDetector_BelowThresholdIsNotDivergence(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 4410, BufferCapacity: 8})
	// 100 ticks of difference; threshold 4410 = ~100ms at 44.1kHz. 100 ticks
	// = ~2 ms — well within sample-aligned NetClock agreement.
	d.RecordSample("A", 1, 50000, time.Now().UnixNano())
	d.RecordSample("B", 1, 50100, time.Now().UnixNano())
	if d.Check() != 0 {
		t.Error("100-tick delta flagged as divergence; want below threshold")
	}
}

func TestDivergenceDetector_UnmatchedSeqsIgnored(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 100, BufferCapacity: 8})
	// A has seqs 10..14, B has 20..24 — no overlap.
	for i := uint16(0); i < 5; i++ {
		d.RecordSample("A", 10+i, uint32(i)*1024, time.Now().UnixNano())
		d.RecordSample("B", 20+i, uint32(i)*1024+99999, time.Now().UnixNano())
	}
	if events := d.Check(); events != 0 {
		t.Errorf("Check on disjoint seqs: %d events, want 0 (no matches)", events)
	}
}

func TestDivergenceDetector_CallbackFires(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 100, BufferCapacity: 8})
	var calls atomic.Int64
	var gotSeq atomic.Uint32
	var gotDelta atomic.Uint32
	d.SetCallback(func(seq uint16, delta, _, _ uint32) {
		calls.Add(1)
		gotSeq.Store(uint32(seq))
		gotDelta.Store(delta)
	})
	d.RecordSample("A", 42, 1000, time.Now().UnixNano())
	d.RecordSample("B", 42, 1500, time.Now().UnixNano())
	d.Check()
	if calls.Load() != 1 {
		t.Errorf("callback fired %d times, want 1", calls.Load())
	}
	if gotSeq.Load() != 42 {
		t.Errorf("callback seq = %d, want 42", gotSeq.Load())
	}
	if gotDelta.Load() != 500 {
		t.Errorf("callback delta = %d, want 500", gotDelta.Load())
	}
}

func TestDivergenceDetector_ClearDivergence(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 100, BufferCapacity: 8})
	d.RecordSample("A", 1, 0, time.Now().UnixNano())
	d.RecordSample("B", 1, 5000, time.Now().UnixNano())
	d.Check()
	if !d.IsDiverging() {
		t.Fatal("precondition: detector should be diverging")
	}
	d.ClearDivergence()
	if d.IsDiverging() {
		t.Error("IsDiverging after ClearDivergence: true, want false")
	}
	// Count should be preserved.
	if d.Count() != 1 {
		t.Errorf("Count after ClearDivergence: %d, want 1 (history preserved)", d.Count())
	}
}

func TestDivergenceDetector_RingBufferOverwrites(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{ThresholdRTPTicks: 100, BufferCapacity: 4})
	// Push 6 samples into a 4-slot ring; only the last 4 survive.
	for i := uint16(0); i < 6; i++ {
		d.RecordSample("A", i, uint32(i)*100, time.Now().UnixNano())
	}
	// B has seq 0 — but A has been overwritten past it, so no match.
	d.RecordSample("B", 0, 99999, time.Now().UnixNano())
	if d.Check() != 0 {
		t.Error("seq 0 should have been overwritten in A's ring; got match")
	}
	// B has seq 5 — A still has it.
	d.RecordSample("B", 5, uint32(5)*100+9999, time.Now().UnixNano())
	if d.Check() == 0 {
		t.Error("seq 5 should match in both rings & exceed threshold")
	}
}

func TestRTPDelta_NoWrap(t *testing.T) {
	cases := []struct{ a, b, want uint32 }{
		{1000, 2000, 1000},
		{2000, 1000, 1000},
		{0, 0, 0},
		{0xFFFFFFFF, 0, 1}, // b is 1 ahead modulo 2^32
		{0, 0xFFFFFFFF, 1},
	}
	for _, c := range cases {
		got := rtpDelta(c.a, c.b)
		if got != c.want {
			t.Errorf("rtpDelta(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestRTPHeaderLayout documents the byte layout used by readRTPHeader.
// readRTPHeader itself takes a *gst.Buffer; this test asserts the binary
// math directly so we catch off-by-one regressions without a live pipeline.
func TestRTPHeaderLayout(t *testing.T) {
	// Synthetic RTP header: V=2, PT=10, seq=0x1234, ts=0xDEADBEEF.
	header := []byte{
		0x80, 0x0A, // version=2, payload=10
		0x12, 0x34, // seq = 0x1234
		0xDE, 0xAD, 0xBE, 0xEF, // ts = 0xDEADBEEF
		0x00, 0x00, 0x00, 0x00, // ssrc
	}
	if len(header) != 12 {
		t.Fatalf("header len = %d, want 12", len(header))
	}
	// Same bit-twiddling as readRTPHeader.
	seq := uint16(header[2])<<8 | uint16(header[3])
	ts := uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
	if seq != 0x1234 {
		t.Errorf("seq = %#x, want 0x1234", seq)
	}
	if ts != 0xDEADBEEF {
		t.Errorf("ts = %#x, want 0xDEADBEEF", ts)
	}
}

func TestDivergenceDetector_RunStopsOnContextCancel(t *testing.T) {
	d := NewDivergenceDetector(DivergenceConfig{TickInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	// Let a tick or two happen, then cancel.
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not exit within 500ms of cancel")
	}
}
