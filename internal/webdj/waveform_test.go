/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webdj

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newWaveformSvc reuses the sqlite harness from service_test.go. meClient is nil,
// so generation always falls through the ffmpeg fallback to the placeholder.
func newWaveformSvc(t *testing.T) (*WaveformService, *gorm.DB) {
	t.Helper()
	_, db := newSvc(t)
	return NewWaveformService(db, nil, nil, t.TempDir(), zerolog.Nop()), db
}

// pcmStereo encodes frames as signed 16-bit little-endian interleaved stereo,
// which is the exact format the ffmpeg fallback pipes back.
func pcmStereo(frames [][2]int16) []byte {
	var buf bytes.Buffer
	for _, f := range frames {
		_ = binary.Write(&buf, binary.LittleEndian, f[0])
		_ = binary.Write(&buf, binary.LittleEndian, f[1])
	}
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// PCM peak extraction
// ---------------------------------------------------------------------------

func TestComputePeaksFromPCM_WindowsAndPeaks(t *testing.T) {
	// 100 Hz decoded at 10 samples/sec means a 10-frame window.
	frames := make([][2]int16, 0, 25)
	for i := 0; i < 25; i++ {
		switch {
		case i == 3:
			frames = append(frames, [2]int16{16384, -8192}) // peak of window 0
		case i == 15:
			frames = append(frames, [2]int16{-32768, 32767}) // peak of window 1
		case i == 22:
			frames = append(frames, [2]int16{8192, 4096}) // peak of the short tail
		default:
			frames = append(frames, [2]int16{100, -100})
		}
	}

	r := bufio.NewReader(bytes.NewReader(pcmStereo(frames)))
	left, right, total, err := computePeaksFromPCM(r, 100, 10)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if total != 25 {
		t.Fatalf("frames = %d, want 25", total)
	}
	// Two full windows plus the partial tail flushed at EOF.
	if len(left) != 3 || len(right) != 3 {
		t.Fatalf("windows = (%d, %d), want 3 each", len(left), len(right))
	}

	want := []struct{ l, r float32 }{
		{16384.0 / 32768.0, 8192.0 / 32768.0},
		{32767.0 / 32768.0, 32767.0 / 32768.0}, // -32768 folds to 32767
		{8192.0 / 32768.0, 4096.0 / 32768.0},
	}
	for i, w := range want {
		if left[i] != w.l {
			t.Fatalf("left[%d] = %v, want %v", i, left[i], w.l)
		}
		if right[i] != w.r {
			t.Fatalf("right[%d] = %v, want %v", i, right[i], w.r)
		}
	}
}

// A trailing odd byte count must not produce a bogus final frame.
func TestComputePeaksFromPCM_IgnoresPartialFrame(t *testing.T) {
	data := append(pcmStereo([][2]int16{{1000, 2000}, {3000, 4000}}), 0x7f, 0x7f)
	left, _, frames, err := computePeaksFromPCM(bufio.NewReader(bytes.NewReader(data)), 4, 2)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if frames != 2 {
		t.Fatalf("frames = %d, want 2 complete frames", frames)
	}
	if len(left) != 1 {
		t.Fatalf("windows = %d, want 1", len(left))
	}
}

func TestComputePeaksFromPCM_EmptyInput(t *testing.T) {
	left, right, frames, err := computePeaksFromPCM(bufio.NewReader(bytes.NewReader(nil)), 100, 10)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(left) != 0 || len(right) != 0 || frames != 0 {
		t.Fatalf("got (%d, %d, %d), want all zero", len(left), len(right), frames)
	}
}

func TestComputePeaksFromPCM_InvalidRates(t *testing.T) {
	for _, tc := range []struct{ sampleRate, samplesPerSec int }{
		{0, 10}, {100, 0}, {-1, 10}, {100, -5},
	} {
		if _, _, _, err := computePeaksFromPCM(bufio.NewReader(bytes.NewReader(nil)), tc.sampleRate, tc.samplesPerSec); err == nil {
			t.Fatalf("computePeaksFromPCM(%d, %d) accepted, want error", tc.sampleRate, tc.samplesPerSec)
		}
	}
}

// When samplesPerSec exceeds sampleRate the window floor is 1 frame, not 0,
// which would otherwise divide by zero.
func TestComputePeaksFromPCM_WindowFloorOfOne(t *testing.T) {
	frames := [][2]int16{{1000, -1000}, {2000, -2000}, {3000, -3000}}
	left, _, _, err := computePeaksFromPCM(bufio.NewReader(bytes.NewReader(pcmStereo(frames))), 5, 50)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(left) != 3 {
		t.Fatalf("windows = %d, want 3 (one per frame)", len(left))
	}
}

func TestAbs16(t *testing.T) {
	cases := map[int16]int16{
		0:      0,
		7:      7,
		-7:     7,
		32767:  32767,
		-32767: 32767,
		-32768: 32767, // saturates instead of overflowing back to itself
	}
	for in, want := range cases {
		if got := abs16(in); got != want {
			t.Fatalf("abs16(%d) = %d, want %d", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// placeholder generation
// ---------------------------------------------------------------------------

func TestGeneratePlaceholderWaveform_SampleCounts(t *testing.T) {
	w, _ := newWaveformSvc(t)

	for _, tc := range []struct {
		name       string
		durationMS int64
		want       int
	}{
		{"zero duration floors at 10", 0, 10},
		{"sub-second floors at 10", 400, 10},
		{"five seconds at 10/sec", 5000, 50},
		{"long recording caps at 10000", 5 * 60 * 60 * 1000, 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := w.generatePlaceholderWaveform("m1", tc.durationMS)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if len(data.PeakLeft) != tc.want || len(data.PeakRight) != tc.want {
				t.Fatalf("samples = (%d, %d), want %d", len(data.PeakLeft), len(data.PeakRight), tc.want)
			}
			if data.SamplesPerSec != 10 {
				t.Fatalf("samples/sec = %d, want 10", data.SamplesPerSec)
			}
			if data.DurationMS != tc.durationMS {
				t.Fatalf("duration = %d, want %d", data.DurationMS, tc.durationMS)
			}
			for i, v := range data.PeakLeft {
				if v < 0 || v > 1 {
					t.Fatalf("peakLeft[%d] = %v, outside the 0..1 range the UI draws", i, v)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// cache encoding
// ---------------------------------------------------------------------------

func TestCompressDecompressWaveform_RoundTrip(t *testing.T) {
	w, _ := newWaveformSvc(t)
	generated := time.Now().UTC().Truncate(time.Second)
	orig := &WaveformData{
		MediaID:       "m1",
		SamplesPerSec: 10,
		DurationMS:    4200,
		PeakLeft:      []float32{0, 0.25, 0.5, 0.75, 1},
		PeakRight:     []float32{1, 0.75, 0.5, 0.25, 0},
		GeneratedAt:   generated,
	}

	blob, err := w.compressWaveform(orig)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("compressed blob empty")
	}

	got, err := w.decompressWaveform(&models.WaveformCache{
		MediaID:       orig.MediaID,
		SamplesPerSec: orig.SamplesPerSec,
		DurationMS:    orig.DurationMS,
		PeakData:      blob,
		GeneratedAt:   generated,
	})
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if got.MediaID != orig.MediaID || got.SamplesPerSec != 10 || got.DurationMS != 4200 {
		t.Fatalf("header = %+v", got)
	}
	if !got.GeneratedAt.Equal(generated) {
		t.Fatalf("generated at = %v, want %v", got.GeneratedAt, generated)
	}
	for i := range orig.PeakLeft {
		if got.PeakLeft[i] != orig.PeakLeft[i] || got.PeakRight[i] != orig.PeakRight[i] {
			t.Fatalf("sample %d = (%v, %v), want (%v, %v)", i, got.PeakLeft[i], got.PeakRight[i], orig.PeakLeft[i], orig.PeakRight[i])
		}
	}
}

// Mono sources arrive with a short right channel; the encoder mirrors left so
// the decoder always reads a full stereo pair.
func TestCompressWaveform_MirrorsShortRightChannel(t *testing.T) {
	w, _ := newWaveformSvc(t)
	orig := &WaveformData{MediaID: "m1", PeakLeft: []float32{0.1, 0.2, 0.3}, PeakRight: []float32{0.9}}

	blob, err := w.compressWaveform(orig)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	got, err := w.decompressWaveform(&models.WaveformCache{MediaID: "m1", PeakData: blob})
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if len(got.PeakRight) != 3 {
		t.Fatalf("right channel = %d samples, want 3", len(got.PeakRight))
	}
	if got.PeakRight[0] != 0.9 {
		t.Fatalf("right[0] = %v, want the supplied 0.9", got.PeakRight[0])
	}
	if got.PeakRight[1] != 0.2 || got.PeakRight[2] != 0.3 {
		t.Fatalf("right tail = %v, want it mirrored from left", got.PeakRight[1:])
	}
}

func TestCompressWaveform_Empty(t *testing.T) {
	w, _ := newWaveformSvc(t)
	blob, err := w.compressWaveform(&WaveformData{MediaID: "m1"})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	got, err := w.decompressWaveform(&models.WaveformCache{MediaID: "m1", PeakData: blob})
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if len(got.PeakLeft) != 0 {
		t.Fatalf("samples = %d, want 0", len(got.PeakLeft))
	}
}

func TestDecompressWaveform_Errors(t *testing.T) {
	w, _ := newWaveformSvc(t)

	if _, err := w.decompressWaveform(&models.WaveformCache{MediaID: "m1", PeakData: []byte("not gzip at all")}); err == nil {
		t.Fatal("non-gzip payload accepted, want error")
	}

	if _, err := w.decompressWaveform(&models.WaveformCache{MediaID: "m1", PeakData: gzipBytes(t, nil)}); err == nil {
		t.Fatal("payload with no sample-count header accepted, want error")
	}

	// Header claims 4 samples but only one full stereo pair follows.
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, int32(4))
	_ = binary.Write(&body, binary.LittleEndian, float32(0.5))
	_ = binary.Write(&body, binary.LittleEndian, float32(0.5))
	if _, err := w.decompressWaveform(&models.WaveformCache{MediaID: "m1", PeakData: gzipBytes(t, body.Bytes())}); err == nil {
		t.Fatal("truncated sample data accepted, want error")
	}

	// Header claims 1 sample but the right channel is missing.
	var halfPair bytes.Buffer
	_ = binary.Write(&halfPair, binary.LittleEndian, int32(1))
	_ = binary.Write(&halfPair, binary.LittleEndian, float32(0.5))
	if _, err := w.decompressWaveform(&models.WaveformCache{MediaID: "m1", PeakData: gzipBytes(t, halfPair.Bytes())}); err == nil {
		t.Fatal("missing right channel accepted, want error")
	}
}

func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(payload); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// GetWaveform end to end
// ---------------------------------------------------------------------------

func TestGetWaveform_ServesFromCache(t *testing.T) {
	w, db := newWaveformSvc(t)
	orig := &WaveformData{
		MediaID:       "m1",
		SamplesPerSec: 10,
		DurationMS:    2000,
		PeakLeft:      []float32{0.1, 0.2},
		PeakRight:     []float32{0.3, 0.4},
		GeneratedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := w.cacheWaveform(bg(), orig); err != nil {
		t.Fatalf("cache: %v", err)
	}

	// No media_items row exists, so a cache miss would fail with ErrMediaNotFound.
	got, err := w.GetWaveform(bg(), "m1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.PeakLeft) != 2 || got.PeakLeft[1] != 0.2 {
		t.Fatalf("peaks = %v", got.PeakLeft)
	}

	var rows int64
	db.Model(&models.WaveformCache{}).Count(&rows)
	if rows != 1 {
		t.Fatalf("cache rows = %d, want 1", rows)
	}
}

// cacheWaveform upserts: regenerating a waveform replaces the row instead of
// erroring on the primary key.
func TestCacheWaveform_Upserts(t *testing.T) {
	w, db := newWaveformSvc(t)

	if err := w.cacheWaveform(bg(), &WaveformData{MediaID: "m1", SamplesPerSec: 10, DurationMS: 1000, PeakLeft: []float32{0.1}, PeakRight: []float32{0.1}}); err != nil {
		t.Fatalf("first cache: %v", err)
	}
	if err := w.cacheWaveform(bg(), &WaveformData{MediaID: "m1", SamplesPerSec: 5, DurationMS: 9999, PeakLeft: []float32{0.9}, PeakRight: []float32{0.9}}); err != nil {
		t.Fatalf("second cache: %v", err)
	}

	var rows []models.WaveformCache
	db.Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("cache rows = %d, want 1", len(rows))
	}
	if rows[0].DurationMS != 9999 || rows[0].SamplesPerSec != 5 {
		t.Fatalf("row = %+v, want the second write", rows[0])
	}
}

// A cache row that no longer decodes must not be fatal: the service falls
// through to regeneration, which here means the media lookup.
func TestGetWaveform_CorruptCacheFallsThrough(t *testing.T) {
	w, db := newWaveformSvc(t)
	db.Create(&models.WaveformCache{MediaID: "m1", SamplesPerSec: 10, PeakData: []byte("corrupt")})

	_, err := w.GetWaveform(bg(), "m1")
	if !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("err = %v, want ErrMediaNotFound after falling through the bad cache", err)
	}
}

func TestGetWaveform_MediaNotFound(t *testing.T) {
	w, _ := newWaveformSvc(t)
	if _, err := w.GetWaveform(bg(), "ghost"); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("err = %v, want ErrMediaNotFound", err)
	}
}

// With no media engine and no decodable file on disk, generation degrades to
// the placeholder and still caches the result so the next call is a hit.
func TestGetWaveform_GeneratesPlaceholderAndCaches(t *testing.T) {
	w, db := newWaveformSvc(t)
	seedMedia(t, db, "m1", "st1")

	got, err := w.GetWaveform(bg(), "m1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// 7m29s at 10 samples/sec.
	if len(got.PeakLeft) != 4490 {
		t.Fatalf("samples = %d, want 4490", len(got.PeakLeft))
	}

	var rows int64
	db.Model(&models.WaveformCache{}).Where("media_id = ?", "m1").Count(&rows)
	if rows != 1 {
		t.Fatalf("cached rows = %d, want the generated waveform to be stored", rows)
	}

	second, err := w.GetWaveform(bg(), "m1")
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if len(second.PeakLeft) != len(got.PeakLeft) {
		t.Fatalf("cached read returned %d samples, want %d", len(second.PeakLeft), len(got.PeakLeft))
	}
}

// ---------------------------------------------------------------------------
// invalidation
// ---------------------------------------------------------------------------

func TestDeleteAndInvalidateWaveform(t *testing.T) {
	w, db := newWaveformSvc(t)
	db.Create(&models.WaveformCache{MediaID: "m1", SamplesPerSec: 10})
	db.Create(&models.WaveformCache{MediaID: "m2", SamplesPerSec: 10})

	if err := w.DeleteWaveform(bg(), "m1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var remaining []models.WaveformCache
	db.Find(&remaining)
	if len(remaining) != 1 || remaining[0].MediaID != "m2" {
		t.Fatalf("remaining = %+v, want only m2", remaining)
	}

	// Deleting a row that is already gone is not an error.
	if err := w.DeleteWaveform(bg(), "m1"); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}

	if err := w.InvalidateWaveform(bg(), "m2"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	var count int64
	db.Model(&models.WaveformCache{}).Count(&count)
	if count != 0 {
		t.Fatalf("rows after invalidate = %d, want 0", count)
	}
}

func TestNewWaveformService(t *testing.T) {
	_, db := newSvc(t)
	root := t.TempDir()
	w := NewWaveformService(db, nil, nil, root, zerolog.Nop())
	if w.db != db {
		t.Fatal("db not wired")
	}
	if w.mediaRoot != root {
		t.Fatalf("mediaRoot = %q, want %q", w.mediaRoot, root)
	}
}
