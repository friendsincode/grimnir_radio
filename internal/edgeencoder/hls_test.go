/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package edgeencoder

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type mockS3 struct {
	mu      sync.Mutex
	uploads map[string][]byte // bucket/key -> body
}

func newMockS3() *mockS3 {
	return &mockS3{uploads: make(map[string][]byte)}
}

func (m *mockS3) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads[bucket+"/"+key] = data
	return nil
}

func (m *mockS3) Uploads() map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.uploads))
	for k, v := range m.uploads {
		out[k] = v
	}
	return out
}

func TestHLSUploader_UploadsNewTSFile(t *testing.T) {
	dir := t.TempDir()
	s3 := newMockS3()
	u, err := NewHLSUploader(dir, "test-bucket", s3)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	// Let the watcher register before producing events.
	time.Sleep(100 * time.Millisecond)

	segPath := filepath.Join(dir, "segment00001.ts")
	if err := os.WriteFile(segPath, []byte("fake ts data"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.UploadedCount() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	uploads := s3.Uploads()
	if _, ok := uploads["test-bucket/hls/segment00001.ts"]; !ok {
		t.Errorf("uploads = %v, want key test-bucket/hls/segment00001.ts", keys(uploads))
	}
	if got := string(uploads["test-bucket/hls/segment00001.ts"]); got != "fake ts data" {
		t.Errorf("upload body = %q, want %q", got, "fake ts data")
	}
}

func TestHLSUploader_UploadsM3U8File(t *testing.T) {
	dir := t.TempDir()
	s3 := newMockS3()
	u, err := NewHLSUploader(dir, "test-bucket", s3)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = u.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)

	playlistPath := filepath.Join(dir, "playlist.m3u8")
	if err := os.WriteFile(playlistPath, []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.UploadedCount() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	uploads := s3.Uploads()
	if _, ok := uploads["test-bucket/hls/playlist.m3u8"]; !ok {
		t.Errorf("uploads = %v, want test-bucket/hls/playlist.m3u8", keys(uploads))
	}
}

func TestHLSUploader_IgnoresNonHLSFiles(t *testing.T) {
	dir := t.TempDir()
	s3 := newMockS3()
	u, err := NewHLSUploader(dir, "test-bucket", s3)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	go func() { _ = u.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(dir, "random.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	if got := u.UploadedCount(); got != 0 {
		t.Errorf("uploaded = %d, want 0 (non-HLS files should be ignored)", got)
	}
}

func TestNewHLSUploader_Validation(t *testing.T) {
	tmp := t.TempDir()
	s3 := newMockS3()

	if _, err := NewHLSUploader("/nonexistent/path/xyzzy", "b", s3); err == nil {
		t.Error("want error for missing dir, got nil")
	}
	if _, err := NewHLSUploader(tmp, "", s3); err == nil {
		t.Error("want error for empty bucket, got nil")
	}
	if _, err := NewHLSUploader(tmp, "b", nil); err == nil {
		t.Error("want error for nil s3 client, got nil")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
