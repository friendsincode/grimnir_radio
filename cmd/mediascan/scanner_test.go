/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestIsMediaFile(t *testing.T) {
	media := []string{"song.mp3", "TRACK.FLAC", "a.ogg", "b.m4a", "c.aac", "d.wav", "e.wma", "f.opus", "x.audio"}
	for _, n := range media {
		if !isMediaFile(n) {
			t.Errorf("isMediaFile(%q) = false, want true", n)
		}
	}
	notMedia := []string{"cover.jpg", "notes.txt", "playlist.m3u", "archive.zip", "noext", "video.mp4"}
	for _, n := range notMedia {
		if isMediaFile(n) {
			t.Errorf("isMediaFile(%q) = true, want false", n)
		}
	}
}

func TestComputeFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	content := []byte("grimnir mediascan hash test")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := computeFileHash(path)
	if err != nil {
		t.Fatalf("computeFileHash: %v", err)
	}
	sum := sha256.Sum256(content)
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("hash = %s, want %s", got, want)
	}

	if _, err := computeFileHash(filepath.Join(dir, "missing")); err == nil {
		t.Error("hashing a missing file should error")
	}
}

// TestScan_WalksFiltersAndHashes drives the real scan pipeline with metadata
// probing off (so no ffprobe dependency): it must walk recursively, keep only
// media files, hash each, compute paths relative to the root, and roll up
// accurate stats.
func TestScan_WalksFiltersAndHashes(t *testing.T) {
	root := t.TempDir()
	writeFile := func(rel string, data []byte) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("a.mp3", []byte("aaa"))
	writeFile("b.flac", []byte("bbbb"))
	writeFile("cover.jpg", []byte("not media"))      // filtered out
	writeFile("sub/c.ogg", []byte("ccccc"))          // recursion
	writeFile("sub/readme.txt", []byte("ignore me")) // filtered out

	s := &scanner{dirs: []string{root}, workers: 2, noMetadata: true, sourceType: "azuracast"}
	m, err := s.scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if m.Stats.TotalFiles != 3 {
		t.Fatalf("TotalFiles = %d, want 3 (mp3+flac+ogg, not jpg/txt)", m.Stats.TotalFiles)
	}
	if m.Stats.Errors != 0 {
		t.Errorf("Errors = %d, want 0", m.Stats.Errors)
	}
	if want := int64(3 + 4 + 5); m.Stats.TotalSize != want {
		t.Errorf("TotalSize = %d, want %d", m.Stats.TotalSize, want)
	}

	rels := make([]string, len(m.Files))
	for i, f := range m.Files {
		rels[i] = filepath.ToSlash(f.RelativePath)
		if f.ContentHash == "" {
			t.Errorf("%s has no content hash", f.RelativePath)
		}
		if f.Metadata != nil {
			t.Errorf("%s: metadata should be nil with noMetadata=true", f.RelativePath)
		}
	}
	sort.Strings(rels)
	want := []string{"a.mp3", "b.flac", "sub/c.ogg"}
	for i := range want {
		if rels[i] != want[i] {
			t.Errorf("relative paths = %v, want %v", rels, want)
			break
		}
	}
}
