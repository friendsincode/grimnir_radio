/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/media"
)

// newFileOps builds a FileOperations backed by filesystem storage rooted in a
// temp dir, and returns the source dir and the media root.
func newFileOps(t *testing.T) (*FileOperations, string, string) {
	t.Helper()
	sourceDir := t.TempDir()
	mediaRoot := t.TempDir()

	mediaSvc, err := media.NewService(&config.Config{MediaRoot: mediaRoot}, zerolog.Nop())
	if err != nil {
		t.Fatalf("media service: %v", err)
	}
	return NewFileOperations(mediaSvc, zerolog.Nop()), sourceDir, mediaRoot
}

func writeSource(t *testing.T, dir, name, content string) (string, int64) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path, int64(len(content))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// options and small helpers
// ---------------------------------------------------------------------------

func TestDefaultCopyOptions(t *testing.T) {
	opts := DefaultCopyOptions()
	if !opts.VerifyChecksum {
		t.Fatal("checksum verification should be on by default")
	}
	if !opts.SkipExisting {
		t.Fatal("SkipExisting should default true")
	}
	if opts.Concurrency != 4 {
		t.Fatalf("concurrency = %d, want 4", opts.Concurrency)
	}
}

func TestGetFileSize(t *testing.T) {
	dir := t.TempDir()
	path, size := writeSource(t, dir, "track.mp3", "twelve bytes")

	got, err := GetFileSize(path)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if got != size {
		t.Fatalf("size = %d, want %d", got, size)
	}

	if _, err := GetFileSize(filepath.Join(dir, "absent.mp3")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestVerifyFile(t *testing.T) {
	fo, sourceDir, _ := newFileOps(t)
	const content = "the actual audio bytes"
	path, _ := writeSource(t, sourceDir, "track.mp3", content)

	ok, err := fo.VerifyFile(path, sha256Hex(content))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("matching checksum reported as a mismatch")
	}

	ok, err = fo.VerifyFile(path, sha256Hex("different bytes"))
	if err != nil {
		t.Fatalf("verify mismatch: %v", err)
	}
	if ok {
		t.Fatal("mismatched checksum reported as a match")
	}

	if _, err := fo.VerifyFile(filepath.Join(sourceDir, "absent.mp3"), "abc"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// ---------------------------------------------------------------------------
// CopyFiles
// ---------------------------------------------------------------------------

func TestCopyFiles_CopiesAndChecksums(t *testing.T) {
	fo, sourceDir, mediaRoot := newFileOps(t)

	contents := map[string]string{
		"media-1": "first track bytes",
		"media-2": "second track bytes, a little longer",
		"media-3": "third",
	}
	var jobs []FileCopyJob
	for id, body := range contents {
		path, size := writeSource(t, sourceDir, id+".mp3", body)
		jobs = append(jobs, FileCopyJob{SourcePath: path, StationID: "st1", MediaID: id, FileSize: size})
	}

	results, err := fo.CopyFiles(context.Background(), jobs, DefaultCopyOptions())
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	for _, r := range results {
		if !r.Success {
			t.Fatalf("%s failed: %v", r.MediaID, r.Error)
		}
		if r.StorageKey == "" {
			t.Fatalf("%s has no storage key", r.MediaID)
		}
		body := contents[r.MediaID]
		if r.BytesCopied != int64(len(body)) {
			t.Fatalf("%s copied %d bytes, want %d", r.MediaID, r.BytesCopied, len(body))
		}
		if r.Checksum != sha256Hex(body) {
			t.Fatalf("%s checksum = %q, want the sha256 of its contents", r.MediaID, r.Checksum)
		}

		// The bytes must actually be on disk under the media root, and the
		// checksum reset must not have truncated the upload.
		stored, err := os.ReadFile(filepath.Join(mediaRoot, r.StorageKey))
		if err != nil {
			t.Fatalf("read stored %s: %v", r.MediaID, err)
		}
		if string(stored) != body {
			t.Fatalf("%s stored %q, want %q", r.MediaID, stored, body)
		}
	}
}

// With verification off, no checksum is computed and the file pointer is never
// rewound, so this is the path where a truncated upload would show up.
func TestCopyFiles_WithoutChecksumStillStoresFullFile(t *testing.T) {
	fo, sourceDir, mediaRoot := newFileOps(t)
	const body = "unverified but complete"
	path, size := writeSource(t, sourceDir, "track.mp3", body)

	opts := DefaultCopyOptions()
	opts.VerifyChecksum = false

	results, err := fo.CopyFiles(context.Background(), []FileCopyJob{
		{SourcePath: path, StationID: "st1", MediaID: "media-1", FileSize: size},
	}, opts)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Checksum != "" {
		t.Fatalf("checksum = %q, want empty when verification is off", results[0].Checksum)
	}

	stored, err := os.ReadFile(filepath.Join(mediaRoot, results[0].StorageKey))
	if err != nil {
		t.Fatalf("read stored: %v", err)
	}
	if string(stored) != body {
		t.Fatalf("stored %q, want %q", stored, body)
	}
}

// A source file listed in the source DB but missing from disk is the common
// case in a half-migrated library. It fails that one job, not the batch.
func TestCopyFiles_MissingSourceFailsOnlyThatJob(t *testing.T) {
	fo, sourceDir, _ := newFileOps(t)
	good, size := writeSource(t, sourceDir, "present.mp3", "here")

	results, err := fo.CopyFiles(context.Background(), []FileCopyJob{
		{SourcePath: good, StationID: "st1", MediaID: "media-ok", FileSize: size},
		{SourcePath: filepath.Join(sourceDir, "gone.mp3"), StationID: "st1", MediaID: "media-gone", FileSize: 99},
	}, DefaultCopyOptions())
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	byID := map[string]FileCopyResult{}
	for _, r := range results {
		byID[r.MediaID] = r
	}
	if !byID["media-ok"].Success {
		t.Fatalf("good job failed: %v", byID["media-ok"].Error)
	}
	missing := byID["media-gone"]
	if missing.Success {
		t.Fatal("missing source reported as a success")
	}
	if missing.Error == nil {
		t.Fatal("missing source has no error")
	}
	if missing.BytesCopied != 0 {
		t.Fatalf("missing source counted %d bytes copied", missing.BytesCopied)
	}
}

func TestCopyFiles_ProgressCallback(t *testing.T) {
	fo, sourceDir, _ := newFileOps(t)

	var jobs []FileCopyJob
	for i := 0; i < 5; i++ {
		name := string(rune('a'+i)) + ".mp3"
		path, size := writeSource(t, sourceDir, name, "body-"+name)
		jobs = append(jobs, FileCopyJob{SourcePath: path, StationID: "st1", MediaID: name, FileSize: size})
	}

	var mu sync.Mutex
	var copied []int
	var sawTotal int
	opts := DefaultCopyOptions()
	opts.ProgressCallback = func(c, total int) {
		mu.Lock()
		defer mu.Unlock()
		copied = append(copied, c)
		sawTotal = total
	}

	results, err := fo.CopyFiles(context.Background(), jobs, opts)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("results = %d, want 5", len(results))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(copied) != 5 {
		t.Fatalf("callback fired %d times, want once per file", len(copied))
	}
	if sawTotal != 5 {
		t.Fatalf("callback total = %d, want 5", sawTotal)
	}
	sort.Ints(copied)
	if copied[len(copied)-1] != 5 {
		t.Fatalf("final copied count = %d, want 5", copied[len(copied)-1])
	}
}

func TestCopyFiles_EmptyJobList(t *testing.T) {
	fo, _, _ := newFileOps(t)

	results, err := fo.CopyFiles(context.Background(), nil, DefaultCopyOptions())
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0", len(results))
	}
}

// Concurrency is a required field, not an optional one: with zero workers no
// job is ever consumed and CopyFiles returns no results while reporting no
// error. Both production callers use DefaultCopyOptions, which sets 4.
func TestCopyFiles_ZeroConcurrencyCopiesNothing(t *testing.T) {
	fo, sourceDir, _ := newFileOps(t)
	path, size := writeSource(t, sourceDir, "track.mp3", "body")

	results, err := fo.CopyFiles(context.Background(), []FileCopyJob{
		{SourcePath: path, StationID: "st1", MediaID: "media-1", FileSize: size},
	}, CopyOptions{Concurrency: 0})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d; if this now copies, the zero-worker trap was fixed and this test should assert the fix", len(results))
	}
}

// Single-worker copies keep the whole batch serialized, which is what the
// importer falls back to on constrained hosts.
func TestCopyFiles_SingleWorker(t *testing.T) {
	fo, sourceDir, _ := newFileOps(t)

	var jobs []FileCopyJob
	for i := 0; i < 4; i++ {
		name := string(rune('a'+i)) + ".mp3"
		path, size := writeSource(t, sourceDir, name, "body-"+name)
		jobs = append(jobs, FileCopyJob{SourcePath: path, StationID: "st1", MediaID: name, FileSize: size})
	}

	opts := DefaultCopyOptions()
	opts.Concurrency = 1

	results, err := fo.CopyFiles(context.Background(), jobs, opts)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Fatalf("%s failed: %v", r.MediaID, r.Error)
		}
	}
}

// Two stations importing at once must not collide in storage.
func TestCopyFiles_SeparatesStations(t *testing.T) {
	fo, sourceDir, _ := newFileOps(t)
	pathA, sizeA := writeSource(t, sourceDir, "a.mp3", "station one audio")
	pathB, sizeB := writeSource(t, sourceDir, "b.mp3", "station two audio")

	results, err := fo.CopyFiles(context.Background(), []FileCopyJob{
		{SourcePath: pathA, StationID: "st1", MediaID: "media-1", FileSize: sizeA},
		{SourcePath: pathB, StationID: "st2", MediaID: "media-2", FileSize: sizeB},
	}, DefaultCopyOptions())
	if err != nil {
		t.Fatalf("copy: %v", err)
	}

	keys := map[string]string{}
	for _, r := range results {
		if !r.Success {
			t.Fatalf("%s failed: %v", r.MediaID, r.Error)
		}
		keys[r.MediaID] = r.StorageKey
	}
	if keys["media-1"] == keys["media-2"] {
		t.Fatalf("both stations stored to the same key %q", keys["media-1"])
	}
}
